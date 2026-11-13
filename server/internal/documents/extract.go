package documents

import (
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/morse/atlas/server/internal/platform/codes"
)

// Where an extracted value was read, as the provenance sources of the contract name them.
const (
	sourceTitleBlock = "titleblock" // the document's own content
	sourcePath       = "path"       // the file's name
)

// Confidence of each rule. It does not grow with evidence: a rule that fires gets its fixed value, and a person's
// correction (confidence 1) always wins.
const (
	confidenceTitle        = 0.7
	confidenceLabelledCode = 0.9
	confidenceNameCode     = 0.6
	confidenceLanguage     = 0.6
)

// Language detection: the language whose common words appear at least twice as often wins, if there are enough of them.
const (
	minLanguageWords   = 5
	languageWinnerRate = 2
)

var (
	// revisionPattern reads "Rev. 3", "revisión: 2", "edición 4" and the "rev3" of a file name.
	revisionPattern = regexp.MustCompile(`(?i)(?:^|[^\pL])(?:rev(?:isi[oó]n)?|edici[oó]n)\.?[ :_-]*(\d{1,3})\b`)
	// labelledCodePattern reads a code that the title block names as such: "Código: PNT-014", "Ref. PNT-014".
	labelledCodePattern = regexp.MustCompile(`(?i:c[oó]digo|code|documento|document|referencia|reference|ref)\.?\s*:?\s*(` + codes.DocumentPattern + `)\b`)
	// sectionNumber starts a heading that is a numbered section ("1 Objeto", "4.2 Montaje"), not the title.
	sectionNumber = regexp.MustCompile(`^\d+(\.\d+)*\.?\s`)
)

// reading is a value a rule read, where it read it, and how far to trust it. An empty value means nothing was read.
type reading struct {
	value      string
	source     string
	confidence float32
}

// extracted is everything the rules read from one version.
type extracted struct {
	title, code, language, revision reading
}

// extract applies the rules to a parsed document and to the name of its file.
func extract(parsed Parsed, file string) extracted {
	code, revision := codeAndRevision(parsed.Blocks, file)
	return extracted{
		title:    titleReading(parsed.Blocks),
		code:     code,
		revision: revision,
		language: languageReading(parsed.Blocks),
	}
}

func titleReading(blocks []Block) reading {
	return reading{value: titleOf(blocks), source: sourceTitleBlock, confidence: confidenceTitle}
}

func languageReading(blocks []Block) reading {
	return reading{value: languageOf(textOf(blocks)), source: sourceTitleBlock, confidence: confidenceLanguage}
}

// titleOf is the text of the first heading that is not a numbered section, or "" if there is none.
func titleOf(blocks []Block) string {
	for _, b := range blocks {
		text := strings.TrimSpace(b.Text)
		if b.Kind == KindHeading && text != "" && !sectionNumber.MatchString(text) {
			return text
		}
	}
	return ""
}

// codeAndRevision reads the document's code and revision from the title block (the first page) and, failing that,
// from the file name. A code in the body is not taken: it names another document.
// shortcut: takes the first revision of the first page, so a revision table on page one yields its oldest row.
func codeAndRevision(blocks []Block, file string) (code, revision reading) {
	front, name := textOfFirstPage(blocks), path.Base(file)
	code = firstFound(labelledCode(front), nameCode(name))
	revision = firstFound(revisionIn(front, sourceTitleBlock), revisionIn(name, sourcePath))
	return code, revision
}

func firstFound(candidates ...reading) reading {
	for _, c := range candidates {
		if c.value != "" {
			return c
		}
	}
	return reading{}
}

func labelledCode(text string) reading {
	m := labelledCodePattern.FindStringSubmatch(text)
	if m == nil {
		return reading{}
	}
	return reading{value: m[1], source: sourceTitleBlock, confidence: confidenceLabelledCode}
}

func nameCode(name string) reading {
	found := codes.Documents(name)
	if len(found) == 0 {
		return reading{}
	}
	return reading{value: found[0], source: sourcePath, confidence: confidenceNameCode}
}

func revisionIn(text, source string) reading {
	m := revisionPattern.FindStringSubmatch(text)
	if m == nil {
		return reading{}
	}
	return reading{value: m[1], source: source}
}

func textOfFirstPage(blocks []Block) string {
	var first []Block
	for _, b := range blocks {
		if b.Page == 1 {
			first = append(first, b)
		}
	}
	return textOf(first)
}

func textOf(blocks []Block) string {
	texts := make([]string, len(blocks))
	for i, b := range blocks {
		texts[i] = b.Text
	}
	return strings.Join(texts, "\n")
}

// commonWords are words that are frequent in one language and are not words of the other.
var commonWords = map[string]map[string]bool{
	"es": wordSet("de", "la", "el", "en", "y", "que", "los", "las", "del", "con", "para", "por", "una", "se", "al"),
	"en": wordSet("the", "of", "and", "to", "in", "is", "for", "with", "that", "are", "on", "be", "this", "as", "by"),
}

func wordSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

// languageOf is "es" or "en" by how often each language's common words appear, or "" when the text is too short or
// too mixed to tell.
func languageOf(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	spanish, english := countCommon(words, commonWords["es"]), countCommon(words, commonWords["en"])
	switch {
	case spanish >= minLanguageWords && spanish >= languageWinnerRate*english:
		return "es"
	case english >= minLanguageWords && english >= languageWinnerRate*spanish:
		return "en"
	default:
		return ""
	}
}

func countCommon(words []string, common map[string]bool) int {
	count := 0
	for _, w := range words {
		if common[w] {
			count++
		}
	}
	return count
}
