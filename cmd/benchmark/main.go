package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

func generateTestData(count int, dim int) []*store.Memory {
	rand.Seed(time.Now().UnixNano())
	memories := make([]*store.Memory, count)

	categories := []string{"fact", "preference", "insight", "conversation"}
	scopes := []string{"global", "project", "session"}

	for i := 0; i < count; i++ {
		vector := make([]float32, dim)
		for j := 0; j < dim; j++ {
			vector[j] = rand.Float32()
		}

		memories[i] = &store.Memory{
			Text:       fmt.Sprintf("Test memory %d with some content", i),
			Category:   categories[i%len(categories)],
			Scope:      scopes[i%len(scopes)],
			Importance: rand.Float64(),
			Vector:     vector,
			Timestamp:  time.Now().Unix() - int64(rand.Intn(86400*30)),
		}
	}
	return memories
}

func main() {
	fmt.Println("HybridMem-RAG Benchmark")
	fmt.Println("======================")

	config := store.Config{
		DBPath:    "benchmark.db",
		VectorDim: 1536,
	}

	st, err := store.New(config)
	if err != nil {
		panic(err)
	}
	defer st.Close()

	testSizes := []int{100, 1000, 5000, 10000}

	for _, size := range testSizes {
		fmt.Printf("\n## Testing with %d memories\n", size)
		runBenchmark(st, size, config.VectorDim)
	}
}

func runBenchmark(st store.Store, count int, dim int) {
	memories := generateTestData(count, dim)

	// Insert benchmark
	start := time.Now()
	for _, m := range memories {
		if _, err := st.Insert(m); err != nil {
			panic(err)
		}
	}
	insertDuration := time.Since(start)
	fmt.Printf("Insert: %d memories in %v (%.2f ms/op)\n",
		count, insertDuration, float64(insertDuration.Milliseconds())/float64(count))

	// Vector search benchmark
	queryVector := memories[0].Vector
	start = time.Now()
	results, err := st.VectorSearch(queryVector, 10, nil)
	if err != nil {
		panic(err)
	}
	searchDuration := time.Since(start)
	fmt.Printf("Vector Search: %v (%d results)\n", searchDuration, len(results))

	// Cleanup
	for _, m := range memories {
		st.Delete(m.ID)
	}
}

