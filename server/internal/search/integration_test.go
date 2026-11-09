//go:build integration

package search

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/events"
	"github.com/morse/atlas/server/internal/platform/dbtest"
	"github.com/morse/atlas/server/internal/platform/models/tei"
)

type fixedReranker float64

func (f fixedReranker) Rerank(_ context.Context, _ string, texts []string) ([]float64, error) {
	out := make([]float64, len(texts))
	for i := range out {
		out[i] = float64(f)
	}
	return out, nil
}

type world struct {
	t      *testing.T
	ctx    context.Context
	svc    *Service
	who    Actor
	acl    uuid.UUID
	source uuid.UUID
}

func newWorld(t *testing.T, cfg Config) world {
	t.Helper()
	d := dbtest.Start(t)
	cfg.DB = d
	w := world{t: t, ctx: context.Background(), svc: NewService(cfg), source: uuid.New()}
	w.who = Actor{TenantID: dbtest.NewTenant(t, d), UserID: uuid.New()}
	err := d.InTenantTx(w.ctx, w.who.TenantID, func(tx pgx.Tx) error {
		var err error
		w.acl, err = access.DefaultACLSet(w.ctx, tx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// chunk stores one searchable passage of a new document.
func (w world) chunk(code, text string, acl uuid.UUID, current bool) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, func(tx pgx.Tx) error {
		var doc, version uuid.UUID
		if err := tx.QueryRow(w.ctx, `INSERT INTO documents.documents (source_id, external_id, title, code) VALUES ($1, $2, $3, $4) RETURNING id`,
			w.source, uuid.NewString(), code, code).Scan(&doc); err != nil {
			return err
		}
		if err := tx.QueryRow(w.ctx, `INSERT INTO documents.document_versions (document_id, content_hash, mime, size, state) VALUES ($1, $2, 'application/pdf', 1, 'parsed') RETURNING id`,
			doc, uuid.NewString()).Scan(&version); err != nil {
			return err
		}
		_, err := tx.Exec(w.ctx, `INSERT INTO documents.chunks (id, version_id, document_id, ord, page, text, codes, tokens, is_current, acl_set_id)
			VALUES ($1, $2, $3, 0, 3, $4, $5, 10, $6, $7)`, id, version, doc, text, code, current, acl)
		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

func TestOnlyCurrentPassagesOfAllowedSetsAreFound(t *testing.T) {
	w := newWorld(t, Config{})
	visible := w.chunk("PNT-014", "Tightening torque of the spindle is 40 Nm", w.acl, true)
	w.chunk("PNT-015", "Tightening torque of the old revision", w.acl, false)
	w.chunk("PNT-016", "Tightening torque of a restricted folder", uuid.New(), true)
	ret, err := w.svc.retrieve(w.ctx, w.who, "tightening torque spindle")
	if err != nil || len(ret.Candidates) != 1 || ret.Candidates[0].ChunkID != visible {
		t.Fatalf("candidates = %+v, %v; want only the current, allowed passage", ret.Candidates, err)
	}
}

func TestACodeFindsItsPassageExactly(t *testing.T) {
	w := newWorld(t, Config{})
	want := w.chunk("PNT-014", "Procedure for the spindle", w.acl, true)
	w.chunk("PNT-099", "Procedure for the spindle too", w.acl, true)
	ret, _ := w.svc.retrieve(w.ctx, w.who, "PNT-014")
	if len(ret.Candidates) == 0 || ret.Candidates[0].ChunkID != want {
		t.Fatalf("candidates = %+v", ret.Candidates)
	}
}

func TestAnswerIsStoredWithCitationsCandidatesAndAnAuditEvent(t *testing.T) {
	w := newWorld(t, Config{Reranker: fixedReranker(0.9), LLM: &scriptedLLM{pieces: []string{"Forty newton metres [1]."}}})
	w.chunk("PNT-014", "Tightening torque of the spindle is 40 Nm", w.acl, true)
	sink := &recordingSink{}
	if err := w.svc.Ask(w.ctx, Question{Who: w.who, Text: "tightening torque?"}, sink); err != nil {
		t.Fatal(err)
	}
	if sink.done == nil || sink.done.Outcome != OutcomeAnswered || len(sink.done.Citations) != 1 {
		t.Fatalf("done = %+v, failed %q", sink.done, sink.failed)
	}
	_, answers, err := w.svc.Thread(w.ctx, w.who, sink.done.ConversationID)
	if err != nil || len(answers) != 1 || answers[0].Text != "Forty newton metres [1]." || answers[0].Citations[0].Page != 3 {
		t.Fatalf("thread = %+v, %v", answers, err)
	}
	events, _, _ := (&audit.Service{DB: w.svc.DB}).List(w.ctx, w.who.TenantID, audit.Page{Size: 10})
	if len(events) == 0 || events[0].Action != "answer.generated" || events[0].Detail != "outcome=answered" {
		t.Fatalf("audit = %+v", events)
	}
}

func TestNothingRelevantIsStoredAsNotFound(t *testing.T) {
	w := newWorld(t, Config{Reranker: fixedReranker(0.05)})
	w.chunk("PNT-014", "Tightening torque", w.acl, true)
	sink := &recordingSink{}
	_ = w.svc.Ask(w.ctx, Question{Who: w.who, Text: "tightening torque"}, sink)
	if sink.done == nil || sink.done.Outcome != OutcomeNotFound || len(sink.done.Nearby) != 1 {
		t.Fatalf("done = %+v", sink.done)
	}
}

func TestPeopleSeeOnlyTheirOwnConversationsAndCanRateOnlyTheirOwnAnswers(t *testing.T) {
	w := newWorld(t, Config{Reranker: fixedReranker(0.9)})
	w.chunk("PNT-014", "Tightening torque", w.acl, true)
	sink := &recordingSink{}
	_ = w.svc.Ask(w.ctx, Question{Who: w.who, Text: "tightening torque"}, sink)
	other := Actor{TenantID: w.who.TenantID, UserID: uuid.New()}
	if list, _, _ := w.svc.Conversations(w.ctx, other, ConversationPage{Size: 10}); len(list) != 0 {
		t.Fatal("another person must not list this conversation")
	}
	if _, _, err := w.svc.Thread(w.ctx, other, sink.done.ConversationID); err == nil {
		t.Fatal("another person must not open this conversation")
	}
	if err := w.svc.Feedback(w.ctx, other, FeedbackRequest{AnswerID: sink.done.ID, Value: "up"}); err == nil {
		t.Fatal("another person must not rate this answer")
	}
	if err := w.svc.Feedback(w.ctx, w.who, FeedbackRequest{AnswerID: sink.done.ID, Value: "up"}); err != nil {
		t.Fatal(err)
	}
}

func TestAskingInSomeoneElsesConversationFails(t *testing.T) {
	w := newWorld(t, Config{Reranker: fixedReranker(0.9)})
	other := Actor{TenantID: w.who.TenantID, UserID: uuid.New()}
	sink := &recordingSink{}
	_ = w.svc.Ask(w.ctx, Question{Who: w.who, Text: "x"}, sink)
	if err := w.svc.Ask(w.ctx, Question{Who: other, Text: "y", ConversationID: sink.done.ConversationID}, &recordingSink{}); err == nil {
		t.Fatal("a conversation belongs to the person who started it")
	}
}

func TestDeletingAnAccountErasesItsQuestionsAndAnswers(t *testing.T) {
	w := newWorld(t, Config{Reranker: fixedReranker(0.9)})
	w.chunk("PNT-014", "Tightening torque", w.acl, true)
	sink := &recordingSink{}
	_ = w.svc.Ask(w.ctx, Question{Who: w.who, Text: "my private question"}, sink)
	if err := w.svc.OnUserDeleted(w.ctx, events.UserDeleted{TenantID: w.who.TenantID, UserID: w.who.UserID}); err != nil {
		t.Fatal(err)
	}
	var left int
	err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(w.ctx, `SELECT (SELECT count(*) FROM search.conversations) + (SELECT count(*) FROM search.answers)
			+ (SELECT count(*) FROM search.citations) + (SELECT count(*) FROM search.answer_candidates)`).Scan(&left)
	})
	if err != nil || left != 0 {
		t.Fatalf("%d rows left after erasure (%v)", left, err)
	}
}

// axis is a unit vector along one dimension: two different axes are as unlike as two vectors can be.
func axis(i int) []float32 {
	v := make([]float32, tei.Dimension)
	v[i] = 1
	return v
}

// fixedEmbedder embeds every question as the same vector.
type fixedEmbedder []float32

func (f fixedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = f
	}
	return out, nil
}

// vector stores the embedding of a chunk the way the pipeline does.
func (w world) vector(chunk uuid.UUID, v []float32, acl uuid.UUID) {
	w.t.Helper()
	w.exec(func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, `INSERT INTO documents.chunk_embeddings (chunk_id, embed_model, embedding, acl_set_id, is_current)
			VALUES ($1, 'bge-m3', $2::text::vector, $3, true)`, chunk, tei.VectorLiteral(v), acl)
		return err
	})
}

func (w world) exec(fn func(pgx.Tx) error) {
	w.t.Helper()
	if err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, fn); err != nil {
		w.t.Fatal(err)
	}
}

func TestVectorSearchFindsPassagesByMeaningNotByWords(t *testing.T) {
	w := newWorld(t, Config{Embedder: fixedEmbedder(axis(0)), EmbedModel: "bge-m3"})
	near := w.chunk("PNT-014", "alpha beta gamma", w.acl, true)
	far := w.chunk("PNT-015", "delta epsilon zeta", w.acl, true)
	hidden := w.chunk("PNT-016", "eta theta iota", uuid.New(), true)
	w.vector(near, axis(0), w.acl)
	w.vector(far, axis(1), w.acl)
	w.vector(hidden, axis(0), uuid.New()) // as close as the first, but in a set the person may not read
	ret, err := w.svc.retrieve(w.ctx, w.who, "unrelated words entirely")
	if err != nil || len(ret.Candidates) != 2 {
		t.Fatalf("candidates %+v, %v; want the two passages the person may read", ret.Candidates, err)
	}
	if ret.Candidates[0].ChunkID != near || ret.Candidates[0].RankVector != 1 || ret.Candidates[0].RankLexical != 0 {
		t.Fatalf("first = %+v; the closest vector must win without any word in common", ret.Candidates[0])
	}
}

// The query must use the same cast and the same WHERE as the partial index, or Postgres scans every vector.
// This repeats the text of SearchVector (db/queries/search.sql) with the planner pushed toward indexes.
func TestVectorQueryUsesTheHNSWIndex(t *testing.T) {
	w := newWorld(t, Config{})
	w.exec(func(tx pgx.Tx) error {
		// With a plain scan, a bitmap scan and an explicit sort ruled out, the only way left to order by distance is the
		// index itself. If the cast or the WHERE did not match the index, the plan would still need a Sort.
		if _, err := tx.Exec(w.ctx, `SET LOCAL enable_seqscan = off; SET LOCAL enable_bitmapscan = off; SET LOCAL enable_sort = off`); err != nil {
			return err
		}
		rows, err := tx.Query(w.ctx, `EXPLAIN SELECT chunk_id FROM documents.v_searchable_vectors
			WHERE embed_model = 'bge-m3' AND acl_set_id = ANY($1::uuid[])
			ORDER BY (embedding::vector(1024)) <=> ($2::text)::vector(1024) LIMIT 50`, []uuid.UUID{w.acl}, tei.VectorLiteral(axis(0)))
		if err != nil {
			return err
		}
		defer rows.Close()
		var plan string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			plan += line + "\n"
		}
		if !strings.Contains(plan, "chunk_embeddings_bge_m3") {
			t.Fatalf("the vector query does not use the HNSW index:\n%s", plan)
		}
		return rows.Err()
	})
}

// restrictedSet makes a set that lists only the given people, as a restricted source does.
func (w world) restrictedSet(readers ...uuid.UUID) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	err := w.svc.DB.InTenantTx(w.ctx, w.who.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(w.ctx, `INSERT INTO access.acl_sets (origin, allow) VALUES ('source', $1) RETURNING id`, readers).Scan(&id)
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

func TestAPassageOfARestrictedSetIsFoundOnlyByItsReaders(t *testing.T) {
	w := newWorld(t, Config{})
	reader, other := uuid.New(), uuid.New()
	set := w.restrictedSet(reader)
	secret := w.chunk("PNT-777", "Salary bands of the quality department", set, true)
	ask := func(who uuid.UUID) []candidate {
		ret, err := w.svc.retrieve(w.ctx, Actor{TenantID: w.who.TenantID, UserID: who}, "salary bands quality")
		if err != nil {
			t.Fatal(err)
		}
		return ret.Candidates
	}
	if found := ask(reader); len(found) != 1 || found[0].ChunkID != secret {
		t.Fatalf("the reader found %+v, want the passage", found)
	}
	if found := ask(other); len(found) != 0 {
		t.Fatalf("someone else found %+v; a restricted passage must not be found, not even as a weak match", found)
	}
}
