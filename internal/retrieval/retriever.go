// Package retrieval implements OpenViking-style hierarchical retrieval with
// recursive tree search, score propagation, and source-file aggregation.
package retrieval

import (
	"fmt"
	"math"
	"sort"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// Config configures the HierarchicalRetriever.
type Config struct {
	Alpha    float64 // Score propagation weight for child's own vector score (default: 0.7)
	MaxDepth int     // Maximum recursion depth (default: 5)
	MinScore float64 // Minimum score threshold for pruning (default: 0.3)
	SeedSize int     // Number of global results to use as seeds (default: 10)
	PoolSize int     // Global search candidate pool size (default: 50)
}

// DefaultConfig returns a config with values from the OpenViking design doc.
func DefaultConfig() Config {
	return Config{
		Alpha:    0.7,
		MaxDepth: 5,
		MinScore: 0.3,
		SeedSize: 10,
		PoolSize: 50,
	}
}

// MemoryStore is the minimal interface the retriever needs from the store layer.
type MemoryStore interface {
	VectorSearch(query []float32, limit int, scopes []string) ([]store.SearchResult, error)
	GetChildren(parentID string) ([]*store.Memory, error)
}

// HierarchicalRetriever performs OpenViking-style recursive hierarchical search.
type HierarchicalRetriever struct {
	store  MemoryStore
	config Config
}

// New creates a new HierarchicalRetriever.
func New(s MemoryStore, config Config) *HierarchicalRetriever {
	if config.Alpha <= 0 || config.Alpha > 1 {
		config.Alpha = 0.7
	}
	if config.MaxDepth <= 0 {
		config.MaxDepth = 5
	}
	if config.MinScore < 0 {
		config.MinScore = 0.3
	}
	if config.SeedSize <= 0 {
		config.SeedSize = 10
	}
	if config.PoolSize <= 0 {
		config.PoolSize = 50
	}
	return &HierarchicalRetriever{store: s, config: config}
}

// Search performs a full hierarchical search:
//  1. Global vector search (flat, all nodes)
//  2. Recursive child search from top seeds (tree traversal with score propagation)
//  3. RRF merge of global + hierarchical results
//  4. Aggregate by source_file (combine chunks from same document)
func (r *HierarchicalRetriever) Search(queryVector []float32, limit int, scopes []string) ([]store.SearchResult, error) {
	if len(queryVector) == 0 {
		return nil, fmt.Errorf("query vector is empty")
	}

	// Strategy 1: Global vector search
	globalResults, err := r.store.VectorSearch(queryVector, r.config.PoolSize, scopes)
	if err != nil {
		return nil, fmt.Errorf("global vector search failed: %w", err)
	}

	// Strategy 2: Hierarchical recursive search from top seeds
	seedCount := r.config.SeedSize
	if seedCount > len(globalResults) {
		seedCount = len(globalResults)
	}
	hierarchicalResults := r.hierarchicalSearch(queryVector, globalResults[:seedCount])

	// Strategy 3: RRF merge
	merged := rrfMerge(globalResults, hierarchicalResults)

	// Strategy 4: Aggregate by source_file
	aggregated := aggregateBySource(merged)

	if len(aggregated) > limit {
		return aggregated[:limit], nil
	}
	return aggregated, nil
}

// hierarchicalSearch recursively explores children of seed results using a priority queue.
func (r *HierarchicalRetriever) hierarchicalSearch(queryVector []float32, seeds []store.SearchResult) []store.SearchResult {
	candidates := make(map[string]*store.SearchResult)
	pq := NewPriorityQueue()
	visited := make(map[string]bool)

	// Initialize: add seeds to candidates and queue
	for _, seed := range seeds {
		s := seed // copy
		candidates[seed.Entry.ID] = &s
		pq.Push(&SearchNode{
			ID:    seed.Entry.ID,
			Score: seed.Score,
			Depth: 0,
		})
	}

	for pq.Len() > 0 {
		node := pq.Pop()

		// Pruning
		if node.Depth >= r.config.MaxDepth {
			continue
		}
		if node.Score < r.config.MinScore {
			continue
		}
		if visited[node.ID] {
			continue
		}
		visited[node.ID] = true

		// Get children (with vectors loaded)
		children, err := r.store.GetChildren(node.ID)
		if err != nil || len(children) == 0 {
			continue
		}

		for _, child := range children {
			// Compute child's own vector score
			childVecScore := float64(0)
			if len(child.Vector) > 0 && len(queryVector) > 0 {
				childVecScore = cosineSimilarity(queryVector, child.Vector)
			}

			// Score propagation with depth decay
			childDepth := node.Depth + 1
			depthDecay := math.Pow(0.9, float64(childDepth))
			finalScore := r.config.Alpha*childVecScore + (1-r.config.Alpha)*node.Score*depthDecay

			// Update candidate set (keep best score)
			if existing, ok := candidates[child.ID]; !ok || finalScore > existing.Score {
				candidates[child.ID] = &store.SearchResult{
					Entry: *child,
					Score: finalScore,
				}
			}

			// Always push children to queue — GetChildren returning empty handles leaves.
			// This avoids N+1 HasChildren queries.
			pq.Push(&SearchNode{
				ID:    child.ID,
				Score: finalScore,
				Depth: childDepth,
			})
		}
	}

	// Convert map to sorted slice
	results := make([]store.SearchResult, 0, len(candidates))
	for _, r := range candidates {
		results = append(results, *r)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// rrfMerge combines two result lists using Reciprocal Rank Fusion.
func rrfMerge(global, hierarchical []store.SearchResult) []store.SearchResult {
	const k = 60
	scores := make(map[string]float64)
	entries := make(map[string]store.SearchResult)

	for rank, r := range global {
		scores[r.Entry.ID] += 1.0 / float64(rank+1+k)
		entries[r.Entry.ID] = r
	}
	for rank, r := range hierarchical {
		scores[r.Entry.ID] += 1.0 / float64(rank+1+k)
		if _, exists := entries[r.Entry.ID]; !exists {
			entries[r.Entry.ID] = r
		}
	}

	merged := make([]store.SearchResult, 0, len(scores))
	for id, score := range scores {
		entry := entries[id]
		entry.Score = score
		merged = append(merged, entry)
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})
	return merged
}

// aggregateBySource groups results by SourceFile and combines chunks from the same document.
func aggregateBySource(results []store.SearchResult) []store.SearchResult {
	groups := make(map[string][]store.SearchResult)

	for _, r := range results {
		key := r.Entry.SourceFile
		if key == "" {
			key = r.Entry.ID // ungrouped: use ID as key
		}
		groups[key] = append(groups[key], r)
	}

	aggregated := make([]store.SearchResult, 0, len(groups))
	for _, group := range groups {
		// Sort by chunk_index within group
		sort.Slice(group, func(i, j int) bool {
			return group[i].Entry.ChunkIndex < group[j].Entry.ChunkIndex
		})

		// Pick the highest-scoring chunk as representative
		best := group[0]
		for _, r := range group[1:] {
			if r.Score > best.Score {
				best = r
			}
		}

		// Combine abstracts from top-3 chunks
		topN := 3
		if topN > len(group) {
			topN = len(group)
		}
		combined := ""
		for i := 0; i < topN; i++ {
			combined += fmt.Sprintf("[Part %d] %s\n", group[i].Entry.ChunkIndex+1, group[i].Entry.Abstract)
		}
		if len(group) > topN {
			combined += fmt.Sprintf("... (+%d more chunks)\n", len(group)-topN)
		}

		best.Entry.Abstract = combined
		best.ChunkCount = len(group)
		aggregated = append(aggregated, best)
	}

	sort.Slice(aggregated, func(i, j int) bool {
		return aggregated[i].Score > aggregated[j].Score
	})
	return aggregated
}

// cosineSimilarity computes cosine similarity between a float32 query and a float32 document vector.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
