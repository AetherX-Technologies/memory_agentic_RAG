package store

// Memory 表示一条记忆（对齐 OpenViking L0/L1/L2 三层表示）
type Memory struct {
	ID             string    `json:"id"`
	Text           string    `json:"text"`                       // L2: 完整内容
	Abstract       string    `json:"abstract,omitempty"`         // L0: 摘要（~100 tokens），用于快速预览
	Overview       string    `json:"overview,omitempty"`         // L1: 概览（~500 tokens），用于向量检索
	Vector         []float32 `json:"-"`
	Category       string    `json:"category"`
	Scope          string    `json:"scope"`
	Importance     float64   `json:"importance"`
	Timestamp      int64     `json:"timestamp"`
	Metadata       string    `json:"metadata,omitempty"`
	HierarchyPath  string    `json:"hierarchy_path,omitempty"`   // 层次路径，如 "/project/src/auth"
	HierarchyLevel int       `json:"hierarchy_level,omitempty"`
	ParentID       string    `json:"parent_id,omitempty"`        // 父节点 ID（构建树结构）
	NodeType       string    `json:"node_type,omitempty"`        // "directory" | "file" | "chunk"
	SourceFile     string    `json:"source_file,omitempty"`      // 原始文件路径
	ChunkIndex     int       `json:"chunk_index,omitempty"`      // 拆分后的序号
	TokenCount     int       `json:"token_count,omitempty"`      // Token 数量
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
