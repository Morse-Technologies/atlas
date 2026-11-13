package search

import (
	"testing"

	"github.com/google/uuid"
)

func citing(label string, page int) []Citation {
	return []Citation{{Label: label, Page: page, DocumentID: uuid.New()}}
}

func answeredWith(citations []Citation, top float64) Trial {
	return Trial{Outcome: OutcomeAnswered, Citations: citations, TopScore: top, Reranked: true}
}

func foundNothing(top float64) Trial {
	return Trial{Outcome: OutcomeNotFound, TopScore: top, Reranked: true}
}

func wantCode(code string, page int) *Expected { return &Expected{Code: code, Page: page} }

type verdictCase struct {
	expected *Expected
	trial    Trial
	want     Verdict
}

var verdictCases = map[string]verdictCase{
	"cites the expected document":        {wantCode("PNT-014", 0), answeredWith(citing("PNT-014", 4), 0.9), VerdictCorrect},
	"cites the expected page":            {wantCode("PNT-014", 4), answeredWith(citing("PNT-014", 4), 0.9), VerdictCorrect},
	"cites another page":                 {wantCode("PNT-014", 5), answeredWith(citing("PNT-014", 4), 0.9), VerdictUnsupported},
	"cites another document":             {wantCode("PNT-014", 0), answeredWith(citing("PNT-099", 4), 0.9), VerdictUnsupported},
	"answers a question with no source":  {nil, answeredWith(citing("PNT-014", 4), 0.9), VerdictUnsupported},
	"finds nothing and there is nothing": {nil, foundNothing(0.1), VerdictNotFound},
	"finds nothing but there was a doc":  {wantCode("PNT-014", 0), foundNothing(0.1), VerdictMissed},
	"refuses an answer":                  {wantCode("PNT-014", 0), Trial{Outcome: OutcomeRefused, Citations: citing("PNT-014", 1)}, VerdictMissed},
}

func TestAVerdictComparesTheTrialWithWhatWasExpected(t *testing.T) {
	for name, tt := range verdictCases {
		t.Run(name, func(t *testing.T) {
			if got := verdictOf(EvalCase{Expected: tt.expected}, tt.trial); got != tt.want {
				t.Fatalf("verdict = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestADocumentResolvedByPathIsComparedById(t *testing.T) {
	id := uuid.New()
	trial := answeredWith([]Citation{{DocumentID: id, Page: 1}}, 0.9)
	if got := verdictOf(EvalCase{Expected: &Expected{DocumentID: id}}, trial); got != VerdictCorrect {
		t.Fatalf("verdict = %s, want correct", got)
	}
}

func TestPrecisionAndUnsupportedAnswersAreSharesOfTheirOwnWhole(t *testing.T) {
	cases := []EvalCase{{Expected: wantCode("A", 0)}, {Expected: wantCode("B", 0)}, {Expected: wantCode("C", 0)}, {}}
	trials := []Trial{answeredWith(citing("A", 1), 0.9), answeredWith(citing("B", 1), 0.9), answeredWith(citing("X", 1), 0.9), foundNothing(0.1)}
	report := Score(cases, trials)
	if report.Precision != 2.0/3 || report.Unsupported != 0.25 {
		t.Fatalf("precision %v, unsupported %v; want 2/3 and 1/4", report.Precision, report.Unsupported)
	}
}

func TestTheBarNeedsPrecisionAndAnAlmostEmptyUnsupportedRate(t *testing.T) {
	tests := map[string]struct {
		report EvalReport
		want   bool
	}{
		"exactly the precision":        {EvalReport{Precision: MinPrecision}, true},
		"just under the precision":     {EvalReport{Precision: MinPrecision - 0.01}, false},
		"unsupported just under limit": {EvalReport{Precision: 1, Unsupported: MaxUnsupported - 0.01}, true},
		"unsupported at the limit":     {EvalReport{Precision: 1, Unsupported: MaxUnsupported}, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tt.report.Passes(); got != tt.want {
				t.Fatalf("Passes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTheProposedThresholdIsTheScoreThatGetsTheMostRight(t *testing.T) {
	cases := []EvalCase{{Expected: wantCode("A", 0)}, {Expected: wantCode("B", 0)}, {}, {}}
	trials := []Trial{answeredWith(citing("A", 1), 0.8), answeredWith(citing("B", 1), 0.5), foundNothing(0.3), foundNothing(0.2)}
	if got := Score(cases, trials).ProposedThreshold; got != 0.5 {
		t.Fatalf("threshold = %v, want 0.5: it answers both real questions and refuses both without source", got)
	}
}

func TestOnATieTheHighestThresholdWins(t *testing.T) {
	cases := []EvalCase{{Expected: wantCode("A", 0)}, {Expected: wantCode("A", 0)}}
	trials := []Trial{answeredWith(citing("A", 1), 0.8), answeredWith(citing("X", 1), 0.6)}
	// At 0.8 the second question is missed, at 0.6 it is answered wrongly: one right either way.
	if got := Score(cases, trials).ProposedThreshold; got != 0.8 {
		t.Fatalf("threshold = %v, want 0.8", got)
	}
}

func TestWithoutAReRankerThereIsNoThresholdToPropose(t *testing.T) {
	cases := []EvalCase{{Expected: wantCode("A", 0)}}
	trials := []Trial{{Outcome: OutcomeAnswered, Citations: citing("A", 1)}}
	if got := Score(cases, trials).ProposedThreshold; got != 0 {
		t.Fatalf("threshold = %v, want 0", got)
	}
}

func TestAnUnrankedQuestionWithoutCandidatesStaysNotFoundAtAnyThreshold(t *testing.T) {
	if answersAt(Trial{Outcome: OutcomeNotFound}, 0) {
		t.Fatal("a question that found nothing cannot be answered by lowering the threshold")
	}
}
