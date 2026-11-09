package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/documents"
)

type fakeLibrary struct {
	DocumentLibrary
	dir string
}

func (f fakeLibrary) OpenFile(context.Context, documents.Actor, documents.FileRequest) (documents.ViewerFile, error) {
	path := filepath.Join(f.dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		return documents.ViewerFile{}, err
	}
	file, err := os.Open(path)
	return documents.ViewerFile{File: file, Name: "folder/PNT-014.pdf", ModTime: time.Unix(0, 0)}, err
}

func (fakeLibrary) List(context.Context, documents.Actor, documents.Listing) ([]documents.Document, uuid.UUID, error) {
	doc := documents.Document{ID: uuid.New(), Title: "PNT-014", Band: "procedure", Status: "current", Path: "a/b.pdf",
		Attributes: map[string]string{}, Versions: []documents.Version{{Revision: "v1", State: "indexed", Pages: 4}}}
	return []documents.Document{doc}, uuid.New(), nil
}

func TestVersionFileAnswersRangeRequests(t *testing.T) {
	req := adminRequest(http.MethodGet, "/api/v1/documents/"+uuid.NewString()+"/versions/"+uuid.NewString()+"/file", "")
	req.Header.Set("Range", "bytes=2-5")
	rec := serveTo(t, &Server{Accounts: people, Documents: fakeLibrary{dir: t.TempDir()}}, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" {
		t.Fatalf("status %d, body %q; want 206 and bytes 2-5", rec.Code, rec.Body)
	}
	if rec.Header().Get("Content-Type") != "application/pdf" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers %v", rec.Header())
	}
}

func TestDocumentListMapsFieldsAndNextCursor(t *testing.T) {
	rec := serveTo(t, &Server{Accounts: people, Documents: fakeLibrary{}}, adminRequest(http.MethodGet, "/api/v1/documents?band=procedure&q=pnt", ""))
	body := rec.Body.String()
	for _, want := range []string{`"title":"PNT-014"`, `"band":"procedure"`, `"revision":"v1"`, `"entities":[]`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s: %s", want, body)
		}
	}
	if rec.Header().Get("Next-Cursor") == "" {
		t.Fatal("a full page must carry Next-Cursor")
	}
}

func TestViewerCanReadDocumentsButNotCorrectThem(t *testing.T) {
	viewer := func(method, path, body string) *http.Request {
		req := adminRequest(method, path, body)
		req.Header.Set("Cookie", sessionCookie+"=viewer")
		return req
	}
	srv := &Server{Accounts: people, Documents: fakeLibrary{}}
	if rec := serveTo(t, srv, viewer(http.MethodGet, "/api/v1/documents", "")); rec.Code != http.StatusOK {
		t.Fatalf("viewer list: %d", rec.Code)
	}
	rec := httptest.NewRecorder()
	NewHandler(srv).ServeHTTP(rec, viewer(http.MethodPatch, "/api/v1/documents/"+uuid.NewString()+"/metadata", `{"title":"x"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer correct: %d, want 403", rec.Code)
	}
}
