package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const (
	jinaAPIKey    = "jina_20dd039936c649f2989d66c7d5be53d9M9DPc25QT6pJHs51PUw6v0pZCZGf"
	jinaEmbedURL  = "https://api.jina.ai/v1/embeddings"
	jinaRerankURL = "https://api.jina.ai/v1/rerank"
	dimension     = 1024
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
	httpReq, _ := http.NewRequest("POST", jinaEmbedURL, bytes.NewReader(body))
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
	fmt.Println("Complete System Test with Rerank Comparison")
	fmt.Println("===========================================\n")

	docDir := "/Volumes/SN770Coder/documents/本地AI撰写"

	// Recursively find all .md and .txt files
	var files []string
	filepath.Walk(docDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			ext := filepath.Ext(path)
			if ext == ".md" || ext == ".txt" {
				files = append(files, path)
			}
		}
		return nil
	})

	if len(files) > 10 {
		files = files[:10]
	}

	if len(files) == 0 {
		fmt.Println("No documents found, exiting")
		os.Exit(1)
	}

	fmt.Printf("Found %d documents\n\n", len(files))

	// Test 1: Without rerank
	fmt.Println("=== Test 1: Without Rerank ===")
	testWithoutRerank(files)

	fmt.Println("\n=== Test 2: With Rerank ===")
	testWithRerank(files)
}
