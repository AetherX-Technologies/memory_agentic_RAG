package store

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// BenchmarkInsertWithOpenViking measures insert performance with new OpenViking fields.
func BenchmarkInsertWithOpenViking(b *testing.B) {
	config := Config{DBPath: ":memory:", VectorDim: 768}
	st, err := New(config)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mem := &Memory{
			Text:       fmt.Sprintf("Memory content %d with some substantial text for testing.", i),
			Abstract:   fmt.Sprintf("Abstract %d", i),
			Overview:   fmt.Sprintf("Overview of memory %d with structured content.", i),
			Category:   "benchmark",
			Scope:      "global",
			Importance: 0.7,
			NodeType:   "chunk",
			SourceFile: "/docs/bench.md",
			ChunkIndex: i % 10,
			TokenCount: 50,
			ParentID:   "parent-1",
			HierarchyPath: fmt.Sprintf("/docs/bench/chunk_%d", i),
		}
		if _, err := st.Insert(mem); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetChildrenWithVectors measures GetChildren (LEFT JOIN vectors) performance.
func BenchmarkGetChildrenWithVectors(b *testing.B) {
	config := Config{DBPath: ":memory:", VectorDim: 8}
	st, err := New(config)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	// Insert parent + 50 children with vectors
	parentMem := &Memory{
		Text: "parent", Category: "test", Scope: "global", Importance: 0.5,
		NodeType: "directory", SourceFile: "/test.md",
	}
	parentID, err := st.Insert(parentMem)
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < 50; i++ {
		vec := make([]float32, 8)
		for j := range vec {
			vec[j] = rand.Float32()
		}
		child := &Memory{
			Text:       fmt.Sprintf("Child content %d", i),
			Abstract:   fmt.Sprintf("Abstract %d", i),
			Category:   "test",
			Scope:      "global",
			Importance: 0.5,
			ParentID:   parentID,
			NodeType:   "chunk",
			SourceFile: "/test.md",
			ChunkIndex: i,
			Vector:     vec,
		}
		if _, err := st.Insert(child); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		children, err := st.GetChildren(parentID)
		if err != nil {
			b.Fatal(err)
		}
		if len(children) != 50 {
			b.Fatalf("expected 50 children, got %d", len(children))
		}
		// Verify vectors loaded
		if len(children[0].Vector) == 0 {
			b.Fatal("vector not loaded")
		}
	}
}

// BenchmarkVectorSearchWithOpenViking measures vector search with all new fields populated.
func BenchmarkVectorSearchWithOpenViking(b *testing.B) {
	for _, n := range []int{1000, 5000, 10000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			config := Config{DBPath: ":memory:", VectorDim: 768}
			st, err := New(config)
			if err != nil {
				b.Fatal(err)
			}
			defer st.Close()

			// Populate
			rng := rand.New(rand.NewSource(42))
			for i := 0; i < n; i++ {
				vec := make([]float32, 768)
				for j := range vec {
					vec[j] = rng.Float32()*2 - 1
				}
				NormalizeVector(vec)
				mem := &Memory{
					Text:       fmt.Sprintf("Document %d content for benchmarking with substantial text.", i),
					Abstract:   fmt.Sprintf("Abstract of document %d.", i),
					Overview:   fmt.Sprintf("Overview: document %d covers topic X with details.", i),
					Category:   "bench",
					Scope:      "global",
					Importance: 0.5 + rng.Float64()*0.5,
					Timestamp:  time.Now().Unix() - int64(rng.Intn(86400*30)),
					NodeType:   "chunk",
					SourceFile: fmt.Sprintf("/docs/file_%d.md", i/10),
					ChunkIndex: i % 10,
					TokenCount: 100 + rng.Intn(400),
					Vector:     vec,
				}
				if _, err := st.Insert(mem); err != nil {
					b.Fatal(err)
				}
			}

			// Query vector
			queryVec := make([]float32, 768)
			for j := range queryVec {
				queryVec[j] = rng.Float32()*2 - 1
			}
			NormalizeVector(queryVec)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				results, err := st.VectorSearch(queryVec, 10, nil)
				if err != nil {
					b.Fatal(err)
				}
				if len(results) == 0 {
					b.Fatal("no results")
				}
			}
		})
	}
}
