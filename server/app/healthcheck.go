package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/morse/atlas/server/internal/platform/config"
)

const (
	healthTimeout = 3 * time.Second
	// heartbeatEvery is how often a running worker touches heartbeatFile; the health check accepts a few missed beats.
	heartbeatEvery  = 30 * time.Second
	heartbeatMaxAge = 3 * heartbeatEvery
	heartbeatFile   = "/tmp/atlas-heartbeat"
	heartbeatMode   = 0o600
)

// Roles the health check understands: the container it runs in.
const (
	roleAPI    = "api"
	roleWorker = "worker"
	roleWatch  = "watch"
)

// runHealthcheck is the container health check: the image has no shell or curl, so Compose runs `atlas healthcheck`.
// It exits 0 when the role it checks is alive:
//
//	--role api (default)  the local API answers 200 on /healthz
//	--role worker         the database answers and the job client has beaten its heart recently
//	--role watch          the database answers
func runHealthcheck(ctx context.Context, args []string) error {
	role, err := parseHealthRole(args)
	if err != nil {
		return err
	}
	switch role {
	case roleWorker:
		return errors.Join(checkDatabase(ctx), checkHeartbeat(heartbeatFile, time.Now()))
	case roleWatch:
		return checkDatabase(ctx)
	default:
		return checkAPI(ctx)
	}
}

func parseHealthRole(args []string) (string, error) {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	role := flags.String("role", roleAPI, "api, worker or watch")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if *role != roleAPI && *role != roleWorker && *role != roleWatch {
		return "", fmt.Errorf("healthcheck: unknown role %q (api, worker or watch)", *role)
	}
	return *role, nil
}

func checkAPI(ctx context.Context) error {
	cfg, err := config.Load[config.HTTP]()
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return fmt.Errorf("ATLAS_HTTP_ADDR: %w", err)
	}
	return getOK(ctx, "http://127.0.0.1:"+port+"/healthz")
}

func checkDatabase(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	d, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer d.Pool.Close()
	return d.Pool.Ping(ctx)
}

// checkHeartbeat fails when the file written by beatWhileRunning is missing or older than heartbeatMaxAge.
func checkHeartbeat(path string, now time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("worker heartbeat: %w", err)
	}
	if age := now.Sub(info.ModTime()); age > heartbeatMaxAge {
		return fmt.Errorf("worker heartbeat is %s old (limit %s)", age.Round(time.Second), heartbeatMaxAge)
	}
	return nil
}

// beatWhileRunning touches path now and every heartbeatEvery until ctx ends or the job client stops.
func beatWhileRunning(ctx context.Context, stopped <-chan struct{}, path string) {
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	for {
		if err := os.WriteFile(path, nil, heartbeatMode); err != nil {
			slog.Warn("worker heartbeat not written", "path", path, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-stopped:
			return
		case <-ticker.C:
		}
	}
}

func getOK(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz answered %d", resp.StatusCode)
	}
	return nil
}
