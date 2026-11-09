//go:build integration

package idempotency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/idempotency"
)

func newStore(t *testing.T) (*idempotency.Store, idempotency.Request) {
	t.Helper()
	d := dbtest.Start(t)
	store := idempotency.NewStore(d)
	return store, idempotency.Request{Tenant: dbtest.NewTenant(t, d), User: uuid.New(), Key: "k", Hash: "h1"}
}

func TestFirstRequestRunsAndTheRetryGetsTheSameAnswer(t *testing.T) {
	ctx := context.Background()
	store, req := newStore(t)
	if replay, err := store.Begin(ctx, req); replay != nil || err != nil {
		t.Fatalf("first Begin = %v, %v; want to proceed", replay, err)
	}
	if err := store.Finish(ctx, req, idempotency.Outcome{Status: 201, ContentType: "application/json", Body: []byte(`{"id":1}`)}); err != nil {
		t.Fatal(err)
	}
	replay, err := store.Begin(ctx, req)
	if err != nil || replay == nil || replay.Status != 201 || string(replay.Body) != `{"id":1}` {
		t.Fatalf("retry = %+v, %v; want the first answer", replay, err)
	}
}

func TestARunningRequestAndAReusedKeyAreRefused(t *testing.T) {
	ctx := context.Background()
	store, req := newStore(t)
	_, _ = store.Begin(ctx, req)
	if _, err := store.Begin(ctx, req); !errors.Is(err, idempotency.ErrInProgress) {
		t.Fatalf("second Begin while running = %v, want ErrInProgress", err)
	}
	other := req
	other.Hash = "h2"
	if _, err := store.Begin(ctx, other); !errors.Is(err, idempotency.ErrKeyReused) {
		t.Fatalf("Begin with another request = %v, want ErrKeyReused", err)
	}
}

func TestAnAbandonedKeyIsFreedAfterAWhile(t *testing.T) {
	ctx := context.Background()
	store, req := newStore(t)
	_, _ = store.Begin(ctx, req)
	store.Now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if replay, err := store.Begin(ctx, req); replay != nil || err != nil {
		t.Fatalf("Begin on an abandoned key = %v, %v; want to proceed", replay, err)
	}
}

func TestAKeyIsPerPerson(t *testing.T) {
	ctx := context.Background()
	store, req := newStore(t)
	_, _ = store.Begin(ctx, req)
	someoneElse := req
	someoneElse.User = uuid.New()
	if replay, err := store.Begin(ctx, someoneElse); replay != nil || err != nil {
		t.Fatalf("another person's Begin = %v, %v; keys must not collide across people", replay, err)
	}
}
