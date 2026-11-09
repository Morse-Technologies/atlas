// Package app wires the modules into the roles of the single atlas binary (ATLAS.md §4.1).
// Each subcommand builds only the clients it needs; composition is explicit, without a DI framework.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/morse/atlas/server/internal/platform/config"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/log"
)

// Version is set at build time with -ldflags "-X github.com/morse/atlas/server/app.Version=...".
var Version = "dev"

const (
	exitOK      = 0
	exitFailure = 1
)

type command func(ctx context.Context, args []string) error

var commands = map[string]command{
	"api":         runAPI,
	"worker":      runWorker,
	"watch":       runWatch,
	"migrate":     runMigrate,
	"setup":       runSetup,
	"source":      runSource,
	"quarantine":  runQuarantine,
	"user":        runUser,
	"audit":       runAudit,
	"healthcheck": runHealthcheck,
	"bench":       runBench,
}

// Main runs the subcommand in args and returns the process exit code. SIGINT and SIGTERM cancel ctx.
func Main(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, args); err != nil {
		fmt.Fprintln(os.Stderr, "atlas:", err)
		return exitFailure
	}
	return exitOK
}

var errUsage = errors.New("usage: atlas <" + strings.Join(commandNames(), "|") + "> [flags]")

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return errUsage
	}
	return cmd(ctx, args[1:])
}

func commandNames() []string {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// newLogger builds the process logger from ATLAS_LOG_LEVEL.
func newLogger() (*slog.Logger, error) {
	cfg, err := config.Load[config.Log]()
	if err != nil {
		return nil, err
	}
	return log.New(cfg.Level)
}

// openDB connects as the app role.
func openDB(ctx context.Context) (*db.DB, error) {
	cfg, err := config.Load[config.Database]()
	if err != nil {
		return nil, err
	}
	return db.ConnectPool(ctx, db.PoolConfig{URL: cfg.URL, MaxConns: cfg.MaxConns})
}
