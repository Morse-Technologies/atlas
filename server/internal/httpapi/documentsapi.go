package httpapi

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"path"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/graph"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/sources"
)

// DocumentLibrary is what the HTTP layer needs from the documents module. *documents.Library implements it.
type DocumentLibrary interface {
	List(ctx context.Context, who documents.Actor, in documents.Listing) ([]documents.Document, uuid.UUID, error)
	Get(ctx context.Context, who documents.Actor, id uuid.UUID) (documents.Document, error)
	Correct(ctx context.Context, who documents.Actor, c documents.Correction) (documents.Document, error)
	OpenFile(ctx context.Context, who documents.Actor, req documents.FileRequest) (documents.ViewerFile, error)
	OpenOriginal(ctx context.Context, who documents.Actor, req documents.FileRequest) (documents.ViewerFile, error)
}

// Taxonomy is what the HTTP layer needs from the catalog module. *catalog.Service implements it.
type Taxonomy interface {
	Snapshot(ctx context.Context, tenant uuid.UUID) (catalog.Snapshot, error)
}

type requestKey struct{}

func documentActor(ctx context.Context) documents.Actor {
	p := principalFrom(ctx)
	return documents.Actor{TenantID: p.TenantID, UserID: p.UserID}
}

// ListDocuments returns one page of documents with the filters of the Documents screen.
func (s *Server) ListDocuments(ctx context.Context, req ListDocumentsRequestObject) (ListDocumentsResponseObject, error) {
	after, err := uuidCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	in := documents.Listing{After: after, Size: pageSize(req.Params.Limit), SourceID: req.Params.SourceId,
		Query: deref(req.Params.Q), Band: string(derefBand(req.Params.Band)), Status: string(derefStatus(req.Params.Status))}
	docs, next, err := s.Documents.List(ctx, documentActor(ctx), in)
	if err != nil {
		return nil, err
	}
	body := make([]Document, len(docs))
	for i, d := range docs {
		body[i] = toDocumentJSON(d)
	}
	if err := s.fillEntities(ctx, body); err != nil {
		return nil, err
	}
	return ListDocuments200JSONResponse{Body: body, Headers: ListDocuments200ResponseHeaders{NextCursor: cursorOf(next)}}, nil
}

// GetDocument returns one document.
func (s *Server) GetDocument(ctx context.Context, req GetDocumentRequestObject) (GetDocumentResponseObject, error) {
	doc, err := s.Documents.Get(ctx, documentActor(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	docs := []Document{toDocumentJSON(doc)}
	if err := s.fillEntities(ctx, docs); err != nil {
		return nil, err
	}
	return GetDocument200JSONResponse(docs[0]), nil
}

// DeleteDocument deletes a document that came from an upload. The person must be able to see it (404 otherwise);
// a document of a folder source is refused with 409 by the sources module.
func (s *Server) DeleteDocument(ctx context.Context, req DeleteDocumentRequestObject) (DeleteDocumentResponseObject, error) {
	doc, err := s.Documents.Get(ctx, documentActor(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	file := sources.UploadedFile{SourceID: doc.SourceID, ExternalID: doc.Path, DocumentID: doc.ID}
	if err := s.Sources.DeleteUpload(ctx, actorOf(ctx), file); err != nil {
		return nil, err
	}
	return DeleteDocument204Response{}, nil
}

// ListRelated returns the documents linked to this one by a reference or as a duplicate. Each is loaded as the
// person, so one they cannot read is left out as if it did not exist.
func (s *Server) ListRelated(ctx context.Context, req ListRelatedRequestObject) (ListRelatedResponseObject, error) {
	if _, err := s.Documents.Get(ctx, documentActor(ctx), req.Id); err != nil {
		return nil, err
	}
	if s.Graph == nil {
		return ListRelated200JSONResponse([]Related{}), nil
	}
	links, err := s.Graph.RelatedTo(ctx, graphActor(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	related, err := s.readableRelated(ctx, links)
	if err != nil {
		return nil, err
	}
	return ListRelated200JSONResponse(related), nil
}

// readableRelated loads the linked documents the person can read, each with what it names.
func (s *Server) readableRelated(ctx context.Context, links []graph.Link) ([]Related, error) {
	related, err := s.loadRelated(ctx, links)
	if err != nil {
		return nil, err
	}
	docs := make([]Document, len(related))
	for i, r := range related {
		docs[i] = r.Document
	}
	if err := s.fillEntities(ctx, docs); err != nil {
		return nil, err
	}
	for i := range related {
		related[i].Document = docs[i]
	}
	return related, nil
}

// loadRelated reads each linked document as the person and leaves out the ones they cannot see.
func (s *Server) loadRelated(ctx context.Context, links []graph.Link) ([]Related, error) {
	related := []Related{}
	for _, link := range links {
		doc, err := s.Documents.Get(ctx, documentActor(ctx), link.DocumentID)
		if errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		related = append(related, Related{Document: toDocumentJSON(doc), Via: EdgeKind(link.Kind)})
	}
	return related, nil
}

// CorrectMetadata applies a manual correction; it survives re-indexing.
func (s *Server) CorrectMetadata(ctx context.Context, req CorrectMetadataRequestObject) (CorrectMetadataResponseObject, error) {
	doc, err := s.Documents.Correct(ctx, documentActor(ctx), documents.Correction{ID: req.Id, Patch: toPatch(req.Body)})
	if err != nil {
		return nil, err
	}
	docs := []Document{toDocumentJSON(doc)}
	if err := s.fillEntities(ctx, docs); err != nil {
		return nil, err
	}
	return CorrectMetadata200JSONResponse(docs[0]), nil
}

// GetVersionFile streams the PDF of a version; http.ServeContent answers Range requests for pdf.js.
func (s *Server) GetVersionFile(ctx context.Context, req GetVersionFileRequestObject) (GetVersionFileResponseObject, error) {
	file, err := s.Documents.OpenFile(ctx, documentActor(ctx), documents.FileRequest{DocumentID: req.Id, VersionID: req.VersionId})
	if err != nil {
		return nil, err
	}
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return fileResponse{file: file, request: r, disposition: "inline", suffix: ".pdf"}, nil
}

// GetVersionOriginal sends the file as it was read, to be saved rather than shown.
func (s *Server) GetVersionOriginal(ctx context.Context, req GetVersionOriginalRequestObject) (GetVersionOriginalResponseObject, error) {
	file, err := s.Documents.OpenOriginal(ctx, documentActor(ctx), documents.FileRequest{DocumentID: req.Id, VersionID: req.VersionId})
	if err != nil {
		return nil, err
	}
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return fileResponse{file: file, request: r, disposition: "attachment"}, nil
}

// GetCatalog returns the tenant's document and entity types.
func (s *Server) GetCatalog(ctx context.Context, _ GetCatalogRequestObject) (GetCatalogResponseObject, error) {
	snap, err := s.Taxonomy.Snapshot(ctx, principalFrom(ctx).TenantID)
	if err != nil {
		return nil, err
	}
	var out Catalog
	for _, d := range snap.DocTypes {
		out.DocTypes = append(out.DocTypes, struct {
			Band  BandKind `json:"band"`
			Key   string   `json:"key"`
			Label string   `json:"label"`
		}{Band: BandKind(d.Band), Key: d.Key, Label: d.Label})
	}
	for _, e := range snap.EntityTypes {
		out.EntityTypes = append(out.EntityTypes, struct {
			Key   string `json:"key"`
			Label string `json:"label"`
		}{Key: e.Key, Label: e.Label})
	}
	return GetCatalog200JSONResponse(out), nil
}

// fileResponse serves the file itself so Range, If-Modified-Since and the content length work. The viewer's PDF
// is "inline" and gets .pdf added to its name; an original is an "attachment" under its own name.
type fileResponse struct {
	file        documents.ViewerFile
	request     *http.Request
	disposition string
	suffix      string
}

func (f fileResponse) VisitGetVersionFileResponse(w http.ResponseWriter) error { return f.serve(w) }

func (f fileResponse) VisitGetVersionOriginalResponse(w http.ResponseWriter) error { return f.serve(w) }

func (f fileResponse) serve(w http.ResponseWriter) error {
	defer func() { _ = f.file.Close() }()
	w.Header().Set("Content-Type", f.file.Mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(f.disposition, map[string]string{"filename": path.Base(f.file.Name) + f.suffix}))
	http.ServeContent(w, f.request, f.file.Name, f.file.ModTime, f.file)
	return nil
}

func toPatch(b *MetadataPatch) documents.MetadataPatch {
	p := documents.MetadataPatch{Code: b.Code, Title: b.Title, DocType: b.DocType, Department: b.Department,
		Language: b.Language, OwnerID: b.OwnerId}
	if b.Status != nil {
		status := string(*b.Status)
		p.Status = &status
	}
	if b.ApprovedAt != nil {
		p.ApprovedAt = &b.ApprovedAt.Time
	}
	if b.ReviewDueAt != nil {
		p.ReviewDueAt = &b.ReviewDueAt.Time
	}
	if b.Attributes != nil {
		p.Attributes = *b.Attributes
	}
	return p
}

func toDocumentJSON(d documents.Document) Document {
	out := Document{Id: d.ID, SourceId: d.SourceID, SourceKind: DocumentSourceKind(d.SourceKind), CurrentVersionId: d.CurrentVersionID, Code: d.Code, Title: d.Title,
		DocType: d.DocType, Band: BandKind(d.Band), Status: DocStatus(d.Status), Department: d.Department, Path: d.Path,
		Language: d.Language, Summary: d.Summary, OwnerId: d.OwnerID, Attributes: d.Attributes,
		ApprovedAt: dateOf(d.ApprovedAt), ReviewDueAt: dateOf(d.ReviewDueAt),
		Entities: []EntityRef{}, Versions: []DocumentVersion{}, Provenance: []FieldProvenance{}}
	for _, v := range d.Versions {
		out.Versions = append(out.Versions, DocumentVersion{Id: v.ID, Revision: v.Revision,
			State: DocumentVersionState(v.State), IndexedAt: v.IndexedAt, Pages: v.Pages, Preview: v.Preview})
	}
	for _, p := range d.Provenance {
		out.Provenance = append(out.Provenance, FieldProvenance{Field: p.Field, Value: p.Value,
			Source: FieldProvenanceSource(p.Source), Confidence: p.Confidence})
	}
	return out
}

func dateOf(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	return &openapi_types.Date{Time: *t}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefBand(b *ListDocumentsParamsBand) ListDocumentsParamsBand {
	if b == nil {
		return ""
	}
	return *b
}

func derefStatus(s *ListDocumentsParamsStatus) ListDocumentsParamsStatus {
	if s == nil {
		return ""
	}
	return *s
}
