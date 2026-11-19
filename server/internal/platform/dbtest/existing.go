//go:build integration

package dbtest

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/schema"
)

const (
	// envDatabaseURL is the owner URL of a Postgres that already holds a migrated database. Each test works on a
	// private copy of it, so tests stay independent. Without it the tests of a package share one container of their
	// own (see sharedTemplateURL).
	envDatabaseURL = "ATLAS_TEST_DATABASE_URL"
	// envSkipMigrate set to 1 leaves the schema as it is. The migration check needs it: the database was migrated by
	// a newer version than the code under test, which must not try to bring the schema back to its own version.
	envSkipMigrate = "ATLAS_TEST_SKIP_MIGRATE"

	maintenanceDatabase = "postgres"
	// cloneLockKey makes the copies of one template wait for each other: Postgres refuses two at the same time.
	cloneLockKey = 7301
)

func skipMigrate() bool { return os.Getenv(envSkipMigrate) == "1" }

// ownerDatabase returns the owner URL of an empty-or-migrated database for one test.
func ownerDatabase(t *testing.T) string {
	t.Helper()
	if templateURL := os.Getenv(envDatabaseURL); templateURL != "" {
		return cloneDatabase(t, templateURL)
	}
	return cloneDatabase(t, sharedTemplateURL(t))
}

// sharedTemplate is the migrated Postgres that every test of one test binary copies. Starting a container per
// test made the integration suite take minutes. Ryuk (testcontainers) removes the container when the binary ends.
var sharedTemplate struct {
	once sync.Once
	url  string
	err  error
}

func sharedTemplateURL(t *testing.T) string {
	t.Helper()
	sharedTemplate.once.Do(func() {
		ctx := context.Background()
		url, _, err := runPostgres(ctx)
		if err == nil {
			err = schema.Migrate(ctx, url)
		}
		sharedTemplate.url, sharedTemplate.err = url, err
	})
	if sharedTemplate.err != nil {
		t.Fatal(sharedTemplate.err)
	}
	return sharedTemplate.url
}

// cloneDatabase makes a copy of the database in templateURL and drops it when the test ends.
func cloneDatabase(t *testing.T, templateURL string) string {
	t.Helper()
	ctx := context.Background()
	name := "atlas_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := pgx.Connect(ctx, withDatabase(t, templateURL, maintenanceDatabase))
	if err != nil {
		t.Fatalf("connect to %s: %v", envDatabaseURL, err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	createFromTemplate(t, admin, name, databaseName(t, templateURL))
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	return withDatabase(t, templateURL, name)
}

func createFromTemplate(t *testing.T, admin *pgx.Conn, name, template string) {
	t.Helper()
	ctx := context.Background()
	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", cloneLockKey); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "SELECT pg_advisory_unlock($1)", cloneLockKey) //nolint:errcheck // the lock ends with the session
	create := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize() + " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	if _, err := admin.Exec(ctx, create); err != nil {
		t.Fatalf("copy database %s: %v", template, err)
	}
}

func databaseName(t *testing.T, rawURL string) string {
	t.Helper()
	return strings.TrimPrefix(parseURL(t, rawURL).Path, "/")
}

func withDatabase(t *testing.T, rawURL, database string) string {
	t.Helper()
	u := parseURL(t, rawURL)
	u.Path = "/" + database
	return u.String()
}

func parseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %s: %v", envDatabaseURL, err)
	}
	return u
}
