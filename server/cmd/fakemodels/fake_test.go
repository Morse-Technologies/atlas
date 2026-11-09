package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/morse/atlas/server/internal/fakemodel"
)

func post(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func cosine(a, b []float32) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

func TestEmbeddingsAreUnitVectorsOfTheSizeTheIndexExpects(t *testing.T) {
	v := embed("tightening torque of the spindle")
	if len(v) != fakemodel.Dimension || math.Abs(cosine(v, v)-1) > 1e-5 {
		t.Fatalf("length %d, norm %v", len(v), cosine(v, v))
	}
}

func TestTextsThatShareWordsAreCloserThanTextsThatDoNot(t *testing.T) {
	q := embed("tightening torque spindle")
	near, far := embed("the tightening torque of the spindle is forty"), embed("holiday calendar for the office")
	if cosine(q, near) <= cosine(q, far) {
		t.Fatalf("near %v far %v", cosine(q, near), cosine(q, far))
	}
}

func TestEmbeddingIsDeterministic(t *testing.T) {
	a, b := embed("same words"), embed("same words")
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("the same text must always give the same vector")
		}
	}
}

func TestEmbedAcceptsOneStringOrAList(t *testing.T) {
	for _, body := range []string{`{"inputs":"one"}`, `{"inputs":["one","two"]}`} {
		var out [][]float32
		if err := json.Unmarshal(post(t, "/embed", body).Body.Bytes(), &out); err != nil || len(out) == 0 {
			t.Fatalf("%s: %v %d", body, err, len(out))
		}
	}
}

func TestRerankScoresByShareOfQueryWordsAndSortsBestFirst(t *testing.T) {
	rec := post(t, "/rerank", `{"query":"tightening torque spindle","texts":["calendar of holidays","the tightening torque of the spindle"]}`)
	var out []struct {
		Index int
		Score float64
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out[0].Index != 1 || out[0].Score != 1 || out[1].Score != 0 {
		t.Fatalf("out %+v, err %v", out, err)
	}
}

func TestTokenizeCountsWords(t *testing.T) {
	var out [][]map[string]any
	_ = json.Unmarshal(post(t, "/tokenize", `{"inputs":["a b c",""]}`).Body.Bytes(), &out)
	if len(out[0]) != 3 || len(out[1]) != 0 {
		t.Fatalf("out %v", out)
	}
}

const promptWithPassage = `<doc id="x1" n="1" source="PNT-014" page="3">
PNT-014 > Torque
Tighten the spindle to 40 Nm. Then check it twice.
</doc id="x1">

Question: torque?`

func TestTheModelQuotesAndCitesTheFirstPassage(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": promptWithPassage}}})
	out := post(t, "/v1/chat/completions", string(body)).Body.String()
	var text strings.Builder
	for _, line := range strings.Split(out, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct{ Delta struct{ Content string } }
		}
		_ = json.Unmarshal([]byte(data), &chunk)
		text.WriteString(chunk.Choices[0].Delta.Content)
	}
	if text.String() != "According to the document: Tighten the spindle to 40 Nm [1]." || !strings.HasSuffix(out, "data: [DONE]\n\n") {
		t.Fatalf("text %q, stream %q", text.String(), out)
	}
}

func TestWithoutPassagesTheModelSaysItCannotAnswerAndCitesNothing(t *testing.T) {
	if got := answerFrom("Question: anything?"); got != noAnswerText || strings.Contains(got, "[1]") {
		t.Fatalf("got %q", got)
	}
}

func TestModelsAreListed(t *testing.T) {
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if !strings.Contains(rec.Body.String(), fakeModelName) {
		t.Fatalf("body %s", rec.Body)
	}
}
