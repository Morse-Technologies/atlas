// Command fakemodels stands in for the embedding server, the reranker and the language model during local
// development and end-to-end tests. It speaks the same protocols as text-embeddings-inference and an
// OpenAI-compatible chat server, but its answers are simple and deterministic: the same input always gives the
// same output, and passages that share words come out close together. It proves the plumbing, not the quality.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/morse/atlas/server/internal/fakemodel"
)

const (
	minWordLength  = 4 // shorter words ("the", "is", "de") say nothing about meaning
	sentenceEnds   = ".?!"
	fakeModelName  = "fake-llm"
	noAnswerText   = "I cannot answer from the documents provided."
	answerPrefix   = "According to the document: "
	citationSuffix = " [1]."
)

// words splits text into lowercase words of letters and digits.
func words(text string) []string { return fakemodel.Words(text) }

// significant keeps the words long enough to carry meaning, without repeats.
func significant(text string) map[string]bool {
	set := map[string]bool{}
	for _, w := range words(text) {
		if len(w) >= minWordLength {
			set[w] = true
		}
	}
	return set
}

// embed is the shared deterministic embedding.
func embed(text string) []float32 { return fakemodel.Embed(text) }

// relevance is the share of the query's significant words that the text contains, between 0 and 1.
func relevance(query, text string) float64 {
	wanted, have := significant(query), significant(text)
	if len(wanted) == 0 {
		return 0
	}
	hits := 0
	for w := range wanted {
		if have[w] {
			hits++
		}
	}
	return float64(hits) / float64(len(wanted))
}

// inputs reads TEI's "inputs", which is one string or a list of them.
type inputs []string

func (in *inputs) UnmarshalJSON(raw []byte) error {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		*in = inputs{one}
		return nil
	}
	return json.Unmarshal(raw, (*[]string)(in))
}

// badRequest answers 422, as TEI does for a body it cannot read.
func badRequest(w http.ResponseWriter) {
	http.Error(w, "bad request", http.StatusUnprocessableEntity)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func handleEmbed(w http.ResponseWriter, r *http.Request) {
	var req struct{ Inputs inputs }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		badRequest(w)
		return
	}
	out := make([][]float32, len(req.Inputs))
	for i, text := range req.Inputs {
		out[i] = embed(text)
	}
	writeJSON(w, out)
}

func handleRerank(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string   `json:"query"`
		Texts []string `json:"texts"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		badRequest(w)
		return
	}
	type scored struct {
		Index int     `json:"index"`
		Score float64 `json:"score"`
	}
	out := make([]scored, len(req.Texts))
	for i, text := range req.Texts {
		out[i] = scored{Index: i, Score: relevance(req.Query, text)}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	writeJSON(w, out)
}

func handleTokenize(w http.ResponseWriter, r *http.Request) {
	var req struct{ Inputs inputs }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		badRequest(w)
		return
	}
	type token struct {
		ID      int    `json:"id"`
		Text    string `json:"text"`
		Special bool   `json:"special"`
	}
	out := make([][]token, len(req.Inputs))
	for i, text := range req.Inputs {
		for j, w := range words(text) {
			out[i] = append(out[i], token{ID: j, Text: w})
		}
		if out[i] == nil {
			out[i] = []token{}
		}
	}
	writeJSON(w, out)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func handleModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"data": []map[string]string{{"id": fakeModelName}}})
}

// docBlock finds the first numbered passage Atlas put in the prompt: <doc id=".." n="1" ..>header\ntext</doc ..>.
var docBlock = regexp.MustCompile(`(?s)<doc id="[^"]*" n="1"[^>]*>\n(.*?)\n</doc id=`)

// answerFrom writes an answer that quotes the first sentence of the first passage and cites it.
func answerFrom(prompt string) string {
	m := docBlock.FindStringSubmatch(prompt)
	if m == nil {
		return noAnswerText
	}
	_, text, found := strings.Cut(m[1], "\n") // the first line is the passage's header
	if !found {
		text = m[1]
	}
	return answerPrefix + strings.TrimRight(firstSentence(text), sentenceEnds+" ") + citationSuffix
}

func firstSentence(text string) string {
	if i := strings.IndexAny(text, sentenceEnds); i >= 0 {
		return strings.TrimSpace(text[:i+1])
	}
	return strings.TrimSpace(text)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Messages) == 0 {
		badRequest(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for _, piece := range strings.SplitAfter(answerFrom(req.Messages[len(req.Messages)-1].Content), " ") {
		body, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": piece}}}})
		fmt.Fprintf(w, "data: %s\n\n", body)
		if flusher != nil {
			flusher.Flush()
		}
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// routes serves the three protocols on one port, so one container can stand in for all the model services.
func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /embed", handleEmbed)
	mux.HandleFunc("POST /rerank", handleRerank)
	mux.HandleFunc("POST /tokenize", handleTokenize)
	mux.HandleFunc("GET /v1/models", handleModels)
	mux.HandleFunc("POST /v1/chat/completions", handleChat)
	return mux
}
