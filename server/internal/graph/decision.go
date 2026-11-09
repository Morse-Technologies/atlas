package graph

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// decided is a decision and who made it.
type decided struct {
	who Actor
	d   Decision
}

func (s *Service) recordDecision(ctx context.Context, tx pgx.Tx, in decided) error {
	return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Actor: in.who.UserID, Action: "edge.decided", Subject: "edge:" + in.d.EdgeID, Detail: "status=" + in.d.Status})
}

func notFoundOr(err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return fmt.Errorf("%s: %w", what, errs.ErrNotFound)
}
