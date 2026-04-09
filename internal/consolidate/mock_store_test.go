package consolidate

import "github.com/yourusername/hybridmem-rag/internal/store"

type mockMem struct {
	id, text, memType string
}

type mockConsolidateStore struct {
	unconsolidated     []*mockMem
	consolidated       bool
	consolidationCount int
	connections        []string
}

func (m *mockConsolidateStore) ListUnconsolidated(limit int) ([]*store.Memory, error) {
	var result []*store.Memory
	for _, mm := range m.unconsolidated {
		result = append(result, &store.Memory{ID: mm.id, Text: mm.text, MemoryType: mm.memType, Importance: 0.7})
	}
	return result, nil
}

func (m *mockConsolidateStore) CountUnconsolidated() (int64, error) {
	return int64(len(m.unconsolidated)), nil
}

func (m *mockConsolidateStore) MarkConsolidated(ids []string) error {
	m.consolidated = true
	return nil
}

func (m *mockConsolidateStore) CreateConsolidation(c *store.Consolidation) (string, error) {
	m.consolidationCount++
	return c.ID, nil
}

func (m *mockConsolidateStore) ListConsolidations(limit int) ([]*store.Consolidation, error) {
	return nil, nil
}

func (m *mockConsolidateStore) AddConnection(memoryID, linkedTo, relationship string) error {
	m.connections = append(m.connections, memoryID+"→"+linkedTo)
	return nil
}

// Remaining Store interface methods (unused in consolidation tests)
func (m *mockConsolidateStore) Insert(mem *store.Memory) (string, error) { return "id", nil }
func (m *mockConsolidateStore) Get(id string) (*store.Memory, error)     { return nil, nil }
func (m *mockConsolidateStore) Delete(id string) error                   { return nil }
func (m *mockConsolidateStore) List(scope string, limit int) ([]*store.Memory, error) { return nil, nil }
func (m *mockConsolidateStore) Search(qv []float32, qt string, cp string, limit int, scopes []string) ([]store.SearchResult, error) { return nil, nil }
func (m *mockConsolidateStore) VectorSearch(q []float32, limit int, scopes []string) ([]store.SearchResult, error) { return nil, nil }
func (m *mockConsolidateStore) HybridSearch(qv []float32, qt string, limit int, scopes []string) ([]store.SearchResult, error) { return nil, nil }
func (m *mockConsolidateStore) HierarchicalHybridSearch(qv []float32, qt string, cp string, limit int, scopes []string) ([]store.SearchResult, error) { return nil, nil }
func (m *mockConsolidateStore) GetChildren(parentID string) ([]*store.Memory, error) { return nil, nil }
func (m *mockConsolidateStore) HasChildren(id string) (bool, error)                  { return false, nil }
func (m *mockConsolidateStore) GetContent(id string) (string, error)                 { return "", nil }
func (m *mockConsolidateStore) UpdateConfidence(id string, delta float64) error       { return nil }
func (m *mockConsolidateStore) UpdateImportance(id string, importance float64) error  { return nil }
func (m *mockConsolidateStore) RecordSupersession(oldID, newID string) error          { return nil }
func (m *mockConsolidateStore) SoftDelete(id string, now int64) error                 { return nil }
func (m *mockConsolidateStore) Restore(id string) error                               { return nil }
func (m *mockConsolidateStore) ListTrash(limit int) ([]*store.Memory, error)          { return nil, nil }
func (m *mockConsolidateStore) PermanentDelete(id string) error                       { return nil }
func (m *mockConsolidateStore) RunCleanup(now int64) error                            { return nil }
func (m *mockConsolidateStore) SetTags(memoryID string, tags []string) error          { return nil }
func (m *mockConsolidateStore) GetTags(memoryID string) ([]string, error)             { return nil, nil }
func (m *mockConsolidateStore) GetMemoryIDsByTag(tag string) ([]string, error)        { return nil, nil }
func (m *mockConsolidateStore) Close() error                                          { return nil }
