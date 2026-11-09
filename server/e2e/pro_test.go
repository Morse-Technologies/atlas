//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"slices"
	"testing"
	"time"
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

func (j *journey) proWorks(t *testing.T) {
	var lic struct {
		Plan     string   `json:"plan"`
		Features []string `json:"features"`
	}
	expect(t, j.api.get("/system/license"), http.StatusOK).json(t, &lic)
	if lic.Plan != "pro" || !slices.Contains(lic.Features, "alerts") || !slices.Contains(lic.Features, "map") {
		t.Fatalf("license = %+v", lic)
	}
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
