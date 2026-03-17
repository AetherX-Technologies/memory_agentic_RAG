package store

import (
	"container/heap"
	"math"
	"runtime"
	"sort"
	"sync"
)

// resultHeap 实现 heap.Interface
type resultHeap []SearchResult

func (h resultHeap) Len() int           { return len(h) }
func (h resultHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h resultHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *resultHeap) Push(x interface{}) {
	*h = append(*h, x.(SearchResult))
}
func (h *resultHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// NormalizeVector 归一化向量
func NormalizeVector(v []float32) {
	var norm float32
	for _, val := range v {
		norm += val * val
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm > 0 {
		for i := range v {
			v[i] /= norm
		}
	}
}

// CosineSimilarityNormalized 计算归一化向量的余弦相似度
func CosineSimilarityNormalized(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

func topK(results []SearchResult, k int) []SearchResult {
	if k <= 0 || len(results) == 0 {
		return results
	}
	if k >= len(results) {
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})
		return results
	}

	// 使用最小堆保留 top-k
	h := make(resultHeap, 0, k)
	heap.Init(&h)

	for _, r := range results {
		if h.Len() < k {
			heap.Push(&h, r)
		} else if r.Score > h[0].Score {
			heap.Pop(&h)
			heap.Push(&h, r)
		}
	}

	// 转换为降序切片
	topResults := make([]SearchResult, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		topResults[i] = heap.Pop(&h).(SearchResult)
	}
	return topResults
}

// parallelVectorSearch 并行向量搜索
func (s *sqliteStore) parallelVectorSearch(query []float32, limit int, scopes []string) ([]SearchResult, error) {
	if limit <= 0 {
		return nil, nil
	}

	scopeFilter := ""
	if len(scopes) > 0 {
		scopeFilter = " WHERE m.scope IN ("
		for i := range scopes {
			if i > 0 {
				scopeFilter += ","
			}
			scopeFilter += "?"
		}
		scopeFilter += ")"
	}

	queryStr := `SELECT v.memory_id, v.vector, m.text, m.category, m.scope,
		m.importance, m.timestamp, m.metadata, m.hierarchy_path, m.hierarchy_level
		FROM vectors v JOIN memories m ON v.memory_id = m.id` + scopeFilter

	args := make([]interface{}, len(scopes))
	for i, scope := range scopes {
		args[i] = scope
	}

	rows, err := s.db.Query(queryStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type item struct {
		memory Memory
		vector []float32
	}

	// 预分配切片容量
	items := make([]item, 0, 1024)
	for rows.Next() {
		var memoryID, text, category, scope, metadata string
		var hierarchyPath *string
		var importance float64
		var timestamp int64
		var hierarchyLevel int
		var vectorData []byte

		if err := rows.Scan(&memoryID, &vectorData, &text, &category, &scope,
			&importance, &timestamp, &metadata, &hierarchyPath, &hierarchyLevel); err != nil {
			return nil, err
		}

		vector, err := DeserializeVector(vectorData)
		if err != nil {
			return nil, err
		}

		m := Memory{
			ID:             memoryID,
			Text:           text,
			Category:       category,
			Scope:          scope,
			Importance:     importance,
			Timestamp:      timestamp,
			Metadata:       metadata,
			HierarchyLevel: hierarchyLevel,
		}
		if hierarchyPath != nil {
			m.HierarchyPath = *hierarchyPath
		}

		items = append(items, item{
			memory: m,
			vector: vector,
		})
	}

	// 并行计算相似度（使用归一化向量优化）
	numWorkers := runtime.NumCPU()
	chunkSize := (len(items) + numWorkers - 1) / numWorkers

	results := make([]SearchResult, len(items))
	var wg sync.WaitGroup

	// 归一化查询向量
	queryNorm := make([]float32, len(query))
	copy(queryNorm, query)
	NormalizeVector(queryNorm)

	for w := 0; w < numWorkers; w++ {
		start := w * chunkSize
		end := start + chunkSize
		if end > len(items) {
			end = len(items)
		}
		if start >= len(items) {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				// 向量已归一化，直接计算点积
				score := CosineSimilarityNormalized(queryNorm, items[i].vector)
				results[i] = SearchResult{
					Entry: items[i].memory,
					Score: float64(score),
				}
			}
		}(start, end)
	}

	wg.Wait()

	return topK(results, limit), nil
}
