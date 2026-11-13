package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/graph"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/license"
)

type fixedLicense license.License

func (f fixedLicense) Current() license.License { return license.License(f) }

func (fixedLicense) Install([]byte) (license.License, error) {
	return license.License{}, errors.New("a fixed license cannot be replaced")
}

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
	req := adminRequest(http.MethodGet, "/api/v1/audit/export?from=2026-01-02&to=2026-01-01", "")
	req.Header.Set("Cookie", sessionCookie+"=auditor")
	if rec := serveTo(t, srv, req); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422 (past the gates, refused by the handler: the range runs backwards)", rec.Code)
	}
}

// fakeGraph answers with an empty graph and no alerts.
type fakeGraph struct{ GraphView }

func (fakeGraph) Alerts(context.Context, graph.Actor, graph.AlertPage) ([]graph.Alert, uuid.UUID, error) {
	return nil, uuid.Nil, nil
}

// replaceableLicense accepts the text "good" and refuses anything else the way the real holder does.
type replaceableLicense struct{ current license.License }

func (r *replaceableLicense) Current() license.License { return r.current }

func (r *replaceableLicense) Install(raw []byte) (license.License, error) {
	if string(raw) != "good" {
		return license.License{}, errs.WithPublicDetail(fmt.Errorf("%w: %w", &license.Rejected{Reason: license.ReasonSignature}, errs.ErrInvalid), "The signature does not match.")
	}
	r.current = license.License{Plan: "pro", Features: []string{"map"}}
	return r.current, nil
}

type recordingAudit struct {
	AuditLog
	entries []audit.Entry
}

func (r *recordingAudit) Append(_ context.Context, _ uuid.UUID, e audit.Entry) error {
	r.entries = append(r.entries, e)
	return nil
}

func install(t *testing.T, srv *Server, content string) *httptest.ResponseRecorder {
	t.Helper()
	return serveTo(t, srv, adminRequest(http.MethodPut, "/api/v1/system/license", `{"content":"`+content+`"}`))
}

func TestAValidLicenseIsInstalledAndAudited(t *testing.T) {
	log, source := &recordingAudit{}, &replaceableLicense{current: license.Core()}
	rec := install(t, &Server{Accounts: people, Licenses: source, Audit: log}, "good")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"plan":"pro"`) || !source.current.Has("map") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if len(log.entries) != 1 || log.entries[0].Action != "license.installed" {
		t.Fatalf("audit %+v; want license.installed", log.entries)
	}
}

func TestARefusedLicenseAnswers422WithTheReasonAndLeavesTheCurrentOne(t *testing.T) {
	log, source := &recordingAudit{}, &replaceableLicense{current: license.License{Plan: "pro", Features: []string{"alerts"}}}
	rec := install(t, &Server{Accounts: people, Licenses: source, Audit: log}, "forged")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"detail":"The signature does not match."`) || !source.current.Has("alerts") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if len(log.entries) != 1 || log.entries[0].Action != "license.rejected" || log.entries[0].Detail != "reason=signature" || strings.Contains(log.entries[0].Detail, "forged") {
		t.Fatalf("audit %+v; want license.rejected with the fixed reason and no file content", log.entries)
	}
}

func TestOnlyAnAdministratorInstallsALicense(t *testing.T) {
	req := adminRequest(http.MethodPut, "/api/v1/system/license", `{"content":"good"}`)
	req.Header.Set("Cookie", sessionCookie+"=viewer")
	if rec := serveTo(t, &Server{Accounts: people, Licenses: &replaceableLicense{}, Audit: &recordingAudit{}}, req); rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
}
