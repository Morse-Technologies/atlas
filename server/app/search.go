package app

import (
	"time"

	"github.com/morse/atlas/server/internal/httpapi"
	"github.com/morse/atlas/server/internal/platform/config"
	"github.com/morse/atlas/server/internal/platform/db"
	"github.com/morse/atlas/server/internal/platform/models/llm"
	"github.com/morse/atlas/server/internal/platform/models/tei"
	"github.com/morse/atlas/server/internal/search"
)

// newSearch builds the search service from the model URLs in the environment. Each model is optional: what is
// not configured is left out and search degrades (ATLAS.md §11.3). It also returns a readiness check per model.
func newSearch(d *db.DB) (*search.Service, []httpapi.Check, error) {
	embed, err := config.Load[config.Embedding]()
	if err != nil {
		return nil, nil, err
	}
	answering, err := config.Load[config.Answering]()
	if err != nil {
		return nil, nil, err
	}
	cfg := search.Config{DB: d, EmbedModel: embed.Model, RerankModel: answering.RerankModel, MaxGenerations: answering.MaxGenerations, CommonShare: answering.CommonShare}
	checks := connectModels(&cfg, modelURLs{rerank: answering.RerankURL, rerankTimeout: answering.RerankTimeout, llm: answering.LLMURL, llmModel: answering.LLMModel})
	embedChecks, err := connectEmbedding(&cfg, embed)
	if err != nil {
		return nil, nil, err
	}
	return search.NewService(cfg), append(embedChecks, checks...), nil
}

// modelURLs are the optional model services; an empty URL leaves that model out.
type modelURLs struct {
	rerank, llm, llmModel string
	rerankTimeout         time.Duration
}

// connectModels adds a client to cfg for every URL that is set, and returns a readiness check for each.
func connectModels(cfg *search.Config, urls modelURLs) []httpapi.Check {
	var checks []httpapi.Check
	if urls.rerank != "" {
		client := tei.New(urls.rerank).WithTimeout(urls.rerankTimeout)
		cfg.Reranker, checks = client, append(checks, httpapi.Check{Name: "rerank", Probe: client.Health})
	}
	if urls.llm != "" {
		client := llm.New(urls.llm, urls.llmModel)
		cfg.LLM, checks = client, append(checks, httpapi.Check{Name: "llm", Probe: client.Health})
	}
	return checks
}

// connectEmbedding adds the embedding client to cfg when its URL is set, with its readiness check.
func connectEmbedding(cfg *search.Config, embed config.Embedding) ([]httpapi.Check, error) {
	if embed.URL == "" {
		return nil, nil
	}
	e, err := newEmbedding(embed)
	if err != nil {
		return nil, err
	}
	cfg.Embedder = e.embedder
	return []httpapi.Check{{Name: "embed", Probe: e.health}}, nil
}
