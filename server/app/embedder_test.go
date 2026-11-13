package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/config"
)

// slowServer answers any request after pause, with an empty body.
func slowServer(t *testing.T, pause time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(pause)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTheEmbeddingTimeoutLimitsEachKindOfServer(t *testing.T) {
	const pause, limit = 300 * time.Millisecond, 50 * time.Millisecond
	srv := slowServer(t, pause)
	for _, api := range []string{embedAPITEI, embedAPIOllama} {
		e, err := newEmbedding(config.Embedding{URL: srv.URL, Model: "bge-m3", API: api, Timeout: limit})
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		_, err = e.embedder.Embed(t.Context(), []string{"x"})
		if err == nil || time.Since(started) >= pause {
			t.Errorf("%s: Embed = %v after %v; the %v limit should have cut the call", api, err, time.Since(started), limit)
		}
	}
}

func TestAnUnknownEmbeddingServerKindIsRefused(t *testing.T) {
	if _, err := newEmbedding(config.Embedding{URL: "http://x", API: "other"}); err == nil {
		t.Fatal("an API kind that is not built must be refused")
	}
}
