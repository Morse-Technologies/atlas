package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func serveHealth(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return ":" + port
}

func TestHealthcheckPassesWhenHealthzAnswers200(t *testing.T) {
	t.Setenv("ATLAS_HTTP_ADDR", serveHealth(t, http.StatusOK))
	if err := runHealthcheck(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestHealthcheckFailsOnAnyOtherStatusOrWhenNothingListens(t *testing.T) {
	t.Setenv("ATLAS_HTTP_ADDR", serveHealth(t, http.StatusServiceUnavailable))
	if runHealthcheck(t.Context(), nil) == nil {
		t.Fatal("a 503 must fail the check")
	}
	t.Setenv("ATLAS_HTTP_ADDR", ":1")
	if runHealthcheck(t.Context(), nil) == nil {
		t.Fatal("a closed port must fail the check")
	}
}

func TestHealthcheckRejectsAnUnknownRole(t *testing.T) {
	if err := runHealthcheck(t.Context(), []string{"--role", "cook"}); err == nil {
		t.Fatal("an unknown role must be an error")
	}
}

func TestHealthcheckOfWatchAndWorkerFailsWhenTheDatabaseIsDown(t *testing.T) {
	t.Setenv("ATLAS_DATABASE_URL", "postgres://atlas_app:x@127.0.0.1:1/atlas?sslmode=disable")
	for _, role := range []string{"watch", "worker"} {
		if runHealthcheck(t.Context(), []string{"--role", role}) == nil {
			t.Fatalf("role %s must fail when Postgres is unreachable", role)
		}
	}
}

func TestHealthcheckHeartbeatIsFreshJustBeforeTheLimitAndStaleAfterIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	written := info.ModTime()
	if err := checkHeartbeat(path, written.Add(heartbeatMaxAge)); err != nil {
		t.Fatalf("a heartbeat at the limit is still alive: %v", err)
	}
	if checkHeartbeat(path, written.Add(heartbeatMaxAge+time.Second)) == nil {
		t.Fatal("a heartbeat older than the limit must fail")
	}
}

func TestHealthcheckHeartbeatFailsWhenTheWorkerNeverBeat(t *testing.T) {
	if checkHeartbeat(filepath.Join(t.TempDir(), "missing"), time.Now()) == nil {
		t.Fatal("a missing heartbeat file must fail")
	}
}

func TestHealthcheckHeartbeatIsWrittenAtOnceAndStopsWithTheContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { beatWhileRunning(ctx, make(chan struct{}), path); close(done) }()
	waitFor(t, func() bool { _, err := os.Stat(path); return err == nil })
	cancel()
	<-done
}

func TestHealthcheckHeartbeatStopsWhenTheJobClientStops(t *testing.T) {
	stopped := make(chan struct{})
	done := make(chan struct{})
	go func() { beatWhileRunning(t.Context(), stopped, filepath.Join(t.TempDir(), "hb")); close(done) }()
	close(stopped)
	<-done
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(healthTimeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
