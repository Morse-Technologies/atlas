// Package llm streams chat completions from any OpenAI-compatible server: vLLM or Ollama (ATLAS.md §11, ADR-0008).
package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/morse/atlas/server/internal/platform/models"
)

const (
	completionsPath = "/chat/completions"
	modelsPath      = "/models"
	dataPrefix      = "data: "
	doneMarker      = "[DONE]"
	// connectTimeout limits how long the server may take to start answering; the stream itself ends with the context.
	connectTimeout = 60 * time.Second
	maxLineBytes   = 1 << 20
)

// Client talks to one chat server.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// New returns a client. baseURL includes the version path, for example http://llm:8000/v1.
func New(baseURL, model string) *Client {
	return &Client{baseURL: baseURL, model: model, http: &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: connectTimeout}}}
}

// Model is the name sent to the server and stored with every answer.
func (c *Client) Model() string { return c.model }

// Message is one turn of the prompt.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is one completion. Priority orders the vLLM queue: lower runs first, so questions use 0 (ATLAS.md §11.4).
type Request struct {
	Messages  []Message
	MaxTokens int
	Priority  int
}

// Health reports whether the server lists its models.
func (c *Client) Health(ctx context.Context) error {
	resp, err := models.Get(ctx, c.http, c.baseURL+modelsPath)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// Stream sends the request and calls onToken for every piece of text, until the model ends or ctx is cancelled.
func (c *Client) Stream(ctx context.Context, req Request, onToken func(string) error) error {
	body := map[string]any{"model": c.model, "messages": req.Messages, "stream": true, "temperature": 0,
		"max_tokens": req.MaxTokens, "priority": req.Priority}
	resp, err := models.OpenJSON(ctx, models.Endpoint{Client: c.http, URL: c.baseURL + completionsPath}, body)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLineBytes)
	for scanner.Scan() {
		done, err := handleLine(scanner.Text(), onToken)
		if done || err != nil {
			return err
		}
	}
	return streamEnd(ctx, scanner.Err())
}

// handleLine reads one SSE line. It reports done on the end marker.
func handleLine(line string, onToken func(string) error) (bool, error) {
	data, ok := strings.CutPrefix(line, dataPrefix)
	if !ok {
		return false, nil
	}
	if data == doneMarker {
		return true, nil
	}
	var c chunk
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		return false, fmt.Errorf("llm: unreadable chunk: %w", err)
	}
	for _, choice := range c.Choices {
		if choice.Delta.Content == "" {
			continue
		}
		if err := onToken(choice.Delta.Content); err != nil {
			return false, err
		}
	}
	return false, nil
}

func streamEnd(ctx context.Context, err error) error {
	if err != nil {
		return fmt.Errorf("llm: stream interrupted: %w", err)
	}
	return ctx.Err()
}
