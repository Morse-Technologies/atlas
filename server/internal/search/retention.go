package search

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/search/searchdb"
)

const hoursPerDay = 24

// PurgeExpired erases the questions and answers older than the organization's retention, with their citations
// and candidates, and the conversations left empty. The audit log is not touched: it keeps only ids and outcomes.
func (s *Service) PurgeExpired(ctx context.Context, tenant uuid.UUID, now time.Time) error {
	return s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		settings, err := access.LoadSettings(ctx, tx)
		if err != nil {
			return err
		}
		cutoff := now.Add(-time.Duration(settings.RetentionDays) * hoursPerDay * time.Hour)
		return eraseBefore(ctx, searchdb.New(tx), cutoff)
	})
}

func eraseBefore(ctx context.Context, q *searchdb.Queries, cutoff time.Time) error {
	answers, err := q.DeleteAnswersBefore(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("delete old answers: %w", err)
	}
	conversations, err := q.DeleteEmptyConversationsBefore(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("delete empty conversations: %w", err)
	}
	slog.InfoContext(ctx, "erased answers past retention", "answers", answers, "conversations", conversations)
	return nil
}
