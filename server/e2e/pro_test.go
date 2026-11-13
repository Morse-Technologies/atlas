//go:build e2e

package e2e

import (
	"crypto/sha512"
	"encoding/base64"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/signing"
)

const alertsTimeout = 2 * time.Minute

// proFollowsLicense checks the lock on a Core installation and the features on one started with a license
// (`deploy/scripts/dev-env.sh --pro`, ATLAS_E2E_EDITION=pro).
func (j *journey) proFollowsLicense(t *testing.T) {
	if os.Getenv("ATLAS_E2E_EDITION") == "pro" {
		j.proWorks(t)
		return
	}
	j.proIsLocked(t)
}

type alertJSON struct {
	ID          string   `json:"id"`
	Rule        string   `json:"rule"`
	Status      string   `json:"status"`
	DocumentIDs []string `json:"document_ids"`
}

// installLicense installs the development license the way an administrator does, after a refused file left
// the installation as it was.
func (j *journey) installLicense(t *testing.T) {
	refused := j.api.do(request{method: http.MethodPut, path: "/system/license", body: map[string]string{"content": "not a license"}})
	var problem struct {
		Detail string `json:"detail"`
	}
	expect(t, refused, http.StatusUnprocessableEntity).json(t, &problem)
	if problem.Detail == "" {
		t.Fatalf("a refused license must say why: %s", refused.body)
	}
	raw, err := os.ReadFile(os.Getenv("ATLAS_E2E_LICENSE_FILE"))
	if err != nil {
		t.Fatalf("the Pro journey needs ATLAS_E2E_LICENSE_FILE (deploy/scripts/e2e.sh --pro sets it): %v", err)
	}
	var installed struct {
		Plan string `json:"plan"`
	}
	expect(t, j.api.do(request{method: http.MethodPut, path: "/system/license", body: map[string]string{"content": string(raw)}}), http.StatusOK).json(t, &installed)
	if installed.Plan != "pro" {
		t.Fatalf("plan after installing = %q", installed.Plan)
	}
}

func (j *journey) proWorks(t *testing.T) {
	j.installLicense(t)
	var lic struct {
		Plan     string   `json:"plan"`
		Features []string `json:"features"`
	}
	expect(t, j.api.get("/system/license"), http.StatusOK).json(t, &lic)
	if lic.Plan != "pro" || !slices.Contains(lic.Features, "alerts") || !slices.Contains(lic.Features, "map") {
		t.Fatalf("license = %+v", lic)
	}
	j.mapShowsDocuments(t)
	j.giveSameCode(t)
	alerts := j.waitForAlerts(t)
	j.decideAlert(t, alerts)
	var graph struct {
		Nodes []struct{ ID, Kind string } `json:"nodes"`
		Edges []struct{ ID string }       `json:"edges"`
	}
	expect(t, j.api.get("/graph/neighborhood"), http.StatusOK).json(t, &graph)
	if len(graph.Nodes) < 2 {
		t.Fatalf("the map has %d nodes; the documents and what they mention should be there", len(graph.Nodes))
	}
}

// mapShowsDocuments checks the map of a fresh installation, where nothing links the documents yet: they are still there.
func (j *journey) mapShowsDocuments(t *testing.T) {
	var graph struct {
		Nodes []struct{ Kind string } `json:"nodes"`
	}
	expect(t, j.api.get("/graph/neighborhood"), http.StatusOK).json(t, &graph)
	documents := 0
	for _, n := range graph.Nodes {
		if n.Kind == "document" {
			documents++
		}
	}
	if documents < 2 {
		t.Fatalf("the map shows %d documents before any link exists; want every indexed document", documents)
	}
}

// giveSameCode sets one code on every indexed document: Atlas does not read codes from the text yet (backlog),
// and documents with the same code are what the DUPLICATE rule looks for.
func (j *journey) giveSameCode(t *testing.T) {
	for _, d := range j.indexed() {
		expect(t, j.api.patch("/documents/"+d.ID+"/metadata", map[string]string{"code": "PNT-014"}), http.StatusOK)
	}
}

// waitForAlerts waits for the quality rules to run: the two identical PDFs must raise a DUPLICATE alert.
func (j *journey) waitForAlerts(t *testing.T) []alertJSON {
	var alerts []alertJSON
	eventually(t, alertsTimeout, "the DUPLICATE alert", func() bool {
		expect(t, j.api.get("/alerts"), http.StatusOK).json(t, &alerts)
		return slices.ContainsFunc(alerts, func(a alertJSON) bool { return a.Rule == "DUPLICATE" })
	})
	return alerts
}

func (j *journey) decideAlert(t *testing.T, alerts []alertJSON) {
	i := slices.IndexFunc(alerts, func(a alertJSON) bool { return a.Rule == "DUPLICATE" })
	path, key := "/alerts/"+alerts[i].ID, map[string]string{"Idempotency-Key": "e2e-alert-1"}
	patch := request{method: http.MethodPatch, path: path, body: map[string]any{"status": "dismissed", "note": "same file, two names"}, headers: key}
	first, second := expect(t, j.api.do(patch), http.StatusOK), expect(t, j.api.do(patch), http.StatusOK)
	if second.header.Get("Idempotent-Replayed") != "true" || string(first.body) != string(second.body) {
		t.Fatalf("a repeated decision must replay: %v", second.header)
	}
	var after alertJSON
	first.json(t, &after)
	if after.Status != "dismissed" {
		t.Fatalf("status = %q", after.Status)
	}
}

// signedReport exports today's audit events and checks the signature the way an auditor outside Atlas would:
// with the public key from the API and the file as received. A byte changed must not verify.
func (j *journey) signedReport(t *testing.T) {
	today := time.Now().UTC().Format(time.DateOnly)
	report := expect(t, j.api.get("/audit/export?from="+today+"&to="+today+"&format=csv"), http.StatusOK)
	if !strings.HasPrefix(string(report.body), "id,at,actor_id") {
		t.Fatalf("the report starts with %q; want the CSV header", report.body[:min(40, len(report.body))])
	}
	var key struct {
		KeyID     string `json:"key_id"`
		PublicKey string `json:"public_key"`
		Algorithm string `json:"algorithm"`
	}
	expect(t, j.api.get("/system/signing-key"), http.StatusOK).json(t, &key)
	public, errPublic := base64.StdEncoding.DecodeString(key.PublicKey)
	signature, errSignature := base64.StdEncoding.DecodeString(report.header.Get("Atlas-Signature"))
	if errPublic != nil || errSignature != nil || report.header.Get("Atlas-Signature-Key-Id") != key.KeyID || key.Algorithm != signing.Algorithm {
		t.Fatalf("key %+v, headers %v", key, report.header)
	}
	digest := sha512.Sum512(report.body)
	if !signing.Verify(public, digest[:], signature) {
		t.Fatal("the report as received must verify under the public key of the installation")
	}
	report.body[len(report.body)-1] ^= 1
	changed := sha512.Sum512(report.body)
	if signing.Verify(public, changed[:], signature) {
		t.Fatal("a report with one byte changed must not verify")
	}
}
