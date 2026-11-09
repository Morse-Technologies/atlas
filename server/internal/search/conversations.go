package search

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/audit"
	"github.com/morse/atlas/server/internal/platform/errs"
	"github.com/morse/atlas/server/internal/search/searchdb"
)

const maxSourcesShown = 5

// Conversation is a thread of questions and answers.
type Conversation struct {
	ID        uuid.UUID
	Title     string
	UpdatedAt time.Time
	AnswerIDs []uuid.UUID
}

// ConversationSummary is a conversation as listed.
type ConversationSummary struct {
	Conversation
	AskedByID   uuid.UUID
	LastOutcome Outcome
	Sources     []string
}

// ConversationPage selects conversations older than Before (uuid.Nil: the newest).
type ConversationPage struct {
	Before uuid.UUID
	Size   int32
}

// Conversations lists the person's own conversations, newest first, and the cursor of the next page.
func (s *Service) Conversations(ctx context.Context, who Actor, page ConversationPage) ([]ConversationSummary, uuid.UUID, error) {
	var out []ConversationSummary
	err := s.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		var listErr error
		out, listErr = listSummaries(ctx, searchdb.New(tx), ownList{who: who, page: page})
		return listErr
	})
	if err != nil || int32(len(out)) <= page.Size {
		return out, uuid.Nil, err
	}
	return out[:page.Size], out[page.Size-1].ID, nil
}

// ownList is a person and the page of their conversations to read.
type ownList struct {
	who  Actor
	page ConversationPage
}

func listSummaries(ctx context.Context, q *searchdb.Queries, in ownList) ([]ConversationSummary, error) {
	before := in.page.Before
	if before == uuid.Nil {
		before = uuid.Max
	}
	rows, err := q.ListOwnConversations(ctx, searchdb.ListOwnConversationsParams{UserID: in.who.UserID, Before: before, PageSize: in.page.Size + 1})
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	answers, labels, err := answersAndSources(ctx, q, rows)
	if err != nil {
		return nil, err
	}
	out := make([]ConversationSummary, len(rows))
	for i, r := range rows {
		out[i] = ConversationSummary{Conversation: Conversation{ID: r.ID, Title: r.Title, UpdatedAt: r.UpdatedAt, AnswerIDs: answerIDs(answers, r.ID)},
			AskedByID: r.UserID, LastOutcome: Outcome(r.LastOutcome), Sources: labels[r.ID]}
	}
	return out, nil
}

// answersAndSources reads the answers of the listed conversations and the documents they cite.
func answersAndSources(ctx context.Context, q *searchdb.Queries, rows []searchdb.ListOwnConversationsRow) ([]searchdb.SearchAnswer, map[uuid.UUID][]string, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	answers, err := q.ListAnswersOf(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("list answers: %w", err)
	}
	labels, err := sourcesOf(ctx, q, answers)
	return answers, labels, err
}

func answerIDs(answers []searchdb.SearchAnswer, conversation uuid.UUID) []uuid.UUID {
	ids := []uuid.UUID{}
	for _, a := range answers {
		if a.ConversationID == conversation {
			ids = append(ids, a.ID)
		}
	}
	return ids
}

// sourcesOf names the documents each conversation cited, at most maxSourcesShown each.
func sourcesOf(ctx context.Context, q *searchdb.Queries, answers []searchdb.SearchAnswer) (map[uuid.UUID][]string, error) {
	ids := make([]uuid.UUID, len(answers))
	owner := make(map[uuid.UUID]uuid.UUID, len(answers))
	for i, a := range answers {
		ids[i], owner[a.ID] = a.ID, a.ConversationID
	}
	cites, err := q.ListCitationsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list citations: %w", err)
	}
	seen := map[[2]string]bool{}
	out := map[uuid.UUID][]string{}
	for _, c := range cites {
		conv := owner[c.AnswerID]
		key := [2]string{conv.String(), c.Label}
		if !seen[key] && len(out[conv]) < maxSourcesShown {
			seen[key] = true
			out[conv] = append(out[conv], c.Label)
		}
	}
	return out, nil
}

// Thread returns one of the person's conversations with all its answers and their citations.
func (s *Service) Thread(ctx context.Context, who Actor, id uuid.UUID) (Conversation, []Answer, error) {
	conv, err := s.ownConversation(ctx, who, id)
	if err != nil {
		return Conversation{}, nil, err
	}
	var answers []Answer
	err = s.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		var readErr error
		answers, readErr = readAnswers(ctx, searchdb.New(tx), id)
		return readErr
	})
	ids := make([]uuid.UUID, len(answers))
	for i, a := range answers {
		ids[i] = a.ID
	}
	return Conversation{ID: conv.ID, Title: conv.Title, UpdatedAt: conv.UpdatedAt, AnswerIDs: ids}, answers, err
}

func readAnswers(ctx context.Context, q *searchdb.Queries, conversation uuid.UUID) ([]Answer, error) {
	rows, err := q.ListAnswersOf(ctx, []uuid.UUID{conversation})
	if err != nil {
		return nil, fmt.Errorf("list answers: %w", err)
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	cites, err := q.ListCitationsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list citations: %w", err)
	}
	answers := make([]Answer, len(rows))
	for i, r := range rows {
		answers[i] = storedAnswer(r)
		answers[i].Citations = citationsFor(cites, r.ID)
	}
	return answers, nil
}

func storedAnswer(r searchdb.SearchAnswer) Answer {
	return Answer{ID: r.ID, ConversationID: r.ConversationID, Question: r.Question, AskedByID: r.UserID, AskedAt: r.AskedAt,
		Outcome: Outcome(r.Outcome), Text: r.Text, Notes: notNil(r.Notes), Nearby: notNilIDs(r.Nearby), LatencyMs: int(r.LatencyMs),
		Model: r.Model, Scope: r.Scope, Feedback: r.Feedback}
}

func citationsFor(rows []searchdb.ListCitationsOfRow, answer uuid.UUID) []Citation {
	out := []Citation{}
	for _, r := range rows {
		if r.AnswerID == answer {
			out = append(out, Citation{N: int(r.N), DocumentID: r.DocumentID, VersionID: r.VersionID, Label: r.Label, Page: int(r.Page),
				Section: r.Section, Quote: r.Quote, Score: float64(r.Score), Box: boxOf(r.Bbox)})
		}
	}
	return out
}

// FeedbackRequest is a thumb on one of the person's answers.
type FeedbackRequest struct {
	AnswerID uuid.UUID
	Value    string // "up" or "down"
}

// Feedback records the person's verdict on their own answer.
func (s *Service) Feedback(ctx context.Context, who Actor, f FeedbackRequest) error {
	if f.Value != "up" && f.Value != "down" {
		return fmt.Errorf("feedback must be up or down: %w", errs.ErrInvalid)
	}
	return s.DB.InTenantTx(ctx, who.TenantID, func(tx pgx.Tx) error {
		n, err := searchdb.New(tx).SetFeedback(ctx, searchdb.SetFeedbackParams{ID: f.AnswerID, UserID: who.UserID, Feedback: &f.Value})
		if err != nil || n == 0 {
			return notFoundOr(err, "answer")
		}
		return audit.Record(ctx, tx, audit.Entry{At: s.Now(), Actor: who.UserID, Action: "answer.feedback",
			Subject: "answer:" + f.AnswerID.String(), Detail: "value=" + f.Value})
	})
}

func notFoundOr(err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return fmt.Errorf("%s: %w", what, errs.ErrNotFound)
}

// DayStats counts the questions asked on one day.
type DayStats struct {
	Day       time.Time
	Questions int
	Answered  int
	NotFound  int
}

// Activity is the question traffic of the last days, for the panel. Days are UTC.
// shortcut: add a tenant time zone when an organization asks for local days.
type Activity struct {
	Days            []DayStats // days with questions, oldest first
	MedianLatencyMs int        // of the answers since MedianSince
}

// Window says from when to count questions per day, and from when to take the median latency.
type Window struct {
	Since       time.Time
	MedianSince time.Time
}

// ActivitySince counts questions per day and the median latency over the window.
func (s *Service) ActivitySince(ctx context.Context, tenant uuid.UUID, w Window) (Activity, error) {
	var out Activity
	err := s.DB.InTenantTx(ctx, tenant, func(tx pgx.Tx) error {
		q := searchdb.New(tx)
		rows, err := q.CountAnswersByDay(ctx, w.Since)
		if err != nil {
			return fmt.Errorf("count answers: %w", err)
		}
		for _, r := range rows {
			out.Days = append(out.Days, DayStats{Day: r.Day, Questions: int(r.Questions), Answered: int(r.Answered), NotFound: int(r.NotFound)})
		}
		median, err := q.MedianLatency(ctx, w.MedianSince)
		out.MedianLatencyMs = int(median)
		return err
	})
	return out, err
}
