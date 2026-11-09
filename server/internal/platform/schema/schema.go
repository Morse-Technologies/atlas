// Package schema applies and checks database migrations: goose for the schema, rivermigrate for River
// (ATLAS.md §4.3). Used by `atlas migrate`, by the startup check of every role and by integration tests.
package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/morse/atlas/server/db/migrations"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
)

// migrationLockKey serializes concurrent `atlas migrate` runs. goose's own session locker would cover only
// goose, not the River migrations that follow, so one advisory lock wraps both.
const migrationLockKey = 7_311_042

var errNoMigrations = errors.New("no embedded migrations")

// Migrate applies the schema migrations, then River's, under one advisory lock. url is the owner role.
func Migrate(ctx context.Context, url string) error {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("migrate connect: %w", err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate connect: %w", err)
	}
	defer conn.Release()
	return withAdvisoryLock(ctx, conn.Conn(), func() error { return migrateAll(ctx, pool) })
}

// withAdvisoryLock holds a session lock on conn while fn runs. The lock also ends with the session.
func withAdvisoryLock(ctx context.Context, conn *pgx.Conn, fn func() error) error {
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("migration lock: %w", err)
	}
	fnErr := fn()
	_, unlockErr := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockKey)
	return errors.Join(fnErr, unlockErr)
}

func migrateAll(ctx context.Context, pool *pgxpool.Pool) error {
	provider, sqlDB, err := newProvider(pool)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("schema migrations: %w", err)
	}
	if err := migrateRiver(ctx, pool); err != nil {
		return err
	}
	// The app role reads the version at startup (Require).
	_, err = pool.Exec(ctx, "GRANT SELECT ON goose_db_version TO atlas_app")
	return err
}

// newProvider builds a goose provider over the embedded migrations on a database/sql view of the pool.
func newProvider(pool *pgxpool.Pool) (*goose.Provider, *sql.DB, error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("goose: %w", err)
	}
	return provider, sqlDB, nil
}

func migrateRiver(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: jobs.Schema})
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrations: %w", err)
	}
	return nil
}

// Require stops a process whose database is older than its code and returns the version the code needs.
// A newer database is accepted: expand/contract keeps it compatible with the previous binary (ATLAS.md §4.3).
func Require(ctx context.Context, d *db.DB) (int64, error) {
	provider, sqlDB, err := newProvider(d.Pool)
	if err != nil {
		return 0, err
	}
	defer sqlDB.Close()
	required, err := newestMigration(provider)
	if err != nil {
		return 0, err
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read schema version (run `atlas migrate` first): %w", err)
	}
	if current < required {
		return 0, fmt.Errorf("database schema %d is older than %d: run `atlas migrate`", current, required)
	}
	return required, nil
}

func newestMigration(provider *goose.Provider) (int64, error) {
	sources := provider.ListSources()
	if len(sources) == 0 {
		return 0, errNoMigrations
	}
	return sources[len(sources)-1].Version, nil
}
