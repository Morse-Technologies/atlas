// Package codes recognizes the identifiers organizations print on their documents: document codes such as PNT-014
// and standards such as ISO 9001:2015. The graph reads them from passages and documents reads them from a file's
// title block and name, so both must agree on what a code looks like.
package codes

import "regexp"

var (
	// standard finds ISO standards with an optional year: ISO 9001, ISO-9001:2015. Submatches: number, year.
	standard = regexp.MustCompile(`\b(?i:iso)[ -]?(\d{3,6})(?::(\d{4}))?\b`)
	// document finds document codes such as PNT-014.
	document = regexp.MustCompile(`\b[A-Z]{2,6}-\d{1,5}\b`)
)

// Standards returns every standard named in text, each as [whole match, number, year]; year may be empty.
func Standards(text string) [][]string {
	return standard.FindAllStringSubmatch(text, -1)
}

// Documents returns every document code named in text, in order of appearance. Standards are masked first,
// or "ISO-9001" would also be taken for a document code.
func Documents(text string) []string {
	return document.FindAllString(standard.ReplaceAllString(text, " "), -1)
}

// DocumentPattern is the expression of a document code, for callers that need it inside a longer one.
const DocumentPattern = `[A-Z]{2,6}-\d{1,5}`
