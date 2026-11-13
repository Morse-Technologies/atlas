package graph

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/graph/graphdb"
	"github.com/morse/atlas/server/internal/platform/db"
)

// maxRelated bounds how many linked documents one document lists.
const maxRelated = 20

// Link is a document connected to another one, and how (references or duplicates).
type Link struct {
	DocumentID uuid.UUID
	Kind       string
}

// RelatedTo returns the documents linked to document by a reference or as a duplicate, in either direction,
// leaving out rejected links. It reads as the person, so a document they cannot read is not among the links;
// the caller still loads each one through the documents module, which applies the same rule.
func (s *Service) RelatedTo(ctx context.Context, who Actor, document uuid.UUID) ([]Link, error) {
	var links []Link
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		rows, err := graphdb.New(tx).ListLinksOf(ctx, graphdb.ListLinksOfParams{DocumentID: document, Top: maxRelated})
		if err != nil {
			return fmt.Errorf("list links: %w", err)
		}
		for _, r := range rows {
			links = append(links, Link{DocumentID: r.Other, Kind: r.Kind})
		}
		return nil
	})
	return links, err
}
