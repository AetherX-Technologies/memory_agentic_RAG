package store

import (
	"fmt"
	"math"
	"sort"
)

// CosineSimilarity 计算余弦相似度
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	result := dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
	if math.IsNaN(float64(result)) || math.IsInf(float64(result), 0) {
		return 0
	}
	return result
}

// VectorSearch 暴力向量搜索
func (s *sqliteStore) VectorSearch(query []float32, limit int, scopes []string) ([]SearchResult, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}
	if s.config.VectorDim > 0 && len(query) != s.config.VectorDim {
		return nil, fmt.Errorf("query vector dimension mismatch: expected %d, got %d", s.config.VectorDim, len(query))
	}

	// 归一化查询向量（存储的向量已归一化）
	queryNorm := make([]float32, len(query))
	copy(queryNorm, query)
	NormalizeVector(queryNorm)

	// 构建 scope 过滤条件
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

	// 读取所有向量
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

	var results []SearchResult
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

		score := CosineSimilarityNormalized(queryNorm, vector)
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
		results = append(results, SearchResult{
			Entry: m,
			Score: float64(score),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 排序并取 Top-K
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}
