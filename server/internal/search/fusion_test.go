package search

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.MustParse("00000000-0000-0000-0000-00000000000" + string(rune('a'+i)))
	}
	return out
}

func TestAPassageInBothListsBeatsOneInOnlyTheFirst(t *testing.T) {
	p := ids(3)
	got := fuse([]uuid.UUID{p[0], p[1]}, []uuid.UUID{p[2], p[1]}) // p1 is second in both
	if got[0].ID != p[1] {
		t.Fatalf("top = %v, want the passage found by both searches", got[0].ID)
	}
	if got[0].RankLexical != 2 || got[0].RankVector != 2 {
		t.Fatalf("ranks = %d/%d, want 2/2", got[0].RankLexical, got[0].RankVector)
	}
}

func TestFusionScoreIsTheSumOfReciprocalRanks(t *testing.T) {
	p := ids(1)
	got := fuse([]uuid.UUID{p[0]}, []uuid.UUID{p[0]})
	want := 2.0 / float64(rrfK+1)
	if d := got[0].Score - want; d > 1e-12 || d < -1e-12 {
		t.Fatalf("score %v, want %v", got[0].Score, want)
	}
}

func TestFusionWithOneEmptyListKeepsTheOtherOrder(t *testing.T) {
	p := ids(3)
	got := fuse(nil, p)
	for i := range p {
		if got[i].ID != p[i] || got[i].RankLexical != 0 {
			t.Fatalf("position %d = %+v", i, got[i])
		}
	}
}

func TestTiesAreBrokenByIDSoTheOrderIsStable(t *testing.T) {
	p := ids(2)
	a := fuse([]uuid.UUID{p[0]}, []uuid.UUID{p[1]})
	b := fuse([]uuid.UUID{p[0]}, []uuid.UUID{p[1]})
	if a[0].ID != b[0].ID || a[0].ID != p[0] {
		t.Fatal("equal scores must always come out in the same order")
	}
}

func TestLexicalQueryKeepsUsefulTermsAndCodes(t *testing.T) {
	got := lexicalQuery("What is the tightening torque of the spindle in PNT-014?", nil)
	if got != "tightening | torque | spindle | pnt-014" {
		t.Fatalf("query = %q", got)
	}
}

func TestLexicalQueryCannotCarryTsqueryOperators(t *testing.T) {
	got := lexicalQuery("torque' & !(secret) | <-> :* x", nil)
	if strings.ContainsAny(got, "'&!()<>:*") || got != "torque | secret" {
		t.Fatalf("query = %q", got)
	}
}

func TestLexicalQueryIsEmptyWhenOnlyStopwordsRemain(t *testing.T) {
	if got := lexicalQuery("What is the?", nil); got != "" {
		t.Fatalf("query = %q, want empty", got)
	}
}

func TestLexicalQueryHandlesSpanishAndAccents(t *testing.T) {
	if got := lexicalQuery("¿Cuál es el plazo de preaviso del contrato?", nil); got != "plazo | preaviso | contrato" {
		t.Fatalf("query = %q", got)
	}
}

func TestLexicalQueryIsBounded(t *testing.T) {
	words := make([]string, 40)
	for i := range words {
		words[i] = "term" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if n := strings.Count(lexicalQuery(strings.Join(words, " "), nil), "|") + 1; n != maxQueryTerms {
		t.Fatalf("%d terms, want %d", n, maxQueryTerms)
	}
}

func TestCommonWordsAreLeftOutWhenARarerWordIsThere(t *testing.T) {
	common := map[string]bool{"procedimiento": true, "revision": true}
	if got := lexicalQuery("procedimiento torque husillo", common); got != "torque | husillo" {
		t.Fatalf("got %q; the word in most passages only makes every passage a match", got)
	}
}

func TestCommonWordsStayWhenTheyAreAllThereIs(t *testing.T) {
	common := map[string]bool{"procedimiento": true, "revision": true}
	if got := lexicalQuery("procedimiento revision", common); got != "procedimiento | revision" {
		t.Fatalf("got %q; a question of only common words must still search", got)
	}
}
