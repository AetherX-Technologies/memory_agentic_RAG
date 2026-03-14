package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

const (
	jinaAPIKey = "jina_20dd039936c649f2989d66c7d5be53d9M9DPc25QT6pJHs51PUw6v0pZCZGf"
	jinaURL    = "https://api.jina.ai/v1/embeddings"
	dimension  = 1024
)

type EmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Task       string   `json:"task"`
	Dimensions int      `json:"dimensions"`
	Normalized bool     `json:"normalized"`
}

type EmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func getEmbedding(text string) ([]float32, error) {
	req := EmbedRequest{
		Model:      "jina-embeddings-v3",
		Input:      []string{text},
		Task:       "retrieval.passage",
		Dimensions: dimension,
		Normalized: true,
	}

	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest("POST", jinaURL, bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+jinaAPIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var embedResp EmbedResponse
	json.Unmarshal(data, &embedResp)

	if len(embedResp.Data) > 0 {
		return embedResp.Data[0].Embedding, nil
	}
	return nil, fmt.Errorf("no embedding")
}

func main() {
	fmt.Println("High Complexity Test")
	fmt.Println("====================\n")

	st, _ := store.New(store.Config{DBPath: "complex_test.db", VectorDim: dimension})
	defer st.Close()

	docDir := "/Volumes/SN770Coder/documents/本地AI撰写"
	files, _ := filepath.Glob(filepath.Join(docDir, "**/*.md"))
	files = append(files, mustGlob(filepath.Join(docDir, "**/*.txt"))...)

	if len(files) > 15 {
		files = files[:15]
	}

	fmt.Printf("Processing %d documents...\n\n", len(files))

	for i, file := range files {
		content, err := os.ReadFile(file)
		if err != nil || len(content) == 0 {
			continue
		}

		text := string(content)
		if len(text) > 2000 {
			text = text[:2000]
		}

		fmt.Printf("[%d/%d] %s\n", i+1, len(files), filepath.Base(file))

		vec, err := getEmbedding(text)
		if err != nil {
			fmt.Printf("  ✗ Error: %v\n", err)
			continue
		}

		st.Insert(&store.Memory{
			Text:       text,
			Vector:     vec,
			Category:   "document",
			Scope:      "global",
			Importance: 0.8,
			Metadata:   filepath.Base(file),
		})

		fmt.Printf("  ✓ Embedded\n")
		time.Sleep(200 * time.Millisecond)
	}

	runQueries(st)
}

func mustGlob(pattern string) []string {
	matches, _ := filepath.Glob(pattern)
	return matches
}

func runQueries(st store.Store) {
	fmt.Println("\n\nRunning test queries...")

	queries := []string{
		"AI人工智能相关的简历",
		"会议纪要",
		"周报",
	}

	for _, q := range queries {
		fmt.Printf("\nQuery: %s\n", q)
		qVec, _ := getEmbedding(q)
		results, _ := st.VectorSearch(qVec, 3, nil)

		for i, r := range results {
			name := r.Entry.Metadata
			if name == "" {
				name = "unknown"
			}
			fmt.Printf("  %d. %s (%.4f)\n", i+1, name, r.Score)
		}
	}
}
