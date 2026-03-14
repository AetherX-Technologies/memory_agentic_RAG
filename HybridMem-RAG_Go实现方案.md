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

