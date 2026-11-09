package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

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
