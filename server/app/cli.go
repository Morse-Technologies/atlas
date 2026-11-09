package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/sources"
)

// Admin commands work on the database directly: HTTP admin endpoints need login, which comes in week 7.

const tabPadding = 2

var errNeedSubcommand = errors.New("missing subcommand")

// runSetup creates the organization once and seeds its Core catalog. Running it again changes nothing.
func runSetup(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	org := flags.String("org", "", "organization name (required)")
	if err := flags.Parse(args); err != nil || *org == "" {
		return errors.New("usage: atlas setup --org NAME")
	}
	d, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer d.Pool.Close()
	id, err := createTenantOnce(ctx, d, *org)
	if err == nil {
		fmt.Println("tenant", id)
	}
	return err
}

// createTenantOnce returns the existing tenant, or creates it with its Core catalog in one transaction.
func createTenantOnce(ctx context.Context, d *db.DB, org string) (uuid.UUID, error) {
	existing, err := access.ListTenants(ctx, d.Pool)
	switch {
	case err != nil:
		return uuid.Nil, err
	case len(existing) > 0:
		return existing[0].ID, nil
	}
	var id uuid.UUID
	err = d.InTx(ctx, func(tx pgx.Tx) error {
		created, err := createTenantWithCatalog(ctx, tx, org)
		id = created
		return err
	})
	return id, err
}

func createTenantWithCatalog(ctx context.Context, tx pgx.Tx, org string) (uuid.UUID, error) {
	id, err := access.CreateTenant(ctx, tx, org)
	if err != nil {
		return uuid.Nil, err
	}
	if err := db.SetTenant(ctx, tx, id); err != nil {
		return uuid.Nil, err
	}
	return id, catalog.SeedCore(ctx, tx)
}

// cliSession is what every admin subcommand needs: the database, the tenant and a client to enqueue jobs.
type cliSession struct {
	db       *db.DB
	tenantID uuid.UUID
}

func openCLISession(ctx context.Context) (context.Context, cliSession, error) {
	d, err := openDB(ctx)
	if err != nil {
		return ctx, cliSession{}, err
	}
	tenantID, err := soleTenant(ctx, d)
	if err != nil {
		d.Pool.Close()
		return ctx, cliSession{}, err
	}
	client, err := jobs.NewClient(d.Pool, jobs.Setup{})
	if err != nil {
		d.Pool.Close()
		return ctx, cliSession{}, err
	}
	return jobs.WithClient(ctx, client), cliSession{db: d, tenantID: tenantID}, nil
}

// soleTenant returns the installation's tenant. On-prem has exactly one (ATLAS.md §10.2).
func soleTenant(ctx context.Context, d *db.DB) (uuid.UUID, error) {
	tenants, err := access.ListTenants(ctx, d.Pool)
	switch {
	case err != nil:
		return uuid.Nil, err
	case len(tenants) != 1:
		return uuid.Nil, fmt.Errorf("expected one tenant, found %d: run `atlas setup --org NAME`", len(tenants))
	}
	return tenants[0].ID, nil
}

func (s cliSession) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	return s.db.InTenantTx(ctx, s.tenantID, fn)
}

var sourceCommands = map[string]func(context.Context, cliSession, []string) error{
	"add":  sourceAdd,
	"list": sourceList,
	"sync": sourceSync,
}

// runSource: atlas source add --name N --path P | list | sync [--allow-mass-deletion] ID
func runSource(ctx context.Context, args []string) error {
	return runSubcommand(ctx, subcommandCall{args: args, table: sourceCommands})
}

var quarantineCommands = map[string]func(context.Context, cliSession, []string) error{
	"list":  quarantineList,
	"retry": quarantineRetry,
}

// runQuarantine: atlas quarantine list | retry ID
func runQuarantine(ctx context.Context, args []string) error {
	return runSubcommand(ctx, subcommandCall{args: args, table: quarantineCommands})
}

type subcommandCall struct {
	args  []string
	table map[string]func(context.Context, cliSession, []string) error
}

func runSubcommand(ctx context.Context, call subcommandCall) error {
	if len(call.args) == 0 || call.table[call.args[0]] == nil {
		return errNeedSubcommand
	}
	ctx, session, err := openCLISession(ctx)
	if err != nil {
		return err
	}
	defer session.db.Pool.Close()
	return call.table[call.args[0]](ctx, session, call.args[1:])
}

func sourceAdd(ctx context.Context, s cliSession, args []string) error {
	flags := flag.NewFlagSet("source add", flag.ContinueOnError)
	name := flags.String("name", "", "display name")
	path := flags.String("path", "", "folder path on this host")
	if err := flags.Parse(args); err != nil || *name == "" || *path == "" {
		return errors.New("usage: atlas source add --name NAME --path PATH")
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		id, err := sources.AddFolder(ctx, tx, sources.NewFolder{TenantID: s.tenantID, Name: *name, Path: *path})
		if err == nil {
			fmt.Println("source", id, "added; first scan scheduled")
		}
		return err
	})
}

func sourceList(ctx context.Context, s cliSession, _ []string) error {
	var list []sources.Source
	err := s.inTx(ctx, func(tx pgx.Tx) (err error) {
		list, err = sources.List(ctx, tx)
		return err
	})
	if err != nil {
		return err
	}
	rows := make([][]string, len(list))
	for i, src := range list {
		rows[i] = []string{src.ID.String(), src.Name, src.Status, timeOrDash(src.LastScanAt), src.Root, textOrDash(src.LastError)}
	}
	return printTable([]string{"ID", "NAME", "STATUS", "LAST SCAN", "ROOT", "LAST ERROR"}, rows)
}

func sourceSync(ctx context.Context, s cliSession, args []string) error {
	flags := flag.NewFlagSet("source sync", flag.ContinueOnError)
	allow := flags.Bool("allow-mass-deletion", false, "apply deletions even if the source lost many files at once")
	if err := flags.Parse(args); err != nil {
		return err
	}
	id, err := singleIDArg(flags.Args(), "usage: atlas source sync [--allow-mass-deletion] SOURCE_ID")
	if err != nil {
		return err
	}
	req := events.SourceRescan{TenantID: s.tenantID, SourceID: id, AllowMassDeletion: *allow}
	return s.inTx(ctx, func(tx pgx.Tx) error { return sources.RequestRescan(ctx, tx, req) })
}

func quarantineList(ctx context.Context, s cliSession, _ []string) error {
	var list []sources.QuarantinedFile
	err := s.inTx(ctx, func(tx pgx.Tx) (err error) {
		list, err = sources.ListQuarantine(ctx, tx)
		return err
	})
	if err != nil {
		return err
	}
	rows := make([][]string, len(list))
	for i, q := range list {
		rows[i] = []string{q.ID.String(), q.Reason, q.ExternalID, q.Detail}
	}
	return printTable([]string{"ID", "REASON", "PATH", "DETAIL"}, rows)
}

func quarantineRetry(ctx context.Context, s cliSession, args []string) error {
	id, err := singleIDArg(args, "usage: atlas quarantine retry QUARANTINE_ID")
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		return sources.RetryQuarantined(ctx, tx, sources.Retry{TenantID: s.tenantID, QuarantineID: id})
	})
}

// singleIDArg expects exactly one argument, a UUID; usage is the error shown otherwise.
func singleIDArg(args []string, usage string) (uuid.UUID, error) {
	if len(args) != 1 {
		return uuid.Nil, errors.New(usage)
	}
	id, err := uuid.Parse(args[0])
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s: %w", usage, err)
	}
	return id, nil
}

// printTable writes aligned columns to stdout.
func printTable(header []string, rows [][]string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, tabPadding, ' ', 0)
	fmt.Fprintln(w, strings.Join(header, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

func timeOrDash(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func textOrDash(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}
