package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/sources"
)

// Enrich turns the parsed document into blocks and makes the version current.
// Part 2 adds chunking, metadata and entities to this job (ATLAS.md §7.2).
func (m *Module) Enrich(ctx context.Context, args events.VersionEnrich) error {
	v, err := m.loadVersion(ctx, args.TenantID, args.VersionID)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(m.Blobs.DerivedPath(parseKey(v.row.ContentHash)))
	if err != nil {
		return fmt.Errorf("read parsed document: %w", err)
	}
	parsed, err := Normalize(raw)
	if err != nil {
		return m.quarantineIfPermanent(ctx, failedVersion{v: v, stage: StepEnrich, err: err})
	}
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		return storeEnriched(ctx, tx, enriched{blocks: savedBlocks{v: v, parsed: parsed}, args: args})
	})
}

// enriched is a parsed version ready to be stored.
type enriched struct {
	blocks savedBlocks
	args   events.VersionEnrich
}

func storeEnriched(ctx context.Context, tx pgx.Tx, e enriched) error {
	if err := saveBlocks(ctx, tx, e.blocks); err != nil {
		return err
	}
	if err := sources.RecordStep(ctx, tx, e.blocks.v.step(StepEnrich, sources.StepDone)); err != nil {
		return err
	}
	return jobs.Enqueue(ctx, tx, events.VersionChunk{TenantID: e.args.TenantID, VersionID: e.args.VersionID})
}

type savedBlocks struct {
	v      version
	parsed Parsed
}

// saveBlocks replaces the version's blocks and switches the document to this version in one transaction.
func saveBlocks(ctx context.Context, tx pgx.Tx, s savedBlocks) error {
	q := documentsdb.New(tx)
	if err := q.DeleteBlocks(ctx, s.v.row.ID); err != nil {
		return fmt.Errorf("delete blocks: %w", err)
	}
	params, err := blockParams(s)
	if err != nil {
		return err
	}
	if err := q.InsertBlocks(ctx, params); err != nil {
		return fmt.Errorf("insert blocks: %w", err)
	}
	return makeCurrent(ctx, q, s)
}

func blockParams(s savedBlocks) (documentsdb.InsertBlocksParams, error) {
	p := documentsdb.InsertBlocksParams{VersionID: s.v.row.ID}
	for i, b := range s.parsed.Blocks {
		box, err := boxJSON(b.Box)
		if err != nil {
			return p, err
		}
		p.Ords = append(p.Ords, int32(i))
		p.Kinds = append(p.Kinds, b.Kind)
		p.Pages = append(p.Pages, int32(b.Page))
		p.Bboxes = append(p.Bboxes, box)
		p.Texts = append(p.Texts, b.Text)
	}
	return p, nil
}

func boxJSON(r *Rect) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return json.Marshal(r)
}

func makeCurrent(ctx context.Context, q *documentsdb.Queries, s savedBlocks) error {
	row := s.v.row
	if err := q.MarkVersionParsed(ctx, documentsdb.MarkVersionParsedParams{ID: row.ID, Pages: new(int32(s.parsed.Pages))}); err != nil {
		return fmt.Errorf("mark parsed: %w", err)
	}
	if err := q.SupersedeOtherVersions(ctx, documentsdb.SupersedeOtherVersionsParams{DocumentID: row.DocumentID, CurrentID: row.ID}); err != nil {
		return fmt.Errorf("supersede: %w", err)
	}
	return q.SetCurrentVersion(ctx, documentsdb.SetCurrentVersionParams{DocumentID: row.DocumentID, VersionID: &row.ID})
}
