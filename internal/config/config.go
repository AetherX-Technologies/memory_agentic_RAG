// Package config provides a unified configuration system for HybridMem-RAG.
// All model choices, API keys, and behavior flags are controlled from a single YAML file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/yourusername/hybridmem-rag/internal/embedder"
	"github.com/yourusername/hybridmem-rag/internal/generator"
	"github.com/yourusername/hybridmem-rag/internal/parser"
	"github.com/yourusername/hybridmem-rag/internal/retrieval"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

// AppConfig is the root configuration loaded from config.yaml.
type AppConfig struct {
	Store     StoreConfig     `yaml:"store"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Rerank    RerankConfig    `yaml:"rerank"`
	LLM       LLMConfig       `yaml:"llm"`
	Splitter  SplitterConfig  `yaml:"splitter"`
	Retrieval RetrievalConfig `yaml:"retrieval"`
}

// StoreConfig controls the database.
type StoreConfig struct {
	DBPath    string `yaml:"db_path"`    // default: "data/memories.db"
	VectorDim int    `yaml:"vector_dim"` // 0 = auto-detect
}

// EmbeddingConfig controls the embedding model (local or API).
type EmbeddingConfig struct {
	Provider string           `yaml:"provider"` // "local" | "jina" | "openai"
	Local    LocalModelConfig `yaml:"local"`
	Jina     APIModelConfig   `yaml:"jina"`
	OpenAI   APIModelConfig   `yaml:"openai"`
}

// RerankConfig controls the reranker (API-based or disabled).
type RerankConfig struct {
	Enabled           bool    `yaml:"enabled"`
	Provider          string  `yaml:"provider"` // "jina" | "none"
	APIKey            string  `yaml:"api_key"`
	Model             string  `yaml:"model"`              // default: "jina-reranker-v2-base-multilingual"
	Endpoint          string  `yaml:"endpoint"`           // default: "https://api.jina.ai/v1/rerank"
	Timeout           int     `yaml:"timeout"`            // seconds, default: 5
	BlendWeight       float64 `yaml:"blend_weight"`       // default: 0.6
	MaxCandidates     int     `yaml:"max_candidates"`     // default: 50
	MaxDocLength      int     `yaml:"max_doc_length"`     // default: 2000
	UnreturnedPenalty float64 `yaml:"unreturned_penalty"` // default: 0.8
	MinBlendedScore   float64 `yaml:"min_blended_score"`  // default: 0.5
}

// LLMConfig controls the LLM for L0/L1 generation and memory extraction.
// Only OpenAI-compatible endpoints are supported (set endpoint for custom providers).
type LLMConfig struct {
	APIKey      string         `yaml:"api_key"`
	Model       string         `yaml:"model"`       // default: "gpt-4o-mini"
	Endpoint    string         `yaml:"endpoint"`    // OpenAI-compatible endpoint, default: "https://api.openai.com/v1/chat/completions"
	Timeout     int            `yaml:"timeout"`     // seconds, default: 30
	Concurrency int            `yaml:"concurrency"` // default: 5
	Summary     *LLMSubConfig  `yaml:"summary,omitempty"` // optional smaller model for lightweight tasks
}

// LLMSubConfig is a tier-specific LLM override (e.g. summary tier).
// Empty fields inherit from the parent LLMConfig.
type LLMSubConfig struct {
	APIKey   string `yaml:"api_key"`  // empty = inherit
	Model    string `yaml:"model"`    // empty = inherit
	Endpoint string `yaml:"endpoint"` // empty = inherit
	Timeout  int    `yaml:"timeout"`  // 0 = inherit
}

// SplitterConfig controls document splitting behavior.
type SplitterConfig struct {
	MaxChunkSize   int  `yaml:"max_chunk_size"`  // default: 512
	MinChunkSize   int  `yaml:"min_chunk_size"`  // default: 256
	EnableSemantic bool `yaml:"enable_semantic"` // if true, uses embedding.local model
	MinSegment     int  `yaml:"min_segment"`     // default: 3
}

// RetrievalConfig controls the retrieval strategy.
type RetrievalConfig struct {
	Mode     string  `yaml:"mode"`      // "bm25" | "vector" | "hybrid" | "openviking"
	Alpha    float64 `yaml:"alpha"`     // OpenViking score propagation weight, default: 0.7
	MaxDepth int     `yaml:"max_depth"` // OpenViking max recursion depth, default: 5
}

// LocalModelConfig for ONNX-based local models.
type LocalModelConfig struct {
	ModelPath string `yaml:"model_path"`
	BatchSize int    `yaml:"batch_size"`  // default: 16
	MaxSeqLen int    `yaml:"max_seq_len"` // default: 512
}

// APIModelConfig for API-based models (Jina, OpenAI, etc.).
type APIModelConfig struct {
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
	Endpoint string `yaml:"endpoint"`
	Timeout  int    `yaml:"timeout"` // seconds, default: 30
}

// Load reads and parses a YAML config file.
func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	cfg := &AppConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	applyDefaults(cfg)
	return cfg, nil
}

// Default returns a config object populated with defaults.
func Default() *AppConfig {
	cfg := &AppConfig{}
	applyDefaults(cfg)
	return cfg
}

// ApplyDefaults fills in missing values with sensible defaults.
func ApplyDefaults(cfg *AppConfig) {
	applyDefaults(cfg)
}

// applyDefaults fills in missing values with sensible defaults.
func applyDefaults(c *AppConfig) {
	if c.Store.DBPath == "" {
		c.Store.DBPath = "data/memories.db"
	}
	if c.Embedding.Provider == "" {
		c.Embedding.Provider = "local"
	}
	if c.Embedding.Local.BatchSize <= 0 {
		c.Embedding.Local.BatchSize = 16
	}
	if c.Embedding.Local.MaxSeqLen <= 0 {
		c.Embedding.Local.MaxSeqLen = 512
	}
	if c.Rerank.Model == "" {
		c.Rerank.Model = "jina-reranker-v2-base-multilingual"
	}
	if c.Rerank.Endpoint == "" {
		c.Rerank.Endpoint = "https://api.jina.ai/v1/rerank"
	}
	if c.Rerank.Timeout <= 0 {
		c.Rerank.Timeout = 5
	}
	if c.Rerank.BlendWeight <= 0 {
		c.Rerank.BlendWeight = 0.6
	}
	if c.Rerank.MaxCandidates <= 0 {
		c.Rerank.MaxCandidates = 50
	}
	if c.Rerank.MaxDocLength <= 0 {
		c.Rerank.MaxDocLength = 2000
	}
	if c.Rerank.UnreturnedPenalty <= 0 {
		c.Rerank.UnreturnedPenalty = 0.8
	}
	if c.Rerank.MinBlendedScore <= 0 {
		c.Rerank.MinBlendedScore = 0.5
	}
	if c.LLM.Model == "" {
		c.LLM.Model = "gpt-4o-mini"
	}
	if c.LLM.Endpoint == "" {
		c.LLM.Endpoint = "https://api.openai.com/v1/chat/completions"
	}
	if c.LLM.Timeout <= 0 {
		c.LLM.Timeout = 30
	}
	if c.LLM.Concurrency <= 0 {
		c.LLM.Concurrency = 5
	}
	if c.Splitter.MaxChunkSize <= 0 {
		c.Splitter.MaxChunkSize = 512
	}
	if c.Splitter.MinChunkSize <= 0 {
		c.Splitter.MinChunkSize = 256
	}
	if c.Splitter.MinSegment <= 0 {
		c.Splitter.MinSegment = 3
	}
	if c.Retrieval.Mode == "" {
		c.Retrieval.Mode = "hybrid"
	}
	if c.Retrieval.Alpha <= 0 {
		c.Retrieval.Alpha = 0.7
	}
	if c.Retrieval.MaxDepth <= 0 {
		c.Retrieval.MaxDepth = 5
	}
}

// ToStoreConfig converts to the store package's Config.
func (c *AppConfig) ToStoreConfig() store.Config {
	return store.Config{
		DBPath:    c.Store.DBPath,
		VectorDim: c.Store.VectorDim,
		RerankConfig: store.RerankConfig{
			Enabled:           c.Rerank.Enabled,
			Provider:          c.Rerank.Provider,
			APIKey:            c.Rerank.APIKey,
			Model:             c.Rerank.Model,
			Endpoint:          c.Rerank.Endpoint,
			Timeout:           c.Rerank.Timeout,
			BlendWeight:       c.Rerank.BlendWeight,
			MaxCandidates:     c.Rerank.MaxCandidates,
			MaxDocLength:      c.Rerank.MaxDocLength,
			UnreturnedPenalty: c.Rerank.UnreturnedPenalty,
			MinBlendedScore:   c.Rerank.MinBlendedScore,
		},
	}
}

// ToEmbeddingConfig converts to the store package's EmbeddingConfig (for API embedders).
func (c *AppConfig) ToEmbeddingConfig() store.EmbeddingConfig {
	switch c.Embedding.Provider {
	case "jina":
		timeout := c.Embedding.Jina.Timeout
		if timeout <= 0 {
			timeout = 30
		}
		return store.EmbeddingConfig{
			Enabled:  true,
			Provider: "jina",
			APIKey:   c.Embedding.Jina.APIKey,
			Model:    c.Embedding.Jina.Model,
			Endpoint: c.Embedding.Jina.Endpoint,
			Timeout:  timeout,
		}
	case "openai":
		timeout := c.Embedding.OpenAI.Timeout
		if timeout <= 0 {
			timeout = 30
		}
		return store.EmbeddingConfig{
			Enabled:  true,
			Provider: "openai",
			APIKey:   c.Embedding.OpenAI.APIKey,
			Model:    c.Embedding.OpenAI.Model,
			Endpoint: c.Embedding.OpenAI.Endpoint,
			Timeout:  timeout,
		}
	default:
		return store.EmbeddingConfig{Enabled: false}
	}
}

// ToLocalEmbedderConfig converts to the embedder package's Config (for local ONNX).
func (c *AppConfig) ToLocalEmbedderConfig() embedder.Config {
	return embedder.Config{
		ModelPath: c.Embedding.Local.ModelPath,
		BatchSize: c.Embedding.Local.BatchSize,
		MaxSeqLen: c.Embedding.Local.MaxSeqLen,
	}
}

// ToGeneratorConfig converts to the generator package's Config.
func (c *AppConfig) ToGeneratorConfig() generator.Config {
	return generator.Config{
		APIKey:      c.LLM.APIKey,
		Model:       c.LLM.Model,
		Endpoint:    c.LLM.Endpoint,
		Timeout:     c.LLM.Timeout,
		Concurrency: c.LLM.Concurrency,
		MaxRetries:  2, // generator default
	}
}

// ToSplitterConfig converts to the parser package's SplitterConfig.
func (c *AppConfig) ToSplitterConfig() parser.SplitterConfig {
	return parser.SplitterConfig{
		MaxChunkSize:   c.Splitter.MaxChunkSize,
		MinChunkSize:   c.Splitter.MinChunkSize,
		EnableSemantic: c.Splitter.EnableSemantic,
		MinSegment:     c.Splitter.MinSegment,
	}
}

// ToRetrievalConfig converts to the retrieval package's Config.
func (c *AppConfig) ToRetrievalConfig() retrieval.Config {
	return retrieval.Config{
		Alpha:    c.Retrieval.Alpha,
		MaxDepth: c.Retrieval.MaxDepth,
	}
}

// IsLocalEmbedding returns true if the embedding provider is "local".
func (c *AppConfig) IsLocalEmbedding() bool {
	return c.Embedding.Provider == "local"
}
