package sources

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

// AccessSetOf returns the ACL set of a source's documents, nil if every signed-in person may read them.
func AccessSetOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*uuid.UUID, error) {
	set, err := sourcesdb.New(tx).GetSourceACLSet(ctx, id)
	if err != nil {
		return nil, db.NotFoundIfNoRows(err, "source")
	}
	return set, nil
}

// ReadersChange is who may read a source from now on; Readers nil means every signed-in person.
type ReadersChange struct {
	ID      uuid.UUID
	Readers []uuid.UUID
}

// SetReaders restricts a source to some people, or opens it to everyone again. The documents follow in the same
// transaction, so nothing a person may no longer read stays readable.
func (a *Admin) SetReaders(ctx context.Context, who Actor, c ReadersChange) (Source, error) {
	var out Source
	err := a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		if err := a.changeReaders(ctx, tx, c); err != nil {
			return err
		}
		var err error
		if out, err = get(ctx, tx, c.ID); err != nil {
			return err
		}
		return a.record(ctx, tx, change{by: who.UserID, action: "source.access_changed", subject: sourceSubject(c.ID), detail: fmt.Sprintf("readers=%d", len(c.Readers))})
	})
	return out, err
}

func (a *Admin) changeReaders(ctx context.Context, tx pgx.Tx, c ReadersChange) error {
	current, err := AccessSetOf(ctx, tx, c.ID)
	if err != nil {
		return err
	}
	set, err := access.SetReaders(ctx, tx, access.ReadersChange{Current: current, Readers: c.Readers})
	if err != nil {
		return err
	}
	if _, err := sourcesdb.New(tx).SetSourceACLSet(ctx, sourcesdb.SetSourceACLSetParams{ID: c.ID, AclSetID: set}); err != nil {
		return fmt.Errorf("set source access: %w", err)
	}
	if a.ApplyAccess == nil {
		return nil
	}
	return a.ApplyAccess(ctx, tx, AccessChange{SourceID: c.ID, SetID: set})
}
