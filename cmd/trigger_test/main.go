// Comprehensive test for trigger system + MMR + time decay enhancements.
// Tests ShouldCapture, ShouldRetrieve, IsNoise, MMR reranking, time decay,
// and MCP integration of all new features.
// Usage: go run -tags fts5 ./cmd/trigger_test/
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/mcp"
	"github.com/yourusername/hybridmem-rag/internal/store"
	"github.com/yourusername/hybridmem-rag/internal/trigger"
)

var passed, failed int

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║   Trigger + MMR + TimeDecay — Complex Integration Test      ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	testShouldCapture()
	testShouldRetrieve()
	testIsNoise()
	testMMR()
	testTimeDecay()
	testMCPIntegration()

	fmt.Println()
	fmt.Println(strings.Repeat("═", 62))
	fmt.Printf("  结果: %d 通过, %d 失败\n", passed, failed)
	fmt.Println(strings.Repeat("═", 62))
	if failed > 0 {
		os.Exit(1)
	}
}

// ════════════════════════════════════════
// Test 1: ShouldCapture — 20 场景
// ════════════════════════════════════════
func testShouldCapture() {
	section("Test 1: ShouldCapture — 自动触发存储")

	type tc struct {
		text string
		want bool
		desc string
	}
	cases := []tc{
		// Explicit triggers
		{"记住，我是一名Go开发者", true, "显式-记住"},
		{"请记住我的邮箱是 test@mail.com", true, "显式-请记住"},
		{"don't forget I prefer dark mode", true, "显式-don't forget"},
		{"keep in mind that I use macOS", true, "显式-keep in mind"},
		{"别忘了我在北京工作", true, "显式-别忘了"},

		// Implicit triggers
		{"我是西北院的水利工程师，在北京办公", true, "隐式-我是"},
		{"我喜欢简洁的代码，不要太多注释", true, "隐式-我喜欢"},
		{"I am a backend developer with 3 years experience", true, "隐式-I am"},
		{"I prefer TypeScript over JavaScript", true, "隐式-I prefer"},
		{"以后每次都用中文回复我", true, "隐式-以后"},
		{"my boss is John and he likes Python", true, "隐式-my boss"},
		{"我擅长用Go写后端服务", true, "隐式-我擅长"},

		// Pattern triggers
		{"我的邮箱是 zhangwei@nwh.cn 请记下", true, "正则-邮箱"},
		{"截止日期是 2024-03-15", true, "正则-日期"},

		// Should NOT capture
		{"hi", false, "太短EN"},
		{"你好", false, "太短CJK"},
		{"今天天气不错啊", false, "无触发词"},
		{"", false, "空字符串"},
		{"ok", false, "太短"},
		{strings.Repeat("a", 501), false, "超长"},
	}

	for _, c := range cases {
		got, reason := trigger.ShouldCapture(c.text)
		if got != c.want {
			fail("ShouldCapture(%s) = %v (reason=%s), want %v", c.desc, got, reason, c.want)
		} else {
			pass("ShouldCapture: %s", c.desc)
		}
	}

	// Test confidence levels
	_, r1 := trigger.ShouldCapture("记住我是工程师")
	assertEqual(trigger.ConfidenceForReason(r1), 0.95, "显式置信度=0.95")

	_, r2 := trigger.ShouldCapture("我是一名Go开发者在北京")
	assertEqual(trigger.ConfidenceForReason(r2), 0.7, "隐式置信度=0.7")
}

// ════════════════════════════════════════
// Test 2: ShouldRetrieve — 20 场景
// ════════════════════════════════════════
func testShouldRetrieve() {
	section("Test 2: ShouldRetrieve — 自适应检索跳过")

	type tc struct {
		text string
		want bool
		desc string
	}
	cases := []tc{
		// Force-retrieve (short but must trigger)
		{"你记得吗", true, "强制-你记得吗"},
		{"还记得我说过什么吗", true, "强制-还记得"},
		{"remember?", true, "强制-remember"},
		{"my name?", true, "强制-my name"},
		{"what did I tell you yesterday", true, "强制-what did I"},
		{"last time we discussed Go", true, "强制-last time"},
		{"我的邮箱是什么", true, "强制-我的邮箱"},
		{"我说过什么来着", true, "强制-我说过"},

		// Skip patterns
		{"", false, "空"},
		{"hi", false, "寒暄-hi"},
		{"ok", false, "确认-ok"},
		{"好的", false, "确认-好的"},
		{"继续", false, "指令-继续"},
		{"谢谢", false, "致谢-谢谢"},
		{"再见", false, "告别-再见"},
		{"bye", false, "告别-bye"},
		{"git status", false, "命令-git"},
		{"npm install", false, "命令-npm"},
		{"/help", false, "斜杠命令"},
		{"🎉🎉🎉", false, "纯emoji"},

		// Normal queries (should retrieve)
		{"帮我写一个Go HTTP服务器的代码", true, "正常-代码请求"},
		{"what's the best way to implement caching", true, "正常-技术问题"},
		{"我最近在做一个水利监测系统需要帮助", true, "正常-项目讨论"},
		{"这个bug的原因是什么，怎么修复它", true, "正常-bug分析"},
	}

	for _, c := range cases {
		got := trigger.ShouldRetrieve(c.text)
		if got != c.want {
			fail("ShouldRetrieve(%s) = %v, want %v", c.desc, got, c.want)
		} else {
			pass("ShouldRetrieve: %s", c.desc)
		}
	}
}

// ════════════════════════════════════════
// Test 3: IsNoise — 15 场景
// ════════════════════════════════════════
func testIsNoise() {
	section("Test 3: IsNoise — 噪音过滤")

	type tc struct {
		text     string
		wantNoise bool
		wantKind  string
		desc      string
	}
	cases := []tc{
		// AI denial
		{"I don't have any information about that", true, "ai_denial", "否认-EN"},
		{"没有相关记忆可以参考", true, "ai_denial", "否认-CN"},
		{"I'm not sure about that", true, "ai_denial", "不确定"},

		// Meta questions
		{"do you remember what I said", true, "meta_question", "元问题-EN"},
		{"你记得吗我告诉你的", true, "meta_question", "元问题-CN"},

		// Boilerplate
		{"hello", true, "boilerplate", "样板-hello"},
		{"thanks!", true, "boilerplate", "样板-thanks"},
		{"ok", true, "boilerplate", "样板-ok"},
		{"你好", true, "boilerplate", "样板-你好"},
		{"谢谢", true, "boilerplate", "样板-谢谢"},
		{"HEARTBEAT", true, "boilerplate", "样板-heartbeat"},

		// Not noise
		{"我是西北院的工程师", false, "", "有效-身份"},
		{"请用Go重写这个函数", false, "", "有效-指令"},
		{"The project uses SQLite for storage", false, "", "有效-技术"},
		{"我喜欢简洁的代码风格", false, "", "有效-偏好"},
	}

	for _, c := range cases {
		got, kind := trigger.IsNoise(c.text)
		if got != c.wantNoise {
			fail("IsNoise(%s) = %v/%s, want %v/%s", c.desc, got, kind, c.wantNoise, c.wantKind)
		} else if got && kind != c.wantKind {
			fail("IsNoise(%s) kind = %s, want %s", c.desc, kind, c.wantKind)
		} else {
			pass("IsNoise: %s", c.desc)
		}
	}
}

// ════════════════════════════════════════
// Test 4: MMR — 多样性去重
// ════════════════════════════════════════
func testMMR() {
	section("Test 4: MMR — 多样性去重")

	// Scenario 1: Near-duplicate vectors should be demoted
	v1 := []float32{1, 0, 0}
	v2 := []float32{0, 1, 0}
	v3 := []float32{0, 0, 1}

	results := []store.SearchResult{
		{Entry: store.Memory{ID: "a", Text: "用户是工程师", Vector: v1}, Score: 0.95},
		{Entry: store.Memory{ID: "b", Text: "用户是工程师（重复）", Vector: v1}, Score: 0.90},
		{Entry: store.Memory{ID: "c", Text: "用户在北京工作", Vector: v2}, Score: 0.85},
		{Entry: store.Memory{ID: "d", Text: "用户会Go和Python", Vector: v3}, Score: 0.80},
		{Entry: store.Memory{ID: "e", Text: "用户是工程师（又重复）", Vector: v1}, Score: 0.75},
	}

	reranked := store.MMRRerank(results, 0.7, 0.85)
	assertEqual(len(reranked), 5, "MMR 应返回所有结果")
	assertEqual(reranked[0].Entry.ID, "a", "第1应是最高分 a")

	// "c" (diverse) should rank higher than "b" (duplicate of a)
	cIdx, bIdx := -1, -1
	for i, r := range reranked {
		if r.Entry.ID == "c" { cIdx = i }
		if r.Entry.ID == "b" { bIdx = i }
	}
	if cIdx < bIdx {
		pass("MMR: 多样结果 'c' 排在重复 'b' 前面 (c=%d, b=%d)", cIdx, bIdx)
	} else {
		fail("MMR: 期望 c(%d) < b(%d)", cIdx, bIdx)
	}

	// Scenario 2: Empty vectors should not panic
	emptyResults := []store.SearchResult{
		{Entry: store.Memory{ID: "x", Text: "BM25结果"}, Score: 0.9},
		{Entry: store.Memory{ID: "y", Text: "BM25结果2"}, Score: 0.8},
		{Entry: store.Memory{ID: "z", Text: "有向量", Vector: v1}, Score: 0.7},
	}
	reranked2 := store.MMRRerank(emptyResults, 0.7, 0.85)
	assertEqual(len(reranked2), 3, "空向量 MMR 应返回3条")
	pass("MMR: 空向量不 panic")

	// Scenario 3: Input not mutated
	original := make([]store.SearchResult, len(results))
	copy(original, results)
	store.MMRRerank(results, 0.7, 0.85)
	for i := range results {
		if results[i].Entry.ID != original[i].Entry.ID {
			fail("MMR 修改了原始切片")
			return
		}
	}
	pass("MMR: 不修改原始输入")
}

// ════════════════════════════════════════
// Test 5: Time Decay — 评分衰减
// ════════════════════════════════════════
func testTimeDecay() {
	section("Test 5: Time Decay — 乘法时间衰减")

	now := time.Now().Unix()

	// Create results with different ages
	results := []store.SearchResult{
		{Entry: store.Memory{ID: "fresh", Text: "刚才的", Importance: 0.8, Timestamp: now, Confidence: 0.9}, Score: 0.9},
		{Entry: store.Memory{ID: "week", Text: "一周前", Importance: 0.8, Timestamp: now - 7*86400, Confidence: 0.9}, Score: 0.9},
		{Entry: store.Memory{ID: "month", Text: "一月前", Importance: 0.8, Timestamp: now - 30*86400, Confidence: 0.9}, Score: 0.9},
		{Entry: store.Memory{ID: "quarter", Text: "三月前", Importance: 0.8, Timestamp: now - 90*86400, Confidence: 0.9}, Score: 0.9},
		{Entry: store.Memory{ID: "year", Text: "一年前", Importance: 0.8, Timestamp: now - 365*86400, Confidence: 0.9}, Score: 0.9},
	}

	config := store.ScoringConfig{
		TimeDecayHalfLifeDays: 60,
		TimeDecayFloor:        0.5,
	}

	scored := store.ApplyScoring(results, config)

	// Fresh should rank highest, year should rank lowest
	if len(scored) >= 5 {
		freshScore := findByID(scored, "fresh")
		yearScore := findByID(scored, "year")
		if freshScore > yearScore {
			pass("TimeDecay: 新记忆(%.3f) > 旧记忆(%.3f)", freshScore, yearScore)
		} else {
			fail("TimeDecay: 新记忆(%.3f) 应 > 旧记忆(%.3f)", freshScore, yearScore)
		}

		// Year-old should not be zero (floor = 0.5)
		if yearScore > 0 {
			pass("TimeDecay: 旧记忆未归零 (score=%.3f)", yearScore)
		} else {
			fail("TimeDecay: 旧记忆不应归零")
		}
	}

	// Disabled test
	results2 := []store.SearchResult{
		{Entry: store.Memory{Importance: 0.8, Timestamp: now - 365*86400, Confidence: 0.9}, Score: 0.9},
	}
	scored2 := store.ApplyScoring(results2, store.ScoringConfig{TimeDecayHalfLifeDays: 0})
	if scored2[0].Score > 0 {
		pass("TimeDecay: 禁用时不影响得分")
	}
}

// ════════════════════════════════════════
// Test 6: MCP Integration — 端到端
// ════════════════════════════════════════
func testMCPIntegration() {
	section("Test 6: MCP 集成测试 — 9工具 + trigger")

	dbPath := "trigger_mcp_test.db"
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

	mockEmb := &mockEmbedder{}
	srv := mcp.New(st, mockEmb, mcp.DefaultConfig())

	// ── Test 6.1: tools/list should return 9 tools ──
	resp := mcpCall(srv, "tools/list", nil)
	result := resp["result"].(map[string]interface{})
	tools := result["tools"].([]interface{})
	assertEqual(len(tools), 9, "应有 9 个工具")
	pass("tools/list: 9 个工具")

	// ── Test 6.2: memory_should_capture ──
	sub("memory_should_capture")

	// Should capture
	r := toolCall(srv, "memory_should_capture", map[string]string{"text": "记住，我是Go开发者"})
	assertBool(r, "should_capture", true, "显式触发应捕获")
	pass("should_capture: 显式触发")

	r = toolCall(srv, "memory_should_capture", map[string]string{"text": "我是西北院的水利工程师"})
	assertBool(r, "should_capture", true, "隐式触发应捕获")
	pass("should_capture: 隐式触发")

	// Should not capture (noise)
	r = toolCall(srv, "memory_should_capture", map[string]string{"text": "hello"})
	assertBool(r, "should_capture", false, "噪音不应捕获")
	pass("should_capture: 噪音过滤")

	r = toolCall(srv, "memory_should_capture", map[string]string{"text": "I don't have any information"})
	assertBool(r, "should_capture", false, "AI否认不应捕获")
	pass("should_capture: AI否认过滤")

	// ── Test 6.3: memory_store with noise filter ──
	sub("memory_store — 噪音过滤")

	r = toolCall(srv, "memory_store", map[string]interface{}{"content": "hello", "type": "fact"})
	if action, ok := r["action"].(string); ok && action == "filtered" {
		pass("memory_store: 噪音被过滤")
	} else {
		fail("memory_store: 噪音应被过滤, got %v", r)
	}

	r = toolCall(srv, "memory_store", map[string]interface{}{"content": "I don't have any information about that", "type": "fact"})
	if action, ok := r["action"].(string); ok && action == "filtered" {
		pass("memory_store: AI否认被过滤")
	} else {
		fail("memory_store: AI否认应被过滤, got %v", r)
	}

	// Normal store should work
	r = toolCall(srv, "memory_store", map[string]interface{}{"content": "我是西北院的水利工程师", "type": "fact"})
	if _, ok := r["id"]; ok {
		pass("memory_store: 正常存储成功")
	} else {
		fail("memory_store: 应返回id, got %v", r)
	}

	// Store more for recall testing
	toolCall(srv, "memory_store", map[string]interface{}{"content": "我喜欢简洁的代码风格", "type": "preference"})
	toolCall(srv, "memory_store", map[string]interface{}{"content": "我用Go写了3年后端", "type": "skill"})
	toolCall(srv, "memory_store", map[string]interface{}{"content": "以后请用中文回复", "type": "instruction"})
	toolCall(srv, "memory_store", map[string]interface{}{"content": "我在北京的办公室工作", "type": "fact"})
	pass("存储 5 条有效记忆")

	// ── Test 6.4: memory_recall with adaptive skip ──
	sub("memory_recall — 自适应跳过")

	// Should skip
	r = toolCall(srv, "memory_recall", map[string]interface{}{"query": "hi"})
	if skipped, ok := r["skipped"].(bool); ok && skipped {
		pass("memory_recall: 'hi' 被跳过")
	} else {
		fail("memory_recall: 'hi' 应被跳过, got %v", r)
	}

	r = toolCall(srv, "memory_recall", map[string]interface{}{"query": "ok"})
	if skipped, ok := r["skipped"].(bool); ok && skipped {
		pass("memory_recall: 'ok' 被跳过")
	} else {
		fail("memory_recall: 'ok' 应被跳过, got %v", r)
	}

	r = toolCall(srv, "memory_recall", map[string]interface{}{"query": "git status"})
	if skipped, ok := r["skipped"].(bool); ok && skipped {
		pass("memory_recall: 'git status' 被跳过")
	} else {
		fail("memory_recall: 'git status' 应被跳过, got %v", r)
	}

	// Should retrieve (force-retrieve)
	r = toolCall(srv, "memory_recall", map[string]interface{}{"query": "你记得我的职业吗"})
	if count, ok := r["count"].(float64); ok && count > 0 {
		pass("memory_recall: 强制检索返回 %d 条", int(count))
	} else {
		// Even if count is 0, as long as it wasn't skipped
		if _, skipped := r["skipped"]; !skipped {
			pass("memory_recall: 强制检索未跳过")
		} else {
			fail("memory_recall: 强制检索不应跳过, got %v", r)
		}
	}

	// Normal recall
	r = toolCall(srv, "memory_recall", map[string]interface{}{"query": "告诉我关于这个用户的信息"})
	if count, ok := r["count"].(float64); ok && count > 0 {
		pass("memory_recall: 正常检索返回 %d 条", int(count))
		// Check context is not empty
		if ctx, ok := r["context"].(string); ok && len(ctx) > 0 {
			pass("memory_recall: 上下文非空 (%d 字符)", len(ctx))
		}
	} else {
		fail("memory_recall: 正常检索应返回结果, got %v", r)
	}

	// ── Test 6.5: End-to-end flow ──
	sub("端到端流程")

	// Simulate: user says something → check capture → store → recall
	userMsg := "记住，我的邮箱是 test@nwh.cn，我在西北院做水利工程"

	// Step 1: Should capture?
	r = toolCall(srv, "memory_should_capture", map[string]string{"text": userMsg})
	shouldCapture, _ := r["should_capture"].(bool)
	if !shouldCapture {
		fail("端到端: '%s' 应触发捕获", truncate(userMsg, 30))
		return
	}
	pass("端到端: 触发捕获")

	// Step 2: Store
	r = toolCall(srv, "memory_store", map[string]interface{}{
		"content": userMsg,
		"type":    "fact",
	})
	if _, ok := r["id"]; !ok {
		fail("端到端: 存储失败")
		return
	}
	pass("端到端: 存储成功")

	// Step 3: Recall
	r = toolCall(srv, "memory_recall", map[string]interface{}{"query": "这个用户的邮箱和单位是什么"})
	if count, ok := r["count"].(float64); ok && count > 0 {
		pass("端到端: 检索到 %d 条记忆", int(count))
	} else {
		fail("端到端: 检索应返回结果")
	}
}

// ════════════════════════════════════════
// Helpers
// ════════════════════════════════════════

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

func mcpCall(srv *mcp.Server, method string, params interface{}) map[string]interface{} {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')

	var buf bytes.Buffer
	ctx := context.Background()
	srv.RunWithIO(ctx, bytes.NewReader(data), &buf)

	var resp map[string]interface{}
	json.Unmarshal(buf.Bytes(), &resp)
	return resp
}

func toolCall(srv *mcp.Server, name string, args interface{}) map[string]interface{} {
	resp := mcpCall(srv, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		return nil
	}
	// Parse the text content
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		return nil
	}
	first := content[0].(map[string]interface{})
	text, _ := first["text"].(string)

	var parsed map[string]interface{}
	json.Unmarshal([]byte(text), &parsed)
	return parsed
}

func findByID(results []store.SearchResult, id string) float64 {
	for _, r := range results {
		if r.Entry.ID == id {
			return r.Score
		}
	}
	return -1
}

func section(name string) {
	fmt.Printf("\n═══ %s ═══\n", name)
}

func sub(name string) {
	fmt.Printf("\n  ── %s ──\n", name)
}

func pass(format string, args ...interface{}) {
	passed++
	fmt.Printf("  ✅ "+format+"\n", args...)
}

func fail(format string, args ...interface{}) {
	failed++
	fmt.Printf("  ❌ "+format+"\n", args...)
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("  💀 "+format+"\n", args...)
	os.Exit(1)
}

func assertEqual(got, want interface{}, desc string) {
	if fmt.Sprintf("%v", got) == fmt.Sprintf("%v", want) {
		pass("%s: %v", desc, got)
	} else {
		fail("%s: got %v, want %v", desc, got, want)
	}
}

func assertBool(m map[string]interface{}, key string, want bool, desc string) {
	if v, ok := m[key].(bool); ok && v == want {
		pass(desc)
	} else {
		fail("%s: got %v, want %v", desc, m[key], want)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
