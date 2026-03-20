// Real Document Test — Tests the full OpenViking pipeline with real documents and the Qwen3 ONNX model.
//
// Usage: go run -tags fts5 ./cmd/real_doc_test/ [document_path]
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/embedder"
	"github.com/yourusername/hybridmem-rag/internal/generator"
	"github.com/yourusername/hybridmem-rag/internal/parser"
	"github.com/yourusername/hybridmem-rag/internal/retrieval"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

const defaultModelPath = "models/qwen3-embedding-0.6b-onnx-uint8/dynamic_uint8.onnx"

func main() {
	// 1. Determine document path
	docPath := "/Volumes/SN770Coder/documents/本地AI撰写/openClaw/openclaw原理.md"
	if len(os.Args) > 1 {
		docPath = os.Args[1]
	}

	fmt.Printf("=== Real Document Test ===\n")
	fmt.Printf("Document: %s\n\n", docPath)

	content, err := os.ReadFile(docPath)
	if err != nil {
		log.Fatalf("Failed to read document: %v", err)
	}
	fmt.Printf("Document size: %d bytes, %d runes\n", len(content), len([]rune(string(content))))

	// 2. Try to load local embedder (optional — falls back to structural-only splitting)
	var localEmb *embedder.LocalEmbedder
	modelPath := defaultModelPath
	if _, err := os.Stat(modelPath); err == nil {
		fmt.Printf("\n--- Loading Qwen3 ONNX model ---\n")
		start := time.Now()
		localEmb, err = embedder.NewLocalEmbedder(embedder.Config{
			ModelPath: modelPath,
			BatchSize: 16,
		})
		if err != nil {
			fmt.Printf("Warning: Failed to load model: %v\n", err)
			fmt.Printf("Continuing without semantic splitting...\n")
		} else {
			defer localEmb.Close()
			fmt.Printf("Model loaded in %v\n", time.Since(start))

			// Quick embedding test
			testStart := time.Now()
			vec, err := localEmb.Embed("这是一个测试句子。")
			if err != nil {
				fmt.Printf("Warning: Embedding test failed: %v\n", err)
				localEmb = nil
			} else {
				fmt.Printf("Embedding test: dim=%d, time=%v\n", len(vec), time.Since(testStart))
			}
		}
	} else {
		fmt.Printf("\nNote: Model not found at %s, using structural splitting only.\n", modelPath)
	}

	// 3. Split document
	fmt.Printf("\n--- Splitting document ---\n")
	splitterConfig := parser.SplitterConfig{
		MaxChunkSize:   512,
		MinChunkSize:   128,
		EnableSemantic: localEmb != nil,
		MinSegment:     3,
	}

	var batchEmb parser.BatchEmbedder
	if localEmb != nil {
		batchEmb = localEmb
	}
	splitter := parser.NewSmartSplitter(splitterConfig, batchEmb)

	splitStart := time.Now()
	sections, err := splitter.Split(string(content), docPath)
	if err != nil {
		log.Fatalf("Split failed: %v", err)
	}
	splitDuration := time.Since(splitStart)

	fmt.Printf("Split into %d chunks in %v\n", len(sections), splitDuration)
	for i, s := range sections {
		titleStr := s.Title
		if titleStr == "" {
			titleStr = "(no title)"
		}
		contentPreview := []rune(s.Content)
		if len(contentPreview) > 60 {
			contentPreview = contentPreview[:60]
		}
		fmt.Printf("  [%d] title=%q tokens=%d content=%q\n", i, titleStr, s.TokenCount, string(contentPreview))
	}

	// 4. Generate L0/L1 (fallback mode — no real LLM)
	fmt.Printf("\n--- Generating L0/L1 (fallback mode) ---\n")
	gen, err := generator.New(generator.Config{
		APIKey: "test", Endpoint: "http://localhost:1/x", Timeout: 1, MaxRetries: 0,
	})
	if err != nil {
		log.Fatalf("Generator creation failed: %v", err)
	}
	ctx := context.Background()

	genStart := time.Now()
	for i, s := range sections {
		l0, _ := gen.GenerateL0(ctx, s.Content)
		l1, _ := gen.GenerateL1(ctx, s.Content)
		if i < 5 {
			fmt.Printf("  [%d] L0: %s\n", i, truncate(l0, 60))
			fmt.Printf("       L1: %s\n", truncate(l1, 80))
		}
	}
	fmt.Printf("L0/L1 generated for %d chunks in %v\n", len(sections), time.Since(genStart))

	// 5. Store in SQLite (in-memory)
	fmt.Printf("\n--- Storing chunks ---\n")
	st, err := store.New(store.Config{DBPath: ":memory:", VectorDim: 0})
	if err != nil {
		log.Fatalf("Store creation failed: %v", err)
	}
	defer st.Close()

	storeStart := time.Now()
	var parentID string
	for i, sec := range sections {
		l0, _ := gen.GenerateL0(ctx, sec.Content)
		l1, _ := gen.GenerateL1(ctx, sec.Content)

		// Generate embedding if model available
		var vec []float32
		if localEmb != nil && l1 != "" {
			vec, _ = localEmb.Embed(l1)
		}

		mem := &store.Memory{
			Text:          sec.Content,
			Abstract:      l0,
			Overview:      l1,
			Category:      "document",
			Scope:         "global",
			Importance:    0.7,
			NodeType:      sec.NodeType,
			SourceFile:    sec.SourceFile,
			ChunkIndex:    i,
			TokenCount:    sec.TokenCount,
			HierarchyPath: sec.Hierarchy,
			ParentID:      parentID,
			Vector:        vec,
		}
		id, err := st.Insert(mem)
		if err != nil {
			log.Fatalf("Insert failed for chunk %d: %v", i, err)
		}
		if i == 0 {
			parentID = id // first chunk becomes parent for demo
		}
	}
	fmt.Printf("Stored %d chunks in %v\n", len(sections), time.Since(storeStart))

	// 6. Search test (if embeddings available)
	if localEmb != nil {
		fmt.Printf("\n--- Hierarchical Search Test ---\n")
		queries := []string{
			"OpenClaw 的核心原理是什么",
			"系统架构设计",
			"技术实现方案",
		}

		for _, q := range queries {
			qVec, err := localEmb.Embed(q)
			if err != nil {
				fmt.Printf("  Query embedding failed: %v\n", err)
				continue
			}

			retriever := retrieval.New(st, retrieval.DefaultConfig())
			searchStart := time.Now()
			results, err := retriever.Search(qVec, 5, nil)
			if err != nil {
				fmt.Printf("  Search failed: %v\n", err)
				continue
			}

			fmt.Printf("\n  Query: %q (%v)\n", q, time.Since(searchStart))
			for j, r := range results {
				abstract := truncate(r.Entry.Abstract, 60)
				fmt.Printf("    [%d] score=%.4f chunks=%d abstract=%q\n", j, r.Score, r.ChunkCount, abstract)
			}
		}
	} else {
		fmt.Printf("\n--- Skipping search test (no embeddings) ---\n")
		fmt.Printf("Download the model to enable: hf download electroglyph/Qwen3-Embedding-0.6B-onnx-uint8\n")
	}

	fmt.Printf("\n=== Test Complete ===\n")
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
