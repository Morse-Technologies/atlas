package maint

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

var errBoom = errors.New("boom")

func recording(name string, ran *[]string, fail error) Purger {
	return Purger{Name: name, Purge: func(context.Context, uuid.UUID, time.Time) error {
		*ran = append(*ran, name)
		return fail
	}}
}

func TestAPurgerThatFailsDoesNotStopTheOthers(t *testing.T) {
	var ran []string
	s := &Service{Now: time.Now, Purgers: []Purger{recording("first", &ran, errBoom), recording("second", &ran, nil)}}

	err := s.RunTenant(context.Background(), uuid.New())

	if len(ran) != 2 {
		t.Fatalf("ran %v, want both purgers", ran)
	}
	if !errors.Is(err, errBoom) || err.Error() != "purge first: boom" {
		t.Fatalf("err = %v, want the first purger's failure named", err)
	}
}

func TestEveryFailureIsReturnedTogether(t *testing.T) {
	var ran []string
	s := &Service{Now: time.Now, Purgers: []Purger{recording("a", &ran, errBoom), recording("b", &ran, errBoom)}}

	err := s.RunTenant(context.Background(), uuid.New())

	if err == nil || err.Error() != "purge a: boom\npurge b: boom" {
		t.Fatalf("err = %q, want both failures", err)
	}
}

func TestPurgersAreGivenTheInjectedClock(t *testing.T) {
	at := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	var got time.Time
	s := &Service{Now: func() time.Time { return at }, Purgers: []Purger{
		{Name: "clock", Purge: func(_ context.Context, _ uuid.UUID, now time.Time) error { got = now; return nil }}}}

	if err := s.RunTenant(context.Background(), uuid.New()); err != nil || !got.Equal(at) {
		t.Fatalf("purger saw %v, %v; want %v", got, err, at)
	}
}

func TestTheCleanupRunsOnceADay(t *testing.T) {
	if got := len(PeriodicJobs()); got != 1 {
		t.Fatalf("%d periodic jobs, want 1", got)
	}
}
