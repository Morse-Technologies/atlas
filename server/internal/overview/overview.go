// Package overview composes the panel: it asks search, documents, sources and graph for their numbers and
// puts them in one answer. It owns no tables (ATLAS.md §5.1).
package overview

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/search"
)

// weekDays is how many days the activity chart shows, ending today.
const weekDays = 14

// day is the length of a chart bucket. Days are UTC.
const day = 24 * time.Hour

// Today is the last day's traffic.
type Today struct {
	Questions       int
	Answered        int
	NotFound        int
	MedianLatencyMs int
}

// Day is the traffic of one day.
type Day struct {
	Day       time.Time
	Questions int
	NotFound  int
}

// Documents is what the index holds.
type Documents struct {
	Indexed     int
	ByBand      map[string]int
	Processing  int
	Quarantined int
}

// Panel is everything the dashboard shows.
type Panel struct {
	Today      Today
	Week       []Day
	Documents  Documents
	OpenAlerts map[string]int
	Finding    string
}

// The sources of the numbers. Each is one module's facade.
type (
	// Questions gives the question traffic.
	Questions interface {
		ActivitySince(ctx context.Context, tenant uuid.UUID, w search.Window) (search.Activity, error)
	}
	// Library gives the document counts.
	Library interface {
		Counts(ctx context.Context, scope db.Scope) (documents.Counts, error)
	}
	// Quarantine gives the files waiting in quarantine.
	Quarantine interface {
		QuarantineCount(ctx context.Context, tenant uuid.UUID) (int, error)
	}
	// Alerts gives the open alerts by severity.
	Alerts interface {
		OpenAlerts(ctx context.Context, scope db.Scope) (map[string]int, error)
	}
	// Taxonomy gives the bands of the document types.
	Taxonomy interface {
		Snapshot(ctx context.Context, tenant uuid.UUID) (catalog.Snapshot, error)
	}
)

// Service builds the panel.
type Service struct {
	Questions  Questions
	Library    Library
	Quarantine Quarantine
	Alerts     Alerts
	Taxonomy   Taxonomy
	Now        func() time.Time
}

// Panel reads the numbers of one tenant. Alerts are optional: without the feature the counts are zero.
func (s *Service) Panel(ctx context.Context, scope db.Scope) (Panel, error) {
	tenant := scope.Tenant
	now := s.Now().UTC()
	today := now.Truncate(day)
	since := today.AddDate(0, 0, -(weekDays - 1))
	activity, err := s.Questions.ActivitySince(ctx, tenant, search.Window{Since: since, MedianSince: today})
	if err != nil {
		return Panel{}, err
	}
	docs, err := s.documents(ctx, scope)
	if err != nil {
		return Panel{}, err
	}
	alerts, err := s.Alerts.OpenAlerts(ctx, scope)
	if err != nil {
		return Panel{}, err
	}
	p := Panel{Today: todayOf(activity, today), Week: fillDays(activity.Days, today), Documents: docs, OpenAlerts: alerts}
	p.Finding = finding(p)
	return p, nil
}

func (s *Service) documents(ctx context.Context, scope db.Scope) (Documents, error) {
	tenant := scope.Tenant
	counts, err := s.Library.Counts(ctx, scope)
	if err != nil {
		return Documents{}, err
	}
	tax, err := s.Taxonomy.Snapshot(ctx, tenant)
	if err != nil {
		return Documents{}, err
	}
	quarantined, err := s.Quarantine.QuarantineCount(ctx, tenant)
	if err != nil {
		return Documents{}, fmt.Errorf("quarantine count: %w", err)
	}
	out := Documents{ByBand: map[string]int{}, Processing: counts.Processing, Quarantined: quarantined}
	for docType, n := range counts.ByType {
		out.ByBand[tax.BandOf(docType)] += n
		out.Indexed += n
	}
	return out, nil
}

func todayOf(a search.Activity, today time.Time) Today {
	out := Today{MedianLatencyMs: a.MedianLatencyMs}
	for _, d := range a.Days {
		if d.Day.Equal(today) {
			out.Questions, out.Answered, out.NotFound = d.Questions, d.Answered, d.NotFound
		}
	}
	return out
}

// fillDays returns exactly weekDays days ending today, with zeros for the days nobody asked anything.
func fillDays(days []search.DayStats, today time.Time) []Day {
	byDay := make(map[time.Time]search.DayStats, len(days))
	for _, d := range days {
		byDay[d.Day.UTC().Truncate(day)] = d
	}
	out := make([]Day, weekDays)
	for i := range out {
		day := today.AddDate(0, 0, i-(weekDays-1))
		out[i] = Day{Day: day, Questions: byDay[day].Questions, NotFound: byDay[day].NotFound}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day.Before(out[j].Day) })
	return out
}

// Thresholds for the one-line finding.
const (
	minQuestionsForRate = 10 // a rate over fewer questions says nothing
	notFoundWorry       = 0.3
	percent             = 100
)

// finding is one sentence about what deserves attention, or empty when nothing does.
func finding(p Panel) string {
	switch {
	case p.OpenAlerts["high"] > 0:
		return fmt.Sprintf("%d high-priority alert(s) need attention.", p.OpenAlerts["high"])
	case p.Documents.Quarantined > 0:
		return fmt.Sprintf("%d file(s) could not be read and are in quarantine.", p.Documents.Quarantined)
	case p.Today.Questions >= minQuestionsForRate && float64(p.Today.NotFound)/float64(p.Today.Questions) >= notFoundWorry:
		return fmt.Sprintf("%d%% of today's questions found no answer. The documents may be missing something people ask about.",
			p.Today.NotFound*percent/p.Today.Questions)
	}
	return ""
}
