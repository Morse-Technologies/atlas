package httpapi

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const (
	decimal = 10
	bitSize = 64
)

// AuditLog is what the HTTP layer needs from the audit module. *audit.Service implements it.
type AuditLog interface {
	List(ctx context.Context, tenant uuid.UUID, page audit.Page) ([]audit.Event, int64, error)
	Verify(ctx context.Context, tenant uuid.UUID) (audit.Verification, error)
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
