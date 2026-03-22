package mcp

import "github.com/yourusername/hybridmem-rag/internal/store"

// minimalMockStore implements store.Store for MCP tests.
type minimalMockStore struct{}

func newMinimalMockStore() *minimalMockStore { return &minimalMockStore{} }

func (m *minimalMockStore) Insert(mem *store.Memory) (string, error) { return "mock-id-1", nil }
func (m *minimalMockStore) Get(id string) (*store.Memory, error) {
	return &store.Memory{ID: id, Text: "test", Category: "memory", Scope: "global", MemoryType: "fact", Importance: 0.7, Confidence: 0.5}, nil
}
func (m *minimalMockStore) Delete(id string) error                   { return nil }
func (m *minimalMockStore) List(scope string, limit int) ([]*store.Memory, error) { return nil, nil }
func (m *minimalMockStore) Search(qv []float32, qt string, cp string, limit int, scopes []string) ([]store.SearchResult, error) {
	return nil, nil
}
func (m *minimalMockStore) VectorSearch(q []float32, limit int, scopes []string) ([]store.SearchResult, error) {
	return nil, nil
}
func (m *minimalMockStore) HybridSearch(qv []float32, qt string, limit int, scopes []string) ([]store.SearchResult, error) {
	return nil, nil
}
func (m *minimalMockStore) HierarchicalHybridSearch(qv []float32, qt string, cp string, limit int, scopes []string) ([]store.SearchResult, error) {
	return nil, nil
}
func (m *minimalMockStore) GetChildren(parentID string) ([]*store.Memory, error) { return nil, nil }
func (m *minimalMockStore) HasChildren(id string) (bool, error)                  { return false, nil }
func (m *minimalMockStore) GetContent(id string) (string, error)                 { return "test", nil }
func (m *minimalMockStore) UpdateConfidence(id string, delta float64) error         { return nil }
func (m *minimalMockStore) UpdateImportance(id string, importance float64) error   { return nil }
func (m *minimalMockStore) RecordSupersession(oldID, newID string) error           { return nil }
func (m *minimalMockStore) SoftDelete(id string, now int64) error                 { return nil }
func (m *minimalMockStore) Restore(id string) error                               { return nil }
func (m *minimalMockStore) ListTrash(limit int) ([]*store.Memory, error)          { return nil, nil }
func (m *minimalMockStore) PermanentDelete(id string) error                       { return nil }
func (m *minimalMockStore) RunCleanup(now int64) error                            { return nil }
func (m *minimalMockStore) SetTags(memoryID string, tags []string) error                   { return nil }
func (m *minimalMockStore) GetMemoryIDsByTag(tag string) ([]string, error)                 { return nil, nil }
func (m *minimalMockStore) ListUnconsolidated(limit int) ([]*store.Memory, error)          { return nil, nil }
func (m *minimalMockStore) CountUnconsolidated() (int64, error)                            { return 0, nil }
func (m *minimalMockStore) MarkConsolidated(ids []string) error                            { return nil }
func (m *minimalMockStore) CreateConsolidation(c *store.Consolidation) (string, error)     { return "c1", nil }
func (m *minimalMockStore) ListConsolidations(limit int) ([]*store.Consolidation, error)   { return nil, nil }
func (m *minimalMockStore) AddConnection(memoryID, linkedTo, relationship string) error    { return nil }
func (m *minimalMockStore) Close() error                                                   { return nil }
