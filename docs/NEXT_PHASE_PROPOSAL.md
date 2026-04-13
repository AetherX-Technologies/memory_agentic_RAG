# HybridMem-RAG 下一阶段增强方案

> 供专家审核
> 基于 lossless-claw-enhanced (LCM) 项目的未采纳技术
> 2026-04-13

---

## 一、当前状态

### 已完成（本轮 session）

| 能力 | 来源 | 验证 |
|------|------|------|
| CJK-aware token 预算 | LCM `estimate-tokens.ts` | 误差 5%（vs 旧 55%） |
| 预算感知上下文组装 | LCM `assembler.ts` | max_tokens 参数 + 自动分配 |
| 智能记忆联想（connections） | Google always-on-memory-agent | 8/8 自动关联（真实模型） |
| Recall 关联展开 | 原创 | 🔗 关联记忆独立区域 |
| 自动阈值校准 | 原创 | 23 对标定，按模型缓存 |
| Tags 完整持久化 | bug fix | 覆盖全部 store/update/export/import 路径 |
| SourceConv 过滤 | 原创 | 按对话 ID 过滤 + 展示 |

### 未触碰的痛点

1. **Consolidation 仍然是"盲"的** — 随机取 50 条，不按语义分组
2. **无长文本保护** — 超长记忆浪费存储和搜索性能
3. **LLM 调用无降级** — consolidation/冲突检测失败即终止

---

## 二、方案 A：DAG 层级 Consolidation

### 2.1 问题

当前 `consolidate.Consolidate()` 的行为：

```
ListUnconsolidated(50)  →  取最老的 50 条（不管是否相关）
        ↓
    全部格式化为文本
        ↓
    一次 LLM 调用
        ↓
    一个 consolidation 记录
```

**缺陷**：
- 50 条不相关记忆混在一起，LLM 很难发现有意义的模式
- 一次调用 token 量大（50 条 × 平均 30 字 = 1500 字 + prompt），成本高
- 输出质量低——强迫 LLM 在不相关事物间找关联

### 2.2 LCM 的做法

LCM 的 compaction 不是一次性处理所有内容，而是分层：

```
Level 0 (Leaf):      5-10 条相关消息 → 一个 leaf 摘要
Level 1 (Condensed): 多个 leaf 摘要 → 更高层级摘要
Level 2+:            递归压缩
```

每层只处理**同主题**的小组，信息密度逐层提升。

### 2.3 移植方案

**利用已有的 connections 图做智能分组**（不需要新的聚类算法）：

```
Step 1: 取种子记忆（最老的未聚合记忆）
Step 2: 通过 connections 扩展到相关记忆（BFS，max 10）
Step 3: 这一组送 LLM consolidation
Step 4: 标记为 consolidated，产生 depth-0 consolidation
Step 5: 重复 Step 1-4 直到没有未聚合记忆
Step 6: 当 depth-0 consolidation 数量 ≥ 5，condensation → depth-1
```

**改动范围**：
- `internal/consolidate/consolidate.go` — 新增 `LeafPass()` 和 `CondensationPass()`
- `internal/store/` — `consolidations` 表新增 `depth` 列
- `internal/consolidate/scheduler.go` — leaf 每 10 分钟，condensation 每小时

**前置条件**：connections 已就位 ✅

### 2.4 风险

| 风险 | 概率 | 缓解 |
|------|------|------|
| connections 不足导致分组太小 | 中 | 允许"孤立组"（无连接的记忆单独成组） |
| 多级 LLM 调用成本 | 低 | leaf 用 mini 模型，condensation 用强模型 |
| depth 表结构迁移 | 低 | ALTER TABLE ADD COLUMN，兼容旧数据 |

### 2.5 预期收益

- Consolidation 质量大幅提升（同主题小组 vs 随机大批）
- 渐进式知识压缩（depth 0 → 1 → 2，信息密度递增）
- 更好的洞察（"在过去 10 次对话中，你一直偏好..."）

### 2.6 工作量估算

核心改动 ~300 行 Go + schema 迁移 + 测试。

---

## 三、方案 B：长文本拦截与摘要引用

### 3.1 问题

当前系统允许任意长度的记忆文本存入。如果用户存入一段 5000 字的笔记：
- 向量化只取前 512 tokens（ONNX 限制），后面的内容不被搜索
- FTS5 索引全文，但 BM25 对长文档有 length normalization 惩罚
- 格式化输出时浪费 token 预算

### 3.2 LCM 的做法

`large-files.ts` 拦截 >25k tokens 的内容：
- 存储原始内容到独立表
- 替换为紧凑引用（`[File: xxx, 2500 tokens]`）
- 按需展开

### 3.3 移植方案

**在 `StoreWithDedup` 中加入长度检查**：

```
if estimateTokens(content) > MaxMemoryTokens (默认 500):
    1. 生成摘要（LLM 或截断前 200 tokens）
    2. 存储摘要为 Memory.Text
    3. 原始全文存入 Memory.Metadata（JSON）
    4. Memory.Abstract = 摘要
    5. 向量化使用摘要（保证 512 token 内）
```

**不需要新表**——利用现有的 `abstract` 和 `metadata` 字段。

### 3.4 风险

| 风险 | 概率 | 缓解 |
|------|------|------|
| 摘要丢失关键信息 | 中 | 保留原文在 metadata，recall 可选展开 |
| LLM 摘要成本 | 低 | 只对 >500 tokens 触发 |

### 3.5 工作量估算

~100 行改动（dedup.go + service.go）。

---

## 四、方案 C：LLM 调用三级降级

### 4.1 问题

当前 LLM 调用失败（超时、API 错误）直接返回 error：
- `consolidate.Consolidate()` → 整个 consolidation 失败
- `dedup.detectConflict()` → 跳过冲突检测，可能存入矛盾记忆

### 4.2 LCM 的做法

三级降级保证进度：
1. **Normal**: temperature=0.2，完整 prompt
2. **Aggressive**: temperature=0.1，更紧凑的 prompt，更低的 token 上限
3. **Fallback**: 确定性截断（前 512 tokens + marker），不调 LLM

### 4.3 移植方案

```go
func (c *Consolidator) consolidateWithEscalation(ctx context.Context, text string) (*Result, error) {
    // Level 1: Normal
    result, err := c.callLLM(ctx, normalPrompt, text, 1024, 0.2)
    if err == nil { return result, nil }

    // Level 2: Aggressive (shorter prompt, lower tokens)
    result, err = c.callLLM(ctx, aggressivePrompt, text, 512, 0.1)
    if err == nil { return result, nil }

    // Level 3: Deterministic fallback (no LLM)
    return &Result{
        Summary: text[:min(len(text), 500)] + "...",
        Insight: "[auto-truncated: LLM unavailable]",
    }, nil
}
```

### 4.4 风险

极低——降级只在 LLM 失败时触发，正常路径不变。

### 4.5 工作量估算

~50 行改动（consolidate.go）。

---

## 五、方案对比

| 维度 | A: DAG Consolidation | B: 长文本拦截 | C: LLM 降级 |
|------|---------------------|-------------|------------|
| **用户价值** | 高（洞察质量提升） | 中（防止性能退化） | 低（仅在 LLM 故障时） |
| **技术风险** | 中（schema 迁移） | 低 | 极低 |
| **工作量** | ~300 行 | ~100 行 | ~50 行 |
| **前置依赖** | connections ✅ | 无 | 无 |
| **可独立交付** | 是 | 是 | 是 |
| **推荐顺序** | 第 1 | 第 2 | 第 3 |

---

## 六、不推荐做的

以下是从 LCM 分析中明确排除的方案及原因：

| 方案 | 排除原因 |
|------|---------|
| 主动记忆浮现（方案 8） | 每个 turn 注入记忆绕过 ShouldRetrieve 门控，会注入无关上下文，浪费 token 且降低 LLM 性能 |
| 统一 DAG 大合并（方案 13） | 多月重写，风险/回报比灾难级。两个系统松耦合是特性不是缺陷 |
| 自我改进反馈闭环（方案 12） | 测量噪声当信号反馈，会导致提取质量回归 |
| 记忆感知摘要（方案 6/9） | 循环依赖，解决的问题（事实重复出现两次）对 LLM 几乎无害 |

---

## 七、附录：已有基础设施

方案 A/B/C 可以复用的现有组件：

```
connections 图         → A 的智能分组
tokutil.EstimateTokens → B 的长度检测
store.AddConnection    → A 的 consolidation 连接
llmutil.CallLLM        → C 的多级调用
dedup.StoreWithDedup   → B 的拦截点
consolidate.Scheduler  → A 的分层调度
```

---

**请审核并反馈：优先级排序是否合理？是否有遗漏的风险？**
