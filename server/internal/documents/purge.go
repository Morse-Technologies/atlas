package documents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
)

// orphanGrace is how long deleted content and unreferenced files are kept before they are erased. It covers a file
// that comes back, and a blob stored a moment before the version that references it is recorded.
const orphanGrace = 24 * time.Hour

// PurgeDeleted erases what only deleted documents needed: their blocks, passages and vectors, and the originals
// and derived files no document in force references. The two steps are independent; both are tried.
func (m *Module) PurgeDeleted(ctx context.Context, tenant uuid.UUID, now time.Time) error {
	cutoff := now.Add(-orphanGrace)
	return errors.Join(m.purgeDeletedContent(ctx, tenant, cutoff), m.purgeUnreferencedFiles(ctx, tenant, cutoff))
}

func (m *Module) purgeDeletedContent(ctx context.Context, tenant uuid.UUID, cutoff time.Time) error {
	return m.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		q := documentsdb.New(tx)
		blocks, err := q.DeleteBlocksOfDocumentsDeletedBefore(ctx, &cutoff)
		if err != nil {
			return fmt.Errorf("delete blocks of deleted documents: %w", err)
		}
		chunks, err := q.DeleteChunksOfDocumentsDeletedBefore(ctx, &cutoff)
		if err != nil {
			return fmt.Errorf("delete chunks of deleted documents: %w", err)
		}
		slog.InfoContext(ctx, "erased the content of deleted documents", "blocks", blocks, "chunks", chunks)
		return nil
	})
}

func (m *Module) purgeUnreferencedFiles(ctx context.Context, tenant uuid.UUID, cutoff time.Time) error {
	live, err := m.liveContentHashes(ctx, tenant)
	if err != nil {
		return err
	}
	stored, err := m.Blobs.ListBlobs(tenant)
	if err != nil {
		return err
	}
	var failures []error
	for _, blob := range stored {
		if !live[blob.Hash] && blob.ModTime.Before(cutoff) {
			failures = append(failures, m.removeFiles(tenant, blob.Hash))
		}
	}
	return errors.Join(failures...)
}

func (m *Module) removeFiles(tenant uuid.UUID, hash string) error {
	return errors.Join(m.Blobs.RemoveBlob(tenant, hash), m.Blobs.RemoveDerived(hash))
}

func (m *Module) liveContentHashes(ctx context.Context, tenant uuid.UUID) (map[string]bool, error) {
	var hashes []string
	err := m.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		var listErr error
		hashes, listErr = documentsdb.New(tx).ListLiveContentHashes(ctx)
		return listErr
	})
	if err != nil {
		return nil, fmt.Errorf("list live content hashes: %w", err)
	}
	live := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		live[h] = true
	}
	return live, nil
}
