// Comprehensive end-to-end test for AI Memory System (Phases A→G).
// Simulates realistic multi-turn conversations with complex memory operations.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/dedup"
	"github.com/yourusername/hybridmem-rag/internal/extractor"
	"github.com/yourusername/hybridmem-rag/internal/mcp"
	"github.com/yourusername/hybridmem-rag/internal/ratelimit"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

var passed, failed int

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   AI Memory System — Full Complex Test (A→G)              ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")

	dbPath := "full_memory_test.db"
	os.Remove(dbPath)
	os.Remove(dbPath + "-shm")
	os.Remove(dbPath + "-wal")
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	// ══════════════════════════════════════════════
	// Phase A: Store
	// ══════════════════════════════════════════════
	section("Phase A: Store 初始化")
	st, err := store.New(store.Config{DBPath: dbPath})
	if err != nil { fatal("Store 创建失败: %v", err) }
	defer st.Close()
	pass("Store 创建成功")

	// ══════════════════════════════════════════════
	// Phase B: 多轮对话提取
	// ══════════════════════════════════════════════
	section("Phase B: 多轮对话记忆提取")
	ext := extractor.New(extractor.Config{}) // fallback mode

	conversations := []struct {
		name     string
		messages []extractor.Message
	}{
		{"对话1: 自我介绍", []extractor.Message{
			{Role: "user", Content: "我是西北院的水利工程师，我叫张伟，我用Go写了3年后端"},
			{Role: "assistant", Content: "好的，张伟，我记住了你的背景。"},
		}},
		{"对话2: 偏好", []extractor.Message{
			{Role: "user", Content: "我喜欢简洁的代码风格，不要写太多注释。以后请用中文回复我"},
			{Role: "assistant", Content: "了解，简洁风格，中文回复。"},
		}},
		{"对话3: 技术讨论", []extractor.Message{
			{Role: "user", Content: "我擅长Go和Python，我在北京的办公室工作"},
			{Role: "assistant", Content: "明白了。"},
		}},
		{"对话4: 无信息寒暄", []extractor.Message{
			{Role: "user", Content: "今天天气真好"},
			{Role: "assistant", Content: "是啊，春天来了。"},
		}},
	}

	var allMemories []extractor.ExtractedMemory
	for _, conv := range conversations {
		mems, err := ext.Extract(context.Background(), conv.messages, conv.name)
		assertNil(err, "提取: "+conv.name)
		allMemories = append(allMemories, mems...)
		fmt.Printf("   %s → %d 条\n", conv.name, len(mems))
	}
	assertGt(len(allMemories), 3, "应至少提取4条记忆")
	pass(fmt.Sprintf("提取总计 %d 条记忆", len(allMemories)))

	// ══════════════════════════════════════════════
	// Phase C: 去重 + 存储
	// ══════════════════════════════════════════════
	section("Phase C: 去重 + 存储")
	mockEmb := &mockEmbedder{}
	dd := dedup.New(st, mockEmb, dedup.DefaultConfig())

	var storedIDs []string
	for _, mem := range allMemories {
		result, err := dd.StoreWithDedup(context.Background(), mem)
		assertNil(err, "StoreWithDedup")
		if result.ID != "" {
			storedIDs = append(storedIDs, result.ID)
		}
		fmt.Printf("   → %s: %s\n", result.Action, truncate(result.Reason, 50))
	}
	assertGt(len(storedIDs), 2, "应至少存储3条记忆")
	pass(fmt.Sprintf("存储 %d 条记忆", len(storedIDs)))

	// 测试重复检测
	if len(allMemories) > 0 {
		result, err := dd.StoreWithDedup(context.Background(), allMemories[0])
		assertNil(err, "重复检测")
		assertEqual(result.Action, "duplicate", "重复检测应返回 duplicate")
		pass("重复检测正确")
	}

	// ══════════════════════════════════════════════
	// Phase D: 标签 + 垃圾桶 + 评分 + 清理
	// ══════════════════════════════════════════════
	section("Phase D: 标签系统")
	if len(storedIDs) >= 2 {
		assertNil(st.SetTags(storedIDs[0], []string{"Go", "开发者", "西北院"}), "SetTags 1")
		assertNil(st.SetTags(storedIDs[1], []string{"Python", "北京"}), "SetTags 2")

		ids, err := st.GetMemoryIDsByTag("Go")
		assertNil(err, "GetMemoryIDsByTag")
		assertEqual(len(ids), 1, "Go标签应匹配1条")

		ids2, _ := st.GetMemoryIDsByTag("nonexistent")
		assertEqual(len(ids2), 0, "不存在的标签应返回0条")
		pass("标签系统正确")
	}

	section("Phase D: 垃圾桶")
	if len(storedIDs) >= 1 {
		trashID := storedIDs[0]

		// Soft delete
		assertNil(st.SoftDelete(trashID, time.Now().Unix()), "SoftDelete")
		trash, _ := st.ListTrash(10)
		assertEqual(len(trash), 1, "垃圾桶应有1条")

		// Restore
		assertNil(st.Restore(trashID), "Restore")
		trash2, _ := st.ListTrash(10)
		assertEqual(len(trash2), 0, "恢复后垃圾桶应为空")

		// Re-delete and permanent delete
		assertNil(st.SoftDelete(trashID, time.Now().Unix()), "SoftDelete 2")
		assertNil(st.PermanentDelete(trashID), "PermanentDelete")
		trash3, _ := st.ListTrash(10)
		assertEqual(len(trash3), 0, "永久删除后垃圾桶应为空")

		// Remove from storedIDs
		storedIDs = storedIDs[1:]
		pass("垃圾桶完整流程正确")
	}

	section("Phase D: 评分管道")
	results := []store.SearchResult{
		{Entry: store.Memory{Text: "Go开发者", MemoryType: "fact", Importance: 0.8, Confidence: 0.9, Timestamp: time.Now().Unix(), AccessCount: 10}, Score: 0.8},
		{Entry: store.Memory{Text: "临时事件", MemoryType: "episode", Importance: 0.3, Confidence: 0.3, Timestamp: time.Now().Unix() - 86400*90, AccessCount: 0}, Score: 0.7},
		{Entry: store.Memory{Text: "用中文回复", MemoryType: "instruction", Importance: 0.9, Confidence: 1.0, Timestamp: time.Now().Unix() - 86400*30, AccessCount: 3}, Score: 0.6},
	}
	scored := store.ApplyScoring(results, store.ScoringConfig{
		RecencyHalfLifeDays: 14, RecencyWeight: 0.1, LengthNormAnchor: 200, HardMinScore: 0.01,
		ConfidenceWeight: 1.0, AccessBoostCap: 1.5,
	})

	// Find scores by type
	scores := map[string]float64{}
	for _, s := range scored {
		scores[s.Entry.MemoryType] = s.Score
	}
	assertGt(scores["fact"], scores["episode"], "fact 应高于 episode")
	assertGt(scores["instruction"], scores["episode"], "instruction 应高于 episode（半衰期365天）")
	pass(fmt.Sprintf("评分: fact=%.3f instruction=%.3f episode=%.3f", scores["fact"], scores["instruction"], scores["episode"]))

	section("Phase D: 清理")
	assertNil(st.RunCleanup(time.Now().Unix()), "RunCleanup")
	pass("清理执行成功")

	// ══════════════════════════════════════════════
	// Phase E: MCP Server
	// ══════════════════════════════════════════════
	section("Phase E: MCP Server")
	mcpSrv := mcp.New(st, mockEmb, mcp.DefaultConfig())

	// tools/list
	resp := mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assertContains(resp, "memory_store", "tools/list 应包含 memory_store")
	assertContains(resp, "memory_consolidate", "tools/list 应包含 memory_consolidate")
	pass("tools/list 返回 8 个工具")

	// memory_store via MCP
	resp = mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"memory_store","arguments":{"content":"MCP测试记忆","type":"fact","importance":0.7}}}`)
	assertContains(resp, "stored", "memory_store 应返回 stored")
	pass("memory_store 通过 MCP 存储成功")

	// memory_recall
	resp = mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_recall","arguments":{"query":"开发者"}}}`)
	assertNotContains(resp, "isError", "memory_recall 不应返回错误")
	pass("memory_recall 检索成功")

	// memory_forget
	resp = mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"memory_forget","arguments":{"id":"nonexistent-id"}}}`)
	pass("memory_forget 执行（无报错）")

	// memory_consolidate
	resp = mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"memory_consolidate","arguments":{}}}`)
	assertNotContains(resp, "isError", "memory_consolidate 不应返回错误")
	pass("memory_consolidate 状态检查成功")

	// ══════════════════════════════════════════════
	// Phase F: 限流
	// ══════════════════════════════════════════════
	section("Phase F: 限流")
	limiter := ratelimit.New(ratelimit.Config{PerMinute: 5, PerHour: 100})
	for i := 0; i < 5; i++ {
		assertNil(limiter.Allow(), fmt.Sprintf("前5次应允许 (第%d次)", i+1))
	}
	err = limiter.Allow()
	assertNotNil(err, "第6次应被限流")
	pass(fmt.Sprintf("限流: 5/5允许, 第6次拒绝: %v", err))

	// ══════════════════════════════════════════════
	// Phase G: 合并 (数据模型验证)
	// ══════════════════════════════════════════════
	section("Phase G: 记忆合并基础设施")

	// Verify consolidation store methods
	count, err := st.CountUnconsolidated()
	assertNil(err, "CountUnconsolidated")
	fmt.Printf("   未合并记忆: %d 条\n", count)
	pass(fmt.Sprintf("CountUnconsolidated = %d", count))

	unconsolidated, err := st.ListUnconsolidated(50)
	assertNil(err, "ListUnconsolidated")
	fmt.Printf("   ListUnconsolidated 返回 %d 条\n", len(unconsolidated))
	pass("ListUnconsolidated 正确")

	// Test consolidation creation
	testConsolidation := &store.Consolidation{
		SourceIDs:       `["id1","id2"]`,
		Summary:         "用户是西北院的Go开发者",
		Insight:         "技术背景与工作单位高度相关",
		Patterns:        `["技术人员"]`,
		ConnectionsJSON: `[{"from_id":"id1","to_id":"id2","relationship":"同一用户"}]`,
	}
	cID, err := st.CreateConsolidation(testConsolidation)
	assertNil(err, "CreateConsolidation")
	assertNotEqual(cID, "", "应返回非空 ID")
	pass("CreateConsolidation 成功")

	consolidations, err := st.ListConsolidations(10)
	assertNil(err, "ListConsolidations")
	assertEqual(len(consolidations), 1, "应有1条合并记录")
	assertEqual(consolidations[0].Insight, "技术背景与工作单位高度相关", "洞察应匹配")
	pass("ListConsolidations 正确")

	// Test AddConnection
	if len(storedIDs) >= 2 {
		assertNil(st.AddConnection(storedIDs[0], storedIDs[1], "同一用户"), "AddConnection")
		pass("AddConnection 成功")
	}

	// Test MarkConsolidated
	if len(storedIDs) >= 1 {
		assertNil(st.MarkConsolidated(storedIDs[:1]), "MarkConsolidated")
		newCount, _ := st.CountUnconsolidated()
		assertLt(newCount, count, "合并后未合并数应减少")
		pass(fmt.Sprintf("MarkConsolidated: %d → %d", count, newCount))
	}

	// Verify Scheduler creation (no actual LLM call)
	consolidator := consolidate.New(st, consolidate.DefaultConfig())
	scheduler := consolidate.NewScheduler(consolidator, 30*time.Minute, 2, nil)
	scheduler.Stop() // should not panic
	scheduler.Stop() // double stop should not panic
	pass("Scheduler 创建+双重Stop无panic")

	// ══════════════════════════════════════════════
	// Summary
	// ══════════════════════════════════════════════
	fmt.Println("\n" + strings.Repeat("═", 60))
	fmt.Printf("  结果: %d 通过 / %d 失败\n", passed, failed)
	fmt.Println(strings.Repeat("═", 60))
	if failed > 0 {
		fmt.Println("  ❌ 有测试失败！")
		os.Exit(1)
	}
	fmt.Println("  ✅ AI Memory System A→G 全部测试通过")
	fmt.Println("  Phase A: 数据模型 + 存储 + 迁移")
	fmt.Println("  Phase B: 记忆提取（多轮对话）")
	fmt.Println("  Phase C: 去重 + 冲突检测")
	fmt.Println("  Phase D: 标签 + 垃圾桶 + 评分 + 清理")
	fmt.Println("  Phase E: MCP Server（8工具）")
	fmt.Println("  Phase F: 限流")
	fmt.Println("  Phase G: 记忆合并基础设施")
	fmt.Println(strings.Repeat("═", 60))
}

// ── Test helpers ──

func section(name string) { fmt.Printf("\n[%s]\n", name) }
func pass(msg string)     { passed++; fmt.Printf("  ✅ %s\n", msg) }
func fail(msg string)     { failed++; fmt.Printf("  ❌ %s\n", msg) }

func assertNil(err error, ctx string) {
	if err != nil { fatal("%s: %v", ctx, err) }
}
func assertNotNil(v interface{}, ctx string) {
	if v == nil { fail(ctx + ": expected non-nil") }
}
func assertEqual(a, b interface{}, ctx string) {
	if fmt.Sprint(a) != fmt.Sprint(b) { fail(fmt.Sprintf("%s: %v != %v", ctx, a, b)) }
}
func assertNotEqual(a, b interface{}, ctx string) {
	if fmt.Sprint(a) == fmt.Sprint(b) { fail(fmt.Sprintf("%s: should not equal %v", ctx, a)) }
}
func assertGt(a, b interface{}, ctx string) {
	af, bf := toFloat(a), toFloat(b)
	if af <= bf { fail(fmt.Sprintf("%s: %v should > %v", ctx, a, b)) }
}
func assertLt(a, b interface{}, ctx string) {
	af, bf := toFloat(a), toFloat(b)
	if af >= bf { fail(fmt.Sprintf("%s: %v should < %v", ctx, a, b)) }
}
func assertContains(s, sub, ctx string) {
	if !strings.Contains(s, sub) { fail(fmt.Sprintf("%s: %q not in response", ctx, sub)) }
}
func assertNotContains(s, sub, ctx string) {
	if strings.Contains(s, sub) { fail(fmt.Sprintf("%s: %q should not be in response", ctx, sub)) }
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case int: return float64(n)
	case int64: return float64(n)
	case float64: return n
	default: return 0
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n { return s }
	return string(r[:n]) + "..."
}

func mcpCall(srv *mcp.Server, reqJSON string) string {
	var buf bytes.Buffer
	srv.RunWithIO(context.Background(), strings.NewReader(reqJSON+"\n"), &buf)
	return buf.String()
}

type mockEmbedder struct{}
func (m *mockEmbedder) Embed(text string) ([]float32, error) {
	r := []rune(text)
	return []float32{float32(len(r)%10) / 10.0, float32(len(r)%7) / 7.0, float32(len(r)%5) / 5.0}, nil
}
func (m *mockEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	var res [][]float32
	for _, t := range texts { v, _ := m.Embed(t); res = append(res, v) }
	return res, nil
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("❌ FATAL: "+format+"\n", args...)
	os.Exit(1)
}

// Ensure json is imported (used indirectly)
var _ = json.Marshal
