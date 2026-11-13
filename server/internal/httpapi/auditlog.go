package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/signing"
)

const (
	decimal = 10
	bitSize = 64
)

// AuditLog is what the HTTP layer needs from the audit module. *audit.Service implements it.
type AuditLog interface {
	List(ctx context.Context, tenant uuid.UUID, page audit.Page) ([]audit.Event, int64, error)
	Verify(ctx context.Context, tenant uuid.UUID) (audit.Verification, error)
	Export(ctx context.Context, req audit.ExportRequest, w io.Writer) (audit.Digest, error)
	Append(ctx context.Context, tenant uuid.UUID, entry audit.Entry) error
}

// SigningKeys is what the HTTP layer needs from platform/signing. *signing.Keys implements it.
type SigningKeys interface {
	SignDigest(ctx context.Context, tenant uuid.UUID, digest []byte) (signing.Signature, error)
	PublicKey(ctx context.Context, tenant uuid.UUID) (signing.PublicKey, error)
}

// ListAuditEvents returns one page of the audit log, newest first.
func (s *Server) ListAuditEvents(ctx context.Context, req ListAuditEventsRequestObject) (ListAuditEventsResponseObject, error) {
	page, err := auditPageFrom(req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	events, next, err := s.Audit.List(ctx, principalFrom(ctx).TenantID, page)
	if err != nil {
		return nil, err
	}
	body := make([]AuditEvent, len(events))
	for i, e := range events {
		body[i] = AuditEvent{Id: e.ID, At: e.At, ActorId: e.ActorID, Action: e.Action, Subject: e.Subject,
			Detail: e.Detail, Hash: e.Hash, PrevHash: e.PrevHash}
	}
	return ListAuditEvents200JSONResponse{Body: body, Headers: ListAuditEvents200ResponseHeaders{NextCursor: auditCursor(next)}}, nil
}

// VerifyAudit walks the hash chain and says where it breaks, if it does.
func (s *Server) VerifyAudit(ctx context.Context, _ VerifyAuditRequestObject) (VerifyAuditResponseObject, error) {
	v, err := s.Audit.Verify(ctx, principalFrom(ctx).TenantID)
	if err != nil {
		return nil, err
	}
	return VerifyAudit200JSONResponse{Ok: v.OK, Checked: int(v.Checked), BrokenAt: v.BrokenAt}, nil
}

// auditPageFrom clamps the page size and decodes the cursor, which is the id of the last event seen.
func auditPageFrom(limit *Limit, cursor *Cursor) (audit.Page, error) {
	page := audit.Page{Size: pageSize(limit)}
	if cursor == nil || *cursor == "" {
		return page, nil
	}
	before, err := strconv.ParseInt(*cursor, decimal, bitSize)
	if err != nil || before < 1 {
		return page, fmt.Errorf("cursor: %w", errs.ErrInvalid)
	}
	page.Before = before
	return page, nil
}

func auditCursor(next int64) *string {
	if next == 0 {
		return nil
	}
	s := strconv.FormatInt(next, decimal)
	return &s
}

// ExportAudit writes the events of a range to a temporary file, signs it and sends both. The file is a stream to
// the signature: it is hashed as it is written, never held in memory.
func (s *Server) ExportAudit(ctx context.Context, req ExportAuditRequestObject) (ExportAuditResponseObject, error) {
	p := principalFrom(ctx)
	format := audit.FormatCSV
	if req.Params.Format != nil {
		format = string(*req.Params.Format)
	}
	days := audit.Range{From: req.Params.From.Time, To: req.Params.To.Time}
	if days.To.Before(days.From) {
		return nil, fmt.Errorf("the report ends before it starts: %w", errs.ErrInvalid)
	}
	report, err := s.signedReport(ctx, audit.ExportRequest{Tenant: p.TenantID, Actor: p.UserID, Range: days, Format: format})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func (s *Server) signedReport(ctx context.Context, req audit.ExportRequest) (*reportResponse, error) {
	file, err := os.CreateTemp("", "atlas-audit-*")
	if err != nil {
		return nil, fmt.Errorf("create report file: %w", err)
	}
	report := &reportResponse{file: file, name: reportName(req), contentType: reportContentTypes[req.Format]}
	digest, err := s.Audit.Export(ctx, req, file)
	if err == nil {
		report.signature, err = s.Signing.SignDigest(ctx, req.Tenant, digest.Sum)
	}
	if err != nil {
		report.discard()
		return nil, err
	}
	return report, nil
}

func reportName(req audit.ExportRequest) string {
	day := func(t time.Time) string { return t.Format(time.DateOnly) }
	return fmt.Sprintf("atlas-audit-%s-%s.%s", day(req.Range.From), day(req.Range.To), req.Format)
}

// reportContentTypes maps the export formats to the type the file is sent as.
var reportContentTypes = map[string]string{audit.FormatCSV: "text/csv", audit.FormatJSONL: "application/x-ndjson"}

// reportResponse sends the temporary file with its signature and removes it afterwards.
type reportResponse struct {
	file        *os.File
	name        string
	contentType string
	signature   signing.Signature
}

func (r *reportResponse) discard() {
	_ = r.file.Close()
	_ = os.Remove(r.file.Name())
}

func (r *reportResponse) VisitExportAuditResponse(w http.ResponseWriter) error {
	defer r.discard()
	if _, err := r.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", r.contentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": r.name}))
	h.Set("Atlas-Signature", base64.StdEncoding.EncodeToString(r.signature.Value))
	h.Set("Atlas-Signature-Key-Id", r.signature.KeyID)
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, r.file)
	return err
}

// GetSigningKey is the public key that checks the reports this installation signs.
func (s *Server) GetSigningKey(ctx context.Context, _ GetSigningKeyRequestObject) (GetSigningKeyResponseObject, error) {
	key, err := s.Signing.PublicKey(ctx, principalFrom(ctx).TenantID)
	if err != nil {
		return nil, err
	}
	return GetSigningKey200JSONResponse{KeyId: key.ID, PublicKey: base64.StdEncoding.EncodeToString(key.Key), Algorithm: SigningKeyAlgorithm(signing.Algorithm)}, nil
}
