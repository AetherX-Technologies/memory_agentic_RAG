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
	"gopkg.in/yaml.v3"
)

// Config 测试配置
type Config struct {
	RetrievalMode int `yaml:"retrieval_mode"`
	Embedding     struct {
		Enabled   bool   `yaml:"enabled"`
		Provider  string `yaml:"provider"`
		APIKey    string `yaml:"api_key"`
		Model     string `yaml:"model"`
		Endpoint  string `yaml:"endpoint"`
		Dimension int    `yaml:"dimension"`
	} `yaml:"embedding"`
	Rerank struct {
		Enabled  bool   `yaml:"enabled"`
		Provider string `yaml:"provider"`
		APIKey   string `yaml:"api_key"`
		Model    string `yaml:"model"`
		Endpoint string `yaml:"endpoint"`
	} `yaml:"rerank"`
}

// TestCase 测试用例
type TestCase struct {
	Name          string
	Query         string
	ExpectedDocs  []string // 期望包含的文档关键词
	MinResults    int      // 最少结果数
	TopKRelevant  int      // 前K个结果中应该相关的数量
	K             int      // 检查前K个结果
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile("cmd/real_world_test/config.yaml")
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              HybridMem-RAG 真实场景端到端测试                   ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 加载配置
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 显示检索模式
	modeNames := map[int]string{
		1: "仅关键词（BM25）",
		2: "关键词 + 向量检索（Hybrid）",
		3: "关键词 + 向量 + 重排（Full Pipeline）",
	}
	fmt.Printf("检索模式: %d - %s\n\n", cfg.RetrievalMode, modeNames[cfg.RetrievalMode])

	// 初始化数据库
	dbPath := "real_world_test.db"
	os.Remove(dbPath)

	start := time.Now()
	vectorDim := 0
	if cfg.RetrievalMode >= 2 {
		vectorDim = cfg.Embedding.Dimension
	}

	storeConfig := store.Config{
		DBPath:    dbPath,
		VectorDim: vectorDim,
	}

	// 配置重排（模式3）
	if cfg.RetrievalMode == 3 && cfg.Rerank.Enabled {
		rerankCfg := store.DefaultRerankConfig()
		rerankCfg.Enabled = true
		rerankCfg.Provider = cfg.Rerank.Provider
		rerankCfg.APIKey = cfg.Rerank.APIKey
		rerankCfg.Model = cfg.Rerank.Model
		rerankCfg.Endpoint = cfg.Rerank.Endpoint
		storeConfig.RerankConfig = rerankCfg
	}

	st, err := store.New(storeConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	fmt.Printf("✓ 数据库初始化: %v\n", time.Since(start))

	// 初始化embedder（模式2和3）
	var embedder store.Embedder
	if cfg.RetrievalMode >= 2 && cfg.Embedding.Enabled {
		embedder = store.NewEmbedder(store.EmbeddingConfig{
			Enabled:  true,
			Provider: cfg.Embedding.Provider,
			APIKey:   cfg.Embedding.APIKey,
			Model:    cfg.Embedding.Model,
			Endpoint: cfg.Embedding.Endpoint,
		})
		fmt.Printf("✓ Embedder 已启用 (%s)\n", cfg.Embedding.Provider)
	}

	// 录入真实文档
	docCount, docMap := insertRealDocuments(st, embedder)
	fmt.Printf("✓ 录入 %d 个真实文档\n\n", docCount)

	// 定义复杂测试用例
	testCases := defineComplexTestCases()

	// 执行测试
	totalScore := 0.0
	passCount := 0

	for i, tc := range testCases {
		fmt.Printf("━━━ 测试 %d: %s ━━━\n", i+1, tc.Name)
		fmt.Printf("    查询: \"%s\"\n", tc.Query)

		result := runTestCase(st, tc, docMap, cfg, embedder)

		status := "✗"
		if result.Pass {
			status = "✓"
			passCount++
		}

		fmt.Printf("    %s 准确率: %.1f%% | 相关结果: %d/%d | 总结果: %d\n",
			status, result.Accuracy, result.RelevantCount, tc.K, result.TotalResults)
		fmt.Printf("    详情: %s\n", result.Details)
		fmt.Printf("    前3结果: %s\n\n", strings.Join(result.Top3Docs, ", "))

		totalScore += result.Accuracy
	}

	// 综合报告
	printFinalReport(testCases, totalScore, passCount, docCount)
}

func insertRealDocuments(st store.Store, embedder store.Embedder) (int, map[string]string) {
	docsDir := "/Volumes/SN770Coder/documents"
	count := 0
	docMap := make(map[string]string) // memoryID -> 文档路径

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
		if len(text) > 20000 {
			text = text[:20000]
		}

		relPath := strings.TrimPrefix(path, docsDir)
		hierarchyPath := filepath.Dir(relPath)

		// 生成向量（如果启用）
		var vector []float32
		if embedder != nil {
			vector, err = embedder.Embed(text)
			if err != nil {
				fmt.Printf("警告: 文档 %s 向量化失败: %v\n", filepath.Base(path), err)
				return nil
			}
		}

		id, err := st.Insert(&store.Memory{
			Text:          text,
			Vector:        vector,
			Scope:         "real_test",
			HierarchyPath: hierarchyPath,
			Category:      "document",
			Importance:    0.8,
		})
		if err == nil {
			docMap[id] = filepath.Base(path)
			count++
		}
		return nil
	})

	return count, docMap
}

func defineComplexTestCases() []TestCase {
	return []TestCase{
		{
			Name:         "精确关键词-人工智能",
			Query:        "人工智能",
			ExpectedDocs: []string{"人工智能", "AI", "智能"},
			MinResults:   5,
			TopKRelevant: 4,
			K:            5,
		},
		{
			Name:         "公司项目查询",
			Query:        "西北院 工程",
			ExpectedDocs: []string{"西北院", "工程", "电建"},
			MinResults:   3,
			TopKRelevant: 2,
			K:            5,
		},
		{
			Name:         "技术领域-深度学习",
			Query:        "深度学习 神经网络",
			ExpectedDocs: []string{"深度学习", "神经网络", "模型", "算法"},
			MinResults:   2,
			TopKRelevant: 1,
			K:            5,
		},
		{
			Name:         "简历筛选-学历",
			Query:        "西安电子科技大学 硕士",
			ExpectedDocs: []string{"西安电子科技大学", "硕士", "学历"},
			MinResults:   3,
			TopKRelevant: 2,
			K:            5,
		},
		{
			Name:         "会议相关",
			Query:        "会议 评审",
			ExpectedDocs: []string{"会议", "评审", "讨论"},
			MinResults:   3,
			TopKRelevant: 2,
			K:            5,
		},
		{
			Name:         "技术栈查询",
			Query:        "Python Java",
			ExpectedDocs: []string{"python", "java", "编程"},
			MinResults:   2,
			TopKRelevant: 1,
			K:            5,
		},
		{
			Name:         "报告文档",
			Query:        "报告 方案",
			ExpectedDocs: []string{"报告", "方案", "实施"},
			MinResults:   3,
			TopKRelevant: 2,
			K:            5,
		},
		{
			Name:         "复杂组合-AI应用",
			Query:        "人工智能 应用场景 知识",
			ExpectedDocs: []string{"人工智能", "应用", "场景", "知识"},
			MinResults:   3,
			TopKRelevant: 2,
			K:            5,
		},
		{
			Name:         "技术能力查询",
			Query:        "算法 数据结构",
			ExpectedDocs: []string{"算法", "数据", "结构"},
			MinResults:   2,
			TopKRelevant: 1,
			K:            5,
		},
		{
			Name:         "工作经验查询",
			Query:        "项目经验 开发",
			ExpectedDocs: []string{"项目", "经验", "开发"},
			MinResults:   2,
			TopKRelevant: 1,
			K:            5,
		},
	}
}

type TestResult struct {
	Pass          bool
	Accuracy      float64
	RelevantCount int
	TotalResults  int
	Top3Docs      []string
	Details       string
}

func runTestCase(st store.Store, tc TestCase, docMap map[string]string, cfg *Config, embedder store.Embedder) TestResult {
	start := time.Now()

	// 生成查询向量（模式2和3）
	var queryVector []float32
	if cfg.RetrievalMode >= 2 && embedder != nil {
		var err error
		queryVector, err = embedder.Embed(tc.Query)
		if err != nil {
			return TestResult{
				Pass:     false,
				Accuracy: 0,
				Details:  fmt.Sprintf("查询向量化失败: %v", err),
			}
		}
	}

	// 根据模式执行检索
	var results []store.SearchResult
	var err error

	switch cfg.RetrievalMode {
	case 1:
		// 仅BM25
		results, err = st.Search(nil, tc.Query, "", 10, []string{"real_test"})
	case 2, 3:
		// 混合检索（模式3的重排在store内部自动处理）
		results, err = st.Search(queryVector, tc.Query, "", 10, []string{"real_test"})
	}

	duration := time.Since(start)

	if err != nil {
		return TestResult{
			Pass:     false,
			Accuracy: 0,
			Details:  fmt.Sprintf("查询失败: %v", err),
		}
	}

	// 评估结果
	relevantCount := 0
	top3 := []string{}

	for i, r := range results {
		if i < 3 {
			docName := docMap[r.Entry.ID]
			if docName == "" {
				docName = r.Entry.ID[:8]
			}
			top3 = append(top3, docName)
		}

		if i < tc.K {
			// 检查是否包含期望的关键词
			text := strings.ToLower(r.Entry.Text)
			for _, keyword := range tc.ExpectedDocs {
				if strings.Contains(text, strings.ToLower(keyword)) {
					relevantCount++
					break
				}
			}
		}
	}

	accuracy := 0.0
	if tc.K > 0 {
		accuracy = float64(relevantCount) / float64(tc.K) * 100
	}

	pass := relevantCount >= tc.TopKRelevant && len(results) >= tc.MinResults

	return TestResult{
		Pass:          pass,
		Accuracy:      accuracy,
		RelevantCount: relevantCount,
		TotalResults:  len(results),
		Top3Docs:      top3,
		Details:       fmt.Sprintf("耗时: %v", duration),
	}
}

func printFinalReport(testCases []TestCase, totalScore float64, passCount int, docCount int) {
	fmt.Println("\n╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    真实场景测试综合报告                         ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")

	avgAccuracy := totalScore / float64(len(testCases))
	passRate := float64(passCount) / float64(len(testCases)) * 100

	fmt.Println("\n【测试统计】")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("文档数量:     %d\n", docCount)
	fmt.Printf("测试用例:     %d\n", len(testCases))
	fmt.Printf("通过用例:     %d\n", passCount)
	fmt.Printf("失败用例:     %d\n", len(testCases)-passCount)
	fmt.Printf("通过率:       %.1f%%\n", passRate)
	fmt.Printf("平均准确率:   %.1f%%\n", avgAccuracy)

	fmt.Println("\n【准确率评级】")
	fmt.Println(strings.Repeat("─", 70))
	if avgAccuracy >= 80 {
		fmt.Println("  🚀 优秀 - 检索准确率非常高")
	} else if avgAccuracy >= 60 {
		fmt.Println("  ✅ 良好 - 检索准确率达标")
	} else if avgAccuracy >= 40 {
		fmt.Println("  ⚠️  可接受 - 检索准确率需要改进")
	} else {
		fmt.Println("  ❌ 不合格 - 检索准确率过低")
	}

	fmt.Println("\n【整体评价】")
	fmt.Println(strings.Repeat("─", 70))
	if passRate >= 75 && avgAccuracy >= 60 {
		fmt.Println("  🎯 系统在真实场景下表现良好，可用于生产环境")
	} else if passRate >= 50 {
		fmt.Println("  📊 系统基本可用，建议优化检索算法")
	} else {
		fmt.Println("  🔧 系统需要进一步优化")
	}

	fmt.Println(strings.Repeat("═", 70))
}
