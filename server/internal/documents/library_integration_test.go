//go:build integration

package documents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/dbtest"
)

type libraryFixture struct {
	t    *testing.T
	ctx  context.Context
	lib  *Library
	who  Actor
	docs map[string]uuid.UUID
}

// newLibraryFixture inserts two documents with a parsed current version: a procedure and a standard.
func newLibraryFixture(t *testing.T) libraryFixture {
	t.Helper()
	d := dbtest.Start(t)
	f := libraryFixture{t: t, ctx: dbtest.WithJobs(t, d), lib: NewLibrary(d, blobs.New(t.TempDir())),
		who: Actor{TenantID: dbtest.NewTenant(t, d), UserID: uuid.New()}, docs: map[string]uuid.UUID{}}
	source := uuid.New()
	err := d.InTenantTx(f.ctx, f.who.TenantID, func(tx pgx.Tx) error {
		for _, doc := range []struct{ name, docType string }{{"a/PNT-014.pdf", "procedure"}, {"b/ISO-9001.pdf", "standard"}} {
			id, err := f.insert(tx, source, doc.name, doc.docType)
			if err != nil {
				return err
			}
			f.docs[doc.name] = id
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f libraryFixture) insert(tx pgx.Tx, source uuid.UUID, name, docType string) (uuid.UUID, error) {
	q := documentsdb.New(tx)
	id, err := q.UpsertDocument(f.ctx, documentsdb.UpsertDocumentParams{SourceID: source, ExternalID: name, Title: titleFromPath(name)})
	if err != nil {
		return uuid.Nil, err
	}
	version, err := q.UpsertVersion(f.ctx, documentsdb.UpsertVersionParams{DocumentID: id, ContentHash: uuid.NewString(), Mime: pdfMIME, Size: 1})
	if err != nil {
		return uuid.Nil, err
	}
	if err := q.MarkVersionParsed(f.ctx, documentsdb.MarkVersionParsedParams{ID: version, Pages: ptr(int32(3))}); err != nil {
		return uuid.Nil, err
	}
	if err := q.MarkVersionIndexed(f.ctx, version); err != nil {
		return uuid.Nil, err
	}
	if err := q.SetCurrentVersion(f.ctx, documentsdb.SetCurrentVersionParams{DocumentID: id, VersionID: &version}); err != nil {
		return uuid.Nil, err
	}
	_, err = tx.Exec(f.ctx, "UPDATE documents.documents SET doc_type = $1 WHERE id = $2", docType, id)
	return id, err
}

func TestListFiltersByBandAndText(t *testing.T) {
	f := newLibraryFixture(t)
	norms, _, err := f.lib.List(f.ctx, f.who, Listing{Band: "norm", Size: 10})
	if err != nil || len(norms) != 1 || norms[0].Path != "b/ISO-9001.pdf" || norms[0].Band != "norm" {
		t.Fatalf("norm band = %+v, %v", norms, err)
	}
	hits, _, err := f.lib.List(f.ctx, f.who, Listing{Query: "pnt-014", Size: 10})
	if err != nil || len(hits) != 1 || hits[0].Path != "a/PNT-014.pdf" {
		t.Fatalf("text search = %+v, %v", hits, err)
	}
	none, _, _ := f.lib.List(f.ctx, f.who, Listing{Query: "100%", Size: 10})
	if len(none) != 0 {
		t.Fatal("a typed % must not match everything")
	}
}

func TestListPagesByCursor(t *testing.T) {
	f := newLibraryFixture(t)
	first, next, err := f.lib.List(f.ctx, f.who, Listing{Size: 1})
	if err != nil || len(first) != 1 || next == uuid.Nil {
		t.Fatalf("first page = %d, next %v, %v", len(first), next, err)
	}
	second, next, err := f.lib.List(f.ctx, f.who, Listing{After: next, Size: 1})
	if err != nil || len(second) != 1 || next != uuid.Nil || second[0].ID == first[0].ID {
		t.Fatalf("second page = %+v, next %v, %v", second, next, err)
	}
}

func TestCorrectionIsManualAndSurvivesAMove(t *testing.T) {
	f := newLibraryFixture(t)
	id := f.docs["a/PNT-014.pdf"]
	doc, err := f.lib.Correct(f.ctx, f.who, Correction{ID: id, Patch: MetadataPatch{Title: ptr("Torque procedure"), Status: ptr("current")}})
	if err != nil || doc.Title != "Torque procedure" || len(doc.Provenance) != 2 || doc.Provenance[0].Source != "manual" {
		t.Fatalf("Correct = %+v, %v", doc, err)
	}
	dbtest.RequireJobs(t, f.lib.DB, events.DocumentChanged{TenantID: f.who.TenantID, DocumentID: id})
	err = f.lib.DB.InTenantTx(f.ctx, f.who.TenantID, func(tx pgx.Tx) error {
		return documentsdb.New(tx).MoveDocument(f.ctx, documentsdb.MoveDocumentParams{
			SourceID: doc.SourceID, OldExternalID: "a/PNT-014.pdf", NewExternalID: "c/renamed.pdf", Title: "renamed"})
	})
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := f.lib.Get(f.ctx, f.who, id)
	if moved.Title != "Torque procedure" || moved.Path != "c/renamed.pdf" {
		t.Fatalf("after move: title %q path %q; the corrected title must stay", moved.Title, moved.Path)
	}
}

func TestAnotherTenantCannotSeeTheDocument(t *testing.T) {
	f := newLibraryFixture(t)
	other := Actor{TenantID: dbtest.NewTenant(t, f.lib.DB)}
	if _, err := f.lib.Get(f.ctx, other, f.docs["a/PNT-014.pdf"]); err == nil {
		t.Fatal("RLS must hide the document from another tenant")
	}
}
