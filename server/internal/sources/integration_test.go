//go:build integration

package sources

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/dbtest"
)

// fixture is one tenant with one folder source.
type fixture struct {
	t        *testing.T
	ctx      context.Context
	m        *Module
	tenantID uuid.UUID
	sourceID uuid.UUID
	root     string
}

func newFixture(t *testing.T, files ...string) fixture {
	t.Helper()
	d := dbtest.Start(t)
	f := fixture{t: t, ctx: dbtest.WithJobs(t, d), tenantID: dbtest.NewTenant(t, d), root: writeTree(t, files...)}
	f.m = &Module{DB: d, Blobs: blobs.New(t.TempDir()), Logger: slog.Default()}
	f.sourceID = f.addSource(f.tenantID)
	return f
}

func (f fixture) addSource(tenantID uuid.UUID) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := f.m.DB.InTenantTx(f.ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		id, err = AddFolder(f.ctx, tx, NewFolder{TenantID: tenantID, Name: "docs", Path: f.root})
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f fixture) rescan() {
	f.t.Helper()
	if err := f.m.Rescan(f.ctx, events.SourceRescan{TenantID: f.tenantID, SourceID: f.sourceID}); err != nil {
		f.t.Fatal(err)
	}
}

func (f fixture) fetch(externalID string) {
	f.t.Helper()
	if err := f.m.Fetch(f.ctx, events.VersionFetch{TenantID: f.tenantID, SourceID: f.sourceID, ExternalID: externalID}); err != nil {
		f.t.Fatal(err)
	}
}

func TestRLSIsolatesTenants(t *testing.T) {
	f := newFixture(t, "a.pdf")
	other := dbtest.NewTenant(t, f.m.DB)
	f.addSource(other)
	err := f.m.DB.InTenantTx(f.ctx, f.tenantID, func(tx pgx.Tx) error {
		list, err := List(f.ctx, tx)
		if err == nil && (len(list) != 1 || list[0].ID != f.sourceID) {
			t.Errorf("tenant sees %d sources, want only its own", len(list))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRescanFetchesNewFiles(t *testing.T) {
	f := newFixture(t, "Calidad/a.pdf", "Calidad/b.pdf")
	f.rescan()
	dbtest.RequireJobs(t, f.m.DB, events.SourceRescan{}, events.VersionFetch{}, events.VersionFetch{})
}

func TestRescanOfUnmountedRootDeletesNothing(t *testing.T) {
	f := newFixture(t, "a.pdf", "b.pdf")
	f.rescan()
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	f.rescan()
	dbtest.RequireNoJob(t, f.m.DB, events.DocumentDeleted{})
}

// copySamplePDF puts a real PDF at rel: fetch quarantines files that are not a supported type.
func copySamplePDF(t *testing.T, root, rel string) {
	t.Helper()
	pdf, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "PNT-014-rev3.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), pdf, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRenameIsAMove(t *testing.T) {
	f := newFixture(t, "old/PNT-014.pdf")
	copySamplePDF(t, f.root, "old/PNT-014.pdf")
	f.rescan()
	f.fetch("old/PNT-014.pdf")
	if err := os.Rename(filepath.Join(f.root, "old"), filepath.Join(f.root, "new")); err != nil {
		t.Fatal(err)
	}
	f.rescan()
	dbtest.RequireJobs(t, f.m.DB, events.SourceRescan{}, events.VersionFetch{}, events.VersionStored{}, events.DocumentMoved{})
}

func TestUnchangedContentIsNotAnnouncedTwice(t *testing.T) {
	f := newFixture(t, "a.pdf")
	copySamplePDF(t, f.root, "a.pdf")
	f.fetch("a.pdf")
	f.fetch("a.pdf")
	dbtest.RequireJobs(t, f.m.DB, events.SourceRescan{}, events.VersionStored{})
}

func TestHostileFileIsQuarantined(t *testing.T) {
	f := newFixture(t)
	bomb := writeZip(t, map[string][]byte{"word/document.xml": make([]byte, 50<<20)})
	if err := os.Rename(bomb, filepath.Join(f.root, "bomb.docx")); err != nil {
		t.Fatal(err)
	}
	f.fetch("bomb.docx")
	err := f.m.DB.InTenantTx(f.ctx, f.tenantID, func(tx pgx.Tx) error {
		list, err := ListQuarantine(f.ctx, tx)
		if err == nil && (len(list) != 1 || list[0].Reason != "hostile") {
			t.Errorf("quarantine = %+v, want one hostile entry", list)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
