//go:build integration

package documents

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/dbtest"
)

const longAgo = 48 * time.Hour

type purgeFixture struct {
	t      *testing.T
	ctx    context.Context
	m      *Module
	tenant uuid.UUID
	now    time.Time
}

func newPurgeFixture(t *testing.T) purgeFixture {
	t.Helper()
	d := dbtest.Start(t)
	return purgeFixture{t: t, ctx: context.Background(), tenant: dbtest.NewTenant(t, d), now: time.Now(),
		m: &Module{DB: d, Blobs: blobs.New(t.TempDir())}}
}

// storeBlob keeps an original and a derived file for it, last written `age` ago, and returns its content hash.
func (f purgeFixture) storeBlob(content string, age time.Duration) string {
	f.t.Helper()
	staged, err := f.m.Blobs.Stage(strings.NewReader(content))
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.m.Blobs.Commit(f.tenant, staged); err != nil {
		f.t.Fatal(err)
	}
	written := f.now.Add(-age)
	if err := os.Chtimes(f.m.Blobs.BlobPath(f.tenant, staged.Hash), written, written); err != nil {
		f.t.Fatal(err)
	}
	key := blobs.DerivedKey{Hash: staged.Hash, Stage: "parse", Version: "v1"}
	if err := f.m.Blobs.WriteDerived(key, func(w io.Writer) error { _, err := w.Write([]byte("{}")); return err }); err != nil {
		f.t.Fatal(err)
	}
	return staged.Hash
}

// document stores a document with one version of the given content, a block and a passage. deletedAgo is nil for
// a document in force.
func (f purgeFixture) document(hash string, deletedAgo *time.Duration) {
	f.t.Helper()
	err := f.m.DB.InTenantTx(f.ctx, f.tenant, func(tx pgx.Tx) error {
		var doc, version uuid.UUID
		if err := tx.QueryRow(f.ctx, `INSERT INTO documents.documents (source_id, external_id, title, deleted_at)
			VALUES ($1, $2, 't', $3) RETURNING id`, uuid.New(), uuid.NewString(), f.deletedAt(deletedAgo)).Scan(&doc); err != nil {
			return err
		}
		if err := tx.QueryRow(f.ctx, `INSERT INTO documents.document_versions (document_id, content_hash, mime, size, state)
			VALUES ($1, $2, 'application/pdf', 1, 'parsed') RETURNING id`, doc, hash).Scan(&version); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO documents.blocks (version_id, ord, kind, page, text) VALUES ($1, 0, 'paragraph', 1, 'x')`, version); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO documents.chunks (id, version_id, document_id, ord, page, text, tokens)
			VALUES ($1, $2, $3, 0, 1, 'x', 1)`, uuid.New(), version, doc)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f purgeFixture) deletedAt(ago *time.Duration) *time.Time {
	if ago == nil {
		return nil
	}
	at := f.now.Add(-*ago)
	return &at
}

func (f purgeFixture) count(table string) (n int) {
	f.t.Helper()
	err := f.m.DB.InTenantTx(f.ctx, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f purgeFixture) hasFiles(hash string) (original, derived bool) {
	_, origErr := os.Stat(f.m.Blobs.BlobPath(f.tenant, hash))
	_, derivedErr := os.Stat(f.m.Blobs.DerivedPath(blobs.DerivedKey{Hash: hash, Stage: "parse", Version: "v1"}))
	return origErr == nil, derivedErr == nil
}

func TestDeletedDocumentContentIsPurged(t *testing.T) {
	f := newPurgeFixture(t)
	gone, recent := longAgo, time.Hour
	f.document(f.storeBlob("deleted long ago", longAgo), &gone)
	f.document(f.storeBlob("deleted a moment ago", longAgo), &recent)

	if err := f.m.PurgeDeleted(f.ctx, f.tenant, f.now); err != nil {
		t.Fatal(err)
	}

	if blocks, chunks := f.count("documents.blocks"), f.count("documents.chunks"); blocks != 1 || chunks != 1 {
		t.Fatalf("%d blocks and %d chunks left, want only those of the document deleted within the grace period", blocks, chunks)
	}
}

func TestUnreferencedFilesAreErasedAfterTheGracePeriod(t *testing.T) {
	f := newPurgeFixture(t)
	gone := longAgo
	deleted := f.storeBlob("deleted document", longAgo)
	f.document(deleted, &gone)
	orphan := f.storeBlob("nobody references this", longAgo)
	fresh := f.storeBlob("stored a moment ago", time.Minute)

	if err := f.m.PurgeDeleted(f.ctx, f.tenant, f.now); err != nil {
		t.Fatal(err)
	}

	for name, hash := range map[string]string{"deleted document": deleted, "orphan": orphan} {
		if original, derived := f.hasFiles(hash); original || derived {
			t.Errorf("%s: original %v, derived %v; want both erased", name, original, derived)
		}
	}
	if original, derived := f.hasFiles(fresh); !original || !derived {
		t.Error("a file stored within the grace period must stay: its version may not be recorded yet")
	}
}

func TestLiveBlobsAreKept(t *testing.T) {
	f := newPurgeFixture(t)
	live := f.storeBlob("a document in force", longAgo)
	f.document(live, nil)

	if err := f.m.PurgeDeleted(f.ctx, f.tenant, f.now); err != nil {
		t.Fatal(err)
	}

	if original, derived := f.hasFiles(live); !original || !derived {
		t.Fatalf("original %v, derived %v; a document in force keeps its files", original, derived)
	}
	if f.count("documents.blocks") != 1 || f.count("documents.chunks") != 1 {
		t.Fatal("a document in force keeps its blocks and passages")
	}
}
