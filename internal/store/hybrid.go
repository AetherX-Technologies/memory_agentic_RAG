package store

import (
	"fmt"
	"os"
	"sync"
)

const (
	BM25BoostFactor     = 0.15 // BM25命中加成15%
	CandidateMultiplier = 2    // 候选池倍数
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

// fuseResults 融合向量和 BM25 结果
func fuseResults(vectorResults, bm25Results []SearchResult, limit int) []SearchResult {
	vectorMap := make(map[string]SearchResult, len(vectorResults))
	bm25Map := make(map[string]bool, len(bm25Results))

	for _, r := range vectorResults {
		vectorMap[r.Entry.ID] = r
	}

	for _, r := range bm25Results {
		bm25Map[r.Entry.ID] = true
	}

	// 融合分数：向量分数为基础，BM25 命中加成
	fused := make([]SearchResult, 0, len(vectorResults)+len(bm25Results))
	for id, vr := range vectorMap {
		score := vr.Score
		if bm25Map[id] {
			score = score + (BM25BoostFactor * score)
			if score > MaxScore {
				score = MaxScore
			}
		}
		fused = append(fused, SearchResult{
			Entry: vr.Entry,
			Score: score,
		})
	}

	// BM25-only 结果
	for _, br := range bm25Results {
		if _, exists := vectorMap[br.Entry.ID]; !exists {
			fused = append(fused, br)
		}
	}

	return topK(fused, limit)
}
