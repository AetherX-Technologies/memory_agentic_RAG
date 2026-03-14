package store

import (
	"os"
	"testing"
)

func TestHybridSearch(t *testing.T) {
	dbPath := "test_hybrid.db"
	t.Cleanup(func() { os.Remove(dbPath) })

	s, err := New(Config{DBPath: dbPath, VectorDim: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	store := s.(*sqliteStore)

	// 插入测试数据
	memories := []struct {
		text   string
		vector []float32
	}{
		{"Go 语言很强大", []float32{1.0, 0.0, 0.0}},
		{"Python 适合数据分析", []float32{0.0, 1.0, 0.0}},
		{"Go 语言性能优秀", []float32{0.9, 0.1, 0.0}},
	}

	for _, m := range memories {
		_, err := store.Insert(&Memory{
			Text:       m.text,
			Vector:     m.vector,
			Category:   "fact",
			Scope:      "global",
			Importance: 0.5,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// 测试混合检索
	query := []float32{1.0, 0.0, 0.0}
	results, err := store.HybridSearch(query, "Go 语言", 2, []string{"global"})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Error("Expected results")
	}

	// 第一个结果应该包含 "Go 语言"
	if len(results) > 0 && results[0].Entry.Text != "Go 语言很强大" && results[0].Entry.Text != "Go 语言性能优秀" {
		t.Errorf("Expected Go related result, got: %s", results[0].Entry.Text)
	}
}
