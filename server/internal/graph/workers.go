package graph

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
)

// nightlyAt is how often the full check runs; every night is enough for rules about dates and ownership.
const nightly = 24 * time.Hour

// Register adds the graph jobs to a worker process.
func (s *Service) Register(workers *river.Workers) {
	jobs.AddTenantJob(workers, jobs.Job[events.VersionIndexed]{Handle: s.OnVersionIndexed})
	jobs.AddTenantJob(workers, jobs.Job[events.DocumentChanged]{Handle: s.OnDocumentChanged})
	jobs.AddTenantJob(workers, jobs.Job[events.AlertsEvaluate]{Handle: s.onEvaluate})
	river.AddWorker(workers, river.WorkFunc(func(ctx context.Context, _ *river.Job[events.AlertsNightly]) error {
		return s.EnqueueNightly(ctx)
	}))
}

func (s *Service) onEvaluate(ctx context.Context, args events.AlertsEvaluate) error {
	return s.Evaluate(ctx, args.TenantID)
}

// PeriodicJobs are the recurring jobs of this module.
func PeriodicJobs() []*river.PeriodicJob {
	tick := func() (river.JobArgs, *river.InsertOpts) { return events.AlertsNightly{}, nil }
	return []*river.PeriodicJob{river.NewPeriodicJob(river.PeriodicInterval(nightly), tick, &river.PeriodicJobOpts{RunOnStart: false})}
}

// EnqueueNightly queues a full check for every tenant. One tenant's failure does not stop the others.
func (s *Service) EnqueueNightly(ctx context.Context) error {
	tenants, err := access.ListTenants(ctx, s.DB.Pool)
	if err != nil {
		return err
	}
	var failures []error
	for _, t := range tenants {
		failures = append(failures, s.enqueueEvaluation(ctx, t.ID))
	}
	return errors.Join(failures...)
}

func (s *Service) enqueueEvaluation(ctx context.Context, tenant uuid.UUID) error {
	return s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		return jobs.Enqueue(ctx, tx, events.AlertsEvaluate{TenantID: tenant})
	})
}
