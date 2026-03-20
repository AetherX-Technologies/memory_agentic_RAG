package parser

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// topicEmbedder generates embeddings where different "topics" have orthogonal vectors.
// This simulates a real embedding model for testing adaptive threshold boundary detection.
type topicEmbedder struct{}

func (m *topicEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, 16)
		switch {
		case strings.Contains(text, "math") || strings.Contains(text, "equation") || strings.Contains(text, "calculus"):
			vec[0] = 1.0
			vec[1] = 0.1
		case strings.Contains(text, "history") || strings.Contains(text, "dynasty") || strings.Contains(text, "empire"):
			vec[4] = 1.0
			vec[5] = 0.1
		case strings.Contains(text, "code") || strings.Contains(text, "program") || strings.Contains(text, "function"):
			vec[8] = 1.0
			vec[9] = 0.1
		default:
			vec[i%16] = 0.5
		}
		// L2 normalize
		var norm float32
		for _, v := range vec {
			norm += v * v
		}
		norm = float32(math.Sqrt(float64(norm)))
		if norm > 0 {
			for j := range vec {
				vec[j] /= norm
			}
		}
		results[i] = vec
	}
	return results, nil
}

func TestSemanticSplit_DistinctTopics(t *testing.T) {
	embedder := &topicEmbedder{}

	// Build 3 distinct topic clusters with enough sentences for ED-PELT
	var sentences []string
	for i := 0; i < 5; i++ {
		sentences = append(sentences, fmt.Sprintf("math equation calculus topic %d.", i))
	}
	for i := 0; i < 5; i++ {
		sentences = append(sentences, fmt.Sprintf("history dynasty empire topic %d.", i))
	}
	for i := 0; i < 5; i++ {
		sentences = append(sentences, fmt.Sprintf("code program function topic %d.", i))
	}
	content := strings.Join(sentences, " ")

	sections, err := semanticSplit(content, embedder, 2)
	if err != nil {
		t.Fatalf("semanticSplit failed: %v", err)
	}

	t.Logf("Got %d sections from 3 topics (15 sentences)", len(sections))
	for i, s := range sections {
		t.Logf("  section[%d]: %q", i, truncate(s.Content, 80))
	}

	// With 3 clearly distinct topics, expect at least 2 sections
	if len(sections) < 2 {
		t.Errorf("expected at least 2 sections from 3 distinct topics, got %d", len(sections))
	}
}

func TestSemanticSplit_TooFewSentences(t *testing.T) {
	embedder := &topicEmbedder{}

	content := "Only two sentences. Not enough to split."
	sections, err := semanticSplit(content, embedder, 2)
	if err != nil {
		t.Fatalf("semanticSplit failed: %v", err)
	}
	// < 3 sentences → return as single section
	if len(sections) != 1 {
		t.Errorf("expected 1 section for few sentences, got %d", len(sections))
	}
}

func TestSplitByBoundaries(t *testing.T) {
	sentences := []string{"A", "B", "C", "D", "E", "F"}

	// Boundaries at index 2 and 4 → split after sentence 3 and 5
	boundaries := []int{2, 4}
	sections := splitByBoundaries(sentences, boundaries)

	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	if sections[0].Content != "A B C" {
		t.Errorf("section[0] = %q, want %q", sections[0].Content, "A B C")
	}
	if sections[1].Content != "D E" {
		t.Errorf("section[1] = %q, want %q", sections[1].Content, "D E")
	}
	if sections[2].Content != "F" {
		t.Errorf("section[2] = %q, want %q", sections[2].Content, "F")
	}
}

func TestSplitByBoundaries_NoBoundaries(t *testing.T) {
	sentences := []string{"A", "B", "C"}
	sections := splitByBoundaries(sentences, nil)
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Content != "A B C" {
		t.Errorf("content = %q, want %q", sections[0].Content, "A B C")
	}
}

func TestCosineSimilarity32(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	sim := cosineSimilarity32(a, b)
	if math.Abs(sim-1.0) > 1e-6 {
		t.Errorf("identical vectors: sim = %f, want 1.0", sim)
	}

	c := []float32{0, 1, 0}
	sim = cosineSimilarity32(a, c)
	if math.Abs(sim) > 1e-6 {
		t.Errorf("orthogonal vectors: sim = %f, want 0.0", sim)
	}

	d := []float32{-1, 0, 0}
	sim = cosineSimilarity32(a, d)
	if math.Abs(sim+1.0) > 1e-6 {
		t.Errorf("opposite vectors: sim = %f, want -1.0", sim)
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + fmt.Sprintf("...(%d)", len(runes))
}
