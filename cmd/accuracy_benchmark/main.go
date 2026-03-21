// Comprehensive Accuracy Benchmark — 6 retrieval methods compared
//
// Methods tested:
//   1. BM25 only (keyword)
//   2. Vector only — Local Qwen3-0.6B ONNX
//   3. Vector only — Online Qwen3-4B API
//   4. Hybrid BM25+Vector (local embedding)
//   5. Hybrid BM25+Vector (online embedding)
//   6. Hybrid + Reranker (online embedding + Qwen3-Reranker-4B)
//
// Usage: go run -tags fts5 ./cmd/accuracy_benchmark/
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/embedder"
	"github.com/yourusername/hybridmem-rag/internal/parser"
	"github.com/yourusername/hybridmem-rag/internal/store"
	"gopkg.in/yaml.v3"
)

type BenchConfig struct {
	EmbeddingAPI struct {
		Provider string `yaml:"provider"`
		APIKey   string `yaml:"api_key"`
		Model    string `yaml:"model"`
		Endpoint string `yaml:"endpoint"`
	} `yaml:"embedding_api"`
	EmbeddingLocal struct {
		ModelPath string `yaml:"model_path"`
	} `yaml:"embedding_local"`
	Rerank struct {
		Enabled     bool    `yaml:"enabled"`
		Provider    string  `yaml:"provider"`
		APIKey      string  `yaml:"api_key"`
		Model       string  `yaml:"model"`
		Endpoint    string  `yaml:"endpoint"`
		BlendWeight float64 `yaml:"blend_weight"`
		MaxCands    int     `yaml:"max_candidates"`
	} `yaml:"rerank"`
}

type TestCase struct {
	Query    string
	Expected []string
	Category string
	Desc     string
}

type CaseResult struct {
	Found bool
	Rank  int
	Dur   time.Duration
}

type MethodResult struct {
	Name    string
	Cases   []CaseResult
	Recall  float64
	MRR     float64
	AvgTime time.Duration
}

type Doc struct{ ID, Path string }

const topK = 5
const docRoot = "/Volumes/SN770Coder/documents"

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║      HybridMem-RAG 6 方法准确率对比（本地 vs 线上 vs 重排）              ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════════════╝")

	cfg := loadConfig()
	docs := discoverDocs(docRoot)
	cases := buildCases()

	// ── Load local embedder ──
	fmt.Println("\n[1] 加载本地模型 Qwen3-0.6B ONNX")
	localEmb, err := embedder.NewLocalEmbedder(embedder.Config{
		ModelPath: cfg.EmbeddingLocal.ModelPath, BatchSize: 16,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "本地模型加载失败: %v\n", err)
		os.Exit(1)
	}
	defer localEmb.Close()
	fmt.Println("    OK")

	// ── Load online embedder ──
	fmt.Println("\n[2] 配置线上模型 Qwen3-4B API")
	apiEmb := store.NewEmbedder(store.EmbeddingConfig{
		Enabled: true, Provider: cfg.EmbeddingAPI.Provider,
		APIKey: cfg.EmbeddingAPI.APIKey, Model: cfg.EmbeddingAPI.Model,
		Endpoint: cfg.EmbeddingAPI.Endpoint, Timeout: 30,
	})
	// Quick test
	testVec, err := apiEmb.Embed("测试")
	if err != nil {
		fmt.Printf("    线上模型连接失败: %v\n", err)
		fmt.Println("    将跳过线上模型测试")
		apiEmb = nil
	} else {
		fmt.Printf("    OK (dim=%d)\n", len(testVec))
	}

	// ── Create stores ──
	// Store A: local embeddings
	fmt.Println("\n[3] 创建数据库 + 导入文档")
	storeLocal, chunksLocal := createAndIngest("本地0.6B", docs, func(text string) ([]float32, error) {
		return localEmb.Embed(text)
	})
	defer storeLocal.Close()

	// Store B: online embeddings (separate DB because vectors differ)
	var storeOnline store.Store
	if apiEmb != nil {
		storeOnline, _ = createAndIngest("线上4B", docs, func(text string) ([]float32, error) {
			return apiEmb.Embed(text)
		})
		defer storeOnline.Close()
	}

	// Store C: online embeddings + reranker
	var storeRerank store.Store
	if apiEmb != nil && cfg.Rerank.Enabled {
		storeRerank, _ = createAndIngestWithRerank("线上4B+重排", docs, cfg, func(text string) ([]float32, error) {
			return apiEmb.Embed(text)
		})
		defer storeRerank.Close()
	}

	_ = chunksLocal

	// ── Run tests ──
	fmt.Println("\n" + strings.Repeat("═", 75))
	fmt.Printf("  准确率对比（%d 查询 × %d 方法）\n", len(cases), countMethods(apiEmb, storeRerank))
	fmt.Println(strings.Repeat("═", 75))

	var results []MethodResult

	// Method 1: BM25
	results = append(results, runMethod("①BM25关键词", cases, func(tc TestCase) ([]store.SearchResult, error) {
		return storeLocal.Search(nil, tc.Query, "", topK*3, nil)
	}))

	// Method 2: Vector local
	results = append(results, runMethod("②向量-本地0.6B", cases, func(tc TestCase) ([]store.SearchResult, error) {
		qv, err := localEmb.Embed(tc.Query)
		if err != nil {
			return nil, fmt.Errorf("local embed failed: %w", err)
		}
		return storeLocal.VectorSearch(qv, topK*3, nil)
	}))

	// Method 3: Vector online
	if apiEmb != nil && storeOnline != nil {
		results = append(results, runMethod("③向量-线上4B", cases, func(tc TestCase) ([]store.SearchResult, error) {
			qv, err := apiEmb.Embed(tc.Query)
			if err != nil {
				return nil, fmt.Errorf("api embed failed: %w", err)
			}
			return storeOnline.VectorSearch(qv, topK*3, nil)
		}))
	}

	// Method 4: Hybrid local
	results = append(results, runMethod("④混合-本地0.6B", cases, func(tc TestCase) ([]store.SearchResult, error) {
		qv, err := localEmb.Embed(tc.Query)
		if err != nil {
			return nil, fmt.Errorf("local embed failed: %w", err)
		}
		return storeLocal.HybridSearch(qv, tc.Query, topK*3, nil)
	}))

	// Method 5: Hybrid online
	if apiEmb != nil && storeOnline != nil {
		results = append(results, runMethod("⑤混合-线上4B", cases, func(tc TestCase) ([]store.SearchResult, error) {
			qv, err := apiEmb.Embed(tc.Query)
			if err != nil {
				return nil, fmt.Errorf("api embed failed: %w", err)
			}
			return storeOnline.HybridSearch(qv, tc.Query, topK*3, nil)
		}))
	}

	// Method 6: Hybrid online + reranker
	if storeRerank != nil {
		results = append(results, runMethod("⑥混合+重排-线上4B", cases, func(tc TestCase) ([]store.SearchResult, error) {
			qv, err := apiEmb.Embed(tc.Query)
			if err != nil {
				return nil, fmt.Errorf("api embed failed: %w", err)
			}
			return storeRerank.HybridSearch(qv, tc.Query, topK*3, nil)
		}))
	}

	printSummary(results, cases)
	printDetail(results, cases)
}

// ── Store creation helpers ──

func createAndIngest(label string, docs []Doc, embed func(string) ([]float32, error)) (store.Store, int) {
	st, err := store.New(store.Config{DBPath: ":memory:"})
	if err != nil {
		fmt.Printf("    Store创建失败(%s): %v\n", label, err)
		os.Exit(1)
	}

	splitter := parser.NewSmartSplitter(parser.SplitterConfig{
		MaxChunkSize: 512, MinChunkSize: 128,
	}, nil)

	total := 0
	start := time.Now()
	for _, doc := range docs {
		content, err := os.ReadFile(doc.Path)
		if err != nil {
			continue
		}
		sections, err := splitter.Split(string(content), doc.ID)
		if err != nil {
			continue
		}
		for j, sec := range sections {
			vec, err := embed(sec.Content)
			if err != nil {
				continue
			}
			if _, err := st.Insert(&store.Memory{
				Text: sec.Content, Category: "document", Scope: "global",
				Importance: 0.7, Timestamp: time.Now().Unix(),
				NodeType: "chunk", SourceFile: doc.ID,
				ChunkIndex: j, Vector: vec,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: insert failed for %s chunk %d: %v\n", doc.ID, j, err)
				continue
			}
			total++
		}
	}
	fmt.Printf("    %s: %d chunks (%.0fs)\n", label, total, time.Since(start).Seconds())
	return st, total
}

func createAndIngestWithRerank(label string, docs []Doc, cfg *BenchConfig, embed func(string) ([]float32, error)) (store.Store, int) {
	defaults := store.DefaultRerankConfig()
	st, err := store.New(store.Config{
		DBPath: ":memory:",
		RerankConfig: store.RerankConfig{
			Enabled:           true,
			Provider:          cfg.Rerank.Provider,
			APIKey:            cfg.Rerank.APIKey,
			Model:             cfg.Rerank.Model,
			Endpoint:          cfg.Rerank.Endpoint,
			BlendWeight:       cfg.Rerank.BlendWeight,
			MaxCandidates:     cfg.Rerank.MaxCands,
			Timeout:           defaults.Timeout,
			MaxDocLength:      defaults.MaxDocLength,
			UnreturnedPenalty: defaults.UnreturnedPenalty,
			MinBlendedScore:   defaults.MinBlendedScore,
		},
	})
	if err != nil {
		fmt.Printf("    Store创建失败(%s): %v\n", label, err)
		os.Exit(1)
	}

	splitter := parser.NewSmartSplitter(parser.SplitterConfig{
		MaxChunkSize: 512, MinChunkSize: 128,
	}, nil)

	total := 0
	start := time.Now()
	for _, doc := range docs {
		content, err := os.ReadFile(doc.Path)
		if err != nil {
			continue
		}
		sections, err := splitter.Split(string(content), doc.ID)
		if err != nil {
			continue
		}
		for j, sec := range sections {
			vec, err := embed(sec.Content)
			if err != nil {
				continue
			}
			if _, err := st.Insert(&store.Memory{
				Text: sec.Content, Category: "document", Scope: "global",
				Importance: 0.7, Timestamp: time.Now().Unix(),
				NodeType: "chunk", SourceFile: doc.ID,
				ChunkIndex: j, Vector: vec,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: insert failed for %s chunk %d: %v\n", doc.ID, j, err)
				continue
			}
			total++
		}
	}
	fmt.Printf("    %s: %d chunks (%.0fs)\n", label, total, time.Since(start).Seconds())
	return st, total
}

// ── Test infrastructure ──

func runMethod(name string, cases []TestCase, search func(TestCase) ([]store.SearchResult, error)) MethodResult {
	fmt.Printf("\n  【%s】", name)
	r := MethodResult{Name: name}
	for _, tc := range cases {
		start := time.Now()
		results, err := search(tc)
		dur := time.Since(start)
		cr := CaseResult{Dur: dur}
		if err != nil {
			// Embed or search failed — mark as not found
			cr.Found = false
		} else if len(tc.Expected) > 0 {
			cr.Found, cr.Rank = check(results, tc.Expected)
		} else {
			// Negative case: top-K always returns results so we cannot reliably
			// evaluate false positives. Rank=-1 excludes from recall/MRR metrics.
			// Found=false so printSummary shows them as unevaluated, not as passes.
			cr.Found = false
			cr.Rank = -1
		}
		r.Cases = append(r.Cases, cr)
	}
	calc(&r)
	fmt.Printf("  Recall@%d=%.0f%%  MRR=%.3f  Avg=%v\n", topK, r.Recall*100, r.MRR, r.AvgTime)
	return r
}

func check(results []store.SearchResult, expected []string) (bool, int) {
	for rank, r := range results {
		if rank >= topK {
			break
		}
		sf := r.Entry.SourceFile
		for _, exp := range expected {
			// Match against full path, basename, or suffix (handles relative path IDs)
			if sf == exp || filepath.Base(sf) == exp || strings.HasSuffix(sf, "/"+exp) || strings.Contains(sf, exp) {
				return true, rank + 1
			}
		}
	}
	return false, 0
}

func calc(r *MethodResult) {
	found, rr := 0, 0.0
	evaluated := 0
	var dur time.Duration
	for _, c := range r.Cases {
		dur += c.Dur
		if c.Rank == -1 {
			continue // skip negative cases from recall/MRR
		}
		evaluated++
		if c.Found {
			found++
			if c.Rank > 0 {
				rr += 1.0 / float64(c.Rank)
			}
		}
	}
	if evaluated > 0 {
		r.Recall = float64(found) / float64(evaluated)
		r.MRR = rr / float64(evaluated)
	}
	if len(r.Cases) > 0 {
		r.AvgTime = dur / time.Duration(len(r.Cases))
	}
}

// ── Test cases ──

func buildCases() []TestCase {
	return []TestCase{
		// 精确匹配
		{"OpenClaw Framework Core 框架核心层", []string{"openclaw原理"}, "exact", "OpenClaw层名"},
		{"Bootstrap Hook 动态注入 System Prompt", []string{"openclaw原理"}, "exact", "Hook细节"},
		{"杨智斐 中北大学 西安电子科技大学 博士", []string{"AI-杨智斐（中北大学+西安电子科技大学+博士）"}, "exact", "候选人"},
		{"概要设计说明书第三章修改意见", []string{"修改意见"}, "exact", "验收文件"},
		{"CRP 114 炎症指标异常偏高", []string{"我把文档看完后，判断这不是\u201c所有检查都正常\u201d，而是炎症证据很强，但真正病灶还没有被定位出来。报告里最突出的异常是：CRP 114"}, "exact", "医疗"},
		{"Always-On Memory Agent 长期记忆后端", []string{"周报改写稿-2026-03-15"}, "exact", "特定周报"},
		{"西北院人工智能行动方案2026-2028评审", []string{"会议纪要_矫正版"}, "exact", "会议"},
		{"JAVA曹聪西安电子科技大学", []string{"JAVA-曹聪（西安电子科技大学）"}, "exact", "Java候选人"},
		// 语义匹配
		{"如何为AI助手设计多层系统提示词架构", []string{"openclaw原理"}, "semantic", "系统提示词"},
		{"求职者的学术背景和研究方向评估", []string{"AI电话面试手卡-5人版", "AI技术面评价-5人-分布版"}, "semantic", "招聘"},
		{"大型工程企业的数字化智能化战略规划", []string{"会议纪要_矫正版", "会议信息"}, "semantic", "企业规划"},
		{"血液检查报告解读和异常分析", []string{"我把文档看完后，判断这不是\u201c所有检查都正常\u201d，而是炎症证据很强，但真正病灶还没有被定位出来。报告里最突出的异常是：CRP 114"}, "semantic", "医疗语义"},
		{"员工绩效考核和年终述职汇报", []string{"王笑冰-2025年终总结-PPT原始稿", "王笑冰-2025年终总结-PPT原始稿-1"}, "semantic", "年终"},
		{"软件工程项目验收标准和文档质量", []string{"修改意见"}, "semantic", "验收语义"},
		// 否定
		{"量子计算在密码学中的应用前景", []string{}, "negative", "无关-量子"},
		{"火星殖民计划的技术可行性分析", []string{}, "negative", "无关-火星"},
	}
}

// ── Output ──

func printSummary(results []MethodResult, cases []TestCase) {
	fmt.Println("\n" + strings.Repeat("═", 75))
	fmt.Println("  汇总对比")
	fmt.Println(strings.Repeat("═", 75))

	fmt.Printf("  %-20s Recall@%d   MRR    精确    语义    否定    耗时\n", "方法", topK)
	fmt.Println("  " + strings.Repeat("-", 70))

	for _, r := range results {
		cats := map[string][2]int{}
		for i, c := range r.Cases {
			cat := cases[i].Category
			s := cats[cat]
			s[1]++
			if c.Found {
				s[0]++
			}
			cats[cat] = s
		}
		fmt.Printf("  %-20s %5.1f%%   %.3f  %d/%-2d   %d/%-2d   %d/%-2d   %v\n",
			r.Name, r.Recall*100, r.MRR,
			cats["exact"][0], cats["exact"][1],
			cats["semantic"][0], cats["semantic"][1],
			cats["negative"][0], cats["negative"][1],
			r.AvgTime.Round(time.Millisecond))
	}
}

func printDetail(results []MethodResult, cases []TestCase) {
	fmt.Println("\n  逐查询详情:")
	for i, tc := range cases {
		marks := ""
		for _, r := range results {
			if i < len(r.Cases) {
				c := r.Cases[i]
				m := "✗"
				if c.Found && c.Rank > 0 {
					m = fmt.Sprintf("✓%d", c.Rank)
				} else if c.Found {
					m = "✓-"
				}
				marks += fmt.Sprintf(" %-4s", m)
			}
		}
		fmt.Printf("  [%-8s] %s %s\n", tc.Category, trunc(tc.Query, 28), marks)
	}
	fmt.Print("\n  图例:")
	for _, r := range results {
		fmt.Printf(" %s |", trunc(r.Name, 8))
	}
	fmt.Println()
}

func trunc(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s + strings.Repeat(" ", int(math.Max(0, float64(n-len(runes)))))
	}
	return string(runes[:n-2]) + ".."
}

func countMethods(api store.Embedder, rerank store.Store) int {
	n := 2 // BM25 + local vector
	if api != nil {
		n += 2 // online vector + hybrid online
	}
	n++ // hybrid local
	if rerank != nil {
		n++ // hybrid + rerank
	}
	return n
}

// ── Config + Discovery ──

func loadConfig() *BenchConfig {
	data, err := os.ReadFile("cmd/accuracy_benchmark/config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置文件读取失败: %v\n", err)
		os.Exit(1)
	}
	// Expand environment variables (e.g. ${BENCH_API_KEY})
	expanded := os.ExpandEnv(string(data))
	cfg := &BenchConfig{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "配置文件解析失败: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func discoverDocs(root string) []Doc {
	var docs []Doc
	seen := map[string]bool{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Size() < 2000 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		if seen[path] {
			return nil
		}
		seen[path] = true
		// Use relative path as ID to avoid basename collisions
		relPath, _ := filepath.Rel(root, path)
		if relPath == "" {
			relPath = filepath.Base(path)
		}
		id := strings.TrimSuffix(relPath, ext)
		docs = append(docs, Doc{ID: id, Path: path})
		return nil
	})
	return docs
}
