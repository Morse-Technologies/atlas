// Package documents owns documents, versions and their parsed blocks (ATLAS.md §5.1, §8).
// It consumes version.stored.v1 from sources, renders and parses the file, and stores blocks.
package documents

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/models/docling"
	"github.com/morse/atlas/server/internal/platform/models/gotenberg"
	"github.com/morse/atlas/server/internal/sources"
)

// Module holds what the documents jobs need.
type Module struct {
	DB        *db.DB
	Blobs     *blobs.Store
	Docling   *docling.Client
	Gotenberg *gotenberg.Client
	Logger    *slog.Logger
	// Tokens and Embedder are optional: without them chunks are sized by an estimate and only searched by words.
	Tokens     TokenCounter
	Embedder   Embedder
	EmbedModel string
}

// OnVersionStored records the document and its version and schedules conversion.
func (m *Module) OnVersionStored(ctx context.Context, args events.VersionStored) error {
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		q := documentsdb.New(tx)
		acl, err := sources.AccessSetOf(ctx, tx, args.SourceID)
		if err != nil {
			return err
		}
		docID, err := q.UpsertDocument(ctx, documentsdb.UpsertDocumentParams{
			SourceID: args.SourceID, ExternalID: args.ExternalID, Title: titleFromPath(args.ExternalID), AclSetID: acl,
		})
		if err != nil {
			return fmt.Errorf("upsert document: %w", err)
		}
		versionID, err := q.UpsertVersion(ctx, documentsdb.UpsertVersionParams{
			DocumentID: docID, ContentHash: args.ContentHash, Mime: args.MIME, Size: args.Size,
		})
		if err != nil {
			return fmt.Errorf("upsert version: %w", err)
		}
		return jobs.Enqueue(ctx, tx, events.VersionConvert{TenantID: args.TenantID, VersionID: versionID})
	})
}

// titleFromPath is the provisional title until metadata extraction (part 2) reads the real one.
func titleFromPath(externalID string) string {
	base := path.Base(externalID)
	return strings.TrimSuffix(base, path.Ext(base))
}

// OnDocumentDeleted hides the document from search at once; blobs are purged later (ATLAS.md §10.3).
func (m *Module) OnDocumentDeleted(ctx context.Context, args events.DocumentDeleted) error {
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		q := documentsdb.New(tx)
		if err := retireChunks(ctx, q, args); err != nil {
			return err
		}
		return q.MarkDocumentDeleted(ctx, documentsdb.MarkDocumentDeletedParams{
			SourceID: args.SourceID, ExternalID: args.ExternalID,
		})
	})
}

// OnDocumentMoved keeps the document, its corrections and history under the new path (ATLAS.md §6.1).
func (m *Module) OnDocumentMoved(ctx context.Context, args events.DocumentMoved) error {
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		return documentsdb.New(tx).MoveDocument(ctx, documentsdb.MoveDocumentParams{
			SourceID: args.SourceID, OldExternalID: args.OldExternalID,
			NewExternalID: args.NewExternalID, Title: titleFromPath(args.NewExternalID),
		})
	})
}

// version is a version loaded for processing, with what sources needs to log steps about it.
type version struct {
	tenantID uuid.UUID
	row      documentsdb.GetVersionRow
}

func (m *Module) loadVersion(ctx context.Context, tenantID, versionID uuid.UUID) (version, error) {
	v := version{tenantID: tenantID}
	err := m.DB.InTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		row, err := documentsdb.New(tx).GetVersion(ctx, versionID)
		v.row = row
		return err
	})
	if err != nil {
		return v, db.NotFoundIfNoRows(err, "version "+versionID.String())
	}
	return v, nil
}

// retireChunks stops search from finding a deleted document at once.
func retireChunks(ctx context.Context, q *documentsdb.Queries, args events.DocumentDeleted) error {
	if err := q.RetireDocumentChunks(ctx, documentsdb.RetireDocumentChunksParams{SourceID: args.SourceID, ExternalID: args.ExternalID}); err != nil {
		return fmt.Errorf("retire chunks: %w", err)
	}
	err := q.RetireDocumentEmbeddings(ctx, documentsdb.RetireDocumentEmbeddingsParams{SourceID: args.SourceID, ExternalID: args.ExternalID})
	if err != nil {
		return fmt.Errorf("retire embeddings: %w", err)
	}
	return nil
}
