package graph

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/graph/graphdb"
)

// OnVersionIndexed reads the document's current passages, records what they mention, links the document to the
// ones it names or shares a code with, and checks the alert rules about it.
func (s *Service) OnVersionIndexed(ctx context.Context, args events.VersionIndexed) error {
	return s.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		return s.link(ctx, tx, args.DocumentID)
	})
}

// OnDocumentChanged links again and re-checks the rules about a document whose metadata a person corrected: its
// code decides which documents are duplicates of it, its owner and review dates what some rules look at.
func (s *Service) OnDocumentChanged(ctx context.Context, args events.DocumentChanged) error {
	return s.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		return s.link(ctx, tx, args.DocumentID)
	})
}

func (s *Service) link(ctx context.Context, tx pgx.Tx, doc uuid.UUID) error {
	q := graphdb.New(tx)
	code, err := q.GetDocCode(ctx, doc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // the document was deleted or has no current version since the job was queued
	}
	if err != nil {
		return fmt.Errorf("get document code: %w", err)
	}
	mentions, err := recordMentions(ctx, q, doc)
	if err != nil {
		return err
	}
	if err := linkReferences(ctx, q, linking{doc: doc, code: code, mentions: mentions}); err != nil {
		return err
	}
	if err := linkEqualCodes(ctx, q, linking{doc: doc, code: code}); err != nil {
		return err
	}
	return s.evaluate(ctx, tx, scope{document: &doc})
}

// linking is the document being linked, its own code and what it mentions.
type linking struct {
	doc      uuid.UUID
	code     string
	mentions []Mention
}

func recordMentions(ctx context.Context, q *graphdb.Queries, doc uuid.UUID) ([]Mention, error) {
	rows, err := q.ListCurrentTexts(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("list passages: %w", err)
	}
	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = r.Text
	}
	mentions := Extract(texts)
	if err := q.DeleteMentions(ctx, doc); err != nil {
		return nil, fmt.Errorf("delete mentions: %w", err)
	}
	return mentions, storeMentions(ctx, q, found{doc: doc, mentions: mentions})
}

// found is what a document mentions.
type found struct {
	doc      uuid.UUID
	mentions []Mention
}

func storeMentions(ctx context.Context, q *graphdb.Queries, f found) error {
	if len(f.mentions) == 0 {
		return nil
	}
	p := graphdb.InsertMentionsParams{DocumentID: f.doc}
	for _, m := range f.mentions {
		p.EntityTypes, p.Labels, p.Counts = append(p.EntityTypes, m.Type), append(p.Labels, m.Label), append(p.Counts, int32(m.Count))
	}
	if err := q.InsertMentions(ctx, p); err != nil {
		return fmt.Errorf("insert mentions: %w", err)
	}
	return nil
}

// linkReferences links this document to those whose code it names, and those that name its code to it.
func linkReferences(ctx context.Context, q *graphdb.Queries, l linking) error {
	if err := q.DeleteOwnEdges(ctx, graphdb.DeleteOwnEdgesParams{DocumentID: l.doc, ProducedBy: producerCodeRegex}); err != nil {
		return fmt.Errorf("delete references: %w", err)
	}
	targets, err := namedDocuments(ctx, q, l)
	if err != nil {
		return err
	}
	set := edgeSet{kind: edgeReferences, from: repeat(l.doc, len(targets)), to: targets, layer: layerPatterns, producer: producerCodeRegex}
	if err := insertEdges(ctx, q, set); err != nil {
		return err
	}
	return linkIncoming(ctx, q, l)
}

// namedDocuments finds the documents whose code this one mentions.
func namedDocuments(ctx context.Context, q *graphdb.Queries, l linking) ([]uuid.UUID, error) {
	var named []string
	for _, m := range l.mentions {
		if m.Type == EntityCode && m.Label != l.code {
			named = append(named, m.Label)
		}
	}
	rows, err := q.ListDocsByCode(ctx, graphdb.ListDocsByCodeParams{Codes: named, Self: l.doc})
	if err != nil {
		return nil, fmt.Errorf("find named documents: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids, nil
}

// linkIncoming adds the references other documents already made to this one's code.
func linkIncoming(ctx context.Context, q *graphdb.Queries, l linking) error {
	if l.code == "" {
		return nil
	}
	from, err := q.ListDocsMentioningCode(ctx, graphdb.ListDocsMentioningCodeParams{Code: l.code, Self: l.doc})
	if err != nil {
		return fmt.Errorf("find documents naming this code: %w", err)
	}
	return insertEdges(ctx, q, edgeSet{kind: edgeReferences, from: from, to: repeat(l.doc, len(from)), layer: layerPatterns, producer: producerCodeRegex})
}

// linkEqualCodes marks documents with the same code as duplicates of each other, one edge per pair.
func linkEqualCodes(ctx context.Context, q *graphdb.Queries, l linking) error {
	if err := q.DeleteDuplicateEdges(ctx, l.doc); err != nil {
		return fmt.Errorf("delete duplicates: %w", err)
	}
	if l.code == "" {
		return nil
	}
	others, err := q.ListDocsWithSameCode(ctx, graphdb.ListDocsWithSameCodeParams{Code: l.code, Self: l.doc})
	if err != nil {
		return fmt.Errorf("find documents with the same code: %w", err)
	}
	set := edgeSet{kind: edgeDuplicates, layer: layerEqualCodes, producer: producerEqualCode}
	for _, other := range others {
		a, b := orderedPair(l.doc, other)
		set.from, set.to = append(set.from, a), append(set.to, b)
	}
	return insertEdges(ctx, q, set)
}

// orderedPair puts the smaller id first, so a pair of duplicates is one edge whichever document is indexed first.
func orderedPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if bytes.Compare(a[:], b[:]) <= 0 {
		return a, b
	}
	return b, a
}

// edgeSet is edges of one kind between from[i] and to[i].
type edgeSet struct {
	kind     string
	from, to []uuid.UUID
	layer    int32
	producer string
}

func insertEdges(ctx context.Context, q *graphdb.Queries, set edgeSet) error {
	if len(set.from) == 0 {
		return nil
	}
	err := q.InsertEdges(ctx, graphdb.InsertEdgesParams{Srcs: set.from, Dsts: set.to, Kind: set.kind, Layer: set.layer, ProducedBy: set.producer})
	if err != nil {
		return fmt.Errorf("insert %s edges: %w", set.kind, err)
	}
	return nil
}

func repeat(id uuid.UUID, n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = id
	}
	return out
}
