package store

import (
	"math"
	"testing"
)

func TestMMRRerank_Empty(t *testing.T) {
	result := MMRRerank(nil, 0.7, 0.85)
	if len(result) != 0 {
		t.Errorf("MMRRerank(nil) = %d results, want 0", len(result))
	}
}

func TestMMRRerank_Single(t *testing.T) {
	input := []SearchResult{{Entry: Memory{Text: "a"}, Score: 1.0}}
	result := MMRRerank(input, 0.7, 0.85)
	if len(result) != 1 {
		t.Fatalf("MMRRerank(1 item) = %d results, want 1", len(result))
	}
	if result[0].Entry.Text != "a" {
		t.Error("result[0] mismatch")
	}
}

func TestMMRRerank_Diverse(t *testing.T) {
	// Two items with different vectors should both be kept
	input := []SearchResult{
		{Entry: Memory{Text: "a", Vector: []float32{1, 0, 0}}, Score: 0.9},
		{Entry: Memory{Text: "b", Vector: []float32{0, 1, 0}}, Score: 0.8},
	}
	result := MMRRerank(input, 0.7, 0.85)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
}

func TestMMRRerank_Duplicates(t *testing.T) {
	// Three near-identical vectors: MMR should penalize the duplicates
	v := []float32{1, 0, 0}
	input := []SearchResult{
		{Entry: Memory{Text: "a", Vector: v}, Score: 0.9},
		{Entry: Memory{Text: "a-dup", Vector: v}, Score: 0.85},
		{Entry: Memory{Text: "b", Vector: []float32{0, 1, 0}}, Score: 0.7},
	}
	result := MMRRerank(input, 0.7, 0.85)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	// "b" should rank higher than "a-dup" due to diversity bonus
	if result[1].Entry.Text != "b" {
		t.Errorf("expected 'b' at position 1 (diversity), got %q", result[1].Entry.Text)
	}
}

func TestMMRRerank_EmptyVectors(t *testing.T) {
	// BM25-only results with no vectors should not panic and should be kept
	input := []SearchResult{
		{Entry: Memory{Text: "a"}, Score: 0.9},
		{Entry: Memory{Text: "b"}, Score: 0.8},
		{Entry: Memory{Text: "c", Vector: []float32{1, 0, 0}}, Score: 0.7},
	}
	result := MMRRerank(input, 0.7, 0.85)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
}

func TestMMRRerank_DoesNotMutateInput(t *testing.T) {
	input := []SearchResult{
		{Entry: Memory{Text: "a", Vector: []float32{1, 0, 0}}, Score: 0.9},
		{Entry: Memory{Text: "b", Vector: []float32{0, 1, 0}}, Score: 0.8},
		{Entry: Memory{Text: "c", Vector: []float32{0, 0, 1}}, Score: 0.7},
	}
	original := make([]SearchResult, len(input))
	copy(original, input)

	MMRRerank(input, 0.7, 0.85)

	for i := range input {
		if input[i].Entry.Text != original[i].Entry.Text || input[i].Score != original[i].Score {
			t.Errorf("MMRRerank mutated input[%d]: got %q/%.2f, want %q/%.2f",
				i, input[i].Entry.Text, input[i].Score, original[i].Entry.Text, original[i].Score)
		}
	}
}

func TestCosineSim(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{[]float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{[]float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{nil, nil, 0.0},
		{[]float32{1}, []float32{1, 2}, 0.0}, // different lengths
	}
	for _, tt := range tests {
		got := cosineSim(tt.a, tt.b)
		if math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("cosineSim(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
		}
	}
}
