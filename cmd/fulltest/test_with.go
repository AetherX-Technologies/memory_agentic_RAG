package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

func testWithRerank(files []string) {
	cfg := store.Config{
		DBPath:    ":memory:",
		VectorDim: dimension,
		RerankConfig: store.RerankConfig{
			Enabled:           true,
			Provider:          "jina",
			APIKey:            jinaAPIKey,
			Model:             "jina-reranker-v2-base-multilingual",
			Endpoint:          jinaRerankURL,
			Timeout:           5,
			BlendWeight:       0.6,
			MaxCandidates:     50,
			MaxDocLength:      2000,
			UnreturnedPenalty: 0.8,
			MinBlendedScore:   0.5,
		},
	}

	st, _ := store.New(cfg)
	defer st.Close()

	// Insert documents
	fmt.Println("Inserting documents...")
	for i, file := range files {
		content, err := os.ReadFile(file)
		if err != nil || len(content) == 0 {
			continue
		}

		text := string(content)
		if len(text) > 2000 {
			text = text[:2000]
		}

		vec, err := getEmbedding(text)
		if err != nil {
			fmt.Printf("  ✗ Error: %v\n", err)
			continue
		}

		st.Insert(&store.Memory{
			Text:       text,
			Vector:     vec,
			Scope:      "global",
			Importance: 0.8,
			Metadata:   filepath.Base(file),
		})
		fmt.Printf("  ✓ %d/%d: %s\n", i+1, len(files), filepath.Base(file))
		time.Sleep(200 * time.Millisecond)
	}

	// Run queries
	queries := []string{
		"AI人工智能",
		"会议纪要",
		"技术文档",
	}

	for _, q := range queries {
		fmt.Printf("\nQuery: %s\n", q)
		qVec, _ := getEmbedding(q)
		results, _ := st.HybridSearch(qVec, q, 3, nil)

		for i, r := range results {
			name := r.Entry.Metadata
			if name == "" {
				name = "unknown"
			}
			fmt.Printf("  %d. %.4f - %s\n", i+1, r.Score, name)
		}
	}
}
