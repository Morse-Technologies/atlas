package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/sources"
)

type fakeSources struct {
	SourceAdmin
	paused, resumed bool
}

func (f *fakeSources) List(context.Context, sources.Actor) ([]sources.Source, error) {
	return []sources.Source{{ID: uuid.New(), Name: "Docs", Kind: "folder", Root: "/sources/docs", Status: "active"}}, nil
}

func (f *fakeSources) Pause(context.Context, sources.Actor, uuid.UUID) (sources.Source, error) {
	f.paused = true
	return sources.Source{Status: "paused"}, nil
}

func (f *fakeSources) Resume(context.Context, sources.Actor, uuid.UUID) (sources.Source, error) {
	f.resumed = true
	return sources.Source{Status: "active"}, nil
}

func (f *fakeSources) Scans(context.Context, sources.Actor, sources.Page) ([]sources.Scan, uuid.UUID, error) {
	return []sources.Scan{{ID: uuid.New(), StartedAt: time.Now(), FinishedAt: time.Now(), Discovered: 10, Changed: 3, Failed: 1}}, uuid.Nil, nil
}

// adminRequest is a JSON request from the signed-in administrator.
func adminRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "admin"})
	return req
}

func serveTo(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	NewHandler(srv).ServeHTTP(rec, req)
	return rec
}

func TestListSourcesMapsLocationAndKeepsProOff(t *testing.T) {
	rec := serveTo(t, &Server{Accounts: people, Sources: &fakeSources{}}, adminRequest(http.MethodGet, "/api/v1/sources", ""))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"location":"/sources/docs"`) || !strings.Contains(body, `"pro":false`) {
		t.Fatalf("status %d, body %s", rec.Code, body)
	}
}

func TestPatchPausedPicksPauseOrResume(t *testing.T) {
	f := &fakeSources{}
	srv := &Server{Accounts: people, Sources: f}
	id := uuid.NewString()
	serveTo(t, srv, adminRequest(http.MethodPatch, "/api/v1/sources/"+id, `{"paused":true}`))
	serveTo(t, srv, adminRequest(http.MethodPatch, "/api/v1/sources/"+id, `{"paused":false}`))
	if !f.paused || !f.resumed {
		t.Fatalf("paused=%v resumed=%v, want both", f.paused, f.resumed)
	}
}

func TestPatchWithoutFieldsIs422(t *testing.T) {
	srv := &Server{Accounts: people, Sources: &fakeSources{}}
	if rec := serveTo(t, srv, adminRequest(http.MethodPatch, "/api/v1/sources/"+uuid.NewString(), `{}`)); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
}

func TestRunMapsSkippedAndFailed(t *testing.T) {
	rec := serveTo(t, &Server{Accounts: people, Sources: &fakeSources{}}, adminRequest(http.MethodGet, "/api/v1/ingestion/runs", ""))
	for _, want := range []string{`"discovered":10`, `"indexed":3`, `"skipped":7`, `"failed":1`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %s: %s", want, rec.Body)
		}
	}
}

func checkRequest(t *testing.T, root, path string) *httptest.ResponseRecorder {
	t.Helper()
	srv := &Server{Accounts: people, Sources: &sources.Admin{Roots: []string{root}}}
	return serveTo(t, srv, adminRequest(http.MethodPost, "/api/v1/sources/check", `{"path":"`+path+`"}`))
}

func TestCheckFolderReportsAMissingFolderAsOkFalse(t *testing.T) {
	root := t.TempDir()
	rec := checkRequest(t, root, root+"/not-here")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":false`) {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestCheckFolderAcceptsAReadableDirectoryInsideTheRoots(t *testing.T) {
	root := t.TempDir()
	if rec := checkRequest(t, root, root); !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestCheckFolderRefusesPathsOutsideTheRoots(t *testing.T) {
	if rec := checkRequest(t, t.TempDir(), t.TempDir()); !strings.Contains(rec.Body.String(), `"ok":false`) {
		t.Fatalf("a folder outside the allowed roots must not pass: %s", rec.Body)
	}
}
