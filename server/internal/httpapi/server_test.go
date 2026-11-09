package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
)

var problemCases = map[error]int{
	ErrNotImplemented:                          http.StatusNotImplemented,
	fmt.Errorf("source: %w", errs.ErrNotFound): http.StatusNotFound,
	errs.ErrInvalid:                            http.StatusUnprocessableEntity,
	errs.ErrUnauthenticated:                    http.StatusUnauthorized,
	errs.NewTransient(errors.New("db down")):   http.StatusServiceUnavailable,
	errors.New("boom"):                         http.StatusInternalServerError,
}

func TestProblemForMapsErrorClasses(t *testing.T) {
	for err, want := range problemCases {
		if got := ProblemFor(err); got.Status != want || got.Title != http.StatusText(want) {
			t.Errorf("ProblemFor(%v) = %+v, want status %d", err, got, want)
		}
	}
}

func serve(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	NewHandler(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestReadyzReportsAFailingDependency(t *testing.T) {
	down := func(context.Context) error { return errors.New("connection refused") }
	rec := serve(t, &Server{Checks: []Check{{Name: "docling", Probe: down}}}, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestUnbuiltOperationsAnswer501ProblemDetails(t *testing.T) {
	rec := call(t, "/api/v1/audit/export?from=2026-01-01&to=2026-01-02", "auditor")
	if rec.Code != http.StatusNotImplemented || rec.Header().Get("Content-Type") != problemContentType {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestRequestOutsideTheContractIs400WithDetail(t *testing.T) {
	rec := call(t, "/api/v1/documents?source_id=not-a-uuid", "viewer")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "source_id") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
}
