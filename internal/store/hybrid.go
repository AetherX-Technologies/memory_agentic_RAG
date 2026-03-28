package store

import (
	"fmt"
	"os"
	"sort"
	"sync"
)

const (
	CandidateMultiplier = 3    // 候选池倍数（增大以给 RRF 更多候选）
	VectorRRFWeight     = 0.55 // 向量检索在 RRF 中的权重（略高于 BM25）
	BM25RRFWeight       = 0.45 // BM25 在 RRF 中的权重
	rrfK                = 60   // RRF 融合常数
	vectorMinCosine     = 0.1  // 向量候选最低余弦相似度（低于此值不参与 RRF）
)

// HybridSearch 混合检索：向量 + BM25 + RRF 融合 + 重排
func (s *sqliteStore) HybridSearch(queryVector []float32, queryText string, limit int, scopes []string) ([]SearchResult, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}

	// 空向量时只执行 BM25 搜索
	if len(queryVector) == 0 {
		if queryText == "" {
			return nil, fmt.Errorf("both query vector and text are empty")
		}
		results, err := s.BM25Search(queryText, limit, scopes)
		if err != nil {
			return nil, err
		}
		// 应用重排（如果启用）
		if s.reranker != nil {
			reranked, err := s.reranker.Rerank(queryText, results)
			if err != nil {
				fmt.Fprintf(os.Stderr, "rerank failed: %v\n", err)
			} else {
				results = reranked
			}
		}
		return topK(results, limit), nil
	}

	var vectorResults []SearchResult
	var bm25Results []SearchResult
	var wg sync.WaitGroup
	var vectorErr, bm25Err error

	// 并行执行向量检索和 BM25 检索
	wg.Add(2)

	go func() {
		defer wg.Done()
		vectorResults, vectorErr = s.parallelVectorSearch(queryVector, limit*CandidateMultiplier, scopes)
	}()

	go func() {
		defer wg.Done()
		if queryText != "" {
			bm25Results, bm25Err = s.BM25Search(queryText, limit*CandidateMultiplier, scopes)
		}
	}()

	wg.Wait()

	if vectorErr != nil {
		return nil, vectorErr
	}
	if bm25Err != nil {
		return nil, bm25Err
	}

	// RRF 融合
	fused := fuseResults(vectorResults, bm25Results, limit*CandidateMultiplier)

	// 重排（如果启用）
	if s.reranker != nil && queryText != "" {
		reranked, err := s.reranker.Rerank(queryText, fused)
		if err != nil {
			// Rerank failed, continue with original results
			fmt.Fprintf(os.Stderr, "rerank failed: %v\n", err)
		} else {
			fused = reranked
		}
	}

	return topK(fused, limit), nil
}

// fuseResults 使用加权 RRF（Reciprocal Rank Fusion）融合向量和 BM25 结果。
// 融合后分数归一化到 [0, 1]，兼容下游 MMR 评分。
// 低质量向量候选（余弦相似度 < vectorMinCosine）不参与排名。
func fuseResults(vectorResults, bm25Results []SearchResult, limit int) []SearchResult {
	scoreMap := make(map[string]float64)
	memoryMap := make(map[string]SearchResult)

	// 向量结果：过滤低质量候选后按排名贡献 RRF 分数
	// 如果过滤后无结果且 BM25 也为空，回退到未过滤的向量结果
	rank := 0
	for _, r := range vectorResults {
		if r.Score < vectorMinCosine {
			continue
		}
		scoreMap[r.Entry.ID] += VectorRRFWeight / float64(rrfK+rank+1)
		memoryMap[r.Entry.ID] = r
		rank++
	}
	if rank == 0 && len(bm25Results) == 0 {
		// ��有向量候选都低于门槛且无 BM25 结果，回退到未过滤向量
		for rank, r := range vectorResults {
			scoreMap[r.Entry.ID] += VectorRRFWeight / float64(rrfK+rank+1)
			memoryMap[r.Entry.ID] = r
		}
	}

	// BM25 结果按排名贡献 RRF 分数
	for rank, r := range bm25Results {
		scoreMap[r.Entry.ID] += BM25RRFWeight / float64(rrfK+rank+1)
		if _, exists := memoryMap[r.Entry.ID]; !exists {
			memoryMap[r.Entry.ID] = r
		}
	}

	fused := make([]SearchResult, 0, len(scoreMap))
	for id, score := range scoreMap {
		sr := memoryMap[id]
		fused = append(fused, SearchResult{
			Entry: sr.Entry,
			Score: score,
		})
	}

	sort.Slice(fused, func(i, j int) bool {
		return fused[i].Score > fused[j].Score
	})

	// 归一化分数到 [0, 1]，使下游 MMR 的 lambda*score 和 (1-lambda)*similarity 量级匹配
	if len(fused) > 0 {
		maxScore := fused[0].Score
		if maxScore > 0 {
			for i := range fused {
				fused[i].Score /= maxScore
			}
		}
	}

	return topK(fused, limit)
}
