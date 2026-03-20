package parser

import (
	"testing"
)

func TestSplitSentences_Chinese(t *testing.T) {
	text := "这是第一句话。这是第二句话！这是第三句话？"
	sentences := SplitSentences(text)
	if len(sentences) != 3 {
		t.Fatalf("expected 3 sentences, got %d: %v", len(sentences), sentences)
	}
	if sentences[0] != "这是第一句话。" {
		t.Errorf("sentence[0] = %q, want %q", sentences[0], "这是第一句话。")
	}
	if sentences[1] != "这是第二句话！" {
		t.Errorf("sentence[1] = %q, want %q", sentences[1], "这是第二句话！")
	}
}

func TestSplitSentences_English(t *testing.T) {
	text := "This is the first sentence. This is the second! And the third?"
	sentences := SplitSentences(text)
	if len(sentences) != 3 {
		t.Fatalf("expected 3 sentences, got %d: %v", len(sentences), sentences)
	}
}

func TestSplitSentences_Mixed(t *testing.T) {
	text := "OpenViking 是一个分层检索系统。It supports hierarchical search. 这很有用！"
	sentences := SplitSentences(text)
	if len(sentences) != 3 {
		t.Fatalf("expected 3 sentences, got %d: %v", len(sentences), sentences)
	}
}

func TestSplitSentences_Empty(t *testing.T) {
	sentences := SplitSentences("")
	if len(sentences) != 0 {
		t.Fatalf("expected 0 sentences, got %d", len(sentences))
	}
}

func TestSplitSentences_NoEnding(t *testing.T) {
	text := "This text has no sentence ending"
	sentences := SplitSentences(text)
	if len(sentences) != 1 {
		t.Fatalf("expected 1 sentence, got %d: %v", len(sentences), sentences)
	}
	if sentences[0] != text {
		t.Errorf("got %q, want %q", sentences[0], text)
	}
}

func TestEstimateTokenCount(t *testing.T) {
	tests := []struct {
		text    string
		minToks int
		maxToks int
	}{
		{"", 0, 0},
		{"hello world", 2, 4},
		{"这是一个测试", 6, 12},
		{"Hello 世界 test 你好", 6, 15},
	}
	for _, tt := range tests {
		tc := EstimateTokenCount(tt.text)
		if tc < tt.minToks || tc > tt.maxToks {
			t.Errorf("EstimateTokenCount(%q) = %d, want in [%d, %d]", tt.text, tc, tt.minToks, tt.maxToks)
		}
	}
}
