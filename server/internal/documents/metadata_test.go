package documents

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/catalog"
	"github.com/morse/atlas/server/internal/documents/documentsdb"
	"github.com/morse/atlas/server/internal/platform/errs"
)

var taxonomy = catalog.Snapshot{DocTypes: []catalog.DocType{
	{Key: "procedure", Band: catalog.BandProcedure}, {Key: "standard", Band: catalog.BandNorm}, {Key: "manual", Band: catalog.BandManual},
}}

func current() documentsdb.DocumentsDocument {
	return documentsdb.DocumentsDocument{ID: uuid.New(), Title: "PNT-014", Code: "PNT-014", DocType: "procedure",
		Status: "current", Attributes: []byte(`{"line":"2"}`)}
}

func ptr[T any](v T) *T { return &v }

func TestOnlyChangedFieldsAreReported(t *testing.T) {
	p := MetadataPatch{Title: ptr("PNT-014"), Code: ptr("PNT-015"), Status: ptr("obsolete")}
	_, changed, err := applyPatch(current(), p, taxonomy)
	if err != nil || len(changed) != 2 || changed[0].field != "code" || changed[1].field != "status" {
		t.Fatalf("changed = %+v, %v; want code and status (title is the same)", changed, err)
	}
}

func TestUntouchedFieldsKeepTheirValue(t *testing.T) {
	out, _, err := applyPatch(current(), MetadataPatch{Department: ptr("Quality")}, taxonomy)
	if err != nil || out.Title != "PNT-014" || out.Code != "PNT-014" || out.Department != "Quality" {
		t.Fatalf("out = %+v, %v", out, err)
	}
}

func TestInvalidPatchesAreRefused(t *testing.T) {
	bad := map[string]MetadataPatch{
		"empty title":      {Title: ptr("  ")},
		"unknown status":   {Status: ptr("final")},
		"unknown doc type": {DocType: ptr("blueprint")},
	}
	for name, p := range bad {
		if _, _, err := applyPatch(current(), p, taxonomy); !errors.Is(err, errs.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestAttributesReplaceTheSetAndReportChangedKeys(t *testing.T) {
	p := MetadataPatch{Attributes: map[string]string{"line": "2", "shift": "night"}}
	out, changed, err := applyPatch(current(), p, taxonomy)
	if err != nil || len(changed) != 1 || changed[0].field != "attributes.shift" || string(out.Attributes) != `{"line":"2","shift":"night"}` {
		t.Fatalf("out %s, changed %+v, %v", out.Attributes, changed, err)
	}
}

func TestDatesAreComparedByInstant(t *testing.T) {
	day := time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC)
	cur := current()
	cur.ApprovedAt = &day
	_, changed, _ := applyPatch(cur, MetadataPatch{ApprovedAt: ptr(day)}, taxonomy)
	if len(changed) != 0 {
		t.Fatalf("same date reported as changed: %+v", changed)
	}
}

func TestLikeWildcardsTypedByAPersonAreEscaped(t *testing.T) {
	if got := likeEscape(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("likeEscape = %q", got)
	}
}

func TestBandFilterSelectsOnlyItsTypes(t *testing.T) {
	p := listParams(Listing{Band: catalog.BandNorm, Status: "all", Size: 10}, taxonomy)
	if !p.FilterTypes || len(p.DocTypes) != 1 || p.DocTypes[0] != "standard" || p.Status != "" || p.PageSize != 11 {
		t.Fatalf("params = %+v", p)
	}
	if all := listParams(Listing{Band: "all"}, taxonomy); all.FilterTypes {
		t.Fatal(`band "all" must not filter`)
	}
}
