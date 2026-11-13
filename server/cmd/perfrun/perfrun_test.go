package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The limits of the server (server/internal/httpapi/ratelimit.go) that the load test has to stay under.
const (
	serverAsksPerMinute    = 20
	serverSignInsPerMinute = 10
)

func TestAskPauseKeepsOnePersonUnderTheServerLimit(t *testing.T) {
	if perMinute := int(time.Minute / askPause); perMinute >= serverAsksPerMinute {
		t.Fatalf("a person asks %d times a minute, the server allows %d", perMinute, serverAsksPerMinute)
	}
}

func TestSignInGapKeepsSetupUnderTheSignInLimit(t *testing.T) {
	if perMinute := int(time.Minute / signInGap); perMinute >= serverSignInsPerMinute {
		t.Fatalf("setup signs in %d times a minute, the server allows %d", perMinute, serverSignInsPerMinute)
	}
}

func TestRunNeedsOnePersonPerUser(t *testing.T) {
	st := state{URL: "https://localhost", People: []person{{Session: "one"}, {Session: "two"}}}
	if _, err := clientsFor(st, 3); err == nil {
		t.Error("clientsFor with 3 users and 2 people succeeded, want an error")
	}
	clients, err := clientsFor(st, 2)
	if err != nil || len(clients) != 2 {
		t.Errorf("clientsFor with 2 users and 2 people = %d clients, %v; want 2", len(clients), err)
	}
}

func TestAnAnswerOf429CountsAsRateLimited(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	c, err := newClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ask("any question"); !errors.Is(err, errRateLimited) {
		t.Fatalf("ask answered with 429 = %v, want errRateLimited", err)
	}
}

func TestReadersListsTheAdministratorAndEveryPerson(t *testing.T) {
	st := state{UserID: "admin", People: []person{{UserID: "one"}, {UserID: "two"}}}
	if got := st.readerIDs(); got != "admin,one,two" {
		t.Fatalf("readerIDs = %q, want the administrator and both people", got)
	}
}
