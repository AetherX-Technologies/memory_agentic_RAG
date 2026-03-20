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
	memories map[string]*store.Memory
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
func (m *mockStore) Delete(id string) error                   { delete(m.memories, id); return nil }
func (m *mockStore) List(scope string, limit int) ([]*store.Memory, error) { return nil, nil }
func (m *mockStore) Search(qv []float32, qt string, cp string, limit int, scopes []string) ([]store.SearchResult, error) {
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
func (m *mockStore) Close() error { return nil }

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
