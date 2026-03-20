package main

import (
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

type TestSuite struct {
	Name        string
	Description string
	Tests       []TestCase
}

type TestCase struct {
	Name          string
	Query         string
	QueryVec      []float32
	Path          string
	Limit         int
	ExpectedTerms []string
	MinResults    int
}

type TestResult struct {
	Suite     string
	Name      string
	Duration  time.Duration
	Found     int
	Precision float64
	Success   bool
	Error     string
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║        HybridMem-RAG 完整集成测试与性能基准测试                ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 初始化数据库
	dbPath := "full_integration_test.db"
	os.Remove(dbPath)

	start := time.Now()
	st, err := store.New(store.Config{
		DBPath:    dbPath,
		VectorDim: 384, // 使用小向量维度进行测试
	})
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	initTime := time.Since(start)

	fmt.Printf("✓ 数据库初始化完成: %v\n", initTime)

	// 插入测试数据
	docCount, insertTime := insertTestDocuments(st)
	fmt.Printf("✓ 插入 %d 个文档: %v (平均 %.2fms/doc)\n\n",
		docCount, insertTime, float64(insertTime.Milliseconds())/float64(docCount))

	// 定义测试套件
	testSuites := defineTestSuites()

	// 运行所有测试
	allResults := make([]TestResult, 0)
	for _, suite := range testSuites {
		fmt.Printf("━━━ %s ━━━\n", suite.Name)
		fmt.Printf("    %s\n\n", suite.Description)
		results := runTestSuite(st, suite)
		allResults = append(allResults, results...)
	}

	// 输出综合报告
	printComprehensiveReport(allResults, docCount, initTime, insertTime)
}

func insertTestDocuments(st store.Store) (int, time.Duration) {
	start := time.Now()
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

		// 生成随机向量用于测试
		vec := generateRandomVector(384)

		_, err = st.Insert(&store.Memory{
			Text:          text,
			Vector:        vec,
			Scope:         "test",
			HierarchyPath: hierarchyPath,
			Importance:    0.5 + rand.Float64()*0.5,
		})
		if err == nil {
			count++
		}
		return nil
	})

	return count, time.Since(start)
}

func generateRandomVector(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rand.Float32()*2 - 1
	}
	return vec
}

func defineTestSuites() []TestSuite {
	return []TestSuite{
		{
			Name:        "BM25 全文检索测试",
			Description: "测试 FTS5 + simple tokenizer 的中文检索能力",
			Tests: []TestCase{
				{Name: "短词-2字", Query: "报告", Limit: 10, ExpectedTerms: []string{"报告"}, MinResults: 1},
				{Name: "短词-2字", Query: "公司", Limit: 10, ExpectedTerms: []string{"公司"}, MinResults: 1},
				{Name: "长词-4字", Query: "人工智能", Limit: 10, ExpectedTerms: []string{"人工智能"}, MinResults: 1},
				{Name: "特殊字符", Query: "C++", Limit: 10, ExpectedTerms: []string{"C++"}, MinResults: 0},
				{Name: "FTS操作符", Query: "AND", Limit: 10, ExpectedTerms: []string{"and"}, MinResults: 0},
			},
		},
		{
			Name:        "向量检索测试",
			Description: "测试余弦相似度向量搜索性能",
			Tests: []TestCase{
				{Name: "向量搜索-10", QueryVec: generateRandomVector(384), Limit: 10, MinResults: 10},
				{Name: "向量搜索-20", QueryVec: generateRandomVector(384), Limit: 20, MinResults: 20},
				{Name: "向量搜索-50", QueryVec: generateRandomVector(384), Limit: 50, MinResults: 40},
			},
		},
		{
			Name:        "混合检索测试",
			Description: "测试 Vector + BM25 RRF 融合检索",
			Tests: []TestCase{
				{Name: "混合-报告", Query: "报告", QueryVec: generateRandomVector(384), Limit: 10, ExpectedTerms: []string{"报告"}, MinResults: 5},
				{Name: "混合-人工智能", Query: "人工智能", QueryVec: generateRandomVector(384), Limit: 10, ExpectedTerms: []string{"人工智能"}, MinResults: 5},
			},
		},
		{
			Name:        "分层检索测试",
			Description: "测试 OpenViking 风格的层次化检索",
			Tests: []TestCase{
				{Name: "分层-年度报告", Query: "年度", Path: "/years_report", Limit: 10, ExpectedTerms: []string{"年度"}, MinResults: 1},
				{Name: "分层-会议", Query: "会议", Path: "/本地AI撰写/20260202_会议纪要", Limit: 10, ExpectedTerms: []string{"会议"}, MinResults: 1},
				{Name: "分层-简历", Query: "简历", Path: "/本地AI撰写/简历筛查", Limit: 10, ExpectedTerms: []string{"简历", "学历"}, MinResults: 1},
			},
		},
		{
			Name:        "压力测试",
			Description: "测试高并发和大批量查询性能",
			Tests: []TestCase{
				{Name: "批量查询-100次", Query: "测试", Limit: 5, MinResults: 0},
			},
		},
	}
}

func runTestSuite(st store.Store, suite TestSuite) []TestResult {
	results := make([]TestResult, 0)

	for _, tc := range suite.Tests {
		var result TestResult
		result.Suite = suite.Name
		result.Name = tc.Name

		// 特殊处理压力测试
		if tc.Name == "批量查询-100次" {
			result = runStressTest(st, tc)
		} else {
			result = runSingleTest(st, tc)
		}

		results = append(results, result)

		// 输出单个测试结果
		status := "✓"
		if !result.Success {
			status = "✗"
		}
		fmt.Printf("  %s %-20s | %8v | 结果: %2d | 准确率: %5.1f%%\n",
			status, result.Name, result.Duration, result.Found, result.Precision*100)
	}

	fmt.Println()
	return results
}

func runSingleTest(st store.Store, tc TestCase) TestResult {
	start := time.Now()

	var results []store.SearchResult
	var err error

	// 根据测试类型选择搜索方法
	if len(tc.QueryVec) > 0 && tc.Query != "" {
		// 混合搜索
		results, err = st.HybridSearch(tc.QueryVec, tc.Query, tc.Limit, []string{"test"})
	} else if len(tc.QueryVec) > 0 {
		// 纯向量搜索
		results, err = st.VectorSearch(tc.QueryVec, tc.Limit, []string{"test"})
	} else {
		// BM25 或分层搜索
		results, err = st.Search(nil, tc.Query, tc.Path, tc.Limit, []string{"test"})
	}

	duration := time.Since(start)

	result := TestResult{
		Name:     tc.Name,
		Duration: duration,
		Found:    len(results),
	}

	if err != nil {
		result.Error = err.Error()
		result.Success = false
		return result
	}

	// 计算准确率
	if len(tc.ExpectedTerms) > 0 {
		result.Precision = calculatePrecision(results, tc.ExpectedTerms)
	} else {
		result.Precision = 1.0 // 向量搜索没有预期词，默认100%
	}

	// 判断成功
	result.Success = result.Found >= tc.MinResults && result.Precision >= 0.5

	return result
}

func runStressTest(st store.Store, tc TestCase) TestResult {
	iterations := 100
	start := time.Now()

	successCount := 0
	for i := 0; i < iterations; i++ {
		_, err := st.Search(nil, tc.Query, tc.Path, tc.Limit, []string{"test"})
		if err == nil {
			successCount++
		}
	}

	duration := time.Since(start)
	avgDuration := duration / time.Duration(iterations)

	return TestResult{
		Name:      tc.Name,
		Duration:  avgDuration,
		Found:     successCount,
		Precision: float64(successCount) / float64(iterations),
		Success:   successCount == iterations,
	}
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

func printComprehensiveReport(results []TestResult, docCount int, initTime, insertTime time.Duration) {
	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                      综合测试报告                               ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")

	// 按测试套件分组统计
	suiteStats := make(map[string]struct {
		count     int
		success   int
		totalTime time.Duration
		avgPrec   float64
	})

	for _, r := range results {
		stats := suiteStats[r.Suite]
		stats.count++
		if r.Success {
			stats.success++
		}
		stats.totalTime += r.Duration
		stats.avgPrec += r.Precision
		suiteStats[r.Suite] = stats
	}

	fmt.Println("\n【测试套件统计】")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("%-30s %8s %10s %12s\n", "测试套件", "通过率", "平均延迟", "平均准确率")
	fmt.Println(strings.Repeat("─", 70))

	totalSuccess := 0
	totalTests := 0
	var totalDuration time.Duration
	totalPrecision := 0.0

	for suite, stats := range suiteStats {
		passRate := float64(stats.success) / float64(stats.count) * 100
		avgDuration := stats.totalTime / time.Duration(stats.count)
		avgPrecision := stats.avgPrec / float64(stats.count) * 100

		fmt.Printf("%-30s %7.1f%% %10v %11.1f%%\n",
			suite, passRate, avgDuration, avgPrecision)

		totalSuccess += stats.success
		totalTests += stats.count
		totalDuration += stats.totalTime
		totalPrecision += stats.avgPrec
	}

	fmt.Println(strings.Repeat("─", 70))
	overallPassRate := float64(totalSuccess) / float64(totalTests) * 100
	overallAvgDuration := totalDuration / time.Duration(totalTests)
	overallAvgPrecision := totalPrecision / float64(totalTests) * 100

	fmt.Printf("%-30s %7.1f%% %10v %11.1f%%\n",
		"总计", overallPassRate, overallAvgDuration, overallAvgPrecision)

	// 性能指标
	fmt.Println("\n【性能指标】")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("数据库初始化时间:     %v\n", initTime)
	fmt.Printf("文档插入总时间:       %v\n", insertTime)
	fmt.Printf("文档数量:             %d\n", docCount)
	fmt.Printf("平均插入时间:         %.2fms/doc\n", float64(insertTime.Milliseconds())/float64(docCount))
	fmt.Printf("平均查询延迟:         %v\n", overallAvgDuration)

	// 性能评级
	fmt.Println("\n【性能评级】")
	if overallAvgDuration < 1*time.Millisecond {
		fmt.Println("  ✅ 优秀 - 平均延迟 < 1ms")
	} else if overallAvgDuration < 10*time.Millisecond {
		fmt.Println("  ✅ 良好 - 平均延迟 < 10ms")
	} else if overallAvgDuration < 50*time.Millisecond {
		fmt.Println("  ⚠️  可接受 - 平均延迟 < 50ms")
	} else {
		fmt.Println("  ❌ 需优化 - 平均延迟 > 50ms")
	}

	// 准确率评级
	fmt.Println("\n【准确率评级】")
	if overallAvgPrecision > 90 {
		fmt.Println("  ✅ 优秀 - 平均准确率 > 90%")
	} else if overallAvgPrecision > 70 {
		fmt.Println("  ✅ 良好 - 平均准确率 > 70%")
	} else if overallAvgPrecision > 50 {
		fmt.Println("  ⚠️  可接受 - 平均准确率 > 50%")
	} else {
		fmt.Println("  ❌ 需优化 - 平均准确率 < 50%")
	}

	// 整体评估
	fmt.Println("\n【整体评估】")
	if overallPassRate == 100 && overallAvgPrecision > 90 && overallAvgDuration < 10*time.Millisecond {
		fmt.Println("  🚀 生产就绪 - 所有指标优秀")
	} else if overallPassRate >= 90 && overallAvgPrecision > 70 {
		fmt.Println("  ✅ 可用 - 主要指标良好")
	} else {
		fmt.Println("  ⚠️  需改进 - 部分指标未达标")
	}

	fmt.Println(strings.Repeat("═", 70))
}
