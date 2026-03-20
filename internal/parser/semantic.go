package parser

import (
	"fmt"
	"math"
	"strings"
)

// semanticSplit splits content into semantically coherent chunks using:
//  1. Sentence splitting
//  2. Embedding each sentence (via local Qwen3 model)
//  3. Computing cosine distance between adjacent sentences
//  4. Adaptive threshold boundary detection (mean + 2*std)
//
// Note: ED-PELT (pgregory.net/changepoint) was initially planned but empirically
// fails on spike-pattern distance data. Adaptive threshold is more reliable for
// detecting topic boundaries in pairwise sentence distances.
func semanticSplit(content string, embedder BatchEmbedder, minSegment int) ([]Section, error) {
	// 1. Split into sentences
	sentences := SplitSentences(content)
	if len(sentences) < 3 {
		return []Section{{Content: content, TokenCount: EstimateTokenCount(content)}}, nil
	}

	// 2. Embed all sentences
	embeddings, err := embedder.EmbedBatch(sentences)
	if err != nil {
		return nil, fmt.Errorf("failed to embed sentences: %w", err)
	}
	if len(embeddings) != len(sentences) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, expected %d", len(embeddings), len(sentences))
	}

	// 3. Compute pairwise distance (1 - cosine similarity)
	// High distance = topic change, low distance = same topic
	distances := make([]float64, len(embeddings)-1)
	for i := 0; i < len(embeddings)-1; i++ {
		sim := cosineSimilarity32(embeddings[i], embeddings[i+1])
		distances[i] = 1.0 - sim
	}

	// 4. Adaptive threshold boundary detection
	if minSegment < 1 {
		minSegment = 2
	}
	boundaries := findBoundaries(distances, minSegment)

	// 5. Split sentences at detected boundaries
	return splitByBoundaries(sentences, boundaries), nil
}

// findBoundaries detects topic boundaries using an adaptive threshold.
// A boundary is flagged where the distance exceeds mean + 2*std, with a
// minimum absolute threshold of 0.3 to avoid splitting on noise.
// The minSegment parameter enforces a minimum number of sentences between boundaries.
func findBoundaries(distances []float64, minSegment int) []int {
	if len(distances) < 3 {
		return nil
	}

	// Compute mean and standard deviation
	var sum, sumSq float64
	for _, d := range distances {
		sum += d
		sumSq += d * d
	}
	n := float64(len(distances))
	mean := sum / n
	variance := sumSq/n - mean*mean
	if variance < 0 {
		variance = 0 // guard against float rounding
	}
	std := math.Sqrt(variance)

	// Adaptive threshold: statistically significant spikes
	threshold := mean + 2.0*std
	// Absolute minimum: don't split if all distances are small (uniform topic)
	if threshold < 0.3 {
		threshold = 0.3
	}

	var boundaries []int
	lastBound := -(minSegment + 1) // ensure first boundary can appear at position >= 0
	for i, d := range distances {
		if d > threshold && i-lastBound >= minSegment {
			boundaries = append(boundaries, i)
			lastBound = i
		}
	}
	return boundaries
}

// splitByBoundaries groups sentences into sections based on boundary indices.
func splitByBoundaries(sentences []string, boundaries []int) []Section {
	if len(boundaries) == 0 {
		joined := strings.Join(sentences, " ")
		return []Section{{
			Content:    joined,
			TokenCount: EstimateTokenCount(joined),
		}}
	}

	var sections []Section
	prev := 0

	for _, boundary := range boundaries {
		// boundary is an index in the distances array (len = len(sentences)-1).
		// Distance at index b is between sentence[b] and sentence[b+1].
		// Split so sentences[prev:b+1] form one chunk, sentences[b+1:] start the next.
		splitIdx := boundary + 1
		if splitIdx <= prev || splitIdx >= len(sentences) {
			continue
		}

		chunk := strings.Join(sentences[prev:splitIdx], " ")
		sections = append(sections, Section{
			Content:    chunk,
			TokenCount: EstimateTokenCount(chunk),
		})
		prev = splitIdx
	}

	// Last segment
	if prev < len(sentences) {
		chunk := strings.Join(sentences[prev:], " ")
		sections = append(sections, Section{
			Content:    chunk,
			TokenCount: EstimateTokenCount(chunk),
		})
	}

	return sections
}
