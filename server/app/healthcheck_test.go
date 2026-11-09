package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
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
