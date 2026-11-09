package ollama

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
	return New(srv.URL, "bge-m3")
}

// echoVectors answers every input with one vector and checks the model that was asked for.
func echoVectors(t *testing.T, calls *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*calls++
		var in struct {
			Model string
			Input []string
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Model != "bge-m3" {
			t.Errorf("model %q", in.Model)
		}
		out := make([][]float32, len(in.Input))
		for i := range out {
			out[i] = []float32{1}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}
}

func TestEmbedAsksTheConfiguredModelAndSplitsIntoBatches(t *testing.T) {
	calls := 0
	c := server(t, echoVectors(t, &calls))
	vectors, err := c.Embed(context.Background(), make([]string, batchSize+1))
	if err != nil || len(vectors) != batchSize+1 || calls != 2 {
		t.Fatalf("vectors %d, calls %d, err %v", len(vectors), calls, err)
	}
}

func TestEmbedFailsWhenTheServerReturnsTheWrongCount(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"embeddings":[[1]]}`)) })
	if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("a missing vector must be an error")
	}
}

func TestHealthIsOkWhenTagsAnswer(t *testing.T) {
	c := server(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}
