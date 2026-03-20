package parser

// Section represents a chunk produced by document splitting.
type Section struct {
	Content    string // The text content of this chunk
	Title      string // Heading or generated title
	ChunkIndex int    // Position within the source document
	SourceFile string // Original file path
	TokenCount int    // Estimated token count
	Hierarchy  string // Hierarchy path, e.g. "/docs/chapter1/section2"
	ParentID   string // Parent node ID (for tree structure)
	NodeType   string // "directory" | "file" | "chunk"
}

// SplitterConfig configures the SmartSplitter behavior.
type SplitterConfig struct {
	MaxChunkSize int  // Max tokens per chunk (default: 512)
	MinChunkSize int  // Min tokens per chunk (default: 256)
	OverlapSize  int  // Token overlap between adjacent chunks (default: 50) — TODO: implement overlap logic

	EnableSemantic bool   // Whether to use semantic splitting for large chunks
	LocalModelPath string // Path to local ONNX model for semantic splitting
	MinSegment     int    // Minimum segment length for ED-PELT (default: 2)
}

// BatchEmbedder is the interface required for semantic splitting.
// The LocalEmbedder in internal/embedder satisfies this interface.
type BatchEmbedder interface {
	EmbedBatch(texts []string) ([][]float32, error)
}

// DefaultSplitterConfig returns a config with sensible defaults.
func DefaultSplitterConfig() SplitterConfig {
	return SplitterConfig{
		MaxChunkSize:   512,
		MinChunkSize:   256,
		OverlapSize:    50,
		EnableSemantic: false,
		MinSegment:     2,
	}
}
