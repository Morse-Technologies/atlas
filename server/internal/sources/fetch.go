package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/formats"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

// StepFetch is the pipeline step name of this stage.
const StepFetch = "fetch"

// fetchTarget is what fetch needs to know about the file before reading it.
type fetchTarget struct {
	root         string
	maxBytes     int64
	previousHash string
}

// stagedFile is a file copied into the blob temp area, with the state the scan compares (size, mtime).
type stagedFile struct {
	blobs.Staged
	state FileState
}

// checkedFile is a staged file that passed the hostile-file checks.
type checkedFile struct {
	stagedFile
	format formats.Format
}

// Fetch reads one file, applies the hostile-file checks, stores the blob and tells documents (ATLAS.md §8).
func (m *Module) Fetch(ctx context.Context, args events.VersionFetch) error {
	target, err := m.loadFetchTarget(ctx, args)
	if err != nil {
		return err
	}
	staged, err := m.stageFile(target, args.ExternalID)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // gone since it was listed; the next complete scan records the deletion
	}
	if err != nil {
		return err
	}
	format, err := checkHostileFile(staged.Staged, target.maxBytes)
	if err != nil {
		m.Blobs.Discard(staged.Staged)
		return m.quarantineIfPermanent(ctx, args, err)
	}
	return m.storeIfChanged(ctx, args, storeRequest{file: checkedFile{stagedFile: staged, format: format}, previousHash: target.previousHash})
}

func (m *Module) loadFetchTarget(ctx context.Context, args events.VersionFetch) (fetchTarget, error) {
	var t fetchTarget
	err := m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		q := sourcesdb.New(tx)
		src, err := q.GetSourceRoot(ctx, args.SourceID)
		if err != nil {
			return db.NotFoundIfNoRows(err, "source")
		}
		hash, err := q.GetFileHash(ctx, sourcesdb.GetFileHashParams{SourceID: args.SourceID, ExternalID: args.ExternalID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("file hash: %w", err)
		}
		t = fetchTarget{root: src.RootPath, maxBytes: src.MaxFileBytes, previousHash: deref(hash)}
		return nil
	})
	return t, err
}

// stageFile copies the file into the blob temp area, reading at most one byte past the size limit.
func (m *Module) stageFile(t fetchTarget, externalID string) (stagedFile, error) {
	f, err := folder{root: t.root}.open(externalID)
	if err != nil {
		return stagedFile{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return stagedFile{}, err
	}
	staged, err := m.Blobs.Stage(io.LimitReader(f, t.maxBytes+1))
	state := FileState{ExternalID: externalID, Size: info.Size(), ModifiedAt: info.ModTime().UTC()}
	return stagedFile{Staged: staged, state: state}, err
}

func (m *Module) quarantineIfPermanent(ctx context.Context, args events.VersionFetch, err error) error {
	if errs.Classify(err) != errs.ClassPermanent {
		return err
	}
	failure := Failure{SourceID: args.SourceID, ExternalID: args.ExternalID, Stage: StepFetch, Err: err}
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		return Quarantine(ctx, tx, failure)
	})
}

type storeRequest struct {
	file         checkedFile
	previousHash string
}

// storeIfChanged commits the blob and announces it, unless the content is the one already fetched.
func (m *Module) storeIfChanged(ctx context.Context, args events.VersionFetch, req storeRequest) error {
	step := Step{SourceID: args.SourceID, ExternalID: args.ExternalID, ContentHash: req.file.Hash, Name: StepFetch}
	if req.file.Hash == req.previousHash {
		m.Blobs.Discard(req.file.Staged)
		step.Status, step.Detail = StepSkipped, "content unchanged"
		return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error { return RecordStep(ctx, tx, step) })
	}
	if err := m.Blobs.Commit(args.TenantID, req.file.Staged); err != nil {
		m.Blobs.Discard(req.file.Staged)
		return err
	}
	step.Status = StepDone
	return m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		return publishStored(ctx, tx, storedVersion{args: args, file: req.file, step: step})
	})
}

type storedVersion struct {
	args events.VersionFetch
	file checkedFile
	step Step
}

func publishStored(ctx context.Context, tx pgx.Tx, s storedVersion) error {
	err := sourcesdb.New(tx).RecordFetchedFile(ctx, sourcesdb.RecordFetchedFileParams{
		SourceID: s.args.SourceID, ExternalID: s.args.ExternalID, Size: s.file.state.Size,
		ModifiedAt: s.file.state.ModifiedAt, ContentHash: new(s.file.Hash),
	})
	if err != nil {
		return fmt.Errorf("save file hash: %w", err)
	}
	if err := RecordStep(ctx, tx, s.step); err != nil {
		return err
	}
	return jobs.Enqueue(ctx, tx, events.VersionStored{
		TenantID: s.args.TenantID, SourceID: s.args.SourceID, ExternalID: s.args.ExternalID,
		ContentHash: s.file.Hash, MIME: s.file.format.MIME, Size: s.file.Size,
	})
}
