//go:build integration

package httpapi_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base32"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/graph"
	"github.com/morse/atlas/server/internal/httpapi"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/idempotency"
	"github.com/morse/atlas/server/internal/platform/license"
	"github.com/morse/atlas/server/internal/platform/signing"
	"github.com/morse/atlas/server/internal/sources"
)

const (
	trailEmail    = "ana@acme.test"
	trailPassword = "a-long-enough-password"
	trailNewPass  = "another-long-password"
	sessionCookie = "atlas_session"
)

// trail is a real server over a real database, driven through HTTP by one person who is administrator,
// curator and auditor at once. Each step does one thing and the audit log must end with its event.
type trail struct {
	t       *testing.T
	handler http.Handler
	store   *db.DB
	log     *audit.Service
	tenant  uuid.UUID
	cookie  *http.Cookie
	secret  []byte
	signer  ed25519.PrivateKey
	folder  string // a folder sources may be registered from
	ids     map[string]string
}

func newTrail(t *testing.T) *trail {
	t.Helper()
	d := dbtest.Start(t)
	box, err := access.NewBox(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	pub, signer, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &trail{t: t, store: d, log: &audit.Service{DB: d}, signer: signer, folder: t.TempDir(), ids: map[string]string{}}
	tr.handler = withJobs(t, d, httpapi.NewHandler(tr.server(box, pub)))
	return tr
}

// server wires the real services over the trail's database.
func (tr *trail) server(box *access.Box, licenseKey ed25519.PublicKey) *httpapi.Server {
	d := tr.store
	admin := sources.NewAdmin(d, tr.t.TempDir(), []string{tr.folder})
	admin.ApplyAccess = documents.ApplyAccess
	library := documents.NewLibrary(d, blobs.New(tr.t.TempDir()))
	return &httpapi.Server{
		Accounts: access.NewService(d, box, createTenant), Audit: tr.log, Signing: &signing.Keys{DB: d, Sealer: box},
		Sources: admin, Documents: library, SearchLanguages: library, Taxonomy: &catalog.Service{DB: d},
		Graph: graph.NewService(d), Idempotency: idempotency.NewStore(d),
		Licenses: license.NewHolder(filepath.Join(tr.t.TempDir(), "license.json"), licenseKey),
	}
}

// withJobs lets handlers queue jobs, as the real server does (app.withJobs).
func withJobs(t *testing.T, d *db.DB, next http.Handler) http.Handler {
	t.Helper()
	client, err := jobs.NewClient(d.Pool, jobs.Setup{})
	if err != nil {
		t.Fatal(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(jobs.WithClient(r.Context(), client)))
	})
}

// send makes one request as the signed-in person and checks the status.
func (tr *trail) send(method, path string, body any, want int) *httptest.ResponseRecorder {
	tr.t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		tr.t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return tr.serve(req, want)
}

func (tr *trail) serve(req *http.Request, want int) *httptest.ResponseRecorder {
	tr.t.Helper()
	if tr.cookie != nil {
		req.AddCookie(tr.cookie)
	}
	rec := httptest.NewRecorder()
	tr.handler.ServeHTTP(rec, req)
	if rec.Code != want {
		tr.t.Fatalf("%s %s = %d, want %d: %s", req.Method, req.URL.Path, rec.Code, want, rec.Body)
	}
	return rec
}

// reply decodes the JSON answer of a request into a map.
func reply(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("answer %q is not JSON: %v", rec.Body, err)
	}
	return out
}

// lastAction is the action of the newest event of the chain.
func (tr *trail) lastAction() string {
	tr.t.Helper()
	events, _, err := tr.log.List(context.Background(), tr.tenant, audit.Page{Size: 1})
	if err != nil || len(events) != 1 {
		tr.t.Fatalf("audit log = %v, %v", events, err)
	}
	return events[0].Action
}

// step is one operation and the event it must leave.
type step struct {
	action string
	run    func(tr *trail)
}

func TestAuditEventsCoverEveryAdministrativeAction(t *testing.T) {
	tr := newTrail(t)
	steps := []step{
		{"setup.completed", (*trail).setUp},
		{"user.login_failed", (*trail).failToSignIn},
		{"user.login", (*trail).signIn},
		{"user.roles_changed", (*trail).takeEveryRole},
		{"user.created", (*trail).addBea},
		{"user.password_reset", (*trail).resetBeaPassword},
		{"user.created", (*trail).addCarlos},
		{"user.totp_reset", (*trail).resetCarlosSecondFactor},
		{"settings.updated", (*trail).changeRetention},
		{"settings.updated", (*trail).changeSearchLanguage},
		{"source.created", (*trail).addFolder},
		{"source.sync_requested", (*trail).syncFolder},
		{"source.paused", (*trail).pauseFolder},
		{"source.resumed", (*trail).resumeFolder},
		{"source.access_changed", (*trail).restrictFolder},
		{"quarantine.retried", (*trail).retryQuarantinedFile},
		{"source.removed", (*trail).removeFolder},
		{"upload.received", (*trail).uploadFile},
		{"metadata.corrected", (*trail).correctMetadata},
		{"document.deleted", (*trail).deleteUploadedDocument},
		{"license.rejected", (*trail).installBadLicense},
		{"license.installed", (*trail).installLicense},
		{"alert.updated", (*trail).acknowledgeAlert},
		{"edge.decided", (*trail).confirmLink},
		{"audit.exported", (*trail).exportAudit},
		{"user.password_changed", (*trail).changePassword},
		{"user.deleted", (*trail).deleteBea},
		{"user.logout", (*trail).signOut},
	}
	for i, s := range steps {
		s.run(tr)
		if got := tr.lastAction(); got != s.action {
			t.Fatalf("step %d: the last audit event is %q, want %q", i+1, got, s.action)
		}
	}
	tr.requireNoPersonalDataInTheLog()
}

func (tr *trail) setUp() {
	body := map[string]string{"org_name": "Acme", "name": "Ana", "email": trailEmail, "password": trailPassword}
	created := reply(tr.t, tr.send(http.MethodPost, "/api/v1/setup", body, http.StatusCreated))
	tr.secret = secretFromURI(tr.t, created["totp_uri"].(string))
	tenants, err := access.ListTenants(context.Background(), tr.store.Pool)
	if err != nil || len(tenants) != 1 {
		tr.t.Fatalf("tenants = %v, %v", tenants, err)
	}
	tr.tenant = tenants[0].ID
}

func (tr *trail) failToSignIn() {
	body := map[string]string{"email": trailEmail, "password": "not-the-password-at-all"}
	tr.send(http.MethodPost, "/api/v1/auth/login", body, http.StatusUnauthorized)
}

func (tr *trail) signIn() {
	body := map[string]string{"email": trailEmail, "password": trailPassword}
	challenge := reply(tr.t, tr.send(http.MethodPost, "/api/v1/auth/login", body, http.StatusOK))
	code := access.CodeAt(tr.secret, time.Now())
	verified := tr.send(http.MethodPost, "/api/v1/auth/verify", map[string]string{"challenge_id": challenge["challenge_id"].(string), "code": code}, http.StatusNoContent)
	for _, c := range verified.Result().Cookies() {
		if c.Name == sessionCookie {
			tr.cookie = c
		}
	}
	if tr.cookie == nil {
		tr.t.Fatal("sign-in set no session cookie")
	}
	tr.ids["me"] = reply(tr.t, tr.send(http.MethodGet, "/api/v1/me", nil, http.StatusOK))["id"].(string)
}

func (tr *trail) takeEveryRole() {
	roles := map[string][]string{"roles": {"admin", "curator", "auditor"}}
	tr.send(http.MethodPatch, "/api/v1/users/"+tr.ids["me"]+"/roles", roles, http.StatusOK)
}

// addPerson creates an account and remembers its id under name.
func (tr *trail) addPerson(name string, roles []string, secondFactor bool) {
	body := map[string]any{"name": name, "email": name + "@acme.test", "roles": roles, "second_factor": secondFactor}
	created := reply(tr.t, tr.send(http.MethodPost, "/api/v1/users", body, http.StatusCreated))
	tr.ids[name] = created["person"].(map[string]any)["id"].(string)
}

func (tr *trail) addBea()    { tr.addPerson("bea", []string{"viewer"}, false) }
func (tr *trail) addCarlos() { tr.addPerson("carlos", []string{"curator"}, true) }

func (tr *trail) resetBeaPassword() {
	tr.send(http.MethodPost, "/api/v1/users/"+tr.ids["bea"]+"/password-reset", nil, http.StatusOK)
}

func (tr *trail) resetCarlosSecondFactor() {
	tr.send(http.MethodPost, "/api/v1/users/"+tr.ids["carlos"]+"/totp-reset", nil, http.StatusOK)
}

func (tr *trail) changeRetention() {
	tr.send(http.MethodPatch, "/api/v1/settings", map[string]int{"retention_days": 365}, http.StatusOK)
}

func (tr *trail) changeSearchLanguage() {
	tr.send(http.MethodPatch, "/api/v1/settings", map[string]string{"search_language": "es"}, http.StatusOK)
}

func (tr *trail) addFolder() {
	body := map[string]string{"name": "Docs", "kind": "folder", "location": tr.folder}
	tr.ids["folder"] = reply(tr.t, tr.send(http.MethodPost, "/api/v1/sources", body, http.StatusCreated))["id"].(string)
}

func (tr *trail) syncFolder() {
	tr.send(http.MethodPost, "/api/v1/sources/"+tr.ids["folder"]+"/sync", nil, http.StatusAccepted)
}

func (tr *trail) patchFolder(patch map[string]any) {
	tr.send(http.MethodPatch, "/api/v1/sources/"+tr.ids["folder"], patch, http.StatusOK)
}

func (tr *trail) pauseFolder()  { tr.patchFolder(map[string]any{"paused": true}) }
func (tr *trail) resumeFolder() { tr.patchFolder(map[string]any{"paused": false}) }

func (tr *trail) restrictFolder() {
	tr.patchFolder(map[string]any{"access": "restricted", "readers": []string{tr.ids["me"]}})
}

func (tr *trail) retryQuarantinedFile() {
	var quarantined string
	tr.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO sources.quarantine (source_id, external_id, reason, detail)
			VALUES ($1, 'broken.pdf', 'corrupt', 'unreadable') RETURNING id`, tr.ids["folder"]).Scan(&quarantined)
	})
	tr.send(http.MethodPost, "/api/v1/ingestion/quarantine/"+quarantined+"/retry", nil, http.StatusAccepted)
}

func (tr *trail) removeFolder() {
	tr.send(http.MethodDelete, "/api/v1/sources/"+tr.ids["folder"], nil, http.StatusNoContent)
}

func (tr *trail) uploadFile() {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("files", "notes.pdf")
	if err != nil {
		tr.t.Fatal(err)
	}
	_, _ = part.Write([]byte("%PDF-1.4 a small file"))
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	tr.serve(req, http.StatusAccepted)
}

// storeUploadedDocument gives the uploaded file the document the pipeline would have made for it.
func (tr *trail) storeUploadedDocument() {
	var document string
	tr.inTenant(func(tx pgx.Tx) error {
		var source string
		if err := tx.QueryRow(context.Background(), `SELECT id FROM sources.sources WHERE kind = 'upload'`).Scan(&source); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `INSERT INTO documents.documents (source_id, external_id, title)
			VALUES ($1, 'notes.pdf', 'Notes') RETURNING id`, source).Scan(&document)
	})
	tr.ids["document"] = document
}

func (tr *trail) correctMetadata() {
	tr.storeUploadedDocument()
	tr.send(http.MethodPatch, "/api/v1/documents/"+tr.ids["document"]+"/metadata", map[string]string{"title": "Notes, corrected"}, http.StatusOK)
}

func (tr *trail) deleteUploadedDocument() {
	tr.send(http.MethodDelete, "/api/v1/documents/"+tr.ids["document"], nil, http.StatusNoContent)
}

func (tr *trail) installBadLicense() {
	tr.send(http.MethodPut, "/api/v1/system/license", map[string]string{"content": "this is not a license"}, http.StatusUnprocessableEntity)
}

func (tr *trail) installLicense() {
	grant := license.Grant{Organization: "Acme", ValidUntil: time.Now().AddDate(1, 0, 0), Features: license.Features()}
	signed, err := license.Sign(tr.signer, grant)
	if err != nil {
		tr.t.Fatal(err)
	}
	tr.send(http.MethodPut, "/api/v1/system/license", map[string]string{"content": string(signed)}, http.StatusOK)
}

func (tr *trail) acknowledgeAlert() {
	var alert string
	tr.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO graph.alerts (rule, severity, title, detail, document_ids, fingerprint)
			VALUES ('NO_OWNER', 'low', 'No owner', 'detail', '{}', 'fp') RETURNING id`).Scan(&alert)
	})
	tr.send(http.MethodPatch, "/api/v1/alerts/"+alert, map[string]string{"status": "ack"}, http.StatusOK)
}

func (tr *trail) confirmLink() {
	var edge string
	tr.inTenant(func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO graph.edges (src, dst, kind, layer, status, produced_by)
			VALUES ($1, $2, 'references', 1, 'suggested', 'test') RETURNING id`, uuid.New(), uuid.New()).Scan(&edge)
	})
	tr.send(http.MethodPatch, "/api/v1/graph/edges/"+edge, map[string]string{"status": "confirmed"}, http.StatusNoContent)
}

func (tr *trail) exportAudit() {
	today := time.Now().UTC().Format(time.DateOnly)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/export?from=2020-01-01&to="+today, nil)
	tr.serve(req, http.StatusOK)
}

func (tr *trail) changePassword() {
	body := map[string]string{"current_password": trailPassword, "new_password": trailNewPass}
	tr.send(http.MethodPut, "/api/v1/me/password", body, http.StatusNoContent)
}

func (tr *trail) deleteBea() {
	tr.send(http.MethodDelete, "/api/v1/users/"+tr.ids["bea"], nil, http.StatusNoContent)
}

func (tr *trail) signOut() {
	tr.send(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusNoContent)
}

// inTenant runs SQL as the application would, scoped to the tenant.
func (tr *trail) inTenant(run func(tx pgx.Tx) error) {
	tr.t.Helper()
	if err := tr.store.InTenantTx(context.Background(), tr.tenant, run); err != nil {
		tr.t.Fatal(err)
	}
}

// requireNoPersonalDataInTheLog: the log cannot be edited, so it holds ids and outcomes, never an e-mail,
// a password or a name.
func (tr *trail) requireNoPersonalDataInTheLog() {
	tr.t.Helper()
	events, _, err := tr.log.List(context.Background(), tr.tenant, audit.Page{Size: 200})
	if err != nil {
		tr.t.Fatal(err)
	}
	for _, e := range events {
		text := e.Subject + " " + e.Detail
		if strings.ContainsAny(text, "@") || strings.Contains(text, trailPassword) || strings.Contains(text, "otpauth") {
			tr.t.Errorf("event %d (%s) holds personal data: %q", e.ID, e.Action, text)
		}
	}
	if v, err := tr.log.Verify(context.Background(), tr.tenant); err != nil || !v.OK {
		tr.t.Fatalf("Verify = %+v, %v; the chain must be whole", v, err)
	}
}

// secretFromURI is the shared secret an authenticator app reads from the otpauth:// link.
func secretFromURI(t *testing.T, uri string) []byte {
	t.Helper()
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(parsed.Query().Get("secret"))
	if err != nil {
		t.Fatal(err)
	}
	return secret
}
