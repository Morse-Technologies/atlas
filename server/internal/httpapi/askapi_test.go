package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/search"
)

// scriptedAnswers plays a fixed answer into the sink, or fails before starting.
type scriptedAnswers struct {
	Answers
	before error
	asked  search.Question
}

func (s *scriptedAnswers) Ask(_ context.Context, q search.Question, sink search.Sink) error {
	s.asked = q
	if s.before != nil {
		return s.before
	}
	_ = sink.Meta(search.Meta{AnswerID: uuid.New(), Citations: []search.Citation{{N: 1, Page: 2, Quote: "40 Nm"}}})
	_ = sink.Token("The torque ")
	_ = sink.Token("is 40 Nm [1].")
	return sink.Done(search.Answer{ID: uuid.New(), Outcome: search.OutcomeAnswered, Text: "The torque is 40 Nm [1]."})
}

func ask(t *testing.T, srv *Server, body string) *strings.Builder {
	t.Helper()
	rec := serveTo(t, srv, adminRequest(http.MethodPost, "/api/v1/ask", body))
	out := &strings.Builder{}
	out.WriteString(rec.Header().Get("Content-Type") + "\n" + rec.Body.String())
	if rec.Code != http.StatusOK {
		out.WriteString("\nstatus " + http.StatusText(rec.Code))
	}
	return out
}

func TestAskStreamsMetaTokensThenDone(t *testing.T) {
	out := ask(t, &Server{Accounts: people, Answers: &scriptedAnswers{}}, `{"question":"torque?"}`).String()
	if !strings.HasPrefix(out, "text/event-stream\n") {
		t.Fatalf("not an event stream: %q", out)
	}
	meta, token, done := strings.Index(out, `"type":"meta"`), strings.Index(out, `"type":"token"`), strings.Index(out, `"type":"done"`)
	if meta < 0 || meta >= token || token >= done {
		t.Fatalf("events out of order (meta %d, token %d, done %d):\n%s", meta, token, done, out)
	}
	if !strings.Contains(out, "data: {") || !strings.Contains(out, "\n\n") {
		t.Fatalf("frames are not SSE:\n%s", out)
	}
}

func TestAnUnknownConversationIsAnOrdinaryNotFoundNotAStream(t *testing.T) {
	srv := &Server{Accounts: people, Answers: &scriptedAnswers{before: errs.ErrNotFound}}
	rec := serveTo(t, srv, adminRequest(http.MethodPost, "/api/v1/ask", `{"question":"x","conversation_id":"`+uuid.NewString()+`"}`))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != problemContentType {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestEmptyOrHugeQuestionsAreRefused(t *testing.T) {
	srv := &Server{Accounts: people, Answers: &scriptedAnswers{}}
	for _, q := range []string{"", "   ", strings.Repeat("a", maxQuestionRunes+1)} {
		body := `{"question":"` + q + `"}`
		if rec := serveTo(t, srv, adminRequest(http.MethodPost, "/api/v1/ask", body)); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("question of %d chars: status %d, want 422", len(q), rec.Code)
		}
	}
}

func TestTheQuestionIsAskedAsTheSignedInPerson(t *testing.T) {
	f := &scriptedAnswers{}
	ask(t, &Server{Accounts: people, Answers: f}, `{"question":"torque?","scope":"quality"}`)
	if f.asked.Who.UserID != people.sessions["admin"].UserID || f.asked.Text != "torque?" || f.asked.Scope == nil || *f.asked.Scope != "quality" {
		t.Fatalf("asked %+v", f.asked)
	}
}
