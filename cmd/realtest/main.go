package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

const (
	jinaAPIKey string
	jinaURL    = "https://api.jina.ai/v1/embeddings"
	jinaModel  = "jina-embeddings-v3"
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

func getEmbedding(text string, task string) ([]float32, error) {
	req := EmbedRequest{
		Model:      jinaModel,
		Input:      []string{text},
		Task:       task,
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
	return nil, fmt.Errorf("no embedding returned")
}

func main() {
	jinaAPIKey = os.Getenv("JINA_API_KEY")
	if jinaAPIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: JINA_API_KEY environment variable not set")
		os.Exit(1)
	}

	fmt.Println("Real-world Comparison Test with Jina API")
	fmt.Println("=========================================\n")

	st, _ := store.New(store.Config{DBPath: ":memory:", VectorDim: dimension})
	defer st.Close()

	testTexts := []string{
		"Python is a high-level programming language",
		"JavaScript is used for web development",
		"Go is a compiled programming language",
	}

	fmt.Println("Step 1: Generating embeddings...")
	memories := make([]*store.Memory, len(testTexts))
	for i, text := range testTexts {
		vec, err := getEmbedding(text, "retrieval.passage")
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		memories[i] = &store.Memory{
			Text:       text,
			Vector:     vec,
			Scope:      "global",
			Importance: 0.8,
		}
		fmt.Printf("  ✓ %s\n", text)
	}

	fmt.Println("\nStep 2: Inserting into database...")
	for _, m := range memories {
		st.Insert(m)
	}

	fmt.Println("\nStep 3: Testing retrieval...")
	queryText := "programming language"
	queryVec, _ := getEmbedding(queryText, "retrieval.query")

	results, _ := st.VectorSearch(queryVec, 3, nil)
	fmt.Printf("\nQuery: \"%s\"\n", queryText)
	fmt.Println("Results:")
	for i, r := range results {
		fmt.Printf("  %d. %s (score: %.4f)\n", i+1, r.Entry.Text, r.Score)
	}
}
