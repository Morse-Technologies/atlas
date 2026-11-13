package search

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/models/llm"
	"github.com/morse/atlas/server/internal/search/searchdb"
)

//go:embed prompts/rewrite.v1.md
var rewritePromptText string

const (
	followUpMaxWords  = 8   // a question shorter than this, asked after another one, is taken as a follow-up
	rewriteMaxTokens  = 64  // a standalone question is one short line
	rewrittenMaxRunes = 300 // anything longer is not a question: the original is searched instead
	searchedForNote   = "Searched for: "
	previousHead      = "Previous question: "
	lastHead          = "\nLast question: "
	quoteMarks        = "\"'“”«»`"
)

// followUpStarters are the words (lower case) that begin a question which only makes sense after another one.
var followUpStarters = []string{"y", "¿y", "e", "and", "what about", "how about", "also", "también"}

// isFollowUp says whether a question probably leans on the previous one: it is short, or it begins with a connector.
func isFollowUp(question string) bool {
	words := strings.Fields(strings.ToLower(question))
	return len(words) < followUpMaxWords || beginsWithConnector(words)
}

func beginsWithConnector(words []string) bool {
	for _, starter := range followUpStarters {
		length := len(strings.Fields(starter))
		if len(words) >= length && strings.Join(words[:length], " ") == starter {
			return true
		}
	}
	return false
}

// rewritePrompt asks the model for a standalone version of question, using the previous one as context.
// The fake model of the development stack (cmd/fakemodels) recognizes this layout.
func rewritePrompt(previous, question string) []llm.Message {
	return []llm.Message{
		{Role: "system", Content: rewritePromptText},
		{Role: "user", Content: previousHead + previous + lastHead + question},
	}
}

// standalone is the text to search for. A follow-up is rewritten with the previous question of its conversation so
// it makes sense alone; every other question, and any failure to rewrite, searches for the question as asked.
func (s *Service) standalone(ctx context.Context, t *turn) string {
	if t.q.ConversationID == uuid.Nil || s.LLM == nil || !isFollowUp(t.q.Text) {
		return t.q.Text
	}
	rewritten, err := s.rewriteFollowUp(ctx, t)
	if err != nil {
		slog.WarnContext(ctx, "follow-up not rewritten, searching as asked", "error", err)
		return t.q.Text
	}
	t.rewritten = rewritten
	t.notes = append(t.notes, searchedForNote+rewritten)
	return rewritten
}

// rewriteFollowUp returns the standalone question, or an error if the conversation has no earlier question or the
// model gave nothing usable.
func (s *Service) rewriteFollowUp(ctx context.Context, t *turn) (string, error) {
	previous, err := s.lastQuestion(ctx, t)
	if err != nil {
		return "", fmt.Errorf("read previous question: %w", err)
	}
	text, err := s.askToRewrite(ctx, rewritePrompt(previous, t.q.Text))
	if err != nil {
		return "", err
	}
	return cleanRewrite(text)
}

func (s *Service) lastQuestion(ctx context.Context, t *turn) (string, error) {
	var question string
	err := s.DB.InTenantTx(ctx, t.who.TenantID, func(tx pgx.Tx) error {
		var getErr error
		question, getErr = searchdb.New(tx).GetLastQuestion(ctx, searchdb.GetLastQuestionParams{ConversationID: t.q.ConversationID, UserID: t.who.UserID})
		return db.NotFoundIfNoRows(getErr, "previous question")
	})
	return question, err
}

// askToRewrite runs the model in one of the same slots as answers, so rewriting never adds load beyond the limit.
func (s *Service) askToRewrite(ctx context.Context, messages []llm.Message) (string, error) {
	if err := s.slots.Acquire(ctx, 1); err != nil {
		return "", fmt.Errorf("wait for the model: %w", err)
	}
	defer s.slots.Release(1)
	var text strings.Builder
	err := s.LLM.Stream(ctx, llm.Request{Messages: messages, MaxTokens: rewriteMaxTokens, Priority: askPriority},
		func(tok string) error { text.WriteString(tok); return nil })
	if err != nil {
		return "", fmt.Errorf("rewrite the question: %w", err)
	}
	return text.String(), nil
}

// cleanRewrite keeps the first line of the model's reply, without quotes. A reply that is empty or too long to be a
// question is an error.
func cleanRewrite(reply string) (string, error) {
	line, _, _ := strings.Cut(strings.TrimSpace(reply), "\n")
	line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), quoteMarks))
	if line == "" || utf8.RuneCountInString(line) > rewrittenMaxRunes {
		return "", fmt.Errorf("the model's rewrite is not a question (%d characters)", utf8.RuneCountInString(line))
	}
	return line, nil
}
