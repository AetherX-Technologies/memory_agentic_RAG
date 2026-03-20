package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

type TestCase struct {
	Name          string
	Query         string
	Path          string
	ExpectedTerms []string
}

type TestResult struct {
	Name      string
	Duration  time.Duration
	Found     int
	Precision float64
}

func main() {
	dbPath := "benchmark_integration.db"
	os.Remove(dbPath)

	start := time.Now()
	st, err := store.New(store.Config{
		DBPath:    dbPath,
		VectorDim: 0,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	fmt.Printf("✓ Database initialized in %v\n\n", time.Since(start))

	// 插入真实文档
	docCount := insertDocuments(st)
	fmt.Printf("✓ Inserted %d documents\n\n", docCount)

	// 测试用例
	testCases := []TestCase{
		{
			Name:          "短词-报告",
			Query:         "报告",
			Path:          "",
			ExpectedTerms: []string{"报告"},
		},
		{
			Name:          "短词-公司",
			Query:         "公司",
			Path:          "",
			ExpectedTerms: []string{"公司"},
		},
		{
			Name:          "长词-人工智能",
			Query:         "人工智能",
			Path:          "",
			ExpectedTerms: []string{"人工智能", "AI"},
		},
		{
			Name:          "技术词-机器学习",
			Query:         "机器学习",
			Path:          "",
			ExpectedTerms: []string{"机器学习", "算法"},
		},
		{
			Name:          "特殊字符-C++",
			Query:         "C++",
			Path:          "",
			ExpectedTerms: []string{"C++", "编程"},
		},
		{
			Name:          "分层-年度报告",
			Query:         "年度",
			Path:          "/years_report",
			ExpectedTerms: []string{"年度", "2025"},
		},
		{
			Name:          "分层-会议",
			Query:         "会议",
			Path:          "/本地AI撰写/20260202_会议纪要",
			ExpectedTerms: []string{"会议", "评审"},
		},
	}

	// 执行测试
	results := runTests(st, testCases)

	// 输出结果
	printResults(results)
}

func insertDocuments(st store.Store) int {
	docsDir := "/Volumes/SN770Coder/documents"
	count := 0

	filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		text := string(content)
		if len(text) > 10000 {
			text = text[:10000]
		}

		relPath := strings.TrimPrefix(path, docsDir)
		hierarchyPath := filepath.Dir(relPath)

		_, err = st.Insert(&store.Memory{
			Text:          text,
			Scope:         "benchmark",
			HierarchyPath: hierarchyPath,
		})
		if err == nil {
			count++
		}
		return nil
	})

	return count
}

func runTests(st store.Store, testCases []TestCase) []TestResult {
	results := make([]TestResult, 0, len(testCases))

	for _, tc := range testCases {
		fmt.Printf("Running: %s\n", tc.Name)

		start := time.Now()
		searchResults, err := st.Search(nil, tc.Query, tc.Path, 10, []string{"benchmark"})
		duration := time.Since(start)

		if err != nil {
			fmt.Printf("  ❌ Error: %v\n", err)
			continue
		}

		precision := calculatePrecision(searchResults, tc.ExpectedTerms)

		result := TestResult{
			Name:      tc.Name,
			Duration:  duration,
			Found:     len(searchResults),
			Precision: precision,
		}
		results = append(results, result)

		fmt.Printf("  ✓ Found: %d, Duration: %v, Precision: %.2f%%\n",
			result.Found, result.Duration, result.Precision*100)
	}

	return results
}

func calculatePrecision(results []store.SearchResult, expectedTerms []string) float64 {
	if len(results) == 0 {
		return 0
	}

	relevant := 0
	for _, r := range results {
		text := strings.ToLower(r.Entry.Text)
		for _, term := range expectedTerms {
			if strings.Contains(text, strings.ToLower(term)) {
				relevant++
				break
			}
		}
	}

	return float64(relevant) / float64(len(results))
}

func printResults(results []TestResult) {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("BENCHMARK RESULTS")
	fmt.Println(strings.Repeat("=", 70))

	var totalDuration time.Duration
	var totalPrecision float64

	fmt.Printf("%-25s %12s %8s %12s\n", "Test Case", "Duration", "Found", "Precision")
	fmt.Println(strings.Repeat("-", 70))

	for _, r := range results {
		fmt.Printf("%-25s %12v %8d %11.1f%%\n",
			r.Name, r.Duration, r.Found, r.Precision*100)
		totalDuration += r.Duration
		totalPrecision += r.Precision
	}

	fmt.Println(strings.Repeat("-", 70))
	avgDuration := totalDuration / time.Duration(len(results))
	avgPrecision := totalPrecision / float64(len(results))

	fmt.Printf("%-25s %12v %8s %11.1f%%\n",
		"AVERAGE", avgDuration, "-", avgPrecision*100)
	fmt.Println(strings.Repeat("=", 70))

	// 性能评估
	fmt.Println("\nPERFORMANCE ASSESSMENT:")
	if avgDuration < 50*time.Millisecond {
		fmt.Println("  ✅ Excellent - Average latency < 50ms")
	} else if avgDuration < 100*time.Millisecond {
		fmt.Println("  ✅ Good - Average latency < 100ms")
	} else if avgDuration < 200*time.Millisecond {
		fmt.Println("  ⚠️  Acceptable - Average latency < 200ms")
	} else {
		fmt.Println("  ❌ Poor - Average latency > 200ms")
	}

	// 准确率评估
	fmt.Println("\nACCURACY ASSESSMENT:")
	if avgPrecision > 0.9 {
		fmt.Println("  ✅ Excellent - Precision > 90%")
	} else if avgPrecision > 0.7 {
		fmt.Println("  ✅ Good - Precision > 70%")
	} else if avgPrecision > 0.5 {
		fmt.Println("  ⚠️  Acceptable - Precision > 50%")
	} else {
		fmt.Println("  ❌ Poor - Precision < 50%")
	}
}
