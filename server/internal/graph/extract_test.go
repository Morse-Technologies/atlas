package graph

import "testing"

func labels(ms []Mention, typ string) []string {
	var out []string
	for _, m := range ms {
		if m.Type == typ {
			out = append(out, m.Label)
		}
	}
	return out
}

func TestStandardsAreNormalizedWithTheirYear(t *testing.T) {
	got := labels(Extract([]string{"Según ISO 9001:2015 y iso-14001, ver ISO9001."}), EntityStandard)
	want := []string{"ISO 14001", "ISO 9001", "ISO 9001:2015"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("standards = %v, want %v", got, want)
	}
}

func TestAStandardIsNotAlsoADocumentCode(t *testing.T) {
	ms := Extract([]string{"Cumple ISO-9001 y el procedimiento PNT-014."})
	if codes := labels(ms, EntityCode); len(codes) != 1 || codes[0] != "PNT-014" {
		t.Fatalf("codes = %v; ISO-9001 must not count as a document code", codes)
	}
}

func TestMentionsAreCountedAcrossTexts(t *testing.T) {
	ms := Extract([]string{"PNT-014 y PNT-014", "otra vez PNT-014 y FRM-002"})
	counts := map[string]int{}
	for _, m := range ms {
		counts[m.Label] = m.Count
	}
	if counts["PNT-014"] != 3 || counts["FRM-002"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestOrderIsStableSoReprocessingWritesTheSameRows(t *testing.T) {
	a := Extract([]string{"FRM-002 PNT-014 ISO 9001 PNT-001"})
	b := Extract([]string{"PNT-001 ISO 9001 PNT-014 FRM-002"})
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("order differs at %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestOrdinaryWordsAndShortNumbersAreIgnored(t *testing.T) {
	if ms := Extract([]string{"Step 3 of 12, page A-1b, ISO 9 is not a standard, code X-1"}); len(ms) != 0 {
		t.Fatalf("mentions = %v", ms)
	}
}
