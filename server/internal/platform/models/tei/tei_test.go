package tei

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func server(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestEmbedSplitsLargeInputsIntoBatches(t *testing.T) {
	calls := 0
	c := server(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in struct{ Inputs []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		out := make([][]float32, len(in.Inputs))
		for i := range out {
			out[i] = []float32{1}
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	vectors, err := c.Embed(context.Background(), make([]string, batchSize*2+1))
	if err != nil || len(vectors) != batchSize*2+1 || calls != 3 {
		t.Fatalf("vectors %d, calls %d, err %v; want 3 batches", len(vectors), calls, err)
	}
}

func TestEmbedFailsWhenTheServerReturnsTheWrongCount(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[[1]]`)) })
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("a missing vector must be an error, not silent misalignment")
	}
}

func TestRerankReturnsScoresInInputOrder(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"index":1,"score":0.9},{"index":0,"score":0.1}]`))
	})
	scores, err := c.Rerank(context.Background(), "q", []string{"a", "b"})
	if err != nil || scores[0] != 0.1 || scores[1] != 0.9 {
		t.Fatalf("scores %v, err %v", scores, err)
	}
}

func TestRerankRefusesAnIndexOutsideTheInput(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[{"index":5,"score":1}]`)) })
	if _, err := c.Rerank(context.Background(), "q", []string{"a"}); err == nil {
		t.Fatal("index out of range must be an error")
	}
}

func TestCountTokensCountsPerInput(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[[{"special":false},{"special":false}],[{"special":false}]]`))
	})
	counts, err := c.CountTokens(context.Background(), []string{"a b", "c"})
	if err != nil || counts[0] != 2 || counts[1] != 1 {
		t.Fatalf("counts %v, err %v", counts, err)
	}
}

func TestServerErrorsAreTransient(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	_, err := c.Embed(context.Background(), []string{"a"})
	if err == nil || err.Error() == "" {
		t.Fatal("a 503 must fail")
	}
}
