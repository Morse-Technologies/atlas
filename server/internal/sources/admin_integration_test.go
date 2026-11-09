//go:build integration

package sources

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/dbtest"
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
