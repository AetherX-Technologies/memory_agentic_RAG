# 数据模型设计

> 参考：`../memory-lancedb-pro-main/src/store.ts`
> 更新：2024-03-13

---

## 一、核心数据结构

### 1.1 Memory（记忆）

**参考**：TypeScript `MemoryEntry`

```go
// internal/store/types.go
package store

type Memory struct {
    ID         string    `json:"id"`
    Text       string    `json:"text"`
    Vector     []float32 `json:"-"` // 不序列化到 JSON
    Category   string    `json:"category"` // preference, fact, decision, entity, other
    Scope      string    `json:"scope"`
    Importance float64   `json:"importance"` // 0.0 - 1.0
    Timestamp  int64     `json:"timestamp"`  // Unix timestamp
    Metadata   string    `json:"metadata,omitempty"` // JSON string
}

type SearchResult struct {
    Entry Memory  `json:"entry"`
    Score float64 `json:"score"`
}
```

---

## 二、SQLite 表结构

### 2.1 memories 表（主表）

```sql
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    text TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'other',
    scope TEXT NOT NULL DEFAULT 'global',
    importance REAL NOT NULL DEFAULT 0.7,
    timestamp INTEGER NOT NULL,
    metadata TEXT DEFAULT '{}'
);

CREATE INDEX idx_memories_scope ON memories(scope);
CREATE INDEX idx_memories_timestamp ON memories(timestamp DESC);
CREATE INDEX idx_memories_category ON memories(category);
```

### 2.2 vectors 表（向量存储）

```sql
CREATE TABLE IF NOT EXISTS vectors (
    memory_id TEXT PRIMARY KEY,
    vector BLOB NOT NULL,
    dimension INTEGER NOT NULL,
    FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
);
```

**向量序列化**：
- Go `[]float32` → SQLite `BLOB`
- 使用 `encoding/binary` 序列化

### 2.3 fts_memories 表（全文检索）

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS fts_memories USING fts5(
    memory_id UNINDEXED,
    content,
    tokenize='unicode61'
);
```

**触发器**（自动同步）：
```sql
CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO fts_memories(memory_id, content) VALUES (new.id, new.text);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    DELETE FROM fts_memories WHERE memory_id = old.id;
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    UPDATE fts_memories SET content = new.text WHERE memory_id = new.id;
END;
```

---

## 三、对比原项目

| 维度 | LanceDB (TypeScript) | SQLite (Go) |
|------|---------------------|-------------|
| 主存储 | LanceDB 单表 | memories + vectors 分离 |
| 向量存储 | 内置 vector 列 | BLOB 字段 |
| 全文检索 | LanceDB FTS | SQLite FTS5 |
| 索引 | 自动 | 手动创建 |
| 外键 | 无 | CASCADE 删除 |

---

## 四、实现文件

```
internal/store/
├── types.go       # 数据结构定义
├── store.go       # Store 接口和实现
├── schema.go      # 数据库初始化
└── vector.go      # 向量序列化工具
```

---

**下一步**：实现 Store 接口
