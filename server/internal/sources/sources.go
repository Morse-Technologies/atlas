// Package sources owns where documents come from: sources, scans, fetch, quarantine and the pipeline
// step log (ATLAS.md §5.1, §6). Other modules call only the functions in this file.
package sources

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/blobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

// Module holds what the sources jobs need.
type Module struct {
	DB     *db.DB
	Blobs  *blobs.Store
	Logger *slog.Logger
}

// StatusActive is a source that is scanned and watched; paused and error sources are not.
const StatusActive = "active"

// Step outcomes recorded in pipeline_steps.
const (
	StepDone    = "done"
	StepSkipped = "skipped"
	StepFailed  = "failed"
)

// Step is one row of the pipeline log for a file.
type Step struct {
	SourceID    uuid.UUID
	ExternalID  string
	ContentHash string
	Name        string
	Status      string
	Detail      string
}

// RecordStep appends to the pipeline log in the caller's transaction.
func RecordStep(ctx context.Context, tx pgx.Tx, step Step) error {
	err := sourcesdb.New(tx).RecordStep(ctx, sourcesdb.RecordStepParams{
		SourceID: step.SourceID, ExternalID: step.ExternalID, ContentHash: optional(step.ContentHash),
		Step: step.Name, Status: step.Status, Detail: optional(step.Detail),
	})
	if err != nil {
		return fmt.Errorf("record step %s: %w", step.Name, err)
	}
	return nil
}

// Failure is a file that failed permanently at some stage.
type Failure struct {
	SourceID    uuid.UUID
	ExternalID  string
	ContentHash string
	Stage       string
	Err         error
}

// Quarantine stores a permanent failure where admins see it and logs the failed step.
func Quarantine(ctx context.Context, tx pgx.Tx, f Failure) error {
	err := sourcesdb.New(tx).UpsertQuarantine(ctx, sourcesdb.UpsertQuarantineParams{
		SourceID: f.SourceID, ExternalID: f.ExternalID,
		Reason: string(errs.PermanentReason(f.Err)), Detail: f.Err.Error(),
	})
	if err != nil {
		return fmt.Errorf("quarantine: %w", err)
	}
	return RecordStep(ctx, tx, Step{SourceID: f.SourceID, ExternalID: f.ExternalID, ContentHash: f.ContentHash,
		Name: f.Stage, Status: StepFailed, Detail: f.Err.Error()})
}

// NewFolder is a folder source to register.
type NewFolder struct {
	TenantID uuid.UUID
	Name     string
	Path     string
}

// AddFolder registers a folder source and schedules its first scan.
func AddFolder(ctx context.Context, tx pgx.Tx, nf NewFolder) (uuid.UUID, error) {
	root, err := absoluteDir(nf.Path)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := sourcesdb.New(tx).CreateFolderSource(ctx, sourcesdb.CreateFolderSourceParams{Name: nf.Name, RootPath: root})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create source: %w", err)
	}
	return id, jobs.Enqueue(ctx, tx, events.SourceRescan{TenantID: nf.TenantID, SourceID: id})
}

// absoluteDir resolves path and checks it is a directory this process can read.
func absoluteDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a readable directory: %w", abs, errs.ErrInvalid)
	}
	return abs, nil
}

// Source is a registered source as listed to admins.
type Source struct {
	ID         uuid.UUID
	Name       string
	Kind       string
	Root       string
	Status     string
	LastScanAt *time.Time
	LastError  *string
	// Readers are the only people who may read this source's documents; nil means every signed-in person.
	Readers []uuid.UUID
}

// List returns the tenant's sources.
func List(ctx context.Context, tx pgx.Tx) ([]Source, error) {
	rows, err := sourcesdb.New(tx).ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	out := make([]Source, len(rows))
	for i, r := range rows {
		readers, err := access.ReadersOf(ctx, tx, r.AclSetID)
		if err != nil {
			return nil, err
		}
		out[i] = Source{ID: r.ID, Name: r.Name, Kind: r.Kind, Root: r.RootPath, Status: r.Status, LastScanAt: r.LastScanAt, LastError: r.LastError, Readers: readers}
	}
	return out, nil
}

// RequestRescan schedules a scan of an existing source.
func RequestRescan(ctx context.Context, tx pgx.Tx, req events.SourceRescan) error {
	if _, err := sourcesdb.New(tx).GetSourceRoot(ctx, req.SourceID); err != nil {
		return db.NotFoundIfNoRows(err, "source")
	}
	return jobs.Enqueue(ctx, tx, req)
}

// QuarantinedFile is one quarantine entry.
type QuarantinedFile struct {
	ID         uuid.UUID
	SourceID   uuid.UUID
	ExternalID string
	Reason     string
	Detail     string
	At         time.Time
}

// ListQuarantine returns the tenant's quarantined files, newest first.
func ListQuarantine(ctx context.Context, tx pgx.Tx) ([]QuarantinedFile, error) {
	rows, err := sourcesdb.New(tx).ListQuarantine(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quarantine: %w", err)
	}
	out := make([]QuarantinedFile, len(rows))
	for i, r := range rows {
		out[i] = QuarantinedFile{ID: r.ID, SourceID: r.SourceID, ExternalID: r.ExternalID, Reason: r.Reason, Detail: r.Detail, At: r.At}
	}
	return out, nil
}

// Retry identifies a quarantine entry to send through the pipeline again.
type Retry struct {
	TenantID     uuid.UUID
	QuarantineID uuid.UUID
}

// RetryQuarantined removes the entry and fetches the file again.
func RetryQuarantined(ctx context.Context, tx pgx.Tx, r Retry) error {
	row, err := sourcesdb.New(tx).TakeQuarantined(ctx, r.QuarantineID)
	if err != nil {
		return db.NotFoundIfNoRows(err, "quarantine entry")
	}
	return jobs.Enqueue(ctx, tx, events.VersionFetch{TenantID: r.TenantID, SourceID: row.SourceID, ExternalID: row.ExternalID})
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
