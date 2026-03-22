// Real LLM integration test — tests extraction and consolidation with actual API calls.
// Usage: MEMORY_LLM_KEY=xxx go run -tags fts5 ./cmd/real_llm_test/
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/dedup"
	"github.com/yourusername/hybridmem-rag/internal/extractor"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

const (
	apiEndpoint = "https://api-vip.codex-for.me/v1/chat/completions"
	apiModel    = "gpt-5.4"
)

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║   Real LLM Integration Test (Extraction + Consolidation)║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	apiKey := os.Getenv("MEMORY_LLM_KEY")
	if apiKey == "" {
		fmt.Println("❌ 请设置 MEMORY_LLM_KEY 环境变量")
		os.Exit(1)
	}

	dbPath := "real_llm_test.db"
	os.Remove(dbPath)
	os.Remove(dbPath + "-shm")
	os.Remove(dbPath + "-wal")
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := store.New(store.Config{DBPath: dbPath})
	if err != nil {
		fatal("Store: %v", err)
	}
	defer st.Close()
	fmt.Println("✅ Store 初始化")

	// ══════════════════════════════════════════
	// Test 1: Real LLM Extraction
	// ══════════════════════════════════════════
	fmt.Println("\n═══ Test 1: 真实 LLM 记忆提取 ═══")

	ext := extractor.New(extractor.Config{
		APIKey:   apiKey,
		Model:    apiModel,
		Endpoint: apiEndpoint,
		Timeout:  60,
	})

	conversations := []struct {
		name     string
		messages []extractor.Message
	}{
		{"自我介绍", []extractor.Message{
			{Role: "user", Content: "我叫张伟，是西北院的水利工程师。我用Go写了3年后端，也会Python。平时在北京的办公室工作。"},
			{Role: "assistant", Content: "你好张伟！了解了你的背景 — 西北院水利工程师，Go后端3年经验，也会Python，在北京工作。"},
		}},
		{"偏好与指令", []extractor.Message{
			{Role: "user", Content: "以后回复请用中文。我喜欢简洁的代码风格，不要太多注释。代码里的变量名用英文。"},
			{Role: "assistant", Content: "好的，以后我会用中文回复，代码简洁风格，变量名用英文。"},
		}},
		{"项目讨论", []extractor.Message{
			{Role: "user", Content: "我最近在做一个水利监测系统，用Go+SQLite，需要处理传感器数据的实时存储和查询。"},
			{Role: "assistant", Content: "水利监测系统用Go+SQLite是个不错的选择。传感器数据的实时存储可以考虑WAL模式提升写入性能。"},
		}},
		{"无意义寒暄", []extractor.Message{
			{Role: "user", Content: "今天北京天气不错啊"},
			{Role: "assistant", Content: "是啊，春天到了。"},
		}},
	}

	var allMemories []extractor.ExtractedMemory
	for _, conv := range conversations {
		fmt.Printf("\n  [%s]\n", conv.name)
		start := time.Now()
		mems, err := ext.Extract(context.Background(), conv.messages, conv.name)
		dur := time.Since(start)
		if err != nil {
			fmt.Printf("  ⚠️ 提取失败 (%.1fs): %v\n", dur.Seconds(), err)
			// Fallback extraction should still work
			continue
		}
		fmt.Printf("  ✅ %d 条记忆 (%.1fs)\n", len(mems), dur.Seconds())
		for _, m := range mems {
			fmt.Printf("     [%s] %s (importance=%.1f, confidence=%.1f)\n",
				m.MemoryType, m.Content, m.Importance, m.Confidence)
			if len(m.Tags) > 0 {
				fmt.Printf("       标签: %v\n", m.Tags)
			}
		}
		allMemories = append(allMemories, mems...)
	}

	fmt.Printf("\n  总计提取: %d 条记忆\n", len(allMemories))
	if len(allMemories) == 0 {
		fatal("LLM 提取返回 0 条记忆")
	}

	// ══════════════════════════════════════════
	// Test 2: Store with Dedup
	// ══════════════════════════════════════════
	fmt.Println("\n═══ Test 2: 去重存储 ═══")

	mockEmb := &mockEmbedder{}
	dd := dedup.New(st, mockEmb, dedup.DefaultConfig())

	stored := 0
	for _, mem := range allMemories {
		result, err := dd.StoreWithDedup(context.Background(), mem)
		if err != nil {
			fmt.Printf("  ⚠️ 存储失败: %v\n", err)
			continue
		}
		fmt.Printf("  → %s: %s\n", result.Action, truncate(result.Reason, 60))
		if result.Action == "stored" {
			stored++
		}
	}
	fmt.Printf("  ✅ 存储 %d / %d 条\n", stored, len(allMemories))

	// Test duplicate detection
	if len(allMemories) > 0 {
		result, _ := dd.StoreWithDedup(context.Background(), allMemories[0])
		fmt.Printf("  重复检测: %s\n", result.Action)
	}

	// ══════════════════════════════════════════
	// Test 3: Real LLM Consolidation
	// ══════════════════════════════════════════
	fmt.Println("\n═══ Test 3: 真实 LLM 记忆合并 ═══")

	count, _ := st.CountUnconsolidated()
	fmt.Printf("  未合并记忆: %d 条\n", count)

	if count >= 2 {
		cons := consolidate.New(st, consolidate.Config{
			LLMAPIKey:   apiKey,
			LLMModel:    apiModel,
			LLMEndpoint: apiEndpoint,
			LLMTimeout:  60,
			MaxMemories: 50,
		})

		start := time.Now()
		result, err := cons.Consolidate(context.Background())
		dur := time.Since(start)
		if err != nil {
			fmt.Printf("  ⚠️ 合并失败 (%.1fs): %v\n", dur.Seconds(), err)
		} else if result != nil {
			fmt.Printf("  ✅ 合并完成 (%.1fs)\n", dur.Seconds())
			fmt.Printf("     摘要: %s\n", truncate(result.Summary, 80))
			fmt.Printf("     洞察: %s\n", result.Insight)
			fmt.Printf("     模式: %s\n", result.Patterns)

			// Verify it's stored
			consolidations, _ := st.ListConsolidations(5)
			fmt.Printf("     已存储合并记录: %d 条\n", len(consolidations))

			// Check connections were updated
			newCount, _ := st.CountUnconsolidated()
			fmt.Printf("     合并后未处理: %d → %d\n", count, newCount)
		}
	} else {
		fmt.Println("  ⏭ 记忆不足，跳过合并")
	}

	// ══════════════════════════════════════════
	// Test 4: Memory Type Distribution
	// ══════════════════════════════════════════
	fmt.Println("\n═══ Test 4: 记忆类型分布 ═══")
	typeCounts := map[string]int{}
	for _, m := range allMemories {
		typeCounts[m.MemoryType]++
	}
	for t, c := range typeCounts {
		fmt.Printf("  %s: %d 条\n", t, c)
	}

	// Verify we got diverse types
	if len(typeCounts) < 2 {
		fmt.Println("  ⚠️ 类型单一，LLM 分类可能有问题")
	} else {
		fmt.Printf("  ✅ %d 种不同类型\n", len(typeCounts))
	}

	// ══════════════════════════════════════════
	// Summary
	// ══════════════════════════════════════════
	fmt.Println("\n" + strings.Repeat("═", 58))
	fmt.Println("  ✅ Real LLM Integration Test 完成")
	fmt.Printf("  提取: %d 条记忆 (%d 种类型)\n", len(allMemories), len(typeCounts))
	fmt.Printf("  存储: %d 条 (去重后)\n", stored)
	fmt.Printf("  合并: %d 条未合并记忆\n", count)
	fmt.Println(strings.Repeat("═", 58))
}

type mockEmbedder struct{}

func (m *mockEmbedder) Embed(text string) ([]float32, error) {
	r := []rune(text)
	return []float32{float32(len(r)%10) / 10.0, float32(len(r)%7) / 7.0, float32(len(r)%5) / 5.0}, nil
}
func (m *mockEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	var res [][]float32
	for _, t := range texts {
		v, _ := m.Embed(t)
		res = append(res, v)
	}
	return res, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("❌ "+format+"\n", args...)
	os.Exit(1)
}
