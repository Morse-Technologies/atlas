package access

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// ReadersChange says who may read what a source holds. Readers nil means every signed-in person, and removes
// the source's own set; a list restricts the source to those people.
type ReadersChange struct {
	Current *uuid.UUID // the source's set today, nil if everyone
	Readers []uuid.UUID
}

// SetReaders applies a change and returns the source's set afterwards (nil: everyone).
func SetReaders(ctx context.Context, tx pgx.Tx, c ReadersChange) (*uuid.UUID, error) {
	q := accessdb.New(tx)
	if c.Readers == nil {
		return nil, dropSet(ctx, q, c.Current)
	}
	if len(c.Readers) == 0 {
		return nil, fmt.Errorf("a restricted source needs at least one reader: %w", errs.ErrInvalid)
	}
	if err := readersExist(ctx, tx, c.Readers); err != nil {
		return nil, err
	}
	if c.Current == nil {
		id, err := q.CreateReadersACLSet(ctx, c.Readers)
		return &id, err
	}
	_, err := q.SetACLSetReaders(ctx, accessdb.SetACLSetReadersParams{ID: *c.Current, Readers: c.Readers})
	return c.Current, err
}

func dropSet(ctx context.Context, q *accessdb.Queries, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	return q.DeleteACLSet(ctx, *id)
}

func readersExist(ctx context.Context, tx pgx.Tx, readers []uuid.UUID) error {
	for _, id := range readers {
		ok, err := UserExists(ctx, tx, id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("reader %s is not a user of this organization: %w", id, errs.ErrInvalid)
		}
	}
	return nil
}

// ReadersOf lists the people a set allows; nil for no set (everyone).
func ReadersOf(ctx context.Context, tx pgx.Tx, set *uuid.UUID) ([]uuid.UUID, error) {
	if set == nil {
		return nil, nil
	}
	readers, err := accessdb.New(tx).GetACLSetReaders(ctx, *set)
	if err != nil {
		return nil, fmt.Errorf("get readers: %w", err)
	}
	return readers, nil
}
