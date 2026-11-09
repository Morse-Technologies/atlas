package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/graph"
)

var proLicense = fixedLicense{Plan: "pro", Features: []string{"alerts", "map"}}

type scriptedGraph struct {
	GraphView
	change   graph.AlertChange
	decision graph.Decision
}

func (g *scriptedGraph) Neighborhood(context.Context, graph.Actor, graph.Query) (graph.Map, error) {
	ref := uuid.NewString()
	return graph.Map{
		Nodes: []graph.Node{{ID: "d:" + ref, Kind: "document", Label: "PNT-014", Ref: &ref, Band: "procedure"}, {ID: "standard:ISO 9001", Kind: "entity", Label: "ISO 9001", EntityType: "standard"}},
		Edges: []graph.MapEdge{{ID: "e1", Source: "d:" + ref, Target: "standard:ISO 9001", Kind: "cites_norm", Layer: 1, Status: "confirmed"}},
	}, nil
}

func (g *scriptedGraph) UpdateAlert(_ context.Context, _ graph.Actor, c graph.AlertChange) (graph.Alert, error) {
	g.change = c
	return graph.Alert{ID: c.ID, Rule: "NO_OWNER", Severity: "low", Status: *c.Status, Title: "t", Detail: "d", DocumentIDs: []uuid.UUID{uuid.New()},
		FirstSeenAt: time.Now(), LastSeenAt: time.Now(), Evidence: []graph.Evidence{{Page: 2, Quote: "q"}}}, nil
}

func (g *scriptedGraph) DecideEdge(_ context.Context, _ graph.Actor, d graph.Decision) error {
	g.decision = d
	return nil
}

func TestTheMapKeepsNodesEdgesAndTheirKinds(t *testing.T) {
	srv := &Server{Accounts: people, Graph: &scriptedGraph{}, Licenses: proLicense}
	rec := serveTo(t, srv, adminRequest(http.MethodGet, "/api/v1/graph/neighborhood?depth=2", ""))
	for _, want := range []string{`"kind":"document"`, `"band":"procedure"`, `"entity_type":"standard"`, `"kind":"cites_norm"`, `"layer":1`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %s: %s", want, rec.Body)
		}
	}
}

func TestUpdatingAnAlertPassesStatusAndNoteAndReturnsTheEvidence(t *testing.T) {
	g := &scriptedGraph{}
	srv := &Server{Accounts: people, Graph: g, Licenses: proLicense}
	rec := serveTo(t, srv, adminRequest(http.MethodPatch, "/api/v1/alerts/"+uuid.NewString(), `{"status":"dismissed","note":"on purpose"}`))
	if rec.Code != http.StatusOK || g.change.Status == nil || *g.change.Status != "dismissed" || *g.change.Note != "on purpose" {
		t.Fatalf("status %d, change %+v", rec.Code, g.change)
	}
	if !strings.Contains(rec.Body.String(), `"quote":"q"`) {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestDecidingALinkNeedsTheCuratorRoleAndTheMapFeature(t *testing.T) {
	g := &scriptedGraph{}
	body := `{"status":"rejected"}`
	viewer := adminRequest(http.MethodPatch, "/api/v1/graph/edges/abc", body)
	viewer.Header.Set("Cookie", sessionCookie+"=viewer")
	if rec := serveTo(t, &Server{Accounts: people, Graph: g, Licenses: proLicense}, viewer); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer: status %d, want 403", rec.Code)
	}
	if rec := serveTo(t, &Server{Accounts: people, Graph: g}, adminRequest(http.MethodPatch, "/api/v1/graph/edges/abc", body)); rec.Code != http.StatusForbidden {
		t.Fatalf("no license: status %d, want 403", rec.Code)
	}
	rec := serveTo(t, &Server{Accounts: people, Graph: g, Licenses: proLicense}, adminRequest(http.MethodPatch, "/api/v1/graph/edges/abc", body))
	if rec.Code != http.StatusNoContent || g.decision.EdgeID != "abc" || g.decision.Status != "rejected" {
		t.Fatalf("status %d, decision %+v", rec.Code, g.decision)
	}
}
