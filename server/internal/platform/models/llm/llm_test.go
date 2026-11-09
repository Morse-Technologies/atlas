package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const streamBody = `data: {"choices":[{"delta":{"role":"assistant"}}]}

data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`

func client(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-model")
}

func collect(c *Client) (string, error) {
	var sb strings.Builder
	err := c.Stream(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}},
		func(s string) error { sb.WriteString(s); return nil })
	return sb.String(), err
}

func TestStreamDeliversEveryPieceInOrder(t *testing.T) {
	text, err := collect(client(t, streamBody))
	if err != nil || text != "Hello world" {
		t.Fatalf("text %q, err %v", text, err)
	}
}

func TestStreamStopsAtTheEndMarker(t *testing.T) {
	text, _ := collect(client(t, streamBody+"data: {\"choices\":[{\"delta\":{\"content\":\"late\"}}]}\n\n"))
	if text != "Hello world" {
		t.Fatalf("text %q; nothing after [DONE] may be read", text)
	}
}

func TestACallbackErrorStopsTheStream(t *testing.T) {
	stop := errors.New("client left")
	err := client(t, streamBody).Stream(context.Background(), Request{}, func(string) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("err %v, want the callback error", err)
	}
}

func TestACancelledContextEndsTheStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client(t, streamBody).Stream(ctx, Request{}, func(string) error { return nil }); err == nil {
		t.Fatal("a cancelled context must end the stream with an error")
	}
}
