package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

const (
	requestTimeout = 3 * time.Minute
	scannerBuffer  = 1 << 20
	dataPrefix     = "data: "
	apiPath        = "/api/v1"
	idleConns      = 256
)

// client talks to a running Atlas the way a browser does: one cookie, Caddy's own certificate trusted.
type client struct {
	base string
	http *http.Client
}

func newClient(base, session string) (*client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, MaxIdleConnsPerHost: idleConns} //nolint:gosec // local, Caddy's internal CA
	c := &client{base: strings.TrimRight(base, "/"), http: &http.Client{Jar: jar, Transport: transport, Timeout: requestTimeout}}
	if session != "" {
		req, _ := http.NewRequest(http.MethodGet, c.base, nil)
		jar.SetCookies(req.URL, []*http.Cookie{{Name: "atlas_session", Value: session, Path: "/"}})
	}
	return c, nil
}

// call sends a request and returns the status, the body and how long it took.
func (c *client) call(method, path string, body any) (int, []byte, time.Duration, error) {
	var payload io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+apiPath+path, payload)
	if err != nil {
		return 0, nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, time.Since(started), err
}

// timings are the moments of an answer, from the request: the sources, the first word, the end.
type timings struct {
	Meta, FirstToken, Done time.Duration
	Outcome                string
	Cited                  []string // ids of the documents cited
}

type streamEvent struct {
	Type   string `json:"type"`
	Answer struct {
		Outcome   string `json:"outcome"`
		Citations []struct {
			DocumentID string `json:"document_id"`
		} `json:"citations"`
	} `json:"answer"`
}

// ask posts a question and reads the answer stream, noting when each part arrives.
func (c *client) ask(question string) (timings, error) {
	raw, _ := json.Marshal(map[string]any{"question": question})
	req, err := http.NewRequest(http.MethodPost, c.base+apiPath+"/ask", bytes.NewReader(raw))
	if err != nil {
		return timings{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return timings{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return timings{}, fmt.Errorf("ask: status %d", resp.StatusCode)
	}
	return readStream(resp.Body, started)
}

func readStream(body io.Reader, started time.Time) (timings, error) {
	var t timings
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, scannerBuffer), scannerBuffer)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), dataPrefix)
		if !ok {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return t, fmt.Errorf("unreadable event: %w", err)
		}
		note(&t, ev, time.Since(started))
	}
	return t, scanner.Err()
}

func note(t *timings, ev streamEvent, at time.Duration) {
	switch ev.Type {
	case "meta":
		t.Meta = at
	case "token":
		if t.FirstToken == 0 {
			t.FirstToken = at
		}
	case "done":
		t.Done, t.Outcome = at, ev.Answer.Outcome
		for _, c := range ev.Answer.Citations {
			t.Cited = append(t.Cited, c.DocumentID)
		}
	}
}
