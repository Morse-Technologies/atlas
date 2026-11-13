package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/platform/metrics"
)

func scrapeMetrics(reg *metrics.Registry) string {
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

func TestRequestsAreCountedByOperationAndStatus(t *testing.T) {
	reg := metrics.NewRegistry()
	serve(t, &Server{Metrics: reg}, "/healthz")
	serve(t, &Server{Metrics: reg}, "/healthz")
	body := scrapeMetrics(reg)
	for _, want := range []string{
		`atlas_http_requests_total{operation="GetHealthz",status="200"} 2`,
		`atlas_http_request_seconds_count{operation="GetHealthz"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %q", want, body)
		}
	}
}

func TestUnroutedRequestsAreCountedAsUnknown(t *testing.T) {
	reg := metrics.NewRegistry()
	serve(t, &Server{Metrics: reg}, "/no/such/path")
	if body := scrapeMetrics(reg); !strings.Contains(body, `operation="unknown",status="404"`) {
		t.Fatalf("not counted: %q", body)
	}
}
