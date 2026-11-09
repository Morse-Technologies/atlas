package audit

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"
	"github.com/morse/atlas/server/internal/audit/auditdb"
	"github.com/morse/atlas/server/internal/platform/db"
)

// verifyBatch is how many events Verify reads at a time, so a long log never sits in memory at once.
const verifyBatch = 1000

// Service reads and checks the log of one tenant at a time.
type Service struct{ DB *db.DB }

// Page selects events older than Before (0: from the newest). Size is the page size.
type Page struct {
	Before int64
	Size   int32
}

// Verification is the result of walking the chain.
type Verification struct {
	OK       bool
	Checked  int64
	BrokenAt *int64 // id of the first event whose link or hash does not match
}

// List returns one page, newest first, and the cursor of the next page (0: there is none).
func (s *Service) List(ctx context.Context, tenant uuid.UUID, page Page) ([]Event, int64, error) {
	var events []Event
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		before := page.Before
		if before == 0 {
			before = math.MaxInt64
		}
		rows, err := auditdb.New(tx).ListEventsNewestFirst(ctx, auditdb.ListEventsNewestFirstParams{Before: before, PageSize: page.Size + 1})
		for _, r := range rows {
			events = append(events, fromRow(r))
		}
		return err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("audit: list: %w", err)
	}
	if int32(len(events)) <= page.Size {
		return events, 0, nil
	}
	return events[:page.Size], events[page.Size-1].ID, nil
}

// Verify walks the whole chain from the first event and reports where it first breaks.
func (s *Service) Verify(ctx context.Context, tenant uuid.UUID) (Verification, error) {
	result := Verification{OK: true}
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		var walkErr error
		result, walkErr = walk(ctx, auditdb.New(tx))
		return walkErr
	})
	return result, err
}

func walk(ctx context.Context, q *auditdb.Queries) (Verification, error) {
	result, prev, after := Verification{OK: true}, GenesisHash, int64(0)
	for {
		rows, err := q.ListEventsOldestFirst(ctx, auditdb.ListEventsOldestFirstParams{After: after, PageSize: verifyBatch})
		if err != nil {
			return result, fmt.Errorf("audit: verify: %w", err)
		}
		for _, r := range rows {
			e := fromRow(r)
			if !linkIsValid(prev, e) {
				return Verification{Checked: result.Checked, BrokenAt: &e.ID}, nil
			}
			prev, after = e.Hash, e.ID
			result.Checked++
		}
		if len(rows) < verifyBatch {
			return result, nil
		}
	}
}
