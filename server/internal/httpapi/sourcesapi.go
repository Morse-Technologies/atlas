package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources"
)

// SourceAdmin is what the HTTP layer needs from the sources module. *sources.Admin implements it.
type SourceAdmin interface {
	List(ctx context.Context, who sources.Actor) ([]sources.Source, error)
	Create(ctx context.Context, who sources.Actor, in sources.NewSource) (sources.Source, error)
	Pause(ctx context.Context, who sources.Actor, id uuid.UUID) (sources.Source, error)
	Resume(ctx context.Context, who sources.Actor, id uuid.UUID) (sources.Source, error)
	Sync(ctx context.Context, who sources.Actor, id uuid.UUID) error
	Scans(ctx context.Context, who sources.Actor, page sources.Page) ([]sources.Scan, uuid.UUID, error)
	Quarantine(ctx context.Context, who sources.Actor, page sources.Page) ([]sources.QuarantinedFile, uuid.UUID, error)
	RetryQuarantine(ctx context.Context, who sources.Actor, id uuid.UUID) error
	SetReaders(ctx context.Context, who sources.Actor, c sources.ReadersChange) (sources.Source, error)
	Check(path string) error
	Upload(ctx context.Context, who sources.Actor, parts sources.PartReader) (sources.UploadResult, error)
}

func actorOf(ctx context.Context) sources.Actor {
	p := principalFrom(ctx)
	return sources.Actor{TenantID: p.TenantID, UserID: p.UserID}
}

func sourcePage(limit *Limit, cursor *Cursor) (sources.Page, error) {
	before, err := uuidCursor(cursor)
	return sources.Page{Before: before, Size: pageSize(limit)}, err
}

// ListSources returns the tenant's sources.
func (s *Server) ListSources(ctx context.Context, _ ListSourcesRequestObject) (ListSourcesResponseObject, error) {
	list, err := s.Sources.List(ctx, actorOf(ctx))
	if err != nil {
		return nil, err
	}
	body := make([]Source, len(list))
	for i, src := range list {
		body[i] = toSource(src)
	}
	return ListSources200JSONResponse(body), nil
}

// CreateSource registers a source and schedules its first scan.
func (s *Server) CreateSource(ctx context.Context, req CreateSourceRequestObject) (CreateSourceResponseObject, error) {
	b := req.Body
	src, err := s.Sources.Create(ctx, actorOf(ctx), sources.NewSource{Name: b.Name, Kind: string(b.Kind), Path: b.Location})
	if err != nil {
		return nil, err
	}
	return CreateSource201JSONResponse(toSource(src)), nil
}

// CheckFolder tells the setup wizard whether a path can be read.
func (s *Server) CheckFolder(_ context.Context, req CheckFolderRequestObject) (CheckFolderResponseObject, error) {
	if err := s.Sources.Check(req.Body.Path); err != nil {
		return CheckFolder200JSONResponse{Ok: false, Detail: err.Error()}, nil
	}
	return CheckFolder200JSONResponse{Ok: true, Detail: "Atlas can read this folder."}, nil
}

// UpdateSource pauses or resumes a source, or changes who may read it.
func (s *Server) UpdateSource(ctx context.Context, req UpdateSourceRequestObject) (UpdateSourceResponseObject, error) {
	if req.Body.Paused == nil && req.Body.Access == nil {
		return nil, errNothingToChange
	}
	who := actorOf(ctx)
	var src sources.Source
	var err error
	if req.Body.Access != nil {
		src, err = s.changeReaders(ctx, who, req)
	}
	if err == nil && req.Body.Paused != nil {
		src, err = s.pauseOrResume(ctx, who, req)
	}
	if err != nil {
		return nil, err
	}
	return UpdateSource200JSONResponse(toSource(src)), nil
}

func (s *Server) pauseOrResume(ctx context.Context, who sources.Actor, req UpdateSourceRequestObject) (sources.Source, error) {
	if *req.Body.Paused {
		return s.Sources.Pause(ctx, who, req.Id)
	}
	return s.Sources.Resume(ctx, who, req.Id)
}

// changeReaders restricts the source to the given people, or opens it to everyone.
func (s *Server) changeReaders(ctx context.Context, who sources.Actor, req UpdateSourceRequestObject) (sources.Source, error) {
	change := sources.ReadersChange{ID: req.Id}
	if *req.Body.Access == SourcePatchAccessRestricted {
		if req.Body.Readers == nil {
			return sources.Source{}, fmt.Errorf("readers are needed to restrict a source: %w", errs.ErrInvalid)
		}
		change.Readers = *req.Body.Readers
	}
	return s.Sources.SetReaders(ctx, who, change)
}

// SyncSource asks for a scan now; progress shows up in the runs list.
func (s *Server) SyncSource(ctx context.Context, req SyncSourceRequestObject) (SyncSourceResponseObject, error) {
	if err := s.Sources.Sync(ctx, actorOf(ctx), req.Id); err != nil {
		return nil, err
	}
	return SyncSource202Response{}, nil
}

// ListRuns returns recent scans, newest first.
func (s *Server) ListRuns(ctx context.Context, req ListRunsRequestObject) (ListRunsResponseObject, error) {
	page, err := sourcePage(req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	scans, next, err := s.Sources.Scans(ctx, actorOf(ctx), page)
	if err != nil {
		return nil, err
	}
	body := make([]IngestionRun, len(scans))
	for i, sc := range scans {
		body[i] = toRun(sc)
	}
	return ListRuns200JSONResponse{Body: body, Headers: ListRuns200ResponseHeaders{NextCursor: cursorOf(next)}}, nil
}

// ListQuarantine returns files that failed permanently, newest first.
func (s *Server) ListQuarantine(ctx context.Context, req ListQuarantineRequestObject) (ListQuarantineResponseObject, error) {
	page, err := sourcePage(req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	files, next, err := s.Sources.Quarantine(ctx, actorOf(ctx), page)
	if err != nil {
		return nil, err
	}
	body := make([]QuarantineItem, len(files))
	for i, f := range files {
		body[i] = QuarantineItem{Id: f.ID, SourceId: f.SourceID, Path: f.ExternalID,
			Reason: QuarantineItemReason(f.Reason), Detail: f.Detail, At: f.At}
	}
	return ListQuarantine200JSONResponse{Body: body, Headers: ListQuarantine200ResponseHeaders{NextCursor: cursorOf(next)}}, nil
}

// RetryQuarantine sends a quarantined file through the pipeline again.
func (s *Server) RetryQuarantine(ctx context.Context, req RetryQuarantineRequestObject) (RetryQuarantineResponseObject, error) {
	if err := s.Sources.RetryQuarantine(ctx, actorOf(ctx), req.Id); err != nil {
		return nil, err
	}
	return RetryQuarantine202Response{}, nil
}

func toSource(s sources.Source) Source {
	return Source{Id: s.ID, Name: s.Name, Kind: SourceKind(s.Kind), Location: s.Root,
		Status: SourceStatus(s.Status), LastSyncAt: s.LastScanAt, LastError: s.LastError, Pro: false,
		Access: accessOf(s), Readers: readersOf(s)}
}

func accessOf(s sources.Source) SourceAccess {
	if s.Readers == nil {
		return SourceAccessEveryone
	}
	return SourceAccessRestricted
}

// readersOf is never null in the response: a list that is empty when everyone may read.
func readersOf(s sources.Source) []uuid.UUID {
	if s.Readers == nil {
		return []uuid.UUID{}
	}
	return s.Readers
}

// toRun maps a scan to the run the UI shows. Files not changed are "skipped": the scan listed them and found nothing to do.
func toRun(sc sources.Scan) IngestionRun {
	finished := sc.FinishedAt
	return IngestionRun{Id: sc.ID, SourceId: sc.SourceID, StartedAt: sc.StartedAt, FinishedAt: &finished,
		Discovered: sc.Discovered, Indexed: sc.Changed, Skipped: max(sc.Discovered-sc.Changed, 0), Failed: sc.Failed}
}

// UploadFiles stores the files of a multipart body in the upload folder; the pipeline does the rest.
func (s *Server) UploadFiles(ctx context.Context, req UploadFilesRequestObject) (UploadFilesResponseObject, error) {
	result, err := s.Sources.Upload(ctx, actorOf(ctx), req.Body)
	if err != nil {
		return nil, err
	}
	rejected := make([]struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}, len(result.Rejected))
	for i, r := range result.Rejected {
		rejected[i].Name, rejected[i].Reason = r.Name, r.Reason
	}
	return UploadFiles202JSONResponse{Accepted: result.Accepted, Rejected: rejected}, nil
}
