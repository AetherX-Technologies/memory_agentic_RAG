package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EmbeddingConfig holds embedding configuration
type EmbeddingConfig struct {
	Enabled  bool
	Provider string // "openai", "jina", "voyage"
	APIKey   string
	Model    string
	Endpoint string
	Timeout  int // seconds
}

// DefaultEmbeddingConfig returns default config
func DefaultEmbeddingConfig() EmbeddingConfig {
	return EmbeddingConfig{
		Enabled:  false,
		Provider: "jina",
		Model:    "jina-embeddings-v3",
		Endpoint: "https://api.jina.ai/v1/embeddings",
		Timeout:  10,
	}
}

// Embedder interface for different providers
type Embedder interface {
	Embed(text string) ([]float32, error)
	EmbedBatch(texts []string) ([][]float32, error)
}

// NewEmbedder creates an embedder based on config
func NewEmbedder(config EmbeddingConfig) Embedder {
	if !config.Enabled {
		return &noopEmbedder{}
	}

	client := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
	}

	switch config.Provider {
	case "jina":
		return &jinaEmbedder{config: config, client: client}
	case "openai":
		return &openaiEmbedder{config: config, client: client}
	default:
		return &noopEmbedder{}
	}
}

// noopEmbedder does nothing
type noopEmbedder struct{}

func (e *noopEmbedder) Embed(text string) ([]float32, error) {
	return nil, fmt.Errorf("embedder not enabled")
}

func (e *noopEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("embedder not enabled")
}

// jinaEmbedder implements Jina AI embeddings
type jinaEmbedder struct {
	config EmbeddingConfig
	client *http.Client
}

func (e *jinaEmbedder) Embed(text string) ([]float32, error) {
	results, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func (e *jinaEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	reqBody := map[string]interface{}{
		"model": e.config.Model,
		"input": texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", e.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+e.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
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
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, err
	}

	results := make([][]float32, len(apiResp.Data))
	for i, d := range apiResp.Data {
		results[i] = d.Embedding
	}

	return results, nil
}

// openaiEmbedder implements OpenAI embeddings
type openaiEmbedder struct {
	config EmbeddingConfig
	client *http.Client
}

func (e *openaiEmbedder) Embed(text string) ([]float32, error) {
	results, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func (e *openaiEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	endpoint := e.config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/embeddings"
	}

	reqBody := map[string]interface{}{
		"model": e.config.Model,
		"input": texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+e.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai API error: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, err
	}

	results := make([][]float32, len(apiResp.Data))
	for i, d := range apiResp.Data {
		results[i] = d.Embedding
	}

	return results, nil
}
