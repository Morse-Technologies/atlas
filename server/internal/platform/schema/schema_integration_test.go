//go:build integration

package schema_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/morse/atlas/server/db/migrations"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/schema"
)

const moduleSchemas = `('sources','documents','catalog','search','access','graph','audit','platform','river')`

func countTables(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	row := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema IN ` + moduleSchemas)
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Every migration has a Down. None had ever run, so this proves they undo what the Up did, and that the schema
// can be built again from nothing afterwards.
func TestMigrationsGoUpThenDownThenUpAgain(t *testing.T) {
	ctx := context.Background()
	url := dbtest.StartOwner(t)
	if err := schema.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	conn := stdlib.OpenDB(*mustConfig(t, url))
	t.Cleanup(func() { _ = conn.Close() })
	built := countTables(t, conn)
	if built == 0 {
		t.Fatal("migrating built no tables")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("rolling every migration back: %v", err)
	}
	if left := countTables(t, conn); left != 0 {
		t.Fatalf("%d tables are left after rolling everything back", left)
	}
	if err := schema.Migrate(ctx, url); err != nil {
		t.Fatalf("migrating again after a full rollback: %v", err)
	}
	if again := countTables(t, conn); again != built {
		t.Fatalf("%d tables after rebuilding, %d the first time", again, built)
	}
}

func mustConfig(t *testing.T, url string) *pgx.ConnConfig {
	t.Helper()
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
