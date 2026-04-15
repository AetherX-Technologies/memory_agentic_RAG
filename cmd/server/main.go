package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/yourusername/hybridmem-rag/internal/api"
	"github.com/yourusername/hybridmem-rag/internal/bootstrap"
)

func main() {
	app, err := bootstrap.Load()
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	handler := api.NewHandlerWithLLMAndAbstractor(app.Store, app.Embedder, app.Consolidator, app.MainLLM, app.Abstractor())

	addr := envOr("MEMORY_HTTP_ADDR", ":8080")
	fmt.Printf("Server starting on %s (db=%s)\n", addr, app.DBPath)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
