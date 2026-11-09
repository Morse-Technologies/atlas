// Package ollama embeds text with an Ollama server. It is the light option for trials on a small computer:
// the same bge-m3 model as TEI, behind Ollama's own API. Ollama has no reranker and no tokenizer endpoint, so
// search ranks by fusion and chunk sizes are approximated.
package ollama

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/morse/atlas/server/internal/platform/models"
)

const (
	embedPath  = "/api/embed"
	healthPath = "/api/tags"
	// requestTimeout is long because a CPU loads the model on the first call.
	requestTimeout = 60 * time.Second
	// batchSize keeps one request small for a CPU.
	batchSize = 16
)

// Client embeds with one Ollama model.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// New returns a client for the model served at baseURL.
func New(baseURL, model string) *Client {
	return &Client{baseURL: baseURL, model: model, http: &http.Client{Timeout: requestTimeout}}
}

// Health reports whether the server answers.
func (c *Client) Health(ctx context.Context) error {
	resp, err := models.Get(ctx, c.http, c.baseURL+healthPath)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

type embedReply struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed returns one vector per text, in order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		batch := texts[start:min(start+batchSize, len(texts))]
		vectors, err := c.embedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}
	return out, nil
}

func (c *Client) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	var reply embedReply
	call := models.JSONCall{In: map[string]any{"model": c.model, "input": batch, "truncate": true}, Out: &reply}
	endpoint := models.Endpoint{Client: c.http, URL: c.baseURL + embedPath}
	if err := models.PostJSON(ctx, endpoint, call); err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(reply.Embeddings) != len(batch) {
		return nil, fmt.Errorf("embed: asked for %d vectors, got %d", len(batch), len(reply.Embeddings))
	}
	return reply.Embeddings, nil
}
