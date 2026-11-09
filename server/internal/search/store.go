package search

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/search/searchdb"
)

const quoteRunes = 400

// citationsOf builds the citations for the numbered passages in numbers.
func citationsOf(sent []candidate, numbers []int) []Citation {
	best := 1.0
	if len(sent) > 0 && !sent[0].Reranked {
		best = topFusedScore
	}
	out := make([]Citation, 0, len(numbers))
	for _, n := range numbers {
		c := sent[n-1]
		score := c.Rerank
		if !c.Reranked {
			score = c.Score / best
		}
		out = append(out, Citation{N: n, DocumentID: c.DocumentID, VersionID: c.VersionID, ChunkID: c.ChunkID, Label: c.Label(), Page: c.Page,
			Section: c.Section, Quote: truncate(c.Text, quoteRunes), Score: score, Box: boxOf(c.Box)})
	}
	return out
}

func truncate(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	cut := []rune(s)[:runes]
	return string(cut) + "…"
}

func boxOf(raw []byte) *Box {
	if len(raw) == 0 {
		return nil
	}
	var b Box
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return &b
}

func (s *Service) answerOf(t *turn) Answer {
	a := Answer{ID: t.id, Question: t.q.Text, AskedByID: t.who.UserID, AskedAt: t.started.UTC().Truncate(time.Microsecond),
		Outcome: t.outcome, Text: t.text, Citations: citationsOf(t.sent, t.cited), Notes: t.notes, Nearby: t.nearby,
		LatencyMs: int(s.Now().Sub(t.started).Milliseconds()), Model: t.model, Scope: t.q.Scope}
	if a.Model == "" && s.LLM != nil {
		a.Model = s.LLM.Model()
	}
	return a
}

// save stores the conversation turn, its citations and the candidates it considered, and the audit event,
// in one transaction. It fills in the conversation id.
func (s *Service) save(ctx context.Context, t *turn, a *Answer) error {
	return s.DB.InTenantTx(ctx, t.who.TenantID, func(tx pgx.Tx) error {
		q := searchdb.New(tx)
		conv, err := s.conversationFor(ctx, q, t)
		if err != nil {
			return err
		}
		a.ConversationID = conv
		if err := s.insertAnswer(ctx, q, stored{answer: *a, turn: t}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Actor: t.who.UserID, Action: "answer.generated",
			Subject: "answer:" + a.ID.String(), Detail: "outcome=" + string(a.Outcome)})
	})
}

func (s *Service) conversationFor(ctx context.Context, q *searchdb.Queries, t *turn) (uuid.UUID, error) {
	if t.q.ConversationID == uuid.Nil {
		id, err := q.CreateConversation(ctx, searchdb.CreateConversationParams{UserID: t.who.UserID, Title: truncate(t.q.Text, titleRunes)})
		if err != nil {
			return uuid.Nil, fmt.Errorf("create conversation: %w", err)
		}
		return id, nil
	}
	return t.q.ConversationID, q.TouchConversation(ctx, t.q.ConversationID)
}

// stored is an answer and the turn that produced it.
type stored struct {
	answer Answer
	turn   *turn
}

func (s *Service) insertAnswer(ctx context.Context, q *searchdb.Queries, in stored) error {
	a, t := in.answer, in.turn
	err := q.InsertAnswer(ctx, searchdb.InsertAnswerParams{ID: a.ID, ConversationID: a.ConversationID, UserID: a.AskedByID,
		Question: a.Question, AskedAt: a.AskedAt, Outcome: string(a.Outcome), Text: a.Text, Notes: notNil(a.Notes), Nearby: notNilIDs(a.Nearby),
		LatencyMs: int32(a.LatencyMs), Model: a.Model, Scope: a.Scope, PromptVersion: promptVersionOf(t), EmbedModel: s.EmbedModel,
		RerankModel: s.RerankModel, NotFoundThreshold: float32(t.options.NotFoundThreshold), ContextHash: t.hash})
	if err != nil {
		return fmt.Errorf("insert answer: %w", err)
	}
	if err := q.InsertCitations(ctx, citationParams(a)); err != nil {
		return fmt.Errorf("insert citations: %w", err)
	}
	if err := q.InsertCandidates(ctx, candidateParams(t)); err != nil {
		return fmt.Errorf("insert candidates: %w", err)
	}
	return nil
}

func promptVersionOf(t *turn) string {
	if t.hash == "" {
		return ""
	}
	return PromptVersion
}

func notNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func notNilIDs(s []uuid.UUID) []uuid.UUID {
	if s == nil {
		return []uuid.UUID{}
	}
	return s
}

func citationParams(a Answer) searchdb.InsertCitationsParams {
	p := searchdb.InsertCitationsParams{AnswerID: a.ID}
	for _, c := range a.Citations {
		p.Ns = append(p.Ns, int32(c.N))
		p.DocumentIds = append(p.DocumentIds, c.DocumentID)
		p.VersionIds = append(p.VersionIds, c.VersionID)
		p.ChunkIds = append(p.ChunkIds, c.ChunkID)
		p.Labels = append(p.Labels, c.Label)
		p.Pages = append(p.Pages, int32(c.Page))
		p.Sections = append(p.Sections, c.Section)
		p.Quotes = append(p.Quotes, c.Quote)
		p.Scores = append(p.Scores, float32(c.Score))
		p.Bboxes = append(p.Bboxes, boxJSON(c.Box))
	}
	return p
}

func boxJSON(b *Box) []byte {
	if b == nil {
		return nil
	}
	raw, _ := json.Marshal(b) // four floats: cannot fail
	return raw
}

// candidateParams records every passage the search considered, and which reached the model (AI Act art. 12).
func candidateParams(t *turn) searchdb.InsertCandidatesParams {
	sent := map[uuid.UUID]bool{}
	for _, c := range t.sent {
		sent[c.ChunkID] = true
	}
	var p searchdb.InsertCandidatesParams
	p.AnswerID = t.id
	for _, c := range t.ret.Candidates {
		p.ChunkIds = append(p.ChunkIds, c.ChunkID)
		p.VersionIds = append(p.VersionIds, c.VersionID)
		p.RanksLexical = append(p.RanksLexical, int32(c.RankLexical))
		p.RanksVector = append(p.RanksVector, int32(c.RankVector))
		p.RrfScores = append(p.RrfScores, float32(c.Score))
		p.RerankScores = append(p.RerankScores, rerankOrNone(c))
		p.Sent = append(p.Sent, sent[c.ChunkID])
	}
	return p
}

// rerankOrNone is the stored score, or -1 for "not reranked", which the insert turns into NULL.
func rerankOrNone(c candidate) float32 {
	if !c.Reranked {
		return -1
	}
	return float32(c.Rerank)
}

func (s *Service) ownConversation(ctx context.Context, who Actor, id uuid.UUID) (searchdb.GetOwnConversationRow, error) {
	var row searchdb.GetOwnConversationRow
	err := s.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		var getErr error
		row, getErr = searchdb.New(tx).GetOwnConversation(ctx, searchdb.GetOwnConversationParams{ID: id, UserID: who.UserID})
		return db.NotFoundIfNoRows(getErr, "conversation")
	})
	return row, err
}
