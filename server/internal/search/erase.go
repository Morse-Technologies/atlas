package search

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/search/searchdb"
)

// Register adds the search jobs to a worker process.
func (s *Service) Register(workers *river.Workers) {
	jobs.AddTenantJob(workers, jobs.Job[events.UserDeleted]{Handle: s.OnUserDeleted})
}

// OnUserDeleted erases the person's conversations, with their questions, answers and citations. The audit log
// keeps only ids and outcomes, so nothing personal is left behind.
func (s *Service) OnUserDeleted(ctx context.Context, args events.UserDeleted) error {
	return s.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		n, err := searchdb.New(tx).EraseUserConversations(ctx, args.UserID)
		if err != nil {
			return fmt.Errorf("erase conversations: %w", err)
		}
		slog.InfoContext(ctx, "erased a deleted account's conversations", "conversations", n)
		return nil
	})
}
