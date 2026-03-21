package store

// Memory 表示一条记忆（OpenViking L0/L1/L2 + AI 记忆系统）
type Memory struct {
	// ── 基础字段 ──
	ID             string    `json:"id"`
	Text           string    `json:"text"`                       // L2: 完整内容
	Abstract       string    `json:"abstract,omitempty"`         // L0: 摘要
	Overview       string    `json:"overview,omitempty"`         // L1: 概览
	Vector         []float32 `json:"-"`
	Category       string    `json:"category"`
	Scope          string    `json:"scope"`
	Importance     float64   `json:"importance"`
	Timestamp      int64     `json:"timestamp"`
	Metadata       string    `json:"metadata,omitempty"`

	// ── OpenViking 层次结构 ──
	HierarchyPath  string `json:"hierarchy_path,omitempty"`
	HierarchyLevel int    `json:"hierarchy_level,omitempty"`
	ParentID       string `json:"parent_id,omitempty"`
	NodeType       string `json:"node_type,omitempty"`        // "directory" | "file" | "chunk"
	SourceFile     string `json:"source_file,omitempty"`
	ChunkIndex     int    `json:"chunk_index,omitempty"`
	TokenCount     int    `json:"token_count,omitempty"`

	// ── AI 记忆系统 ──
	MemoryType   string  `json:"memory_type,omitempty"`   // fact | preference | skill | episode | instruction | relationship
	Confidence   float64 `json:"confidence,omitempty"`    // 置信度 [0.05, 1.0]
	AccessCount  int     `json:"access_count,omitempty"`  // 被召回次数
	LastAccessed int64   `json:"last_accessed,omitempty"` // 最后召回时间戳
	ExpiresAt    int64   `json:"expires_at,omitempty"`    // 过期时间戳（0=永不）
	SourceConv   string  `json:"source_conv,omitempty"`   // 来源对话 ID
	ContentHash  string  `json:"content_hash,omitempty"`  // SHA256 前 16 位
	Expired      bool    `json:"expired,omitempty"`       // 是否已过期
}

// SearchResult 表示检索结果
type SearchResult struct {
	Entry      Memory  `json:"entry"`
	Score      float64 `json:"score"`
	ChunkCount int     `json:"chunk_count,omitempty"` // 聚合时同文件的 chunk 总数
	ContentURL string  `json:"content_url,omitempty"` // v2 API: 按需加载 URL
}

// Config 存储配置
type Config struct {
	DBPath       string
	VectorDim    int
	RerankConfig RerankConfig
}
