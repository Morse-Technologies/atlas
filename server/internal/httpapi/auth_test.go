package httpapi

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// fakeAccounts maps a cookie value to the principal it stands for. Unknown cookies are not signed in.
type fakeAccounts struct {
	Accounts
	sessions map[string]access.Principal
}

func (f fakeAccounts) Authenticate(_ context.Context, token string) (access.Principal, error) {
	if p, ok := f.sessions[token]; ok {
		return p, nil
	}
	return access.Principal{}, errs.ErrUnauthenticated
}

func (f fakeAccounts) Verify(context.Context, string, string) (access.SessionTicket, error) {
	return access.SessionTicket{Token: "tok", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (f fakeAccounts) GetUser(_ context.Context, _ access.Principal, id uuid.UUID) (access.User, error) {
	return access.User{ID: id, Name: "Ada", Roles: []access.Role{access.RoleViewer}}, nil
}

func (f fakeAccounts) OrgName(context.Context, access.Principal) (string, error) {
	return "Acme", nil
}

func (f fakeAccounts) ListPeople(context.Context, access.Principal, access.Page) ([]access.PersonName, uuid.UUID, error) {
	return []access.PersonName{{ID: uuid.New(), Name: "Ada"}}, uuid.Nil, nil
}

var people = fakeAccounts{sessions: map[string]access.Principal{
	"newcomer": {UserID: uuid.New(), Roles: []access.Role{access.RoleAdmin}, MustChangePassword: true},
	"viewer":   {UserID: uuid.New(), Roles: []access.Role{access.RoleViewer}},
	"admin":    {UserID: uuid.New(), Roles: []access.Role{access.RoleAdmin}},
	"auditor":  {UserID: uuid.New(), Roles: []access.Role{access.RoleAuditor}},
}}

func call(t *testing.T, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	NewHandler(&Server{Accounts: people, Licenses: allFeatures}).ServeHTTP(rec, req)
	return rec
}

type accessRule struct {
	name, path, cookie string
	want               int
}

// A backwards range is refused by the handler itself (422), so 422 means the request passed authentication and the role check.
var accessRules = []accessRule{
	{"no cookie", "/api/v1/sources", "", http.StatusUnauthorized},
	{"forged cookie", "/api/v1/sources", "forged", http.StatusUnauthorized},
	{"viewer cannot manage sources", "/api/v1/sources", "viewer", http.StatusForbidden},
	{"auditor reaches the export handler", "/api/v1/audit/export?from=2026-01-02&to=2026-01-01", "auditor", http.StatusUnprocessableEntity},
	{"admin cannot read the audit log", "/api/v1/audit/export?from=2026-01-01&to=2026-01-02", "admin", http.StatusForbidden},
	{"viewer cannot export the audit log", "/api/v1/audit/export?from=2026-01-01&to=2026-01-02", "viewer", http.StatusForbidden},
	{"viewer lists people", "/api/v1/people", "viewer", http.StatusOK},
	{"auditor lists people", "/api/v1/people", "auditor", http.StatusOK},
	{"admin lists people", "/api/v1/people", "admin", http.StatusOK},
	{"no cookie cannot list people", "/api/v1/people", "", http.StatusUnauthorized},
	{"viewer cannot list accounts", "/api/v1/users", "viewer", http.StatusForbidden},
	{"health is public", "/healthz", "", http.StatusOK},
}

func TestAccessRules(t *testing.T) {
	for _, c := range accessRules {
		if got := call(t, c.path, c.cookie).Code; got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

func TestMeCarriesTheOrganizationName(t *testing.T) {
	for _, cookie := range []string{"viewer", "auditor", "admin"} {
		rec := call(t, "/api/v1/me", cookie)
		var me map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: /me = %d, %v", cookie, rec.Code, err)
		}
		if me["org_name"] != "Acme" || me["name"] != "Ada" || me["roles"] == nil {
			t.Errorf("%s: /me = %v, want org_name Acme and the fields of a person", cookie, me)
		}
	}
}

func TestPeopleListCarriesOnlyIdAndName(t *testing.T) {
	var items []map[string]any
	if err := json.Unmarshal(call(t, "/api/v1/people", "viewer").Body.Bytes(), &items); err != nil || len(items) != 1 {
		t.Fatalf("decoding the people list: %v, %d items", err, len(items))
	}
	keys := slices.Sorted(maps.Keys(items[0]))
	if !slices.Equal(keys, []string{"id", "name"}) {
		t.Fatalf("keys of a listed person = %v, want only id and name", keys)
	}
}

func TestWriteFromAnotherOriginIsRefused(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "admin"})
	rec := httptest.NewRecorder()
	NewHandler(&Server{Accounts: people}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin write: status %d, want 403", rec.Code)
	}
}

func TestVerifyLoginSetsAnHttpOnlySecureCookie(t *testing.T) {
	body := strings.NewReader(`{"challenge_id":"c","code":"123456"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewHandler(&Server{Accounts: people}).ServeHTTP(rec, req)
	cookie := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"atlas_session=tok", "HttpOnly", "Secure", "SameSite=Lax"} {
		if rec.Code != http.StatusNoContent || !strings.Contains(cookie, want) {
			t.Fatalf("status %d, Set-Cookie %q lacks %q", rec.Code, cookie, want)
		}
	}
}

func TestEveryOperationDeclaresItsAccess(t *testing.T) {
	if _, err := requirementsFromSpec(); err != nil {
		t.Fatal(err)
	}
}

type fakeAudit struct{ AuditLog }

func (fakeAudit) List(context.Context, uuid.UUID, audit.Page) ([]audit.Event, int64, error) {
	return []audit.Event{{ID: 7, At: time.Now(), Action: "user.login"}}, 6, nil
}

func TestAuditListReturnsEventsAndTheNextCursor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?limit=1", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "auditor"})
	rec := httptest.NewRecorder()
	NewHandler(&Server{Accounts: people, Audit: fakeAudit{}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Next-Cursor") != "6" || !strings.Contains(rec.Body.String(), `"user.login"`) {
		t.Fatalf("status %d, Next-Cursor %q, body %s", rec.Code, rec.Header().Get("Next-Cursor"), rec.Body)
	}
}

func TestAuditCursorMustBeAPositiveNumber(t *testing.T) {
	for _, bad := range []string{"abc", "0", "-3"} {
		cursor := bad
		if _, err := auditPageFrom(nil, &cursor); err == nil {
			t.Errorf("cursor %q must be refused", bad)
		}
	}
}

// allFeatures is a license with every Pro feature, for tests that are not about the license.
var allFeatures = fixedLicense{Plan: "pro", Features: []string{"alerts", "map", "audit_reports", "sso", "inherited_acl", "sharepoint", "erp"}}

func TestARejectedSessionCookieIsClearedSoTheBrowserCannotLoop(t *testing.T) {
	rec := call(t, "/api/v1/me", "forged")
	cleared := rec.Header().Get("Set-Cookie")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(cleared, "atlas_session=") || !strings.Contains(cleared, "Max-Age=0") {
		t.Fatalf("status %d, Set-Cookie %q; the 401 must also end the stale cookie", rec.Code, cleared)
	}
}

func TestAnonymousRequestsGetNoCookieHeader(t *testing.T) {
	rec := call(t, "/api/v1/me", "")
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Set-Cookie") != "" {
		t.Fatalf("status %d, Set-Cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}

func TestPendingPasswordChangeBlocksOtherOperations(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{"/api/v1/me", http.StatusOK},
		{"/api/v1/system/license", http.StatusOK},
		{"/api/v1/users", http.StatusForbidden},
		{"/api/v1/sources", http.StatusForbidden},
		{"/api/v1/documents", http.StatusForbidden},
	}
	for _, c := range cases {
		if got := call(t, c.path, "newcomer").Code; got != c.want {
			t.Errorf("%s: status %d, want %d", c.path, got, c.want)
		}
	}
}
