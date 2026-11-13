package access

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access/accessdb"
)

// PurgeExpired forgets the sessions and sign-in challenges of a tenant that ran out before now.
// Neither is needed after it expires: an expired one is already refused.
func (s *Service) PurgeExpired(ctx context.Context, tenant uuid.UUID, now time.Time) error {
	return s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		q := accessdb.New(tx)
		if err := q.DeleteExpiredSessions(ctx, accessdb.DeleteExpiredSessionsParams{TenantID: tenant, Now: now}); err != nil {
			return fmt.Errorf("delete expired sessions: %w", err)
		}
		if err := q.DeleteExpiredChallenges(ctx, accessdb.DeleteExpiredChallengesParams{TenantID: tenant, Now: now}); err != nil {
			return fmt.Errorf("delete expired challenges: %w", err)
		}
		return nil
	})
}
