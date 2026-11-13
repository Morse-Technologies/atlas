package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/platform/metrics"
)

func scrape(t *testing.T, r *metrics.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	return rec.Body.String()
}

func TestCountersAreWrittenInTextFormat(t *testing.T) {
	r := metrics.NewRegistry()
	r.Inc("atlas_http_requests_total", metrics.Labels{"status": "200", "operation": "Ask"})
	r.Inc("atlas_http_requests_total", metrics.Labels{"operation": "Ask", "status": "200"})
	body := scrape(t, r)
	want := "# TYPE atlas_http_requests_total counter\natlas_http_requests_total{operation=\"Ask\",status=\"200\"} 2\n"
	if body != want {
		t.Fatalf("got %q, want %q", body, want)
	}
}

func TestObserveWritesSumAndCount(t *testing.T) {
	r := metrics.NewRegistry()
	r.Observe("atlas_model_request_seconds", metrics.Labels{"model": "embed"}, 1500*time.Millisecond)
	r.Observe("atlas_model_request_seconds", metrics.Labels{"model": "embed"}, 500*time.Millisecond)
	body := scrape(t, r)
	for _, line := range []string{`atlas_model_request_seconds_sum{model="embed"} 2`, `atlas_model_request_seconds_count{model="embed"} 2`} {
		if !strings.Contains(body, line) {
			t.Errorf("missing %q in %q", line, body)
		}
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	r := metrics.NewRegistry()
	r.Inc("atlas_x", metrics.Labels{"v": "a\"b\\c\nd"})
	if body := scrape(t, r); !strings.Contains(body, `atlas_x{v="a\"b\\c\nd"} 1`) {
		t.Fatalf("not escaped: %q", body)
	}
}

func TestGaugesAreReadOnEveryScrape(t *testing.T) {
	r := metrics.NewRegistry()
	value := 1.0
	r.AddGauge(metrics.Gauge{Name: "atlas_jobs", Read: func(context.Context) ([]metrics.Sample, error) {
		return []metrics.Sample{{Labels: metrics.Labels{"queue": "default", "state": "available"}, Value: value}}, nil
	}})
	first := scrape(t, r)
	value = 3
	second := scrape(t, r)
	if !strings.Contains(first, `atlas_jobs{queue="default",state="available"} 1`) || !strings.Contains(second, `atlas_jobs{queue="default",state="available"} 3`) {
		t.Fatalf("gauge not re-read: %q then %q", first, second)
	}
}

func TestFailingGaugeDoesNotHideTheOthers(t *testing.T) {
	r := metrics.NewRegistry()
	r.Inc("atlas_ok", nil)
	r.AddGauge(metrics.Gauge{Name: "atlas_broken", Read: func(context.Context) ([]metrics.Sample, error) {
		return nil, errors.New("database down")
	}})
	body := scrape(t, r)
	if !strings.Contains(body, "atlas_ok 1") || strings.Contains(body, "atlas_broken") {
		t.Fatalf("unexpected body %q", body)
	}
}
