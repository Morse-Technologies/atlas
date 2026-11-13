// Package maint runs the daily cleanup: it asks each module to forget what has outlived its retention.
// It owns no tables; every module purges its own data and maint only schedules the work (ATLAS.md §5.1).
package maint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
)

// maintEvery is how often the cleanup runs; once a day is enough for retention measured in days.
const maintEvery = 24 * time.Hour

// Purger forgets what one module no longer needs for a tenant, as of now.
type Purger struct {
	Name  string
	Purge func(ctx context.Context, tenant uuid.UUID, now time.Time) error
}

// Service schedules the cleanup and runs the purgers of a tenant.
type Service struct {
	DB      *db.DB
	Purgers []Purger
	Now     func() time.Time
}

// Register adds the cleanup jobs to a worker process.
func (s *Service) Register(workers *river.Workers) {
	jobs.AddTenantJob(workers, jobs.Job[events.MaintTenant]{Handle: s.onTenant})
	river.AddWorker(workers, river.WorkFunc(func(ctx context.Context, _ *river.Job[events.MaintDue]) error {
		return s.EnqueueDue(ctx)
	}))
}

// PeriodicJobs are the recurring jobs of this module.
func PeriodicJobs() []*river.PeriodicJob {
	tick := func() (river.JobArgs, *river.InsertOpts) { return events.MaintDue{}, nil }
	return []*river.PeriodicJob{river.NewPeriodicJob(river.PeriodicInterval(maintEvery), tick, &river.PeriodicJobOpts{RunOnStart: false})}
}

// EnqueueDue queues the cleanup of every tenant. One tenant's failure does not stop the others.
func (s *Service) EnqueueDue(ctx context.Context) error {
	tenants, err := access.ListTenants(ctx, s.DB.Pool)
	if err != nil {
		return err
	}
	var failures []error
	for _, t := range tenants {
		failures = append(failures, s.enqueueTenant(ctx, t.ID))
	}
	return errors.Join(failures...)
}

func (s *Service) enqueueTenant(ctx context.Context, tenant uuid.UUID) error {
	return s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		return jobs.Enqueue(ctx, tx, events.MaintTenant{TenantID: tenant})
	})
}

func (s *Service) onTenant(ctx context.Context, args events.MaintTenant) error {
	return s.RunTenant(ctx, args.TenantID)
}

// RunTenant runs every purger. A purger that fails does not stop the others; the failures come back together,
// so the job is retried and the log says which purgers failed.
func (s *Service) RunTenant(ctx context.Context, tenant uuid.UUID) error {
	now := s.Now()
	var failures []error
	for _, p := range s.Purgers {
		if err := p.Purge(ctx, tenant, now); err != nil {
			failures = append(failures, fmt.Errorf("purge %s: %w", p.Name, err))
		}
	}
	return errors.Join(failures...)
}
