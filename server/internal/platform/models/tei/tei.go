// Package tei talks to Hugging Face text-embeddings-inference: embeddings, reranking and tokenization
// (ATLAS.md §8, §11). Embedding and reranking run as two instances, so a Client is built per URL.
package tei

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/morse/atlas/server/internal/platform/models"
)

const (
	embedPath    = "/embed"
	rerankPath   = "/rerank"
	tokenizePath = "/tokenize"
	healthPath   = "/health"
	// requestTimeout is the per-call limit of ATLAS.md §16.2.
	requestTimeout = 5 * time.Second
	// batchSize keeps one request small enough for TEI's default limits.
	batchSize = 32
	// Dimension is the size of bge-m3 vectors, the model the HNSW index of migration 0011 is built for.
	Dimension = 1024
	// bytesPerValue sizes the buffer of a vector literal; float32Bits is the precision it prints.
	bytesPerValue = 8
	float32Bits   = 32
)

// Client talks to one TEI instance.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for one instance.
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: requestTimeout}}
}

// WithTimeout sets the per-call limit. The default suits a GPU; a trial on an emulated CPU needs more.
func (c *Client) WithTimeout(d time.Duration) *Client {
	c.http.Timeout = d
	return c
}

func (c *Client) endpoint(path string) models.Endpoint {
	return models.Endpoint{Client: c.http, URL: c.baseURL + path}
}

// Health reports whether the instance answers.
func (c *Client) Health(ctx context.Context) error {
	resp, err := models.Get(ctx, c.http, c.baseURL+healthPath)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// Embed returns one vector per text, in order. Long inputs are truncated by the server.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		batch := texts[start:min(start+batchSize, len(texts))]
		var vectors [][]float32
		call := models.JSONCall{In: map[string]any{"inputs": batch, "truncate": true}, Out: &vectors}
		if err := models.PostJSON(ctx, c.endpoint(embedPath), call); err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
		if len(vectors) != len(batch) {
			return nil, fmt.Errorf("embed: asked for %d vectors, got %d", len(batch), len(vectors))
		}
		out = append(out, vectors...)
	}
	return out, nil
}

// Score is the relevance of one text to the query, between 0 and 1.
type Score struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}

// Rerank scores texts against query and returns them in the order given (not sorted).
func (c *Client) Rerank(ctx context.Context, query string, texts []string) ([]float64, error) {
	var scores []Score
	call := models.JSONCall{In: map[string]any{"query": query, "texts": texts, "truncate": true}, Out: &scores}
	if err := models.PostJSON(ctx, c.endpoint(rerankPath), call); err != nil {
		return nil, fmt.Errorf("rerank: %w", err)
	}
	out := make([]float64, len(texts))
	for _, s := range scores {
		if s.Index < 0 || s.Index >= len(out) {
			return nil, fmt.Errorf("rerank: index %d out of range", s.Index)
		}
		out[s.Index] = s.Score
	}
	return out, nil
}

type token struct {
	Special bool `json:"special"`
}

// CountTokens returns how many tokens each text has for this instance's tokenizer.
func (c *Client) CountTokens(ctx context.Context, texts []string) ([]int, error) {
	counts := make([]int, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		batch := texts[start:min(start+batchSize, len(texts))]
		var tokens [][]token
		call := models.JSONCall{In: map[string]any{"inputs": batch, "add_special_tokens": false}, Out: &tokens}
		if err := models.PostJSON(ctx, c.endpoint(tokenizePath), call); err != nil {
			return nil, fmt.Errorf("tokenize: %w", err)
		}
		if len(tokens) != len(batch) {
			return nil, fmt.Errorf("tokenize: asked for %d counts, got %d", len(batch), len(tokens))
		}
		for _, t := range tokens {
			counts = append(counts, len(t))
		}
	}
	return counts, nil
}

// VectorLiteral is the text form pgvector reads: [0.1,0.2,...].
func VectorLiteral(v []float32) string {
	buf := make([]byte, 0, len(v)*bytesPerValue)
	buf = append(buf, '[')
	for i, x := range v {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = strconv.AppendFloat(buf, float64(x), 'g', -1, float32Bits)
	}
	return string(append(buf, ']'))
}
