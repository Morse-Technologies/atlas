package search

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/access"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/platform/models/llm"
)

// Texts shown to people.
const (
	notFoundText     = "I couldn't find this in your documents."
	refusedText      = "I can't give an answer I can't back with a source. These are the passages I looked at."
	searchModeText   = "I can't write an answer right now. These are the most relevant passages: "
	interruptedNote  = "The answer was interrupted."
	failureMessage   = "Something went wrong while answering. Nothing was changed; try again."
	suspectNote      = "A cited passage contains text that looks like instructions for an AI. Check the source."
	noCitationNote   = "The model answered without citing a source, so its text was withheld."
	shownUncitedNote = "This answer has no citations. Check it against your documents."
	uncitedNoteFmt   = "%d statement(s) in this answer have no citation. Check them against the sources."
	conversationGone = "conversation"
)

// turn is one question being answered, from the first search to the stored result.
type turn struct {
	id      uuid.UUID
	who     Actor
	q       Question
	started time.Time
	ret     retrieval
	sent    []candidate // the passages given to the model, numbered 1..len
	outcome Outcome
	text    string
	notes   []string
	nearby  []uuid.UUID
	model   string
	hash    string
	cited   []int
	options access.Settings // the organization's choices when the question was asked
}

// Ask answers a question, writing the answer to sink as it is produced. It returns an error only when the
// question cannot be started (unknown conversation); anything that goes wrong later is reported through sink and stored.
func (s *Service) Ask(ctx context.Context, q Question, sink Sink) error {
	if err := s.checkConversation(ctx, q); err != nil {
		return err
	}
	t := &turn{id: uuid.Must(uuid.NewV7()), who: q.Who, q: q, started: s.Now(), outcome: OutcomeAnswered,
		options: s.settingsFor(ctx, q.Who)}
	s.run(ctx, t, sink)
	return s.finish(ctx, t, sink)
}

func (s *Service) run(ctx context.Context, t *turn, sink Sink) {
	ret, err := s.retrieve(ctx, t.who, t.q.Text)
	if err != nil {
		slog.ErrorContext(ctx, "search failed", "error", err)
		t.outcome = OutcomeError
		return
	}
	t.ret = ret
	s.decide(ctx, t, sink)
}

// decide turns what was found into an outcome: nothing relevant, a written answer, or the plain passages.
func (s *Service) decide(ctx context.Context, t *turn, sink Sink) {
	ret := t.ret
	switch {
	case len(ret.Candidates) == 0, ret.Reranked && ret.Candidates[0].Rerank < t.options.NotFoundThreshold:
		s.notFound(t)
	default:
		s.answer(ctx, t, sink)
	}
}

func (s *Service) notFound(t *turn) {
	t.outcome, t.text = OutcomeNotFound, notFoundText
	t.notes = append(t.notes, t.ret.Notes...)
	seen := map[uuid.UUID]bool{}
	for _, c := range t.ret.Candidates {
		if !seen[c.DocumentID] && len(t.nearby) < nearbyDocuments {
			seen[c.DocumentID] = true
			t.nearby = append(t.nearby, c.DocumentID)
		}
	}
}

// answer sends the sources first, then either writes the answer or, if it cannot, lists the passages.
func (s *Service) answer(ctx context.Context, t *turn, sink Sink) {
	t.sent = t.ret.Candidates[:min(len(t.ret.Candidates), contextPassages)]
	t.notes = append(t.notes, t.ret.Notes...)
	if err := sink.Meta(Meta{AnswerID: t.id, Citations: citationsOf(t.sent, numbers(len(t.sent))), Notes: t.notes}); err != nil {
		t.interrupt("")
		return
	}
	if s.LLM == nil || !t.ret.Reranked {
		t.searchMode()
		return
	}
	s.generate(ctx, t, sink)
}

// searchMode lists the passages when no answer can be written: no model, or no reliable ranking.
func (t *turn) searchMode() {
	marks := make([]string, len(t.sent))
	for i := range t.sent {
		marks[i] = fmt.Sprintf("[%d]", i+1)
	}
	t.text = searchModeText + strings.Join(marks, " ")
	t.cited = numbers(len(t.sent))
}

func (s *Service) generate(ctx context.Context, t *turn, sink Sink) {
	delimiter, err := newDelimiter()
	if err != nil {
		t.interrupt("")
		return
	}
	messages := buildPrompt(promptInput{question: t.q.Text, passages: passagesOf(t.sent), delimiter: delimiter, language: t.options.AnswerLanguage})
	t.hash, t.model = contextHash(messages), s.LLM.Model()
	if err := s.slots.Acquire(ctx, 1); err != nil {
		t.interrupt("")
		return
	}
	defer s.slots.Release(1)
	var text strings.Builder
	err = s.LLM.Stream(ctx, llm.Request{Messages: messages, MaxTokens: answerMaxTokens, Priority: askPriority},
		func(tok string) error { text.WriteString(tok); return sink.Token(tok) })
	if err != nil {
		t.interrupt(text.String())
		return
	}
	t.applyCitations(text.String())
}

// applyCitations checks the model's text against the passages it was given. An answer with no valid citation
// is withheld: Atlas never gives an answer without a source.
func (t *turn) applyCitations(text string) {
	ch := checkCitations(text, len(t.sent))
	if len(ch.Used) == 0 {
		t.withoutCitations(ch.Text)
		return
	}
	t.text, t.cited = ch.Text, ch.Used
	if ch.Uncited > 0 {
		t.notes = append(t.notes, fmt.Sprintf(uncitedNoteFmt, ch.Uncited))
	}
	for _, n := range ch.Used {
		if t.sent[n-1].Suspect {
			t.notes = append(t.notes, suspectNote)
			return
		}
	}
}

// withoutCitations handles an answer that cites nothing. By default it is withheld; an organization can allow
// it, and then it is shown with a warning.
func (t *turn) withoutCitations(text string) {
	t.cited = numbers(len(t.sent))
	if t.options.RequireCitation {
		t.outcome, t.text = OutcomeRefused, refusedText
		t.notes = append(t.notes, noCitationNote)
		return
	}
	t.text = text
	t.notes = append(t.notes, shownUncitedNote)
}

// settingsFor reads the organization's settings; if they cannot be read the defaults apply, never a failure.
func (s *Service) settingsFor(ctx context.Context, who Actor) access.Settings {
	var out access.Settings
	err := s.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		var loadErr error
		out, loadErr = access.LoadSettings(ctx, tx)
		return loadErr
	})
	if err != nil {
		slog.WarnContext(ctx, "settings unavailable, using defaults", "error", err)
		return access.DefaultSettings()
	}
	return out
}

// interrupt records an answer that did not finish; what was written so far is kept.
func (t *turn) interrupt(partial string) {
	t.outcome, t.text = OutcomeError, partial
	t.notes = append(t.notes, interruptedNote)
}

func numbers(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

func passagesOf(cands []candidate) []Passage {
	out := make([]Passage, len(cands))
	for i, c := range cands {
		out[i] = c.Passage
	}
	return out
}

// finish stores the answer and tells the client. The answer is stored even if the client has gone: if it is
// not in the record, it did not happen (ATLAS.md §11.2).
func (s *Service) finish(ctx context.Context, t *turn, sink Sink) error {
	answer := s.answerOf(t)
	if err := s.save(context.WithoutCancel(ctx), t, &answer); err != nil {
		_ = sink.Fail(failureMessage)
		return fmt.Errorf("save answer: %w", err)
	}
	if t.outcome == OutcomeError {
		_ = sink.Fail(failureMessage)
		return nil
	}
	_ = sink.Done(answer)
	return nil
}

func (s *Service) checkConversation(ctx context.Context, q Question) error {
	if q.ConversationID == uuid.Nil {
		return nil
	}
	_, err := s.ownConversation(ctx, q.Who, q.ConversationID)
	if err != nil {
		return fmt.Errorf("%s: %w", conversationGone, errs.ErrNotFound)
	}
	return nil
}
