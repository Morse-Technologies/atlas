//go:build integration

package app

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
)

func TestJobCountsShowAQueuedJobAndQuarantineStartsAtZero(t *testing.T) {
	session, _ := consoleFixture(t)
	ctx := context.Background()
	queue, err := jobs.NewClient(session.db.Pool, jobs.Setup{})
	if err != nil {
		t.Fatal(err)
	}
	err = session.db.InTenantTx(jobs.WithClient(ctx, queue), session.tenantID, func(tx pgx.Tx) error {
		return jobs.Enqueue(jobs.WithClient(ctx, queue), tx, events.MaintTenant{TenantID: session.tenantID})
	})
	if err != nil {
		t.Fatal(err)
	}

	counts, err := readJobCounts(ctx, session.db)
	if err != nil || len(counts) != 1 || counts[0].Value != 1 || counts[0].Labels["queue"] != events.QueueMaint {
		t.Fatalf("job counts = %+v, %v; want one job in the maint queue", counts, err)
	}
	quarantine, err := readQuarantineCount(ctx, session.db)
	if err != nil || len(quarantine) != 1 || quarantine[0].Value != 0 {
		t.Fatalf("quarantine = %+v, %v; want a single zero", quarantine, err)
	}
}
