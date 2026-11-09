package sources

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/sources/sourcesdb"
)

// Scan outcomes recorded in sources.scans.
const (
	scanCompleted = "completed"
	scanFailed    = "failed"
	scanHeld      = "held"
)

// scanState is the source as the scan found it before walking.
type scanState struct {
	generation int64
	root       string
	known      []FileState
	startedAt  time.Time
}

// scanResult is everything applyScan writes.
type scanResult struct {
	args    events.SourceRescan
	state   scanState
	seen    []FileState
	plan    ScanPlan
	walkErr error
}

// Rescan walks a source and turns what changed into fetch, move and delete jobs (ATLAS.md §6.1).
func (m *Module) Rescan(ctx context.Context, args events.SourceRescan) error {
	state, err := m.startScan(ctx, args)
	if err != nil {
		return err
	}
	src := folder{root: state.root}
	seen, walkErr := src.walk(ctx)
	if walkErr == nil {
		walkErr = hashAddedFiles(src, seen, state.known)
	}
	plan := PlanScan(ScanInput{Known: state.known, Seen: seen, WalkComplete: walkErr == nil, AllowMassDeletion: args.AllowMassDeletion})
	return m.applyScan(ctx, scanResult{args: args, state: state, seen: seen, plan: plan, walkErr: walkErr})
}

func (m *Module) startScan(ctx context.Context, args events.SourceRescan) (scanState, error) {
	state := scanState{startedAt: time.Now()}
	err := m.DB.InTenantTx(ctx, args.TenantID, func(tx pgx.Tx) error {
		q := sourcesdb.New(tx)
		row, err := q.StartScan(ctx, args.SourceID)
		if err != nil {
			return db.NotFoundIfNoRows(err, "source")
		}
		state.generation, state.root = row.ScanGeneration, row.RootPath
		state.known, err = presentFiles(ctx, q, args)
		return err
	})
	return state, err
}

func presentFiles(ctx context.Context, q *sourcesdb.Queries, args events.SourceRescan) ([]FileState, error) {
	rows, err := q.ListPresentFiles(ctx, args.SourceID)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	out := make([]FileState, len(rows))
	for i, r := range rows {
		out[i] = FileState{ExternalID: r.ExternalID, Size: r.Size, ModifiedAt: r.ModifiedAt, Hash: deref(r.ContentHash)}
	}
	return out, nil
}

// hashAddedFiles hashes only files not known before, which is all move detection needs.
// A file that vanished in the meantime makes the walk count as incomplete.
func hashAddedFiles(src folder, seen, known []FileState) error {
	knownIDs := byExternalID(known)
	for i := range seen {
		if _, ok := knownIDs[seen[i].ExternalID]; ok {
			continue
		}
		hash, err := src.hash(seen[i].ExternalID)
		if err != nil {
			return err
		}
		seen[i].Hash = hash
	}
	return nil
}

func (m *Module) applyScan(ctx context.Context, r scanResult) error {
	return m.DB.InTenantTx(ctx, r.args.TenantID, func(tx pgx.Tx) error {
		q := sourcesdb.New(tx)
		if err := q.UpsertSeenFiles(ctx, seenFileRows(r)); err != nil {
			return fmt.Errorf("save seen files: %w", err)
		}
		if err := applyMoves(ctx, tx, r); err != nil {
			return err
		}
		if err := applyDeletes(ctx, tx, r); err != nil {
			return err
		}
		if err := jobs.Enqueue(ctx, tx, fetchJobs(r)...); err != nil {
			return err
		}
		return recordScan(ctx, q, r)
	})
}

// seenFileRows saves every seen file after a complete walk. After an incomplete one it only refreshes known
// files: a new file saved without being fetched would look unchanged at the next scan and never be ingested.
func seenFileRows(r scanResult) sourcesdb.UpsertSeenFilesParams {
	known := byExternalID(r.state.known)
	walkComplete := r.walkErr == nil
	p := sourcesdb.UpsertSeenFilesParams{SourceID: r.args.SourceID, Generation: r.state.generation}
	for _, f := range r.seen {
		_, isKnown := known[f.ExternalID]
		if !walkComplete && !isKnown {
			continue
		}
		p.ExternalIds = append(p.ExternalIds, f.ExternalID)
		p.Sizes = append(p.Sizes, f.Size)
		p.ModifiedAts = append(p.ModifiedAts, f.ModifiedAt)
	}
	return p
}

func applyMoves(ctx context.Context, tx pgx.Tx, r scanResult) error {
	q := sourcesdb.New(tx)
	for _, mv := range r.plan.Moves {
		if err := q.DeleteFileRow(ctx, sourcesdb.DeleteFileRowParams{SourceID: r.args.SourceID, ExternalID: mv.From}); err != nil {
			return fmt.Errorf("move %s: %w", mv.From, err)
		}
		if err := q.SetFileHash(ctx, sourcesdb.SetFileHashParams{SourceID: r.args.SourceID, ExternalID: mv.To, ContentHash: new(mv.Hash)}); err != nil {
			return fmt.Errorf("move %s: %w", mv.To, err)
		}
		moved := events.DocumentMoved{TenantID: r.args.TenantID, SourceID: r.args.SourceID, OldExternalID: mv.From, NewExternalID: mv.To}
		if err := jobs.Enqueue(ctx, tx, moved); err != nil {
			return err
		}
	}
	return nil
}

func applyDeletes(ctx context.Context, tx pgx.Tx, r scanResult) error {
	if len(r.plan.Delete) == 0 {
		return nil
	}
	err := sourcesdb.New(tx).MarkFilesDeleted(ctx, sourcesdb.MarkFilesDeletedParams{SourceID: r.args.SourceID, ExternalIds: r.plan.Delete})
	if err != nil {
		return fmt.Errorf("mark deleted: %w", err)
	}
	deleted := make([]river.JobArgs, len(r.plan.Delete))
	for i, id := range r.plan.Delete {
		deleted[i] = events.DocumentDeleted{TenantID: r.args.TenantID, SourceID: r.args.SourceID, ExternalID: id}
	}
	return jobs.Enqueue(ctx, tx, deleted...)
}

func fetchJobs(r scanResult) []river.JobArgs {
	out := make([]river.JobArgs, len(r.plan.Fetch))
	for i, id := range r.plan.Fetch {
		out[i] = events.VersionFetch{TenantID: r.args.TenantID, SourceID: r.args.SourceID, ExternalID: id}
	}
	return out
}

func recordScan(ctx context.Context, q *sourcesdb.Queries, r scanResult) error {
	status, errText := scanOutcome(r)
	err := q.RecordScan(ctx, sourcesdb.RecordScanParams{
		SourceID: r.args.SourceID, Generation: r.state.generation, Status: status,
		Discovered: int32(len(r.seen)), Changed: int32(len(r.plan.Fetch)), Moved: int32(len(r.plan.Moves)),
		Deleted: int32(len(r.plan.Delete)), HeldDeletions: int32(r.plan.HeldDeletions),
		Error: errText, StartedAt: r.state.startedAt,
	})
	if err != nil {
		return fmt.Errorf("record scan: %w", err)
	}
	return q.FinishSourceScan(ctx, sourcesdb.FinishSourceScanParams{ID: r.args.SourceID, LastError: errText})
}

var errHeld = errors.New("deletions held: the source lost too many files at once; run `atlas source sync --allow-mass-deletion` if this is intended")

func scanOutcome(r scanResult) (string, *string) {
	switch {
	case r.walkErr != nil:
		msg := "incomplete walk, nothing deleted: " + r.walkErr.Error()
		return scanFailed, &msg
	case r.plan.HeldDeletions > 0:
		msg := errHeld.Error()
		return scanHeld, &msg
	default:
		return scanCompleted, nil
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
