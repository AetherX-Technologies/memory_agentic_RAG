// MCP Server binary — runs over stdio for Claude Code / Chatbox / AI Agent integration.
//
// Environment variables:
//   MEMORY_DB_PATH       — SQLite database path (default: memory.db)
//   MEMORY_EMBED_PROVIDER — Embedding provider: "jina", "openai", or "" for none (default: "")
//   MEMORY_EMBED_KEY     — Embedding API key
//   MEMORY_EMBED_MODEL   — Embedding model name (default: jina-embeddings-v3 / text-embedding-3-small)
//   MEMORY_EMBED_ENDPOINT — Embedding API endpoint (optional, has defaults per provider)
//   MEMORY_LLM_KEY       — LLM API key for consolidation (optional)
//   MEMORY_LLM_MODEL     — LLM model name (default: gpt-5.4)
//   MEMORY_LLM_ENDPOINT  — LLM API endpoint (default: https://api.openai.com/v1/chat/completions)
//
// If no MEMORY_EMBED_PROVIDER is set, embedding is disabled (BM25-only search).
// If no MEMORY_LLM_KEY is set, memory_consolidate returns "unavailable".
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

	st, err := store.New(store.Config{DBPath: dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	// Embedding: disabled by default, enable with MEMORY_EMBED_PROVIDER
	var embedder store.Embedder
	embedProvider := os.Getenv("MEMORY_EMBED_PROVIDER")
	if embedProvider != "" {
		cfg := store.EmbeddingConfig{
			Enabled:  true,
			Provider: embedProvider,
			APIKey:   os.Getenv("MEMORY_EMBED_KEY"),
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
		fmt.Fprintf(os.Stderr, "[memory] embedding: %s/%s\n", embedProvider, cfg.Model)
	} else {
		fmt.Fprintf(os.Stderr, "[memory] embedding: disabled (BM25-only)\n")
	}

	// LLM: optional, for memory_consolidate
	var cons *consolidate.Consolidator
	llmKey := os.Getenv("MEMORY_LLM_KEY")
	if llmKey != "" {
		cons = consolidate.New(st, consolidate.Config{
			LLMAPIKey:   llmKey,
			LLMModel:    envOr("MEMORY_LLM_MODEL", "gpt-5.4"),
			LLMEndpoint: envOr("MEMORY_LLM_ENDPOINT", "https://api.openai.com/v1/chat/completions"),
			LLMTimeout:  120,
			MaxMemories: 50,
		})
		fmt.Fprintf(os.Stderr, "[memory] LLM consolidation: enabled\n")
	} else {
		fmt.Fprintf(os.Stderr, "[memory] LLM consolidation: disabled\n")
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
