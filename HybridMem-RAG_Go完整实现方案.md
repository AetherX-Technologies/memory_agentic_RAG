# HybridMem-RAG Go 实现方案

## 一、技术栈选择

### 1.1 向量数据库选择

**推荐：PostgreSQL + pgvector**

理由：
- ✅ Go 生态成熟（`pgx` 驱动）
- ✅ 支持向量检索 + 全文检索（tsvector）
- ✅ 支持 JSONB 存储结构化字段
- ✅ 事务支持，便于实现连接关系
- ✅ 部署简单，运维成熟

备选方案：
- **Weaviate**：功能强大，但需要额外部署
- **Qdrant**：性能好，但 Go SDK 不如 Python/Rust 成熟
- **Milvus**：企业级，但过于重量

### 1.2 核心依赖

```go
// 数据库
"github.com/jackc/pgx/v5"
"github.com/jackc/pgx/v5/pgxpool"

// 向量操作
"github.com/pgvector/pgvector-go"

// LLM 调用
"github.com/sashabaranov/go-openai"

// 全文检索（BM25）
// PostgreSQL 内置 tsvector，无需额外依赖

// 配置管理
"github.com/spf13/viper"

// 日志
"go.uber.org/zap"
```

---

## 二、数据模型设计

### 2.1 核心表结构

```sql
-- 记忆表
CREATE TABLE memories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- 基础内容
    raw_text TEXT NOT NULL,
    summary TEXT,
    vector vector(1024),  -- pgvector 类型

    -- 结构化字段（借鉴 Memory Agent）
    entities JSONB DEFAULT '[]',
    topics JSONB DEFAULT '[]',

    -- 元数据（借鉴 LanceDB-Pro）
    category VARCHAR(50) DEFAULT 'other',  -- fact, preference, decision, entity, insight
    scope VARCHAR(100) DEFAULT 'global',   -- global, agent:<id>, custom:<name>
    importance FLOAT DEFAULT 0.5,

    -- 层次结构（借鉴 OpenViking）
    uri VARCHAR(500),
    parent_uri VARCHAR(500),
    level INT DEFAULT 2,  -- 0=L0(abstract), 1=L1(overview), 2=L2(detail)

    -- 状态
    consolidated BOOLEAN DEFAULT FALSE,

    -- 全文检索
    tsv tsvector GENERATED ALWAYS AS (
        to_tsvector('english', coalesce(raw_text, '') || ' ' || coalesce(summary, ''))
    ) STORED,

    -- 时间戳
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- 向量索引（HNSW）
CREATE INDEX ON memories USING hnsw (vector vector_cosine_ops);

-- 全文检索索引（GIN）
CREATE INDEX ON memories USING gin(tsv);

-- 作用域索引
CREATE INDEX ON memories(scope);

-- URI 索引（支持前缀查询）
CREATE INDEX ON memories(uri text_pattern_ops);

-- 整合状态索引
CREATE INDEX ON memories(consolidated) WHERE NOT consolidated;
```

### 2.2 连接关系表

```sql
-- 记忆连接表（双向图）
CREATE TABLE memory_connections (
    from_id UUID REFERENCES memories(id) ON DELETE CASCADE,
    to_id UUID REFERENCES memories(id) ON DELETE CASCADE,
    relationship VARCHAR(50) NOT NULL,  -- "因果", "对比", "补充", "引用"
    strength FLOAT DEFAULT 0.5,         -- 0-1，连接强度
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (from_id, to_id)
);

CREATE INDEX ON memory_connections(from_id);
CREATE INDEX ON memory_connections(to_id);
CREATE INDEX ON memory_connections(strength) WHERE strength > 0.7;
```

### 2.3 整合记录表

```sql
-- 整合记录表（类似 Memory Agent 的 Consolidation）
CREATE TABLE consolidations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_ids UUID[] NOT NULL,
    summary TEXT NOT NULL,
    insight TEXT NOT NULL,
    patterns JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX ON consolidations USING gin(source_ids);
```

---

## 三、核心模块设计

### 3.1 项目结构

```
memory-rag/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── domain/
│   │   └── models.go
│   ├── repository/
│   │   ├── memory_repo.go
│   │   └── connection_repo.go
│   ├── service/
│   │   ├── ingest_service.go
│   │   ├── retrieval_service.go
│   │   ├── consolidate_service.go
│   │   └── scheduler.go
│   ├── llm/
│   │   └── client.go
│   └── config/
│       └── config.go
├── pkg/
│   ├── vector/
│   │   └── embedder.go
│   └── scoring/
│       └── pipeline.go
└── go.mod
```

### 3.2 领域模型

```go
package domain

type Memory struct {
    ID           string
    RawText      string
    Summary      string
    Vector       []float32
    Entities     []string
    Topics       []string
    Category     string
    Scope        string
    Importance   float64
    URI          string
    ParentURI    *string
    Level        int
    Consolidated bool
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type Connection struct {
    FromID       string
    ToID         string
    Relationship string
    Strength     float64
}

type SearchResult struct {
    Memory Memory
    Score  float64
}
```

## 四、核心算法实现

### 4.1 混合检索（Vector + BM25）

```go
package service

type RetrievalService struct {
    repo   repository.MemoryRepository
    embed  vector.Embedder
    scorer scoring.Pipeline
}

func (s *RetrievalService) Search(ctx context.Context, query string, scope []string, limit int) ([]domain.SearchResult, error) {
    // 1. 向量化查询
    queryVec, err := s.embed.Embed(ctx, query)
    if err != nil {
        return nil, err
    }

    // 2. 并行检索
    vectorCh := make(chan []domain.SearchResult, 1)
    bm25Ch := make(chan []domain.SearchResult, 1)

    go func() {
        results, _ := s.repo.VectorSearch(ctx, queryVec, scope, limit*2)
        vectorCh <- results
    }()

    go func() {
        results, _ := s.repo.BM25Search(ctx, query, scope, limit*2)
        bm25Ch <- results
    }()

    vectorResults := <-vectorCh
    bm25Results := <-bm25Ch

    // 3. RRF 融合
    fused := s.rrfFusion(vectorResults, bm25Results)

    // 4. 12阶段评分管道
    scored := s.scorer.Score(ctx, query, fused)

    // 5. 返回 Top-K
    if len(scored) > limit {
        scored = scored[:limit]
    }

    return scored, nil
}

// RRF 融合算法
func (s *RetrievalService) rrfFusion(vector, bm25 []domain.SearchResult) []domain.SearchResult {
    const k = 60.0
    scoreMap := make(map[string]float64)
    memoryMap := make(map[string]domain.Memory)

    // Vector 分数
    for rank, r := range vector {
        scoreMap[r.Memory.ID] += 1.0 / (float64(rank) + k)
        memoryMap[r.Memory.ID] = r.Memory
    }

    // BM25 分数（权重 1.15）
    for rank, r := range bm25 {
        scoreMap[r.Memory.ID] += 1.15 / (float64(rank) + k)
        memoryMap[r.Memory.ID] = r.Memory
    }

    // 合并结果
    var results []domain.SearchResult
    for id, score := range scoreMap {
        results = append(results, domain.SearchResult{
            Memory: memoryMap[id],
            Score:  score,
        })
    }

    // 排序
    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })

    return results
}
```

### 4.2 PostgreSQL 检索实现

```go
package repository

// Vector 检索（使用 pgvector）
func (r *MemoryRepository) VectorSearch(ctx context.Context, queryVec []float32, scopes []string, limit int) ([]domain.SearchResult, error) {
    query := `
        SELECT id, raw_text, summary, entities, topics, category, scope,
               importance, uri, parent_uri, level, created_at,
               1 - (vector <=> $1) as score
        FROM memories
        WHERE scope = ANY($2)
        ORDER BY vector <=> $1
        LIMIT $3
    `

    rows, err := r.db.Query(ctx, query, pgvector.NewVector(queryVec), scopes, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var results []domain.SearchResult
    for rows.Next() {
        var m domain.Memory
        var score float64
        err := rows.Scan(&m.ID, &m.RawText, &m.Summary, &m.Entities, &m.Topics,
            &m.Category, &m.Scope, &m.Importance, &m.URI, &m.ParentURI,
            &m.Level, &m.CreatedAt, &score)
        if err != nil {
            continue
        }
        results = append(results, domain.SearchResult{Memory: m, Score: score})
    }

    return results, nil
}

// BM25 检索（使用 PostgreSQL tsvector）
func (r *MemoryRepository) BM25Search(ctx context.Context, query string, scopes []string, limit int) ([]domain.SearchResult, error) {
    sql := `
        SELECT id, raw_text, summary, entities, topics, category, scope,
               importance, uri, parent_uri, level, created_at,
               ts_rank(tsv, plainto_tsquery('english', $1)) as score
        FROM memories
        WHERE scope = ANY($2)
          AND tsv @@ plainto_tsquery('english', $1)
        ORDER BY score DESC
        LIMIT $3
    `

    rows, err := r.db.Query(ctx, sql, query, scopes, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var results []domain.SearchResult
    for rows.Next() {
        var m domain.Memory
        var score float64
        err := rows.Scan(&m.ID, &m.RawText, &m.Summary, &m.Entities, &m.Topics,
            &m.Category, &m.Scope, &m.Importance, &m.URI, &m.ParentURI,
            &m.Level, &m.CreatedAt, &score)
        if err != nil {
            continue
        }
        results = append(results, domain.SearchResult{Memory: m, Score: score})
    }

    return results, nil
}
```

### 4.3 12阶段评分管道

```go
package scoring

type Pipeline struct {
    connRepo repository.ConnectionRepository
    reranker Reranker
}

type Config struct {
    RecencyHalfLife      int     // 新近度半衰期（天）
    RecencyWeight        float64 // 新近度权重
    TimeDecayHalfLife    int     // 时间衰减半衰期（天）
    LengthNormAnchor     int     // 长度归一化锚点（字符数）
    HardMinScore         float64 // 硬性最低分数
    ConnectionBoostWeight float64 // 关联加权系数
}

func (p *Pipeline) Score(ctx context.Context, query string, results []domain.SearchResult) []domain.SearchResult {
    // Stage 1-4: 已在 RRF 融合中完成

    // Stage 5: 交叉编码器重排（可选）
    if p.reranker != nil {
        results = p.crossEncoderRerank(ctx, query, results)
    }

    // Stage 6: 新近度提升
    results = p.applyRecencyBoost(results)

    // Stage 7: 重要性加权
    results = p.applyImportanceWeight(results)

    // Stage 8: 长度归一化
    results = p.applyLengthNorm(results)

    // Stage 9: 时间衰减
    results = p.applyTimeDecay(results)

    // Stage 10: 关联图谱加权 ⭐
    results = p.applyConnectionBoost(ctx, results)

    // Stage 11: 硬性过滤
    results = p.hardFilter(results)

    // Stage 12: MMR 多样性
    results = p.applyMMR(results)

    return results
}

// Stage 6: 新近度提升
func (p *Pipeline) applyRecencyBoost(results []domain.SearchResult) []domain.SearchResult {
    now := time.Now()
    for i := range results {
        ageDays := now.Sub(results[i].Memory.CreatedAt).Hours() / 24
        boost := math.Exp(-ageDays/float64(p.config.RecencyHalfLife)) * p.config.RecencyWeight
        results[i].Score += boost
    }
    return results
}

// Stage 7: 重要性加权
func (p *Pipeline) applyImportanceWeight(results []domain.SearchResult) []domain.SearchResult {
    for i := range results {
        importance := results[i].Memory.Importance
        results[i].Score *= (0.7 + 0.3*importance)
    }
    return results
}

// Stage 8: 长度归一化
func (p *Pipeline) applyLengthNorm(results []domain.SearchResult) []domain.SearchResult {
    anchor := float64(p.config.LengthNormAnchor)
    for i := range results {
        length := float64(len(results[i].Memory.RawText))
        penalty := 1.0 / (1.0 + 0.5*math.Log2(length/anchor))
        results[i].Score *= penalty
    }
    return results
}

// Stage 9: 时间衰减
func (p *Pipeline) applyTimeDecay(results []domain.SearchResult) []domain.SearchResult {
    now := time.Now()
    for i := range results {
        ageDays := now.Sub(results[i].Memory.CreatedAt).Hours() / 24
        decay := 0.5 + 0.5*math.Exp(-ageDays/float64(p.config.TimeDecayHalfLife))
        results[i].Score *= decay
    }
    return results
}

// Stage 10: 关联图谱加权 ⭐ 核心创新
func (p *Pipeline) applyConnectionBoost(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
    // 构建结果ID到分数的映射
    scoreMap := make(map[string]float64)
    for _, r := range results {
        scoreMap[r.Memory.ID] = r.Score
    }

    // 为每个结果计算连接加权
    for i := range results {
        memoryID := results[i].Memory.ID

        // 获取该记忆的所有连接
        connections, err := p.connRepo.GetConnections(ctx, memoryID)
        if err != nil {
            continue
        }

        // 计算连接加权：如果连接到高分记忆，获得加权
        var boost float64
        for _, conn := range connections {
            if connectedScore, exists := scoreMap[conn.ToID]; exists {
                // 加权 = 连接强度 × 目标分数 × 权重系数
                boost += conn.Strength * connectedScore * p.config.ConnectionBoostWeight
            }
        }

        results[i].Score += boost
    }

    // 重新排序
    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })

    return results
}

// Stage 11: 硬性过滤
func (p *Pipeline) hardFilter(results []domain.SearchResult) []domain.SearchResult {
    var filtered []domain.SearchResult
    for _, r := range results {
        if r.Score >= p.config.HardMinScore {
            filtered = append(filtered, r)
        }
    }
    return filtered
}

// Stage 12: MMR 多样性
func (p *Pipeline) applyMMR(results []domain.SearchResult) []domain.SearchResult {
    if len(results) <= 1 {
        return results
    }

    // 简化版 MMR：降权相似度 > 0.85 的结果
    for i := 0; i < len(results); i++ {
        for j := i + 1; j < len(results); j++ {
            sim := cosineSimilarity(results[i].Memory.Vector, results[j].Memory.Vector)
            if sim > 0.85 {
                results[j].Score *= 0.8
            }
        }
    }

    // 重新排序
    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })

    return results
}

func cosineSimilarity(a, b []float32) float64 {
    var dot, normA, normB float64
    for i := range a {
        dot += float64(a[i] * b[i])
        normA += float64(a[i] * a[i])
        normB += float64(b[i] * b[i])
    }
    return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
```

### 4.4 主动整合机制（借鉴 Memory Agent + LanceDB-Pro）

```go
package service

type ConsolidateService struct {
    repo     repository.MemoryRepository
    connRepo repository.ConnectionRepository
    llm      llm.Client
}

// 整合未整合的记忆
func (s *ConsolidateService) Consolidate(ctx context.Context) (*domain.Consolidation, error) {
    // 1. 获取未整合的记忆
    memories, err := s.repo.ListUnconsolidated(ctx, 100)
    if err != nil {
        return nil, err
    }

    if len(memories) < 2 {
        return nil, nil // 记忆太少，跳过
    }

    // 2. LLM 分析关联和洞见
    analysis, err := s.llm.AnalyzeConnections(ctx, memories)
    if err != nil {
        return nil, err
    }

    // 3. 更新连接关系
    for _, conn := range analysis.Connections {
        if err := s.connRepo.Create(ctx, conn); err != nil {
            // 记录错误但继续
            continue
        }
    }

    // 4. 生成洞见记忆（类似 LanceDB-Pro 的 lesson）
    for _, insight := range analysis.Insights {
        insightMemory := &domain.Memory{
            RawText:    insight.Content,
            Summary:    insight.Summary,
            Category:   "insight",
            Importance: 0.9, // 洞见通常很重要
            Scope:      "global",
            Level:      0, // L0 层级（抽象）
        }

        // 向量化
        vec, _ := s.embed.Embed(ctx, insight.Content)
        insightMemory.Vector = vec

        // 存储
        if err := s.repo.Create(ctx, insightMemory); err != nil {
            continue
        }

        // 建立洞见与源记忆的连接
        for _, sourceID := range insight.SourceIDs {
            conn := &domain.Connection{
                FromID:       insightMemory.ID,
                ToID:         sourceID,
                Relationship: "总结自",
                Strength:     0.8,
            }
            s.connRepo.Create(ctx, conn)
        }
    }

    // 5. 标记源记忆为已整合
    sourceIDs := make([]string, len(memories))
    for i, m := range memories {
        sourceIDs[i] = m.ID
    }
    s.repo.MarkConsolidated(ctx, sourceIDs)

    // 6. 创建整合记录
    consolidation := &domain.Consolidation{
        SourceIDs: sourceIDs,
        Summary:   analysis.Summary,
        Insight:   analysis.MainInsight,
        Patterns:  analysis.Patterns,
    }

    return consolidation, nil
}
```

### 4.5 LLM 分析 Prompt

```go
package llm

const analyzeConnectionsPrompt = `分析以下记忆片段，找出它们之间的关联关系和潜在洞见。

记忆列表：
{{range .Memories}}
- [{{.ID}}] {{.Summary}} (实体: {{.Entities}}, 主题: {{.Topics}})
{{end}}

请返回 JSON 格式：
{
  "summary": "整体摘要",
  "main_insight": "核心洞见（1-2句话）",
  "patterns": ["模式1", "模式2"],
  "connections": [
    {
      "from_id": "记忆ID1",
      "to_id": "记忆ID2",
      "relationship": "因果|对比|补充|引用",
      "strength": 0.0-1.0,
      "reason": "连接原因"
    }
  ],
  "insights": [
    {
      "content": "洞见内容（完整描述）",
      "summary": "洞见摘要",
      "source_ids": ["相关记忆ID1", "相关记忆ID2"]
    }
  ]
}

规则：
1. connections 只包含确实存在关联的记忆对
2. strength 表示连接强度：0.9-1.0=强关联，0.7-0.9=中等，0.5-0.7=弱关联
3. insights 是跨记忆的高层次发现，不是简单重复
4. 如果没有明显关联，connections 可以为空数组

只返回 JSON，不要其他内容。`
```

### 4.6 定时调度器

```go
package service

type ConsolidateScheduler struct {
    service              *ConsolidateService
    interval             time.Duration
    minMemoriesThreshold int
    stopCh               chan struct{}
}

func NewConsolidateScheduler(service *ConsolidateService, interval time.Duration, minMemories int) *ConsolidateScheduler {
    return &ConsolidateScheduler{
        service:              service,
        interval:             interval,
        minMemoriesThreshold: minMemories,
        stopCh:               make(chan struct{}),
    }
}

func (s *ConsolidateScheduler) Start(ctx context.Context) {
    ticker := time.NewTicker(s.interval)
    go func() {
        for {
            select {
            case <-ticker.C:
                s.runConsolidation(ctx)
            case <-s.stopCh:
                ticker.Stop()
                return
            }
        }
    }()
}

func (s *ConsolidateScheduler) Stop() {
    close(s.stopCh)
}

func (s *ConsolidateScheduler) runConsolidation(ctx context.Context) {
    count, err := s.service.repo.CountUnconsolidated(ctx)
    if err != nil {
        return
    }

    if count < s.minMemoriesThreshold {
        return // 未达到阈值，跳过
    }

    // 执行整合
    _, _ = s.service.Consolidate(ctx)
}
```

---

## 五、与三个参考系统的对比

| 特性 | Memory Agent | LanceDB-Pro | OpenViking | **HybridMem-RAG (Go)** |
|------|--------------|-------------|------------|------------------------|
| 语言 | Go | TypeScript | Python | **Go** |
| 数据库 | SQLite | LanceDB | VikingDB | **PostgreSQL + pgvector** |
| 向量检索 | ❌ | ✅ | ✅ | ✅ |
| BM25检索 | ❌ | ✅ | ❌ | ✅ |
| 混合检索 | ❌ | ✅ | ❌ | ✅ |
| 结构化提取 | ✅ | ❌ | ❌ | ✅ |
| 作用域隔离 | ❌ | ✅ | ✅ (通过uri) | ✅ |
| 层次结构 | ❌ | ❌ | ✅ | ✅ |
| 主动整合 | ✅ (自动) | ✅ (手动/worker) | ❌ | ✅ (自动) |
| 关联图谱 | ✅ (Connections字段) | ❌ | ✅ (related_uri) | ✅ (双向图) |
| 图谱加权 | ❌ | ❌ | ❌ | ✅ (Stage 10) |
| 12阶段评分 | ❌ | ✅ | ❌ | ✅ |

**核心创新**：
1. ✅ 融合三个系统的优势
2. ✅ 新增"关联图谱加权"（Stage 10）
3. ✅ 统一的 Go 技术栈
4. ✅ PostgreSQL 简化部署

---

## 六、实施路径

### 阶段1：基础框架（1周）

**目标**：搭建项目骨架，实现基础存储和检索

**任务**：
- [ ] 初始化 Go 项目，配置依赖
- [ ] 创建 PostgreSQL 数据库和表结构
- [ ] 实现 `MemoryRepository` 基础 CRUD
- [ ] 实现向量化（调用 OpenAI API）
- [ ] 实现简单的向量检索

**验证**：
```bash
# 存储一条记忆
curl -X POST http://localhost:8080/memories \
  -d '{"text": "用户喜欢简洁的代码风格"}'

# 检索
curl http://localhost:8080/search?q=代码风格
```

### 阶段2：混合检索和评分管道（1-2周）

**目标**：实现 Vector + BM25 混合检索和12阶段评分

**任务**：
- [ ] 实现 BM25 全文检索（PostgreSQL tsvector）
- [ ] 实现 RRF 融合算法
- [ ] 实现12阶段评分管道（Stage 1-9, 11-12）
- [ ] 添加作用域过滤

**验证**：
- 测试纯语义查询（应召回语义相似的记忆）
- 测试精确关键词查询（应召回包含关键词的记忆）
- 对比混合检索 vs 纯向量检索的召回率

### 阶段3：结构化提取和层次结构（1周）

**目标**：LLM 结构化提取 + 虚拟层次结构

**任务**：
- [ ] 实现 LLM 结构化提取（summary/entities/topics）
- [ ] 定义预设目录结构（参考 OpenViking）
- [ ] 实现 URI 生成逻辑
- [ ] 支持按 URI 前缀过滤检索

**验证**：
- 存储记忆时自动提取结构化字段
- 验证 URI 层次结构正确
- 测试分层检索效果

### 阶段4：主动整合和关联图谱（1-2周）

**目标**：实现核心创新功能

**任务**：
- [ ] 实现连接关系存储和查询
- [ ] 实现主动整合服务（LLM 分析关联）
- [ ] 实现定时调度器（30分钟或10条触发）
- [ ] 实现 Stage 10：关联图谱加权
- [ ] 生成洞见记忆（类似 LanceDB-Pro 的 lesson）

**验证**：
- 存储多条相关记忆，等待整合触发
- 检查是否生成了连接关系
- 验证是否产生了洞见记忆
- 测试关联图谱加权是否提升了召回质量

### 阶段5：性能优化和生产化（1-2周）

**目标**：优化性能，准备生产部署

**任务**：
- [ ] 添加连接关系的索引优化
- [ ] 实现图遍历缓存（Redis）
- [ ] 添加可观测性（OpenTelemetry）
- [ ] 编写单元测试和集成测试
- [ ] 性能基准测试（目标：检索 < 500ms）
- [ ] 编写部署文档（Docker Compose）

**验证**：
- 压力测试（1000 QPS）
- 内存和 CPU 使用率监控
- 端到端延迟分析

---

## 七、关键配置

```yaml
# config.yaml
database:
  host: localhost
  port: 5432
  database: memory_rag
  user: postgres
  password: ${DB_PASSWORD}
  pool_size: 20

embedding:
  provider: openai
  api_key: ${OPENAI_API_KEY}
  model: text-embedding-3-large
  dimensions: 1024

llm:
  provider: openai
  api_key: ${OPENAI_API_KEY}
  model: gpt-4o-mini

retrieval:
  candidate_pool_size: 20
  recency_half_life_days: 14
  recency_weight: 0.1
  time_decay_half_life_days: 60
  length_norm_anchor: 500
  hard_min_score: 0.35
  connection_boost_weight: 0.1

consolidation:
  interval_minutes: 30
  min_memories_threshold: 10
  enabled: true

scopes:
  default: global
  agent_access:
    main: [global, agent:main]
    discord-bot: [global, agent:discord-bot]
```

---

## 八、总结

### 核心优势

1. **统一技术栈**：纯 Go 实现，部署简单
2. **成熟数据库**：PostgreSQL + pgvector，运维友好
3. **融合三家之长**：
   - Memory Agent 的结构化提取和主动整合
   - LanceDB-Pro 的混合检索和12阶段评分
   - OpenViking 的层次结构
4. **核心创新**：关联图谱加权（Stage 10）
5. **生产就绪**：事务支持、索引优化、可观测性

### 与原设计方案的改进

| 原方案问题 | 改进方案 |
|-----------|---------|
| 假设三个系统可简单融合 | 重新设计统一数据模型 |
| 误解 OpenViking 的层次结构 | 使用虚拟 URI，预定义目录 |
| 语言不一致（TypeScript） | 统一使用 Go |
| 数据库不兼容 | 统一使用 PostgreSQL |
| 关联图谱需从头实现 | 明确设计双向图和加权算法 |

### 预期效果

- **召回率提升**：混合检索 + 关联图谱加权，预计提升 15-25%
- **洞见质量**：主动整合生成高层次知识
- **检索延迟**：< 500ms（含12阶段评分）
- **部署复杂度**：仅需 PostgreSQL + Go 二进制

### 风险和缓解

1. **LLM 成本**：结构化提取和整合都需要调用 LLM
   - 缓解：使用 gpt-4o-mini，控制 prompt 长度
2. **图遍历性能**：连接关系多时可能变慢
   - 缓解：限制遍历深度（2跳），使用 Redis 缓存
3. **整合质量**：LLM 可能生成低质量连接
   - 缓解：设置 strength 阈值，过滤弱连接

---

## 九、下一步行动

1. **确认技术栈**：PostgreSQL + pgvector 是否满足需求？
2. **开始阶段1**：搭建项目骨架，实现基础存储
3. **设计实验**：准备测试数据集，验证关联图谱加权效果
4. **迭代优化**：根据实验结果调整评分权重

**预计总开发时间**：5-8周
**核心里程碑**：阶段4完成后，系统具备完整的"主动思考"能力
