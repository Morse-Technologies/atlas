package search

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/search/searchdb"
)

const (
	// commonTermsTTL is how long the list of common words is kept: it changes only as the corpus does.
	commonTermsTTL = 10 * time.Minute
)

// commonTerms remembers the common words of the corpus for a while.
type commonTerms struct {
	mu       sync.Mutex
	loadedAt time.Time
	terms    map[string]bool
}

// commonLexemes returns the words found in many passages. If they cannot be read the search still works, only
// slower on a large corpus, so a failure is logged and the last list (or none) is used.
func (s *Service) commonLexemes(ctx context.Context, tenant db.Scope) map[string]bool {
	if s.CommonShare <= 0 {
		return nil
	}
	s.common.mu.Lock()
	defer s.common.mu.Unlock()
	if s.common.terms != nil && s.Now().Sub(s.common.loadedAt) < commonTermsTTL {
		return s.common.terms
	}
	terms, err := s.readCommonLexemes(ctx, tenant)
	if err != nil {
		slog.WarnContext(ctx, "common words unavailable", "error", err)
		return s.common.terms
	}
	s.common.terms, s.common.loadedAt = terms, s.Now()
	return terms
}

func (s *Service) readCommonLexemes(ctx context.Context, scope db.Scope) (map[string]bool, error) {
	terms := map[string]bool{}
	err := s.DB.InScopeTx(ctx, scope, func(tx pgx.Tx) error {
		rows, err := searchdb.New(tx).ListCommonLexemes(ctx, float32(s.CommonShare))
		for _, r := range rows {
			terms[r] = true
		}
		return err
	})
	return terms, err
}
