package httpapi

import (
	"context"
	"mime"
	"net/http"
	"path"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents"
)

// DocumentLibrary is what the HTTP layer needs from the documents module. *documents.Library implements it.
type DocumentLibrary interface {
	List(ctx context.Context, who documents.Actor, in documents.Listing) ([]documents.Document, uuid.UUID, error)
	Get(ctx context.Context, who documents.Actor, id uuid.UUID) (documents.Document, error)
	Correct(ctx context.Context, who documents.Actor, c documents.Correction) (documents.Document, error)
	OpenFile(ctx context.Context, who documents.Actor, req documents.FileRequest) (documents.ViewerFile, error)
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

// ListRelated returns documents linked to this one. Links come from the graph module, which is not built
// yet, so the answer is an empty list for a document that exists.
func (s *Server) ListRelated(ctx context.Context, req ListRelatedRequestObject) (ListRelatedResponseObject, error) {
	if _, err := s.Documents.Get(ctx, documentActor(ctx), req.Id); err != nil {
		return nil, err
	}
	return ListRelated200JSONResponse([]Related{}), nil
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
	return pdfResponse{file: file, request: r}, nil
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

// pdfResponse serves the file itself so Range, If-Modified-Since and the content length work.
type pdfResponse struct {
	file    documents.ViewerFile
	request *http.Request
}

func (p pdfResponse) VisitGetVersionFileResponse(w http.ResponseWriter) error {
	defer func() { _ = p.file.Close() }()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": path.Base(p.file.Name) + ".pdf"}))
	http.ServeContent(w, p.request, p.file.Name, p.file.ModTime, p.file)
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
	out := Document{Id: d.ID, SourceId: d.SourceID, CurrentVersionId: d.CurrentVersionID, Code: d.Code, Title: d.Title,
		DocType: d.DocType, Band: BandKind(d.Band), Status: DocStatus(d.Status), Department: d.Department, Path: d.Path,
		Language: d.Language, Summary: d.Summary, OwnerId: d.OwnerID, Attributes: d.Attributes,
		ApprovedAt: dateOf(d.ApprovedAt), ReviewDueAt: dateOf(d.ReviewDueAt),
		Entities: []EntityRef{}, Versions: []DocumentVersion{}, Provenance: []FieldProvenance{}}
	for _, v := range d.Versions {
		out.Versions = append(out.Versions, DocumentVersion{Id: v.ID, Revision: v.Revision,
			State: DocumentVersionState(v.State), IndexedAt: v.IndexedAt, Pages: v.Pages})
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
