package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/platform/errs"
)

var problemCases = map[error]int{
	fmt.Errorf("source: %w", errs.ErrNotFound): http.StatusNotFound,
	errs.ErrInvalid:                          http.StatusUnprocessableEntity,
	errs.ErrUnauthenticated:                  http.StatusUnauthorized,
	errs.NewTransient(errors.New("db down")): http.StatusServiceUnavailable,
	errors.New("boom"):                       http.StatusInternalServerError,
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

func TestReadyzNamesTheAnswerModeWithoutCallingItNotReady(t *testing.T) {
	rec := serve(t, &Server{AnswerMode: "keyword"}, "/readyz")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"answer_mode":"keyword"`) {
		t.Fatalf("status %d, body %s; a degraded mode is reported but the server is still ready", rec.Code, rec.Body)
	}
}

func TestReadyzOmitsTheAnswerModeWhenUnknown(t *testing.T) {
	if body := serve(t, &Server{}, "/readyz").Body.String(); strings.Contains(body, "answer_mode") {
		t.Fatalf("body %s names a mode nobody declared", body)
	}
}

// Every operation of the contract has a handler: a 501 would mean one was left as a stub.
func TestNoOperationAnswersNotImplemented(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, ".gen.go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(source), "StatusNotImplemented") || strings.Contains(string(source), "ErrNotImplemented") {
			t.Errorf("%s still answers 501", file)
		}
	}
}

func TestRequestOutsideTheContractIs400WithDetail(t *testing.T) {
	rec := call(t, "/api/v1/documents?source_id=not-a-uuid", "viewer")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "source_id") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestOnlyAPublicDetailReachesTheProblem(t *testing.T) {
	plain := ProblemFor(fmt.Errorf("db: %w", errs.ErrInvalid))
	public := ProblemFor(errs.WithPublicDetail(fmt.Errorf("secret internals: %w", errs.ErrInvalid), "the license has expired"))
	if plain.Detail != nil || public.Detail == nil || *public.Detail != "the license has expired" || public.Status != http.StatusUnprocessableEntity {
		t.Fatalf("plain %+v, public %+v", plain, public)
	}
}
