// Package graph owns what documents mention and how they relate, and the alerts a curator works through
// (ATLAS.md §12). Core builds layers 1 and 2: links found by patterns and by equal codes.
package graph

import (
	"sort"
	"strings"

	"github.com/morse/atlas/server/internal/platform/codes"
)

// Entity types the extractors produce. "code" is not shown as an entity: it only links documents.
const (
	EntityStandard = "standard"
	EntityCode     = "code"
)

// Mention is something a document names, and how many times.
type Mention struct {
	Type  string
	Label string
	Count int
}

// Extract returns the standards and document codes named in texts, in a stable order.
func Extract(texts []string) []Mention {
	counts := map[Mention]int{}
	for _, text := range texts {
		countIn(text, counts)
	}
	out := make([]Mention, 0, len(counts))
	for m, n := range counts {
		m.Count = n
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// countIn adds the standards and codes of one text to counts.
func countIn(text string, counts map[Mention]int) {
	for _, m := range codes.Standards(text) {
		counts[Mention{Type: EntityStandard, Label: standardLabel(m)}]++
	}
	for _, code := range codes.Documents(text) {
		counts[Mention{Type: EntityCode, Label: code}]++
	}
}

func standardLabel(match []string) string {
	label := "ISO " + match[1]
	if match[2] != "" {
		label += ":" + match[2]
	}
	return strings.TrimSpace(label)
}
