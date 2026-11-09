// Package db owns the connection pool and the tenant-scoped transaction helper (ATLAS.md §10.2).
// Every tenant-owned table defaults tenant_id from, and filters by, the atlas.tenant_id transaction setting.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/morse/atlas/server/internal/platform/errs"
)

// DB wraps the pool so transaction helpers stay within three parameters.
type DB struct {
	Pool *pgxpool.Pool
}

// Connect opens and pings a pool of the default size (the number of CPUs, at least 4).
func Connect(ctx context.Context, url string) (*DB, error) {
	return ConnectPool(ctx, PoolConfig{URL: url})
}

// PoolConfig is where to connect and how many connections to keep at most (0: the default).
type PoolConfig struct {
	URL      string
	MaxConns int32
}

// ConnectPool opens and pings a pool.
func ConnectPool(ctx context.Context, p PoolConfig) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(p.URL)
	if err != nil {
		return nil, fmt.Errorf("db config: %w", err)
	}
	if p.MaxConns > 0 {
		cfg.MaxConns = p.MaxConns
	}
	// A prepared query gets a generic plan after a few runs, made without knowing its values. For a word search
	// that is a walk over every document (hundreds of times slower), for a filter on a tenant or a person a poor
	// guess. Planning each query with its values costs a fraction of a millisecond.
	cfg.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_custom_plan"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// InTx runs fn in a transaction with no tenant: only tables without RLS (the tenant registry) are visible.
func (d *DB) InTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, d.Pool, fn)
}

// InTenantTx runs fn in a transaction scoped to one tenant by RLS.
func (d *DB) InTenantTx(ctx context.Context, tenantID uuid.UUID, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if err := SetTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// Scope is whose view of the data a transaction has: a tenant, and a person inside it.
type Scope struct {
	Tenant uuid.UUID
	User   uuid.UUID
}

// InScopeTx runs fn in a transaction scoped to one tenant and one person: row-level security then hides what
// that person may not read (a source's documents, ATLAS.md §10.2). Jobs use InTenantTx and see everything.
func (d *DB) InScopeTx(ctx context.Context, scope Scope, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if err := SetTenant(ctx, tx, scope.Tenant); err != nil {
			return err
		}
		if scope.User == uuid.Nil {
			return fmt.Errorf("set user: missing user id: %w", errs.ErrInvalid)
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('atlas.user_id', $1, true)", scope.User.String()); err != nil {
			return fmt.Errorf("set user: %w", err)
		}
		return fn(tx)
	})
}

// SetTenant scopes the rest of tx to tenantID. A nil tenant is refused: it would silently match nothing.
func SetTenant(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("set tenant: missing tenant id: %w", errs.ErrInvalid)
	}
	// set_config(..., true) is SET LOCAL with a bind parameter: it ends with the transaction.
	if _, err := tx.Exec(ctx, "SELECT set_config('atlas.tenant_id', $1, true)", tenantID.String()); err != nil {
		return fmt.Errorf("set tenant: %w", err)
	}
	return nil
}

// NotFoundIfNoRows turns pgx.ErrNoRows into errs.ErrNotFound, so callers and jobs treat a missing row as a
// domain error (404, job cancelled) instead of retrying it. Other errors are wrapped with what was read.
// A nil error stays nil, so it can wrap the result of a query whether or not it failed.
func NotFoundIfNoRows(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, errs.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", what, err)
}
