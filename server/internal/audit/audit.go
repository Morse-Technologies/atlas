// Package audit owns the append-only, hash-chained record of what happened (ATLAS.md §15).
// Other modules call Record inside the transaction of the change they report: if the event is not in the
// log, the change did not happen.
package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit/auditdb"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Event is one stored record.
type Event struct {
	ID       int64
	At       time.Time
	ActorID  *uuid.UUID // nil: the system did it
	Action   string
	Subject  string
	Detail   string
	PrevHash string
	Hash     string
}

// Entry is what a caller reports. Keep personal data out of Subject and Detail: the log cannot be edited.
type Entry struct {
	At      time.Time
	Actor   uuid.UUID // uuid.Nil: the system
	Action  string
	Subject string
	Detail  string
}

// Record appends an event to the chain of the tenant that tx is scoped to. Writers of one tenant queue on an
// advisory lock until their transaction ends, so the chain stays linear.
func Record(ctx context.Context, tx pgx.Tx, entry Entry) error {
	if entry.At.IsZero() || entry.Action == "" {
		return fmt.Errorf("audit: an entry needs a time and an action: %w", errs.ErrInvalid)
	}
	q := auditdb.New(tx)
	prev, err := lockAndReadTip(ctx, q)
	if err != nil {
		return err
	}
	id, err := q.NextEventID(ctx)
	if err != nil {
		return fmt.Errorf("audit: next id: %w", err)
	}
	// Postgres keeps microseconds; hashing the same precision lets Verify recompute the hash later.
	event := Event{ID: id, At: entry.At.UTC().Truncate(time.Microsecond), ActorID: actorPtr(entry.Actor),
		Action: entry.Action, Subject: entry.Subject, Detail: entry.Detail, PrevHash: prev}
	if event.Hash, err = hashOf(prev, event); err != nil {
		return err
	}
	return insert(ctx, q, event)
}

func lockAndReadTip(ctx context.Context, q *auditdb.Queries) (string, error) {
	tenant, err := q.CurrentTenant(ctx)
	if err != nil {
		return "", fmt.Errorf("audit: no tenant on this transaction: %w", errs.ErrInvalid)
	}
	if err := q.LockChain(ctx, tenant); err != nil {
		return "", fmt.Errorf("audit: lock chain: %w", err)
	}
	prev, err := q.LastHash(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return GenesisHash, nil
	}
	if err != nil {
		return "", fmt.Errorf("audit: last hash: %w", err)
	}
	return prev, nil
}

func actorPtr(actor uuid.UUID) *uuid.UUID {
	if actor == uuid.Nil {
		return nil
	}
	return &actor
}

func insert(ctx context.Context, q *auditdb.Queries, e Event) error {
	err := q.InsertEvent(ctx, auditdb.InsertEventParams{ID: e.ID, At: e.At, ActorID: e.ActorID,
		Action: e.Action, Subject: e.Subject, Detail: e.Detail, PrevHash: e.PrevHash, Hash: e.Hash})
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

func fromRow(r auditdb.AuditEvent) Event {
	return Event{ID: r.ID, At: r.At, ActorID: r.ActorID, Action: r.Action, Subject: r.Subject,
		Detail: r.Detail, PrevHash: r.PrevHash, Hash: r.Hash}
}
