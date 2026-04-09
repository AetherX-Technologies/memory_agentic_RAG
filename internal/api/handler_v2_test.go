package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// mockStore implements store.Store for testing without FTS5 dependency.
type mockStore struct {
	memories         map[string]*store.Memory
	lastQueryText    string
	lastQueryVector  []float32
	lastCurrentPath  string
	lastSearchScopes []string
}

func newMockStore() *mockStore {
	return &mockStore{memories: make(map[string]*store.Memory)}
}

func (m *mockStore) Insert(mem *store.Memory) (string, error) {
	if mem.ID == "" {
		mem.ID = "mock-id"
	}
	m.memories[mem.ID] = mem
	return mem.ID, nil
}
func (m *mockStore) Get(id string) (*store.Memory, error) {
	mem, ok := m.memories[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return mem, nil
}
func (m *mockStore) Delete(id string) error { delete(m.memories, id); return nil }
func (m *mockStore) List(scope string, limit int) ([]*store.Memory, error) {
	var list []*store.Memory
	for _, mem := range m.memories {
		if mem.Scope == scope {
			list = append(list, mem)
		}
		if len(list) >= limit {
			break
		}
	}
	return list, nil
}
func (m *mockStore) Search(qv []float32, qt string, cp string, limit int, scopes []string) ([]store.SearchResult, error) {
	m.lastQueryText = qt
	m.lastCurrentPath = cp
	m.lastQueryVector = append([]float32(nil), qv...)
	m.lastSearchScopes = append([]string(nil), scopes...)
	var results []store.SearchResult
	for _, mem := range m.memories {
		results = append(results, store.SearchResult{Entry: *mem, Score: 0.5})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}
func (m *mockStore) VectorSearch(q []float32, limit int, scopes []string) ([]store.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) HybridSearch(qv []float32, qt string, limit int, scopes []string) ([]store.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) HierarchicalHybridSearch(qv []float32, qt string, cp string, limit int, scopes []string) ([]store.SearchResult, error) {
	return nil, nil
}
func (m *mockStore) GetChildren(parentID string) ([]*store.Memory, error) { return nil, nil }
func (m *mockStore) HasChildren(id string) (bool, error)                  { return false, nil }
func (m *mockStore) GetContent(id string) (string, error) {
	mem, ok := m.memories[id]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return mem.Text, nil
}
func (m *mockStore) Close() error                                    { return nil }
func (m *mockStore) UpdateConfidence(id string, delta float64) error    { return nil }
func (m *mockStore) UpdateImportance(id string, importance float64) error { return nil }
func (m *mockStore) RecordSupersession(oldID, newID string) error      { return nil }
func (m *mockStore) SoftDelete(id string, now int64) error            { return nil }
func (m *mockStore) Restore(id string) error                          { return nil }
func (m *mockStore) ListTrash(limit int) ([]*store.Memory, error)     { return nil, nil }
func (m *mockStore) PermanentDelete(id string) error                  { return nil }
func (m *mockStore) RunCleanup(now int64) error                                 { return nil }
func (m *mockStore) SetTags(memoryID string, tags []string) error                    { return nil }
func (m *mockStore) GetTags(memoryID string) ([]string, error)                       { return nil, nil }
func (m *mockStore) GetMemoryIDsByTag(tag string) ([]string, error)                  { return nil, nil }
func (m *mockStore) ListUnconsolidated(limit int) ([]*store.Memory, error)           { return nil, nil }
func (m *mockStore) CountUnconsolidated() (int64, error)                             { return 0, nil }
func (m *mockStore) MarkConsolidated(ids []string) error                             { return nil }
func (m *mockStore) CreateConsolidation(c *store.Consolidation) (string, error)      { return "c1", nil }
func (m *mockStore) ListConsolidations(limit int) ([]*store.Consolidation, error)    { return nil, nil }
func (m *mockStore) AddConnection(memoryID, linkedTo, relationship string) error     { return nil }

type mockEmbedder struct {
	vector []float32
}

func (m *mockEmbedder) Embed(text string) ([]float32, error) {
	return append([]float32(nil), m.vector...), nil
}

func (m *mockEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = append([]float32(nil), m.vector...)
	}
	return out, nil
}

// --- Tests ---

func TestSearchV1(t *testing.T) {
	ms := newMockStore()
	ms.Insert(&store.Memory{
		ID: "mem1", Text: "Full content here", Abstract: "Short abstract",
		Category: "test", Scope: "global", Importance: 0.5,
	})

	handler := NewHandler(ms)
	req := httptest.NewRequest(http.MethodGet, "/api/memories/search?q=test", nil)
	// No X-API-Version header → defaults to v1
	w := httptest.NewRecorder()

	handler.SearchMemories(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// v1 returns raw SearchResult array
	var results []store.SearchResult
	json.NewDecoder(w.Body).Decode(&results)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// v1 includes full text
	if results[0].Entry.Text == "" {
		t.Error("v1 should include full text")
	}
}

func TestSearchV2(t *testing.T) {
	ms := newMockStore()
	ms.Insert(&store.Memory{
		ID: "mem1", Text: "Full content here", Abstract: "Short abstract",
		Overview: "Structured overview", SourceFile: "doc.md",
		Category: "test", Scope: "global", Importance: 0.5,
	})

	handler := NewHandler(ms)
	req := httptest.NewRequest(http.MethodGet, "/api/memories/search?q=test", nil)
	req.Header.Set("X-API-Version", "v2")
	w := httptest.NewRecorder()

	handler.SearchMemories(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Version string `json:"version"`
		Results []struct {
			ID         string  `json:"id"`
			Abstract   string  `json:"abstract"`
			Score      float64 `json:"score"`
			ContentURL string  `json:"content_url"`
		} `json:"results"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Version != "v2" {
		t.Errorf("expected version v2, got %q", resp.Version)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected results")
	}
	if resp.Results[0].ContentURL == "" {
		t.Error("v2 should include content_url")
	}
	if resp.Results[0].Abstract == "" {
		t.Error("v2 should include abstract")
	}
	t.Logf("v2 result: id=%s abstract=%s url=%s", resp.Results[0].ID, resp.Results[0].Abstract, resp.Results[0].ContentURL)
}

func TestLegacySearchUsesSharedRecallOptions(t *testing.T) {
	ms := newMockStore()
	ms.Insert(&store.Memory{
		ID:         "mem1",
		Text:       "用户正在使用浏览器",
		Category:   "memory",
		Scope:      "global",
		Importance: 0.5,
	})

	handler := NewHandlerWithDeps(ms, &mockEmbedder{vector: []float32{0.1, 0.2}}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/memories/search?q=浏览器&current_path=/project/src&scope=global,browser:chatgpt", nil)
	w := httptest.NewRecorder()

	handler.SearchMemories(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ms.lastQueryText != "浏览器" {
		t.Fatalf("expected raw query to reach store search, got %q", ms.lastQueryText)
	}
	if len(ms.lastQueryVector) != 2 {
		t.Fatalf("expected embedder vector to be used, got %v", ms.lastQueryVector)
	}
	if ms.lastCurrentPath != "/project/src" {
		t.Fatalf("expected current_path to be preserved, got %q", ms.lastCurrentPath)
	}
	if len(ms.lastSearchScopes) != 2 || ms.lastSearchScopes[0] != "global" || ms.lastSearchScopes[1] != "browser:chatgpt" {
		t.Fatalf("expected scopes to be preserved, got %v", ms.lastSearchScopes)
	}
}

func TestLegacySearchDoesNotAdaptiveSkip(t *testing.T) {
	ms := newMockStore()
	ms.Insert(&store.Memory{
		ID:         "mem1",
		Text:       "hello memory",
		Category:   "memory",
		Scope:      "global",
		Importance: 0.5,
	})

	handler := NewHandler(ms)
	req := httptest.NewRequest(http.MethodGet, "/api/memories/search?q=hi", nil)
	w := httptest.NewRecorder()

	handler.SearchMemories(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var results []store.SearchResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected legacy search to query store even for adaptive-skip phrases")
	}
	if ms.lastQueryText != "hi" {
		t.Fatalf("expected legacy query to be passed through, got %q", ms.lastQueryText)
	}
}

func TestGetMemoryContent(t *testing.T) {
	ms := newMockStore()
	ms.Insert(&store.Memory{ID: "doc123", Text: "This is the full document content."})

	handler := NewHandler(ms)

	// Test via ServeHTTP to verify routing
	req := httptest.NewRequest(http.MethodGet, "/api/memories/doc123/content", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.ID != "doc123" {
		t.Errorf("expected id=doc123, got %s", resp.ID)
	}
	if resp.Content != "This is the full document content." {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestGetMemoryContent_NotFound(t *testing.T) {
	ms := newMockStore()
	handler := NewHandler(ms)

	req := httptest.NewRequest(http.MethodGet, "/api/memories/nonexistent/content", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestRoutingContentEndpoint(t *testing.T) {
	ms := newMockStore()
	ms.Insert(&store.Memory{ID: "abc", Text: "content"})
	handler := NewHandler(ms)

	// Should route to content endpoint, not delete
	req := httptest.NewRequest(http.MethodGet, "/api/memories/abc/content", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/memories/abc/content should return 200, got %d", w.Code)
	}
}
