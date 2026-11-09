package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/platform/idempotency"
	"github.com/morse/atlas/server/internal/sources"
)

// memoryKeys keeps keys in a map, with the same rules as the Postgres store.
type memoryKeys struct {
	mu       sync.Mutex
	rows     map[string]*idempotency.Outcome
	hashes   map[string]string
	released int
}

func newMemoryKeys() *memoryKeys {
	return &memoryKeys{rows: map[string]*idempotency.Outcome{}, hashes: map[string]string{}}
}

func (m *memoryKeys) Begin(_ context.Context, r idempotency.Request) (*idempotency.Outcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hash, seen := m.hashes[r.Key]
	switch {
	case !seen:
		m.hashes[r.Key], m.rows[r.Key] = r.Hash, nil
		return nil, nil
	case hash != r.Hash:
		return nil, idempotency.ErrKeyReused
	case m.rows[r.Key] == nil:
		return nil, idempotency.ErrInProgress
	}
	return m.rows[r.Key], nil
}

func (m *memoryKeys) Finish(_ context.Context, r idempotency.Request, out idempotency.Outcome) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[r.Key] = &out
	return nil
}

func (m *memoryKeys) Release(_ context.Context, r idempotency.Request) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.hashes, r.Key)
	delete(m.rows, r.Key)
	m.released++
	return nil
}

type countingSources struct {
	SourceAdmin
	pauses int
	fail   bool
}

func (c *countingSources) Pause(context.Context, sources.Actor, uuid.UUID) (sources.Source, error) {
	c.pauses++
	if c.fail {
		return sources.Source{}, context.DeadlineExceeded
	}
	return sources.Source{Status: "paused"}, nil
}

// keyed is a pause request: the Idempotency-Key it carries (may be empty) and its JSON body.
type keyed struct{ key, body string }

func pause(t *testing.T, srv *Server, k keyed) *httptest.ResponseRecorder {
	t.Helper()
	req := adminRequest(http.MethodPatch, "/api/v1/sources/"+uuid.NewString(), k.body)
	req.URL.Path = "/api/v1/sources/00000000-0000-0000-0000-000000000001"
	if k.key != "" {
		req.Header.Set("Idempotency-Key", k.key)
	}
	return serveTo(t, srv, req)
}

func TestRetryWithTheSameKeyRunsTheChangeOnce(t *testing.T) {
	c := &countingSources{}
	srv := &Server{Accounts: people, Sources: c, Idempotency: newMemoryKeys()}
	first := pause(t, srv, keyed{"k1", `{"paused":true}`})
	second := pause(t, srv, keyed{"k1", `{"paused":true}`})
	if c.pauses != 1 || second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("pauses %d, codes %d/%d; want one run and the same answer", c.pauses, first.Code, second.Code)
	}
	if second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("a replay must say so")
	}
}

func TestSameKeyWithAnotherBodyIsRefused(t *testing.T) {
	srv := &Server{Accounts: people, Sources: &countingSources{}, Idempotency: newMemoryKeys()}
	pause(t, srv, keyed{"k2", `{"paused":true}`})
	if rec := pause(t, srv, keyed{"k2", `{"paused":false}`}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
}

func TestAServerFailureFreesTheKeyForARetry(t *testing.T) {
	c, keys := &countingSources{fail: true}, newMemoryKeys()
	srv := &Server{Accounts: people, Sources: c, Idempotency: keys}
	if rec := pause(t, srv, keyed{"k3", `{"paused":true}`}); rec.Code < 500 {
		t.Fatalf("status %d, want a server error", rec.Code)
	}
	c.fail = false
	pause(t, srv, keyed{"k3", `{"paused":true}`})
	if c.pauses != 2 || keys.released != 1 {
		t.Fatalf("pauses %d released %d; the retry must run again", c.pauses, keys.released)
	}
}

func TestRequestsWithoutAKeyAreNotRecorded(t *testing.T) {
	c := &countingSources{}
	srv := &Server{Accounts: people, Sources: c, Idempotency: newMemoryKeys()}
	pause(t, srv, keyed{"", `{"paused":true}`})
	pause(t, srv, keyed{"", `{"paused":true}`})
	if c.pauses != 2 {
		t.Fatalf("pauses %d, want 2", c.pauses)
	}
}
