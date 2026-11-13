package documents

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// failedAttempt is a pipeline step of one version that failed in a way retrying might fix.
type failedAttempt struct {
	tenant    uuid.UUID
	version   uuid.UUID
	stage     string
	err       error
	exhausted bool
}

// onStageFailed is the OnFailed hook of a pipeline stage: while River retries, the version shows as retrying;
// when the tries run out, it goes to quarantine so it is not left "processing" for ever.
func onStageFailed[T jobs.TenantArgs](m *Module, stage string, versionOf func(T) uuid.UUID) func(context.Context, jobs.Failed[T]) error {
	return func(ctx context.Context, f jobs.Failed[T]) error {
		return m.handleFailedAttempt(ctx, failedAttempt{tenant: f.Args.Tenant(), version: versionOf(f.Args),
			stage: stage, err: f.Err, exhausted: f.Exhausted})
	}
}

func (m *Module) handleFailedAttempt(ctx context.Context, a failedAttempt) error {
	if !a.exhausted {
		return m.DB.InTenantTx(ctx, a.tenant, func(tx pgx.Tx) error {
			if err := documentsdb.New(tx).MarkVersionRetrying(ctx, a.version); err != nil {
				return fmt.Errorf("mark version retrying: %w", err)
			}
			return nil
		})
	}
	v, err := m.loadVersion(ctx, a.tenant, a.version)
	if err != nil {
		return err
	}
	gaveUp := errs.NewPermanent(errs.ReasonRetriesExhausted, a.err)
	return m.quarantineIfPermanent(ctx, failedVersion{v: v, stage: a.stage, err: gaveUp})
}

// clearRetrying puts a version back to processing once a step succeeds, in that step's transaction.
func clearRetrying(ctx context.Context, tx pgx.Tx, versionID uuid.UUID) error {
	if err := documentsdb.New(tx).ClearVersionRetrying(ctx, versionID); err != nil {
		return fmt.Errorf("clear version retrying: %w", err)
	}
	return nil
}
