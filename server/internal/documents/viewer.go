package documents

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/formats"
)

const pdfMIME = "application/pdf"

// ViewerFile is the PDF the viewer shows for one version. The caller closes it.
type ViewerFile struct {
	*os.File
	Name    string
	ModTime time.Time
}

// FileRequest names a version of a document.
type FileRequest struct {
	DocumentID uuid.UUID
	VersionID  uuid.UUID
}

// OpenFile opens the PDF of a version: the original if it is a PDF, otherwise its rendering.
// Formats that are not rendered to PDF (spreadsheets) have no viewer file and answer not found.
func (l *Library) OpenFile(ctx context.Context, who Actor, req FileRequest) (ViewerFile, error) {
	var row documentsdb.GetVersionForViewerRow
	err := l.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var getErr error
		row, getErr = documentsdb.New(tx).GetVersionForViewer(ctx, documentsdb.GetVersionForViewerParams{ID: req.VersionID, DocumentID: req.DocumentID})
		return db.NotFoundIfNoRows(getErr, "version")
	})
	if err != nil {
		return ViewerFile{}, err
	}
	path, err := l.viewerPath(who.TenantID, row)
	if err != nil {
		return ViewerFile{}, err
	}
	return openPDF(path, row.ExternalID)
}

func (l *Library) viewerPath(tenant uuid.UUID, row documentsdb.GetVersionForViewerRow) (string, error) {
	if row.Mime == pdfMIME {
		return l.Blobs.BlobPath(tenant, row.ContentHash), nil
	}
	if format, ok := formats.ByMIME(row.Mime); ok && format.RenderToPDF {
		return l.Blobs.DerivedPath(blobs.DerivedKey{Hash: row.ContentHash, Stage: renderStage, Version: renderVersion}), nil
	}
	return "", fmt.Errorf("version has no PDF to show: %w", errs.ErrNotFound)
}

func openPDF(path, name string) (ViewerFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return ViewerFile{}, fmt.Errorf("the file is not available yet: %w", errs.ErrNotFound)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return ViewerFile{}, fmt.Errorf("stat viewer file: %w", err)
	}
	return ViewerFile{File: f, Name: name, ModTime: info.ModTime()}, nil
}
