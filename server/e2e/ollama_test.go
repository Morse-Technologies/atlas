//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	realIngestTimeout = 20 * time.Minute
	trialThreshold    = 0.0005
)

// A small real model is not deterministic, so this journey checks what must hold whatever it writes: the stream
// ends, retrieval finds the right passage by meaning, an answer that is given cites it, and nothing is invented
// for a question outside the documents. Latencies are printed to size the not-found threshold and the timeouts.
// Needs the stack of `deploy/scripts/dev-env.sh ollama` and ATLAS_E2E_REAL_MODELS=1.
func TestSmallRealModelsAnswerFromTheDocuments(t *testing.T) {
	if os.Getenv("ATLAS_E2E_REAL_MODELS") != "1" {
		t.Skip("ATLAS_E2E_REAL_MODELS=1 and a stack from `dev-env.sh ollama` are needed")
	}
	ingestTimeout = realIngestTimeout
	j := &journey{api: newAPI(t)}
	started := time.Now()
	for _, step := range []func(*testing.T){j.setup, j.signIn, j.addSource} {
		step(t)
	}
	// bge-reranker-base (the trial reranker) scores a clearly relevant passage around 0.001 to 0.04 and an
	// unrelated one around 0.00004, so the 0.35 meant for bge-reranker-v2-m3 would reject everything. The
	// threshold belongs to the reranker model: measure it again whenever the model changes.
	expect(t, j.api.patch("/settings", map[string]any{"not_found_threshold": trialThreshold}), http.StatusOK)
	t.Logf("two PDFs indexed with real embeddings in %s", time.Since(started).Round(time.Second))
	t.Run("a question with other words than the document finds it by meaning", j.askByMeaning)
	t.Run("a question outside the documents is not answered", j.askUnrelated)
}

func (j *journey) askByMeaning(t *testing.T) {
	// The procedure says "Apretar la tuerca del husillo a 40 Nm"; none of these words is in the question.
	started := time.Now()
	events, answer := j.api.ask("¿Con qué fuerza hay que ajustar la pieza que sujeta el eje principal de la máquina?")
	t.Logf("answer in %s, outcome %q: %s", time.Since(started).Round(time.Millisecond), answer.Outcome, answer.Text)
	if len(events) == 0 || events[len(events)-1].Type != "done" {
		t.Fatalf("the stream must end with done: %+v", events)
	}
	switch answer.Outcome {
	case "answered":
		c := answer.Citations
		if len(c) == 0 || c[0].DocumentID == "" || c[0].Page != 4 {
			t.Fatalf("the cited passage should be page 4 (Montaje): %+v", c)
		}
		t.Logf("cites page %d, score %.2f; mentions 40 Nm: %v", c[0].Page, c[0].Score, strings.Contains(answer.Text, "40"))
	case "refused":
		t.Log("the small model did not cite, so Atlas withheld its text: expected sometimes, the safe outcome")
	default:
		t.Fatalf("outcome %q: retrieval did not find the passage by meaning (threshold too high or embeddings broken)", answer.Outcome)
	}
}

func (j *journey) askUnrelated(t *testing.T) {
	started := time.Now()
	_, answer := j.api.ask("¿Cuál es la capital de Mongolia y quién ganó el mundial de 1998?")
	t.Logf("in %s, outcome %q, %d citations", time.Since(started).Round(time.Millisecond), answer.Outcome, len(answer.Citations))
	if answer.Outcome == "answered" {
		t.Fatalf("nothing in the documents answers this, yet Atlas answered: %s", answer.Text)
	}
}
