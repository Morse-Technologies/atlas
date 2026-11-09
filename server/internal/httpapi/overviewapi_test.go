package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/morse/atlas/server/internal/overview"
	"github.com/morse/atlas/server/internal/platform/db"
)

type fixedPanel overview.Panel

func (f fixedPanel) Panel(context.Context, db.Scope) (overview.Panel, error) {
	return overview.Panel(f), nil
}

func TestThePanelKeepsEveryNumberAndFormatsDaysAsDates(t *testing.T) {
	panel := fixedPanel{
		Today:      overview.Today{Questions: 14, Answered: 11, NotFound: 3, MedianLatencyMs: 4200},
		Week:       []overview.Day{{Day: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), Questions: 14, NotFound: 3}},
		Documents:  overview.Documents{Indexed: 120, ByBand: map[string]int{"procedure": 80, "norm": 40}, Processing: 2, Quarantined: 1},
		OpenAlerts: map[string]int{"high": 1, "medium": 4, "low": 9},
		Finding:    "1 high-priority alert(s) need attention.",
	}
	rec := serveTo(t, &Server{Accounts: people, Panel: panel}, adminRequest(http.MethodGet, "/api/v1/stats/overview", ""))
	for _, want := range []string{`"questions":14`, `"median_latency_ms":4200`, `"day":"2026-11-07"`, `"indexed":120`, `"procedure":80`,
		`"quarantined":1`, `"high":1`, `"low":9`, `"top_topics":[]`, `high-priority`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body lacks %s: %s", want, rec.Body)
		}
	}
}
