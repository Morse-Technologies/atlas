package search

import (
	"context"
	"reflect"
	"testing"

	"github.com/morse/atlas/server/internal/platform/models/llm"
)

type stubGenerator struct{}

func (stubGenerator) Model() string { return "stub" }
func (stubGenerator) Stream(context.Context, llm.Request, func(string) error) error {
	return nil
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(context.Context, []string) ([][]float32, error) { return nil, nil }

type stubReranker struct{}

func (stubReranker) Rerank(context.Context, string, []string) ([]float64, error) { return nil, nil }

type capabilityCase struct {
	cfg  Config
	mode Mode
	gaps []string
}

var capabilityCases = map[string]capabilityCase{
	"everything": {Config{LLM: stubGenerator{}, Reranker: stubReranker{}, Embedder: stubEmbedder{}, EmbedModel: "bge-m3"}, ModeGenerated, []string{}},
	"no vectors still writes": {Config{LLM: stubGenerator{}, Reranker: stubReranker{}}, ModeGenerated,
		[]string{"No embedding model: meaning search is off"}},
	"no reranker": {Config{LLM: stubGenerator{}, Embedder: stubEmbedder{}, EmbedModel: "bge-m3"}, ModePassages,
		[]string{"No reranker: answers are not written"}},
	"no language model": {Config{Reranker: stubReranker{}, Embedder: stubEmbedder{}, EmbedModel: "bge-m3"}, ModePassages,
		[]string{"No language model: answers are not written"}},
	"another embedding model": {Config{Embedder: stubEmbedder{}, EmbedModel: "e5-large"}, ModeKeyword, []string{
		"No language model: answers are not written", "No reranker: answers are not written",
		"Embedding model e5-large does not match the index (bge-m3): meaning search is off"}},
	"nothing": {Config{}, ModeKeyword, []string{
		"No language model: answers are not written", "No reranker: answers are not written",
		"No embedding model: meaning search is off"}},
}

func TestCapabilitiesFollowTheModelsInstalled(t *testing.T) {
	for name, c := range capabilityCases {
		got := (&Service{Config: c.cfg}).Capabilities()
		if got.Mode != c.mode || !reflect.DeepEqual(got.Gaps, c.gaps) {
			t.Errorf("%s: Capabilities = %+v, want mode %s with gaps %v", name, got, c.mode, c.gaps)
		}
	}
}
