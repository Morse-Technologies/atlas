//go:build integration

package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/httpapi"
	"github.com/morse/atlas/server/internal/platform/dbtest"
)

const rateLimitedLogins = 10

func createTenant(ctx context.Context, tx pgx.Tx, org string) (uuid.UUID, error) {
	return access.CreateTenant(ctx, tx, org)
}

func TestRateLimitAnAddressPastTheLoginLimitIsRefusedAndLeavesNoAuditEvent(t *testing.T) {
	ctx := context.Background()
	d := dbtest.Start(t)
	box, err := access.NewBox(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	accounts := access.NewService(d, box, createTenant)
	if _, err := accounts.Setup(ctx, access.SetupInput{OrgName: "Acme", Name: "Ada", Email: "ada@acme.test", Password: "a-long-enough-password"}); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(&httpapi.Server{Accounts: accounts})

	codes := make([]int, 0, rateLimitedLogins+1)
	var last *httptest.ResponseRecorder
	for range rateLimitedLogins + 1 {
		last = wrongLogin(handler)
		codes = append(codes, last.Code)
	}

	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") == "" {
		t.Fatalf("status codes %v, Retry-After %q; want the 11th to be 429 with Retry-After", codes, last.Header().Get("Retry-After"))
	}
	if !strings.Contains(last.Body.String(), "Too many attempts. Try again in") {
		t.Fatalf("body %s lacks the public detail", last.Body)
	}
	if failures := loginFailures(ctx, t, accounts); failures != rateLimitedLogins {
		t.Fatalf("%d user.login_failed events, want %d: the refused attempt must not reach access", failures, rateLimitedLogins)
	}
}

func wrongLogin(handler http.Handler) *httptest.ResponseRecorder {
	body := `{"email":"ada@acme.test","password":"not-the-password"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	return rec
}

func loginFailures(ctx context.Context, t *testing.T, accounts *access.Service) int {
	t.Helper()
	tenants, err := access.ListTenants(ctx, accounts.DB.Pool)
	if err != nil || len(tenants) != 1 {
		t.Fatalf("tenants = %v, %v", tenants, err)
	}
	events, _, err := (&audit.Service{DB: accounts.DB}).List(ctx, tenants[0].ID, audit.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, e := range events {
		if e.Action == "user.login_failed" {
			failures++
		}
	}
	return failures
}
