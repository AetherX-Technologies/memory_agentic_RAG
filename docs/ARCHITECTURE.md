# HybridMem-RAG 架构文档

## 系统概览

HybridMem-RAG 是一个混合检索增强生成（RAG）系统，采用纯 Go 实现，支持跨平台部署。

**核心特性**：
- 混合检索：向量检索 + BM25 全文检索 + RRF 融合
- 交叉编码器重排：可选的 Jina Reranker 精排
- 多维度评分：新近度、重要性、长度归一化
- 纯 Go 实现：无 CGO 依赖，真正跨平台

## 架构分层

```
┌─────────────────────────────────────────┐
│         HTTP API Layer                  │
│    (cmd/server, internal/api)           │
└──────────────┬──────────────────────────┘
               ↓
┌─────────────────────────────────────────┐
│      Hybrid Search Engine               │
│    (internal/store/hybrid.go)           │
│  ┌─────────────┬──────────────────┐     │
│  │ Vector      │ BM25 (FTS5)      │     │
│  │ Search      │ Search           │     │
│  └──────┬──────┴────────┬─────────┘     │
│         └───────┬────────┘               │
│              RRF Fusion                  │
│                 ↓                        │
│         Rerank (Optional)                │
│    (internal/store/rerank.go)           │
└──────────────┬──────────────────────────┘
               ↓
┌─────────────────────────────────────────┐
│       Storage Layer                     │
│    (internal/store/store.go)            │
│  - SQLite (vectors + metadata)          │
│  - FTS5 (full-text index)               │
└─────────────────────────────────────────┘
```

## 核心模块

### 1. 存储层 (Storage Layer)

**文件**: `internal/store/store.go`

**职责**：
- 记忆的 CRUD 操作
- 向量序列化/反序列化
- SQLite 表结构管理

**表结构**：

```sql
-- 主表
CREATE TABLE vectors (
    id TEXT PRIMARY KEY,
    text TEXT NOT NULL,
    vector BLOB NOT NULL,
    category TEXT,
    scope TEXT,
    importance REAL,
    metadata TEXT,
    timestamp INTEGER
);

-- FTS5 全文索引
CREATE VIRTUAL TABLE vectors_fts USING fts5(
    id UNINDEXED,
    text,
    content='vectors',
    content_rowid='rowid'
);
```

**关键函数**：
- `Insert()`: 插入记忆，同时更新 FTS5 索引
- `VectorSearch()`: 余弦相似度检索
- `BM25Search()`: FTS5 全文检索
- `Delete()`: 删除记忆及索引

### 2. 混合检索引擎 (Hybrid Search)

**文件**: `internal/store/hybrid.go`

**算法流程**：

```
Query → Vectorize
         ↓
    ┌────┴────┐
    ↓         ↓
Vector     BM25
Search     Search
    ↓         ↓
    └────┬────┘
         ↓
    RRF Fusion
         ↓
    Rerank (Optional)
         ↓
    Score Adjustment
    - Recency boost
    - Importance weight
    - Length normalization
         ↓
    Top-K Results
```

**RRF 融合算法**：

```go
// Reciprocal Rank Fusion
score = 1 / (rank + 60)

// 合并两个排序列表
finalScore = vectorScore + bm25Score
```

**评分调整**：

1. **新近度提升** (Recency Boost):
   ```go
   days := (now - timestamp) / 86400
   boost := math.Exp(-days/14) * 0.1
   score += boost
   ```

2. **重要性加权** (Importance Weight):
   ```go
   score *= (0.7 + 0.3 * importance)
   ```

3. **长度归一化** (Length Normalization):
   ```go
   if len(text) < 100 {
       score *= 1.1  // 短文本奖励
   } else if len(text) > 500 {
       score *= 0.9  // 长文本惩罚
   }
   ```

### 3. 交叉编码器重排 (Rerank)

**文件**: `internal/store/rerank.go`

**工作原理**：

```
Candidates (Top 20)
    ↓
Jina Reranker API
    ↓
Relevance Scores
    ↓
Score Blending
    ↓
Final Ranking
```

**分数混合策略**：

```go
// 返回的结果：混合分数
blendedScore = 0.6 × rerankScore + 0.4 × originalScore

// 未返回的结果：惩罚
penaltyScore = originalScore × 0.8
```

**降级机制**：
- API 调用失败 → 返回原始结果
- 超时 → 返回原始结果
- 无 API Key → 跳过 Rerank

### 4. HTTP API 层

**文件**: `cmd/server/main.go`, `internal/api/`

**端点**：

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | /api/memories | 创建记忆 |
| GET | /api/memories/search | 检索记忆 |
| DELETE | /api/memories/:id | 删除记忆 |
| PUT | /api/memories/:id | 更新记忆 |
| GET | /api/memories/stats | 统计信息 |

**中间件**：
- 请求体大小限制（10MB）
- JSON 解析
- 错误处理

## 数据流

### 插入流程

```
Client Request
    ↓
POST /api/memories
    ↓
Parse JSON
    ↓
Validate Input
    ↓
Store.Insert()
    ├─ Insert into vectors table
    └─ Update FTS5 index
    ↓
Return ID
```

### 检索流程

```
Client Request
    ↓
GET /api/memories/search?q=...
    ↓
Parse Query
    ↓
HybridSearch()
    ├─ VectorSearch() (parallel)
    ├─ BM25Search() (parallel)
    ↓
RRF Fusion
    ↓
Rerank (if enabled)
    ↓
Score Adjustment
    ├─ Recency boost
    ├─ Importance weight
    └─ Length normalization
    ↓
Sort & Limit
    ↓
Return Results
```

## 性能特征

### 基准测试结果

**环境**: MacBook Pro M1, 16GB RAM

| 数据量 | 检索时间 | 内存使用 |
|--------|----------|----------|
| 1,000 | 8ms | ~50MB |
| 5,000 | 40ms | ~150MB |
| 10,000 | 83ms | ~250MB |

**瓶颈分析**（pprof）：
- SQLite I/O: 69.81%
- 向量计算: 1.94%
- JSON 序列化: 5.23%

**优化措施**：
- 向量归一化预计算
- 并行执行 Vector + BM25
- 连接池复用

### 扩展性

**水平扩展**：
- 无状态设计，支持多实例部署
- SQLite 适合单机场景（< 100万条）
- 大规模场景可迁移到 PostgreSQL + pgvector

**垂直扩展**：
- 内存：线性增长，约 25KB/条记忆
- CPU：并行检索，多核友好
- 磁盘：SQLite 压缩存储，约 10KB/条

## 配置参数

### 检索参数

```go
type SearchConfig struct {
    VectorWeight float64  // 向量检索权重（默认 0.5）
    BM25Weight   float64  // BM25 权重（默认 0.5）
    RRFConstant  int      // RRF 常数 k（默认 60）
    TopK         int      // 返回结果数（默认 10）
}
```

### Rerank 参数

```go
type RerankConfig struct {
    Enabled          bool    // 是否启用
    Provider         string  // "jina"
    APIKey           string  // API Key
    Model            string  // 模型名称
    BlendWeight      float64 // 混合权重（默认 0.6）
    UnreturnedPenalty float64 // 未返回惩罚（默认 0.8）
}
```

### 评分参数

```go
const (
    RecencyHalfLife    = 14    // 新近度半衰期（天）
    RecencyBoostMax    = 0.1   // 最大新近度提升
    ImportanceWeight   = 0.3   // 重要性权重
    ShortTextBonus     = 1.1   // 短文本奖励
    LongTextPenalty    = 0.9   // 长文本惩罚
)
```

## 移动端适配

**文件**: `pkg/mobile/api.go`

**接口设计**：

```go
type MemoryDB struct {
    store *store.Store
}

func NewMemoryDB(dbPath string) (*MemoryDB, error)
func (m *MemoryDB) Insert(text string, vector []float32, ...) (string, error)
func (m *MemoryDB) Search(query string, limit int) (string, error)  // JSON
func (m *MemoryDB) Delete(id string) error
func (m *MemoryDB) Close() error
```

**编译目标**：
- iOS: `gomobile bind -target=ios`
- Android: `gomobile bind -target=android`

## 安全考虑

1. **SQL 注入防护**: 使用参数化查询
2. **请求体限制**: 10MB 上限
3. **API Key 保护**: 环境变量存储
4. **作用域隔离**: 支持 agent/global/custom 作用域

## 扩展点

### 1. 自定义评分函数

```go
type ScoreAdjuster interface {
    Adjust(result SearchResult) float64
}

// 注册自定义调整器
store.RegisterAdjuster(myAdjuster)
```

### 2. 自定义 Reranker

```go
type Reranker interface {
    Rerank(query string, candidates []SearchResult) ([]SearchResult, error)
}

// 替换默认 Reranker
store.SetReranker(myReranker)
```

### 3. 向量化服务

当前使用外部向量化（客户端提供向量），可扩展为：

```go
type Embedder interface {
    Embed(text string) ([]float32, error)
}

// 集成向量化服务
store.SetEmbedder(jinaEmbedder)
```

## 技术选型

| 组件 | 技术 | 理由 |
|------|------|------|
| 数据库 | SQLite | 零配置、跨平台、嵌入式 |
| 全文检索 | FTS5 | SQLite 内置、性能优秀 |
| 向量计算 | 纯 Go | 无 CGO 依赖、可移植 |
| HTTP 框架 | 标准库 | 轻量、稳定 |
| 序列化 | JSON | 通用、易调试 |

## 参考资料

- [SQLite FTS5 文档](https://www.sqlite.org/fts5.html)
- [RRF 论文](https://plg.uwaterloo.ca/~gvcormac/cormacksigir09-rrf.pdf)
- [Jina Reranker API](https://jina.ai/reranker)
