package sources

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

const (
	// rescanTimeout: listing a share with hundreds of thousands of files can take tens of minutes (§6.1).
	rescanTimeout = 30 * time.Minute
	fetchTimeout  = 5 * time.Minute
	// rescanTick is how often due sources are checked; each source has its own rescan_interval.
	rescanTick = time.Minute
)

// Register adds the sources jobs to a worker process.
func (m *Module) Register(workers *river.Workers) {
	jobs.AddTenantJob(workers, jobs.Job[events.SourceRescan]{Handle: m.Rescan, Timeout: rescanTimeout})
	jobs.AddTenantJob(workers, jobs.Job[events.VersionFetch]{Handle: m.Fetch, Timeout: fetchTimeout})
	// The tick spans tenants: it reads the tenant registry, then scopes each tenant's query by RLS.
	river.AddWorker(workers, river.WorkFunc(func(ctx context.Context, _ *river.Job[events.RescanDueSources]) error {
		return m.EnqueueDueRescans(ctx)
	}))
}

// PeriodicJobs are the recurring jobs of this module.
func PeriodicJobs() []*river.PeriodicJob {
	tick := func() (river.JobArgs, *river.InsertOpts) { return events.RescanDueSources{}, nil }
	return []*river.PeriodicJob{river.NewPeriodicJob(river.PeriodicInterval(rescanTick), tick, &river.PeriodicJobOpts{RunOnStart: true})}
}

// EnqueueDueRescans schedules a scan for every source whose interval has passed, in every tenant.
// One tenant's failure does not stop the others; the errors are returned together.
func (m *Module) EnqueueDueRescans(ctx context.Context) error {
	tenants, err := access.ListTenants(ctx, m.DB.Pool)
	if err != nil {
		return err
	}
	var failures []error
	for _, t := range tenants {
		failures = append(failures, m.enqueueTenantDueRescans(ctx, t.ID))
	}
	return errors.Join(failures...)
}

func (m *Module) enqueueTenantDueRescans(ctx context.Context, tenantID uuid.UUID) error {
	return m.DB.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		due, err := sourcesdb.New(tx).ListDueSources(ctx)
		if err != nil {
			return err
		}
		scans := make([]river.JobArgs, len(due))
		for i, id := range due {
			scans[i] = events.SourceRescan{TenantID: tenantID, SourceID: id}
		}
		return jobs.Enqueue(ctx, tx, scans...)
	})
}
