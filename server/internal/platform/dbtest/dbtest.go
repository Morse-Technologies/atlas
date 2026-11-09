//go:build integration

// Package dbtest starts a real, migrated Postgres for integration tests (Postgres is never mocked,
// ATLAS.md §17). Tests connect as atlas_app, so RLS applies exactly as in production.
package dbtest

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/schema"
)

const (
	image         = "pgvector/pgvector:0.8.7-pg18@sha256:c29f30688a9ad961691bdf0ec1650717cc132a5814619d80f80463d6e4c42e47"
	ownerPassword = "postgres"
	appRole       = "atlas_app"
	appPassword   = "atlas-test"
)

// Start runs a migrated Postgres for this test and returns a connection as the app role.
func Start(t *testing.T) *db.DB {
	t.Helper()
	ctx := context.Background()
	ownerURL := startPostgres(t)
	if err := schema.Migrate(ctx, ownerURL); err != nil {
		t.Fatal(err)
	}
	d, err := db.Connect(ctx, enableAppLogin(t, ownerURL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Pool.Close)
	return d
}

// StartOwner starts an empty Postgres and returns the URL of its owner role, for tests of the migrations themselves.
func StartOwner(t *testing.T) string { return startPostgres(t) }

func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := postgres.Run(ctx, image, postgres.WithDatabase("atlas"), postgres.WithUsername("postgres"),
		postgres.WithPassword(ownerPassword), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	ownerURL, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return ownerURL
}

// enableAppLogin does what the deployment does: give atlas_app a password. Returns its connection URL.
func enableAppLogin(t *testing.T, ownerURL string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "ALTER ROLE atlas_app LOGIN PASSWORD '"+appPassword+"'"); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(appRole, appPassword)
	return u.String()
}

// NewTenant creates a tenant with the Core catalog, as `atlas setup` does.
func NewTenant(t *testing.T, d *db.DB) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := d.InTx(ctx, func(tx pgx.Tx) error {
		var err error
		if id, err = access.CreateTenant(ctx, tx, "Org "+uuid.NewString()); err != nil {
			return err
		}
		if err := db.SetTenant(ctx, tx, id); err != nil {
			return err
		}
		return catalog.SeedCore(ctx, tx)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// WithJobs returns a context that can enqueue jobs (insert-only River client).
func WithJobs(t *testing.T, d *db.DB) context.Context {
	t.Helper()
	client, err := jobs.NewClient(d.Pool, jobs.Setup{})
	if err != nil {
		t.Fatal(err)
	}
	return jobs.WithClient(context.Background(), client)
}

// RequireJobs asserts the jobs waiting in River are exactly these kinds, in this order (rivertest).
func RequireJobs(t *testing.T, d *db.DB, expected ...river.JobArgs) {
	t.Helper()
	list := make([]rivertest.ExpectedJob, len(expected))
	for i, args := range expected {
		list[i] = rivertest.ExpectedJob{Args: args, Opts: &rivertest.RequireInsertedOpts{Schema: jobs.Schema}}
	}
	rivertest.RequireManyInserted(context.Background(), t, riverpgxv5.New(d.Pool), list)
}

// RequireNoJob asserts no job of this kind was enqueued.
func RequireNoJob[T river.JobArgs](t *testing.T, d *db.DB, args T) {
	t.Helper()
	rivertest.RequireNotInserted(context.Background(), t, riverpgxv5.New(d.Pool), args, &rivertest.RequireInsertedOpts{Schema: jobs.Schema})
}
