// MCP Server binary — runs over stdio for Claude Code / Chatbox / AI Agent integration.
//
// Reads config.yaml for embedding/LLM/rerank settings.
// Environment variables override config.yaml values.
//
// Environment variables (all optional, override config.yaml):
//
//	MEMORY_DB_PATH, MEMORY_CONFIG_PATH,
//	MEMORY_EMBED_PROVIDER, MEMORY_EMBED_KEY, MEMORY_EMBED_MODEL, MEMORY_EMBED_ENDPOINT,
//	MEMORY_LLM_KEY, MEMORY_LLM_MODEL, MEMORY_LLM_ENDPOINT,
//	MEMORY_RERANK_PROVIDER, MEMORY_RERANK_KEY, MEMORY_RERANK_MODEL, MEMORY_RERANK_ENDPOINT,
//	MEMORY_SINGLE_TOOL=1, MEMORY_EXPOSE_ALL_TOOLS=1
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yourusername/hybridmem-rag/internal/bootstrap"
	"github.com/yourusername/hybridmem-rag/internal/mcp"
)

func main() {
	// Debug log to file (stderr may be discarded by MCP hosts)
	logFile, _ := os.OpenFile("/tmp/hybridmem-mcp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logFile != nil {
		os.Stderr = logFile
	}

	app, err := bootstrap.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap error: %v\n", err)
		os.Exit(1)
	}
	defer app.Close()

	srv := mcp.NewWithLLMAndAbstractor(app.Store, app.Embedder, mcp.DefaultConfig(), app.MainLLM, app.Consolidator, app.Abstractor())
	fmt.Fprintf(os.Stderr, "[memory] server ready (db=%s)\n", app.DBPath)

	if err := srv.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
