package search

import "fmt"

// Mode is what a question gets with the models that are installed.
type Mode string

// Modes, best first.
const (
	ModeGenerated Mode = "generated" // a written answer with citations
	ModePassages  Mode = "passages"  // nothing writes: the best passages, found by words and by meaning
	ModeKeyword   Mode = "keyword"   // nothing writes: the passages that share words with the question
)

// Reasons a mode is not a better one. They are fixed sentences, safe to show to an administrator.
const (
	gapNoLLM      = "No language model: answers are not written"
	gapNoReranker = "No reranker: answers are not written"
	gapNoEmbedder = "No embedding model: meaning search is off"
	gapWrongModel = "Embedding model %s does not match the index (" + bgeM3 + "): meaning search is off"
)

// Capabilities is the mode of the installation and why it is not a better one.
type Capabilities struct {
	Mode Mode
	Gaps []string
}

// Capabilities reports what the configured models allow. A written answer needs the language model and the
// reranker; meaning search needs an embedding service whose model is the one the vector index was built for.
func (s *Service) Capabilities() Capabilities {
	gaps := []string{}
	if s.LLM == nil {
		gaps = append(gaps, gapNoLLM)
	}
	if s.Reranker == nil {
		gaps = append(gaps, gapNoReranker)
	}
	meaning := s.meaningSearchWorks()
	if !meaning {
		gaps = append(gaps, s.meaningSearchGap())
	}
	return Capabilities{Mode: modeFor(s.LLM != nil && s.Reranker != nil, meaning), Gaps: gaps}
}

func modeFor(canWrite, meaning bool) Mode {
	switch {
	case canWrite:
		return ModeGenerated
	case meaning:
		return ModePassages
	default:
		return ModeKeyword
	}
}

func (s *Service) meaningSearchWorks() bool {
	return s.Embedder != nil && s.EmbedModel == bgeM3
}

func (s *Service) meaningSearchGap() string {
	if s.Embedder == nil {
		return gapNoEmbedder
	}
	return fmt.Sprintf(gapWrongModel, s.EmbedModel)
}
