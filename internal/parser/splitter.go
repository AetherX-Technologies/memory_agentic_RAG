package parser

import (
	"fmt"
	"strings"
)

// SmartSplitter implements intelligent document splitting with two strategies:
//   - Structural splitting: uses Markdown headings to divide content
//   - Semantic splitting: uses embeddings + ED-PELT to find topic boundaries
//
// The algorithm is three-phase:
//  1. Structural split by headings (fast, preserves document structure)
//  2. Check each chunk size — chunks exceeding MaxChunkSize are further split semantically
//  3. Final size adjustment — merge small chunks, force-split oversized ones
type SmartSplitter struct {
	config   SplitterConfig
	embedder BatchEmbedder // nil if semantic splitting is disabled
}

// NewSmartSplitter creates a SmartSplitter. Pass nil for embedder if semantic splitting is not needed.
func NewSmartSplitter(config SplitterConfig, embedder BatchEmbedder) *SmartSplitter {
	if config.MaxChunkSize <= 0 {
		config.MaxChunkSize = 512
	}
	if config.MinChunkSize <= 0 {
		config.MinChunkSize = 256
	}
	if config.OverlapSize < 0 {
		config.OverlapSize = 50
	}
	if config.MinSegment <= 0 {
		config.MinSegment = 2
	}
	return &SmartSplitter{
		config:   config,
		embedder: embedder,
	}
}

// Split splits document content into semantically coherent chunks.
// basePath is the source file path used for hierarchy and metadata.
func (s *SmartSplitter) Split(content string, basePath string) ([]Section, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}

	// ── Phase 1: Structural split ──
	var initialChunks []Section
	if HasHeadings(content) {
		initialChunks = SplitByHeadings(content)
	} else {
		initialChunks = []Section{{Content: content}}
	}

	// ── Phase 2: Size check — semantic split for oversized chunks ──
	var result []Section
	for _, chunk := range initialChunks {
		if chunk.Content == "" {
			continue
		}
		tc := EstimateTokenCount(chunk.Content)
		chunk.TokenCount = tc

		if tc <= s.config.MaxChunkSize {
			result = append(result, chunk)
			continue
		}

		// Chunk too large — try semantic split
		if s.embedder != nil && s.config.EnableSemantic {
			subChunks, err := semanticSplit(chunk.Content, s.embedder, s.config.MinSegment)
			if err != nil {
				// Semantic split failed — fallback to paragraph split
				subChunks = splitByParagraph(chunk.Content, s.config.MaxChunkSize)
			}
			// Propagate title to sub-chunks
			for i := range subChunks {
				if subChunks[i].Title == "" {
					subChunks[i].Title = chunk.Title
				}
			}
			result = append(result, subChunks...)
		} else {
			// No embedder — paragraph split
			subChunks := splitByParagraph(chunk.Content, s.config.MaxChunkSize)
			for i := range subChunks {
				if subChunks[i].Title == "" {
					subChunks[i].Title = chunk.Title
				}
			}
			result = append(result, subChunks...)
		}
	}

	// ── Phase 3: Final size adjustment ──
	result = s.adjustSizes(result)

	// ── Set metadata ──
	for i := range result {
		result[i].SourceFile = basePath
		result[i].NodeType = "chunk"
		result[i].ChunkIndex = i
		if result[i].TokenCount == 0 {
			result[i].TokenCount = EstimateTokenCount(result[i].Content)
		}
		if result[i].Title != "" {
			result[i].Hierarchy = fmt.Sprintf("%s/%s", basePath, result[i].Title)
		} else {
			result[i].Hierarchy = fmt.Sprintf("%s/chunk_%d", basePath, i)
		}
	}

	return result, nil
}

// adjustSizes merges chunks smaller than MinChunkSize into their predecessor,
// and force-splits chunks that still exceed MaxChunkSize after semantic splitting.
func (s *SmartSplitter) adjustSizes(sections []Section) []Section {
	if len(sections) == 0 {
		return sections
	}

	// Pass 1: Merge small chunks into previous
	var merged []Section
	for _, sec := range sections {
		tc := EstimateTokenCount(sec.Content)
		if tc < s.config.MinChunkSize && len(merged) > 0 {
			// Merge into previous chunk
			prev := &merged[len(merged)-1]
			prev.Content += "\n\n" + sec.Content
			prev.TokenCount = EstimateTokenCount(prev.Content)
		} else {
			sec.TokenCount = tc
			merged = append(merged, sec)
		}
	}

	// Pass 2: Force-split chunks that are still too large
	var final []Section
	for _, sec := range merged {
		if sec.TokenCount > int(float64(s.config.MaxChunkSize)*1.5) {
			// Way too large — force paragraph split
			subChunks := splitByParagraph(sec.Content, s.config.MaxChunkSize)
			for i := range subChunks {
				subChunks[i].Title = sec.Title
			}
			final = append(final, subChunks...)
		} else {
			final = append(final, sec)
		}
	}

	return final
}
