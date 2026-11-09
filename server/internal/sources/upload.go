package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

// Limits of one upload request. Per-file size matches the default of sources.max_file_bytes (ATLAS.md §8.1).
const (
	KindUpload          = "upload"
	maxUploadFileBytes  = 200 << 20
	maxUploadFiles      = 50
	maxUploadTotalBytes = 1 << 30
	uploadFormField     = "files"
	tempDirName         = ".incoming"
	dirMode             = 0o750
	fileMode            = 0o640
)

// PartReader yields the parts of a multipart body one at a time. *multipart.Reader implements it.
type PartReader interface {
	NextPart() (*multipart.Part, error)
}

// Rejected is a file that was not accepted, and why.
type Rejected struct {
	Name   string
	Reason string
}

// UploadResult says how many files entered the pipeline and which did not.
type UploadResult struct {
	Accepted int
	Rejected []Rejected
}

// Upload stores the files of a multipart body in the tenant's upload folder and asks for a scan. Files go
// through the same pipeline as any other source, including its content checks: this only checks names and sizes.
// Uploading a name that exists adds a new version of that document.
func (a *Admin) Upload(ctx context.Context, who Actor, parts PartReader) (UploadResult, error) {
	dir := filepath.Join(a.UploadRoot, who.TenantID.String())
	result, err := a.store(ctx, dir, parts)
	if err != nil || result.Accepted == 0 {
		return result, err
	}
	err = a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		id, err := uploadSource(ctx, tx, dir)
		if err != nil {
			return err
		}
		if err := jobs.Enqueue(ctx, tx, events.SourceRescan{TenantID: who.TenantID, SourceID: id}); err != nil {
			return err
		}
		return a.record(ctx, tx, change{by: who.UserID, action: "upload.received", subject: sourceSubject(id),
			detail: fmt.Sprintf("files=%d", result.Accepted)})
	})
	return result, err
}

// uploadSource returns the tenant's upload source, creating it on first use.
func uploadSource(ctx context.Context, tx pgx.Tx, dir string) (uuid.UUID, error) {
	q := sourcesdb.New(tx)
	id, err := q.FindUploadSource(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = q.CreateUploadSource(ctx, dir)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("upload source: %w", err)
	}
	return id, nil
}

func (a *Admin) store(_ context.Context, dir string, parts PartReader) (UploadResult, error) {
	var result UploadResult
	var total int64
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return result, fmt.Errorf("create upload folder: %w", err)
	}
	for {
		part, err := nextFile(parts)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err == nil && (result.Accepted+len(result.Rejected) >= maxUploadFiles || total >= maxUploadTotalBytes) {
			err = fmt.Errorf("an upload takes at most %d files and %d MB: %w", maxUploadFiles, maxUploadTotalBytes>>20, errs.ErrInvalid)
		}
		if err != nil {
			return result, err
		}
		size, reason := a.saveFile(dir, part)
		total += size
		result.add(part.FileName(), reason)
	}
}

// nextFile returns the next part that carries a file in the "files" field, skipping anything else.
func nextFile(parts PartReader) (*multipart.Part, error) {
	for {
		part, err := parts.NextPart()
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read upload: %w: %w", err, errs.ErrInvalid)
		}
		if err != nil || (part.FormName() == uploadFormField && part.FileName() != "") {
			return part, err
		}
	}
}

func (r *UploadResult) add(name, rejection string) {
	if rejection == "" {
		r.Accepted++
		return
	}
	r.Rejected = append(r.Rejected, Rejected{Name: name, Reason: rejection})
}

// saveFile writes one part atomically into dir and returns the bytes read and the reason it was refused ("" if saved).
func (a *Admin) saveFile(dir string, part *multipart.Part) (int64, string) {
	name, ok := safeName(part.FileName())
	if !ok {
		return 0, "the file name is not allowed"
	}
	path, size, reason := a.copyToTemp(part)
	defer func() { _ = os.Remove(path) }() // gone after the rename; cleans up after a refusal
	if reason != "" {
		return size, reason
	}
	if err := os.Chmod(path, fileMode); err != nil || os.Rename(path, filepath.Join(dir, name)) != nil {
		return size, "the server could not store the file"
	}
	return size, ""
}

// copyToTemp reads at most one byte over the limit, so an oversized file is noticed without storing all of it.
func (a *Admin) copyToTemp(part *multipart.Part) (path string, size int64, reason string) {
	tmp, err := os.CreateTemp(a.tempDir(), "upload-*")
	if err != nil {
		return "", 0, "the server could not store the file"
	}
	size, err = io.Copy(tmp, io.LimitReader(part, maxUploadFileBytes+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		return tmp.Name(), size, "the upload was interrupted"
	case size == 0:
		return tmp.Name(), size, "the file is empty"
	case size > maxUploadFileBytes:
		return tmp.Name(), size, "the file is larger than the limit"
	}
	return tmp.Name(), size, ""
}

// tempDir is outside every upload folder, so a half-written file is never scanned.
func (a *Admin) tempDir() string {
	dir := filepath.Join(a.UploadRoot, tempDirName)
	_ = os.MkdirAll(dir, dirMode)
	return dir
}

// safeName keeps the last path element and refuses names the scanner ignores or that could climb out of the folder.
func safeName(raw string) (string, bool) {
	name := filepath.Base(strings.ReplaceAll(raw, `\`, "/"))
	hidden := strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$")
	return name, name != "" && name != string(filepath.Separator) && !hidden && !strings.ContainsRune(name, 0)
}
