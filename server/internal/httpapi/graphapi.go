package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/graph"
)

// GraphView is what the HTTP layer needs from the graph module. *graph.Service implements it.
type GraphView interface {
	Neighborhood(ctx context.Context, who graph.Actor, q graph.Query) (graph.Map, error)
	Alerts(ctx context.Context, who graph.Actor, page graph.AlertPage) ([]graph.Alert, uuid.UUID, error)
	UpdateAlert(ctx context.Context, who graph.Actor, c graph.AlertChange) (graph.Alert, error)
	DecideEdge(ctx context.Context, who graph.Actor, d graph.Decision) error
	EntitiesOf(ctx context.Context, tenant uuid.UUID, documents []uuid.UUID) (map[uuid.UUID][]graph.Entity, error)
}

func graphActor(ctx context.Context) graph.Actor {
	p := principalFrom(ctx)
	return graph.Actor{TenantID: p.TenantID, UserID: p.UserID}
}

// GetGraph returns the documents around a node, or the most connected ones.
func (s *Server) GetGraph(ctx context.Context, req GetGraphRequestObject) (GetGraphResponseObject, error) {
	q := graph.Query{NodeID: deref(req.Params.NodeId)}
	if req.Params.Depth != nil {
		q.Depth = *req.Params.Depth
	}
	m, err := s.Graph.Neighborhood(ctx, graphActor(ctx), q)
	if err != nil {
		return nil, err
	}
	out, err := viaJSON[Graph](graphJSON(m))
	return GetGraph200JSONResponse(out), err
}

// mapJSON is the wire form of a graph.Map. The generated Graph type holds anonymous structs, so the map is
// built in this named form and converted by its JSON tags.
type mapJSON struct {
	Nodes []nodeJSON `json:"nodes"`
	Edges []edgeJSON `json:"edges"`
}

type nodeJSON struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Label      string  `json:"label"`
	Ref        *string `json:"ref"`
	Band       string  `json:"band,omitempty"`
	EntityType string  `json:"entity_type,omitempty"`
}

type edgeJSON struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Layer  int    `json:"layer"`
	Status string `json:"status"`
}

func graphJSON(m graph.Map) mapJSON {
	out := mapJSON{Nodes: make([]nodeJSON, len(m.Nodes)), Edges: make([]edgeJSON, len(m.Edges))}
	for i, n := range m.Nodes {
		out.Nodes[i] = nodeJSON{ID: n.ID, Kind: n.Kind, Label: n.Label, Ref: n.Ref, Band: n.Band, EntityType: n.EntityType}
	}
	for i, e := range m.Edges {
		out.Edges[i] = edgeJSON(e)
	}
	return out
}

// viaJSON converts between two shapes that share JSON tags.
func viaJSON[T any](v any) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(raw, &out)
}

// ListAlerts returns one page of alerts, newest first.
func (s *Server) ListAlerts(ctx context.Context, req ListAlertsRequestObject) (ListAlertsResponseObject, error) {
	before, err := uuidCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	list, next, err := s.Graph.Alerts(ctx, graphActor(ctx), graph.AlertPage{Before: before, Size: pageSize(req.Params.Limit)})
	if err != nil {
		return nil, err
	}
	body, err := alertsJSON(list)
	return ListAlerts200JSONResponse{Body: body, Headers: ListAlerts200ResponseHeaders{NextCursor: cursorOf(next)}}, err
}

// UpdateAlert changes the status or the assignee of an alert.
func (s *Server) UpdateAlert(ctx context.Context, req UpdateAlertRequestObject) (UpdateAlertResponseObject, error) {
	change := graph.AlertChange{ID: req.Id, AssigneeID: req.Body.AssigneeId, Note: req.Body.Note}
	if req.Body.Status != nil {
		status := string(*req.Body.Status)
		change.Status = &status
	}
	alert, err := s.Graph.UpdateAlert(ctx, graphActor(ctx), change)
	if err != nil {
		return nil, err
	}
	body, err := alertsJSON([]graph.Alert{alert})
	if err != nil {
		return nil, err
	}
	return UpdateAlert200JSONResponse(body[0]), nil
}

type alertJSON struct {
	ID          uuid.UUID        `json:"id"`
	Rule        string           `json:"rule"`
	Severity    string           `json:"severity"`
	Status      string           `json:"status"`
	Title       string           `json:"title"`
	Detail      string           `json:"detail"`
	DocumentIDs []uuid.UUID      `json:"document_ids"`
	Entity      *EntityRef       `json:"entity"`
	AssigneeID  *uuid.UUID       `json:"assignee_id"`
	FirstSeenAt time.Time        `json:"first_seen_at"`
	LastSeenAt  time.Time        `json:"last_seen_at"`
	Evidence    []graph.Evidence `json:"evidence"`
}

func alertsJSON(list []graph.Alert) ([]Alert, error) {
	wire := make([]alertJSON, len(list))
	for i, a := range list {
		wire[i] = alertJSON{ID: a.ID, Rule: a.Rule, Severity: a.Severity, Status: a.Status, Title: a.Title, Detail: a.Detail,
			DocumentIDs: a.DocumentIDs, AssigneeID: a.AssigneeID, FirstSeenAt: a.FirstSeenAt, LastSeenAt: a.LastSeenAt, Evidence: a.Evidence}
	}
	return viaJSON[[]Alert](wire)
}

// DecideEdge confirms or rejects a suggested link.
func (s *Server) DecideEdge(ctx context.Context, req DecideEdgeRequestObject) (DecideEdgeResponseObject, error) {
	err := s.Graph.DecideEdge(ctx, graphActor(ctx), graph.Decision{EdgeID: req.Id, Status: string(req.Body.Status)})
	if err != nil {
		return nil, err
	}
	return DecideEdge204Response{}, nil
}

// fillEntities adds to each document what it names, from the graph. Without a graph the lists stay empty.
func (s *Server) fillEntities(ctx context.Context, docs []Document) error {
	if s.Graph == nil || len(docs) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(docs))
	for i, d := range docs {
		ids[i] = d.Id
	}
	found, err := s.Graph.EntitiesOf(ctx, principalFrom(ctx).TenantID, ids)
	if err != nil {
		return fmt.Errorf("document entities: %w", err)
	}
	for i := range docs {
		for _, e := range found[docs[i].Id] {
			docs[i].Entities = append(docs[i].Entities, EntityRef{Type: e.Type, Label: e.Label})
		}
	}
	return nil
}
