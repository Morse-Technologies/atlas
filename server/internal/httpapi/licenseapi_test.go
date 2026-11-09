package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/graph"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/license"
)

type fixedLicense license.License

func (f fixedLicense) Current() license.License { return license.License(f) }

func TestAProFeatureIsRefusedWithoutItsLicense(t *testing.T) {
	// /alerts needs the "alerts" feature and the curator role; the administrator passes the role check.
	core := &Server{Accounts: people}
	if rec := serveTo(t, core, adminRequest(http.MethodGet, "/api/v1/alerts", "")); rec.Code != http.StatusForbidden {
		t.Fatalf("Core: status %d, want 403", rec.Code)
	}
	withMap := &Server{Accounts: people, Licenses: fixedLicense{Plan: "pro", Features: []string{"map"}}}
	if rec := serveTo(t, withMap, adminRequest(http.MethodGet, "/api/v1/alerts", "")); rec.Code != http.StatusForbidden {
		t.Fatalf("a license for another feature: status %d, want 403", rec.Code)
	}
}

func TestTheLicensedFeatureReachesTheHandler(t *testing.T) {
	pro := &Server{Accounts: people, Graph: fakeGraph{}, Licenses: fixedLicense{Plan: "pro", Features: []string{"alerts"}}}
	if rec := serveTo(t, pro, adminRequest(http.MethodGet, "/api/v1/alerts", "")); rec.Code != http.StatusOK {
		t.Fatalf("the licensed feature must pass the gate, status %d", rec.Code)
	}
}

func TestTheLicenseEndpointReportsCoreWhenThereIsNoSource(t *testing.T) {
	rec := serveTo(t, &Server{Accounts: people}, adminRequest(http.MethodGet, "/api/v1/system/license", ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"plan":"core"`) || !strings.Contains(rec.Body.String(), `"features":[]`) {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestTheLicenseEndpointListsTheFeaturesInForce(t *testing.T) {
	lic := fixedLicense{Plan: "pro", Organization: "Acme", ValidUntil: time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC), Features: []string{"alerts", "map"}}
	rec := serveTo(t, &Server{Accounts: people, Licenses: lic}, adminRequest(http.MethodGet, "/api/v1/system/license", ""))
	for _, want := range []string{`"plan":"pro"`, `"organization":"Acme"`, `"alerts"`, `"map"`, `"valid_until":"2027-01-31"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %s: %s", want, rec.Body)
		}
	}
}

func TestAnAuditorWithTheLicenseReachesTheExport(t *testing.T) {
	srv := &Server{Accounts: people, Licenses: fixedLicense{Plan: "pro", Features: []string{"audit_reports"}}}
	req := adminRequest(http.MethodGet, "/api/v1/audit/export?from=2026-01-01&to=2026-01-02", "")
	req.Header.Set("Cookie", sessionCookie+"=auditor")
	if rec := serveTo(t, srv, req); rec.Code != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501 (past the gates, handler not built)", rec.Code)
	}
}

// fakeGraph answers with an empty graph and no alerts.
type fakeGraph struct{ GraphView }

func (fakeGraph) Alerts(context.Context, graph.Actor, graph.AlertPage) ([]graph.Alert, uuid.UUID, error) {
	return nil, uuid.Nil, nil
}
