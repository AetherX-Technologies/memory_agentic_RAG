package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
)

type Memory struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	Vector     []float32 `json:"vector"`
	Category   string    `json:"category"`
	Scope      string    `json:"scope"`
	Importance float64   `json:"importance"`
}

type Query struct {
	Name        string    `json:"name"`
	Vector      []float32 `json:"vector"`
	ExpectedTop string    `json:"expected_top1"`
}

func generateVector(dim int, seed int64) []float32 {
	rand.Seed(seed)
	v := make([]float32, dim)
	for i := 0; i < dim; i++ {
		v[i] = rand.Float32()
	}
	return v
}

func main() {
	dim := 128

	dataset := map[string]interface{}{
		"dimension": dim,
		"memories": []Memory{
			{
				ID:         "mem_001",
				Text:       "Python is a high-level programming language",
				Vector:     generateVector(dim, 1),
				Category:   "fact",
				Scope:      "global",
				Importance: 0.8,
			},
			{
				ID:         "mem_002",
				Text:       "JavaScript is used for web development",
				Vector:     generateVector(dim, 2),
				Category:   "fact",
				Scope:      "global",
				Importance: 0.7,
			},
			{
				ID:         "mem_003",
				Text:       "Go is a compiled programming language",
				Vector:     generateVector(dim, 3),
				Category:   "fact",
				Scope:      "global",
				Importance: 0.75,
			},
		},
		"queries": []Query{
			{
				Name:        "query_python",
				Vector:      generateVector(dim, 1),
				ExpectedTop: "mem_001",
			},
			{
				Name:        "query_javascript",
				Vector:      generateVector(dim, 2),
				ExpectedTop: "mem_002",
			},
		},
	}

	data, _ := json.MarshalIndent(dataset, "", "  ")
	os.WriteFile("test_data/comparison_dataset.json", data, 0644)
	fmt.Println("Test dataset generated")
}
