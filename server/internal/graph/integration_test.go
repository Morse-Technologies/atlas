//go:build integration

package graph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/errs"
)

type world struct {
	t   *testing.T
	ctx context.Context
	svc *Service
	who Actor
	acl uuid.UUID
}

func newWorld(t *testing.T) world {
	t.Helper()
	d := dbtest.Start(t)
	w := world{t: t, ctx: context.Background(), svc: NewService(d)}
	w.who = Actor{TenantID: dbtest.NewTenant(t, d), UserID: uuid.New()}
	w.exec(func(tx pgx.Tx) (err error) { w.acl, err = access.DefaultACLSet(w.ctx, tx); return err })
	return w
}

func (w world) exec(fn func(pgx.Tx) error) {
	w.t.Helper()
	if err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, fn); err != nil {
		w.t.Fatal(err)
	}
}

// document stores a document with one current passage and runs the graph job for it.
func (w world) document(code, text string, owner *uuid.UUID) uuid.UUID {
	w.t.Helper()
	var doc uuid.UUID
	w.exec(func(tx pgx.Tx) error {
		var version uuid.UUID
		if err := tx.QueryRow(w.ctx, `INSERT INTO documents.documents (source_id, external_id, title, code, owner_id, status) VALUES ($1, $2, $3, $3, $4, 'current') RETURNING id`,
			uuid.New(), uuid.NewString(), code, owner).Scan(&doc); err != nil {
			return err
		}
		if err := tx.QueryRow(w.ctx, `INSERT INTO documents.document_versions (document_id, content_hash, mime, size, state) VALUES ($1, $2, 'application/pdf', 1, 'parsed') RETURNING id`,
			doc, uuid.NewString()).Scan(&version); err != nil {
			return err
		}
		if _, err := tx.Exec(w.ctx, `UPDATE documents.documents SET current_version_id = $1 WHERE id = $2`, version, doc); err != nil {
			return err
		}
		_, err := tx.Exec(w.ctx, `INSERT INTO documents.chunks (id, version_id, document_id, ord, page, text, tokens, is_current, acl_set_id) VALUES ($1, $2, $3, 0, 1, $4, 5, true, $5)`,
			uuid.New(), version, doc, text, w.acl)
		return err
	})
	if err := w.svc.OnVersionIndexed(w.ctx, events.VersionIndexed{TenantID: w.who.TenantID, DocumentID: doc}); err != nil {
		w.t.Fatal(err)
	}
	return doc
}

func (w world) alerts() []Alert {
	w.t.Helper()
	list, _, err := w.svc.Alerts(w.ctx, w.who, AlertPage{Size: 100})
	if err != nil {
		w.t.Fatal(err)
	}
	return list
}

func (w world) alertOf(rule string) (Alert, bool) {
	for _, a := range w.alerts() {
		if a.Rule == rule {
			return a, true
		}
	}
	return Alert{}, false
}

func TestADocumentNamingAnotherCodeIsLinkedToIt_InEitherOrderOfIndexing(t *testing.T) {
	w := newWorld(t)
	owner := uuid.New()
	a := w.document("PNT-001", "Antes de empezar lea PNT-002.", &owner) // names PNT-002 before it exists
	b := w.document("PNT-002", "Procedimiento de ajuste.", &owner)
	m, err := w.svc.Neighborhood(w.ctx, w.who, Query{NodeID: prefixDocument + a.String(), Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if nodesByKind(m, KindDocument) != 2 || len(m.Edges) == 0 {
		t.Fatalf("map %+v; %s should be linked to %s", m, a, b)
	}
}

func TestStandardsBecomeEntitiesOfTheDocument(t *testing.T) {
	w := newWorld(t)
	owner := uuid.New()
	doc := w.document("PNT-001", "Conforme a ISO 9001:2015.", &owner)
	found, err := w.svc.EntitiesOf(w.ctx, w.who.TenantID, []uuid.UUID{doc})
	if err != nil || len(found[doc]) != 1 || found[doc][0].Label != "ISO 9001:2015" {
		t.Fatalf("entities %+v, %v", found, err)
	}
}

func TestSameCodeRaisesOneDuplicateAlert(t *testing.T) {
	w := newWorld(t)
	owner := uuid.New()
	w.document("PNT-001", "uno", &owner)
	w.document("PNT-001", "dos", &owner)
	n := 0
	for _, a := range w.alerts() {
		if a.Rule == RuleDuplicate {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d duplicate alerts, want 1 for the pair", n)
	}
}

// A person who corrects a code makes documents duplicates (or stops them being): the links must follow.
func TestCorrectingACodeLinksTheDocumentsThatNowShareIt(t *testing.T) {
	w := newWorld(t)
	owner := uuid.New()
	w.document("PNT-001", "uno", &owner)
	other := w.document("PNT-002", "dos", &owner)
	w.exec(func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, `UPDATE documents.documents SET code = 'PNT-001' WHERE id = $1`, other)
		return err
	})
	if err := w.svc.OnDocumentChanged(w.ctx, events.DocumentChanged{TenantID: w.who.TenantID, DocumentID: other}); err != nil {
		t.Fatal(err)
	}
	if a, ok := w.alertOf(RuleDuplicate); !ok || a.Status != StatusOpen {
		t.Fatalf("alert %+v, found %v; the corrected code makes the pair duplicates", a, ok)
	}
}

func TestNoOwnerAlertClosesWhenAnOwnerIsSet(t *testing.T) {
	w := newWorld(t)
	doc := w.document("PNT-001", "texto", nil)
	if a, ok := w.alertOf(RuleNoOwner); !ok || a.Status != StatusOpen {
		t.Fatalf("alert %+v, found %v", a, ok)
	}
	w.exec(func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, `UPDATE documents.documents SET owner_id = $1 WHERE id = $2`, uuid.New(), doc)
		return err
	})
	if err := w.svc.OnDocumentChanged(w.ctx, events.DocumentChanged{TenantID: w.who.TenantID, DocumentID: doc}); err != nil {
		t.Fatal(err)
	}
	if a, _ := w.alertOf(RuleNoOwner); a.Status != StatusResolved {
		t.Fatalf("status %s, want resolved", a.Status)
	}
}

func TestFindingTheSameProblemAgainDoesNotCreateASecondAlert(t *testing.T) {
	w := newWorld(t)
	w.document("PNT-001", "texto", nil)
	_ = w.svc.Evaluate(w.ctx, w.who.TenantID)
	_ = w.svc.Evaluate(w.ctx, w.who.TenantID)
	if n := len(w.alerts()); n != 1 {
		t.Fatalf("%d alerts, want 1", n)
	}
}

func TestADismissedAlertStaysDismissedWhenTheProblemIsFoundAgain(t *testing.T) {
	w := newWorld(t)
	w.document("PNT-001", "texto", nil)
	a, _ := w.alertOf(RuleNoOwner)
	status, note := StatusDismissed, "owned by the whole team"
	if _, err := w.svc.UpdateAlert(w.ctx, w.who, AlertChange{ID: a.ID, Status: &status, Note: &note}); err != nil {
		t.Fatal(err)
	}
	_ = w.svc.Evaluate(w.ctx, w.who.TenantID)
	if got, _ := w.alertOf(RuleNoOwner); got.Status != StatusDismissed {
		t.Fatalf("status %s, want dismissed", got.Status)
	}
}

func TestDismissingNeedsAReasonAndAnAssigneeMustExist(t *testing.T) {
	w := newWorld(t)
	w.document("PNT-001", "texto", nil)
	a, _ := w.alertOf(RuleNoOwner)
	status := StatusDismissed
	if _, err := w.svc.UpdateAlert(w.ctx, w.who, AlertChange{ID: a.ID, Status: &status}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("dismiss without a reason = %v, want ErrInvalid", err)
	}
	ghost := uuid.New()
	if _, err := w.svc.UpdateAlert(w.ctx, w.who, AlertChange{ID: a.ID, AssigneeID: &ghost}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("unknown assignee = %v, want ErrInvalid", err)
	}
}

func TestARejectedLinkIsNotDrawnAndNotRecreatedByARescan(t *testing.T) {
	w := newWorld(t)
	owner := uuid.New()
	a := w.document("PNT-001", "Ver PNT-002.", &owner)
	w.document("PNT-002", "texto", &owner)
	m, _ := w.svc.Neighborhood(w.ctx, w.who, Query{NodeID: prefixDocument + a.String()})
	if len(m.Edges) == 0 {
		t.Fatal("expected a link to reject")
	}
	if err := w.svc.DecideEdge(w.ctx, w.who, Decision{EdgeID: m.Edges[0].ID, Status: "rejected"}); err != nil {
		t.Fatal(err)
	}
	_ = w.svc.OnVersionIndexed(w.ctx, events.VersionIndexed{TenantID: w.who.TenantID, DocumentID: a})
	again, _ := w.svc.Neighborhood(w.ctx, w.who, Query{NodeID: prefixDocument + a.String()})
	for _, e := range again.Edges {
		if e.Kind == "references" {
			t.Fatalf("the rejected link came back: %+v", e)
		}
	}
}

func TestReviewOverdueIsHighWhenItIsVeryLate(t *testing.T) {
	w := newWorld(t)
	owner := uuid.New()
	doc := w.document("PNT-001", "texto", &owner)
	w.exec(func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, `UPDATE documents.documents SET review_due_at = $1 WHERE id = $2`, time.Now().AddDate(0, 0, -200), doc)
		return err
	})
	_ = w.svc.Evaluate(w.ctx, w.who.TenantID)
	if a, ok := w.alertOf(RuleReviewOverdue); !ok || a.Severity != "high" {
		t.Fatalf("alert %+v, found %v", a, ok)
	}
}

func TestSuspectPassagesRaiseAnAlertWithTheirEvidence(t *testing.T) {
	w := newWorld(t)
	owner := uuid.New()
	doc := w.document("PNT-001", "texto", &owner)
	w.exec(func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, `UPDATE documents.chunks SET suspect_injection = true WHERE document_id = $1`, doc)
		return err
	})
	_ = w.svc.Evaluate(w.ctx, w.who.TenantID)
	if a, ok := w.alertOf(RuleSuspect); !ok || len(a.Evidence) != 1 || a.Evidence[0].DocumentID != doc {
		t.Fatalf("alert %+v, found %v", a, ok)
	}
}

func TestAnotherTenantSeesNoAlertsOrLinks(t *testing.T) {
	w := newWorld(t)
	w.document("PNT-001", "texto", nil)
	other := Actor{TenantID: dbtest.NewTenant(t, w.svc.DB), UserID: uuid.New()}
	list, _, err := w.svc.Alerts(w.ctx, other, AlertPage{Size: 10})
	if err != nil || len(list) != 0 {
		t.Fatalf("alerts %+v, %v; RLS must hide them", list, err)
	}
}

// An alert, a map node or a count about a document a person may not read would reveal that it exists.
func TestADocumentOfARestrictedSourceIsNotOnOthersAlertsOrMap(t *testing.T) {
	w := newWorld(t)
	reader, other := uuid.New(), uuid.New()
	var set uuid.UUID
	w.exec(func(tx pgx.Tx) error {
		return tx.QueryRow(w.ctx, `INSERT INTO access.acl_sets (origin, allow) VALUES ('source', $1) RETURNING id`, []uuid.UUID{reader}).Scan(&set)
	})
	// Two documents with one code are duplicates: they are linked, so they are on the map, and they have no owner.
	first, second := w.document("PNT-777", "salary bands", nil), w.document("PNT-777", "salary bands copy", nil)
	w.exec(func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, `UPDATE documents.documents SET acl_set_id = $1 WHERE id = ANY($2)`, set, []uuid.UUID{first, second})
		return err
	})
	as := func(user uuid.UUID) Actor { return Actor{TenantID: w.who.TenantID, UserID: user} }
	seen := func(user uuid.UUID) (alerts, nodes int) {
		list, _, err := w.svc.Alerts(w.ctx, as(user), AlertPage{Size: 100})
		if err != nil {
			t.Fatal(err)
		}
		m, err := w.svc.Neighborhood(w.ctx, as(user), Query{})
		if err != nil {
			t.Fatal(err)
		}
		return len(list), len(m.Nodes)
	}
	if alerts, nodes := seen(reader); alerts == 0 || nodes == 0 {
		t.Fatalf("the reader sees %d alerts and %d nodes; want both", alerts, nodes)
	}
	if alerts, nodes := seen(other); alerts != 0 || nodes != 0 {
		t.Fatalf("someone else sees %d alerts and %d nodes; the document must not exist for them", alerts, nodes)
	}
}
