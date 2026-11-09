//go:build integration

package catalog_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/dbtest"
)

func countEntityTypes(t *testing.T, d *db.DB, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	err := d.InTenantTx(context.Background(), tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM catalog.entity_types").Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSeedCoreTwiceKeepsOneCopy(t *testing.T) {
	d := dbtest.Start(t)
	tenantID := dbtest.NewTenant(t, d) // seeds once
	before := countEntityTypes(t, d, tenantID)
	err := d.InTenantTx(context.Background(), tenantID, func(tx pgx.Tx) error { return catalog.SeedCore(context.Background(), tx) })
	if err != nil || before == 0 || countEntityTypes(t, d, tenantID) != before {
		t.Fatalf("seeding twice changed the catalog (before %d), err %v", before, err)
	}
}
