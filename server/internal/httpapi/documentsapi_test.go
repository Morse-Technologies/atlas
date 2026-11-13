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
	"github.com/morse/atlas/server/internal/graph"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources"
)

type fakeLibrary struct {
	DocumentLibrary
	dir string
}

func (f fakeLibrary) OpenFile(context.Context, documents.Actor, documents.FileRequest) (documents.ViewerFile, error) {
	return f.stored("doc.pdf", "folder/PNT-014.pdf", "application/pdf")
}

func (f fakeLibrary) OpenOriginal(context.Context, documents.Actor, documents.FileRequest) (documents.ViewerFile, error) {
	return f.stored("sheet.xlsx", "folder/Plan.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
}

func (f fakeLibrary) stored(file, name, mime string) (documents.ViewerFile, error) {
	path := filepath.Join(f.dir, file)
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		return documents.ViewerFile{}, err
	}
	opened, err := os.Open(path)
	return documents.ViewerFile{File: opened, Name: name, Mime: mime, ModTime: time.Unix(0, 0)}, err
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

func TestOriginalIsSentAsAttachment(t *testing.T) {
	req := adminRequest(http.MethodGet, "/api/v1/documents/"+uuid.NewString()+"/versions/"+uuid.NewString()+"/original", "")
	rec := serveTo(t, &Server{Accounts: people, Documents: fakeLibrary{dir: t.TempDir()}}, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "0123456789" {
		t.Fatalf("status %d, body %q; want 200 and the whole file", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename=Plan.xlsx` {
		t.Fatalf("Content-Disposition = %q; want an attachment named after the original", got)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml") {
		t.Fatalf("Content-Type = %q; want the type of the stored file", rec.Header().Get("Content-Type"))
	}
}

type recordingSources struct {
	SourceAdmin
	deleted []sources.UploadedFile
}

func (r *recordingSources) DeleteUpload(_ context.Context, _ sources.Actor, f sources.UploadedFile) error {
	r.deleted = append(r.deleted, f)
	return nil
}

func (fakeLibrary) Get(_ context.Context, _ documents.Actor, id uuid.UUID) (documents.Document, error) {
	return documents.Document{ID: id, SourceID: uuid.New(), Path: "a.pdf"}, nil
}

func TestDeleteDocumentDeletesTheUploadedFileOfTheDocument(t *testing.T) {
	rec := &recordingSources{}
	id := uuid.New()
	res := serveTo(t, &Server{Accounts: people, Documents: fakeLibrary{}, Sources: rec}, adminRequest(http.MethodDelete, "/api/v1/documents/"+id.String(), ""))
	if res.Code != http.StatusNoContent || len(rec.deleted) != 1 || rec.deleted[0].ExternalID != "a.pdf" || rec.deleted[0].DocumentID != id {
		t.Fatalf("status %d, deleted %+v; want 204 and the document's own file", res.Code, rec.deleted)
	}
}

func TestAViewerCannotDeleteADocument(t *testing.T) {
	req := adminRequest(http.MethodDelete, "/api/v1/documents/"+uuid.NewString(), "")
	req.Header.Set("Cookie", sessionCookie+"=viewer")
	if res := serveTo(t, &Server{Accounts: people, Documents: fakeLibrary{}, Sources: &recordingSources{}}, req); res.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", res.Code)
	}
}

// hidingLibrary finds every document except one.
type hidingLibrary struct {
	fakeLibrary
	hidden uuid.UUID
}

func (h hidingLibrary) Get(ctx context.Context, who documents.Actor, id uuid.UUID) (documents.Document, error) {
	if id == h.hidden {
		return documents.Document{}, errs.ErrNotFound
	}
	return h.fakeLibrary.Get(ctx, who, id)
}

type linkedGraph struct {
	GraphView
	links []graph.Link
}

func (l linkedGraph) RelatedTo(context.Context, graph.Actor, uuid.UUID) ([]graph.Link, error) {
	return l.links, nil
}

func (linkedGraph) EntitiesOf(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID][]graph.Entity, error) {
	return nil, nil
}

func TestRelatedSkipsDocumentsThePersonCannotRead(t *testing.T) {
	visible, hidden := uuid.New(), uuid.New()
	srv := &Server{Accounts: people, Documents: hidingLibrary{hidden: hidden},
		Graph: linkedGraph{links: []graph.Link{{DocumentID: hidden, Kind: "references"}, {DocumentID: visible, Kind: "duplicates"}}}}
	res := serveTo(t, srv, adminRequest(http.MethodGet, "/api/v1/documents/"+uuid.NewString()+"/related", ""))
	body := res.Body.String()
	if res.Code != http.StatusOK || strings.Contains(body, hidden.String()) || !strings.Contains(body, visible.String()) || !strings.Contains(body, `"via":"duplicates"`) {
		t.Fatalf("status %d, body %s; want only the readable document, with how it is linked", res.Code, body)
	}
}
