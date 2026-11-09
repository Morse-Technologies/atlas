package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/jobs"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

const dateLayout = time.DateOnly

var documentStatuses = map[string]bool{"current": true, "draft": true, "obsolete": true, "unknown": true}

// MetadataPatch is what a person corrects. A nil field is left alone.
// shortcut: a field can be set but not cleared (the API cannot tell "absent" from null); add it if people ask.
type MetadataPatch struct {
	Code        *string
	Title       *string
	DocType     *string
	Status      *string
	Department  *string
	Language    *string
	OwnerID     *uuid.UUID
	ApprovedAt  *time.Time
	ReviewDueAt *time.Time
	Attributes  map[string]string // replaces the whole set when not nil
}

// fieldValue is one corrected field, as text, for the provenance record.
type fieldValue struct{ field, value string }

// Correct applies a manual correction. Each changed field is recorded with source "manual", so later
// extraction does not overwrite it, and the change is written to the audit log in the same transaction.
func (l *Library) Correct(ctx context.Context, who Actor, c Correction) (Document, error) {
	err := l.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		return l.correctIn(ctx, tx, scoped{who: who, Correction: c})
	})
	if err != nil {
		return Document{}, err
	}
	return l.Get(ctx, who, c.ID)
}

// Correction is a patch for one document.
type Correction struct {
	ID    uuid.UUID
	Patch MetadataPatch
}

// scoped is a correction with the person who makes it.
type scoped struct {
	who Actor
	Correction
}

func (l *Library) correctIn(ctx context.Context, tx pgx.Tx, c scoped) error {
	q := documentsdb.New(tx)
	cur, err := q.GetDocument(ctx, c.ID)
	if err != nil {
		return notFound(err, "document")
	}
	params, changed, err := checkedPatch(ctx, tx, patchOf{cur: cur, patch: c.Patch})
	if err != nil || len(changed) == 0 {
		return err
	}
	if err := saveCorrection(ctx, q, saved{params: params, changed: changed}); err != nil {
		return err
	}
	err = audit.Record(ctx, tx, audit.Entry{At: l.Now(), Actor: c.who.UserID, Action: "metadata.corrected",
		Subject: "document:" + c.ID.String(), Detail: "fields=" + joinFields(changed)})
	if err != nil {
		return err
	}
	return jobs.Enqueue(ctx, tx, events.DocumentChanged{TenantID: c.who.TenantID, DocumentID: c.ID})
}

// patchOf is a patch and the row it applies to.
type patchOf struct {
	cur   documentsdb.DocumentsDocument
	patch MetadataPatch
}

// checkedPatch validates the patch against the catalog and the people of the tenant, then applies it.
func checkedPatch(ctx context.Context, tx pgx.Tx, in patchOf) (documentsdb.UpdateMetadataParams, []fieldValue, error) {
	tax, err := catalog.Load(ctx, tx)
	if err != nil {
		return documentsdb.UpdateMetadataParams{}, nil, err
	}
	if err := ownerMustExist(ctx, tx, in.patch.OwnerID); err != nil {
		return documentsdb.UpdateMetadataParams{}, nil, err
	}
	return applyPatch(in.cur, in.patch, tax)
}

// saved is the new row and the fields to mark as manual.
type saved struct {
	params  documentsdb.UpdateMetadataParams
	changed []fieldValue
}

func saveCorrection(ctx context.Context, q *documentsdb.Queries, s saved) error {
	if err := q.UpdateMetadata(ctx, s.params); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	for _, f := range s.changed {
		err := q.SetManualProvenance(ctx, documentsdb.SetManualProvenanceParams{DocumentID: s.params.ID, Field: f.field, Value: f.value})
		if err != nil {
			return fmt.Errorf("record provenance of %s: %w", f.field, err)
		}
	}
	return nil
}

// textField is one text column a patch may set: its name, the new value, the current one, and where to write.
type textField struct {
	name string
	next *string
	cur  string
	dst  *string
}

// applyPatch merges patch into the current row. It returns the full update, and the fields that really changed.
func applyPatch(cur documentsdb.DocumentsDocument, p MetadataPatch, tax catalog.Snapshot) (documentsdb.UpdateMetadataParams, []fieldValue, error) {
	out := documentsdb.UpdateMetadataParams{ID: cur.ID, Title: cur.Title, Code: cur.Code, DocType: cur.DocType,
		Status: cur.Status, Department: cur.Department, OwnerID: cur.OwnerID, ApprovedAt: cur.ApprovedAt,
		ReviewDueAt: cur.ReviewDueAt, Language: cur.Language, Attributes: cur.Attributes}
	if err := validatePatch(p, tax); err != nil {
		return out, nil, err
	}
	changed := applyText(textFields(cur, p, &out))
	changed = append(changed, applyOwner(&out, cur, p)...)
	changed = append(changed, applyDates(&out, cur, p)...)
	attrs, attrChanges, err := applyAttributes(cur.Attributes, p.Attributes)
	out.Attributes = attrs
	return out, append(changed, attrChanges...), err
}

func textFields(cur documentsdb.DocumentsDocument, p MetadataPatch, out *documentsdb.UpdateMetadataParams) []textField {
	return []textField{
		{"title", p.Title, cur.Title, &out.Title}, {"code", p.Code, cur.Code, &out.Code},
		{"doc_type", p.DocType, cur.DocType, &out.DocType}, {"status", p.Status, cur.Status, &out.Status},
		{"department", p.Department, cur.Department, &out.Department}, {"language", p.Language, cur.Language, &out.Language},
	}
}

func applyText(fields []textField) []fieldValue {
	var changed []fieldValue
	for _, f := range fields {
		if f.next == nil {
			continue
		}
		if *f.next != f.cur {
			changed = append(changed, fieldValue{f.name, *f.next})
		}
		*f.dst = *f.next
	}
	return changed
}

func applyOwner(out *documentsdb.UpdateMetadataParams, cur documentsdb.DocumentsDocument, p MetadataPatch) []fieldValue {
	if p.OwnerID == nil {
		return nil
	}
	out.OwnerID = p.OwnerID
	if cur.OwnerID != nil && *cur.OwnerID == *p.OwnerID {
		return nil
	}
	return []fieldValue{{"owner_id", p.OwnerID.String()}}
}

func applyDates(out *documentsdb.UpdateMetadataParams, cur documentsdb.DocumentsDocument, p MetadataPatch) []fieldValue {
	var changed []fieldValue
	if p.ApprovedAt != nil {
		if cur.ApprovedAt == nil || !cur.ApprovedAt.Equal(*p.ApprovedAt) {
			changed = append(changed, fieldValue{"approved_at", p.ApprovedAt.Format(dateLayout)})
		}
		out.ApprovedAt = p.ApprovedAt
	}
	if p.ReviewDueAt != nil {
		if cur.ReviewDueAt == nil || !cur.ReviewDueAt.Equal(*p.ReviewDueAt) {
			changed = append(changed, fieldValue{"review_due_at", p.ReviewDueAt.Format(dateLayout)})
		}
		out.ReviewDueAt = p.ReviewDueAt
	}
	return changed
}

// applyAttributes replaces the attribute set and reports each key whose value differs.
func applyAttributes(current []byte, next map[string]string) ([]byte, []fieldValue, error) {
	if next == nil {
		return current, nil, nil
	}
	var old map[string]string
	_ = json.Unmarshal(current, &old)
	var changed []fieldValue
	for k, v := range next {
		if old[k] != v {
			changed = append(changed, fieldValue{"attributes." + k, v})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].field < changed[j].field })
	raw, err := json.Marshal(next)
	return raw, changed, err
}

func validatePatch(p MetadataPatch, tax catalog.Snapshot) error {
	switch {
	case p.Title != nil && strings.TrimSpace(*p.Title) == "":
		return fmt.Errorf("the title cannot be empty: %w", errs.ErrInvalid)
	case p.Status != nil && !documentStatuses[*p.Status]:
		return fmt.Errorf("unknown status %q: %w", *p.Status, errs.ErrInvalid)
	case p.DocType != nil && !tax.Has(*p.DocType):
		return fmt.Errorf("unknown document type %q: %w", *p.DocType, errs.ErrInvalid)
	}
	return nil
}

func joinFields(changed []fieldValue) string {
	names := make([]string, len(changed))
	for i, f := range changed {
		names[i] = f.field
	}
	return strings.Join(names, ",")
}

func notFound(err error, what string) error { return db.NotFoundIfNoRows(err, what) }

// ownerMustExist refuses an owner that is not an account of the tenant.
func ownerMustExist(ctx context.Context, tx pgx.Tx, owner *uuid.UUID) error {
	if owner == nil {
		return nil
	}
	found, err := access.UserExists(ctx, tx, *owner)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("owner %s is not a user of this organization: %w", owner, errs.ErrInvalid)
	}
	return nil
}
