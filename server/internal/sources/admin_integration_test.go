//go:build integration

package sources

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

func newAdmin(t *testing.T) (*Admin, Actor) {
	t.Helper()
	d := dbtest.Start(t)
	admin := NewAdmin(d, t.TempDir(), []string{os.TempDir()})
	return admin, Actor{TenantID: dbtest.NewTenant(t, d), UserID: uuid.New()}
}

func TestCreatePauseResumeLeaveAnAuditTrail(t *testing.T) {
	ctx := dbtest.WithJobs(t, dbtest.Start(t))
	admin, who := newAdmin(t)
	src, err := admin.Create(ctx, who, NewSource{Name: "Docs", Kind: KindFolder, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if paused, err := admin.Pause(ctx, who, src.ID); err != nil || paused.Status != StatusPaused {
		t.Fatalf("Pause = %+v, %v", paused, err)
	}
	if resumed, err := admin.Resume(ctx, who, src.ID); err != nil || resumed.Status != StatusActive {
		t.Fatalf("Resume = %+v, %v", resumed, err)
	}
	events, _, err := (&audit.Service{DB: admin.DB}).List(ctx, who.TenantID, audit.Page{Size: 10})
	if err != nil || len(events) != 3 || events[0].Action != "source.resumed" {
		t.Fatalf("events = %+v, %v; want created, paused, resumed", events, err)
	}
}

func TestUnknownSourceKindIsRefused(t *testing.T) {
	admin, who := newAdmin(t)
	if _, err := admin.Create(context.Background(), who, NewSource{Name: "x", Kind: "sharepoint", Path: "/"}); err == nil {
		t.Fatal("a kind that is not built must be refused")
	}
}

func TestQuarantinePagesNewestFirst(t *testing.T) {
	admin, who := newAdmin(t)
	ctx := context.Background()
	err := admin.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		src, err := AddFolder(dbtest.WithJobs(t, admin.DB), tx, NewFolder{TenantID: who.TenantID, Name: "s", Path: t.TempDir()})
		if err != nil {
			return err
		}
		for _, name := range []string{"a.pdf", "b.pdf", "c.pdf"} {
			if _, err := tx.Exec(ctx, "INSERT INTO sources.quarantine (source_id, external_id, reason, detail) VALUES ($1, $2, 'corrupt', 'x')", src, name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	first, next, err := admin.Quarantine(ctx, who, Page{Size: 2})
	if err != nil || len(first) != 2 || next == uuid.Nil {
		t.Fatalf("first page = %d rows, next %v, %v", len(first), next, err)
	}
	rest, next, err := admin.Quarantine(ctx, who, Page{Before: next, Size: 2})
	if err != nil || len(rest) != 1 || next != uuid.Nil || rest[0].ExternalID != "a.pdf" {
		t.Fatalf("second page = %+v, next %v, %v", rest, next, err)
	}
}

func TestPressingSyncIsNotSwallowedByTheScanQueuedWhenTheSourceWasAdded(t *testing.T) {
	d := dbtest.Start(t)
	ctx := dbtest.WithJobs(t, d)
	admin := NewAdmin(d, t.TempDir(), []string{os.TempDir()})
	who := Actor{TenantID: dbtest.NewTenant(t, d), UserID: uuid.New()}
	src, err := admin.Create(ctx, who, NewSource{Name: "Docs", Kind: KindFolder, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Sync(ctx, who, src.ID); err != nil {
		t.Fatal(err)
	}
	dbtest.RequireJobs(t, d,
		events.SourceRescan{TenantID: who.TenantID, SourceID: src.ID},
		events.SourceRescan{TenantID: who.TenantID, SourceID: src.ID, Manual: true})
}

func TestTwoUploadsInARowAreBothScanned(t *testing.T) {
	admin, who := newAdmin(t)
	ctx := dbtest.WithJobs(t, admin.DB)
	for _, name := range []string{"a.pdf", "b.pdf"} {
		if _, err := admin.Upload(ctx, who, body(t, upload{name, "x"})); err != nil {
			t.Fatal(err)
		}
	}
	dbtest.RequireJobs(t, admin.DB, events.SourceRescan{}, events.SourceRescan{})
}

// breakingParts yields the parts of its body and then fails, like a connection that drops during an upload.
type breakingParts struct {
	PartReader
	served, allowed int
}

func (b *breakingParts) NextPart() (*multipart.Part, error) {
	if b.served == b.allowed {
		return nil, io.ErrUnexpectedEOF
	}
	b.served++
	return b.PartReader.NextPart()
}

func TestPartialUploadIsScanned(t *testing.T) {
	admin, who := newAdmin(t)
	ctx := dbtest.WithJobs(t, admin.DB)
	parts := &breakingParts{PartReader: body(t, upload{"a.pdf", "x"}, upload{"b.pdf", "x"}, upload{"c.pdf", "x"}), allowed: 2}

	result, err := admin.Upload(ctx, who, parts)

	if !errors.Is(err, errs.ErrInvalid) || result.Accepted != 2 {
		t.Fatalf("Upload = %+v, %v; want the two stored files and the failure returned", result, err)
	}
	dbtest.RequireJobs(t, admin.DB, events.SourceRescan{})
}

func TestRemovedSourceIsNotScanned(t *testing.T) {
	admin, who := newAdmin(t)
	ctx := dbtest.WithJobs(t, admin.DB)
	src, err := admin.Create(ctx, who, NewSource{Name: "Docs", Kind: KindFolder, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Remove(ctx, who, src.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := admin.List(ctx, who); err != nil || len(list) != 0 {
		t.Fatalf("List = %+v, %v; a removed source is not listed", list, err)
	}
	if err := admin.Sync(ctx, who, src.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("Sync on a removed source = %v, want ErrNotFound", err)
	}
	module := &Module{DB: admin.DB}
	if err := module.Rescan(ctx, events.SourceRescan{TenantID: who.TenantID, SourceID: src.ID}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("Rescan of a removed source = %v, want ErrNotFound", err)
	}
	due, err := dueSources(ctx, admin.DB, who.TenantID)
	if err != nil || len(due) != 0 {
		t.Fatalf("due sources = %v, %v; a removed source is never due", due, err)
	}
}

func dueSources(ctx context.Context, d *db.DB, tenant uuid.UUID) (ids []uuid.UUID, err error) {
	err = d.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		ids, err = sourcesdb.New(tx).ListDueSources(ctx)
		return err
	})
	return ids, err
}

func TestUploadSourceCannotBeRemoved(t *testing.T) {
	admin, who := newAdmin(t)
	ctx := dbtest.WithJobs(t, admin.DB)
	if _, err := admin.Upload(ctx, who, body(t, upload{"a.pdf", "x"})); err != nil {
		t.Fatal(err)
	}
	list, err := admin.List(ctx, who)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if err := admin.Remove(ctx, who, list[0].ID); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("Remove(uploads) = %v, want ErrConflict", err)
	}
}

func TestRemovalIsAudited(t *testing.T) {
	admin, who := newAdmin(t)
	ctx := dbtest.WithJobs(t, admin.DB)
	src, _ := admin.Create(ctx, who, NewSource{Name: "Docs", Kind: KindFolder, Path: t.TempDir()})
	if err := admin.Remove(ctx, who, src.ID); err != nil {
		t.Fatal(err)
	}
	events, _, err := (&audit.Service{DB: admin.DB}).List(ctx, who.TenantID, audit.Page{Size: 1})
	if err != nil || events[0].Action != "source.removed" || events[0].Subject != "source:"+src.ID.String() {
		t.Fatalf("events = %+v, %v; want source.removed on the source id", events, err)
	}
}

// uploaded stores one file through the upload path and returns the upload source and the file's place on disk.
func uploaded(t *testing.T, admin *Admin, who Actor, name string) (UploadedFile, string) {
	t.Helper()
	ctx := dbtest.WithJobs(t, admin.DB)
	if _, err := admin.Upload(ctx, who, body(t, upload{name, "x"})); err != nil {
		t.Fatal(err)
	}
	list, err := admin.List(ctx, who)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	return UploadedFile{SourceID: list[0].ID, ExternalID: name, DocumentID: uuid.New()}, filepath.Join(admin.UploadRoot, who.TenantID.String(), name)
}

func TestDeleteUploadRemovesTheFile(t *testing.T) {
	admin, who := newAdmin(t)
	file, path := uploaded(t, admin, who, "a.pdf")
	ctx := dbtest.WithJobs(t, admin.DB)
	if err := admin.DeleteUpload(ctx, who, file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the uploaded file is still there: %v", err)
	}
	dbtest.RequireJobs(t, admin.DB, events.DocumentDeleted{TenantID: who.TenantID, SourceID: file.SourceID, ExternalID: "a.pdf"})
	log, _, err := (&audit.Service{DB: admin.DB}).List(ctx, who.TenantID, audit.Page{Size: 1})
	if err != nil || log[0].Action != "document.deleted" || log[0].Subject != "document:"+file.DocumentID.String() {
		t.Fatalf("events = %+v, %v; want document.deleted on the document id", log, err)
	}
}

func TestDeleteRefusesFolderDocuments(t *testing.T) {
	admin, who := newAdmin(t)
	ctx := dbtest.WithJobs(t, admin.DB)
	dir := t.TempDir()
	src, err := admin.Create(ctx, who, NewSource{Name: "Docs", Kind: KindFolder, Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(admin.UploadRoot, who.TenantID.String(), "a.pdf")
	_ = os.MkdirAll(filepath.Dir(keep), 0o750)
	_ = os.WriteFile(keep, []byte("x"), 0o600)
	err = admin.DeleteUpload(ctx, who, UploadedFile{SourceID: src.ID, ExternalID: "a.pdf", DocumentID: uuid.New()})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("DeleteUpload on a folder source = %v, want ErrConflict", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("nothing may be deleted: %v", err)
	}
}
