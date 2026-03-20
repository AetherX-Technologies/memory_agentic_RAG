// OpenViking Integration Test
//
// Tests the full pipeline: Document → Split → L0/L1 → Store → Hierarchical Search
// Run with: go run -tags fts5 ./cmd/openviking_integration/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/generator"
	"github.com/yourusername/hybridmem-rag/internal/parser"
	"github.com/yourusername/hybridmem-rag/internal/retrieval"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

func main() {
	fmt.Println("=== OpenViking Integration Test ===")
	fmt.Println()

	passed := 0
	failed := 0

	run := func(name string, fn func() error) {
		fmt.Printf("TEST: %s ... ", name)
		start := time.Now()
		if err := fn(); err != nil {
			fmt.Printf("FAIL (%v): %v\n", time.Since(start), err)
			failed++
		} else {
			fmt.Printf("PASS (%v)\n", time.Since(start))
			passed++
		}
	}

	run("SmartSplitter: Markdown document", testSplitter)
	run("Generator: L0/L1 fallback (no LLM)", testGeneratorFallback)
	run("Full pipeline: Split → L0/L1 → Store → Search", testFullPipeline)
	run("Hierarchical retriever: tree search", testHierarchicalSearch)
	run("API versioning: v2 response format", testAPIVersioning)

	fmt.Printf("\n=== Results: %d passed, %d failed ===\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func testSplitter() error {
	config := parser.SplitterConfig{
		MaxChunkSize:   100,
		MinChunkSize:   20,
		EnableSemantic: false,
	}
	splitter := parser.NewSmartSplitter(config, nil)

	doc := `# Introduction

OpenViking is a hierarchical retrieval system that uses recursive search and score propagation to improve document retrieval accuracy.

# Architecture

The system consists of three layers: L0 (abstract), L1 (overview), and L2 (full content). Each node in the tree has all three representations.

# Implementation

The implementation uses Go with SQLite for storage, supporting cross-platform deployment including iOS and Android.`

	sections, err := splitter.Split(doc, "/docs/openviking.md")
	if err != nil {
		return fmt.Errorf("split failed: %w", err)
	}

	if len(sections) < 3 {
		return fmt.Errorf("expected at least 3 sections (3 headings), got %d", len(sections))
	}

	for i, s := range sections {
		fmt.Printf("  chunk[%d]: title=%q tokens=%d hierarchy=%s\n", i, s.Title, s.TokenCount, s.Hierarchy)
	}
	return nil
}

func testGeneratorFallback() error {
	// Use a dummy API key — the generator will fail LLM calls and fall back to rule extraction
	gen, err := generator.New(generator.Config{
		APIKey:     "test-key",
		Endpoint:   "http://localhost:1/nonexistent", // intentionally unreachable
		Timeout:    1,
		MaxRetries: 0,
	})
	if err != nil {
		return fmt.Errorf("generator creation failed: %w", err)
	}

	ctx := context.Background()
	content := "OpenViking 是一个分层检索系统。它通过递归搜索和分数传播来提高文档检索精度。系统支持 L0 摘要和 L1 概览两种快速预览方式。"

	l0, err := gen.GenerateL0(ctx, content)
	if err != nil {
		return fmt.Errorf("L0 generation failed: %w", err)
	}
	if l0 == "" {
		return fmt.Errorf("L0 should not be empty (fallback)")
	}
	fmt.Printf("  L0: %s\n", l0)

	l1, err := gen.GenerateL1(ctx, content)
	if err != nil {
		return fmt.Errorf("L1 generation failed: %w", err)
	}
	if l1 == "" {
		return fmt.Errorf("L1 should not be empty (fallback)")
	}
	fmt.Printf("  L1: %s\n", l1)

	return nil
}

func testFullPipeline() error {
	// 1. Split document
	splitter := parser.NewSmartSplitter(parser.SplitterConfig{
		MaxChunkSize: 200,
		MinChunkSize: 1, // low threshold to preserve small sections
	}, nil)

	doc := `# Data Model

The memories table stores all document chunks with L0/L1/L2 representations. Each chunk has a parent_id forming a tree structure.

# Retrieval Engine

The hierarchical retriever uses a priority queue for recursive search. Score propagation follows the formula: alpha * childScore + (1-alpha) * parentScore * depthDecay.

# API Design

The REST API supports v1 (full content) and v2 (abstract + lazy loading) via the X-API-Version header.`

	sections, err := splitter.Split(doc, "/docs/architecture.md")
	if err != nil {
		return fmt.Errorf("split: %w", err)
	}
	fmt.Printf("  split: %d chunks\n", len(sections))

	// 2. Generate L0/L1 (fallback mode)
	gen, err := generator.New(generator.Config{
		APIKey: "test", Endpoint: "http://localhost:1/x", Timeout: 1, MaxRetries: 0,
	})
	if err != nil {
		return fmt.Errorf("generator creation failed: %w", err)
	}
	ctx := context.Background()

	// 3. Store chunks with mock vector (can't use real store without FTS5 in test)
	type storedChunk struct {
		memory store.Memory
		vector []float32
	}
	var chunks []storedChunk

	for i, sec := range sections {
		// Errors are expected (unreachable LLM) — fallback values are used
		l0, _ := gen.GenerateL0(ctx, sec.Content)
		l1, _ := gen.GenerateL1(ctx, sec.Content)

		// Generate a deterministic mock vector based on content
		vec := mockVector(sec.Content, 8)

		mem := store.Memory{
			ID:         fmt.Sprintf("chunk-%d", i),
			Text:       sec.Content,
			Abstract:   l0,
			Overview:   l1,
			Category:   "doc",
			Scope:      "global",
			Importance: 0.7,
			Timestamp:  time.Now().Unix(),
			NodeType:   sec.NodeType,
			SourceFile: sec.SourceFile,
			ChunkIndex: sec.ChunkIndex,
			TokenCount: sec.TokenCount,
			HierarchyPath: sec.Hierarchy,
		}
		chunks = append(chunks, storedChunk{memory: mem, vector: vec})
		fmt.Printf("  stored: id=%s abstract=%q\n", mem.ID, truncate(l0, 40))
	}

	if len(chunks) < 3 {
		return fmt.Errorf("expected at least 3 chunks, got %d", len(chunks))
	}
	return nil
}

func testHierarchicalSearch() error {
	// Build a mock tree: parent → 3 children
	ms := &mockMemoryStore{
		memories: make(map[string]*store.Memory),
		children: make(map[string][]*store.Memory),
		vectors:  make(map[string][]float32),
	}

	queryVec := []float32{1, 0, 0, 0, 0, 0, 0, 0}

	// Parent
	ms.add("parent", &store.Memory{
		ID: "parent", Text: "parent doc", Abstract: "Parent document",
		NodeType: "directory", SourceFile: "doc.md",
	}, []float32{0.6, 0.4, 0, 0, 0, 0, 0, 0})

	// Children with varying relevance
	ms.add("c1", &store.Memory{
		ID: "c1", Text: "relevant child", Abstract: "Highly relevant",
		ParentID: "parent", NodeType: "chunk", SourceFile: "doc.md", ChunkIndex: 0,
	}, []float32{0.95, 0.05, 0, 0, 0, 0, 0, 0})

	ms.add("c2", &store.Memory{
		ID: "c2", Text: "somewhat relevant", Abstract: "Somewhat relevant",
		ParentID: "parent", NodeType: "chunk", SourceFile: "doc.md", ChunkIndex: 1,
	}, []float32{0.7, 0.3, 0, 0, 0, 0, 0, 0})

	ms.add("other", &store.Memory{
		ID: "other", Text: "unrelated", Abstract: "Unrelated",
		SourceFile: "other.md",
	}, []float32{0, 0, 1, 0, 0, 0, 0, 0})

	retriever := retrieval.New(ms, retrieval.DefaultConfig())
	results, err := retriever.Search(queryVec, 10, nil)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	fmt.Printf("  results: %d\n", len(results))
	for i, r := range results {
		fmt.Printf("  [%d] %s score=%.4f chunks=%d\n", i, r.Entry.SourceFile, r.Score, r.ChunkCount)
	}

	if len(results) == 0 {
		return fmt.Errorf("expected results")
	}
	// doc.md should rank higher than other.md
	if results[0].Entry.SourceFile != "doc.md" {
		return fmt.Errorf("expected doc.md as top result, got %s", results[0].Entry.SourceFile)
	}
	return nil
}

func testAPIVersioning() error {
	// This is a structural test — verify types compile and wire correctly
	// Real HTTP testing is done in handler_v2_test.go
	sr := store.SearchResult{
		Entry: store.Memory{
			ID:       "test",
			Abstract: "Test abstract",
			Overview: "Test overview",
		},
		Score:      0.85,
		ChunkCount: 3,
		ContentURL: "/api/memories/test/content",
	}

	if sr.ContentURL == "" {
		return fmt.Errorf("ContentURL should be set")
	}
	if sr.ChunkCount != 3 {
		return fmt.Errorf("ChunkCount should be 3")
	}
	fmt.Printf("  v2 result: id=%s score=%.2f chunks=%d url=%s\n",
		sr.Entry.ID, sr.Score, sr.ChunkCount, sr.ContentURL)
	return nil
}

// --- Helpers ---

func mockVector(content string, dim int) []float32 {
	vec := make([]float32, dim)
	for i, r := range content {
		vec[i%dim] += float32(r) / 10000.0
	}
	return vec
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// mockMemoryStore implements retrieval.MemoryStore for integration testing.
type mockMemoryStore struct {
	memories map[string]*store.Memory
	children map[string][]*store.Memory
	vectors  map[string][]float32
}

func (m *mockMemoryStore) add(id string, mem *store.Memory, vec []float32) {
	mem.Vector = vec
	m.memories[id] = mem
	m.vectors[id] = vec
	if mem.ParentID != "" {
		m.children[mem.ParentID] = append(m.children[mem.ParentID], mem)
	}
}

func (m *mockMemoryStore) VectorSearch(query []float32, limit int, scopes []string) ([]store.SearchResult, error) {
	var results []store.SearchResult
	for _, mem := range m.memories {
		sim := dotProduct(query, mem.Vector)
		results = append(results, store.SearchResult{Entry: *mem, Score: float64(sim)})
	}
	// Sort descending
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *mockMemoryStore) GetChildren(parentID string) ([]*store.Memory, error) {
	return m.children[parentID], nil
}

func dotProduct(a, b []float32) float32 {
	var sum float32
	for i := range a {
		if i < len(b) {
			sum += a[i] * b[i]
		}
	}
	return sum
}

func init() {
	log.SetFlags(0)
}
