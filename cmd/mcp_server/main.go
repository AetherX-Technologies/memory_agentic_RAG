// MCP Server binary — runs over stdio for Claude Code / Chatbox / AI Agent integration.
//
// All three services (embedding, LLM, rerank) are independently configurable.
// Each can point to a different provider/URL/key/model.
// If not configured, the corresponding feature is disabled gracefully.
//
// Environment variables:
//
//	MEMORY_DB_PATH            — SQLite database path (default: memory.db)
//
//	── Embedding (for vector semantic search) ──
//	MEMORY_EMBED_PROVIDER     — "jina", "openai", or "" to disable (default: "")
//	MEMORY_EMBED_KEY          — API key
//	MEMORY_EMBED_MODEL        — Model name (default per provider)
//	MEMORY_EMBED_ENDPOINT     — API URL (default per provider)
//
//	── LLM (for memory consolidation) ──
//	MEMORY_LLM_KEY            — API key, omit to disable consolidation
//	MEMORY_LLM_MODEL          — Model name (default: gpt-4o)
//	MEMORY_LLM_ENDPOINT       — API URL (default: https://api.openai.com/v1/chat/completions)
//
//	── Reranker (for search result reranking) ──
//	MEMORY_RERANK_PROVIDER    — "jina", or "" to disable (default: "")
//	MEMORY_RERANK_KEY         — API key
//	MEMORY_RERANK_MODEL       — Model name (default per provider)
//	MEMORY_RERANK_ENDPOINT    — API URL (default per provider)
//
// Examples:
//
//	# Minimal (BM25-only, no external APIs):
//	MEMORY_DB_PATH=~/memory.db ./hybridmem-mcp
//
//	# Embedding only (Jina):
//	MEMORY_EMBED_PROVIDER=jina MEMORY_EMBED_KEY=xxx ./hybridmem-mcp
//
//	# All three, different providers:
//	MEMORY_EMBED_PROVIDER=openai MEMORY_EMBED_KEY=sk-xxx MEMORY_EMBED_ENDPOINT=https://my-proxy/v1/embeddings \
//	MEMORY_LLM_KEY=sk-yyy MEMORY_LLM_ENDPOINT=https://my-llm/v1/chat/completions MEMORY_LLM_MODEL=gpt-4o \
//	MEMORY_RERANK_PROVIDER=jina MEMORY_RERANK_KEY=zzz \
//	./hybridmem-mcp
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/mcp"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

func main() {
	dbPath := envOr("MEMORY_DB_PATH", "memory.db")

	// ── Reranker config ──
	rerankCfg := store.DefaultRerankConfig()
	rerankCfg.Enabled = false
	rerankProvider := os.Getenv("MEMORY_RERANK_PROVIDER")
	rerankKey := os.Getenv("MEMORY_RERANK_KEY")
	if rerankProvider != "" && rerankKey != "" {
		rerankCfg.Enabled = true
		rerankCfg.Provider = rerankProvider
		rerankCfg.APIKey = rerankKey
		switch rerankProvider {
		case "jina":
			rerankCfg.Model = envOr("MEMORY_RERANK_MODEL", "jina-reranker-v2-base-multilingual")
			rerankCfg.Endpoint = envOr("MEMORY_RERANK_ENDPOINT", "https://api.jina.ai/v1/rerank")
		default:
			rerankCfg.Model = envOr("MEMORY_RERANK_MODEL", "")
			rerankCfg.Endpoint = envOr("MEMORY_RERANK_ENDPOINT", "")
		}
		fmt.Fprintf(os.Stderr, "[memory] reranker: %s/%s @ %s\n", rerankProvider, rerankCfg.Model, rerankCfg.Endpoint)
	} else {
		fmt.Fprintf(os.Stderr, "[memory] reranker: disabled\n")
	}

	// ── Store ──
	st, err := store.New(store.Config{
		DBPath:       dbPath,
		RerankConfig: rerankCfg,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	// ── Embedding config ──
	var embedder store.Embedder
	embedProvider := os.Getenv("MEMORY_EMBED_PROVIDER")
	embedKey := os.Getenv("MEMORY_EMBED_KEY")
	if embedProvider != "" && embedKey != "" {
		cfg := store.EmbeddingConfig{
			Enabled:  true,
			Provider: embedProvider,
			APIKey:   embedKey,
			Timeout:  10,
		}
		switch embedProvider {
		case "jina":
			cfg.Model = envOr("MEMORY_EMBED_MODEL", "jina-embeddings-v3")
			cfg.Endpoint = envOr("MEMORY_EMBED_ENDPOINT", "https://api.jina.ai/v1/embeddings")
		case "openai":
			cfg.Model = envOr("MEMORY_EMBED_MODEL", "text-embedding-3-small")
			cfg.Endpoint = envOr("MEMORY_EMBED_ENDPOINT", "https://api.openai.com/v1/embeddings")
		default:
			cfg.Model = envOr("MEMORY_EMBED_MODEL", "")
			cfg.Endpoint = envOr("MEMORY_EMBED_ENDPOINT", "")
		}
		embedder = store.NewEmbedder(cfg)
		fmt.Fprintf(os.Stderr, "[memory] embedding: %s/%s @ %s\n", embedProvider, cfg.Model, cfg.Endpoint)
	} else {
		fmt.Fprintf(os.Stderr, "[memory] embedding: disabled (BM25-only)\n")
	}

	// ── LLM config (for consolidation) ──
	var cons *consolidate.Consolidator
	llmKey := os.Getenv("MEMORY_LLM_KEY")
	if llmKey != "" {
		llmEndpoint := envOr("MEMORY_LLM_ENDPOINT", "https://api.openai.com/v1/chat/completions")
		llmModel := envOr("MEMORY_LLM_MODEL", "gpt-4o")
		cons = consolidate.New(st, consolidate.Config{
			LLMAPIKey:   llmKey,
			LLMModel:    llmModel,
			LLMEndpoint: llmEndpoint,
			LLMTimeout:  120,
			MaxMemories: 50,
		})
		fmt.Fprintf(os.Stderr, "[memory] LLM: %s @ %s\n", llmModel, llmEndpoint)
	} else {
		fmt.Fprintf(os.Stderr, "[memory] LLM: disabled (no consolidation)\n")
	}

	// Debug log to file (stderr may be discarded by MCP hosts)
	logFile, _ := os.OpenFile("/tmp/hybridmem-mcp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logFile != nil {
		os.Stderr = logFile
	}

	srv := mcp.New(st, embedder, mcp.DefaultConfig(), cons)
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
