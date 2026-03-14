package store

import (
	"testing"
)

func TestDefaultRerankConfig(t *testing.T) {
	cfg := DefaultRerankConfig()

	if cfg.Enabled {
		t.Error("default should be disabled")
	}
	if cfg.Provider != "jina" {
		t.Errorf("expected jina, got %s", cfg.Provider)
	}
	if cfg.BlendWeight != 0.6 {
		t.Errorf("expected 0.6, got %f", cfg.BlendWeight)
	}
}

func TestNoopReranker(t *testing.T) {
	r := &noopReranker{}

	results := []SearchResult{
		{Entry: Memory{Text: "test1"}, Score: 0.9},
		{Entry: Memory{Text: "test2"}, Score: 0.8},
	}

	reranked, err := r.Rerank("query", results)
	if err != nil {
		t.Fatal(err)
	}

	if len(reranked) != 2 {
		t.Errorf("expected 2 results, got %d", len(reranked))
	}
	if reranked[0].Score != 0.9 {
		t.Errorf("expected 0.9, got %f", reranked[0].Score)
	}
}

func TestBlendScores(t *testing.T) {
	cfg := DefaultRerankConfig()
	cfg.BlendWeight = 0.6
	cfg.UnreturnedPenalty = 0.8
	cfg.MinBlendedScore = 0.5

	r := &jinaReranker{config: cfg}

	candidates := []SearchResult{
		{Entry: Memory{Text: "doc1"}, Score: 0.8},
		{Entry: Memory{Text: "doc2"}, Score: 0.7},
	}

	rerankResults := []RerankResult{
		{Index: 0, RelevanceScore: 0.95},
		{Index: 1, RelevanceScore: 0.85},
	}

	blended := r.blendScores(candidates, rerankResults)

	// 0.6 * 0.95 + 0.4 * 0.8 = 0.57 + 0.32 = 0.89
	expected := 0.89
	if blended[0].Score < expected-0.01 || blended[0].Score > expected+0.01 {
		t.Errorf("expected ~%.2f, got %.2f", expected, blended[0].Score)
	}
}
