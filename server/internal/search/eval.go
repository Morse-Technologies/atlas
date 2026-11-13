package search

import (
	"sort"

	"github.com/google/uuid"
)

// The bar an installation must reach before its answers can be trusted (Project.md §4, N4.1).
const (
	MinPrecision   = 0.85
	MaxUnsupported = 0.03 // exclusive: reaching it fails
)

// Expected is where the answer to a question must come from. A document is named by Code or by Path; the caller
// that resolves a Path sets DocumentID. Page is optional (0: any page).
type Expected struct {
	Code       string    `json:"code,omitempty"`
	Path       string    `json:"path,omitempty"`
	Page       int       `json:"page,omitempty"`
	DocumentID uuid.UUID `json:"-"`
}

// EvalCase is one question and its expected source; a nil Expected means the question must come back "not found".
type EvalCase struct {
	Question string    `json:"question"`
	Expected *Expected `json:"expected"`
}

// Verdict says how a trial compares with what was expected.
type Verdict string

// Verdicts.
const (
	VerdictCorrect     Verdict = "correct"           // answered, and the expected document is cited
	VerdictUnsupported Verdict = "unsupported"       // answered without the expected document, or answered a question that has none
	VerdictMissed      Verdict = "missed"            // not answered, but the document exists
	VerdictNotFound    Verdict = "rightly_not_found" // not answered, and there was nothing to find
)

// EvalRow is the verdict on one question.
type EvalRow struct {
	Question string  `json:"question"`
	Verdict  Verdict `json:"verdict"`
	TopScore float64 `json:"top_score"`
}

// EvalReport is what an evaluation found. Precision is the share of questions with an expected document that were
// answered citing it; Unsupported is the share of all questions answered without a source that backs them.
// ProposedThreshold is 0 when no trial was reranked, because then no threshold applies.
type EvalReport struct {
	Precision         float64   `json:"precision"`
	Unsupported       float64   `json:"unsupported"`
	ProposedThreshold float64   `json:"proposed_threshold"`
	Rows              []EvalRow `json:"rows"`
}

// Passes says whether the installation reaches the bar.
func (r EvalReport) Passes() bool {
	return r.Precision >= MinPrecision && r.Unsupported < MaxUnsupported
}

// Score compares each trial with its case (same order) and proposes the not-found threshold that would have
// answered the most questions correctly.
func Score(cases []EvalCase, trials []Trial) EvalReport {
	report := EvalReport{ProposedThreshold: proposeThreshold(cases, trials)}
	var correct, withExpected, unsupported int
	for i, c := range cases {
		verdict := verdictOf(c, trials[i])
		report.Rows = append(report.Rows, EvalRow{Question: c.Question, Verdict: verdict, TopScore: trials[i].TopScore})
		correct += count(verdict == VerdictCorrect)
		withExpected += count(c.Expected != nil)
		unsupported += count(verdict == VerdictUnsupported)
	}
	report.Precision = ratio(correct, withExpected, 1)
	report.Unsupported = ratio(unsupported, len(cases), 0)
	return report
}

func count(condition bool) int {
	if condition {
		return 1
	}
	return 0
}

// ratio is part/whole, or whenEmpty if there is nothing to measure.
func ratio(part, whole int, whenEmpty float64) float64 {
	if whole == 0 {
		return whenEmpty
	}
	return float64(part) / float64(whole)
}

func verdictOf(c EvalCase, t Trial) Verdict {
	return verdictAt(c, t, t.Outcome == OutcomeAnswered)
}

func verdictAt(c EvalCase, t Trial, answered bool) Verdict {
	switch {
	case answered && c.Expected == nil:
		return VerdictUnsupported
	case answered && cites(t.Citations, *c.Expected):
		return VerdictCorrect
	case answered:
		return VerdictUnsupported
	case c.Expected == nil:
		return VerdictNotFound
	default:
		return VerdictMissed
	}
}

// cites says whether one of the citations is the expected document (and page, if one was given).
func cites(citations []Citation, want Expected) bool {
	for _, c := range citations {
		if isDocument(c, want) && (want.Page == 0 || c.Page == want.Page) {
			return true
		}
	}
	return false
}

// isDocument compares by id when the document was resolved, otherwise by code (a citation is labelled with its code).
func isDocument(c Citation, want Expected) bool {
	if want.DocumentID != uuid.Nil {
		return c.DocumentID == want.DocumentID
	}
	return want.Code != "" && c.Label == want.Code
}

// proposeThreshold tries each top reranker score seen as the not-found threshold and keeps the one with the most
// correct results (a right citation, or a right "not found"); on a tie the highest.
func proposeThreshold(cases []EvalCase, trials []Trial) float64 {
	scores := topScores(trials)
	best, bestHits := 0.0, -1
	for _, threshold := range scores {
		if hits := hitsAt(cases, trials, threshold); hits > bestHits {
			best, bestHits = threshold, hits
		}
	}
	return best
}

// topScores are the distinct best scores of the reranked trials, highest first.
func topScores(trials []Trial) []float64 {
	seen := map[float64]bool{}
	var scores []float64
	for _, t := range trials {
		if t.Reranked && !seen[t.TopScore] {
			seen[t.TopScore] = true
			scores = append(scores, t.TopScore)
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(scores)))
	return scores
}

func hitsAt(cases []EvalCase, trials []Trial, threshold float64) int {
	hits := 0
	for i, c := range cases {
		v := verdictAt(c, trials[i], answersAt(trials[i], threshold))
		hits += count(v == VerdictCorrect || v == VerdictNotFound)
	}
	return hits
}

// answersAt says whether the trial would have been answered with this threshold. A refused answer stays refused.
func answersAt(t Trial, threshold float64) bool {
	switch {
	case t.Outcome == OutcomeRefused:
		return false
	case !t.Reranked:
		return t.Outcome == OutcomeAnswered
	default:
		return t.TopScore >= threshold
	}
}
