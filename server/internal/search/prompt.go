// Package search owns passage retrieval, the answers built from them and the conversations that hold them
// (ATLAS.md §11). It reads passages only through the views documents publishes.
package search

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/morse/atlas/server/internal/platform/models/llm"
)

// PromptVersion names the system prompt stored with every answer, so a past answer can be reproduced.
const PromptVersion = "answer.v1"

//go:embed prompts/answer.v1.md
var systemPrompt string

const delimiterBytes = 8

// Passage is a piece of a document, as search finds it and the answer cites it.
type Passage struct {
	ChunkID    uuid.UUID
	VersionID  uuid.UUID
	DocumentID uuid.UUID
	Page       int
	Section    string
	Header     string
	Text       string
	DocTitle   string
	DocCode    string
	Box        []byte // JSON {x,y,w,h} in PDF points
	Suspect    bool
}

// Label is how a document is named in a citation: its code, or its title.
func (p Passage) Label() string {
	if p.DocCode != "" {
		return p.DocCode
	}
	return p.DocTitle
}

// roleMarkers are the control sequences chat models read as turn boundaries (ChatML, Llama, Gemma).
var roleMarkers = regexp.MustCompile(`<\|[^|>]{1,30}\|>|\[/?INST\]|<<?/?SYS>>?|</?(start|end)_of_turn>`)

// sanitize removes what could close the data block or pose as a turn of the conversation.
func sanitize(text, delimiter string) string {
	text = roleMarkers.ReplaceAllString(text, "")
	return strings.ReplaceAll(text, delimiter, "")
}

// newDelimiter returns a random token for this request, so a document cannot guess the tag that closes its block.
func newDelimiter() (string, error) {
	raw := make([]byte, delimiterBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("new delimiter: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// promptInput is everything that goes into one prompt.
type promptInput struct {
	question  string
	passages  []Passage
	delimiter string
	language  string // access.Settings.AnswerLanguage
}

// languageRules adds a line to the system prompt when the organization fixes the answer language.
var languageRules = map[string]string{"es": "\nAlways answer in Spanish.", "en": "\nAlways answer in English."}

// buildPrompt puts the numbered passages in a data block and the question after it.
func buildPrompt(in promptInput) []llm.Message {
	question, passages, delimiter := in.question, in.passages, in.delimiter
	var b strings.Builder
	for i, p := range passages {
		fmt.Fprintf(&b, "<doc id=\"%s\" n=\"%d\" source=\"%s\" page=\"%d\">\n%s\n%s\n</doc id=\"%s\">\n",
			delimiter, i+1, sanitize(p.Label(), delimiter), p.Page, sanitize(p.Header, delimiter), sanitize(p.Text, delimiter), delimiter)
	}
	return []llm.Message{
		{Role: "system", Content: systemPrompt + languageRules[in.language]},
		{Role: "user", Content: b.String() + "\nQuestion: " + question},
	}
}

// contextHash fingerprints the exact prompt sent, so an answer can be shown to have used the passages on record.
func contextHash(messages []llm.Message) string {
	raw, _ := json.Marshal(messages) // plain strings: cannot fail
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

var citationMark = regexp.MustCompile(`\[(\d+)\]`)

// checked is an answer text after its citations were validated.
type checked struct {
	Text    string
	Used    []int // distinct passage numbers cited, in order of first use
	Uncited int   // sentences that state something with no citation
}

// checkCitations drops markers that name a passage that was not sent, and counts what remains uncited.
func checkCitations(text string, sent int) checked {
	out := checked{}
	seen := map[int]bool{}
	out.Text = citationMark.ReplaceAllStringFunc(text, func(mark string) string {
		n, _ := strconv.Atoi(citationMark.FindStringSubmatch(mark)[1])
		if n < 1 || n > sent {
			return ""
		}
		if !seen[n] {
			seen[n] = true
			out.Used = append(out.Used, n)
		}
		return mark
	})
	out.Uncited = uncitedSentences(out.Text)
	return out
}

var sentenceBreak = regexp.MustCompile(`[.!?]\s+|\n+`)

func uncitedSentences(text string) int {
	count := 0
	for _, s := range sentenceBreak.Split(text, -1) {
		if len(strings.Fields(s)) >= minWordsForClaim && !citationMark.MatchString(s) {
			count++
		}
	}
	return count
}

// minWordsForClaim: shorter fragments ("Yes.", "See above.") are not claims that need a source.
const minWordsForClaim = 4
