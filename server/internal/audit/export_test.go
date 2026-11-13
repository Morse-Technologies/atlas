package audit

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var sampleEvent = Event{ID: 7, At: time.Date(2026, 11, 3, 9, 30, 0, 0, time.UTC), Action: "user.login", Subject: "user:abc",
	Detail: `with "quotes", and a comma`, PrevHash: "p", Hash: "h"}

func written(t *testing.T, format string, events ...Event) string {
	t.Helper()
	var out bytes.Buffer
	w := writers[format](&out)
	for _, e := range events {
		if err := w.write(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.flush(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestCSVHasAHeaderAndQuotesWhatNeedsIt(t *testing.T) {
	actor := uuid.New()
	e := sampleEvent
	e.ActorID = &actor
	rows, err := csv.NewReader(strings.NewReader(written(t, FormatCSV, e, sampleEvent))).ReadAll()
	if err != nil || len(rows) != 3 || rows[0][0] != "id" || rows[1][2] != actor.String() || rows[2][2] != "" || rows[1][5] != sampleEvent.Detail {
		t.Fatalf("rows %q, %v", rows, err)
	}
}

func TestAnEmptyCSVReportStillHasItsHeader(t *testing.T) {
	if got := written(t, FormatCSV); !strings.HasPrefix(got, "id,at,actor_id,action") {
		t.Fatalf("empty report = %q; want the header only", got)
	}
}

func TestJSONLHasOneObjectPerLine(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(written(t, FormatJSONL, sampleEvent, sampleEvent)), "\n")
	var first map[string]any
	if len(lines) != 2 || json.Unmarshal([]byte(lines[0]), &first) != nil || first["action"] != "user.login" || first["actor_id"] != nil {
		t.Fatalf("lines %q", lines)
	}
}

func TestTheRangeIncludesBothEndDays(t *testing.T) {
	from, to := time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
	p := betweenParams(Range{From: from, To: to}, 0)
	if !p.FromAt.Equal(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)) || !p.UntilAt.Equal(time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("range [%v, %v); want the whole 3rd of November", p.FromAt, p.UntilAt)
	}
}

func TestAnUnknownFormatOrABackwardsRangeIsRefused(t *testing.T) {
	svc := &Service{}
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, req := range []ExportRequest{
		{Format: "xml", Range: Range{From: day, To: day}},
		{Format: FormatCSV, Range: Range{From: day, To: day.Add(-time.Hour * 24)}},
	} {
		if _, err := svc.Export(t.Context(), req, &bytes.Buffer{}); err == nil {
			t.Errorf("Export(%+v) accepted a bad request", req)
		}
	}
}
