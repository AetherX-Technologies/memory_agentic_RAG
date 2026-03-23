// MCP Server binary — runs over stdio for Claude Code / Chatbox / AI Agent integration.
//
// Reads config.yaml for embedding/LLM/rerank settings.
// Environment variables override config.yaml values.
//
// Environment variables (all optional, override config.yaml):
//   MEMORY_DB_PATH, MEMORY_CONFIG_PATH,
//   MEMORY_EMBED_PROVIDER, MEMORY_EMBED_KEY, MEMORY_EMBED_MODEL, MEMORY_EMBED_ENDPOINT,
//   MEMORY_LLM_KEY, MEMORY_LLM_MODEL, MEMORY_LLM_ENDPOINT,
//   MEMORY_RERANK_PROVIDER, MEMORY_RERANK_KEY, MEMORY_RERANK_MODEL, MEMORY_RERANK_ENDPOINT,
//   MEMORY_SINGLE_TOOL=1, MEMORY_EXPOSE_ALL_TOOLS=1
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourusername/hybridmem-rag/internal/config"
	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/embedder"
	"github.com/yourusername/hybridmem-rag/internal/mcp"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

func main() {
	// Debug log to file (stderr may be discarded by MCP hosts)
	logFile, _ := os.OpenFile("/tmp/hybridmem-mcp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logFile != nil {
		os.Stderr = logFile
	}

	// ── Load config.yaml ──
	configPath := envOr("MEMORY_CONFIG_PATH", "")
	var cfg *config.AppConfig
	if configPath != "" {
		var err error
		cfg, err = config.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[memory] config load error: %v\n", err)
		}
	}
	if cfg == nil {
		// Try default locations
		for _, p := range []string{"config.yaml", filepath.Join(filepath.Dir(os.Args[0]), "config.yaml")} {
			if _, err := os.Stat(p); err == nil {
				cfg, _ = config.Load(p)
				if cfg != nil {
					fmt.Fprintf(os.Stderr, "[memory] loaded config: %s\n", p)
					break
				}
			}
		}
	}
	if cfg == nil {
		cfg = &config.AppConfig{}
		config.Load("") // just to apply defaults — ignore error
	}

	// ── DB path (env overrides config) ──
	dbPath := envOr("MEMORY_DB_PATH", cfg.Store.DBPath)
	if dbPath == "" {
		dbPath = "memory.db"
	}

	// ── Reranker ──
	rerankProvider := envOr("MEMORY_RERANK_PROVIDER", cfg.Rerank.Provider)
	rerankKey := envOr("MEMORY_RERANK_KEY", cfg.Rerank.APIKey)
	storeCfg := cfg.ToStoreConfig()
	storeCfg.DBPath = dbPath
	if rerankProvider != "" && rerankKey != "" {
		storeCfg.RerankConfig.Enabled = true
		storeCfg.RerankConfig.Provider = rerankProvider
		storeCfg.RerankConfig.APIKey = rerankKey
		storeCfg.RerankConfig.Model = envOr("MEMORY_RERANK_MODEL", storeCfg.RerankConfig.Model)
		storeCfg.RerankConfig.Endpoint = envOr("MEMORY_RERANK_ENDPOINT", storeCfg.RerankConfig.Endpoint)
		fmt.Fprintf(os.Stderr, "[memory] reranker: %s/%s\n", rerankProvider, storeCfg.RerankConfig.Model)
	} else {
		storeCfg.RerankConfig.Enabled = false
		fmt.Fprintf(os.Stderr, "[memory] reranker: disabled\n")
	}

	st, err := store.New(storeCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	// ── Embedding ──
	var emb store.Embedder
	embedProvider := envOr("MEMORY_EMBED_PROVIDER", cfg.Embedding.Provider)
	embedKey := os.Getenv("MEMORY_EMBED_KEY")
	if embedKey == "" && (embedProvider == "jina" || embedProvider == "openai") {
		// Check config for API key
		switch embedProvider {
		case "jina":
			embedKey = cfg.Embedding.Jina.APIKey
		case "openai":
			embedKey = cfg.Embedding.OpenAI.APIKey
		}
	}

	switch embedProvider {
	case "local":
		// Local ONNX embedding (Qwen3-0.6B)
		localCfg := cfg.ToLocalEmbedderConfig()
		// Resolve relative model path
		if !filepath.IsAbs(localCfg.ModelPath) && localCfg.ModelPath != "" {
			// Try relative to executable, then CWD
			exeDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
			candidate := filepath.Join(exeDir, localCfg.ModelPath)
			if _, err := os.Stat(candidate); err == nil {
				localCfg.ModelPath = candidate
			}
		}
		localEmb, err := embedder.NewLocalEmbedder(localCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[memory] local embedder failed: %v (falling back to BM25-only)\n", err)
		} else {
			emb = localEmb
			fmt.Fprintf(os.Stderr, "[memory] embedding: local Qwen3 (%s)\n", localCfg.ModelPath)
		}
	case "jina", "openai":
		if embedKey != "" {
			apiCfg := store.EmbeddingConfig{
				Enabled:  true,
				Provider: embedProvider,
				APIKey:   embedKey,
				Timeout:  10,
			}
			switch embedProvider {
			case "jina":
				apiCfg.Model = envOr("MEMORY_EMBED_MODEL", cfg.Embedding.Jina.Model)
				apiCfg.Endpoint = envOr("MEMORY_EMBED_ENDPOINT", cfg.Embedding.Jina.Endpoint)
			case "openai":
				apiCfg.Model = envOr("MEMORY_EMBED_MODEL", cfg.Embedding.OpenAI.Model)
				apiCfg.Endpoint = envOr("MEMORY_EMBED_ENDPOINT", cfg.Embedding.OpenAI.Endpoint)
			}
			emb = store.NewEmbedder(apiCfg)
			fmt.Fprintf(os.Stderr, "[memory] embedding: %s/%s\n", embedProvider, apiCfg.Model)
		} else {
			fmt.Fprintf(os.Stderr, "[memory] embedding: %s configured but no API key\n", embedProvider)
		}
	default:
		fmt.Fprintf(os.Stderr, "[memory] embedding: disabled (BM25-only)\n")
	}

	// ── LLM (for consolidation) ──
	var cons *consolidate.Consolidator
	llmKey := envOr("MEMORY_LLM_KEY", cfg.LLM.APIKey)
	if llmKey != "" {
		llmEndpoint := envOr("MEMORY_LLM_ENDPOINT", cfg.LLM.Endpoint)
		llmModel := envOr("MEMORY_LLM_MODEL", cfg.LLM.Model)
		cons = consolidate.New(st, consolidate.Config{
			LLMAPIKey:   llmKey,
			LLMModel:    llmModel,
			LLMEndpoint: llmEndpoint,
			LLMTimeout:  120,
			MaxMemories: 50,
		})
		fmt.Fprintf(os.Stderr, "[memory] LLM: %s @ %s\n", llmModel, llmEndpoint)
	} else {
		fmt.Fprintf(os.Stderr, "[memory] LLM: disabled\n")
	}

	srv := mcp.New(st, emb, mcp.DefaultConfig(), cons)
	fmt.Fprintf(os.Stderr, "[memory] server ready (db=%s)\n", dbPath)

	if err := srv.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
