package search

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// wordQuery is a word search: the terms joined by OR, and the sets of passages the person may read.
type wordQuery struct {
	terms string
	acl   []uuid.UUID
}

// wordMatches returns the ids of the best passages for the terms, best first. It calls a database function
// (migration 0015) because under row-level security the word index would not be used otherwise. It is written
// by hand: sqlc does not read the columns of a function that returns a table.
func wordMatches(ctx context.Context, tx pgx.Tx, q wordQuery) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT chunk_id FROM documents.search_words($1::text, $2::uuid[], $3::int)", q.terms, q.acl, rankedPerList)
	if err != nil {
		return nil, fmt.Errorf("search words: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read word match: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
