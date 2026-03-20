package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

func main() {
	// 创建测试数据库
	dbPath := "integration_test.db"
	os.Remove(dbPath)

	st, err := store.New(store.Config{
		DBPath:    dbPath,
		VectorDim: 0, // 暂不使用向量
	})
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	// 扫描并插入文档
	docsPath := "/Volumes/SN770Coder/documents"
	count := 0

	err = filepath.WalkDir(docsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !isTextFile(path) {
			return nil
		}

		// 读取文件内容
		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Skip %s: %v", path, err)
			return nil
		}

		// 限制内容长度
		text := string(content)
		if len(text) > 5000 {
			text = text[:5000]
		}

		// 计算层次路径
		relPath, _ := filepath.Rel(docsPath, filepath.Dir(path))
		hierarchyPath := "/" + strings.ReplaceAll(relPath, string(filepath.Separator), "/")
		if hierarchyPath == "/." {
			hierarchyPath = ""
		}

		memory := &store.Memory{
			Text:          text,
			Category:      "document",
			Scope:         "global",
			Importance:    0.8,
			HierarchyPath: hierarchyPath,
			Metadata:      fmt.Sprintf(`{"filename":"%s"}`, filepath.Base(path)),
		}

		id, err := st.Insert(memory)
		if err != nil {
			log.Printf("Insert failed %s: %v", path, err)
			return nil
		}

		count++
		fmt.Printf("[%d] Inserted: %s (path=%s, id=%s)\n", count, filepath.Base(path), hierarchyPath, id[:8])
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n✓ Inserted %d documents\n\n", count)

	// 执行测试查询
	runTests(st)
}

func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".txt" || ext == ".md" || ext == ".go" || ext == ".json"
}

func runTests(st store.Store) {
	tests := []struct {
		name        string
		query       string
		currentPath string
		limit       int
	}{
		{"全局搜索-人工智能", "人工智能", "", 5},
		{"全局搜索-特殊字符C++", "C++", "", 3},
		{"全局搜索-AND操作符", "AND", "", 3},
		{"分层搜索-years_report", "报告", "/years_report", 5},
		{"分层搜索-公司杂项", "公司", "/公司杂项", 5},
		{"根路径搜索", "文档", "/", 3},
	}

	for i, tt := range tests {
		fmt.Printf("=== Test %d: %s ===\n", i+1, tt.name)
		fmt.Printf("Query: %q, Path: %q, Limit: %d\n", tt.query, tt.currentPath, tt.limit)

		results, err := st.Search(nil, tt.query, tt.currentPath, tt.limit, []string{"global"})
		if err != nil {
			fmt.Printf("❌ Error: %v\n\n", err)
			continue
		}

		fmt.Printf("✓ Found %d results:\n", len(results))
		for j, r := range results {
			preview := r.Entry.Text
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Printf("  [%d] Score=%.4f Path=%s\n      %s\n",
				j+1, r.Score, r.Entry.HierarchyPath, preview)
		}
		fmt.Println()
	}
}
