package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/morse/atlas/server/internal/platform/errs"
)

var retryCases = map[int]time.Duration{
	1:  2 * time.Second,
	3:  32 * time.Second,
	5:  10 * time.Minute,
	99: 10 * time.Minute,
}

func TestRetryDelayFollowsTheCurve(t *testing.T) {
	for attempt, want := range retryCases {
		if got := RetryDelay(attempt); got != want {
			t.Errorf("RetryDelay(%d) = %s, want %s", attempt, got, want)
		}
	}
}

func TestOutcomeCancelsPermanentAndDomainErrors(t *testing.T) {
	for _, err := range []error{errs.NewPermanent(errs.ReasonCorrupt, errors.New("x")), errs.ErrNotFound} {
		var cancel *river.JobCancelError
		if !errors.As(RetryOrCancel(err), &cancel) {
			t.Errorf("RetryOrCancel(%v) should cancel the job", err)
		}
	}
}

func TestOutcomeRetriesTransientErrors(t *testing.T) {
	err := errs.NewTransient(errors.New("503"))
	if RetryOrCancel(err) != err {
		t.Fatal("transient errors must be returned as is so River retries them")
	}
}

type probeArgs struct{ TenantID uuid.UUID }

func (probeArgs) Kind() string        { return "probe.v1" }
func (a probeArgs) Tenant() uuid.UUID { return a.TenantID }

func TestTenantJobIsCancelledWithoutTenant(t *testing.T) {
	ran := false
	w := &tenantWorker[probeArgs]{job: Job[probeArgs]{Handle: func(context.Context, probeArgs) error { ran = true; return nil }}}
	err := w.Work(context.Background(), &river.Job[probeArgs]{Args: probeArgs{}})
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) || ran {
		t.Fatalf("a job without tenant must be cancelled before it runs, got %v (ran=%v)", err, ran)
	}
}

// failingWorker is a worker whose handler fails with err and records what OnFailed was told.
func failingWorker(err error, seen *[]Failed[probeArgs]) *tenantWorker[probeArgs] {
	return &tenantWorker[probeArgs]{job: Job[probeArgs]{
		Handle: func(context.Context, probeArgs) error { return err },
		OnFailed: func(_ context.Context, f Failed[probeArgs]) error {
			*seen = append(*seen, f)
			return nil
		},
	}}
}

func attemptNumber(attempt int) *river.Job[probeArgs] {
	return &river.Job[probeArgs]{JobRow: &rivertype.JobRow{Attempt: attempt, MaxAttempts: maxAttempts}, Args: probeArgs{TenantID: uuid.New()}}
}

func TestOnFailedIsToldOfExhaustionOnlyOnTheLastAttempt(t *testing.T) {
	var seen []Failed[probeArgs]
	w := failingWorker(errs.NewTransient(errors.New("503")), &seen)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_ = w.Work(context.Background(), attemptNumber(attempt))
	}

	if len(seen) != maxAttempts {
		t.Fatalf("OnFailed ran %d times, want once per attempt (%d)", len(seen), maxAttempts)
	}
	for i, f := range seen {
		if want := i == maxAttempts-1; f.Exhausted != want {
			t.Errorf("attempt %d: Exhausted = %v, want %v", i+1, f.Exhausted, want)
		}
	}
}

func TestOnFailedIsNotCalledForAPermanentError(t *testing.T) {
	var seen []Failed[probeArgs]
	w := failingWorker(errs.NewPermanent(errs.ReasonCorrupt, errors.New("bad")), &seen)

	err := w.Work(context.Background(), attemptNumber(1))

	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) || len(seen) != 0 {
		t.Fatalf("err = %v, OnFailed calls = %d; a permanent error cancels the job and skips the hook", err, len(seen))
	}
}

func TestAFailingHookDoesNotHideTheJobsError(t *testing.T) {
	jobErr := errs.NewTransient(errors.New("503"))
	w := &tenantWorker[probeArgs]{job: Job[probeArgs]{
		Handle:   func(context.Context, probeArgs) error { return jobErr },
		OnFailed: func(context.Context, Failed[probeArgs]) error { return errors.New("db down") },
	}}

	err := w.Work(context.Background(), attemptNumber(1))

	if !errors.Is(err, jobErr) {
		t.Fatalf("err = %v, want the job's own error kept so River still retries it", err)
	}
}

func TestOnFailedIsNotCalledWhenTheJobSucceeds(t *testing.T) {
	var seen []Failed[probeArgs]
	w := failingWorker(nil, &seen)
	if err := w.Work(context.Background(), attemptNumber(1)); err != nil || len(seen) != 0 {
		t.Fatalf("err = %v, OnFailed calls = %d; want a quiet success", err, len(seen))
	}
}
