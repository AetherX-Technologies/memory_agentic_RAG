// MCP Server binary — runs over stdio for Claude Code / AI Agent integration.
// Usage: go build -tags fts5 -o hybridmem-mcp ./cmd/mcp_server && ./hybridmem-mcp
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yourusername/hybridmem-rag/internal/mcp"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

func main() {
	dbPath := os.Getenv("MEMORY_DB_PATH")
	if dbPath == "" {
		dbPath = "memory.db"
	}

	st, err := store.New(store.Config{DBPath: dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	srv := mcp.New(st, nil, mcp.DefaultConfig())
	if err := srv.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
