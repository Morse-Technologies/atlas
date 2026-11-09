package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
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

var people = fakeAccounts{sessions: map[string]access.Principal{
	"viewer":  {UserID: uuid.New(), Roles: []access.Role{access.RoleViewer}},
	"admin":   {UserID: uuid.New(), Roles: []access.Role{access.RoleAdmin}},
	"auditor": {UserID: uuid.New(), Roles: []access.Role{access.RoleAuditor}},
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

func TestAccessRules(t *testing.T) {
	// /audit/export is still a 501 stub (the signed export needs the signing key): 501 means the request passed authentication and the role check.
	cases := []struct {
		name, path, cookie string
		want               int
	}{
		{"no cookie", "/api/v1/sources", "", http.StatusUnauthorized},
		{"forged cookie", "/api/v1/sources", "forged", http.StatusUnauthorized},
		{"viewer cannot manage sources", "/api/v1/sources", "viewer", http.StatusForbidden},
		{"auditor reaches a handler not built yet", "/api/v1/audit/export?from=2026-01-01&to=2026-01-02", "auditor", http.StatusNotImplemented},
		{"admin cannot read the audit log", "/api/v1/audit/export?from=2026-01-01&to=2026-01-02", "admin", http.StatusForbidden},
		{"viewer cannot export the audit log", "/api/v1/audit/export?from=2026-01-01&to=2026-01-02", "viewer", http.StatusForbidden},
		{"health is public", "/healthz", "", http.StatusOK},
	}
	for _, c := range cases {
		if got := call(t, c.path, c.cookie).Code; got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
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
