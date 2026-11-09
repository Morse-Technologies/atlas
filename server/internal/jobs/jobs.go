// Package jobs builds the River client and holds the rules every job follows (ATLAS.md §7):
// tenant-scoped arguments, the retry curve, per-queue concurrency and how errors end a job.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Schema is where River keeps its tables (ATLAS.md §5.1).
const Schema = "river"

const (
	maxAttempts = 5
	// rescueStuckAfter must exceed the longest job timeout (convert: 30 min), see ATLAS.md §7.4.
	rescueStuckAfter = 45 * time.Minute
	defaultTimeout   = time.Minute
)

// queueConcurrency follows ATLAS.md §7.2 for the queues that exist in part 1.
var queueConcurrency = map[string]int{
	events.QueueIngest:  8,
	events.QueueConvert: 2,
	events.QueueEnrich:  4,
	events.QueueEmbed:   2,
	events.QueueIndex:   4,
	events.QueueGraph:   2,
	events.QueueMaint:   2,
}

// Client is the River client every role uses: workers run jobs, the API and CLI only insert.
type Client = river.Client[pgx.Tx]

// Setup is what a worker process registers; insert-only clients leave it empty.
type Setup struct {
	Workers  *river.Workers
	Periodic []*river.PeriodicJob
	Logger   *slog.Logger
}

// NewClient builds a River client on the pool. Queues are started only when workers are registered.
func NewClient(pool *pgxpool.Pool, setup Setup) (*Client, error) {
	cfg := &river.Config{
		Schema:               Schema,
		MaxAttempts:          maxAttempts,
		JobTimeout:           defaultTimeout,
		RescueStuckJobsAfter: rescueStuckAfter,
		RetryPolicy:          RetryPolicy{},
		Logger:               setup.Logger,
	}
	if setup.Workers != nil {
		cfg.Workers, cfg.PeriodicJobs, cfg.Queues = setup.Workers, setup.Periodic, queues()
	}
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	return client, nil
}

func queues() map[string]river.QueueConfig {
	out := make(map[string]river.QueueConfig, len(queueConcurrency))
	for name, workers := range queueConcurrency {
		out[name] = river.QueueConfig{MaxWorkers: workers}
	}
	return out
}

// retrySchedule is the backoff of ATLAS.md §7.4. Attempts beyond it reuse the last step.
var retrySchedule = []time.Duration{2 * time.Second, 8 * time.Second, 32 * time.Second, 2 * time.Minute, 10 * time.Minute}

// RetryPolicy implements the fixed backoff curve.
type RetryPolicy struct{}

// NextRetry returns when the job should run again after its latest failure.
func (RetryPolicy) NextRetry(job *rivertype.JobRow) time.Time {
	return time.Now().Add(RetryDelay(job.Attempt))
}

// RetryDelay is the wait after the given attempt number (1-based).
func RetryDelay(attempt int) time.Duration {
	step := min(max(attempt-1, 0), len(retrySchedule)-1)
	return retrySchedule[step]
}

// TenantArgs is implemented by every job that touches tenant data. Requiring it at registration means a
// job without a tenant does not compile; the worker also refuses a zero tenant at run time.
type TenantArgs interface {
	river.JobArgs
	Tenant() uuid.UUID
}

var errMissingTenant = errors.New("job has no tenant id")

// Handler is what a tenant job does with its arguments.
type Handler[T TenantArgs] func(ctx context.Context, args T) error

// Job is a handler and its timeout. A zero timeout uses the client default (defaultTimeout).
type Job[T TenantArgs] struct {
	Handle  Handler[T]
	Timeout time.Duration
}

// AddTenantJob registers a tenant-scoped job. Errors are mapped by RetryOrCancel.
func AddTenantJob[T TenantArgs](workers *river.Workers, job Job[T]) {
	river.AddWorker(workers, &tenantWorker[T]{job: job})
}

type tenantWorker[T TenantArgs] struct {
	river.WorkerDefaults[T]
	job Job[T]
}

func (w *tenantWorker[T]) Timeout(*river.Job[T]) time.Duration { return w.job.Timeout }

func (w *tenantWorker[T]) Work(ctx context.Context, job *river.Job[T]) error {
	if job.Args.Tenant() == uuid.Nil {
		return river.JobCancel(errMissingTenant)
	}
	return RetryOrCancel(w.job.Handle(ctx, job.Args))
}

// RetryOrCancel decides how a failed job ends: permanent and domain errors cancel it (retrying cannot help),
// every other error is returned so River retries it with RetryPolicy.
func RetryOrCancel(err error) error {
	switch errs.Classify(err) {
	case errs.ClassPermanent, errs.ClassDomain:
		return river.JobCancel(err)
	default:
		return err
	}
}
