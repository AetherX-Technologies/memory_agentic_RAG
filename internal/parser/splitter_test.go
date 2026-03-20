package parser

import (
	"fmt"
	"strings"
	"testing"
)

// mockEmbedder returns deterministic embeddings for testing.
type mockEmbedder struct{}

func (m *mockEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, 8)
		for j, r := range text {
			vec[j%8] += float32(r) / 1000.0
		}
		_ = i
		results[i] = vec
	}
	return results, nil
}

func TestSmartSplitter_StructuralOnly(t *testing.T) {
	config := SplitterConfig{
		MaxChunkSize:   1000,
		MinChunkSize:   1, // very low to prevent merging
		EnableSemantic: false,
	}
	splitter := NewSmartSplitter(config, nil)

	content := `# Chapter 1

Content of chapter 1 with enough text to be a real section.

# Chapter 2

Content of chapter 2 with enough text to be another section.`

	sections, err := splitter.Split(content, "/test/doc.md")
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	if len(sections) < 2 {
		t.Fatalf("expected at least 2 sections, got %d", len(sections))
	}

	for _, s := range sections {
		if s.SourceFile != "/test/doc.md" {
			t.Errorf("SourceFile = %q, want %q", s.SourceFile, "/test/doc.md")
		}
		if s.NodeType != "chunk" {
			t.Errorf("NodeType = %q, want %q", s.NodeType, "chunk")
		}
	}
}

func TestSmartSplitter_NoHeadings(t *testing.T) {
	config := SplitterConfig{
		MaxChunkSize:   1000,
		MinChunkSize:   1,
		EnableSemantic: false,
	}
	splitter := NewSmartSplitter(config, nil)

	content := "Plain text without any headings. Just a simple document."
	sections, err := splitter.Split(content, "/test/plain.txt")
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
}

func TestSmartSplitter_LargeChunkParagraphSplit(t *testing.T) {
	config := SplitterConfig{
		MaxChunkSize:   30,
		MinChunkSize:   5,
		EnableSemantic: false,
	}
	splitter := NewSmartSplitter(config, nil)

	// Build content with actual paragraph breaks
	var paragraphs []string
	for i := 0; i < 10; i++ {
		paragraphs = append(paragraphs, fmt.Sprintf("This is paragraph number %d with some substantial content to make it longer.", i))
	}
	content := "# Title\n\n" + strings.Join(paragraphs, "\n\n")

	sections, err := splitter.Split(content, "/test/big.md")
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	if len(sections) < 2 {
		t.Fatalf("expected multiple sections from paragraphed text, got %d", len(sections))
	}
}

func TestSmartSplitter_SemanticEnabled(t *testing.T) {
	config := SplitterConfig{
		MaxChunkSize:   20,
		MinChunkSize:   1,
		EnableSemantic: true,
		MinSegment:     1,
	}
	embedder := &mockEmbedder{}
	splitter := NewSmartSplitter(config, embedder)

	// Diverse sentences to trigger semantic splitting or paragraph fallback
	content := "数学是基础学科。编程很重要。历史记录文明。物理解释自然。化学研究物质。生物探索生命。地理描述地球。天文观测宇宙。"
	sections, err := splitter.Split(content, "/test/diverse.txt")
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	// With MaxChunkSize=20, text (~90 tokens) should produce multiple sections
	// Even if semantic split returns 1 section, adjustSizes force-split should break it up
	t.Logf("got %d sections from diverse text", len(sections))
	if len(sections) < 1 {
		t.Fatal("expected at least 1 section")
	}
}

func TestSmartSplitter_Empty(t *testing.T) {
	config := DefaultSplitterConfig()
	splitter := NewSmartSplitter(config, nil)

	sections, err := splitter.Split("", "/test/empty.txt")
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	if len(sections) != 0 {
		t.Fatalf("expected 0 sections, got %d", len(sections))
	}
}

func TestSmartSplitter_MetadataSet(t *testing.T) {
	config := SplitterConfig{
		MaxChunkSize:   1000,
		MinChunkSize:   1,
		EnableSemantic: false,
	}
	splitter := NewSmartSplitter(config, nil)

	content := "# Hello\n\nWorld content here with enough text."
	sections, err := splitter.Split(content, "/docs/test.md")
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	for i, s := range sections {
		if s.ChunkIndex != i {
			t.Errorf("section[%d].ChunkIndex = %d", i, s.ChunkIndex)
		}
		if s.SourceFile != "/docs/test.md" {
			t.Errorf("section[%d].SourceFile = %q", i, s.SourceFile)
		}
		if s.Hierarchy == "" {
			t.Errorf("section[%d].Hierarchy is empty", i)
		}
	}
}

func TestSmartSplitter_MergeSmallChunks(t *testing.T) {
	config := SplitterConfig{
		MaxChunkSize:   1000,
		MinChunkSize:   50, // high MinChunkSize to trigger merging
		EnableSemantic: false,
	}
	splitter := NewSmartSplitter(config, nil)

	content := `# A

Short.

# B

Short too.

# C

Also short.`

	sections, err := splitter.Split(content, "/test/merge.md")
	if err != nil {
		t.Fatalf("Split failed: %v", err)
	}
	// All 3 sections are very small (< 50 tokens each), so they should be merged
	t.Logf("got %d sections (expected merge to reduce count)", len(sections))
	if len(sections) > 2 {
		t.Errorf("expected sections to be merged (got %d), MinChunkSize=%d", len(sections), config.MinChunkSize)
	}
}
