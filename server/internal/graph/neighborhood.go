package graph

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/graph/graphdb"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/errs"
)

// Limits of one map (ATLAS.md §12).
const (
	maxNodes     = 300
	maxDepth     = 3
	defaultDepth = 1
	hubSeeds     = 15 // documents with the most links, when no node is asked for
	deptSeeds    = 50
)

// Node kinds and ids. Ids are what the map keeps between calls: "d:<uuid>", "dep:<name>", "<type>:<label>".
const (
	KindDocument   = "document"
	KindEntity     = "entity"
	KindDepartment = "department"
	prefixDocument = "d:"
	prefixDept     = "dep:"
)

// Node is a point of the map.
type Node struct {
	ID         string
	Kind       string
	Label      string
	Ref        *string // the document id, for document nodes
	Band       string
	EntityType string
}

// MapEdge is a line of the map.
type MapEdge struct {
	ID     string
	Source string
	Target string
	Kind   string
	Layer  int
	Status string
}

// Map is a piece of the graph.
type Map struct {
	Nodes []Node
	Edges []MapEdge
}

// Query asks for the surroundings of a node. An empty NodeID asks for the most connected documents.
type Query struct {
	NodeID string
	Depth  int
}

// reader is what the walk needs from storage, so it can be tested without a database.
type reader interface {
	edgesOf(ctx context.Context, ids []uuid.UUID) ([]graphdb.ListEdgesOfRow, error)
	mentionsOf(ctx context.Context, ids []uuid.UUID) ([]graphdb.ListMentionsOfRow, error)
	factsOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]docFacts, error)
	seeds(ctx context.Context, q Query) ([]uuid.UUID, error)
}

// Neighborhood returns the documents around a node, up to the depth asked, with what they mention.
func (s *Service) Neighborhood(ctx context.Context, who Actor, q Query) (Map, error) {
	var out Map
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		tax, err := catalog.Load(ctx, tx)
		if err != nil {
			return err
		}
		out, err = walk(ctx, dbReader{q: graphdb.New(tx)}, walkInput{query: q, bandOf: tax.BandOf})
		return err
	})
	return out, err
}

type walkInput struct {
	query  Query
	bandOf func(docType string) string
}

// walk expands from the seeds one level at a time until the depth or the node limit is reached.
func walk(ctx context.Context, r reader, in walkInput) (Map, error) {
	depth := min(max(in.query.Depth, defaultDepth), maxDepth)
	seeds, err := r.seeds(ctx, in.query)
	if err != nil {
		return Map{}, err
	}
	b := &mapBuilder{r: r, bandOf: in.bandOf, seen: map[string]bool{}, edgeSeen: map[string]bool{}}
	frontier := seeds
	for level := 0; level < depth && len(frontier) > 0; level++ {
		if frontier, err = b.expand(ctx, frontier); err != nil {
			return Map{}, err
		}
	}
	return b.out, nil
}

type mapBuilder struct {
	r        reader
	bandOf   func(string) string
	out      Map
	seen     map[string]bool
	edgeSeen map[string]bool
}

// expand adds the frontier documents, their links and mentions, and returns the documents newly reached.
func (b *mapBuilder) expand(ctx context.Context, frontier []uuid.UUID) ([]uuid.UUID, error) {
	edges, err := b.r.edgesOf(ctx, frontier)
	if err != nil {
		return nil, err
	}
	ids := idsOf(frontier, edges)
	facts, err := b.r.factsOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	mentions, err := b.r.mentionsOf(ctx, frontier)
	if err != nil {
		return nil, err
	}
	next := b.addDocuments(ids, frontier, facts)
	b.addLinks(edges)
	b.addMentions(mentions)
	b.addDepartments(frontier, facts)
	return next, nil
}

// addDocuments adds the document nodes and returns those that are new and not already being expanded.
func (b *mapBuilder) addDocuments(ids, frontier []uuid.UUID, facts map[uuid.UUID]docFacts) []uuid.UUID {
	var next []uuid.UUID
	for _, id := range ids {
		f, readable := facts[id]
		if !readable { // a document the person may not read is not on their map, nor is anything only it links
			continue
		}
		if b.addDocument(id, f) && !contains(frontier, id) {
			next = append(next, id)
		}
	}
	return next
}

func idsOf(frontier []uuid.UUID, edges []graphdb.ListEdgesOfRow) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, id := range frontier {
		if !seen[id] {
			seen[id], out = true, append(out, id)
		}
	}
	for _, e := range edges {
		for _, id := range []uuid.UUID{e.Src, e.Dst} {
			if !seen[id] {
				seen[id], out = true, append(out, id)
			}
		}
	}
	return out
}

func contains(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// addDocument adds a document node unless the map is full. It reports whether the node is new.
func (b *mapBuilder) addDocument(id uuid.UUID, f docFacts) bool {
	key := prefixDocument + id.String()
	if b.seen[key] || len(b.out.Nodes) >= maxNodes {
		return false
	}
	b.seen[key] = true
	ref := id.String()
	b.out.Nodes = append(b.out.Nodes, Node{ID: key, Kind: KindDocument, Label: label(f.code, f.title), Ref: &ref, Band: b.bandOf(f.docType)})
	return true
}

func (b *mapBuilder) addLinks(edges []graphdb.ListEdgesOfRow) {
	for _, e := range edges {
		from, to := prefixDocument+e.Src.String(), prefixDocument+e.Dst.String()
		if b.seen[from] && b.seen[to] && !b.edgeSeen[e.ID.String()] {
			b.edgeSeen[e.ID.String()] = true
			b.out.Edges = append(b.out.Edges, MapEdge{ID: e.ID.String(), Source: from, Target: to, Kind: e.Kind, Layer: int(e.Layer), Status: e.Status})
		}
	}
}

// addMentions adds the entities the frontier documents name, linked by "cites_norm" for standards and "mentions" for the rest.
func (b *mapBuilder) addMentions(mentions []graphdb.ListMentionsOfRow) {
	for _, m := range mentions {
		doc := prefixDocument + m.DocumentID.String()
		node := m.EntityType + ":" + m.Label
		if !b.seen[doc] || !b.addEntity(node, m) {
			continue
		}
		kind := "mentions"
		if m.EntityType == EntityStandard {
			kind = "cites_norm"
		}
		b.addDerivedEdge(doc, node, kind)
	}
}

func (b *mapBuilder) addEntity(node string, m graphdb.ListMentionsOfRow) bool {
	if !b.seen[node] {
		if len(b.out.Nodes) >= maxNodes {
			return false
		}
		b.seen[node] = true
		b.out.Nodes = append(b.out.Nodes, Node{ID: node, Kind: KindEntity, Label: m.Label, EntityType: m.EntityType})
	}
	return true
}

// addDepartments links each frontier document to its department node.
func (b *mapBuilder) addDepartments(frontier []uuid.UUID, facts map[uuid.UUID]docFacts) {
	for _, id := range frontier {
		dept := facts[id].department
		doc := prefixDocument + id.String()
		if dept == "" || !b.seen[doc] {
			continue
		}
		node := prefixDept + dept
		if !b.seen[node] && len(b.out.Nodes) < maxNodes {
			b.seen[node] = true
			b.out.Nodes = append(b.out.Nodes, Node{ID: node, Kind: KindDepartment, Label: dept})
		}
		if b.seen[node] {
			b.addDerivedEdge(doc, node, "belongs_to")
		}
	}
}

func (b *mapBuilder) addDerivedEdge(from, to, kind string) {
	id := from + "|" + to + "|" + kind
	if b.edgeSeen[id] {
		return
	}
	b.edgeSeen[id] = true
	b.out.Edges = append(b.out.Edges, MapEdge{ID: id, Source: from, Target: to, Kind: kind, Layer: layerPatterns, Status: "confirmed"})
}

// dbReader reads the map's data inside one transaction.
type dbReader struct{ q *graphdb.Queries }

func (d dbReader) edgesOf(ctx context.Context, ids []uuid.UUID) ([]graphdb.ListEdgesOfRow, error) {
	return d.q.ListEdgesOf(ctx, ids)
}

func (d dbReader) mentionsOf(ctx context.Context, ids []uuid.UUID) ([]graphdb.ListMentionsOfRow, error) {
	return d.q.ListMentionsOf(ctx, ids)
}

func (d dbReader) factsOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]docFacts, error) {
	return factsOf(ctx, d.q, ids)
}

// seeds resolves the node asked for into the documents to start from.
func (d dbReader) seeds(ctx context.Context, q Query) ([]uuid.UUID, error) {
	switch {
	case q.NodeID == "":
		return d.q.ListHubs(ctx, hubSeeds)
	case strings.HasPrefix(q.NodeID, prefixDocument):
		id, err := uuid.Parse(strings.TrimPrefix(q.NodeID, prefixDocument))
		if err != nil {
			return nil, fmt.Errorf("node id: %w", errs.ErrInvalid)
		}
		return []uuid.UUID{id}, nil
	case strings.HasPrefix(q.NodeID, prefixDept):
		return d.q.ListDocsInDepartment(ctx, graphdb.ListDocsInDepartmentParams{Department: strings.TrimPrefix(q.NodeID, prefixDept), Top: deptSeeds})
	}
	kind, label, ok := strings.Cut(q.NodeID, ":")
	if !ok {
		return nil, fmt.Errorf("node id %q: %w", q.NodeID, errs.ErrInvalid)
	}
	return d.q.ListDocsMentioning(ctx, graphdb.ListDocsMentioningParams{EntityType: kind, Label: label})
}

// DecideEdge confirms or rejects a link between documents. A rejected link is not drawn again, and a rescan
// does not bring it back.
func (s *Service) DecideEdge(ctx context.Context, who Actor, d Decision) error {
	if d.Status != "confirmed" && d.Status != "rejected" {
		return fmt.Errorf("a link is confirmed or rejected: %w", errs.ErrInvalid)
	}
	id, err := uuid.Parse(d.EdgeID)
	if err != nil {
		return fmt.Errorf("edge id: %w", errs.ErrNotFound)
	}
	return s.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		n, err := graphdb.New(tx).SetEdgeStatus(ctx, graphdb.SetEdgeStatusParams{ID: id, Status: d.Status})
		if err != nil || n == 0 {
			return notFoundOr(err, "link")
		}
		return s.recordDecision(ctx, tx, decided{who: who, d: d})
	})
}

// Decision is a curator's verdict on a link.
type Decision struct {
	EdgeID string
	Status string // "confirmed" or "rejected"
}
