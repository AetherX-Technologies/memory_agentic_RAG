package store

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

const (
	// 层级权重衰减因子
	hierarchyWeightDecay = 0.8
	// RRF 融合常数 k
	rrfConstantK = 60
	// 每层候选池倍数
	layerCandidateMultiplier = 2
	// 全局回退权重
	globalFallbackWeight = 0.5
)

// parseHierarchyLevels 解析层次路径
func parseHierarchyLevels(path string) []string {
	if path == "" {
		return []string{}
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	levels := make([]string, len(parts))
	for i := range parts {
		levels[i] = "/" + strings.Join(parts[:i+1], "/")
	}
	return levels
}

// calculateLevelWeight 计算层级权重
func calculateLevelWeight(levelIndex, totalLevels int) float64 {
	distance := totalLevels - levelIndex - 1
	return math.Pow(hierarchyWeightDecay, float64(distance))
}

// escapeLike SQL LIKE 转义
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "_", "\\_")
	s = strings.ReplaceAll(s, "%", "\\%")
	return s
}

// convertToInterfaces 字符串切片转接口切片
func convertToInterfaces(strs []string) []interface{} {
	result := make([]interface{}, len(strs))
	for i, s := range strs {
		result[i] = s
	}
	return result
}

// buildScopeFilter 构建作用域过滤条件
func buildScopeFilter(scopes []string) (string, []interface{}) {
	if len(scopes) == 0 {
		return "", nil
	}
	placeholders := strings.Repeat("?,", len(scopes)-1) + "?"
	return " AND m.scope IN (" + placeholders + ")", convertToInterfaces(scopes)
}

// vectorSearchInLevel 在指定层级执行向量检索
func (s *sqliteStore) vectorSearchInLevel(queryVec []float32, level string, limit int, scopes []string) ([]SearchResult, error) {
	scopeFilter, scopeArgs := buildScopeFilter(scopes)

	sql := `
		SELECT m.id, m.text, v.vector, m.abstract, m.overview,
			m.category, m.scope, m.importance, m.timestamp, m.metadata,
			m.hierarchy_path, m.hierarchy_level, m.parent_id, m.node_type,
			m.source_file, m.chunk_index, m.token_count
		FROM memories m
		JOIN vectors v ON m.id = v.memory_id
		WHERE (m.hierarchy_path = ? OR m.hierarchy_path LIKE ? ESCAPE '\')` + scopeFilter

	args := []interface{}{level, escapeLike(level) + "/%"}
	args = append(args, scopeArgs...)
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []SearchResult
	for rows.Next() {
		var m Memory
		var vectorBlob []byte
		var abstract, overview, hierarchyPath, parentID, nodeType, sourceFile *string
		var tokenCount *int
		if err := rows.Scan(&m.ID, &m.Text, &vectorBlob, &abstract, &overview,
			&m.Category, &m.Scope, &m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel, &parentID, &nodeType,
			&sourceFile, &m.ChunkIndex, &tokenCount); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to scan row in vectorSearchInLevel: %v\n", err)
			continue
		}
		assignNullableFields(&m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)

		vec, err := DeserializeVector(vectorBlob)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to deserialize vector for memory %s: %v\n", m.ID, err)
			continue
		}
		m.Vector = vec
		score := float64(CosineSimilarity(queryVec, m.Vector))
		candidates = append(candidates, SearchResult{Entry: m, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > limit {
		return candidates[:limit], nil
	}
	return candidates, nil
}

// bm25SearchInLevel 在指定层级执行BM25检索
func (s *sqliteStore) bm25SearchInLevel(query string, level string, limit int, scopes []string) ([]SearchResult, error) {
	// 转义 FTS5 特殊字符
	query = EscapeFTS5Query(query)

	scopeFilter, scopeArgs := buildScopeFilter(scopes)

	sql := `
		SELECT m.id, m.text, m.abstract, m.overview,
			m.category, m.scope, m.importance, m.timestamp, m.metadata,
			m.hierarchy_path, m.hierarchy_level, m.parent_id, m.node_type,
			m.source_file, m.chunk_index, m.token_count,
			rank as score
		FROM fts_memories
		JOIN memories m ON fts_memories.memory_id = m.id
		WHERE fts_memories MATCH ?
		  AND (m.hierarchy_path = ? OR m.hierarchy_path LIKE ? ESCAPE '\')` + scopeFilter + `
		ORDER BY score ASC
		LIMIT ?`

	args := []interface{}{query, level, escapeLike(level) + "/%"}
	args = append(args, scopeArgs...)
	args = append(args, limit)
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var m Memory
		var score float64
		var abstract, overview, hierarchyPath, parentID, nodeType, sourceFile *string
		var tokenCount *int
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope, &m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel, &parentID, &nodeType,
			&sourceFile, &m.ChunkIndex, &tokenCount,
			&score); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to scan row in bm25SearchInLevel: %v\n", err)
			continue
		}
		assignNullableFields(&m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
		results = append(results, SearchResult{Entry: m, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// rrfFusion RRF融合算法
func rrfFusion(vectorResults, bm25Results []SearchResult) []SearchResult {
	scoreMap := make(map[string]float64)
	memoryMap := make(map[string]Memory)

	for rank, r := range vectorResults {
		scoreMap[r.Entry.ID] += 1.0 / float64(rrfConstantK+rank+1)
		memoryMap[r.Entry.ID] = r.Entry
	}

	for rank, r := range bm25Results {
		scoreMap[r.Entry.ID] += 1.0 / float64(rrfConstantK+rank+1)
		memoryMap[r.Entry.ID] = r.Entry
	}

	var results []SearchResult
	for id, score := range scoreMap {
		results = append(results, SearchResult{Entry: memoryMap[id], Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// aggregateResults 聚合多层结果
func aggregateResults(results []SearchResult, limit int) []SearchResult {
	// 先按分数排序，确保高分结果优先
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	seen := make(map[string]bool)
	var aggregated []SearchResult

	for _, r := range results {
		if !seen[r.Entry.ID] {
			seen[r.Entry.ID] = true
			aggregated = append(aggregated, r)
			if len(aggregated) >= limit {
				break
			}
		}
	}

	return aggregated
}

// HierarchicalHybridSearch 分层混合检索
func (s *sqliteStore) HierarchicalHybridSearch(queryVec []float32, query string, currentPath string, limit int, scopes []string) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("invalid limit: %d (must be 1-100)", limit)
	}

	// 空向量且无文本查询时报错
	if len(queryVec) == 0 && query == "" {
		return nil, fmt.Errorf("both query vector and text are empty")
	}

	levels := parseHierarchyLevels(currentPath)
	var allResults []SearchResult

	for i, level := range levels {
		var vectorResults []SearchResult
		var bm25Results []SearchResult

		// 只在有向量时执行向量搜索
		if len(queryVec) > 0 {
			vr, err := s.vectorSearchInLevel(queryVec, level, limit*layerCandidateMultiplier, scopes)
			if err != nil {
				return nil, fmt.Errorf("vector search failed at level %s: %w", level, err)
			}
			vectorResults = vr
		}

		// 只在有查询文本时执行 BM25 搜索
		if query != "" {
			br, err := s.bm25SearchInLevel(query, level, limit*layerCandidateMultiplier, scopes)
			if err != nil {
				return nil, fmt.Errorf("BM25 search failed at level %s: %w", level, err)
			}
			bm25Results = br
		}

		fusedResults := rrfFusion(vectorResults, bm25Results)

		weight := calculateLevelWeight(i, len(levels))
		for j := range fusedResults {
			fusedResults[j].Score *= weight
		}

		allResults = append(allResults, fusedResults...)
	}

	aggregated := aggregateResults(allResults, limit)

	// 全局 fallback
	if len(aggregated) < limit {
		globalResults, _ := s.searchGlobalMemories(queryVec, query, limit-len(aggregated), scopes)
		for i := range globalResults {
			globalResults[i].Score *= globalFallbackWeight
		}
		// 合并后再次去重
		combined := append(aggregated, globalResults...)
		aggregated = aggregateResults(combined, limit)
	}

	// 评分管道
	scored := s.applyScoring(aggregated, query)

	if len(scored) > limit {
		return scored[:limit], nil
	}
	return scored, nil
}

// searchGlobalMemories 搜索无层次路径的记忆
func (s *sqliteStore) searchGlobalMemories(queryVec []float32, query string, limit int, scopes []string) ([]SearchResult, error) {
	// 只搜索 hierarchy_path IS NULL 的全局记忆
	scopeFilter := " AND m.hierarchy_path IS NULL"
	if len(scopes) > 0 {
		placeholders := strings.Repeat("?,", len(scopes)-1) + "?"
		scopeFilter += " AND m.scope IN (" + placeholders + ")"
	}

	// 如果有向量，执行向量搜索
	if len(queryVec) > 0 {
		sql := `
			SELECT m.id, m.text, v.vector, m.abstract, m.overview,
				m.category, m.scope, m.importance, m.timestamp, m.metadata,
				m.hierarchy_path, m.hierarchy_level, m.parent_id, m.node_type,
				m.source_file, m.chunk_index, m.token_count
			FROM memories m
			JOIN vectors v ON m.id = v.memory_id
			WHERE 1=1` + scopeFilter

		args := []interface{}{}
		if len(scopes) > 0 {
			args = convertToInterfaces(scopes)
		}

		rows, err := s.db.Query(sql, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var candidates []SearchResult
		for rows.Next() {
			var m Memory
			var vectorBlob []byte
			var abstract, overview, hierarchyPath, parentID, nodeType, sourceFile *string
			var tokenCount *int
			if err := rows.Scan(&m.ID, &m.Text, &vectorBlob, &abstract, &overview,
				&m.Category, &m.Scope, &m.Importance, &m.Timestamp, &m.Metadata,
				&hierarchyPath, &m.HierarchyLevel, &parentID, &nodeType,
				&sourceFile, &m.ChunkIndex, &tokenCount); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to scan row in searchGlobalMemories: %v\n", err)
				continue
			}
			assignNullableFields(&m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
			vec, err := DeserializeVector(vectorBlob)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to deserialize vector for memory %s: %v\n", m.ID, err)
				continue
			}
			m.Vector = vec
			score := float64(CosineSimilarity(queryVec, m.Vector))
			candidates = append(candidates, SearchResult{Entry: m, Score: score})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}

		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Score > candidates[j].Score
		})

		if len(candidates) > limit {
			return candidates[:limit], nil
		}
		return candidates, nil
	}

	// 纯 BM25 搜索
	query = EscapeFTS5Query(query)

	sql := `
		SELECT m.id, m.text, m.abstract, m.overview,
			m.category, m.scope, m.importance, m.timestamp, m.metadata,
			m.hierarchy_path, m.hierarchy_level, m.parent_id, m.node_type,
			m.source_file, m.chunk_index, m.token_count,
			rank as score
		FROM fts_memories
		JOIN memories m ON fts_memories.memory_id = m.id
		WHERE fts_memories MATCH ?` + scopeFilter + `
		ORDER BY score ASC
		LIMIT ?`

	args := []interface{}{query}
	if len(scopes) > 0 {
		args = append(args, convertToInterfaces(scopes)...)
	}
	args = append(args, limit)

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var m Memory
		var score float64
		var abstract, overview, hierarchyPath, parentID, nodeType, sourceFile *string
		var tokenCount *int
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope, &m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel, &parentID, &nodeType,
			&sourceFile, &m.ChunkIndex, &tokenCount,
			&score); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to scan row in searchGlobalMemories BM25: %v\n", err)
			continue
		}
		assignNullableFields(&m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
		// BM25 score is negative (smaller-is-better), convert to positive
		results = append(results, SearchResult{Entry: m, Score: -score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// applyScoring 应用评分管道
func (s *sqliteStore) applyScoring(results []SearchResult, query string) []SearchResult {
	config := ScoringConfig{
		RecencyHalfLifeDays: 14,
		RecencyWeight:       0.1,
		LengthNormAnchor:    200,
		HardMinScore:        0.01, // RRF 分数约 0.033，使用更低阈值
	}
	scored := ApplyScoring(results, config)
	if s.reranker != nil {
		scored, _ = s.reranker.Rerank(query, scored)
	}
	return scored
}
