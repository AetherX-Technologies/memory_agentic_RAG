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

1. **Consolidation 仍然是"盲"的** — 取最新 50 条（按时间倒序），不按语义分组
2. **无长文本保护** — 超长记忆浪费存储和搜索性能
3. **Consolidation LLM 无降级** — consolidation 失败即终止（冲突检测已有 fallback）
4. **所有 LLM 任务都走主模型** — 摘要/提取这种轻量任务也用贵的模型，成本浪费

---

## 二、方案 A：DAG 层级 Consolidation

### 2.1 问题

当前 `consolidate.Consolidate()` 的行为：

```
ListUnconsolidated(50)  →  取最新的 50 条（按时间倒序，不按语义分组）
        ↓
    全部格式化为文本
        ↓
    一次 LLM 调用
        ↓
    一个 consolidation 记录
```

**缺陷**：
- 最多 50 条不相关记忆混在一起，LLM 很难发现有意义的模式
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

### 2.4 建议分阶段实施

**Phase 1（Leaf-only 语义分组）**：
- 仅改 `Consolidate()` 的分组策略：用 connections 图 BFS 取 5-10 条相关记忆
- 不加 depth 字段，不做 condensation
- ~150 行改动

**Phase 2（Multi-depth condensation）**：
- **根本问题**：`consolidations` 目前不是 `Memory` 节点，recursive condensation 需要把 consolidation 作为可再处理的节点，这是**数据模型重构**，不是加个 depth 列那么简单
- 两种路径选择：
  - 路径 A：把 consolidation 合并到 `memories` 表（用 `node_type="consolidation"` + `depth` 字段）— 改动大但概念统一
  - 路径 B：保持独立表但新增 `parent_consolidation_id` + `depth` + 专用 condensation API — 改动小但逻辑分叉
- 需要 "list leaf consolidations eligible for condensation" 查询
- 需要 `ListConsolidations` 的 recall 展示区分 depth
- ~600 行改动（含 schema 迁移、新 API、测试），**不是原估的 300 行**

### 2.5 风险

| 风险 | 概率 | 缓解 |
|------|------|------|
| connections 图稀疏（dedup 默认最多 3 条/次，JSON 非关系表） | 中 | 允许"孤立组"；Phase 1 先验证效果 |
| `ListUnconsolidated()` 不返回 connections 字段 | 高 | Phase 1 需额外 `Get()` 调用或新增 store 查询 |
| `consolidated` 是一次性 flag — 后加的 connections 无法让已聚合记忆重新分组 | 中 | 可考虑"周期性重置"或 "connection 变更触发 re-consolidation" |
| consolidation 不是 Memory 节点，无法被再处理 | 高（Phase 2） | 需要新 API 或改数据模型 |
| recall 混合展示不同 depth 的 insight | 低 | 按 depth 排序展示 |

### 2.6 预期收益

- Phase 1：consolidation 质量大幅提升（同主题小组 vs 混杂大批）
- Phase 2：渐进式知识压缩（depth 0 → 1 → 2，信息密度递增）

### 2.7 工作量估算

- Phase 1：~150 行 Go + 测试
- Phase 2：~600 行额外 + schema 重构 + API 扩展 + 测试（需先决策路径 A/B）

---

## 三、方案 B：长文本拦截与摘要引用

### 3.1 问题

当前系统允许任意长度的记忆文本存入。如果用户存入一段 5000 字的笔记：
- 向量化只取前 512 tokens（本地 ONNX 限制），后面的内容不被向量搜索覆盖
- 格式化输出时浪费大量 token 预算（一条长记忆可能占满整个 max_tokens）
- 嵌入质量下降（长文本的 embedding 信号被稀释）

**方案 B 解决的是嵌入精度和展示效率**，不是 FTS 索引或存储体积（全文保留用于关键词搜索）。

### 3.2 LCM 的做法

`large-files.ts` 拦截 >25k tokens 的内容：
- 存储原始内容到独立表
- 替换为紧凑引用（`[File: xxx, 2500 tokens]`）
- 按需展开

### 3.3 移植方案

**保留 Text 不变，利用 Abstract 字段做展示和嵌入**（Codex 审查建议）：

```
if estimateTokens(content) > MaxMemoryTokens (默认 500):
    1. 生成摘要（LLM 或截断前 200 tokens）
    2. Memory.Text = 原始全文（不变，FTS 仍索引全文）
    3. Memory.Abstract = 生成的摘要
    4. 向量化使用 Abstract（保证 512 token 内）
    5. formatMemoryLine 优先展示 Abstract（如有）
```

**不需要新表**——`abstract` 字段已存在于 OpenViking 层次结构中（用于 L0 摘要），此方案新增 AI 记忆系统对它的使用。

**重要副作用**：一旦 embedding 从 Text 切换为 Abstract，下游行为全部改变：
- Dedup 阈值校准数据失效（需重新校准，因为 "Abstract vs Abstract" 的相似度分布和 "Text vs Text" 不同）
- 冲突检测的 cosine 值偏移，conflict band 可能需要调整
- Connection 建立基于 Abstract 的相似度而非原文
- **建议**：B 方案上线后触发一次自动重校准

### 3.6 摘要模型分离（配置需求）

**用户需求**：摘要生成这种轻量任务不应占用主 LLM（gpt-5.4 等），应该能指定便宜的小模型（gpt-4o-mini 等）。

**方案**：`config.local.yaml` 的 `llm` 下新增可选 `summary` 子配置：

```yaml
llm:
  provider: "openai"
  api_key: "clp_..."           # 主模型 key
  model: "gpt-5.4"             # 主模型：consolidation、冲突检测、深度推理
  endpoint: "https://api-vip.codex-for.me/v1/chat/completions"

  summary:                      # 可选：摘要/结构化提取等轻量任务
    api_key: ""                # 空 = 继承主 key
    model: "gpt-4o-mini"
    endpoint: ""               # 空 = 继承主 endpoint
```

**使用策略**：

| 任务 | 用哪个模型 | 理由 |
|------|-----------|------|
| 方案 B: 长文本摘要生成 | summary（或 fallback 主） | 结构化任务，小模型足够 |
| 方案 A-Phase2: condensation 摘要 | summary（或 fallback 主） | 同上 |
| 当前 consolidation | 主 LLM | 需要跨记忆推理发现模式 |
| 当前冲突检测 | 主 LLM | 需要语义理解（可选改为 summary） |
| 未来 episode 提取 | summary | 简单分类任务 |

**实现要点**：
- `consolidate.Config` 新增 `SummaryLLMKey/Model/Endpoint` 字段（optional）
- `llmutil` 新增 `CallSummaryLLM()`——自动 fallback 到主配置
- bootstrap 从 `cfg.LLM.Summary` 读取
- **向后兼容**：如果 `summary` 未配置，所有任务仍用主 LLM

**工作量**：~50 行配置 + ~30 行 llmutil 路由 = 约 80 行基础设施（可独立于 B/A 先落地）

**需要改动的路径**：
- `dedup.StoreWithDedup()` — 主路径，embedding 使用 Abstract
- `dedup.insertMemory()` — 设置 Abstract 字段
- `extractor.ExtractedMemory` — 新增 Abstract 字段
- `Service.Store()` 直接 insert fallback
- `Service.Update()` 内容变更路径
- `Service.autoStore()` 自动捕获路径
- `Service.Import()` 批量恢复
- `memservice/format.go` — `formatMemoryLine()` 优先用 Abstract 展示
- `memservice/service.go` — Recall 的关联记忆渲染路径（`cm.Text` → `cm.Abstract`）
- `mcp/format.go` — MCP 独立的格式化路径（同样需要优先用 Abstract）

### 3.4 风险

| 风险 | 概率 | 缓解 |
|------|------|------|
| 摘要丢失关键信息 | 中 | Text 全文保留，recall 可展开 |
| LLM 摘要成本 | 低 | 只对 >500 tokens 触发 |
| 远程 API embedder 无 512 限制 | 低 | 仍受益于更短的展示文本 |
| Legacy REST 路径（POST/PUT /api/memories）绕过拦截 | 低 | legacy 路径是向后兼容，主流程走 MCP/Tool API |

### 3.5 工作量估算

~200 行改动（dedup.go + extractor.go + service.go + format.go，需覆盖全部 store 路径）。

---

## 四、方案 C：LLM 调用三级降级

### 4.1 问题

当前 LLM 调用失败时行为不一致：
- `consolidate.Consolidate()` → 整个 consolidation 失败，返回 error
- `dedup.detectConflict()` → 已有降级（LLM 失败时返回 false，不阻塞存储）

**真正的问题是 consolidation 没有降级**——LLM 不可用时聚合完全停止。

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

| 维度 | D: 摘要模型分离 | B: 长文本保护 | A-Phase1: Leaf 分组 | A-Phase2: Multi-depth | C: Consolidation 降级 |
|------|-------------|-------------|--------------------|-----------------------|---------------------|
| **用户价值** | 中（降成本） | 中（防止性能退化） | 高（洞察质量提升） | 高（渐进压缩） | 低（LLM 故障时） |
| **技术风险** | 极低 | 低 | 低 | 中（schema 迁移） | 极低 |
| **工作量** | ~80 行 | ~200 行 | ~150 行 | ~600 行额外 | ~50 行 |
| **前置依赖** | 无 | D（推荐）| connections ✅ | A-Phase1 | 无 |
| **可独立交付** | 是 | 是 | 是 | 是 | 是 |
| **推荐顺序** | **第 1** | **第 2** | **第 3** | 第 4 | 第 5 |

> 优先级说明：
> - **D（摘要模型分离）** 作为基础设施先行，让 B/A-Phase2 可以直接用小模型，不用再改一遍
> - **B** 在 D 之后做，生成 Abstract 时直接用小模型
> - **A** 分两期，Phase 1 验证效果后再决定是否做 Phase 2
> - **C** 只在 consolidation 可靠性成为运维痛点时再做

---

## 六、不推荐做的

以下是从 LCM 分析中明确排除的方案及原因：

| 方案 | 排除原因 |
|------|---------|
| 主动记忆浮现（方案 8） | 每个 turn 注入记忆绕过 ShouldRetrieve 门控，会注入无关上下文，浪费 token 且降低 LLM 性能 |
| 统一 DAG 大合并（方案 13） | 多月重写，风险/回报比灾难级。两个系统松耦合是特性不是缺陷 |
| 自我改进反馈闭环（方案 12） | 测量噪声当信号反馈，会导致提取质量回归 |
| 记忆感知摘要 — 完整版（方案 6/9） | 完整版需循环依赖，解决的问题对 LLM 几乎无害。但**窄版本**（用 Abstract/Overview 字段做长记忆的展示/嵌入）已纳入方案 B |

---

## 七、附录：已有基础设施

方案 A/B/C/D 可以复用的现有组件：

```
connections 图         → A 的智能分组
tokutil.EstimateTokens → B 的长度检测
store.AddConnection    → A 的 consolidation 连接
llmutil.CallLLM        → C 的多级调用 & D 的路由包装
dedup.StoreWithDedup   → B 的拦截点
consolidate.Scheduler  → A 的分层调度
config.LLMConfig       → D 的 summary 子配置扩展点
```

---

**请审核并反馈：优先级排序是否合理？是否有遗漏的风险？**
