package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/config"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/metrics"
	"github.com/morse/atlas/server/internal/sources"
)

const (
	jobsGauge       = "atlas_jobs"
	quarantineGauge = "atlas_quarantine_files"
)

// startMetrics publishes the process' metrics on ATLAS_METRICS_ADDR until ctx ends. The numbers are for the
// operator, not for the people using Atlas, so a failure is logged and the role keeps working.
func startMetrics(ctx context.Context, d deps) error {
	cfg, err := config.Load[config.Metrics]()
	if err != nil {
		return err
	}
	addGauges(metrics.Default, d.db)
	server := newMetricsServer(cfg.Addr, metrics.Default)
	go func() {
		if err := serveUntilDone(ctx, server); err != nil {
			slog.Error("metrics server stopped", "addr", cfg.Addr, "error", err)
		}
	}()
	return nil
}

func newMetricsServer(addr string, reg *metrics.Registry) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", reg.Handler())
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
}

// addGauges registers the numbers that are read from the database on each scrape.
func addGauges(reg *metrics.Registry, database *db.DB) {
	reg.AddGauge(metrics.Gauge{Name: jobsGauge, Read: func(ctx context.Context) ([]metrics.Sample, error) {
		return readJobCounts(ctx, database)
	}})
	reg.AddGauge(metrics.Gauge{Name: quarantineGauge, Read: func(ctx context.Context) ([]metrics.Sample, error) {
		return readQuarantineCount(ctx, database)
	}})
}

func readJobCounts(ctx context.Context, database *db.DB) ([]metrics.Sample, error) {
	counts, err := jobs.CountByState(ctx, database.Pool)
	if err != nil {
		return nil, err
	}
	samples := make([]metrics.Sample, len(counts))
	for i, c := range counts {
		samples[i] = metrics.Sample{Labels: metrics.Labels{"queue": c.Queue, "state": c.State}, Value: float64(c.Jobs)}
	}
	return samples, nil
}

// readQuarantineCount adds up the quarantined files of every tenant.
func readQuarantineCount(ctx context.Context, database *db.DB) ([]metrics.Sample, error) {
	tenants, err := access.ListTenants(ctx, database.Pool)
	if err != nil {
		return nil, err
	}
	admin := &sources.Admin{DB: database}
	total := 0
	for _, t := range tenants {
		n, err := admin.QuarantineCount(ctx, t.ID)
		if err != nil {
			return nil, fmt.Errorf("quarantine of a tenant: %w", err)
		}
		total += n
	}
	return []metrics.Sample{{Value: float64(total)}}, nil
}
