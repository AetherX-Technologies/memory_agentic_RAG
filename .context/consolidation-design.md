# 记忆合并与知识发现设计文档（Phase G）

> 创建时间：2026-03-22
> 参考：memory-agent (`/Volumes/SN770Coder/code/memory-agent`)
> 状态：待审查
> 目标：让记忆系统从"被动存取"升级为"主动知识合成"

---

## 一、当前状态 vs 目标

### 1.1 当前（Phase A→F）

```
用户对话 → 提取记忆 → 去重/冲突 → 存储 → 被动召回
```

记忆之间是**孤立的**，没有跨记忆的关联发现。

### 1.2 目标（Phase G）

```
用户对话 → 提取记忆 → 去重/冲突 → 存储 → 被动召回
                                        ↓
                              定时合并（每30分钟）
                                        ↓
                         LLM 分析 → 发现模式/关联/洞察
                                        ↓
                    存储 Consolidation + 更新记忆间连接
                                        ↓
                         召回时同时返回 consolidation insights
```

---

## 二、参考实现分析（memory-agent）

### 2.1 核心流程

memory-agent 的合并流程（`agent_service.go:114-185`）：

1. `ListUnconsolidated()` → 找未参与合并的记忆（最多100条）
2. 格式化为文本 → 传给 LLM
3. LLM 返回 `{summary, insight, patterns[], connections[]}`
4. 存储 `Consolidation` 记录
5. 遍历 `connections`，更新记忆的双向链接
6. 标记源记忆 `consolidated=true`

### 2.2 LLM 合并 Prompt

```
You are a Memory Consolidation Agent. Analyze the memories below and respond with ONLY a JSON object:
{"source_ids": [1, 2, 3], "summary": "synthesized summary", "insight": "key pattern or insight",
 "connections": [{"from_id": 1, "to_id": 2, "relationship": "how they relate"}]}

Find connections and patterns across the memories.
```

### 2.3 数据模型

**Consolidation 结构**：
```go
type Consolidation struct {
    ID          int64
    SourceIDs   []int64                    // 参与合并的记忆 ID
    Summary     string                     // 跨记忆综合摘要
    Insight     string                     // 关键洞察（最有价值的一句话）
    Patterns    []string                   // 发现的模式列表
    Connections []map[string]interface{}    // 记忆间关系 {from_id, to_id, relationship}
    CreatedAt   time.Time
}
```

**Memory 扩展**：
- `Connections []map[string]interface{}` — 双向链接列表
- `Consolidated bool` — 是否已参与合并

### 2.4 调度器

- 默认间隔：30 分钟
- 最少记忆数：2 条（未合并的）
- 后台 goroutine + ticker + stop channel

---

## 三、HybridMem-RAG 集成方案

### 3.1 数据模型变更

#### 3.1.1 新增 consolidations 表

```sql
CREATE TABLE IF NOT EXISTS consolidations (
    id TEXT PRIMARY KEY,
    source_ids TEXT NOT NULL,       -- JSON 数组 ["id1", "id2", ...]
    summary TEXT NOT NULL,          -- 跨记忆综合摘要
    insight TEXT NOT NULL,          -- 关键洞察
    patterns TEXT DEFAULT '[]',     -- JSON 数组 ["pattern1", ...]
    connections TEXT DEFAULT '[]',  -- JSON 数组 [{from_id, to_id, relationship}, ...]
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_consolidation_created ON consolidations(created_at DESC);
```

#### 3.1.2 memories 表扩展

```sql
ALTER TABLE memories ADD COLUMN consolidated INTEGER DEFAULT 0;
ALTER TABLE memories ADD COLUMN connections TEXT DEFAULT '[]';
-- connections 格式：[{"linked_to": "other_id", "relationship": "description"}, ...]

CREATE INDEX IF NOT EXISTS idx_consolidated ON memories(consolidated);
```

#### 3.1.3 Memory 结构扩展

```go
type Memory struct {
    // ... 现有字段 ...
    Consolidated bool                      `json:"consolidated,omitempty"`
    Connections  []map[string]interface{}   `json:"connections,omitempty"`
}
```

### 3.2 新增包：`internal/consolidate/`

#### 3.2.1 文件结构

```
internal/consolidate/
├── consolidate.go       # 核心合并逻辑
├── scheduler.go         # 定时调度器
├── llm.go              # LLM 合并接口（复用 generator 的 LLM 调用能力）
└── consolidate_test.go  # 测试
```

#### 3.2.2 核心接口

```go
// ConsolidationResult 是 LLM 返回的合并结果
type ConsolidationResult struct {
    SourceIDs   []string                   `json:"source_ids"`
    Summary     string                     `json:"summary"`
    Insight     string                     `json:"insight"`
    Patterns    []string                   `json:"patterns"`
    Connections []map[string]interface{}    `json:"connections"`
}

// Consolidator 执行记忆合并
type Consolidator struct {
    store    store.Store
    embedder store.Embedder  // 用于合并洞察的向量化
    config   Config
    client   *http.Client
}

// Config 合并配置
type Config struct {
    LLMAPIKey  string
    LLMModel   string
    LLMEndpoint string
    LLMTimeout  int
    MaxMemories int  // 每次合并最多处理的记忆数（默认 50）
}
```

#### 3.2.3 合并流程

```go
func (c *Consolidator) Consolidate(ctx context.Context) (*Consolidation, error) {
    // 1. 查询未合并的记忆（consolidated=0, deleted_at=0, category="memory"）
    memories := c.store.ListUnconsolidated(limit)

    // 2. 至少 2 条才合并
    if len(memories) < 2 { return nil, nil }

    // 3. 格式化记忆文本
    text := formatMemoriesForLLM(memories)

    // 4. 调用 LLM 合并
    result := c.callConsolidateLLM(ctx, text)

    // 5. 存储 consolidation 记录
    c.store.CreateConsolidation(result)

    // 6. 更新记忆间双向连接
    for _, conn := range result.Connections {
        c.store.AddConnection(fromID, toID, relationship)
    }

    // 7. 标记源记忆为已合并
    c.store.MarkConsolidated(result.SourceIDs)

    return consolidation, nil
}
```

### 3.3 Store 接口扩展

```go
type Store interface {
    // ... 现有方法 ...

    // Phase G: Consolidation
    ListUnconsolidated(limit int) ([]*Memory, error)
    CountUnconsolidated() (int64, error)
    MarkConsolidated(ids []string) error
    CreateConsolidation(c *Consolidation) (string, error)
    ListConsolidations(limit int) ([]*Consolidation, error)
    AddConnection(memoryID string, linkedTo string, relationship string) error
}
```

### 3.4 调度器

```go
type Scheduler struct {
    consolidator *Consolidator
    interval     time.Duration  // 默认 30 分钟
    minMemories  int            // 默认 2
    stopCh       chan struct{}
}

func (s *Scheduler) Start() {
    ticker := time.NewTicker(s.interval)
    go func() {
        for {
            select {
            case <-ticker.C:
                s.run()
            case <-s.stopCh:
                ticker.Stop()
                return
            }
        }
    }()
}
```

### 3.5 LLM Prompt 设计

```
你是一个记忆合并分析器。分析以下记忆，发现它们之间的关联、模式和洞察。

要求：
1. 找出记忆之间的关联关系（因果、补充、对比、时序等）
2. 提炼跨记忆的模式和规律
3. 生成一句话核心洞察（insight）
4. 只返回 JSON，不要其他文字

输出格式：
{
  "source_ids": ["id1", "id2"],
  "summary": "跨记忆综合摘要",
  "insight": "最有价值的一句话洞察",
  "patterns": ["发现的模式1", "模式2"],
  "connections": [
    {"from_id": "id1", "to_id": "id2", "relationship": "关系描述"}
  ]
}

记忆列表：
{memories_text}
```

### 3.6 MCP 工具扩展

新增 `memory_consolidate` 工具（手动触发合并）：

```json
{
  "name": "memory_consolidate",
  "description": "手动触发记忆合并，发现记忆间的关联和模式。",
  "inputSchema": {
    "type": "object",
    "properties": {}
  }
}
```

`memory_recall` 扩展：在返回结果中包含相关的 consolidation insights。

### 3.7 Recall 集成

当 `memory_recall` 执行检索时：
1. 正常检索记忆
2. 额外查询最近 10 条 consolidation insights
3. 在格式化上下文中添加 "🧠 洞察" 分组

```
[记忆系统召回 5 条]

📌 指令
- 回复用中文

👤 用户信息
- 西北院水利工程师
- Go 高级开发者

🧠 洞察
- 用户是西北院的 Go 开发者，主要用 Go 做水利相关的技术项目
- 用户偏好简洁风格，与其工程师背景一致
```

---

## 四、成本控制

| 操作 | LLM 调用 | 频率 |
|------|---------|------|
| 合并 | 1 次/批 | 每 30 分钟（有新记忆时） |
| 无新记忆 | 0 次 | — |
| **日均** | **~48 次**（最多） | 假设每 30 分钟都有新记忆 |

实际成本很低：每次合并处理 ~50 条记忆摘要，输入约 2000 tokens，输出约 500 tokens。

---

## 五、与现有系统的兼容性

| 现有模块 | 改动 | 说明 |
|---------|------|------|
| store | 新增 6 个方法 + 2 列 + 1 表 | 幂等迁移 |
| mcp | 新增 1 个工具 + recall 扩展 | 向后兼容 |
| dedup | 无改动 | — |
| extractor | 无改动 | — |
| scoring | 无改动 | 合并洞察不参与评分 |
| ratelimit | 合并调用计入限流 | — |

---

## 六、降级策略

- **LLM 不可用**：跳过本次合并，下次重试
- **记忆太少**（< 2 条未合并）：跳过
- **LLM 返回解析失败**：记录日志，不影响现有功能
- **合并服务崩溃**：不影响记忆的正常存取（合并是增强功能，非核心路径）

---

## 七、实现计划

| 步骤 | 内容 | 工作量 |
|------|------|--------|
| 1 | 数据模型扩展（consolidations 表 + 2 列） | 小 |
| 2 | Store 方法实现（6 个新方法） | 中 |
| 3 | LLM 合并接口 + Prompt | 小 |
| 4 | Consolidator 核心逻辑 | 中 |
| 5 | Scheduler 定时调度 | 小 |
| 6 | MCP 工具 + Recall 集成 | 小 |
| 7 | 测试 | 中 |
| **总计** | | **~2 天** |
