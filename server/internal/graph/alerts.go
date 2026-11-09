package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/graph/graphdb"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Alert statuses.
const (
	StatusOpen      = "open"
	StatusAck       = "ack"
	StatusResolved  = "resolved"
	StatusDismissed = "dismissed"
)

var alertStatuses = map[string]bool{StatusOpen: true, StatusAck: true, StatusResolved: true, StatusDismissed: true}

// Alert is a problem a curator should look at.
type Alert struct {
	ID          uuid.UUID
	Rule        string
	Severity    string
	Status      string
	Title       string
	Detail      string
	DocumentIDs []uuid.UUID
	AssigneeID  *uuid.UUID
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	Evidence    []Evidence
}

// AlertPage selects alerts older than Before (uuid.Nil: the newest).
type AlertPage struct {
	Before uuid.UUID
	Size   int32
}

// Alerts returns one page of alerts, newest first, and the cursor of the next page (uuid.Nil: none).
func (s *Service) Alerts(ctx context.Context, who Actor, page AlertPage) ([]Alert, uuid.UUID, error) {
	before := page.Before
	if before == uuid.Nil {
		before = uuid.Max
	}
	var rows []graphdb.GraphAlert
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var listErr error
		rows, listErr = graphdb.New(tx).ListAlertsPage(ctx, graphdb.ListAlertsPageParams{Before: before, PageSize: page.Size + 1})
		return listErr
	})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("list alerts: %w", err)
	}
	next := uuid.Nil
	if int32(len(rows)) > page.Size {
		rows, next = rows[:page.Size], rows[page.Size-1].ID
	}
	return toAlerts(rows), next, nil
}

func toAlerts(rows []graphdb.GraphAlert) []Alert {
	out := make([]Alert, len(rows))
	for i, r := range rows {
		out[i] = toAlert(r)
	}
	return out
}

func toAlert(r graphdb.GraphAlert) Alert {
	evidence := []Evidence{}
	_ = json.Unmarshal(r.Evidence, &evidence) // written by this package as a list of Evidence
	return Alert{ID: r.ID, Rule: r.Rule, Severity: r.Severity, Status: r.Status, Title: r.Title, Detail: r.Detail,
		DocumentIDs: r.DocumentIds, AssigneeID: r.AssigneeID, FirstSeenAt: r.FirstSeenAt, LastSeenAt: r.LastSeenAt, Evidence: evidence}
}

// AlertChange is what a curator does to an alert. A nil field is left alone.
// shortcut: an assignee can be set but not cleared (the API cannot tell "absent" from null).
type AlertChange struct {
	ID         uuid.UUID
	Status     *string
	AssigneeID *uuid.UUID
	Note       *string
}

// UpdateAlert changes an alert's status or assignee. Dismissing needs a reason. The audit log gets the new
// status, never the reason, which is free text.
func (s *Service) UpdateAlert(ctx context.Context, who Actor, c AlertChange) (Alert, error) {
	var out Alert
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var saveErr error
		out, saveErr = s.saveAlert(ctx, tx, changing{who: who, change: c})
		return saveErr
	})
	return out, err
}

type changing struct {
	who    Actor
	change AlertChange
}

func (s *Service) saveAlert(ctx context.Context, tx pgx.Tx, in changing) (Alert, error) {
	q := graphdb.New(tx)
	row, err := q.LockAlert(ctx, in.change.ID)
	if err != nil {
		return Alert{}, notFound(err, "alert")
	}
	if err := checkChange(ctx, tx, pending{change: in.change, current: row}); err != nil {
		return Alert{}, err
	}
	next := mergeChange(row, in.change)
	err = q.SaveAlert(ctx, graphdb.SaveAlertParams{ID: next.ID, Status: next.Status, AssigneeID: next.AssigneeID, ResolutionNote: next.ResolutionNote})
	if err != nil {
		return Alert{}, fmt.Errorf("save alert: %w", err)
	}
	entry := audit.Entry{At: s.Now(), Actor: in.who.UserID, Action: "alert.updated", Subject: "alert:" + next.ID.String(), Detail: "status=" + next.Status}
	return toAlert(next), audit.Record(ctx, tx, entry)
}

func mergeChange(cur graphdb.GraphAlert, c AlertChange) graphdb.GraphAlert {
	if c.Status != nil {
		cur.Status = *c.Status
	}
	if c.AssigneeID != nil {
		cur.AssigneeID = c.AssigneeID
	}
	if c.Note != nil {
		cur.ResolutionNote = *c.Note
	}
	return cur
}

// pending is a change and the alert it applies to.
type pending struct {
	change  AlertChange
	current graphdb.GraphAlert
}

// checkChange refuses an unknown status, a dismissal without a reason and an assignee who is not a user.
func checkChange(ctx context.Context, tx pgx.Tx, in pending) error {
	c := in.change
	next := mergeChange(in.current, c)
	switch {
	case !alertStatuses[next.Status]:
		return fmt.Errorf("unknown status %q: %w", next.Status, errs.ErrInvalid)
	case next.Status == StatusDismissed && next.ResolutionNote == "":
		return fmt.Errorf("dismissing an alert needs a reason: %w", errs.ErrInvalid)
	case c.AssigneeID == nil:
		return nil
	}
	found, err := access.UserExists(ctx, tx, *c.AssigneeID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("the assignee is not a user of this organization: %w", errs.ErrInvalid)
	}
	return nil
}

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, errs.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// OpenAlerts counts the open alerts by severity, for the panel.
func (s *Service) OpenAlerts(ctx context.Context, scope db.Scope) (map[string]int, error) {
	counts := map[string]int{severityHigh: 0, severityMedium: 0, severityLow: 0}
	err := s.DB.InScopeTx(ctx, scope, func(tx pgx.Tx) error {
		rows, err := graphdb.New(tx).CountOpenAlerts(ctx)
		for _, r := range rows {
			counts[r.Severity] = int(r.Total)
		}
		return err
	})
	return counts, err
}

// Entity is something a document names, such as a standard.
type Entity struct {
	Type  string
	Label string
}

// EntitiesOf returns what each document names, other than document codes.
func (s *Service) EntitiesOf(ctx context.Context, tenant uuid.UUID, documents []uuid.UUID) (map[uuid.UUID][]Entity, error) {
	out := map[uuid.UUID][]Entity{}
	if len(documents) == 0 {
		return out, nil
	}
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := graphdb.New(tx).ListMentionsOf(ctx, documents)
		for _, r := range rows {
			out[r.DocumentID] = append(out[r.DocumentID], Entity{Type: r.EntityType, Label: r.Label})
		}
		return err
	})
	return out, err
}
