package overview

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/platform/db"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents"
	"github.com/morse/atlas/server/internal/search"
)

var today = time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)

type fakes struct {
	activity   search.Activity
	counts     documents.Counts
	quarantine int
	alerts     map[string]int
}

func (f fakes) ActivitySince(context.Context, uuid.UUID, search.Window) (search.Activity, error) {
	return f.activity, nil
}
func (f fakes) Counts(context.Context, db.Scope) (documents.Counts, error) { return f.counts, nil }
func (f fakes) QuarantineCount(context.Context, uuid.UUID) (int, error)    { return f.quarantine, nil }
func (f fakes) OpenAlerts(context.Context, db.Scope) (map[string]int, error) {
	return f.alerts, nil
}
func (f fakes) Snapshot(context.Context, uuid.UUID) (catalog.Snapshot, error) {
	return catalog.Snapshot{DocTypes: []catalog.DocType{{Key: "procedure", Band: "procedure"}, {Key: "standard", Band: "norm"}}}, nil
}

func panel(t *testing.T, f fakes) Panel {
	t.Helper()
	s := &Service{Questions: f, Library: f, Quarantine: f, Alerts: f, Taxonomy: f, Now: func() time.Time { return today.Add(15 * time.Hour) }}
	p, err := s.Panel(context.Background(), db.Scope{Tenant: uuid.New(), User: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheWeekHasFourteenDaysEndingTodayWithZerosForQuietDays(t *testing.T) {
	f := fakes{activity: search.Activity{Days: []search.DayStats{{Day: today, Questions: 5, NotFound: 1, Answered: 4}, {Day: today.AddDate(0, 0, -3), Questions: 2}}}}
	p := panel(t, f)
	if len(p.Week) != weekDays || !p.Week[weekDays-1].Day.Equal(today) || p.Week[weekDays-1].Questions != 5 || p.Week[weekDays-4].Questions != 2 || p.Week[0].Questions != 0 {
		t.Fatalf("week = %+v", p.Week)
	}
	if p.Today.Questions != 5 || p.Today.NotFound != 1 || p.Today.Answered != 4 {
		t.Fatalf("today = %+v", p.Today)
	}
}

func TestDocumentsAreCountedByBand(t *testing.T) {
	f := fakes{counts: documents.Counts{ByType: map[string]int{"procedure": 3, "standard": 2, "unheard-of": 1}, Processing: 4}, quarantine: 2}
	d := panel(t, f).Documents
	if d.Indexed != 6 || d.ByBand["procedure"] != 3 || d.ByBand["norm"] != 2 || d.ByBand["manual"] != 1 || d.Processing != 4 || d.Quarantined != 2 {
		t.Fatalf("documents = %+v", d)
	}
}

func TestTheFindingPutsHighAlertsFirstThenQuarantineThenUnansweredQuestions(t *testing.T) {
	busy := search.Activity{Days: []search.DayStats{{Day: today, Questions: 20, NotFound: 10, Answered: 10}}}
	cases := []struct {
		name string
		f    fakes
		want string
	}{
		{"nothing", fakes{}, ""},
		{"high alert", fakes{alerts: map[string]int{"high": 2}, quarantine: 1, activity: busy}, "2 high-priority alert(s) need attention."},
		{"quarantine", fakes{quarantine: 3, activity: busy}, "3 file(s) could not be read and are in quarantine."},
		{"unanswered", fakes{activity: busy}, "50% of today's questions found no answer. The documents may be missing something people ask about."},
		{"too few to judge", fakes{activity: search.Activity{Days: []search.DayStats{{Day: today, Questions: 3, NotFound: 3}}}}, ""},
	}
	for _, c := range cases {
		if got := panel(t, c.f).Finding; got != c.want {
			t.Errorf("%s: finding %q, want %q", c.name, got, c.want)
		}
	}
}
