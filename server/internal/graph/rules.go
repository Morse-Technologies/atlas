package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/graph/graphdb"
)

// Rules and severities (ATLAS.md §12). The rest of the Core list needs the entity master, the LLM judge or
// version comparison, which come later.
const (
	RuleNoOwner       = "NO_OWNER"
	RuleReviewOverdue = "REVIEW_OVERDUE"
	RuleSuspect       = "SUSPECT_CONTENT"
	RuleDuplicate     = "DUPLICATE"

	severityHigh   = "high"
	severityMedium = "medium"
	severityLow    = "low"
	// lateEnough is how overdue a review must be to count as high severity.
	lateEnough = 90 * 24 * time.Hour
)

// Evidence points at the passage that triggered an alert.
type Evidence struct {
	DocumentID uuid.UUID `json:"document_id"`
	Page       int       `json:"page"`
	Quote      string    `json:"quote"`
}

// finding is one problem a rule found. The fingerprint identifies it across runs.
type finding struct {
	rule        string
	severity    string
	title       string
	detail      string
	documents   []uuid.UUID
	fingerprint string
	evidence    []Evidence
}

// scope limits a run to the alerts about one document; nil checks everything.
type scope struct{ document *uuid.UUID }

// rule finds the problems of one kind.
type rule struct {
	name string
	find func(ctx context.Context, q *graphdb.Queries, in ruleInput) ([]finding, error)
}

// ruleInput is what a rule needs to decide: where to look and what day it is.
type ruleInput struct {
	scope scope
	today time.Time
}

var rules = []rule{
	{RuleNoOwner, findNoOwner},
	{RuleReviewOverdue, findReviewOverdue},
	{RuleSuspect, findSuspect},
	{RuleDuplicate, findDuplicates},
}

// evaluate runs every rule and stores what they find. An alert not found again is resolved. With a document in
// scope only the alerts about it are touched.
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, sc scope) error {
	q := graphdb.New(tx)
	started := s.Now().UTC()
	for _, r := range rules {
		found, err := r.find(ctx, q, ruleInput{scope: sc, today: started})
		if err != nil {
			return fmt.Errorf("rule %s: %w", r.name, err)
		}
		if err := storeFindings(ctx, q, storing{found: found, seenAt: started}); err != nil {
			return err
		}
		err = q.ResolveUnseen(ctx, graphdb.ResolveUnseenParams{Rule: r.name, RunStarted: started, DocumentID: sc.document})
		if err != nil {
			return fmt.Errorf("resolve %s: %w", r.name, err)
		}
	}
	return nil
}

// Evaluate checks every rule over the whole tenant. It runs nightly, and catches what incremental runs miss.
func (s *Service) Evaluate(ctx context.Context, tenant uuid.UUID) error {
	return s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error { return s.evaluate(ctx, tx, scope{}) })
}

type storing struct {
	found  []finding
	seenAt time.Time
}

func storeFindings(ctx context.Context, q *graphdb.Queries, in storing) error {
	for _, f := range in.found {
		evidence, _ := json.Marshal(nonNilEvidence(f.evidence)) // plain fields: cannot fail
		err := q.UpsertAlert(ctx, graphdb.UpsertAlertParams{Rule: f.rule, Severity: f.severity, Title: f.title, Detail: f.detail,
			DocumentIds: f.documents, Fingerprint: f.fingerprint, Evidence: evidence, SeenAt: in.seenAt})
		if err != nil {
			return fmt.Errorf("store alert: %w", err)
		}
	}
	return nil
}

func nonNilEvidence(e []Evidence) []Evidence {
	if e == nil {
		return []Evidence{}
	}
	return e
}

func label(code, title string) string {
	if code != "" {
		return code
	}
	return title
}

func findNoOwner(ctx context.Context, q *graphdb.Queries, in ruleInput) ([]finding, error) {
	rows, err := q.RuleNoOwner(ctx, in.scope.document)
	if err != nil {
		return nil, err
	}
	out := make([]finding, len(rows))
	for i, r := range rows {
		out[i] = finding{rule: RuleNoOwner, severity: severityLow, title: label(r.Code, r.Title) + " has no owner",
			detail: "Nobody is responsible for keeping this document up to date.", documents: []uuid.UUID{r.ID}, fingerprint: fingerprint(RuleNoOwner, r.ID)}
	}
	return out, nil
}

func findReviewOverdue(ctx context.Context, q *graphdb.Queries, in ruleInput) ([]finding, error) {
	rows, err := q.RuleReviewOverdue(ctx, graphdb.RuleReviewOverdueParams{Today: &in.today, DocumentID: in.scope.document})
	if err != nil {
		return nil, err
	}
	out := make([]finding, len(rows))
	for i, r := range rows {
		severity := severityMedium
		if in.today.Sub(*r.ReviewDueAt) > lateEnough {
			severity = severityHigh
		}
		out[i] = finding{rule: RuleReviewOverdue, severity: severity, title: label(r.Code, r.Title) + " is overdue for review",
			detail: "The review was due on " + r.ReviewDueAt.Format(time.DateOnly) + ".", documents: []uuid.UUID{r.ID}, fingerprint: fingerprint(RuleReviewOverdue, r.ID)}
	}
	return out, nil
}

func findSuspect(ctx context.Context, q *graphdb.Queries, in ruleInput) ([]finding, error) {
	rows, err := q.RuleSuspect(ctx, in.scope.document)
	if err != nil {
		return nil, err
	}
	facts, err := factsOf(ctx, q, suspectIDs(rows))
	if err != nil {
		return nil, err
	}
	out := make([]finding, len(rows))
	for i, r := range rows {
		f := facts[r.DocumentID]
		out[i] = finding{rule: RuleSuspect, severity: severityMedium, title: label(f.code, f.title) + " contains text aimed at an AI",
			detail:    "A passage looks like instructions for an AI assistant. Check the document before people rely on its answers.",
			documents: []uuid.UUID{r.DocumentID}, fingerprint: fingerprint(RuleSuspect, r.DocumentID),
			evidence: []Evidence{{DocumentID: r.DocumentID, Page: int(r.Page), Quote: r.Quote}}}
	}
	return out, nil
}

func suspectIDs(rows []graphdb.RuleSuspectRow) []uuid.UUID {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.DocumentID
	}
	return ids
}

func findDuplicates(ctx context.Context, q *graphdb.Queries, in ruleInput) ([]finding, error) {
	rows, err := q.RuleDuplicates(ctx, in.scope.document)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		ids = append(ids, r.Src, r.Dst)
	}
	facts, err := factsOf(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	out := make([]finding, len(rows))
	for i, r := range rows {
		a := facts[r.Src]
		out[i] = finding{rule: RuleDuplicate, severity: severityMedium, title: label(a.code, a.title) + " exists more than once",
			detail: "Two documents share the same code. People may be reading the wrong one.", documents: []uuid.UUID{r.Src, r.Dst},
			fingerprint: fingerprint(RuleDuplicate, r.Src, r.Dst)}
	}
	return out, nil
}

// docFacts is what the rules and the map print about a document.
type docFacts struct {
	code, title, docType, department string
}

func factsOf(ctx context.Context, q *graphdb.Queries, ids []uuid.UUID) (map[uuid.UUID]docFacts, error) {
	rows, err := q.GetDocFacts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get document facts: %w", err)
	}
	out := make(map[uuid.UUID]docFacts, len(rows))
	for _, r := range rows {
		out[r.ID] = docFacts{code: r.Code, title: r.Title, docType: r.DocType, department: r.Department}
	}
	return out, nil
}

// fingerprint names a problem by its rule and subject documents, so finding it again updates the same alert.
func fingerprint(rule string, subjects ...uuid.UUID) string {
	parts := []string{rule}
	for _, id := range subjects {
		parts = append(parts, id.String())
	}
	return strings.Join(parts, ":")
}
