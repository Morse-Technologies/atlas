//go:build e2e

// Package e2e walks through Atlas the way a person does, against a running stack: the HTTPS address of Caddy
// in ATLAS_E2E_URL, with the fake model servers (deploy/compose, profile "fake"). It needs an empty installation:
// `docker compose down -v` first. Run: ATLAS_E2E_URL=https://localhost go test -tags e2e ./e2e -count=1
package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/access"
)

const (
	adminEmail    = "ana@acme.test"
	adminPassword = "a-long-password-1"
	sourcePath    = "/sources/docs"
	goldenPDF     = "../testdata/golden/PNT-014-rev3.pdf"
)

// ingestTimeout is how long reading the sample folder may take; real models on a CPU need much longer.
var ingestTimeout = 3 * time.Minute

type docJSON struct {
	ID               string `json:"id"`
	Path             string `json:"path"`
	Title            string `json:"title"`
	CurrentVersionID string `json:"current_version_id"`
	Versions         []struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Pages int    `json:"pages"`
	} `json:"versions"`
	Provenance []struct {
		Field  string `json:"field"`
		Source string `json:"source"`
	} `json:"provenance"`
}

// journey holds what one step learns for the next.
type journey struct {
	api      *api
	secret   []byte
	userID   string
	sourceID string
	golden   docJSON
}

func TestAtlasFromFirstSetupToAnAnswerWithItsCitation(t *testing.T) {
	j := &journey{api: newAPI(t)}
	steps := []struct {
		name string
		run  func(*testing.T)
	}{
		{"setup creates the organization and the administrator once", j.setup},
		{"signing in takes a password and then a code", j.signIn},
		{"a request from another origin is refused", j.crossOrigin},
		{"a folder is checked and registered, and its documents are read", j.addSource},
		{"a file that cannot be read goes to quarantine", j.quarantine},
		{"the original PDF is served in ranges", j.serveFile},
		{"a question gets a streamed answer with a citation", j.askWithAnswer},
		{"a question outside the documents is not found", j.askNotFound},
		{"the conversation is kept and can be rated", j.conversation},
		{"a correction survives a rescan", j.correctionSurvivesRescan},
		{"repeating a write with the same key runs it once", j.idempotency},
		{"an uploaded file joins the index", j.uploadFile},
		{"a source can be restricted to some people and opened again", j.restrictSource},
		{"Pro features follow the license", j.proFollowsLicense},
		{"the audit log records it all and its chain is intact", j.auditTrail},
		{"the panel counts what happened", j.panel},
		{"changing the password needs the current one and keeps this session", j.changePassword},
		{"signing out ends the session", j.signOut},
		{"a session cookie the server rejects is cleared", j.rejectedCookie},
	}
	for _, s := range steps {
		if !t.Run(s.name, s.run) {
			t.Fatalf("stopping: %q failed and the next steps depend on it", s.name)
		}
	}
}

func (j *journey) setup(t *testing.T) {
	var status struct {
		SetupDone bool `json:"setup_done"`
	}
	j.api.get("/setup").json(t, &status)
	if status.SetupDone {
		t.Fatal("this installation is already set up; the e2e needs an empty one (docker compose down -v)")
	}
	var created struct {
		TOTPURI string `json:"totp_uri"`
	}
	body := map[string]string{"org_name": "Acme", "name": "Ana", "email": adminEmail, "password": adminPassword}
	expect(t, j.api.post("/setup", body), http.StatusCreated).json(t, &created)
	j.secret = secretOf(t, created.TOTPURI)
	expect(t, j.api.post("/setup", body), http.StatusConflict)
	j.api.get("/setup").json(t, &status)
	if !status.SetupDone {
		t.Fatal("setup must be closed once the administrator exists")
	}
}

func (j *journey) signIn(t *testing.T) {
	expect(t, j.api.get("/me"), http.StatusUnauthorized)
	expect(t, j.api.post("/auth/login", map[string]string{"email": adminEmail, "password": "wrong-password-xx"}), http.StatusUnauthorized)
	var challenge struct {
		ChallengeID string `json:"challenge_id"`
		NeedsCode   bool   `json:"needs_code"`
	}
	expect(t, j.api.post("/auth/login", map[string]string{"email": adminEmail, "password": adminPassword}), http.StatusOK).json(t, &challenge)
	if !challenge.NeedsCode {
		t.Fatal("an administrator always signs in with a second factor")
	}
	code := access.CodeAt(j.secret, time.Now())
	verified := expect(t, j.api.post("/auth/verify", map[string]string{"challenge_id": challenge.ChallengeID, "code": code}), http.StatusNoContent)
	cookie := verified.header.Get("Set-Cookie")
	for _, want := range []string{"atlas_session=", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(cookie, want) {
			t.Fatalf("session cookie %q lacks %s", cookie, want)
		}
	}
	var me struct {
		ID    string   `json:"id"`
		Roles []string `json:"roles"`
	}
	expect(t, j.api.get("/me"), http.StatusOK).json(t, &me)
	j.userID = me.ID
	if len(me.Roles) != 1 || me.Roles[0] != "admin" {
		t.Fatalf("roles = %v", me.Roles)
	}
}

func (j *journey) crossOrigin(t *testing.T) {
	r := j.api.do(request{method: http.MethodPost, path: "/auth/logout", headers: map[string]string{"Origin": "https://evil.example"}})
	expect(t, r, http.StatusForbidden)
	expect(t, j.api.get("/me"), http.StatusOK) // and the session is still alive
}

func (j *journey) addSource(t *testing.T) {
	var check struct {
		OK bool `json:"ok"`
	}
	expect(t, j.api.post("/sources/check", map[string]string{"path": sourcePath}), http.StatusOK).json(t, &check)
	if !check.OK {
		t.Fatalf("%s should be readable", sourcePath)
	}
	expect(t, j.api.post("/sources/check", map[string]string{"path": "/etc"}), http.StatusOK).json(t, &check)
	if check.OK {
		t.Fatal("a folder outside the allowed roots must not pass")
	}
	var src struct {
		ID string `json:"id"`
	}
	expect(t, j.api.post("/sources", map[string]string{"name": "Docs", "kind": "folder", "location": sourcePath}), http.StatusCreated).json(t, &src)
	j.sourceID = src.ID
	eventually(t, ingestTimeout, "both PDFs to be indexed", func() bool { return len(j.indexed()) >= 2 })
	for _, d := range j.indexed() {
		if strings.HasSuffix(d.Path, "PNT-014-rev3.pdf") {
			j.golden = d
		}
	}
	if j.golden.ID == "" || j.golden.Versions[0].Pages != 9 {
		t.Fatalf("the golden document was not read as 9 pages: %s", describe(j.golden))
	}
}

// indexed lists the documents whose current version is searchable.
func (j *journey) indexed() []docJSON {
	var docs []docJSON
	j.api.get("/documents?limit=200").json(j.api.t, &docs)
	var out []docJSON
	for _, d := range docs {
		for _, v := range d.Versions {
			if v.ID == d.CurrentVersionID && v.State == "indexed" {
				out = append(out, d)
			}
		}
	}
	return out
}

func (j *journey) quarantine(t *testing.T) {
	var items []struct {
		Path, Reason string
	}
	eventually(t, ingestTimeout, "notes.bin to be quarantined", func() bool {
		j.api.get("/ingestion/quarantine").json(t, &items)
		return len(items) > 0
	})
	if !strings.HasSuffix(items[0].Path, "notes.bin") || items[0].Reason != "unsupported" {
		t.Fatalf("quarantine = %+v", items)
	}
}

func (j *journey) serveFile(t *testing.T) {
	path := "/documents/" + j.golden.ID + "/versions/" + j.golden.CurrentVersionID + "/file"
	r := j.api.do(request{method: http.MethodGet, path: path, headers: map[string]string{"Range": "bytes=0-99"}})
	if r.status != http.StatusPartialContent || len(r.body) != 100 || !strings.HasPrefix(string(r.body), "%PDF") {
		t.Fatalf("status %d, %d bytes, starts %q", r.status, len(r.body), r.body[:min(8, len(r.body))])
	}
	if r.header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(r.header.Get("Content-Range"), "bytes 0-99/") {
		t.Fatalf("headers %v", r.header)
	}
}

func (j *journey) askWithAnswer(t *testing.T) {
	events, answer := j.api.ask("¿Cómo se hace el montaje del husillo?")
	if len(events) < 3 || events[0].Type != "meta" || events[len(events)-1].Type != "done" {
		t.Fatalf("events = %+v", events)
	}
	streamed := ""
	for _, e := range events {
		streamed += e.T
	}
	if answer.Outcome != "answered" || streamed != answer.Text || !strings.Contains(answer.Text, "[1]") {
		t.Fatalf("outcome %q, text %q, streamed %q", answer.Outcome, answer.Text, streamed)
	}
	c := answer.Citations
	if len(c) == 0 || c[0].DocumentID == "" || c[0].Page < 1 || c[0].Bbox == nil || c[0].Bbox.W <= 0 {
		t.Fatalf("the citation must name a document, a page and a box: %+v", c)
	}
}

func (j *journey) askNotFound(t *testing.T) {
	_, answer := j.api.ask("qwertyuiop zxcvbnmk lkjhgfdsa")
	if answer.Outcome != "not_found" || len(answer.Citations) != 0 || len(answer.Nearby) == 0 {
		t.Fatalf("answer = %+v; nothing matches, so there are no citations and a few nearby documents", answer)
	}
}

func (j *journey) conversation(t *testing.T) {
	var list []struct {
		ID, Title, LastOutcome string
	}
	expect(t, j.api.get("/conversations"), http.StatusOK).json(t, &list)
	if len(list) != 2 {
		t.Fatalf("conversations = %+v, want the two questions", list)
	}
	var detail struct {
		Answers []answerJSON `json:"answers"`
	}
	expect(t, j.api.get("/conversations/"+list[len(list)-1].ID), http.StatusOK).json(t, &detail)
	if len(detail.Answers) != 1 || len(detail.Answers[0].Citations) == 0 {
		t.Fatalf("detail = %+v", detail)
	}
	expect(t, j.api.post("/answers/"+detail.Answers[0].ID+"/feedback", map[string]string{"value": "up"}), http.StatusNoContent)
}

func (j *journey) correctionSurvivesRescan(t *testing.T) {
	var fixed docJSON
	expect(t, j.api.patch("/documents/"+j.golden.ID+"/metadata", map[string]string{"title": "Montaje de husillo (corregido)"}), http.StatusOK).json(t, &fixed)
	if fixed.Title != "Montaje de husillo (corregido)" || len(fixed.Provenance) != 1 || fixed.Provenance[0].Source != "manual" {
		t.Fatalf("document = %+v", fixed)
	}
	var before []struct{ ID string }
	j.api.get("/ingestion/runs").json(t, &before)
	expect(t, j.api.post("/sources/"+j.sourceID+"/sync", nil), http.StatusAccepted)
	eventually(t, time.Minute, "the rescan to finish", func() bool {
		var after []struct{ ID string }
		j.api.get("/ingestion/runs").json(t, &after)
		return len(after) > len(before)
	})
	var again docJSON
	j.api.get("/documents/"+j.golden.ID).json(t, &again)
	if again.Title != "Montaje de husillo (corregido)" {
		t.Fatalf("title after the rescan = %q", again.Title)
	}
}

func (j *journey) idempotency(t *testing.T) {
	send := func() reply {
		return j.api.do(request{method: http.MethodPatch, path: "/settings", body: map[string]any{"retention_days": 365}, headers: map[string]string{"Idempotency-Key": "e2e-key-1"}})
	}
	first, second := expect(t, send(), http.StatusOK), expect(t, send(), http.StatusOK)
	if first.header.Get("Idempotent-Replayed") != "" || second.header.Get("Idempotent-Replayed") != "true" || string(first.body) != string(second.body) {
		t.Fatalf("first %v, second %v", first.header, second.header)
	}
	other := j.api.do(request{method: http.MethodPatch, path: "/settings", body: map[string]any{"retention_days": 400}, headers: map[string]string{"Idempotency-Key": "e2e-key-1"}})
	expect(t, other, http.StatusUnprocessableEntity) // the same key for a different request
}

func (j *journey) uploadFile(t *testing.T) {
	pdf, err := os.ReadFile(goldenPDF)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Accepted int `json:"accepted"`
	}
	expect(t, j.api.upload("subido.pdf", pdf), http.StatusAccepted).json(t, &result)
	if result.Accepted != 1 {
		t.Fatalf("accepted = %d", result.Accepted)
	}
	eventually(t, ingestTimeout, "the uploaded file to be indexed", func() bool { return len(j.indexed()) >= 3 })
	bad := j.api.upload(".hidden.pdf", pdf)
	var refused struct {
		Accepted int `json:"accepted"`
		Rejected []struct{ Name, Reason string }
	}
	expect(t, bad, http.StatusAccepted).json(t, &refused)
	if refused.Accepted != 0 || len(refused.Rejected) != 1 {
		t.Fatalf("a hidden file must be refused with a reason: %+v", refused)
	}
}

func (j *journey) proIsLocked(t *testing.T) {
	for _, path := range []string{"/alerts", "/graph/neighborhood"} {
		expect(t, j.api.get(path), http.StatusForbidden)
	}
	var lic struct {
		Plan     string   `json:"plan"`
		Features []string `json:"features"`
	}
	expect(t, j.api.get("/system/license"), http.StatusOK).json(t, &lic)
	if lic.Plan != "core" || len(lic.Features) != 0 {
		t.Fatalf("license = %+v", lic)
	}
}

func (j *journey) auditTrail(t *testing.T) {
	expect(t, j.api.get("/audit/events"), http.StatusForbidden) // an administrator is not an auditor
	roles := map[string]any{"roles": []string{"admin", "auditor"}}
	expect(t, j.api.patch("/users/"+j.userID+"/roles", roles), http.StatusOK)
	var events []struct{ Action string }
	expect(t, j.api.get("/audit/events?limit=200"), http.StatusOK).json(t, &events)
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.Action] = true
	}
	for _, want := range []string{"setup.completed", "user.login", "source.created", "answer.generated", "answer.feedback", "metadata.corrected", "settings.updated", "upload.received", "user.roles_changed"} {
		if !seen[want] {
			t.Errorf("the audit log has no %q event (has %v)", want, seen)
		}
	}
	var v struct {
		OK      bool `json:"ok"`
		Checked int  `json:"checked"`
	}
	expect(t, j.api.post("/audit/verify", nil), http.StatusOK).json(t, &v)
	if !v.OK || v.Checked < len(events) {
		t.Fatalf("verification = %+v for %d events", v, len(events))
	}
}

func (j *journey) panel(t *testing.T) {
	var o struct {
		Today     struct{ Questions int } `json:"today"`
		Documents struct {
			Indexed     int `json:"indexed"`
			Quarantined int `json:"quarantined"`
		} `json:"documents"`
		Week []struct{ Day string } `json:"week"`
	}
	expect(t, j.api.get("/stats/overview"), http.StatusOK).json(t, &o)
	if o.Today.Questions != 2 || o.Documents.Indexed < 3 || o.Documents.Quarantined < 1 || len(o.Week) != 14 {
		t.Fatalf("overview = %+v", o)
	}
}

func (j *journey) signOut(t *testing.T) {
	expect(t, j.api.post("/auth/logout", nil), http.StatusNoContent)
	expect(t, j.api.get("/me"), http.StatusUnauthorized)
}

// An HttpOnly cookie cannot be removed by the page, so the 401 itself must clear it. Otherwise the browser
// bounces between the login page (signed-in people go home) and the screens (the API says 401).
func (j *journey) rejectedCookie(t *testing.T) {
	r := j.api.do(request{method: http.MethodGet, path: "/me", headers: map[string]string{"Cookie": "atlas_session=forged"}})
	expect(t, r, http.StatusUnauthorized)
	if cleared := r.header.Get("Set-Cookie"); !strings.Contains(cleared, "atlas_session=") || !strings.Contains(cleared, "Max-Age=0") {
		t.Fatalf("Set-Cookie = %q", cleared)
	}
}

func (j *journey) changePassword(t *testing.T) {
	const newPassword = "a-newer-long-password-2"
	put := func(current, next string) reply {
		return j.api.do(request{method: http.MethodPut, path: "/me/password", body: map[string]string{"current_password": current, "new_password": next}})
	}
	expect(t, put("wrong-password-xx", newPassword), http.StatusUnprocessableEntity)
	expect(t, put(adminPassword, "short"), http.StatusUnprocessableEntity)
	expect(t, put(adminPassword, newPassword), http.StatusNoContent)
	expect(t, j.api.get("/me"), http.StatusOK) // this session stays
	other := newAPI(t)                         // a 401 clears the cookie of whoever receives it, so sign-in is tried from another client
	expect(t, other.post("/auth/login", map[string]string{"email": adminEmail, "password": adminPassword}), http.StatusUnauthorized)
	expect(t, other.post("/auth/login", map[string]string{"email": adminEmail, "password": newPassword}), http.StatusOK)
	expect(t, put(newPassword, adminPassword), http.StatusNoContent) // leave it as it was
}

func (j *journey) restrictSource(t *testing.T) {
	var src struct {
		Access  string   `json:"access"`
		Readers []string `json:"readers"`
	}
	patch := func(body map[string]any) reply { return j.api.patch("/sources/"+j.sourceID, body) }
	expect(t, patch(map[string]any{"access": "restricted"}), http.StatusUnprocessableEntity)                        // who may read it?
	expect(t, patch(map[string]any{"access": "restricted", "readers": []string{}}), http.StatusUnprocessableEntity) // nobody is not a choice
	expect(t, patch(map[string]any{"access": "restricted", "readers": []string{j.userID}}), http.StatusOK).json(t, &src)
	if src.Access != "restricted" || len(src.Readers) != 1 || src.Readers[0] != j.userID {
		t.Fatalf("source = %+v", src)
	}
	if n := len(j.indexed()); n < 3 {
		t.Fatalf("the reader still sees %d documents, want all of them", n)
	}
	expect(t, patch(map[string]any{"access": "everyone"}), http.StatusOK).json(t, &src)
	if src.Access != "everyone" || len(src.Readers) != 0 {
		t.Fatalf("source = %+v", src)
	}
}
