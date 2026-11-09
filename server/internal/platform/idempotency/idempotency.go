// Package idempotency makes writes safe to retry (ATLAS.md §10.2). A client sends an Idempotency-Key with a
// write; if the same person repeats the same request with the same key, they get the first answer back
// instead of the change happening twice.
package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/idempotency/platformdb"
)

const (
	keepFor = 24 * time.Hour
	// staleAfter frees a key whose first request never finished (the process died), so retries are not stuck.
	staleAfter = 5 * time.Minute
)

// Retry problems. Both are answered to the client as domain errors.
var (
	ErrInProgress = fmt.Errorf("a request with this key is still running: %w", errs.ErrConflict)
	ErrKeyReused  = fmt.Errorf("this key was used for a different request: %w", errs.ErrInvalid)
)

// Request identifies one write: who sent it, with which key, and a hash of what it asked for.
type Request struct {
	Tenant uuid.UUID
	User   uuid.UUID
	Key    string
	Hash   string
}

// Outcome is the answer to replay.
type Outcome struct {
	Status      int
	ContentType string
	Body        []byte
}

// Store keeps the keys in Postgres, so every API replica sees them.
type Store struct {
	DB  *db.DB
	Now func() time.Time
}

// NewStore returns a Store that reads the real clock.
func NewStore(d *db.DB) *Store { return &Store{DB: d, Now: time.Now} }

// Begin claims the key. It returns an Outcome to replay when this request already finished, and nil when the
// caller must run the request and then call Finish (or Release if it failed).
func (s *Store) Begin(ctx context.Context, r Request) (*Outcome, error) {
	var replay *Outcome
	err := s.DB.InTenantTx(ctx, r.Tenant, func(tx pgx.Tx) error {
		var claimErr error
		replay, claimErr = s.claim(ctx, tx, r)
		return claimErr
	})
	return replay, err
}

func (s *Store) claim(ctx context.Context, tx pgx.Tx, r Request) (*Outcome, error) {
	q := platformdb.New(tx)
	if err := q.PurgeOldKeys(ctx, s.Now().Add(-keepFor)); err != nil {
		return nil, fmt.Errorf("purge idempotency keys: %w", err)
	}
	claimed, err := q.ClaimKey(ctx, platformdb.ClaimKeyParams{UserID: r.User, Key: r.Key, RequestHash: r.Hash})
	if err != nil || claimed == 1 {
		return nil, err
	}
	replay, err := s.existing(ctx, q, r)
	if errors.Is(err, errStale) {
		return nil, s.reclaim(ctx, q, r)
	}
	return replay, err
}

// errStale marks a key whose first request never finished.
var errStale = errors.New("stale idempotency key")

// reclaim frees an abandoned key and takes it for this request, in the same transaction.
func (s *Store) reclaim(ctx context.Context, q *platformdb.Queries, r Request) error {
	if err := s.releaseIn(ctx, q, r); err != nil {
		return err
	}
	_, err := q.ClaimKey(ctx, platformdb.ClaimKeyParams{UserID: r.User, Key: r.Key, RequestHash: r.Hash})
	return err
}

// existing decides what a repeated key means: replay, still running, abandoned, or a different request.
func (s *Store) existing(ctx context.Context, q *platformdb.Queries, r Request) (*Outcome, error) {
	row, err := q.GetKey(ctx, platformdb.GetKeyParams{UserID: r.User, Key: r.Key})
	if err != nil {
		return nil, fmt.Errorf("get idempotency key: %w", err)
	}
	switch {
	case row.RequestHash != r.Hash:
		return nil, ErrKeyReused
	case row.Status != nil:
		return &Outcome{Status: int(*row.Status), ContentType: row.ContentType, Body: row.Body}, nil
	case s.Now().Sub(row.CreatedAt) > staleAfter:
		return nil, errStale
	}
	return nil, ErrInProgress
}

// Finish stores the answer so a retry gets it back.
func (s *Store) Finish(ctx context.Context, r Request, out Outcome) error {
	return s.DB.InTenantTx(ctx, r.Tenant, func(tx pgx.Tx) error {
		err := platformdb.New(tx).FinishKey(ctx, platformdb.FinishKeyParams{UserID: r.User, Key: r.Key,
			Status: ptr(int32(out.Status)), ContentType: out.ContentType, Body: out.Body})
		if err != nil {
			return fmt.Errorf("finish idempotency key: %w", err)
		}
		return nil
	})
}

// Release frees the key after a failure, so the client can retry.
func (s *Store) Release(ctx context.Context, r Request) error {
	return s.DB.InTenantTx(ctx, r.Tenant, func(tx pgx.Tx) error {
		return s.releaseIn(ctx, platformdb.New(tx), r)
	})
}

func (s *Store) releaseIn(ctx context.Context, q *platformdb.Queries, r Request) error {
	if err := q.ReleaseKey(ctx, platformdb.ReleaseKeyParams{UserID: r.User, Key: r.Key}); err != nil {
		return fmt.Errorf("release idempotency key: %w", err)
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
