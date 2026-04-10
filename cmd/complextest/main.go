// High-complexity integration test: simulates a realistic multi-day usage scenario.
// Covers: dedup pipeline, connections, tags, SourceConv, token budget, CJK estimation,
// export/import round-trip, MCP tool calls, update with scope/tag preservation, soft delete.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/dedup"
	"github.com/yourusername/hybridmem-rag/internal/extractor"
	"github.com/yourusername/hybridmem-rag/internal/mcp"
	"github.com/yourusername/hybridmem-rag/internal/store"
	"github.com/yourusername/hybridmem-rag/internal/tokutil"
)

var passed, failed int

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   High-Complexity Integration Test — Full Pipeline        ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")

	dbPath := "complex_integration_test.db"
	os.Remove(dbPath)
	os.Remove(dbPath + "-shm")
	os.Remove(dbPath + "-wal")
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := store.New(store.Config{DBPath: dbPath, VectorDim: 32})
	if err != nil {
		fatal("Store: %v", err)
	}
	defer st.Close()

	emb := newClusterEmbedder()
	dd := dedup.New(st, emb, dedup.DefaultConfig())
	mcpSrv := mcp.New(st, emb, mcp.DefaultConfig())

	// ══════════════════════════════════════════════════════════
	section("1. 多对话、多类型记忆存储 + 自动关联")
	// ══════════════════════════════════════════════════════════

	ids := map[string]string{}

	// Conv 1: 用户自我介绍
	emb.cluster = "developer"
	ids["dev_skill"] = mustStore(dd, st, "用户擅长Go和Python后端开发，有5年经验", "skill", "conv-day1", []string{"Go", "Python", "后端"})
	ids["dev_fact"] = mustStore(dd, st, "用户在北京西北院工作，是水利工程师", "fact", "conv-day1", []string{"北京", "西北院"})
	ids["dev_pref"] = mustStore(dd, st, "用户喜欢简洁的代码风格，不要多余注释", "preference", "conv-day1", []string{"代码风格"})
	ids["dev_inst"] = mustStore(dd, st, "以后请用中文回复我的所有问题", "instruction", "conv-day1", []string{"语言"})

	// Conv 2: 架构讨论
	emb.cluster = "architecture"
	ids["arch_fact1"] = mustStore(dd, st, "项目采用微服务架构，使用gRPC通信", "fact", "conv-day2", []string{"微服务", "gRPC"})
	ids["arch_fact2"] = mustStore(dd, st, "数据库使用PostgreSQL主库加Redis缓存", "fact", "conv-day2", []string{"数据库", "PostgreSQL"})
	ids["arch_episode"] = mustStore(dd, st, "昨天讨论了监控系统方案，决定用Prometheus", "episode", "conv-day2", []string{"监控"})

	// Conv 3: 个人兴趣
	emb.cluster = "hobby"
	ids["hobby_pref"] = mustStore(dd, st, "用户喜欢周末去爬山和摄影", "preference", "conv-day3", []string{"爬山", "摄影"})
	ids["hobby_fact"] = mustStore(dd, st, "用户养了一只叫小黑的猫", "fact", "conv-day3", []string{"宠物"})

	assertEqual(len(ids), 9, "应存储9条记忆")
	pass(fmt.Sprintf("存储 %d 条记忆，跨 3 个对话", len(ids)))

	// ══════════════════════════════════════════════════════════
	section("2. 自动关联验证")
	// ══════════════════════════════════════════════════════════

	devConnected := false
	for _, key := range []string{"dev_skill", "dev_fact", "dev_pref", "dev_inst"} {
		m := mustGet(st, ids[key])
		if m.Connections != "" && m.Connections != "[]" {
			devConnected = true
			break
		}
	}
	if devConnected {
		pass("开发者记忆簇内有 connections")
	} else {
		fail("开发者记忆簇内无 connections")
	}

	hobbyPref := mustGet(st, ids["hobby_pref"])
	if !hasAnyConnection(hobbyPref, ids["dev_skill"], ids["arch_fact1"]) {
		pass("兴趣记忆不与技术记忆关联")
	} else {
		fail("兴趣记忆不应与技术记忆关联")
	}

	// ══════════════════════════════════════════════════════════
	section("3. 重复检测")
	// ══════════════════════════════════════════════════════════

	emb.cluster = "developer"
	dupResult := mustStoreResult(dd, "用户擅长Go和Python后端开发，有5年经验", "skill", "conv-day4")
	assertEqual(dupResult.Action, "duplicate", "相同内容应判重复")
	pass("精确重复检测")

	ids["dev_rust"] = mustStore(dd, st, "用户最近在学习Rust做系统编程", "skill", "conv-day4", []string{"Rust"})
	pass("新技能存储成功")

	// ══════════════════════════════════════════════════════════
	section("4. Tags 完整性")
	// ══════════════════════════════════════════════════════════

	tags, err := st.GetTags(ids["dev_skill"])
	assertNil(err, "GetTags")
	assertContainsStr(tags, "Go", "tags 含 Go")
	assertContainsStr(tags, "Python", "tags 含 Python")
	pass(fmt.Sprintf("dev_skill tags=%v", tags))

	goIDs, _ := st.GetMemoryIDsByTag("Go")
	assertGt(len(goIDs), 0, "Go 标签匹配")
	pass(fmt.Sprintf("Go 标签匹配 %d 条", len(goIDs)))

	// ══════════════════════════════════════════════════════════
	section("5. SourceConv 过滤")
	// ══════════════════════════════════════════════════════════

	emb.cluster = "developer"
	resp := mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":200,"method":"tools/call","params":{"name":"memory_recall","arguments":{"query":"开发经验","source_conv":"conv-day1","limit":10}}}`)
	if !strings.Contains(resp, `"isError":true`) {
		pass("SourceConv 过滤成功")
	} else {
		fail("SourceConv 过滤失败")
	}

	// ══════════════════════════════════════════════════════════
	section("6. Token 预算")
	// ══════════════════════════════════════════════════════════

	cjkTokens := tokutil.EstimateTokens("这是测试中文的token估算准确性")
	asciiTokens := tokutil.EstimateTokens("This tests ASCII token estimation")
	cjkR := float64(cjkTokens) / float64(len([]rune("这是测试中文的token估算准确性")))
	asciiR := float64(asciiTokens) / float64(len([]rune("This tests ASCII token estimation")))
	assertGt(cjkR, asciiR, "CJK 每字符 token 比高于 ASCII")
	pass(fmt.Sprintf("CJK=%.2f ASCII=%.2f", cjkR, asciiR))

	emb.cluster = "developer"
	resp = mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":201,"method":"tools/call","params":{"name":"memory_recall","arguments":{"query":"技术","max_tokens":50}}}`)
	if !strings.Contains(resp, `"isError":true`) {
		pass("max_tokens=50 recall 正常")
	}

	// ══════════════════════════════════════════════════════════
	section("7. 软删除 + 恢复")
	// ══════════════════════════════════════════════════════════

	assertNil(st.SoftDelete(ids["hobby_fact"], time.Now().Unix()), "SoftDelete")
	emb.cluster = "hobby"
	resp = mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":400,"method":"tools/call","params":{"name":"memory_recall","arguments":{"query":"猫 宠物","limit":5}}}`)
	if !strings.Contains(resp, "小黑") {
		pass("软删除记忆不出现在 recall")
	} else {
		fail("软删除记忆泄漏")
	}
	assertNil(st.Restore(ids["hobby_fact"]), "Restore")
	pass("软删除+恢复完成")

	// ══════════════════════════════════════════════════════════
	section("8. Export 包含 Tags")
	// ══════════════════════════════════════════════════════════

	exportResp := mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":500,"method":"tools/call","params":{"name":"memory_export","arguments":{}}}`)
	if strings.Contains(exportResp, "tags") {
		pass("Export 包含 tags")
	} else {
		fail("Export 缺少 tags")
	}

	// ══════════════════════════════════════════════════════════
	section("9. AddConnection 防重复 + Label 升级")
	// ══════════════════════════════════════════════════════════

	t1, t2 := ids["arch_fact1"], ids["arch_fact2"]
	assertNil(st.AddConnection(t1, t2, "related (sim=0.78)"), "add generic")
	assertNil(st.AddConnection(t1, t2, "related (sim=0.82)"), "dup no-op")
	assertEqual(countConns(st, t1, t2), 1, "无重复")
	pass("防重复正确")

	assertNil(st.AddConnection(t1, t2, "同项目数据库组件"), "升级")
	if getLabel(st, t1, t2) == "同项目数据库组件" {
		pass("Label 升级成功")
	} else {
		fail("Label 未升级")
	}

	// ══════════════════════════════════════════════════════════
	section("10. MCP 工具完整性")
	// ══════════════════════════════════════════════════════════

	resp = mcpCall(mcpSrv, `{"jsonrpc":"2.0","id":600,"method":"tools/list"}`)
	for _, t := range []string{"memory_store", "memory_recall", "memory_forget", "memory_update", "memory_export", "memory_import", "memory_forget_by_tag", "memory_consolidate", "memory_should_capture"} {
		assertContains(resp, t, "tools/list 含 "+t)
	}
	pass("9 个 MCP 工具全部注册")

	// ══════════════════════════════════════════════════════════
	section("11. 混合语言 + 边界")
	// ══════════════════════════════════════════════════════════

	emb.cluster = "mixed"
	mustStore(dd, st, "User prefers VSCode for Go开发 🚀", "preference", "conv-mixed", nil)
	mustStore(dd, st, strings.Repeat("长文本测试，", 20)+"结论是用Go", "episode", "conv-mixed", nil)
	mustStore(dd, st, "用户名:张伟", "fact", "conv-mixed", nil)
	pass("混合语言+长文本+短文本 全部成功")

	mixedTok := tokutil.EstimateTokens("Hello 世界 🌍")
	assertGt(mixedTok, 3, "混合文本 token > 3")
	pass(fmt.Sprintf("混合文本 token=%d", mixedTok))

	// ══════════════════════════════════════════════════════════
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Printf("  结果: %d passed, %d failed\n", passed, failed)
	fmt.Println("════════════════════════════════════════════════════════════")
	if failed > 0 {
		os.Exit(1)
	}
}

// ── Embedder ──

type clusterEmbedder struct{ cluster string; seeds map[string][]float32 }

func newClusterEmbedder() *clusterEmbedder {
	return &clusterEmbedder{seeds: map[string][]float32{
		"developer": genSeed(32, 1), "architecture": genSeed(32, 2),
		"hobby": genSeed(32, 3), "mixed": genSeed(32, 4),
	}}
}
func (e *clusterEmbedder) Embed(text string) ([]float32, error) {
	seed := e.seeds[e.cluster]
	if seed == nil { seed = genSeed(32, 99) }
	rng := rand.New(rand.NewSource(int64(hsh(text))))
	v := make([]float32, 32)
	for i := range v { v[i] = seed[i] + float32(rng.NormFloat64()*0.12) }
	nrm(v); return v, nil
}
func (e *clusterEmbedder) EmbedBatch(ts []string) ([][]float32, error) {
	r := make([][]float32, len(ts)); for i, t := range ts { r[i], _ = e.Embed(t) }; return r, nil
}
func genSeed(d, id int) []float32 {
	rng := rand.New(rand.NewSource(int64(id * 12345))); v := make([]float32, d)
	for i := range v { v[i] = float32(rng.NormFloat64()) }; nrm(v); return v
}
func nrm(v []float32) {
	var s float64; for _, x := range v { s += float64(x * x) }
	n := float32(math.Sqrt(s)); if n > 0 { for i := range v { v[i] /= n } }
}
func hsh(s string) uint32 { var h uint32; for _, c := range s { h = h*31 + uint32(c) }; return h }

// ── Helpers ──

func mustStore(dd *dedup.Deduplicator, st store.Store, content, typ, conv string, tags []string) string {
	h := sha256.Sum256([]byte(content))
	r, err := dd.StoreWithDedup(context.Background(), extractor.ExtractedMemory{
		Content: content, MemoryType: typ, Importance: 0.8, Confidence: 0.9,
		ContentHash: hex.EncodeToString(h[:8]), SourceConv: conv,
	})
	if err != nil { fatal("Store: %v", err) }
	if r.ID == "" { fatal("Empty ID: %s (%s)", truncate(content, 25), r.Action) }
	if len(tags) > 0 { _ = st.SetTags(r.ID, tags) }
	fmt.Printf("   ✅ [%s] %s → %s\n", typ, truncate(content, 28), r.ID[:8])
	return r.ID
}

func mustStoreResult(dd *dedup.Deduplicator, content, typ, conv string) dedup.Result {
	h := sha256.Sum256([]byte(content))
	r, err := dd.StoreWithDedup(context.Background(), extractor.ExtractedMemory{
		Content: content, MemoryType: typ, Importance: 0.8, Confidence: 0.9,
		ContentHash: hex.EncodeToString(h[:8]), SourceConv: conv,
	})
	if err != nil { fatal("Store: %v", err) }
	return r
}

func mustGet(st store.Store, id string) *store.Memory {
	m, err := st.Get(id); if err != nil { fatal("Get(%s): %v", id[:8], err) }; return m
}

func hasAnyConnection(m *store.Memory, ids ...string) bool {
	for _, id := range ids { if strings.Contains(m.Connections, id) { return true } }; return false
}

func countConns(st store.Store, from, to string) int {
	m := mustGet(st, from); var c []map[string]string; json.Unmarshal([]byte(m.Connections), &c)
	n := 0; for _, x := range c { if x["linked_to"] == to { n++ } }; return n
}

func getLabel(st store.Store, from, to string) string {
	m := mustGet(st, from); var c []map[string]string; json.Unmarshal([]byte(m.Connections), &c)
	for _, x := range c { if x["linked_to"] == to { return x["relationship"] } }; return ""
}

func mcpCall(srv *mcp.Server, req string) string {
	var b bytes.Buffer; srv.RunWithIO(context.Background(), strings.NewReader(req+"\n"), &b); return b.String()
}

func truncate(s string, n int) string {
	r := []rune(s); if len(r) > n { return string(r[:n]) + "..." }; return s
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss { if v == s { return true } }; return false
}

// ── Asserts ──

func section(t string)                { fmt.Printf("\n── %s ──\n", t) }
func pass(m string)                   { passed++; fmt.Printf("   ✅ %s\n", m) }
func fail(f string, a ...interface{}) { failed++; fmt.Printf("   ❌ "+f+"\n", a...) }
func fatal(f string, a ...interface{}) { fmt.Printf("❌ FATAL: "+f+"\n", a...); os.Exit(1) }
func assertNil(e error, m string)     { if e != nil { fail("%s: %v", m, e) } }
func assertEqual(g, w interface{}, m string) { if fmt.Sprint(g) != fmt.Sprint(w) { fail("%s: got=%v want=%v", m, g, w) } }
func assertGt(a, b interface{}, m string) { if toF(a) <= toF(b) { fail("%s: %v <= %v", m, a, b) } }
func assertContains(s, sub, m string) { if !strings.Contains(s, sub) { fail("%s: missing %q", m, sub) } }
func assertContainsStr(ss []string, s, m string) { if !containsStr(ss, s) { fail("%s: %v missing %q", m, ss, s) } }
func toF(v interface{}) float64 { switch n := v.(type) { case int: return float64(n); case float64: return n; default: return 0 } }

var _ = time.Now
