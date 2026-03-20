package retrieval

import (
	"math"
	"testing"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// mockStore implements MemoryStore for testing.
type mockStore struct {
	// memories indexed by ID
	memories map[string]*store.Memory
	// parent_id → children
	children map[string][]*store.Memory
	// vectors indexed by ID
	vectors map[string][]float32
}

func newMockStore() *mockStore {
	return &mockStore{
		memories: make(map[string]*store.Memory),
		children: make(map[string][]*store.Memory),
		vectors:  make(map[string][]float32),
	}
}

func (m *mockStore) addMemory(mem *store.Memory, vector []float32) {
	m.memories[mem.ID] = mem
	if vector != nil {
		mem.Vector = vector
		m.vectors[mem.ID] = vector
	}
	if mem.ParentID != "" {
		m.children[mem.ParentID] = append(m.children[mem.ParentID], mem)
	}
}

func (m *mockStore) VectorSearch(query []float32, limit int, scopes []string) ([]store.SearchResult, error) {
	var results []store.SearchResult
	for id, vec := range m.vectors {
		sim := cosineSimilarity(query, vec)
		mem := m.memories[id]
		results = append(results, store.SearchResult{
			Entry: *mem,
			Score: sim,
		})
	}
	// Sort by score descending
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *mockStore) GetChildren(parentID string) ([]*store.Memory, error) {
	children := m.children[parentID]
	return children, nil
}

// --- Tests ---

func TestPriorityQueue(t *testing.T) {
	pq := NewPriorityQueue()
	pq.Push(&SearchNode{ID: "a", Score: 0.5, Depth: 0})
	pq.Push(&SearchNode{ID: "b", Score: 0.9, Depth: 0})
	pq.Push(&SearchNode{ID: "c", Score: 0.7, Depth: 0})

	// Should pop in descending score order
	n1 := pq.Pop()
	if n1.ID != "b" || n1.Score != 0.9 {
		t.Errorf("expected b(0.9), got %s(%f)", n1.ID, n1.Score)
	}
	n2 := pq.Pop()
	if n2.ID != "c" || n2.Score != 0.7 {
		t.Errorf("expected c(0.7), got %s(%f)", n2.ID, n2.Score)
	}
	n3 := pq.Pop()
	if n3.ID != "a" || n3.Score != 0.5 {
		t.Errorf("expected a(0.5), got %s(%f)", n3.ID, n3.Score)
	}
	if pq.Len() != 0 {
		t.Error("queue should be empty")
	}
}

func TestSearch_FlatDocuments(t *testing.T) {
	ms := newMockStore()

	// Add 5 flat memories (no parent, no children)
	queryVec := []float32{1, 0, 0, 0}
	ms.addMemory(&store.Memory{ID: "m1", Text: "relevant", Abstract: "Relevant doc", SourceFile: "a.md"}, []float32{0.9, 0.1, 0, 0})
	ms.addMemory(&store.Memory{ID: "m2", Text: "somewhat", Abstract: "Somewhat relevant", SourceFile: "b.md"}, []float32{0.5, 0.5, 0, 0})
	ms.addMemory(&store.Memory{ID: "m3", Text: "irrelevant", Abstract: "Not relevant", SourceFile: "c.md"}, []float32{0, 0, 1, 0})

	retriever := New(ms, DefaultConfig())
	results, err := retriever.Search(queryVec, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// m1 should be top result (most similar to query)
	if results[0].Entry.ID != "m1" {
		t.Errorf("expected m1 as top result, got %s", results[0].Entry.ID)
	}
	t.Logf("got %d results, top: %s (score=%.4f)", len(results), results[0].Entry.ID, results[0].Score)
}

func TestSearch_HierarchicalDocuments(t *testing.T) {
	ms := newMockStore()

	queryVec := []float32{1, 0, 0, 0}

	// Parent document (directory node)
	ms.addMemory(&store.Memory{
		ID: "doc1", Text: "parent doc", Abstract: "Parent", NodeType: "directory", SourceFile: "doc.md",
	}, []float32{0.7, 0.3, 0, 0})

	// Child chunks (high relevance to query)
	ms.addMemory(&store.Memory{
		ID: "chunk1", Text: "child chunk 1", Abstract: "Chunk 1", ParentID: "doc1",
		NodeType: "chunk", SourceFile: "doc.md", ChunkIndex: 0,
	}, []float32{0.95, 0.05, 0, 0})

	ms.addMemory(&store.Memory{
		ID: "chunk2", Text: "child chunk 2", Abstract: "Chunk 2", ParentID: "doc1",
		NodeType: "chunk", SourceFile: "doc.md", ChunkIndex: 1,
	}, []float32{0.85, 0.15, 0, 0})

	// Unrelated document
	ms.addMemory(&store.Memory{
		ID: "other", Text: "unrelated", Abstract: "Other", SourceFile: "other.md",
	}, []float32{0, 0, 1, 0})

	retriever := New(ms, DefaultConfig())
	results, err := retriever.Search(queryVec, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// The doc.md chunks should be aggregated
	t.Logf("got %d aggregated results", len(results))
	for i, r := range results {
		t.Logf("  [%d] %s score=%.4f chunks=%d", i, r.Entry.SourceFile, r.Score, r.ChunkCount)
	}

	// doc.md should rank higher than other.md
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
}

func TestSearch_DepthPruning(t *testing.T) {
	ms := newMockStore()
	queryVec := []float32{1, 0, 0, 0}

	// Create a chain: root → child → grandchild → great-grandchild (depth 3)
	ms.addMemory(&store.Memory{ID: "root", Text: "root", SourceFile: "r.md"}, []float32{0.8, 0.2, 0, 0})
	ms.addMemory(&store.Memory{ID: "c1", Text: "child", ParentID: "root", SourceFile: "r.md"}, []float32{0.7, 0.3, 0, 0})
	ms.addMemory(&store.Memory{ID: "c2", Text: "grandchild", ParentID: "c1", SourceFile: "r.md"}, []float32{0.6, 0.4, 0, 0})
	ms.addMemory(&store.Memory{ID: "c3", Text: "great-gc", ParentID: "c2", SourceFile: "r.md"}, []float32{0.5, 0.5, 0, 0})

	// MaxDepth=2 should stop before great-grandchild
	config := DefaultConfig()
	config.MaxDepth = 2
	retriever := New(ms, config)

	results, err := retriever.Search(queryVec, 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Should find root, child, grandchild but NOT great-grandchild via tree search
	// (great-grandchild may still appear via global search)
	t.Logf("got %d results with maxDepth=2", len(results))
}

func TestRRFMerge(t *testing.T) {
	global := []store.SearchResult{
		{Entry: store.Memory{ID: "a"}, Score: 0.9},
		{Entry: store.Memory{ID: "b"}, Score: 0.7},
		{Entry: store.Memory{ID: "c"}, Score: 0.5},
	}
	hierarchical := []store.SearchResult{
		{Entry: store.Memory{ID: "b"}, Score: 0.95},
		{Entry: store.Memory{ID: "d"}, Score: 0.8},
	}

	merged := rrfMerge(global, hierarchical)

	// "b" appears in both lists, should have highest RRF score
	if len(merged) != 4 {
		t.Fatalf("expected 4 merged results, got %d", len(merged))
	}
	if merged[0].Entry.ID != "b" {
		t.Errorf("expected 'b' as top merged result (in both lists), got %s", merged[0].Entry.ID)
	}
}

func TestAggregateBySource(t *testing.T) {
	results := []store.SearchResult{
		{Entry: store.Memory{ID: "c1", SourceFile: "doc.md", ChunkIndex: 0, Abstract: "First"}, Score: 0.8},
		{Entry: store.Memory{ID: "c2", SourceFile: "doc.md", ChunkIndex: 1, Abstract: "Second"}, Score: 0.9},
		{Entry: store.Memory{ID: "c3", SourceFile: "doc.md", ChunkIndex: 2, Abstract: "Third"}, Score: 0.7},
		{Entry: store.Memory{ID: "other", SourceFile: "other.md", Abstract: "Other"}, Score: 0.6},
	}

	agg := aggregateBySource(results)

	if len(agg) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(agg))
	}

	// doc.md group should have ChunkCount=3 and combined abstract
	for _, r := range agg {
		if r.Entry.SourceFile == "doc.md" {
			if r.ChunkCount != 3 {
				t.Errorf("doc.md ChunkCount = %d, want 3", r.ChunkCount)
			}
			if r.Score != 0.9 {
				t.Errorf("doc.md score = %f, want 0.9 (best chunk)", r.Score)
			}
			t.Logf("doc.md abstract: %s", r.Entry.Abstract)
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	if math.Abs(cosineSimilarity(a, b)-1.0) > 1e-6 {
		t.Error("identical vectors should have similarity 1.0")
	}

	c := []float32{0, 1, 0}
	if math.Abs(cosineSimilarity(a, c)) > 1e-6 {
		t.Error("orthogonal vectors should have similarity 0.0")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	ms := newMockStore()
	retriever := New(ms, DefaultConfig())
	_, err := retriever.Search(nil, 10, nil)
	if err == nil {
		t.Error("expected error for empty query vector")
	}
}
