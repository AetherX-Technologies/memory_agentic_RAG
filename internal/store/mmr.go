package store

import "math"

// MMRRerank applies Maximal Marginal Relevance reranking to diversify results.
// BM25-only results may have empty vectors; they are not penalized for similarity.
func MMRRerank(results []SearchResult, lambda float64, simThreshold float64) []SearchResult {
	if len(results) <= 1 {
		return results
	}

	// Copy slice to avoid mutating caller's data
	pool := make([]SearchResult, len(results))
	copy(pool, results)

	selected := []SearchResult{pool[0]}
	remaining := pool[1:]

	for len(remaining) > 0 {
		bestIdx := -1
		bestScore := -math.MaxFloat64

		for i, candidate := range remaining {
			// Compute max similarity with already-selected results
			// If either vector is empty, maxSim stays 0 (no penalty)
			maxSim := 0.0
			if len(candidate.Entry.Vector) > 0 {
				for _, sel := range selected {
					if len(sel.Entry.Vector) > 0 {
						sim := cosineSim(candidate.Entry.Vector, sel.Entry.Vector)
						if sim > maxSim {
							maxSim = sim
						}
					}
				}
			}

			// MMR: lambda * relevance - (1-lambda) * similarity
			mmrScore := lambda*candidate.Score - (1-lambda)*maxSim

			// Hard penalty for near-duplicates
			if maxSim > simThreshold {
				mmrScore *= 0.3
			}

			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}

		selected = append(selected, remaining[bestIdx])
		// Safe delete: three-index slice to prevent overwriting original backing array
		remaining = append(remaining[:bestIdx:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

// cosineSim computes cosine similarity between two float32 vectors.
func cosineSim(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
