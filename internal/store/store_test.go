package store

import (
	"os"
	"testing"
)

func TestStore(t *testing.T) {
	// 创建临时数据库
	dbPath := "test_memory.db"
	t.Cleanup(func() { os.Remove(dbPath) })

	store, err := New(Config{DBPath: dbPath, VectorDim: 1024})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// 测试插入
	memory := &Memory{
		Text:       "用户喜欢 Go 语言",
		Vector:     make([]float32, 1024),
		Category:   "preference",
		Scope:      "global",
		Importance: 0.8,
		Metadata:   "{}",
	}

	id, err := store.Insert(memory)
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}
	if id == "" {
		t.Fatal("ID should not be empty")
	}

	// 测试读取
	retrieved, err := store.Get(id)
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if retrieved.Text != memory.Text {
		t.Errorf("Expected text %s, got %s", memory.Text, retrieved.Text)
	}

	// 测试列表
	list, err := store.List("global", 10)
	if err != nil {
		t.Fatalf("Failed to list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("Expected 1 memory, got %d", len(list))
	}

	// 测试删除
	if err := store.Delete(id); err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}
}

func TestVectorSearch(t *testing.T) {
	dbPath := "test_vector.db"
	t.Cleanup(func() { os.Remove(dbPath) })

	store, err := New(Config{DBPath: dbPath, VectorDim: 3})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	// 插入测试数据
	vectors := [][]float32{
		{1.0, 0.0, 0.0},
		{0.0, 1.0, 0.0},
		{0.7, 0.7, 0.0},
	}

	for i, vec := range vectors {
		_, err := store.Insert(&Memory{
			Text:       "test",
			Vector:     vec,
			Category:   "fact",
			Scope:      "global",
			Importance: 0.5,
		})
		if err != nil {
			t.Fatalf("Failed to insert %d: %v", i, err)
		}
	}

	// 搜索
	query := []float32{1.0, 0.0, 0.0}
	results, err := store.VectorSearch(query, 2, []string{"global"})
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// 第一个结果应该是最相似的
	if results[0].Score < 0.9 {
		t.Errorf("Expected high similarity, got %f", results[0].Score)
	}
}
