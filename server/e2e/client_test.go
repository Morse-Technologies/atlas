//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// api is a browser-like client for a running Atlas: it keeps cookies and accepts Caddy's own certificate.
type api struct {
	t    *testing.T
	base string
	http *http.Client
}

func newAPI(t *testing.T) *api {
	t.Helper()
	base := os.Getenv("ATLAS_E2E_URL")
	if base == "" {
		t.Skip("ATLAS_E2E_URL is not set: start the stack (deploy/compose, profile fake) and point it at https://localhost")
	}
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // Caddy's internal CA, local only
	return &api{t: t, base: strings.TrimRight(base, "/"), http: &http.Client{Jar: jar, Transport: transport, Timeout: 2 * time.Minute}}
}

// reply is a finished response with its body read.
type reply struct {
	status int
	header http.Header
	body   []byte
}

func (r reply) json(t *testing.T, into any) {
	t.Helper()
	if err := json.Unmarshal(r.body, into); err != nil {
		t.Fatalf("not JSON (status %d): %s", r.status, r.body)
	}
}

type request struct {
	method  string
	path    string
	body    any
	headers map[string]string
}

func (a *api) do(r request) reply {
	a.t.Helper()
	var payload io.Reader
	if r.body != nil {
		raw, _ := json.Marshal(r.body)
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(r.method, a.base+"/api/v1"+r.path, payload)
	if err != nil {
		a.t.Fatal(err)
	}
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	return a.send(req)
}

func (a *api) send(req *http.Request) reply {
	a.t.Helper()
	resp, err := a.http.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, header: resp.Header, body: body}
}

func (a *api) get(path string) reply { return a.do(request{method: http.MethodGet, path: path}) }

func (a *api) post(path string, body any) reply {
	return a.do(request{method: http.MethodPost, path: path, body: body})
}

func (a *api) patch(path string, body any) reply {
	return a.do(request{method: http.MethodPatch, path: path, body: body})
}

func expect(t *testing.T, r reply, status int) reply {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d: %s", r.status, status, r.body)
	}
	return r
}

// upload sends one file as multipart under the "files" field.
func (a *api) upload(name string, content []byte) reply {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("files", name)
	_, _ = part.Write(content)
	_ = w.Close()
	req, _ := http.NewRequest(http.MethodPost, a.base+"/api/v1/uploads", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return a.send(req)
}

// sseEvent is one event of the answer stream.
type sseEvent struct {
	Type    string          `json:"type"`
	T       string          `json:"t"`
	Message string          `json:"message"`
	Answer  json.RawMessage `json:"answer"`
}

// ask posts a question and reads the whole event stream.
func (a *api) ask(question string) (events []sseEvent, answer answerJSON) {
	a.t.Helper()
	raw, _ := json.Marshal(map[string]any{"question": question})
	req, _ := http.NewRequest(http.MethodPost, a.base+"/api/v1/ask", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := a.http.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		body, _ := io.ReadAll(resp.Body)
		a.t.Fatalf("ask: status %d, type %q: %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var ev sseEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			a.t.Fatalf("unreadable event %q: %v", data, err)
		}
		events = append(events, ev)
		if ev.Type == "done" {
			_ = json.Unmarshal(ev.Answer, &answer)
		}
	}
	return events, answer
}

type answerJSON struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	Outcome        string `json:"outcome"`
	Text           string `json:"text"`
	Nearby         []string
	Citations      []struct {
		N          int     `json:"n"`
		DocumentID string  `json:"document_id"`
		Page       int     `json:"page"`
		Score      float64 `json:"score"`
		Bbox       *struct {
			W float64 `json:"w"`
		} `json:"bbox"`
	} `json:"citations"`
}

// eventually polls until check returns true, or fails the test after the timeout.
func eventually(t *testing.T, timeout time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("gave up waiting for %s after %s", what, timeout)
}

// secretOf reads the shared secret out of an otpauth:// address.
func secretOf(t *testing.T, uri string) []byte {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(u.Query().Get("secret"))
	if err != nil {
		t.Fatalf("secret in %q: %v", uri, err)
	}
	return secret
}

func describe(v any) string { return fmt.Sprintf("%+v", v) }
