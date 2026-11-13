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

// ViewerFile is a stored file served for one version: the PDF the viewer shows, or the original to download.
// The caller closes it.
type ViewerFile struct {
	*os.File
	Name    string
	Mime    string
	ModTime time.Time
}

// HasPreview reports whether the viewer can show a version of this type: a PDF, or a format rendered to PDF.
func HasPreview(mime string) bool {
	if mime == pdfMIME {
		return true
	}
	format, ok := formats.ByMIME(mime)
	return ok && format.RenderToPDF
}

// FileRequest names a version of a document.
type FileRequest struct {
	DocumentID uuid.UUID
	VersionID  uuid.UUID
}

// OpenFile opens the PDF of a version: the original if it is a PDF, otherwise its rendering.
// Formats that are not rendered to PDF (spreadsheets) have no viewer file and answer not found.
func (l *Library) OpenFile(ctx context.Context, who Actor, req FileRequest) (ViewerFile, error) {
	row, err := l.versionForViewer(ctx, who, req)
	if err != nil {
		return ViewerFile{}, err
	}
	path, err := l.viewerPath(who.TenantID, row)
	if err != nil {
		return ViewerFile{}, err
	}
	return openStored(path, row.ExternalID, pdfMIME)
}

// OpenOriginal opens the file as it was read, for people to download. The same permissions apply as for the viewer.
func (l *Library) OpenOriginal(ctx context.Context, who Actor, req FileRequest) (ViewerFile, error) {
	row, err := l.versionForViewer(ctx, who, req)
	if err != nil {
		return ViewerFile{}, err
	}
	return openStored(l.Blobs.BlobPath(who.TenantID, row.ContentHash), row.ExternalID, row.Mime)
}

func (l *Library) versionForViewer(ctx context.Context, who Actor, req FileRequest) (documentsdb.GetVersionForViewerRow, error) {
	var row documentsdb.GetVersionForViewerRow
	err := l.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var getErr error
		row, getErr = documentsdb.New(tx).GetVersionForViewer(ctx, documentsdb.GetVersionForViewerParams{ID: req.VersionID, DocumentID: req.DocumentID})
		return db.NotFoundIfNoRows(getErr, "version")
	})
	return row, err
}

func (l *Library) viewerPath(tenant uuid.UUID, row documentsdb.GetVersionForViewerRow) (string, error) {
	if row.Mime == pdfMIME {
		return l.Blobs.BlobPath(tenant, row.ContentHash), nil
	}
	if HasPreview(row.Mime) {
		return l.Blobs.DerivedPath(blobs.DerivedKey{Hash: row.ContentHash, Stage: renderStage, Version: renderVersion}), nil
	}
	return "", fmt.Errorf("version has no PDF to show: %w", errs.ErrNotFound)
}

func openStored(path, name, mime string) (ViewerFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return ViewerFile{}, fmt.Errorf("the file is not available yet: %w", errs.ErrNotFound)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return ViewerFile{}, fmt.Errorf("stat viewer file: %w", err)
	}
	return ViewerFile{File: f, Name: name, Mime: mime, ModTime: info.ModTime()}, nil
}
