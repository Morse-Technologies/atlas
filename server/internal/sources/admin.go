package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

// Source statuses an administrator can set.
const (
	StatusPaused = "paused"
	KindFolder   = "folder"
)

// Actor is who performs an administrative action: the tenant to scope to and the person to put in the audit log.
type Actor struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
}

// Page selects rows older than Before (uuid.Nil: from the newest). Lists read one row more than Size to know
// whether another page exists.
type Page struct {
	Before uuid.UUID
	Size   int32
}

func (p Page) before() uuid.UUID {
	if p.Before == uuid.Nil {
		return uuid.Max
	}
	return p.Before
}

// Admin is what the HTTP layer calls: each action runs in one transaction with its audit event.
type Admin struct {
	DB         *db.DB
	Now        func() time.Time
	UploadRoot string   // where uploaded files are kept, one folder per tenant
	Roots      []string // folders an administrator may register; nothing outside them can be read
	// ApplyAccess carries a source's new readers to its documents, in the same transaction as the change: a
	// restriction must never wait for a job. The composition root sets it (documents imports sources, not back).
	ApplyAccess func(ctx context.Context, tx pgx.Tx, change AccessChange) error
}

// AccessChange is a source whose readers changed, and the set that now applies (nil: everyone).
type AccessChange struct {
	SourceID uuid.UUID
	SetID    *uuid.UUID
}

// NewAdmin returns an Admin that reads the real clock and keeps uploads under uploadRoot.
func NewAdmin(d *db.DB, uploadRoot string, roots []string) *Admin {
	return &Admin{DB: d, Now: time.Now, UploadRoot: uploadRoot, Roots: roots}
}

// NewSource is a source to register. Only folders exist so far.
type NewSource struct {
	Name string
	Kind string
	Path string
}

// List returns the tenant's sources.
func (a *Admin) List(ctx context.Context, who Actor) ([]Source, error) {
	var out []Source
	err := a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		var listErr error
		out, listErr = List(ctx, tx)
		return listErr
	})
	return out, err
}

// Create registers a source and schedules its first scan.
func (a *Admin) Create(ctx context.Context, who Actor, in NewSource) (Source, error) {
	if in.Kind != KindFolder {
		return Source{}, fmt.Errorf("source kind %q is not available: %w", in.Kind, errs.ErrInvalid)
	}
	if err := InsideRoots(in.Path, a.Roots); err != nil {
		return Source{}, err
	}
	var created Source
	err := a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		id, err := AddFolder(ctx, tx, NewFolder{TenantID: who.TenantID, Name: in.Name, Path: in.Path})
		if err != nil {
			return err
		}
		if created, err = get(ctx, tx, id); err != nil {
			return err
		}
		return a.record(ctx, tx, change{by: who.UserID, action: "source.created", subject: sourceSubject(id), detail: "kind=" + in.Kind})
	})
	return created, err
}

// Pause stops scanning and watching a source. Its documents stay searchable.
func (a *Admin) Pause(ctx context.Context, who Actor, id uuid.UUID) (Source, error) {
	return a.setStatus(ctx, who, statusChange{id: id, status: StatusPaused, action: "source.paused"})
}

// Resume starts scanning a paused source again.
func (a *Admin) Resume(ctx context.Context, who Actor, id uuid.UUID) (Source, error) {
	return a.setStatus(ctx, who, statusChange{id: id, status: StatusActive, action: "source.resumed"})
}

type statusChange struct {
	id     uuid.UUID
	status string
	action string
}

func (a *Admin) setStatus(ctx context.Context, who Actor, c statusChange) (Source, error) {
	var out Source
	err := a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		n, err := sourcesdb.New(tx).SetSourceStatus(ctx, sourcesdb.SetSourceStatusParams{ID: c.id, Status: c.status})
		if err != nil || n == 0 {
			return notFoundOr(err, "source")
		}
		if out, err = get(ctx, tx, c.id); err != nil {
			return err
		}
		return a.record(ctx, tx, change{by: who.UserID, action: c.action, subject: sourceSubject(c.id)})
	})
	return out, err
}

// Sync asks for a scan of the source now.
func (a *Admin) Sync(ctx context.Context, who Actor, id uuid.UUID) error {
	return a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		if err := RequestRescan(ctx, tx, events.SourceRescan{TenantID: who.TenantID, SourceID: id, Manual: true}); err != nil {
			return err
		}
		return a.record(ctx, tx, change{by: who.UserID, action: "source.sync_requested", subject: sourceSubject(id)})
	})
}

// Scan is one pass over a source.
type Scan struct {
	ID         uuid.UUID
	SourceID   uuid.UUID
	StartedAt  time.Time
	FinishedAt time.Time
	Discovered int
	Changed    int
	Failed     int
}

// Scans returns one page of scans, newest first, and the cursor of the next page (uuid.Nil: none).
func (a *Admin) Scans(ctx context.Context, who Actor, page Page) ([]Scan, uuid.UUID, error) {
	var rows []sourcesdb.ListScansPageRow
	err := a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		var listErr error
		rows, listErr = sourcesdb.New(tx).ListScansPage(ctx, sourcesdb.ListScansPageParams{Before: page.before(), PageSize: page.Size + 1})
		return listErr
	})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("list scans: %w", err)
	}
	next := uuid.Nil
	if int32(len(rows)) > page.Size {
		rows, next = rows[:page.Size], rows[page.Size-1].ID
	}
	scans := make([]Scan, len(rows))
	for i, r := range rows {
		scans[i] = Scan{ID: r.ID, SourceID: r.SourceID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
			Discovered: int(r.Discovered), Changed: int(r.Changed), Failed: int(r.Failed)}
	}
	return scans, next, nil
}

// Quarantine returns one page of quarantined files, newest first, and the cursor of the next page.
func (a *Admin) Quarantine(ctx context.Context, who Actor, page Page) ([]QuarantinedFile, uuid.UUID, error) {
	var rows []sourcesdb.ListQuarantinePageRow
	err := a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		var listErr error
		rows, listErr = sourcesdb.New(tx).ListQuarantinePage(ctx, sourcesdb.ListQuarantinePageParams{Before: page.before(), PageSize: page.Size + 1})
		return listErr
	})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("list quarantine: %w", err)
	}
	next := uuid.Nil
	if int32(len(rows)) > page.Size {
		rows, next = rows[:page.Size], rows[page.Size-1].ID
	}
	files := make([]QuarantinedFile, len(rows))
	for i, r := range rows {
		files[i] = QuarantinedFile{ID: r.ID, SourceID: r.SourceID, ExternalID: r.ExternalID, Reason: r.Reason, Detail: r.Detail, At: r.At}
	}
	return files, next, nil
}

// RetryQuarantine takes a file out of quarantine and fetches it again.
func (a *Admin) RetryQuarantine(ctx context.Context, who Actor, id uuid.UUID) error {
	return a.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		if err := RetryQuarantined(ctx, tx, Retry{TenantID: who.TenantID, QuarantineID: id}); err != nil {
			return err
		}
		return a.record(ctx, tx, change{by: who.UserID, action: "quarantine.retried", subject: "quarantine:" + id.String()})
	})
}

// Check says whether path is a directory this installation may and can read. It never changes anything.
func (a *Admin) Check(path string) error {
	if err := InsideRoots(path, a.Roots); err != nil {
		return err
	}
	return checkReadable(path)
}

func checkReadable(path string) error {
	abs, err := absoluteDir(path)
	if err != nil {
		return err
	}
	dir, err := os.Open(abs)
	if err != nil {
		return fmt.Errorf("%s cannot be opened: %w", abs, errs.ErrInvalid)
	}
	defer func() { _ = dir.Close() }()
	if _, err := dir.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s cannot be listed: %w", abs, errs.ErrInvalid)
	}
	return nil
}

func get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Source, error) {
	r, err := sourcesdb.New(tx).GetSource(ctx, id)
	if err != nil {
		return Source{}, db.NotFoundIfNoRows(err, "source")
	}
	readers, err := access.ReadersOf(ctx, tx, r.AclSetID)
	if err != nil {
		return Source{}, err
	}
	return Source{ID: r.ID, Name: r.Name, Kind: r.Kind, Root: r.RootPath, Status: r.Status, LastScanAt: r.LastScanAt, LastError: r.LastError, Readers: readers}, nil
}

// notFoundOr turns "no row updated" (err nil) into ErrNotFound and keeps real errors.
func notFoundOr(err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return fmt.Errorf("%s: %w", what, errs.ErrNotFound)
}

// change is what an administrative action reports to the audit log. Ids only: the log cannot be edited.
type change struct {
	by      uuid.UUID
	action  string
	subject string
	detail  string
}

func sourceSubject(id uuid.UUID) string { return "source:" + id.String() }

func (a *Admin) record(ctx context.Context, tx pgx.Tx, c change) error {
	return audit.Record(ctx, tx, audit.Entry{At: a.Now(), Actor: c.by, Action: c.action,
		Subject: c.subject, Detail: c.detail})
}

// QuarantineCount is how many files are waiting in quarantine.
func (a *Admin) QuarantineCount(ctx context.Context, tenant uuid.UUID) (int, error) {
	var n int32
	err := a.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		var countErr error
		n, countErr = sourcesdb.New(tx).CountQuarantine(ctx)
		return countErr
	})
	return int(n), err
}
