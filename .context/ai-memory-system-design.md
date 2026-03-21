# AI Agent 记忆系统设计文档（v2.0）

> 创建时间：2026-03-20
> 最后更新：2026-03-20（根据 Codex 第 3 轮审查修正）
> 状态：审查通过，可实施
> 目标：将 HybridMem-RAG 从"文档检索系统"升级为"AI Agent 记忆后端"

---

## 一、当前状态与目标

### 1.1 已有能力

| 能力 | 状态 |
|------|------|
| 向量存储 + 检索（Qwen3 本地 ONNX, 80.8% Recall@5） | ✅ |
| BM25 全文检索 + 混合 RRF 融合 | ✅ |
| OpenViking 分层检索（优先队列 + 递归 + 分数传播） | ✅ |
| L0/L1/L2 三层表示 + HTTP API v1/v2 | ✅ |
| 评分管道（`scoring.go`: 新近度衰减 + 重要性加权 + 长度归一化） | ✅ |

### 1.2 目标

```
对话/文档 → 记忆提取（LLM）→ 去重 + 冲突 → 存储
                                            ↓
AI Agent ← 格式化上下文 ← 检索 + 统一评分 ← MCP/API
```

---

## 二、记忆类型系统

### 2.1 六种记忆类型

| 类型 | 代码 | 用途 | 示例 | 默认重要性 |
|------|------|------|------|-----------|
| **事实** | `fact` | 关于用户/世界的客观信息 | "用户在西北院工作" | 0.7 |
| **偏好** | `preference` | 用户的喜好、习惯、风格 | "喜欢简洁回复" | 0.8 |
| **技能** | `skill` | 用户的能力和知识领域 | "Go 高级，Python 中级" | 0.6 |
| **事件** | `episode` | 发生过的具体事情 | "2026-03-15 讨论了 OpenViking" | 0.5 |
| **指令** | `instruction` | 用户给 AI 的持久指令 | "代码注释用英文" | 0.9 |
| **关系** | `relationship` | 人/事物之间的关联 | "Alice 是 Bob 的直属上级" | 0.7 |

> **关系类型说明**：关系记忆存储实体间的关联，content 格式为 "实体A [关系] 实体B"，tags 中包含两个实体名。
> 虽然可以用 `fact` 表达关系，但独立类型便于图谱查询和实体关联推理。

### 2.2 数据模型变更

在现有 Memory 结构上扩展：

```go
type Memory struct {
    // ... 现有 22 个字段保持不变 ...

    // 新增：记忆系统字段
    MemoryType   string  `json:"memory_type"`   // fact | preference | skill | episode | instruction | relationship
    Confidence   float64 `json:"confidence"`     // 置信度 [0.05, 1.0]（下限 0.05 防止零分）
    AccessCount  int     `json:"access_count"`   // 被召回次数
    LastAccessed int64   `json:"last_accessed"`  // 最后被召回的 Unix 时间戳
    ExpiresAt    int64   `json:"expires_at"`     // 过期 Unix 时间戳（0=永不过期）
    SourceConv   string  `json:"source_conv"`    // 来源对话 ID
    ContentHash  string  `json:"content_hash"`   // 内容 SHA256 前 16 位（数据库级去重）
    Expired      bool    `json:"expired"`        // 是否已过期（不可变 importance）
}
```

> **移除 `SupersededBy`**：改用独立的关系表（见 2.3），避免单 ID 限制和链断裂问题。

### 2.3 关联表

```sql
-- 记忆覆盖关系（多对多，支持链式覆盖）
CREATE TABLE IF NOT EXISTS memory_supersessions (
    old_id TEXT NOT NULL,
    new_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (old_id, new_id),
    FOREIGN KEY (old_id) REFERENCES memories(id) ON DELETE CASCADE,
    FOREIGN KEY (new_id) REFERENCES memories(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_supersession_old ON memory_supersessions(old_id);

-- 标签表（归一化，支持索引查询）
CREATE TABLE IF NOT EXISTS memory_tags (
    memory_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    PRIMARY KEY (memory_id, tag),
    FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_tag ON memory_tags(tag);
```

### 2.4 迁移 SQL

```sql
ALTER TABLE memories ADD COLUMN memory_type TEXT DEFAULT 'fact';
ALTER TABLE memories ADD COLUMN confidence REAL DEFAULT 0.5;
ALTER TABLE memories ADD COLUMN access_count INTEGER DEFAULT 0;
ALTER TABLE memories ADD COLUMN last_accessed INTEGER DEFAULT 0;
ALTER TABLE memories ADD COLUMN expires_at INTEGER DEFAULT 0;
ALTER TABLE memories ADD COLUMN source_conv TEXT DEFAULT NULL;
ALTER TABLE memories ADD COLUMN content_hash TEXT DEFAULT NULL;
ALTER TABLE memories ADD COLUMN expired INTEGER DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_memory_type ON memories(memory_type);
CREATE INDEX IF NOT EXISTS idx_expires_at ON memories(expires_at);
CREATE INDEX IF NOT EXISTS idx_source_conv ON memories(source_conv);
CREATE UNIQUE INDEX IF NOT EXISTS idx_content_hash_unique ON memories(content_hash)
    WHERE content_hash IS NOT NULL;
```

> **向后兼容**：所有新字段有默认值。现有数据 memory_type='fact', confidence=0.5。

---

## 三、记忆自动提取

### 3.1 提取流程

```
输入：对话（用户 + AI 回复）
  ↓
LLM 提取 → [M1, M2, ..., Mn]
  ↓
批内去重（比较 M1-Mn 两两相似度）→ 去掉重复
  ↓
逐条入库：向量检索现有记忆 → 去重/冲突 → 存储
```

### 3.2 提取 Prompt（含 few-shot）

```
你是一个记忆提取器。从以下对话中提取值得长期记住的信息。

规则：
1. 只提取有长期价值的信息，忽略寒暄和临时内容
2. 每条记忆是独立的陈述句
3. 分类为：fact / preference / skill / episode / instruction / relationship
4. 评估 importance (0.0-1.0) 和 confidence (0.0-1.0)（系统会自动下限到 0.05）
5. 提取标签（辅助检索）
6. 没有值得记住的信息就返回 []

示例输入：
用户："我是西北院的水利工程师，Python 用了5年，以后回复请用中文"
AI："好的，我记住了。"

示例输出：
[
  {"content": "用户是西北院的水利工程师", "type": "fact", "importance": 0.8, "confidence": 0.9, "tags": ["西北院", "水利", "工程师"]},
  {"content": "用户有5年Python使用经验", "type": "skill", "importance": 0.7, "confidence": 0.9, "tags": ["Python", "编程"]},
  {"content": "回复请使用中文", "type": "instruction", "importance": 0.9, "confidence": 1.0, "tags": ["语言", "中文"]}
]

示例输入：
用户："今天天气不错"
AI："是啊，春天到了。"

示例输出：
[]

---
对话内容：
{conversation}
```

### 3.3 JSON 解析管道

LLM 返回的 JSON 可能不规范，解析流程：
1. 去除 markdown 代码块标记（` ```json ``` `）
2. 尝试 `json.Unmarshal`
3. 失败 → 用正则提取第一个 `[...]` 子串，重新解析
4. 再失败 → 记录原始输出到日志，回退到规则提取
5. 对每条记忆：验证 type 是否合法、importance/confidence 范围是否 [0,1]、强制 confidence >= 0.05

### 3.4 降级策略

LLM 不可用时，规则提取：
- 检测模式："我是/我在/我喜欢/我不喜欢/记住/以后/请/不要"
- 提取为 `fact` 或 `preference` 类型
- 置信度设为 0.3

### 3.5 成本控制

| 操作 | LLM 调用次数 | 优化 |
|------|-------------|------|
| 记忆提取 | 1 次/对话 | 一个 prompt 提取全部 |
| 冲突检测 | 0-1 次/批 | 批量检测（见第四节） |
| **总计** | **最多 2 次/对话** | — |

---

## 四、记忆去重与冲突解决

### 4.1 数据库级去重

```go
// 插入前计算 content_hash = SHA256(content)[:16]
// UNIQUE INDEX 保证相同内容不重复插入
hash := sha256.Sum256([]byte(content))
contentHash := hex.EncodeToString(hash[:])[:16]
```

完全相同的内容直接被数据库约束拦截，不需要向量计算。

### 4.2 语义去重

```
新记忆 M_new 要入库：
1. 向量检索全局 top-20，然后在结果中过滤同 memory_type（因为现有向量搜索按 scope 过滤，不按 type）
2. 判断：
   - 相似度 > 0.93 → 语义重复，丢弃 M_new，提升 M_old.confidence += 0.1（上限1.0）
   - 0.85 < 相似度 < 0.93 → 候选冲突，进入冲突检测
   - 相似度 < 0.85 → 不重复，正常存储
```

> **短文本自适应**：content < 20 tokens 时，阈值降低 0.05（短文本嵌入噪声更大）。

### 4.3 冲突解决

```
候选冲突对 (M_new, M_old)：
  ↓
批量 LLM 判断（一次调用处理最多 10 对）：
  Prompt: "以下记忆对是否矛盾？只回答 yes/no + 简短理由"
  ↓
矛盾 → 在 memory_supersessions 表记录 (M_old, M_new)
      → M_old.importance *= 0.3（降权，不删除）
      → M_new 正常存储
不矛盾 → M_new 正常存储（两条共存）
  ↓
降级（LLM 不可用）→ 关键词重叠 > 60% + 同类型 → 标记为候选冲突，等下次确认
```

### 4.4 并发安全

去重检查 → 插入 是 TOCTOU 竞态。解决：
```go
// 在 dedup-check-then-insert 序列上加应用级互斥锁
var memoryStoreMu sync.Mutex

func (s *MemoryStore) StoreWithDedup(mem *Memory) error {
    memoryStoreMu.Lock()
    defer memoryStoreMu.Unlock()
    // 1. content_hash 查重
    // 2. 向量语义查重
    // 3. 冲突检测
    // 4. 插入
}
```

---

## 五、评分与衰减（统一模型）

### 5.1 与现有 scoring.go 的整合

**不创建新的评分管道**。扩展现有 `ApplyScoring` 函数，增加 confidence 和 access 因子：

```go
// scoring.go 扩展（不是替换）
// 注意：RecencyHalfLifeDays 保持 int 类型与现有代码一致
type ScoringConfig struct {
    // 现有字段保持不变（类型不变）
    RecencyHalfLifeDays int     // 保持 int，不改为 float64
    RecencyWeight       float64
    LengthNormAnchor    int
    HardMinScore        float64

    // 新增：记忆系统因子
    ConfidenceWeight    float64 // 默认 1.0（直接乘以 confidence）
    AccessBoostCap      float64 // 默认 1.5（access_factor 上限）
}

// 统一公式（保持现有加法模型，不改为乘法）：
// step 1: score += recency_boost        // 现有：加法新近度提升
// step 2: score *= importance_weight     // 现有：重要性加权
// step 3: score *= length_norm           // 现有：长度归一化
// step 4: score *= confidence            // 新增：置信度
// step 5: score *= min(1 + 0.1*log2(1+access_count), AccessBoostCap)  // 新增：访问加成
//
// 注意：step 1 是加法（与现有 scoring.go 一致），其余是乘法。
```

### 5.2 按类型差异化半衰期

在检索时根据 `memory_type` 覆盖 `RecencyHalfLifeDays`：

| 类型 | 半衰期 | 理由 |
|------|--------|------|
| instruction | 365 天 | 指令几乎不应过期 |
| fact, preference, relationship | 90 天 | 用户信息较稳定 |
| skill | 180 天 | 技能变化缓慢 |
| episode | 30 天 | 事件时效性强 |

### 5.3 主动遗忘（定期清理）

每日运行或启动时执行（使用 Go 传入的 Unix 时间戳，避免 SQLite `CURRENT_TIMESTAMP` 字符串问题）：

```go
func (s *Store) RunCleanup(now int64) error {
    // 1. 标记过期（不修改 importance，用 expired 标志位）
    s.db.Exec(`UPDATE memories SET expired = 1
        WHERE expires_at > 0 AND expires_at < ? AND expired = 0`, now)

    // 2. 清理低价值记忆（重要性低 + 从未召回 + 超过180天 + 低置信度 + 不在覆盖链中）
    cutoff := now - 180*86400
    s.db.Exec(`DELETE FROM memories
        WHERE importance < 0.1 AND confidence < 0.1
          AND access_count = 0 AND timestamp < ?
          AND id NOT IN (SELECT new_id FROM memory_supersessions)
          AND id NOT IN (SELECT old_id FROM memory_supersessions)`, cutoff)

    // 3. 清理过期且超过 30 天的记忆
    expiredCutoff := now - 30*86400
    s.db.Exec(`DELETE FROM memories
        WHERE expired = 1 AND timestamp < ?`, expiredCutoff)

    return nil
}
```

> **不删除被覆盖记忆**：`memory_supersessions` 中的旧记忆不主动删除，只在搜索时排除。
> 检索 WHERE 条件：`WHERE expired = 0 AND id NOT IN (SELECT old_id FROM memory_supersessions)`

---

## 六、MCP Server 接口

### 6.1 工具定义（符合 MCP 2024-11-05 规范）

```json
{
  "jsonrpc": "2.0",
  "result": {
    "tools": [
      {
        "name": "memory_store",
        "description": "存储一条新的记忆。自动去重和冲突检测。",
        "inputSchema": {
          "type": "object",
          "properties": {
            "content": {"type": "string", "description": "记忆内容（一句话）"},
            "type": {"type": "string", "enum": ["fact","preference","skill","episode","instruction","relationship"]},
            "importance": {"type": "number", "minimum": 0, "maximum": 1},
            "tags": {"type": "array", "items": {"type": "string"}}
          },
          "required": ["content"]
        }
      },
      {
        "name": "memory_recall",
        "description": "语义检索相关记忆，返回格式化上下文。",
        "inputSchema": {
          "type": "object",
          "properties": {
            "query": {"type": "string"},
            "limit": {"type": "integer", "default": 10},
            "types": {"type": "array", "items": {"type": "string"}},
            "min_importance": {"type": "number", "default": 0.1},
            "max_tokens": {"type": "integer", "default": 1000},
            "debug": {"type": "boolean", "default": false}
          },
          "required": ["query"]
        }
      },
      {
        "name": "memory_forget",
        "description": "删除一条记忆。自动处理子记忆（重设 parent_id 为 NULL）。",
        "inputSchema": {
          "type": "object",
          "properties": {
            "id": {"type": "string"}
          },
          "required": ["id"]
        }
      },
      {
        "name": "memory_update",
        "description": "更新记忆内容或属性。content 变更会自动重新向量化。",
        "inputSchema": {
          "type": "object",
          "properties": {
            "id": {"type": "string"},
            "content": {"type": "string"},
            "importance": {"type": "number"},
            "tags": {"type": "array", "items": {"type": "string"}}
          },
          "required": ["id"]
        }
      },
      {
        "name": "memory_export",
        "description": "导出全部记忆为 JSON 数组（备份/迁移用）。",
        "inputSchema": {
          "type": "object",
          "properties": {
            "types": {"type": "array", "items": {"type": "string"}},
            "include_expired": {"type": "boolean", "default": false}
          }
        }
      },
      {
        "name": "memory_import",
        "description": "从 JSON 数组批量导入记忆（恢复备份用）。自动跳过 content_hash 冲突。",
        "inputSchema": {
          "type": "object",
          "properties": {
            "memories": {"type": "array", "description": "memory_export 输出的 JSON 数组"},
            "overwrite": {"type": "boolean", "default": false, "description": "是否覆盖已有同 hash 记忆"}
          },
          "required": ["memories"]
        }
      },
      {
        "name": "memory_forget_by_tag",
        "description": "按标签批量删除记忆（如删除所有 pii:employer 标签的记忆）。",
        "inputSchema": {
          "type": "object",
          "properties": {
            "tag": {"type": "string", "description": "要删除的标签"},
            "dry_run": {"type": "boolean", "default": true, "description": "预览模式，只返回匹配数量"}
          },
          "required": ["tag"]
        }
      }
    ]
  }
}
```

### 6.2 tools/call 响应示例

```json
{
  "jsonrpc": "2.0",
  "result": {
    "content": [
      {
        "type": "text",
        "text": "[记忆系统召回 3 条]\n\n📌 指令\n- 回复用中文\n\n👤 用户\n- 西北院水利工程师\n- Go 高级"
      }
    ]
  }
}
```

### 6.3 传输协议

| 传输方式 | 适用场景 |
|---------|---------|
| stdio (JSON-RPC) | Claude Code / 本地 AI Agent |
| HTTP+SSE | 远程 Agent / Web 应用 |

MCP Server 和 HTTP API 共享同一个 Store 实例。

### 6.4 memory_update 自动重新向量化

```go
func (s *MCPServer) HandleUpdate(id, newContent string) error {
    if newContent != "" {
        // 内容变更 → 重算嵌入
        vec, err := s.embedder.Embed(newContent)
        if err != nil { return err }
        // 更新 content + vector + content_hash
    }
}
```

---

## 七、上下文格式化

### 7.1 格式化规则

1. **按类型分组**：instruction → preference → fact → skill → relationship → episode
2. **同类型内按置信度 × 重要性排序**
3. **去冗余**：同类型同标签的重复记忆只保留最高分
4. **Token 预算**：默认 1000 tokens（可通过 `max_tokens` 参数调整），超出时按类型优先级截断（先砍 episode）
5. **时间标注**：episode 标注日期，其他类型不标注

### 7.2 debug 模式

当 `memory_recall` 的 `debug=true` 时，额外返回：
```json
{
  "debug_info": {
    "candidates": 42,
    "after_type_filter": 28,
    "after_importance_filter": 15,
    "after_dedup": 10,
    "returned": 5,
    "scores": [
      {"id": "xxx", "base": 0.85, "recency": 0.92, "confidence": 0.9, "access": 1.2, "final": 0.71}
    ]
  }
}
```

---

## 八、隐私与安全

### 8.1 PII 处理

- 提取阶段：LLM prompt 中不要求提取敏感信息（身份证号、银行卡等）
- 标签分类：提取时可标记 `pii:name`、`pii:employer` 等 PII 标签
- 批量删除：`memory_forget_by_tag` 支持按 tag 批量删除（如"删除所有 pii:employer 标签的记忆"）

### 8.2 数据安全

- 移动端（iOS/Android）：SQLite 数据库文件加密（使用 SQLCipher 或 OS 级文件保护）
- 导出文件：提醒用户导出包含个人信息
- 传输：MCP stdio 是本地通信，无网络风险；HTTP API 应限制 localhost 访问

### 8.3 速率限制

防止记忆膨胀：
- 默认限制：20 条/分钟，200 条/小时
- 超限返回 429 状态码
- 在 MCP/API 入口层实施

---

## 九、可观测性

- **记忆存储日志**：每条记忆的提取来源、去重决策（skip/merge/store）、冲突标记
- **检索日志**：候选数量、各阶段过滤比、最终返回数、评分明细
- **清理日志**：每次清理删除/过期的记忆数量
- 日志格式：结构化 JSON，使用 Go `log/slog`

---

## 十、实现计划

| Phase | 内容 | 天数 | 说明 |
|-------|------|------|------|
| A | 数据模型扩展（8 字段 + 2 关联表 + 迁移） | 2 | 包含所有 Scan/Insert 更新 |
| B | 记忆提取器（LLM + 解析管道 + 降级） | 3 | 含 prompt 调优 |
| C | 去重与冲突（hash + 语义 + LLM 冲突 + 并发锁） | 2 | |
| D | 评分扩展 + 衰减 + 清理 | 1 | 扩展现有 scoring.go |
| E | MCP Server（stdio + 7 个工具 + 格式化） | 3 | 含 Claude Code 对接测试 |
| F | 隐私/安全/限流/可观测 | 1 | |
| **合计** | | **12 天** | |

---

## 十一、验收标准

1. **提取准确率** > 80%（10 段对话，与人工标注对比）
2. **去重**：相同内容 → content_hash 拦截；语义重复（>0.93）→ 合并
3. **冲突**：矛盾信息 → supersessions 表记录 + 旧记忆降权
4. **MCP**：Claude Code 能通过 stdio 调用全部 7 个工具
5. **格式化**：按类型分组、按重要性排序、不超 Token 预算
6. **衰减**：180 天未召回 + 低重要性 + 低置信度 → 被清理
7. **并发**：同时写入不产生重复
8. **导出**：memory_export 输出完整 JSON，memory_import 可导入

---

**文档版本**：v2.0
**修订说明**：根据 Codex 第 1-3 轮审查修正 2 个 CRITICAL + 8 个 HIGH + 10 个 MEDIUM + 2 个 LOW 问题
