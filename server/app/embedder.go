package app

import (
	"context"
	"fmt"

	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/platform/config"
	"github.com/morse/atlas/server/internal/platform/models/ollama"
	"github.com/morse/atlas/server/internal/platform/models/tei"
	"github.com/morse/atlas/server/internal/search"
)

const (
	embedAPITEI    = "tei"
	embedAPIOllama = "ollama"
)

// embedding is the embedding service as configured: the client, its readiness probe, and a token counter
// when the server has a tokenizer (Ollama has none, chunk sizes are then approximated).
type embedding struct {
	embedder search.Embedder
	health   func(context.Context) error
	tokens   documents.TokenCounter
}

// newEmbedding builds the client for the kind of server in the environment.
func newEmbedding(cfg config.Embedding) (embedding, error) {
	switch cfg.API {
	case embedAPITEI:
		client := tei.New(cfg.URL)
		return embedding{embedder: client, health: client.Health, tokens: client}, nil
	case embedAPIOllama:
		client := ollama.New(cfg.URL, cfg.Model)
		return embedding{embedder: client, health: client.Health}, nil
	}
	return embedding{}, fmt.Errorf("ATLAS_EMBED_API %q is not %s or %s", cfg.API, embedAPITEI, embedAPIOllama)
}
