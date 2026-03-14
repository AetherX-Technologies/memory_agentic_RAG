package store

// Memory 表示一条记忆
type Memory struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	Vector     []float32 `json:"-"`
	Category   string  `json:"category"`
	Scope      string  `json:"scope"`
	Importance float64 `json:"importance"`
	Timestamp  int64   `json:"timestamp"`
	Metadata   string  `json:"metadata,omitempty"`
}

// SearchResult 表示检索结果
type SearchResult struct {
	Entry Memory  `json:"entry"`
	Score float64 `json:"score"`
}

// Config 存储配置
type Config struct {
	DBPath       string
	VectorDim    int
	RerankConfig RerankConfig
}
