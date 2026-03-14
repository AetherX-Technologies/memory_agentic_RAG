package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RerankConfig holds reranker configuration
type RerankConfig struct {
	Enabled           bool
	Provider          string  // use ProviderJina, ProviderVoyage, ProviderCohere
	APIKey            string
	Model             string
	Endpoint          string
	Timeout           int     // seconds
	BlendWeight       float64 // rerank score weight (0.6 default)
	MaxCandidates     int     // max candidates to rerank (50 default)
	MaxDocLength      int     // max document length (2000 default)
	UnreturnedPenalty float64 // penalty for unreturned results (0.8 default)
	MinBlendedScore   float64 // min blended score ratio (0.5 default)
}

const (
	ProviderJina   = "jina"
	ProviderVoyage = "voyage"
	ProviderCohere = "cohere"
)

// DefaultRerankConfig returns conservative defaults
func DefaultRerankConfig() RerankConfig {
	return RerankConfig{
		Enabled:           false,
		Provider:          ProviderJina,
		Model:             "jina-reranker-v2-base-multilingual",
		Endpoint:          "https://api.jina.ai/v1/rerank",
		Timeout:           5,
		BlendWeight:       0.6,
		MaxCandidates:     50,
		MaxDocLength:      2000,
		UnreturnedPenalty: 0.8,
		MinBlendedScore:   0.5,
	}
}

// RerankResult represents a single rerank result
type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Reranker interface for different providers
type Reranker interface {
	Rerank(query string, results []SearchResult) ([]SearchResult, error)
}

// jinaReranker implements Jina AI reranker
type jinaReranker struct {
	config RerankConfig
	client *http.Client
}

// NewReranker creates a reranker based on config
func NewReranker(config RerankConfig) Reranker {
	if !config.Enabled {
		return &noopReranker{}
	}

	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	switch config.Provider {
	case ProviderJina:
		return &jinaReranker{config: config, client: client}
	default:
		return &noopReranker{}
	}
}

// noopReranker does nothing
type noopReranker struct{}

func (r *noopReranker) Rerank(query string, results []SearchResult) ([]SearchResult, error) {
	return results, nil
}

// Rerank reranks search results using Jina API
func (r *jinaReranker) Rerank(query string, results []SearchResult) ([]SearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	// Limit candidates
	candidates := results
	if len(candidates) > r.config.MaxCandidates {
		candidates = candidates[:r.config.MaxCandidates]
	}

	// Extract documents
	docs := make([]string, len(candidates))
	for i, res := range candidates {
		text := res.Entry.Text
		if len(text) > r.config.MaxDocLength {
			text = text[:r.config.MaxDocLength]
		}
		docs[i] = text
	}

	// Call API
	rerankResults, err := r.callJinaAPI(query, docs)
	if err != nil {
		// Fallback: return original results on API failure
		return candidates, err
	}

	// Blend scores
	return r.blendScores(candidates, rerankResults), nil
}

func (r *jinaReranker) callJinaAPI(query string, docs []string) ([]RerankResult, error) {
	reqBody := map[string]interface{}{
		"model":     r.config.Model,
		"query":     query,
		"documents": docs,
		"top_n":     len(docs),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", r.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+r.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jina API error: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp struct {
		Results []RerankResult `json:"results"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, err
	}

	return apiResp.Results, nil
}

func (r *jinaReranker) blendScores(candidates []SearchResult, rerankResults []RerankResult) []SearchResult {
	// Create map of rerank scores
	rerankMap := make(map[int]float64)
	for _, rr := range rerankResults {
		rerankMap[rr.Index] = rr.RelevanceScore
	}

	// Blend scores
	blended := make([]SearchResult, len(candidates))
	for i, cand := range candidates {
		var finalScore float64

		if rerankScore, found := rerankMap[i]; found {
			// Returned by rerank API: blend scores
			blendedScore := r.config.BlendWeight*rerankScore + (1-r.config.BlendWeight)*cand.Score

			// Apply min threshold
			minScore := cand.Score * r.config.MinBlendedScore
			if blendedScore < minScore {
				blendedScore = minScore
			}
			finalScore = blendedScore
		} else {
			// Not returned by rerank API: apply penalty only
			finalScore = cand.Score * r.config.UnreturnedPenalty
		}

		blended[i] = SearchResult{
			Entry: cand.Entry,
			Score: finalScore,
		}
	}

	return blended
}
