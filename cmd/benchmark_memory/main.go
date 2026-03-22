// Performance benchmark for the AI Memory System.
// Tests Insert, VectorSearch, HybridSearch, Export, and Cleanup at different scales.
// Usage: go run -tags fts5 ./cmd/benchmark_memory/
package main

import (
	"fmt"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

const vectorDim = 128

func main() {
	fmt.Println("╔════════════════════════════════════════════════════╗")
	fmt.Println("║   Memory System Performance Benchmark             ║")
	fmt.Println("╚════════════════════════════════════════════════════╝")

	sizes := []int{100, 1000, 10000}

	fmt.Printf("\n%-8s %10s %10s %10s %10s %10s\n",
		"Size", "Insert", "Vector", "Hybrid", "Export", "Cleanup")
	fmt.Println("──────── ────────── ────────── ────────── ────────── ──────────")

	for _, size := range sizes {
		dbPath := fmt.Sprintf("/tmp/bench_%d.db", size)
		os.Remove(dbPath)

		st, err := store.New(store.Config{DBPath: dbPath, VectorDim: vectorDim})
		if err != nil {
			fmt.Printf("❌ Failed to create store for size %d: %v\n", size, err)
			continue
		}

		rng := rand.New(rand.NewSource(42))

		// ── Insert ──
		insertTimes := make([]time.Duration, 0, 100)
		for i := 0; i < size; i++ {
			mem := &store.Memory{
				Text:       fmt.Sprintf("记忆内容 #%d - %s", i, randomText(rng, 50)),
				Category:   "memory",
				Scope:      "global",
				Importance: 0.5 + rng.Float64()*0.5,
				MemoryType: randomType(rng),
				Confidence: 0.5 + rng.Float64()*0.5,
				Vector:     randomVector(rng, vectorDim),
			}
			start := time.Now()
			st.Insert(mem)
			dur := time.Since(start)
			// Only track last 100 inserts (after DB is warm)
			if i >= size-100 {
				insertTimes = append(insertTimes, dur)
			}
		}
		insertP50 := percentile(insertTimes, 50)

		// ── VectorSearch ──
		queryVec := randomVector(rng, vectorDim)
		searchTimes := make([]time.Duration, 10)
		for i := range searchTimes {
			start := time.Now()
			st.VectorSearch(queryVec, 10, nil)
			searchTimes[i] = time.Since(start)
		}
		vectorP50 := percentile(searchTimes, 50)

		// ── HybridSearch ──
		hybridTimes := make([]time.Duration, 10)
		for i := range hybridTimes {
			start := time.Now()
			st.HybridSearch(queryVec, "记忆内容", 10, nil)
			hybridTimes[i] = time.Since(start)
		}
		hybridP50 := percentile(hybridTimes, 50)

		// ── Export ──
		start := time.Now()
		st.List("global", size)
		exportDur := time.Since(start)

		// ── Cleanup ──
		start = time.Now()
		st.RunCleanup(time.Now().Unix())
		cleanupDur := time.Since(start)

		st.Close()
		os.Remove(dbPath)

		fmt.Printf("%-8d %10s %10s %10s %10s %10s\n",
			size,
			fmtDur(insertP50), fmtDur(vectorP50), fmtDur(hybridP50),
			fmtDur(exportDur), fmtDur(cleanupDur))
	}

	fmt.Println("\n(Insert=p50 last 100, Vector/Hybrid=p50 of 10 queries, Export/Cleanup=single run)")
}

func randomVector(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = rng.Float32()*2 - 1
	}
	return v
}

func randomText(rng *rand.Rand, length int) string {
	chars := []rune("这是一段随机生成的测试文本用于性能基准测试包含各种中文字符和标点符号")
	out := make([]rune, length)
	for i := range out {
		out[i] = chars[rng.Intn(len(chars))]
	}
	return string(out)
}

func randomType(rng *rand.Rand) string {
	types := []string{"fact", "preference", "skill", "episode", "instruction", "relationship"}
	return types[rng.Intn(len(types))]
}

func percentile(durations []time.Duration, pct int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := len(sorted) * pct / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func fmtDur(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Microseconds()))
	}
	return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
}
