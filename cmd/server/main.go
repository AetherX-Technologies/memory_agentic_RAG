package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/yourusername/hybridmem-rag/internal/api"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

func main() {
	config := store.Config{
		DBPath:    "test_server.db",
		VectorDim: 1536,
	}

	st, err := store.New(config)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	handler := api.NewHandler(st)

	addr := ":8080"
	fmt.Printf("Server starting on %s\n", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
