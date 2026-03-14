package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

type TestDataset struct {
	Dimension int `json:"dimension"`
	Memories  []struct {
		ID         string    `json:"id"`
		Text       string    `json:"text"`
		Vector     []float32 `json:"vector"`
		Category   string    `json:"category"`
		Scope      string    `json:"scope"`
		Importance float64   `json:"importance"`
	} `json:"memories"`
	Queries []struct {
		Name        string    `json:"name"`
		Vector      []float32 `json:"vector"`
		ExpectedTop string    `json:"expected_top1"`
	} `json:"queries"`
}

func main() {
	data, _ := os.ReadFile("test_data/comparison_dataset.json")
	var dataset TestDataset
	json.Unmarshal(data, &dataset)

	st, _ := store.New(store.Config{DBPath: ":memory:", VectorDim: dataset.Dimension})
	defer st.Close()

	// Insert memories
	for _, m := range dataset.Memories {
		st.Insert(&store.Memory{
			ID:         m.ID,
			Text:       m.Text,
			Vector:     m.Vector,
			Category:   m.Category,
			Scope:      m.Scope,
			Importance: m.Importance,
		})
	}

	// Run queries
	fmt.Println("Go Implementation Results:")
	for _, q := range dataset.Queries {
		results, _ := st.VectorSearch(q.Vector, 3, nil)
		fmt.Printf("\n%s:\n", q.Name)
		for i, r := range results {
			match := ""
			if i == 0 && r.Entry.ID == q.ExpectedTop {
				match = " ✅"
			}
			fmt.Printf("  %d. %s (score: %.4f)%s\n", i+1, r.Entry.ID, r.Score, match)
		}
	}
}
