package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/search"
)

const (
	// maxQuestionRunes bounds what a person can send to the models in one question.
	maxQuestionRunes = 2000
	// streamPing keeps proxies from closing a connection that is waiting for the model (ATLAS.md §11.2).
	streamPing = 15 * time.Second
)

// Answers is what the HTTP layer needs from the search module. *search.Service implements it.
type Answers interface {
	Ask(ctx context.Context, q search.Question, sink search.Sink) error
	Conversations(ctx context.Context, who search.Actor, page search.ConversationPage) ([]search.ConversationSummary, uuid.UUID, error)
	Thread(ctx context.Context, who search.Actor, id uuid.UUID) (search.Conversation, []search.Answer, error)
	Feedback(ctx context.Context, who search.Actor, f search.FeedbackRequest) error
}

func searchActor(ctx context.Context) search.Actor {
	p := principalFrom(ctx)
	return search.Actor{TenantID: p.TenantID, UserID: p.UserID}
}

// Ask answers a question as a stream of server-sent events: meta, token*, then done or error.
func (s *Server) Ask(ctx context.Context, req AskRequestObject) (AskResponseObject, error) {
	text := strings.TrimSpace(req.Body.Question)
	if text == "" || utf8.RuneCountInString(text) > maxQuestionRunes {
		return nil, fmt.Errorf("a question has 1 to %d characters: %w", maxQuestionRunes, errs.ErrInvalid)
	}
	q := search.Question{Who: searchActor(ctx), Text: text, Scope: req.Body.Scope}
	if req.Body.ConversationId != nil {
		q.ConversationID = *req.Body.ConversationId
	}
	return askStream{ctx: ctx, question: q, answers: s.Answers}, nil
}

// askStream writes the answer while the search module produces it. The work happens in Visit because the
// response is the stream itself.
type askStream struct {
	ctx      context.Context
	question search.Question
	answers  Answers
}

func (a askStream) VisitAskResponse(w http.ResponseWriter) error {
	sink := &eventSink{w: w}
	stop := sink.keepAlive(a.ctx)
	defer stop()
	err := a.answers.Ask(a.ctx, a.question, sink)
	if err != nil && !sink.started() {
		writeProblem(w, ProblemFor(err)) // the question could not start: a normal error response
	}
	return nil
}

// eventSink writes search events as SSE frames: one AskEvent per `data:` line.
type eventSink struct {
	w  http.ResponseWriter
	mu sync.Mutex
	on bool
}

func (e *eventSink) started() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.on
}

func (e *eventSink) Meta(m search.Meta) error {
	return e.send(AskMetaEvent{Type: "meta", AnswerId: m.AnswerID, Citations: citationsJSON(m.Citations), Notes: notesOf(m.Notes)})
}

func (e *eventSink) Token(t string) error { return e.send(AskTokenEvent{Type: "token", T: t}) }

func (e *eventSink) Done(a search.Answer) error {
	return e.send(AskDoneEvent{Type: "done", Answer: answerJSON(a)})
}

func (e *eventSink) Fail(message string) error {
	return e.send(AskErrorEvent{Type: "error", Message: message})
}

func (e *eventSink) send(event any) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return e.write("data: " + string(body) + "\n\n")
}

// write sends one frame. Headers go out with the first one, so an error before it can still be a normal response.
func (e *eventSink) write(frame string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.on {
		h := e.w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // tell nginx-style proxies not to hold the stream
		e.on = true
	}
	if _, err := fmt.Fprint(e.w, frame); err != nil {
		return err
	}
	return http.NewResponseController(e.w).Flush()
}

// keepAlive pings while the stream is open; the returned function stops it.
func (e *eventSink) keepAlive(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(streamPing)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if e.started() {
					_ = e.write(": ping\n\n")
				}
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(done) }
}

func notesOf(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}

func citationsJSON(cs []search.Citation) []Citation {
	out := make([]Citation, len(cs))
	for i, c := range cs {
		out[i] = Citation{N: c.N, DocumentId: c.DocumentID, VersionId: c.VersionID, Page: c.Page, Section: c.Section,
			Quote: c.Quote, Score: float32(c.Score)}
		if c.Box != nil {
			out[i].Bbox = &Rect{X: float32(c.Box.X), Y: float32(c.Box.Y), W: float32(c.Box.W), H: float32(c.Box.H)}
		}
	}
	return out
}

func answerJSON(a search.Answer) Answer {
	nearby := make([]openapi_types.UUID, len(a.Nearby))
	copy(nearby, a.Nearby)
	out := Answer{Id: a.ID, ConversationId: a.ConversationID, Question: a.Question, AskedById: a.AskedByID, AskedAt: a.AskedAt,
		Outcome: Outcome(a.Outcome), Text: a.Text, Citations: citationsJSON(a.Citations), Notes: notesOf(a.Notes), Nearby: nearby,
		LatencyMs: a.LatencyMs, Model: a.Model, Scope: a.Scope}
	if a.Feedback != nil {
		f := AnswerFeedback(*a.Feedback)
		out.Feedback = &f
	}
	return out
}

// ListConversations returns the person's own conversations, newest first.
func (s *Server) ListConversations(ctx context.Context, req ListConversationsRequestObject) (ListConversationsResponseObject, error) {
	before, err := uuidCursor(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	list, next, err := s.Answers.Conversations(ctx, searchActor(ctx), search.ConversationPage{Before: before, Size: pageSize(req.Params.Limit)})
	if err != nil {
		return nil, err
	}
	body := make([]ConversationSummary, len(list))
	for i, c := range list {
		body[i] = ConversationSummary{Id: c.ID, Title: c.Title, UpdatedAt: c.UpdatedAt, AnswerIds: c.AnswerIDs,
			AskedById: c.AskedByID, LastOutcome: Outcome(c.LastOutcome), Sources: sourcesOrEmpty(c.Sources)}
	}
	return ListConversations200JSONResponse{Body: body, Headers: ListConversations200ResponseHeaders{NextCursor: cursorOf(next)}}, nil
}

func sourcesOrEmpty(s []string) []string { return notesOf(s) }

// GetConversation returns one of the person's conversations with its answers.
func (s *Server) GetConversation(ctx context.Context, req GetConversationRequestObject) (GetConversationResponseObject, error) {
	conv, answers, err := s.Answers.Thread(ctx, searchActor(ctx), req.Id)
	if err != nil {
		return nil, err
	}
	out := ConversationDetail{Conversation: Conversation{Id: conv.ID, Title: conv.Title, UpdatedAt: conv.UpdatedAt, AnswerIds: conv.AnswerIDs}}
	out.Answers = make([]Answer, len(answers))
	for i, a := range answers {
		out.Answers[i] = answerJSON(a)
	}
	return GetConversation200JSONResponse(out), nil
}

// SendFeedback records a thumb on one of the person's answers.
func (s *Server) SendFeedback(ctx context.Context, req SendFeedbackRequestObject) (SendFeedbackResponseObject, error) {
	err := s.Answers.Feedback(ctx, searchActor(ctx), search.FeedbackRequest{AnswerID: req.Id, Value: string(req.Body.Value)})
	if err != nil {
		return nil, err
	}
	return SendFeedback204Response{}, nil
}
