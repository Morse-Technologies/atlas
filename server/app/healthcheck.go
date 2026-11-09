package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/morse/atlas/server/internal/platform/config"
)

const healthTimeout = 3 * time.Second

// runHealthcheck asks the local API whether it is alive. The image has no shell or curl, so Compose uses this
// as its container health check: it exits 0 when /healthz answers 200.
func runHealthcheck(ctx context.Context, _ []string) error {
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
