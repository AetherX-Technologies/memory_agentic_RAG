// Cosine similarity calibration test with real Qwen3 embeddings.
// Measures the distribution across 4 categories of text pairs:
//   1. Exact/near-duplicate (same fact rephrased slightly)
//   2. Related (same topic, different angles) — SHOULD connect
//   3. Weak-related (different topics but share words/style)
//   4. Unrelated (completely different)
//
// Goal: find the right band for automatic connection building.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/yourusername/hybridmem-rag/internal/bootstrap"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

type pair struct {
	category string
	a, b     string
	sim      float64
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║   Cosine Similarity Calibration — Real Qwen3 Embeddings  ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")

	dbFile := "calibration_test.db"
	os.Remove(dbFile)
	os.Setenv("MEMORY_DB_PATH", dbFile)
	defer os.Remove(dbFile)
	defer os.Remove(dbFile + "-shm")
	defer os.Remove(dbFile + "-wal")

	app, err := bootstrap.Load()
	if err != nil {
		fmt.Printf("❌ bootstrap.Load: %v\n", err)
		os.Exit(1)
	}
	defer app.Close()

	if app.Embedder == nil {
		fmt.Println("❌ Embedder is nil")
		os.Exit(1)
	}

	type p3 struct{ cat, a, b string }
	rawPairs := []p3{
		// DUPLICATE (same fact, slight rephrasing)
		{"DUPLICATE", "用户是Go后端工程师", "用户是一名Go后端开发者"},
		{"DUPLICATE", "我喜欢周末爬山", "周末我喜欢去爬山"},
		{"DUPLICATE", "项目用PostgreSQL数据库", "该项目使用PostgreSQL作为数据库"},
		{"DUPLICATE", "I work at OpenAI", "I am employed at OpenAI"},
		{"DUPLICATE", "User prefers dark mode", "User likes dark theme"},
		{"DUPLICATE", "服务部署在AWS上", "这个服务运行在AWS云平台"},
		{"DUPLICATE", "用户名叫张伟", "用户的名字是张伟"},
		{"DUPLICATE", "代码用Rust重写", "重构成Rust代码"},
		{"DUPLICATE", "每天喝三杯咖啡", "每天要喝3杯咖啡"},
		{"DUPLICATE", "The user lives in Seattle", "The user is based in Seattle"},

		// RELATED (same topic, different angles)
		{"RELATED", "用户擅长Go语言", "用户有5年Python开发经验"},
		{"RELATED", "项目用gRPC做服务间通信", "项目采用微服务架构"},
		{"RELATED", "数据库是PostgreSQL", "缓存层用Redis"},
		{"RELATED", "用户在北京工作", "用户是水利工程师"},
		{"RELATED", "喜欢爬山", "周末常去摄影"},
		{"RELATED", "I prefer Vim", "I use tmux for terminal multiplexing"},
		{"RELATED", "用户养了一只猫", "猫的名字叫小黑"},
		{"RELATED", "使用Prometheus监控", "用Grafana做可视化"},
		{"RELATED", "团队用Slack沟通", "项目管理用Jira"},
		{"RELATED", "用户喜欢喝咖啡", "用户常去星巴克"},
		{"RELATED", "部署用Docker容器", "CI/CD流水线用GitHub Actions"},
		{"RELATED", "用户是素食主义者", "用户不吃肉"},
		{"RELATED", "I play guitar", "I love rock music"},
		{"RELATED", "Database queries are slow", "Need to add indexes on user_id"},
		{"RELATED", "写了单元测试", "集成测试用Docker搭建"},
		{"RELATED", "用户在读博士", "研究方向是机器学习"},
		{"RELATED", "API限流用Redis", "限流策略是滑动窗口"},
		{"RELATED", "前端用React", "状态管理用Redux"},

		// WEAK (share words/style but different meaning)
		{"WEAK", "用户喜欢简洁的代码", "用户喜欢周末爬山"},
		{"WEAK", "项目用Go重写", "用户是Go开发者"},
		{"WEAK", "数据库用PostgreSQL", "用户住在北京"},
		{"WEAK", "以后用中文回复", "用户是中国人"},

		// UNRELATED
		{"UNRELATED", "用户擅长Go开发", "今天天气很好"},
		{"UNRELATED", "项目用PostgreSQL", "我喜欢吃火锅"},
		{"UNRELATED", "用户养了一只猫", "项目使用Kubernetes部署"},
		{"UNRELATED", "I prefer dark mode", "The meeting is at 3pm"},
		{"UNRELATED", "北京的天气很冷", "Go是一门编程语言"},
		{"UNRELATED", "用户喜欢摄影", "数据库索引优化"},
		{"UNRELATED", "I love pizza", "The server uses 8GB RAM"},
		{"UNRELATED", "孩子上小学三年级", "CPU使用率很高"},
		{"UNRELATED", "明天开会", "代码覆盖率70%"},
		{"UNRELATED", "用户喜欢红色", "部署到生产环境"},
		{"UNRELATED", "昨天看了一部电影", "Redis缓存失效"},
		{"UNRELATED", "妈妈的生日是5月", "使用OAuth认证"},
	}

	// Convert to pair with computed similarity
	pairs := make([]pair, len(rawPairs))
	for i, rp := range rawPairs {
		vecA, _ := app.Embedder.Embed(rp.a)
		vecB, _ := app.Embedder.Embed(rp.b)
		pairs[i] = pair{
			category: rp.cat,
			a:        rp.a,
			b:        rp.b,
			sim:      float64(store.CosineSimilarity(vecA, vecB)),
		}
	}

	// Print sorted by category + sim
	byCategory := map[string][]pair{}
	for _, p := range pairs {
		byCategory[p.category] = append(byCategory[p.category], p)
	}

	categories := []string{"DUPLICATE", "RELATED", "WEAK", "UNRELATED"}
	for _, cat := range categories {
		ps := byCategory[cat]
		sort.Slice(ps, func(i, j int) bool { return ps[i].sim > ps[j].sim })

		fmt.Printf("\n── %s (%d 对) ──\n", cat, len(ps))
		for _, p := range ps {
			fmt.Printf("   %.4f  %-30s  ↔  %s\n", p.sim, truncate(p.a, 28), truncate(p.b, 28))
		}

		// Stats
		min, max, sum := 1.0, 0.0, 0.0
		for _, p := range ps {
			if p.sim < min {
				min = p.sim
			}
			if p.sim > max {
				max = p.sim
			}
			sum += p.sim
		}
		avg := sum / float64(len(ps))
		fmt.Printf("   [min=%.4f max=%.4f avg=%.4f]\n", min, max, avg)
	}

	// ══════════════════════════════════════════════════════════
	// Recommendation
	// ══════════════════════════════════════════════════════════
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════")
	fmt.Println("  Analysis")
	fmt.Println("════════════════════════════════════════════════════════════")

	dupStats := statsFor(byCategory["DUPLICATE"])
	relStats := statsFor(byCategory["RELATED"])
	weakStats := statsFor(byCategory["WEAK"])
	unrelStats := statsFor(byCategory["UNRELATED"])

	fmt.Printf("\n  DUPLICATE:  min=%.3f  max=%.3f  avg=%.3f\n", dupStats.min, dupStats.max, dupStats.avg)
	fmt.Printf("  RELATED:    min=%.3f  max=%.3f  avg=%.3f\n", relStats.min, relStats.max, relStats.avg)
	fmt.Printf("  WEAK:       min=%.3f  max=%.3f  avg=%.3f\n", weakStats.min, weakStats.max, weakStats.avg)
	fmt.Printf("  UNRELATED:  min=%.3f  max=%.3f  avg=%.3f\n", unrelStats.min, unrelStats.max, unrelStats.avg)

	// Find optimal thresholds
	// Dup threshold: should be > DUPLICATE.min (so duplicates are caught)
	// Connection max: should be < DUPLICATE.min (so connections don't overlap with dups)
	// Connection min: should be > UNRELATED.max (so unrelated don't connect)
	//                  and < RELATED.min (so all related are connected)

	fmt.Println()
	fmt.Println("  Recommended thresholds:")
	recDup := dupStats.min - 0.01
	recConnMax := dupStats.min - 0.01
	recConnMin := max(unrelStats.max+0.01, relStats.min-0.02)

	fmt.Printf("    DupThreshold:      %.3f  (current: 0.93)\n", recDup)
	fmt.Printf("    ConnectionMaxSim:  %.3f  (current: 0.85)\n", recConnMax)
	fmt.Printf("    ConnectionMinSim:  %.3f  (current: 0.70)\n", recConnMin)

	// How many RELATED pairs would be caught in the recommended band?
	caught := 0
	missed := 0
	for _, p := range byCategory["RELATED"] {
		if p.sim >= recConnMin && p.sim < recConnMax {
			caught++
		} else {
			missed++
			fmt.Printf("    ⚠️  RELATED miss: sim=%.3f  %s ↔ %s\n", p.sim, truncate(p.a, 25), truncate(p.b, 25))
		}
	}
	fmt.Printf("\n  RELATED coverage: %d/%d in recommended band\n", caught, len(byCategory["RELATED"]))

	// How many UNRELATED would false-positive?
	fp := 0
	for _, p := range byCategory["UNRELATED"] {
		if p.sim >= recConnMin && p.sim < recConnMax {
			fp++
			fmt.Printf("    ⚠️  UNRELATED false+: sim=%.3f  %s ↔ %s\n", p.sim, truncate(p.a, 25), truncate(p.b, 25))
		}
	}
	fmt.Printf("  UNRELATED false+: %d/%d\n", fp, len(byCategory["UNRELATED"]))
}

type stats struct{ min, max, avg float64 }

func statsFor(ps []pair) stats {
	if len(ps) == 0 {
		return stats{}
	}
	s := stats{min: 1.0, max: 0.0}
	for _, p := range ps {
		if p.sim < s.min {
			s.min = p.sim
		}
		if p.sim > s.max {
			s.max = p.sim
		}
		s.avg += p.sim
	}
	s.avg /= float64(len(ps))
	return s
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}
