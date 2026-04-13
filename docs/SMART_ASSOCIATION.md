# 智能记忆联想系统

> 版本：v2.0 — 2026-04-13
> 基于 lossless-claw-enhanced 项目的跨系统增强方案

---

## 一、背景与动机

### 1.1 问题

HybridMem-RAG v1.0 的记忆是**孤立存储**的——每条记忆独立检索，不知道彼此之间的关系。Consolidation 是"盲的"（按时间取 50 条扔给 LLM），connections 字段存在但从未被检索利用。

### 1.2 参考

- **lossless-claw-enhanced**（Martian LCM）：DAG 层级摘要 + 连接图
- **Google always-on-memory-agent**：LLM 驱动的关联检测 + 周期性聚合

### 1.3 设计决策

经过 Codex + Claude 联合评估，从这两个项目中选择性迁移了 3 个核心能力，拒绝了 4 个不适合的做法：

**迁移的**：
1. 存入时实时建立 connections（而非只在 consolidation 时）
2. Recall 时展开关联记忆（命中记忆 → 关联记忆）
3. LLM 合并判断（高相似度同类型记忆的冲突检测）

**拒绝的**：
- 盲取最新 N 条做 consolidation（我们有向量搜索，不需要盲取）
- 无向量搜索纯 LLM 推理（我们的 hybrid search 远优于全量读取）
- Entity/topic 提取为独立字段（向量嵌入已隐式捕获）
- 文件夹监控自动摄入（和记忆联想无关）

---

## 二、架构

### 2.1 数据流

```
新记忆存入 → StoreWithDedup (dedup.go)
  ├─ Step 1: Embed（向量化）
  ├─ Step 2: VectorSearch（找候选）
  ├─ Step 3: Dedup 检查
  │   ├─ content_hash 精确匹配 → duplicate
  │   ├─ cosine > DupThreshold → semantic duplicate
  │   ├─ cosine > ConflictThreshold → LLM 冲突判断
  │   │   ├─ 矛盾 → supersede 旧记忆
  │   │   └─ 不矛盾 → 正常存储
  │   └─ cosine < ConflictThreshold → 正常存储
  ├─ Step 4: Insert 记忆
  └─ Step 5: buildConnectionsFiltered
      └─ cosine ∈ [ConnectionMinSim, ConnectionMaxSim) → 双向 AddConnection

Recall 查询 → Service.Recall (service.go)
  ├─ Search（向量 + BM25 + RRF + Reranker + MMR）
  ├─ expandConnections（解析 top 3 命中的 connections → 拉取关联记忆）
  └─ formatContext
      ├─ 📌/💡/👤/🔧/🔗/📅 记忆分类展示
      ├─ 🔗 关联记忆（独立区域，15% 预算）
      └─ 🧠 洞察（consolidation insights）
```

### 2.2 Dedup 管道接入

**之前**：`memory_store` / `autoStore` / `memory_update` 直接调用 `store.Insert()`，绕过 dedup。

**之后**：全部路由到 `StoreWithDedup`：

```go
Service.Store()     →  s.dedup.StoreWithDedup()  →  connections 自动建立
Service.autoStore() →  s.dedup.StoreWithDedup()  →  connections 自动建立
Service.Update()    →  s.dedup.StoreWithDedup()  →  scope/tags 保持
```

当 dedup 为 nil 时（embedder 未配置），fallback 到直接 insert（向后兼容）。

### 2.3 Connection 存储格式

```json
[
  {"linked_to": "uuid-xxx", "relationship": "related (sim=0.78)"},
  {"linked_to": "uuid-yyy", "relationship": "同为 Go 编程技能记忆"}
]
```

- **防重复**：同一对记忆只建一条连接
- **Label 升级**：consolidation 产生的具体描述会替换 generic "related (sim=...)" 标签
- **事务保护**：AddConnection 使用 SQLite 事务防止并发丢失

---

## 三、Embedder-Specific 阈值 Profile

### 3.1 问题

不同嵌入模型的余弦相似度分布差异巨大：

| 模型 | UNRELATED baseline | RELATED range | DUPLICATE range |
|------|--------------------|---------------|-----------------|
| Qwen3-0.6B (local ONNX) | 0.83-0.85 | 0.86-0.93 | 0.95-0.98 |
| Qwen3-Embedding-4B (API) | 0.27-0.47 | 0.57-0.78 | 0.90-0.98 |

一组固定阈值无法适配所有模型。

### 3.2 解决方案：自动校准

**首次使用新模型时**，bootstrap 自动跑 23 对标定文本（7 duplicate + 9 related + 7 unrelated），测量真实余弦分布，推导最优阈值：

```
bootstrap.Load()
  → detectModelName() (e.g. "openai:Qwen/Qwen3-Embedding-4B")
  → LoadCachedCalibration()  命中? → 直接用
  → 未命中? → Calibrate()
    → 23 对 embed
    → 统计 DUP/REL/UNREL 的 min/max
    → 推导 DupThreshold / ConflictThreshold / ConnectionBand
    → SaveCalibration() → ~/.hybridmem/calibration.json
```

**校准缓存格式**：

```json
{
  "openai:Qwen/Qwen3-Embedding-4B": {
    "model_name": "openai:Qwen/Qwen3-Embedding-4B",
    "calibrated_at": "2026-04-13T11:12:01+08:00",
    "dup_threshold": 0.893,
    "conflict_threshold": 0.842,
    "connection_min_sim": 0.475,
    "connection_max_sim": 0.893,
    "dup_min": 0.903, "dup_max": 0.982,
    "rel_min": 0.571, "rel_max": 0.781,
    "unrel_min": 0.272, "unrel_max": 0.465,
    "num_pairs": 23
  }
}
```

### 3.3 已知 Profile

| Profile | 触发条件 | 校准状态 |
|---------|---------|---------|
| `qwen3-local` | `embedding.provider=local` + model_path 含 "qwen3" | 硬编码（44 对真实校准） |
| `calibrated:*` | 自动校准过的任何模型 | 缓存在 ~/.hybridmem/ |
| `jina-v3` / `openai-v3` | 对应 provider + model | placeholder（待校准） |
| `generic` | 未知模型 | 保守默认值 |

---

## 四、Token 预算感知

### 4.1 CJK-Aware Token 估算

位于 `internal/tokutil/estimate.go`，替换了原来的 `runes * 2/3` 粗糙估算：

| 字符类 | 权重 | 说明 |
|--------|------|------|
| ASCII/Latin | ×0.25 | 4 chars ≈ 1 token |
| CJK 汉字/假名/谚文 | ×1.5 | 1 char ≈ 1.5 tokens |
| CJK 标点 (U+3000-U+303F) | ×1.5 | 。、「」 等 |
| 全角字符 (U+FF00-U+FFEF) | ×1.5 | ＡＢＣ 等 |
| Emoji | ×2.0 | 包括 flag、extended-A |

**误差对比**（vs cl100k_base 真实 tokenizer）：

| 场景 | 新估算误差 | 旧估算误差 |
|------|-----------|-----------|
| 纯 CJK | 5% | 55% |
| 纯 ASCII | 22% | 211% |
| 混合 | 6% | 25% |

### 4.2 Recall 预算分配

```
max_tokens (默认 1000, 上限 8000)
  ├─ 先计算 insight 实际用量（如无 insight → 全给记忆）
  ├─ 关联记忆：15%（有 connections 时）
  └─ 记忆主体：剩余全部
```

- 每个 section header 写入前检查预算
- 超大 item 跳过不终止，尝试更小的
- 显示的"召回 N 条"是实际渲染的数量，非搜索结果数

---

## 五、Tags 持久化

### 5.1 覆盖的路径

| 操作 | Tags 行为 |
|------|-----------|
| `memory_store`（dedup 路径） | 新记忆设置 tags；duplicate 时传播到已有记忆 |
| `memory_store`（fallback） | 直接 SetTags |
| `memory_update`（metadata） | `tags: null` → 保持；`tags: []` → 清除；`tags: [...]` → 替换 |
| `memory_update`（content change） | 继承旧记忆 tags（除非调用方显式提供） |
| `memory_export` | 包含 tags |
| `memory_import` | 恢复 tags |

### 5.2 UpdateRequest.Tags 语义

`*[]string` 指针类型：
- `nil`（JSON 中省略 `tags` 字段）→ 保持已有 tags
- `&[]string{}`（JSON 中 `"tags": []`）→ 清除所有 tags
- `&[]string{"Go", "后端"}`→ 替换为新 tags

---

## 六、SourceConv 利用

- `memory_recall` 新增 `source_conv` 参数，过滤特定对话产生的记忆
- 搜索池自动扩大 (`limit * 10`) 以补偿过滤损失
- `expandConnections` 也遵循 source_conv 过滤
- `formatMemoryLine` 展示 `[conv:ID]` 后缀
- **设计选择**：跨对话 dedup 是有意的——同一事实不管哪个对话提到都只存一次

---

## 七、Bug 修复（Codex 联合审查）

### 7.1 存储层

| Bug | 修复 |
|-----|------|
| `searchGlobalMemories` 有向量时跳过 BM25 | 两路都执行 + RRF 融合 |
| `parseHierarchyLevels("/")` 生成无效层级 | root path 委托 HybridSearch |
| `AddConnection` 无事务保护 | 包装在 BEGIN/COMMIT 中 |
| `AddConnection` 重复追加 | 检查已有 linked_to |
| `Get()` 不读 connections/deleted_at | 加入 SELECT 列 |

### 7.2 MCP/Service 层

| Bug | 修复 |
|-----|------|
| JSON marshal 失败客户端挂起 | 发送带原始 ID 的错误响应 |
| `Scheduler.Stop()` 并发 close panic | `sync.Once` |
| `consolidate.go` json.Marshal 错误忽略 | 检查并返回 |
| `consolidate.go` `fmt.Sprint(nil)` → `"<nil>"` | 类型断言 |
| `dedup.New` nil embedder panic | 校验 + panic |
| Update `RecordSupersession` fatal 后状态不一致 | 改为 non-fatal log |
| Update 硬编码 `Scope="global"` | 传递 existing.Scope |
| `expandConnections` 不过滤 Types/MinImportance | 添加过滤 |
| `searchLimit` 超过 hierarchical 100 上限 | cap 100 |

---

## 八、测试

### 8.1 测试矩阵

| 测试 | 类型 | 模型 | 通过 |
|------|------|------|------|
| `cmd/association_test` | Mock 集成 | clusterEmbedder (32d) | 13/13 |
| `cmd/complextest` | Mock 集成 | clusterEmbedder (32d) | 18/18 |
| `cmd/calibration_test` | 真实校准 | Qwen3-0.6B ONNX | 44 对 |
| `cmd/realtest` (local) | 真实端到端 | Qwen3-0.6B ONNX | 24/24 |
| `cmd/realtest` (API) | 真实端到端 | Qwen3-4B + Reranker-4B + GPT-5.4 | 29/29 |
| `internal/tokutil` | 单元测试 | — | 12/12 + benchmark |
| `internal/...` | 全量回归 | — | 13 packages OK |

### 8.2 嵌入模型对比

| 指标 | Qwen3-0.6B (local) | Qwen3-4B (API) |
|------|---------------------|----------------|
| 相似文本 cosine | 0.923 | 0.710 |
| 不相关 cosine | 0.884 | 0.503 |
| **区分度** | **0.039** | **0.207** (5x) |
| Connections | 5/8 | 8/8 |
| Recall 准确率 | 4/4 | 4/4 |
| 延迟 | ~0ms | ~2-3s/query |

---

## 九、配置

### 9.1 文件优先级

```
MEMORY_CONFIG_PATH > config.local.yaml > config.yaml > 默认值
```

`config.local.yaml` 在 `.gitignore` 中，包含 API keys，不会提交。

### 9.2 环境变量

| 变量 | 说明 |
|------|------|
| `MEMORY_EMBED_PROVIDER` | 覆盖嵌入模型 provider (local/openai/jina) |
| `MEMORY_LLM_KEY` | LLM API key（冲突检测 + consolidation） |
| `MEMORY_LLM_ENDPOINT` | LLM endpoint |
| `MEMORY_LLM_MODEL` | LLM model name |
| `MEMORY_EMBEDDER_PROFILE` | 手动指定阈值 profile（通常自动检测） |
| `MEMORY_CONFIG_PATH` | 强制指定配置文件路径 |

---

## 十、Commit 历史

```
7bb144f feat: embedder-specific dedup threshold profiles + real calibration tests
8d98d29 Revert "fix: bootstrap graceful degradation for FastText and config"
b6c3802 fix: bootstrap graceful degradation for FastText and config
02be7b3 fix: AddConnection transaction safety + Update supersession non-fatal
c784d51 fix: tags persistence edge cases from joint review
8cba7e2 feat: utilize SourceConv field for recall filtering and display
c49bc89 fix: complete tags persistence across all memory operations
b799158 fix: persist tags on memory store and update
5d1e476 feat: CJK token budget + smart memory association + bug fixes
```
