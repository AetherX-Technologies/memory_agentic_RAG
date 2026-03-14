package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

func main() {
	fmt.Println("\nAccuracy Test")
	fmt.Println("=============")

	config := store.Config{
		DBPath:    "accuracy_test.db",
		VectorDim: 128,
	}

	st, err := store.New(config)
	if err != nil {
		panic(err)
	}
	defer st.Close()

	// Create test dataset with known similarities
	testData := createTestDataset(st, 100, 128)

	// Test 1: Exact match should return highest score
	fmt.Println("\n## Test 1: Exact Match")
	testExactMatch(st, testData)

	// Test 2: Similar vectors should rank higher
	fmt.Println("\n## Test 2: Similarity Ranking")
	testSimilarityRanking(st, testData)

	// Test 3: Top-K accuracy
	fmt.Println("\n## Test 3: Top-K Accuracy")
	testTopKAccuracy(st, testData)
}

func createTestDataset(st store.Store, count int, dim int) []*store.Memory {
	rand.Seed(time.Now().UnixNano())
	memories := make([]*store.Memory, count)

	for i := 0; i < count; i++ {
		vector := make([]float32, dim)
		for j := 0; j < dim; j++ {
			vector[j] = rand.Float32()
		}

		m := &store.Memory{
			Text:       fmt.Sprintf("Memory %d", i),
			Scope:      "global",
			Importance: 0.5,
			Vector:     vector,
		}
		id, _ := st.Insert(m)
		m.ID = id
		memories[i] = m
	}
	return memories
}

func testExactMatch(st store.Store, data []*store.Memory) {
	query := data[0].Vector
	results, _ := st.VectorSearch(query, 5, nil)

	if len(results) > 0 && results[0].Entry.ID == data[0].ID {
		fmt.Printf("✅ Exact match found at rank 1 (score: %.4f)\n", results[0].Score)
	} else {
		fmt.Printf("❌ Exact match not at rank 1\n")
	}
}

func testSimilarityRanking(st store.Store, data []*store.Memory) {
	base := data[0].Vector
	similar := make([]float32, len(base))
	for i := range base {
		similar[i] = base[i] + rand.Float32()*0.1
	}

	results, _ := st.VectorSearch(similar, 10, nil)

	if len(results) > 0 && results[0].Entry.ID == data[0].ID {
		fmt.Printf("✅ Most similar vector ranked first (score: %.4f)\n", results[0].Score)
	} else {
		fmt.Printf("⚠️  Most similar vector at rank: %d\n", findRank(results, data[0].ID))
	}
}

func testTopKAccuracy(st store.Store, data []*store.Memory) {
	correct := 0
	total := 10

	for i := 0; i < total; i++ {
		results, _ := st.VectorSearch(data[i].Vector, 1, nil)
		if len(results) > 0 && results[0].Entry.ID == data[i].ID {
			correct++
		}
	}

	accuracy := float64(correct) / float64(total) * 100
	fmt.Printf("Top-1 Accuracy: %.1f%% (%d/%d)\n", accuracy, correct, total)
}

func findRank(results []store.SearchResult, id string) int {
	for i, r := range results {
		if r.Entry.ID == id {
			return i + 1
		}
	}
	return -1
}

