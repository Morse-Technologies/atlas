package search

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/models/tei"
	"github.com/morse/atlas/server/internal/search/searchdb"
)

// candidate is a passage found for a question, with where it ranked.
type candidate struct {
	Passage
	fused
	Rerank   float64
	Reranked bool
}

// retrieval is what search found for a question.
type retrieval struct {
	Candidates []candidate // best first
	Reranked   bool
	Notes      []string
}

// lookup is one search: who asks what, and which ACL sets they may read.
type lookup struct {
	who      Actor
	question string
	acl      []uuid.UUID
}

// retrieve finds the passages that may answer the question: word and vector search in parallel, fused by rank,
// then reranked. Only passages the person may read are ever candidates.
func (s *Service) retrieve(ctx context.Context, who Actor, question string) (retrieval, error) {
	clock := newStageClock()
	cands, notes, err := s.gather(ctx, lookup{who: who, question: question}, clock)
	if err != nil {
		return retrieval{}, err
	}
	out := s.rerank(ctx, question, cands)
	out.Notes = append(notes, out.Notes...)
	clock.lap("rerank")
	clock.log(ctx, len(cands))
	return out, nil
}

// gather finds the passages the person may read by words and by meaning, fuses the two lists and loads the text.
func (s *Service) gather(ctx context.Context, l lookup, clock *stageClock) ([]candidate, []string, error) {
	acl, err := s.allowedSets(ctx, l.who)
	if err != nil {
		return nil, nil, err
	}
	clock.lap("acl")
	l.acl = acl
	lexical, vector, notes, err := s.searchBoth(ctx, l)
	if err != nil {
		return nil, nil, err
	}
	clock.lap("search")
	merged := fuse(lexical, vector)
	cands, err := s.load(ctx, l.who, merged[:min(len(merged), rankedPerList)])
	clock.lap("load")
	return cands, notes, err
}

func (s *Service) allowedSets(ctx context.Context, who Actor) ([]uuid.UUID, error) {
	var sets []uuid.UUID
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var setErr error
		sets, setErr = access.AllowedACLSets(ctx, tx, who.UserID)
		return setErr
	})
	return sets, err
}

// searchBoth runs the word search and the vector search at once. A failing vector search only degrades the
// answer (a note), a failing word search is an error.
func (s *Service) searchBoth(ctx context.Context, l lookup) (lexical, vector []uuid.UUID, notes []string, err error) {
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var searchErr error
		lexical, searchErr = s.searchWords(gctx, l)
		return searchErr
	})
	g.Go(func() error {
		var searchErr error
		if vector, searchErr = s.searchVectors(gctx, l); searchErr != nil {
			notes = append(notes, "Search by meaning is unavailable; results come from matching words only.")
		}
		return nil
	})
	err = g.Wait()
	return lexical, vector, notes, err
}

func (s *Service) searchWords(ctx context.Context, l lookup) ([]uuid.UUID, error) {
	clock := newStageClock()
	query := lexicalQuery(l.question, s.commonLexemes(ctx, db.Scope{Tenant: l.who.TenantID, User: l.who.UserID}))
	if query == "" {
		return nil, nil
	}
	clock.lap("terms")
	defer func() { clock.lap("query"); clock.logAs(ctx, "word search", 0) }()
	var ids []uuid.UUID
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: l.who.TenantID, User: l.who.UserID}, func(tx pgx.Tx) error {
		var queryErr error
		ids, queryErr = wordMatches(ctx, tx, wordQuery{terms: query, acl: l.acl})
		return queryErr
	})
	if err != nil {
		return nil, fmt.Errorf("word search: %w", err)
	}
	return ids, nil
}

func (s *Service) searchVectors(ctx context.Context, l lookup) ([]uuid.UUID, error) {
	if s.Embedder == nil || s.EmbedModel != bgeM3 {
		return nil, nil
	}
	clock := newStageClock()
	vectors, err := s.Embedder.Embed(ctx, []string{l.question})
	if err != nil {
		return nil, fmt.Errorf("embed question: %w", err)
	}
	if len(vectors) != 1 || len(vectors[0]) != tei.Dimension {
		return nil, fmt.Errorf("embed question: unexpected vector size")
	}
	clock.lap("embed")
	ids, err := s.nearest(ctx, l, tei.VectorLiteral(vectors[0]))
	clock.lap("nearest")
	clock.logAs(ctx, "vector search", len(ids))
	return ids, err
}

// nearest asks the HNSW index for the closest passages the person may read.
func (s *Service) nearest(ctx context.Context, l lookup, vector string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: l.who.TenantID, User: l.who.UserID}, func(tx pgx.Tx) error {
		q := searchdb.New(tx)
		if err := q.UseIterativeVectorScan(ctx); err != nil {
			return err
		}
		rows, err := q.SearchVector(ctx, searchdb.SearchVectorParams{QueryVector: vector, AclSets: l.acl, TopK: rankedPerList})
		for _, r := range rows {
			ids = append(ids, r.ChunkID)
		}
		return err
	})
	return ids, err
}

// load reads the text of the fused passages and keeps their fused order.
func (s *Service) load(ctx context.Context, who Actor, merged []fused) ([]candidate, error) {
	ids := make([]uuid.UUID, len(merged))
	for i, f := range merged {
		ids[i] = f.ID
	}
	rows, err := s.readPassages(ctx, who, ids)
	if err != nil {
		return nil, fmt.Errorf("load passages: %w", err)
	}
	return orderLike(merged, rows), nil
}

func (s *Service) readPassages(ctx context.Context, who Actor, ids []uuid.UUID) ([]searchdb.GetCandidateChunksRow, error) {
	var rows []searchdb.GetCandidateChunksRow
	err := s.DB.InScopeTx(ctx, db.Scope{Tenant: who.TenantID, User: who.UserID}, func(tx pgx.Tx) error {
		var getErr error
		rows, getErr = searchdb.New(tx).GetCandidateChunks(ctx, ids)
		return getErr
	})
	return rows, err
}

// orderLike pairs rows with their fused ranks, in fused order, skipping passages retired since the search.
func orderLike(merged []fused, rows []searchdb.GetCandidateChunksRow) []candidate {
	byID := make(map[uuid.UUID]searchdb.GetCandidateChunksRow, len(rows))
	for _, r := range rows {
		byID[r.ChunkID] = r
	}
	var out []candidate
	for _, f := range merged {
		if r, ok := byID[f.ID]; ok {
			out = append(out, candidate{Passage: passageOf(r), fused: f})
		}
	}
	return out
}

func passageOf(r searchdb.GetCandidateChunksRow) Passage {
	return Passage{ChunkID: r.ChunkID, VersionID: r.VersionID, DocumentID: r.DocumentID, Page: int(r.Page), Section: r.Section,
		Header: r.ContextHeader, Text: r.Text, DocTitle: r.DocumentTitle, DocCode: r.DocumentCode, Box: r.Bbox, Suspect: r.SuspectInjection}
}

// rerank orders the candidates by the reranker's score. Without a reranker, or if it fails, they keep the fused order.
func (s *Service) rerank(ctx context.Context, question string, cands []candidate) retrieval {
	out := retrieval{Candidates: cands}
	if s.Reranker == nil || len(cands) == 0 {
		return out
	}
	texts := make([]string, len(cands))
	for i, c := range cands {
		texts[i] = c.Header + "\n" + c.Text
	}
	scores, err := s.Reranker.Rerank(ctx, question, texts)
	if err != nil || len(scores) != len(cands) {
		out.Notes = []string{"Ranking is approximate: the reranker is unavailable."}
		return out
	}
	for i := range cands {
		cands[i].Rerank, cands[i].Reranked = scores[i], true
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Rerank > cands[j].Rerank })
	out.Reranked = true
	return out
}
