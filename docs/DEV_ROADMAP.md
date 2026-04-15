# HybridMem-RAG 开发规划

> 基于 [NEXT_PHASE_PROPOSAL.md](./NEXT_PHASE_PROPOSAL.md)（已通过 Codex 7 轮联合审查）
> 基于 [SMART_ASSOCIATION.md](./SMART_ASSOCIATION.md)（已交付的功能基线）
> 创建：2026-04-13

---

## 一、规划总览

### 1.1 当前基线（已交付）

| 模块 | 状态 | 验证 |
|------|------|------|
| CJK token 预算感知 | ✅ | 误差 5%（vs 旧 55%） |
| 智能记忆联想（connections + recall 展开） | ✅ | 真实模型 8/8 自动关联 |
| Tags 完整持久化 | ✅ | store/update/export/import 全覆盖 |
| SourceConv 过滤与展示 | ✅ | recall + format 接通 |
| Embedder-specific 阈值 profile | ✅ | qwen3-local + 自动校准 |
| 自动校准（首次新模型） | ✅ | 23 对标定，缓存到 ~/.hybridmem/ |
| 真实模型集成测试（API + ONNX） | ✅ | 24-29/29 通过 |

### 1.2 下阶段四个方案（按优先级）

| 顺序 | 代号 | 名称 | LOC | 价值 | 风险 |
|------|------|------|-----|------|------|
| 1 | **D** | 摘要模型分离（基础设施） | ~190 | 中（降成本） | 极低 |
| 2 | **B** | 长文本保护（Abstract 路径） | ~200 | 中（嵌入精度） | 低 |
| 3 | **A-Phase1** | Leaf 语义分组 Consolidation | ~150 | 高（洞察质量） | 低 |
| 4 | **A-Phase2** | Multi-depth Condensation | ~600 | 高（渐进压缩） | 中（schema 重构） |
| 5 | **C** | Consolidation LLM 三级降级 | ~50 | 低（仅故障时） | 极低 |

---

## 二、Sprint 1：方案 D — 摘要模型分离

**周期估算**：3 天（1 天编码 + 1 天测试 + 1 天联调）

### 2.1 目标

让用户能在 `config.local.yaml` 中指定一个独立的小模型（如 gpt-4o-mini）用于摘要、提取等轻量任务，主模型（gpt-5.4 等）只服务深度推理。

### 2.2 分阶段交付

#### D1：基础设施 + dedup 构造器重构（~130 LOC）

**任务清单**：

- [ ] **D1.1** `config.LLMConfig` 新增 `Summary *LLMSubConfig` 字段
  - 文件：`internal/config/config.go`
  - 子结构包含 api_key/model/endpoint/timeout
  - 改动：~15 行

- [ ] **D1.2** `llmutil.ResolveLLMConfig(cfg config.LLMConfig, tier string) ResolvedLLMConfig`
  - 文件：`internal/llmutil/resolve.go`（新建）
  - tier="main" 或 "summary"
  - 实现 fallback 链：env > yaml > 主配置
  - 改动：~30 行

- [ ] **D1.3** 新增 env vars 读取
  - `MEMORY_LLM_TIMEOUT`（新）
  - `MEMORY_LLM_SUMMARY_KEY/MODEL/ENDPOINT/TIMEOUT`（新）
  - 改动：~30 行

- [ ] **D1.4** `dedup.DefaultConfigFromLLM(llm ResolvedLLMConfig) Config`
  - 文件：`internal/dedup/dedup.go`
  - 替代当前 env-only 的 `DefaultConfig()`
  - 旧 API 保留 + deprecation 注释
  - 改动：~30 行

- [ ] **D1.5** 调用点迁移
  - `internal/api/handler.go:31` — `NewHandlerWithDeps` 接收 `*config.AppConfig`
  - `internal/mcp/server.go:54` — `mcp.New` 接收 `*config.AppConfig`
  - `internal/bootstrap/app.go:146` — `consolidate.New` 用 `ResolveLLMConfig("main").Timeout` 取代硬编码 120s
  - 改动：~25 行

**验收标准**：
- 单测：`ResolveLLMConfig` 覆盖所有 fallback 路径
- 端到端：MCP server 启动加载 `config.local.yaml`，dedup 收到正确的主 LLM 配置
- 向后兼容：未配 `summary` 时所有任务仍走主 LLM

#### D2：现有消费者迁移（~60 LOC）

**任务清单**：

- [ ] **D2.1** `internal/generator/generator.go` — L0/L1 摘要改用 summary tier
- [ ] **D2.2** `internal/extractor/extractor.go` — 记忆提取改用 summary tier
- [ ] **D2.3** 集成测试：验证两个消费者实际使用了 summary 模型（如配了的话）

**验收标准**：
- realtest 中加入 "verify generator/extractor uses summary model" 测试
- 若 `summary.model = "gpt-4o-mini"`，调用日志显示 mini 而非主模型

#### D3：dedup HTTP 客户端统一（可选，~150 LOC）

**目标**：消除 `dedup.go` 的独立 HTTP 客户端，统一走 `llmutil`，让冲突检测也能用 summary tier。

**任务清单**（仅在 D2 完成后评估是否做）：

- [ ] **D3.1** 重构 `dedup.callConflictLLM()` 改用 `llmutil.CallLLM`
- [ ] **D3.2** 移除 `dedup.go` 中的 `bytes/io/net/http` 导入
- [ ] **D3.3** 添加 `dedup.detectConflict()` 的 tier 选择逻辑

**何时跳过 D3**：
- 如果 dedup 当前的 fallback（LLM 失败时返回 false）已经足够稳定
- 如果不需要冲突检测用小模型（语义判断需要主模型质量）

### 2.3 配置示例（用户视角）

```yaml
# config.local.yaml
llm:
  api_key: "clp_xxxxxxxxx"
  model: "gpt-5.4"
  endpoint: "https://api-vip.codex-for.me/v1/chat/completions"
  timeout: 30

  summary:                                # 可选
    api_key: ""                           # 空 = 继承主
    model: "gpt-4o-mini"
    endpoint: "https://api.openai.com/v1/chat/completions"
    timeout: 0                            # 0 = 继承主
```

```bash
# 或 env vars
export MEMORY_LLM_KEY="clp_..."
export MEMORY_LLM_MODEL="gpt-5.4"
export MEMORY_LLM_SUMMARY_MODEL="gpt-4o-mini"
```

---

## 三、Sprint 2：方案 B — 长文本保护

**周期估算**：4 天（1.5 天编码 + 1 天测试 + 1 天文档 + 0.5 天回归）

**前置依赖**：D1 完成（让长文摘要可以用便宜的 summary 模型）

### 3.1 目标

存入 >500 tokens 的记忆时，自动生成摘要存到 `Memory.Abstract`，向量化用摘要、展示优先用摘要、Text 全文保留用于 FTS。

### 3.2 任务清单

- [ ] **B.1** `extractor.ExtractedMemory` 新增 `Abstract` 字段
- [ ] **B.2** `dedup.StoreWithDedup()` 长度检测：
  - `tokutil.EstimateTokens(content) > 500` → 用 summary LLM 生成摘要 → 设置 `Abstract`
- [ ] **B.3** `dedup.insertMemory()` 写入 Abstract 字段
- [ ] **B.4** 嵌入逻辑切换：有 Abstract 时用 Abstract embed，否则用 Text
- [ ] **B.5** 现有所有 store 路径补 Abstract 处理：
  - `Service.Store()` fallback path
  - `Service.Update()` content change path
  - `Service.autoStore()`
  - `Service.Import()`
- [ ] **B.6** 展示路径优先 Abstract：
  - `internal/memservice/format.go formatMemoryLine()`
  - `internal/memservice/service.go` 关联记忆渲染（`cm.Text` → `cm.Abstract`）
  - `internal/mcp/format.go formatMemoryLine()`
- [ ] **B.7** B 上线后**自动触发重校准**（embedding 分布变化）

### 3.3 验收标准

- 存入 1000 字记忆 → Abstract 自动生成 ≤200 字
- recall 输出展示 Abstract（非全文）
- FTS 仍能搜到 Text 中的完整关键词
- 重校准后 connection 行为正常

### 3.4 风险缓解

| 风险 | 缓解措施 |
|------|---------|
| 摘要丢失关键信息 | Text 全文保留，recall 可选展开（未来 API） |
| dedup 阈值失效 | B 上线触发自动重校准 |
| Legacy REST 路径绕过 | 文档说明（向后兼容路径） |

---

## 四、Sprint 3：方案 A-Phase1 — Leaf 语义分组

**周期估算**：3 天

**前置依赖**：智能联想已就位（connections 图）✅

### 4.1 目标

把 consolidation 的"盲取最新 50 条"改成"按 connections 图智能分组成 5-10 条小组"。

### 4.2 任务清单

- [ ] **A1.1** 新增 `store.ListUnconsolidatedWithConnections(limit int)`
  - 一次性返回未聚合记忆 + connections（避免 N+1 查询）
- [ ] **A1.2** `consolidate.Consolidator` 新增 `LeafPass(ctx)` 方法
  - 取一条种子记忆 → BFS 扩展（max 10 个节点）→ 送 LLM
  - 重复直到没有未聚合记忆
- [ ] **A1.3** "孤立组"处理：无 connections 的记忆按时间窗口聚合
- [ ] **A1.4** Scheduler 改用 LeafPass 而非旧的 Consolidate
- [ ] **A1.5** 集成测试：验证同主题分组质量

### 4.3 验收标准

- 一组 consolidation 内的记忆主题相关性 > 单批盲取
- 每组 LLM token 消耗下降（~5-10 条 vs 50 条）
- 旧 Consolidation 数据兼容（不破坏现有 schema）

---

## 五、Sprint 4（待评估）：方案 A-Phase2 — Multi-depth

**周期估算**：2 周（含 schema 决策 + 重构）

**触发条件**：A-Phase1 上线 1 个月后，evaluation 显示需要更高层级抽象

### 5.1 路径选择（必须先决策）

**路径 A**：把 consolidation 合并到 `memories` 表（用 `node_type="consolidation"` + `depth`）
- 优点：概念统一，未来扩展容易
- 缺点：现有代码大量修改

**路径 B**：保持独立表 + `parent_consolidation_id` + `depth` + 专用 condensation API
- 优点：改动局部，影响小
- 缺点：逻辑分叉，长期维护成本高

### 5.2 任务清单（待路径决策后细化）

- [ ] **A2.0** Schema 路径决策（架构评审）
- [ ] **A2.1** schema 迁移
- [ ] **A2.2** `ListLeafConsolidations(eligible)` 查询
- [ ] **A2.3** `CondensationPass()` 实现
- [ ] **A2.4** Recall 展示按 depth 排序
- [ ] **A2.5** Scheduler 增加 condensation 周期

---

## 六、Sprint 5（待定）：方案 C — Consolidation LLM 降级

**周期估算**：1 天

**触发条件**：consolidation 的 LLM 失败成为运维痛点（如月度故障率 > 5%）

### 6.1 任务清单

- [ ] **C.1** `consolidate.consolidateWithEscalation()` 三级降级
  - Normal: temperature=0.2, max_tokens=1024
  - Aggressive: temperature=0.1, max_tokens=512
  - Fallback: 确定性截断（不调 LLM）
- [ ] **C.2** 单测覆盖三级路径
- [ ] **C.3** Metrics：记录每级触发次数

---

## 七、跨 Sprint 共享原则

### 7.1 Codex 联合审查制度

- 每个 Sprint **完成后必走**联合审查
- 至少 3 轮（首检 → 修复 → 复检 → 通过）
- 保留审查记录到 commit message

### 7.2 测试矩阵保持

每个 Sprint 必须维持：
- 13 packages 全量回归 ✅
- `cmd/realtest` 端到端（API + 本地双模型）
- `cmd/calibration_test` 阈值验证

### 7.3 文档同步

每个 Sprint 完成后：
- 更新 `SMART_ASSOCIATION.md`（新增章节）
- 在本文件标记完成状态
- commit message 引用 NEXT_PHASE_PROPOSAL.md 章节号

### 7.4 配置变更管理

- 任何配置字段变更必须更新：
  - `config.yaml`（默认）
  - `config.local.yaml.example`（待创建）
  - `docs/USAGE_GUIDE.md`
  - 本文件的配置示例

---

## 八、预估总周期

| Sprint | 内容 | 周期 | 累计 |
|--------|------|------|------|
| 1 | 方案 D（D1+D2） | 3 天 | 3 天 |
| 2 | 方案 B（含重校准） | 4 天 | 7 天 |
| 3 | 方案 A-Phase1 | 3 天 | 10 天 |
| 4（待评估） | 方案 A-Phase2 | 14 天 | 24 天 |
| 5（待评估） | 方案 C | 1 天 | 25 天 |

**确定性 Sprint 1-3 共 10 工作日**（约 2 周）即可完成核心三个方案。

---

## 九、决策记录（ADR-style）

### ADR-001：方案 D 优先于 B

**决策**：D 作为基础设施先行
**理由**：B 需要生成摘要，没有 D 就只能用主模型生成（成本浪费）。D 是 80→190 行的小投入，但 unblocks B/A-Phase2。

### ADR-002：A 拆分两期

**决策**：A 分 Phase1（leaf）和 Phase2（multi-depth）
**理由**：Codex 审查发现 Phase2 需要 schema 重构（600 LOC）。先用 Phase1 验证语义分组的实际效果，再决定是否值得做 Phase2。

### ADR-003：C 降为最低优先级

**决策**：C 排到最后
**理由**：当前 dedup 的冲突检测已有 fallback；只有 consolidation 没有降级。这只在 LLM 长期不可用时才痛，多数环境用不上。

### ADR-004：Legacy REST 不变

**决策**：方案 B 不修改 `cmd/server/main.go` 的 legacy POST/PUT 路径
**理由**：legacy REST 是向后兼容路径，主流程走 MCP/Tool API。改 legacy 会破坏现有客户端。

---

**审核请关注**：
- Sprint 顺序是否合理？
- D2 和 D3 的边界（generator/extractor 迁移 vs dedup 重构）是否清晰？
- A-Phase2 的 schema 路径决策需要谁拍板？
