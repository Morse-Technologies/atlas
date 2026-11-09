package search

import (
	"context"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/models/llm"
)

// Outcome is how a question ended. Values match search.answers.outcome and the API.
type Outcome string

// Outcomes. "Not found" is a result, not an error (ATLAS.md §16.2).
const (
	OutcomeAnswered Outcome = "answered"
	OutcomeNotFound Outcome = "not_found"
	OutcomeRefused  Outcome = "refused"
	OutcomeError    Outcome = "error"
)

// Actor is who asks: the tenant to scope to, and the person whose permissions apply.
type Actor struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
}

// Question is what a person asks. ConversationID is uuid.Nil to start a new conversation.
type Question struct {
	Who            Actor
	Text           string
	ConversationID uuid.UUID
	Scope          *string
}

// Box is a region of a PDF page in points, origin top-left.
type Box struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Citation points at the passage a statement comes from.
type Citation struct {
	N          int
	DocumentID uuid.UUID
	VersionID  uuid.UUID
	ChunkID    uuid.UUID
	Label      string
	Page       int
	Section    string
	Quote      string
	Score      float64
	Box        *Box
}

// Answer is a stored question and its answer.
type Answer struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	Question       string
	AskedByID      uuid.UUID
	AskedAt        time.Time
	Outcome        Outcome
	Text           string
	Citations      []Citation
	Notes          []string
	Nearby         []uuid.UUID
	LatencyMs      int
	Model          string
	Scope          *string
	Feedback       *string
}

// Meta is sent first: which passages the answer will draw on, so the UI can show sources while text streams.
type Meta struct {
	AnswerID  uuid.UUID
	Citations []Citation
	Notes     []string
}

// Sink receives the answer as it is produced. The HTTP layer writes these as server-sent events.
type Sink interface {
	Meta(Meta) error
	Token(string) error
	Done(Answer) error
	Fail(message string) error
}

// Embedder turns text into vectors. *tei.Client implements it.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Reranker scores passages against a question, from 0 to 1. *tei.Client implements it.
type Reranker interface {
	Rerank(ctx context.Context, query string, texts []string) ([]float64, error)
}

// Generator writes the answer. *llm.Client implements it.
type Generator interface {
	Stream(ctx context.Context, req llm.Request, onToken func(string) error) error
	Model() string
}

// Limits of ATLAS.md §9.5 and §11.4. The not-found threshold is an organization setting (access.Settings).
const (
	DefaultMaxGenerations = 2
	contextPassages       = 6 // passages given to the model
	nearbyDocuments       = 3 // documents suggested when nothing answers
	answerMaxTokens       = 700
	askPriority           = 0 // vLLM runs lower numbers first; background work uses 10
	titleRunes            = 60
	bgeM3                 = "bge-m3" // the model the vector index of migration 0011 is built for
)

// Config is what the Service needs. Embedder, Reranker and LLM may be nil: search then degrades (ATLAS.md §11.3).
type Config struct {
	DB             *db.DB
	Embedder       Embedder
	Reranker       Reranker
	LLM            Generator
	EmbedModel     string
	RerankModel    string
	MaxGenerations int64
	CommonShare    float64 // see config.Answering; 0 searches with every word
}

// Service answers questions and keeps the conversations.
type Service struct {
	Config
	Now   func() time.Time
	slots *semaphore.Weighted

	common commonTerms
}

// NewService returns a Service that reads the real clock and runs at most MaxGenerations answers at once.
func NewService(cfg Config) *Service {
	if cfg.MaxGenerations <= 0 {
		cfg.MaxGenerations = DefaultMaxGenerations
	}
	return &Service{Config: cfg, Now: time.Now, slots: semaphore.NewWeighted(cfg.MaxGenerations)}
}
