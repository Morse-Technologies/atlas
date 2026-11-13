package search

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// errTrialFailed says the question could not be answered at all; the cause is in the log.
var errTrialFailed = errors.New("the question could not be tried: search failed")

// Trial is how a question gets answered right now, measured without storing anything.
type Trial struct {
	Outcome Outcome
	// Citations are the passages the answer cites. When nothing was found above the threshold they are the passages
	// that would have been cited, so a lower threshold can be tried on the same trial.
	Citations []Citation
	TopScore  float64 // the best reranker score; only meaningful when Reranked
	Reranked  bool
}

// Trial runs the search and the decision for a question exactly as Ask does, but stores no answer, no audit event
// and no conversation. It is what `atlas eval` measures.
func (s *Service) Trial(ctx context.Context, q Question) (Trial, error) {
	t := &turn{id: uuid.Must(uuid.NewV7()), who: q.Who, q: q, started: s.Now(), outcome: OutcomeAnswered,
		options: s.settingsFor(ctx, q.Who)}
	s.run(ctx, t, discardSink{})
	if t.outcome == OutcomeError {
		return Trial{}, errTrialFailed
	}
	return trialOf(t), nil
}

func trialOf(t *turn) Trial {
	sent, cited := t.sent, t.cited
	if t.outcome == OutcomeNotFound {
		sent = passagesForModel(t.ret)
		cited = numbers(len(sent))
	}
	trial := Trial{Outcome: t.outcome, Citations: citationsOf(sent, cited), Reranked: t.ret.Reranked}
	if t.ret.Reranked && len(t.ret.Candidates) > 0 {
		trial.TopScore = t.ret.Candidates[0].Rerank
	}
	return trial
}

// discardSink drops what would be shown to a person.
type discardSink struct{}

func (discardSink) Meta(Meta) error    { return nil }
func (discardSink) Token(string) error { return nil }
func (discardSink) Done(Answer) error  { return nil }
func (discardSink) Fail(string) error  { return nil }
