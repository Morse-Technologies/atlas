package graph

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/graph/graphdb"
)

// memory is a small graph held in maps, so the walk can be tested without a database.
type memory struct {
	edges    []graphdb.ListEdgesOfRow
	mentions []graphdb.ListMentionsOfRow
	facts    map[uuid.UUID]docFacts
	hubs     []uuid.UUID
}

func (m memory) edgesOf(_ context.Context, ids []uuid.UUID) ([]graphdb.ListEdgesOfRow, error) {
	var out []graphdb.ListEdgesOfRow
	for _, e := range m.edges {
		if contains(ids, e.Src) || contains(ids, e.Dst) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m memory) mentionsOf(_ context.Context, ids []uuid.UUID) ([]graphdb.ListMentionsOfRow, error) {
	var out []graphdb.ListMentionsOfRow
	for _, x := range m.mentions {
		if contains(ids, x.DocumentID) {
			out = append(out, x)
		}
	}
	return out, nil
}

func (m memory) factsOf(context.Context, []uuid.UUID) (map[uuid.UUID]docFacts, error) {
	return m.facts, nil
}

func (m memory) seeds(_ context.Context, q Query) ([]uuid.UUID, error) {
	if q.NodeID == "" {
		return m.hubs, nil
	}
	id, _ := uuid.Parse(q.NodeID[len(prefixDocument):])
	return []uuid.UUID{id}, nil
}

func band(string) string { return "procedure" }

func chain(n int) (memory, []uuid.UUID) {
	ids := make([]uuid.UUID, n)
	m := memory{facts: map[uuid.UUID]docFacts{}}
	for i := range ids {
		ids[i] = uuid.New()
		m.facts[ids[i]] = docFacts{code: fmt.Sprintf("PNT-%03d", i), department: "Quality"}
		if i > 0 {
			m.edges = append(m.edges, graphdb.ListEdgesOfRow{ID: uuid.New(), Src: ids[i-1], Dst: ids[i], Kind: "references", Layer: 1, Status: "confirmed"})
		}
	}
	return m, ids
}

func nodesByKind(m Map, kind string) int {
	n := 0
	for _, node := range m.Nodes {
		if node.Kind == kind {
			n++
		}
	}
	return n
}

func TestDepthOneShowsTheDirectNeighboursOnly(t *testing.T) {
	m, ids := chain(4)
	got, err := walk(context.Background(), m, walkInput{query: Query{NodeID: prefixDocument + ids[0].String(), Depth: 1}, bandOf: band})
	if err != nil || nodesByKind(got, KindDocument) != 2 {
		t.Fatalf("documents %d, err %v; want the seed and its neighbour", nodesByKind(got, KindDocument), err)
	}
}

func TestDepthTwoReachesTheNeighboursOfNeighbours(t *testing.T) {
	m, ids := chain(4)
	got, _ := walk(context.Background(), m, walkInput{query: Query{NodeID: prefixDocument + ids[0].String(), Depth: 2}, bandOf: band})
	if nodesByKind(got, KindDocument) != 3 {
		t.Fatalf("documents %d, want 3", nodesByKind(got, KindDocument))
	}
}

func TestDepthIsCappedAtThree(t *testing.T) {
	m, ids := chain(10)
	got, _ := walk(context.Background(), m, walkInput{query: Query{NodeID: prefixDocument + ids[0].String(), Depth: 99}, bandOf: band})
	if nodesByKind(got, KindDocument) != 4 {
		t.Fatalf("documents %d; a chain walked 3 levels from its end has 4", nodesByKind(got, KindDocument))
	}
}

func TestEntitiesAndDepartmentsAreLinkedToTheirDocuments(t *testing.T) {
	m, ids := chain(1)
	m.mentions = []graphdb.ListMentionsOfRow{{DocumentID: ids[0], EntityType: EntityStandard, Label: "ISO 9001", Count: 2}}
	got, _ := walk(context.Background(), m, walkInput{query: Query{NodeID: prefixDocument + ids[0].String()}, bandOf: band})
	kinds := map[string]bool{}
	for _, e := range got.Edges {
		kinds[e.Kind] = true
	}
	if nodesByKind(got, KindEntity) != 1 || nodesByKind(got, KindDepartment) != 1 || !kinds["cites_norm"] || !kinds["belongs_to"] {
		t.Fatalf("map = %+v", got)
	}
}

func TestEveryEdgeJoinsTwoNodesOfTheMap(t *testing.T) {
	m, ids := chain(5)
	got, _ := walk(context.Background(), m, walkInput{query: Query{NodeID: prefixDocument + ids[2].String(), Depth: 3}, bandOf: band})
	known := map[string]bool{}
	for _, n := range got.Nodes {
		known[n.ID] = true
	}
	for _, e := range got.Edges {
		if !known[e.Source] || !known[e.Target] {
			t.Fatalf("edge %+v points outside the map", e)
		}
	}
}

func TestTheMapStopsAtTheNodeLimit(t *testing.T) {
	m, ids := chain(maxNodes + 100)
	m.hubs = ids
	got, _ := walk(context.Background(), m, walkInput{query: Query{Depth: 3}, bandOf: band})
	if len(got.Nodes) > maxNodes {
		t.Fatalf("%d nodes, the limit is %d", len(got.Nodes), maxNodes)
	}
}

func TestNoNodeAsksForTheMostConnectedDocuments(t *testing.T) {
	m, ids := chain(3)
	m.hubs = ids[1:2]
	got, _ := walk(context.Background(), m, walkInput{query: Query{}, bandOf: band})
	if nodesByKind(got, KindDocument) != 3 {
		t.Fatalf("documents %d; the hub and both neighbours", nodesByKind(got, KindDocument))
	}
}

func TestFingerprintsAreStableAndDistinctPerSubject(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	first, again, swapped := fingerprint(RuleDuplicate, a, b), fingerprint(RuleDuplicate, a, b), fingerprint(RuleDuplicate, b, a)
	if first != again || first == swapped {
		t.Fatal("same subjects must give the same fingerprint, a different order a different one")
	}
	if fingerprint(RuleNoOwner, a) == fingerprint(RuleReviewOverdue, a) {
		t.Fatal("two rules about one document are two alerts")
	}
}

func TestAPairOfDuplicatesIsOneEdgeWhicheverIsIndexedFirst(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	x1, y1 := orderedPair(a, b)
	x2, y2 := orderedPair(b, a)
	if x1 != x2 || y1 != y2 {
		t.Fatal("the pair must be ordered the same way from either side")
	}
}
