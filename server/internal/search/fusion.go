package search

import (
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

// rrfK is the constant of reciprocal rank fusion (ATLAS.md §9.5).
const rrfK = 60

// Limits of one search.
const (
	rankedPerList = 50 // passages taken from each of the word and vector searches
	maxQueryTerms = 12
	minTermLength = 2
)

// fused is a passage with its position in each list and its fused score.
type fused struct {
	ID          uuid.UUID
	RankLexical int // 1-based; 0: not in that list
	RankVector  int
	Score       float64
}

// topFusedScore is the score of a passage that ranks first in both lists: the top of the fused scale.
const topFusedScore = 2.0 / (rrfK + 1)

// fuse merges two rankings by position: score(d) = sum 1/(k + rank). Ties go to the smaller id, so the order is stable.
func fuse(lexical, vector []uuid.UUID) []fused {
	byID := map[uuid.UUID]*fused{}
	accumulate(byID, lexical, func(f *fused, rank int) { f.RankLexical = rank })
	accumulate(byID, vector, func(f *fused, rank int) { f.RankVector = rank })
	out := make([]fused, 0, len(byID))
	for _, f := range byID {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out
}

func accumulate(byID map[uuid.UUID]*fused, ids []uuid.UUID, setRank func(*fused, int)) {
	for i, id := range ids {
		f := byID[id]
		if f == nil {
			f = &fused{ID: id}
			byID[id] = f
		}
		setRank(f, i+1)
		f.Score += 1 / float64(rrfK+i+1)
	}
}

// stopwords are dropped from the word query: they match nearly every passage and drown the useful terms.
var stopwords = toSet("the a an of to in on for and or is are was be what which who how when where does do with by at as from that this it " +
	"el la los las un una unos unas de del al en y o es son fue ser que qué cuál cuáles quién cómo cuándo dónde con por para se su sus lo")

func toSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}

// lexicalQuery turns a question into a to_tsquery expression: its useful words joined by OR, without the ones
// that nearly every passage contains (common). Only letters,
// digits and hyphens survive, so a question cannot inject tsquery operators. It is empty when nothing is left.
func lexicalQuery(question string, common map[string]bool) string {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-'
	})
	seen := map[string]bool{}
	var terms []string
	for _, w := range words {
		w = strings.Trim(w, "-")
		if len(w) < minTermLength || stopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		terms = append(terms, w)
		if len(terms) == maxQueryTerms {
			break
		}
	}
	return strings.Join(dropCommon(terms, common), " | ")
}

// dropCommon leaves out the words found in many passages, unless nothing else is left to search for.
func dropCommon(terms []string, common map[string]bool) []string {
	var rare []string
	for _, t := range terms {
		if !common[t] {
			rare = append(rare, t)
		}
	}
	if len(rare) == 0 {
		return terms
	}
	return rare
}
