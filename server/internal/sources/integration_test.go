//go:build integration

package sources

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
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

// quarantineReasons lists the reason of every quarantined file.
func (f fixture) quarantineReasons() []string {
	f.t.Helper()
	var reasons []string
	err := f.m.DB.InTenantTx(f.ctx, f.tenantID, func(tx pgx.Tx) error {
		list, err := ListQuarantine(f.ctx, tx)
		for _, entry := range list {
			reasons = append(reasons, entry.Reason)
		}
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return reasons
}

func TestAPasswordProtectedPDFIsQuarantinedAsEncrypted(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(testdataFile("encrypted.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "locked.pdf"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	f.fetch("locked.pdf")

	if reasons := f.quarantineReasons(); len(reasons) != 1 || reasons[0] != "encrypted" {
		t.Fatalf("quarantine reasons = %v, want [encrypted]", reasons)
	}
}

func TestAFetchThatKeepsFailingIsQuarantinedOnlyOnTheLastTry(t *testing.T) {
	f := newFixture(t)
	failure := errs.NewTransient(errors.New("disk busy"))
	args := events.VersionFetch{TenantID: f.tenantID, SourceID: f.sourceID, ExternalID: "a.pdf"}

	if err := f.m.onFetchFailed(f.ctx, jobs.Failed[events.VersionFetch]{Args: args, Err: failure}); err != nil {
		t.Fatal(err)
	}
	if reasons := f.quarantineReasons(); len(reasons) != 0 {
		t.Fatalf("quarantine = %v after a try that will be retried, want nothing", reasons)
	}
	if err := f.m.onFetchFailed(f.ctx, jobs.Failed[events.VersionFetch]{Args: args, Err: failure, Exhausted: true}); err != nil {
		t.Fatal(err)
	}
	if reasons := f.quarantineReasons(); len(reasons) != 1 || reasons[0] != "retries_exhausted" {
		t.Fatalf("quarantine reasons = %v, want [retries_exhausted]", reasons)
	}
}

// scanRow is one row of sources.scans as the tests need it.
type scanRow struct {
	status   string
	finished bool
}

func (f fixture) scans() []scanRow {
	f.t.Helper()
	var out []scanRow
	err := f.m.DB.InTenantTx(f.ctx, f.tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(f.ctx, `SELECT status, finished_at IS NOT NULL FROM sources.scans ORDER BY generation`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r scanRow
			if err := rows.Scan(&r.status, &r.finished); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f fixture) source() Source {
	f.t.Helper()
	var out Source
	err := f.m.DB.InTenantTx(f.ctx, f.tenantID, func(tx pgx.Tx) error {
		list, err := List(f.ctx, tx)
		if err == nil {
			out = list[0]
		}
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f fixture) startScan() scanState {
	f.t.Helper()
	state, err := f.m.startScan(f.ctx, events.SourceRescan{TenantID: f.tenantID, SourceID: f.sourceID})
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}

func TestScanRowIsRunningUntilItFinishes(t *testing.T) {
	f := newFixture(t, "a.pdf")
	state := f.startScan()
	if got := f.scans(); len(got) != 1 || got[0].status != scanRunning || got[0].finished {
		t.Fatalf("scans = %+v, want one running scan with no finish time", got)
	}
	args := events.SourceRescan{TenantID: f.tenantID, SourceID: f.sourceID}
	if err := f.m.applyScan(f.ctx, scanResult{args: args, state: state}); err != nil {
		t.Fatal(err)
	}
	if got := f.scans(); len(got) != 1 || got[0].status != scanCompleted || !got[0].finished {
		t.Fatalf("scans = %+v, want one completed scan with a finish time", got)
	}
}

func TestFailedWalkPutsTheSourceInError(t *testing.T) {
	f := newFixture(t, "a.pdf")
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	f.rescan()
	src := f.source()
	if src.Status != "error" || src.LastError == nil {
		t.Fatalf("source = %q, last error %v; want error with its reason", src.Status, src.LastError)
	}
	if got := f.scans(); len(got) != 1 || got[0].status != scanFailed {
		t.Fatalf("scans = %+v, want one failed scan", got)
	}
}

func TestNextGoodScanClearsTheError(t *testing.T) {
	f := newFixture(t, "a.pdf")
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	f.rescan()
	if err := os.MkdirAll(f.root, 0o750); err != nil {
		t.Fatal(err)
	}
	f.rescan()
	if src := f.source(); src.Status != StatusActive || src.LastError != nil {
		t.Fatalf("source = %q, last error %v; want active with no error", src.Status, src.LastError)
	}
}

func TestFailedScanKeepsAPausedSourcePaused(t *testing.T) {
	f := newFixture(t, "a.pdf")
	pauseSource(f)
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	f.rescan()
	if src := f.source(); src.Status != StatusPaused || src.LastError == nil {
		t.Fatalf("source = %q, last error %v; want paused, with the reason kept", src.Status, src.LastError)
	}
}

func pauseSource(f fixture) {
	f.t.Helper()
	err := f.m.DB.InTenantTx(f.ctx, f.tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `UPDATE sources.sources SET status = 'paused'`)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func TestInterruptedScanIsClosedByTheNextOne(t *testing.T) {
	f := newFixture(t, "a.pdf")
	f.startScan()
	err := f.m.DB.InTenantTx(f.ctx, f.tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(f.ctx, `UPDATE sources.scans SET started_at = now() - interval '2 hours'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	f.rescan()
	if got := f.scans(); len(got) != 2 || got[0].status != scanFailed || got[1].status != scanCompleted {
		t.Fatalf("scans = %+v, want the old one failed as interrupted and the new one completed", got)
	}
}
