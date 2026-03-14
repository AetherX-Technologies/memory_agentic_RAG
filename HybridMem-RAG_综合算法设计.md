# HybridMem-RAG 综合算法设计方案

## Context

当前项目包含三个独立的 RAG 相关教学文档，每个系统都有其独特优势：

- **Memory Agent**: 主动整合机制，能像人脑一样发现记忆间的关联并生成洞见
- **OpenViking**: 分层向量检索，利用文件系统层次结构提供搜索方向
- **Memory-LanceDB-Pro**: 混合检索策略（Vector + BM25）和12阶段精排管道

这些系统各自解决了 RAG 的不同痛点，但都是独立设计。本方案旨在设计一个**统一的综合算法**，融合三者优势，创建一个更强大的记忆检索系统。

## 核心设计理念

**HybridMem-RAG** 采用三层架构：

```
Memory Agent 主动整合层 (顶层)
    ↓ 提供结构化存储和关联发现
OpenViking 分层检索层 (中层)
    ↓ 提供层次化搜索路径
LanceDB-Pro 混合评分层 (底层)
    ↓ 提供精确的相关性评分
```

## 算法流程设计

### 1. 智能存储阶段

**融合点**: Memory Agent 的结构化 + OpenViking 的层次化 + LanceDB-Pro 的作用域隔离

**核心逻辑**:

```typescript
// 1. LLM 结构化提取 (Memory Agent)
structured = {
  summary: "简短摘要",
  entities: ["实体1", "实体2"],
  topics: ["主题A", "主题B"],
  importance: 0.8,
  memoryType: 'fact' | 'preference' | 'insight'
}

// 2. 层次路径解析 (OpenViking)
hierarchy = parseFileSystemContext(filePath)
// 例: /project/src/auth/login.ts → ["project", "project/src", "project/src/auth"]

// 3. 作用域标记 (LanceDB-Pro)
scope = "agent:<agentId>" | "global" | "custom:<name>"

// 4. 统一存储
store.insert({
  content, vector,
  metadata: { ...structured, hierarchy, scope, timestamp }
})

// 5. 触发异步整合
scheduleConsolidation(sessionId)
```

**关键创新**: 每条记忆同时具备语义向量、结构化元数据、层次位置和作用域标签，支持多维度检索。

### 2. 分层混合检索阶段

**融合点**: OpenViking 的分层遍历 + LanceDB-Pro 的混合检索（Vector + BM25）

**核心逻辑**:

```typescript
// 1. 解析当前上下文的层次路径
hierarchyLevels = ["/project", "/project/src", "/project/src/auth"]

// 2. 在每一层执行混合检索
for (level in hierarchyLevels) {
  vectorResults = vectorSearch(queryVector, filter: {hierarchy: level})
  bm25Results = bm25Search(query, filter: {hierarchy: level})

  // RRF 融合: score = 1/(rank+60)
  fusedResults[level] = rrfFusion(vectorResults, bm25Results)
}

// 3. 跨层聚合（当前层权重更高）
aggregated = weightedAggregation(fusedResults, currentLevel)
```

**关键创新**: 不是全局搜索，而是沿着层次结构逐层检索，既保留了上下文关联，又结合了语义和精确匹配。

### 3. 12阶段精排管道

**融合点**: LanceDB-Pro 的完整评分管道 + Memory Agent 的关联图谱加权

**12个阶段**:

1. **自适应判断**: 跳过问候语、命令等无效查询
2. **向量化**: 查询文本转向量
3. **并行检索**: Vector + BM25 同时执行
4. **RRF融合**: 倒数排名融合
5. **交叉编码器重排**: 精排 60% + 原始分 40%
6. **新近度提升**: `boost = exp(-days/14) × 0.1`
7. **重要性加权**: `score × (0.7 + 0.3 × importance)`
8. **长度归一化**: 短文本奖励，长文本惩罚
9. **访问强化衰减**: 频繁召回的记忆半衰期延长
10. **关联图谱加权** (新增): 连接到高分记忆的节点获得加权
11. **硬性过滤**: 移除 score < 0.35 的结果
12. **噪声过滤 + MMR多样性**: 移除拒绝回复、元问题，降权相似记忆

**关键创新**: 第10阶段是新增的，利用 Memory Agent 的双向连接图谱，如果一条记忆连接到其他高分记忆，说明它在知识图谱中处于重要位置，应该提升分数。

### 4. 异步后台整合

**融合点**: Memory Agent 的主动整合机制

**触发条件**:
- 累积 ≥ 10 条未整合记忆
- 或距上次整合 ≥ 30 分钟

**整合流程**:

```typescript
// 1. LLM 分析关联
analysis = llm.analyzeConnections(pendingMemories)
// 输出: { connections: [...], insights: [...] }

// 2. 更新双向连接
for (conn in analysis.connections) {
  store.addConnection(conn.from, conn.to, {
    relationship: conn.type,  // "因果"、"对比"、"补充"
    strength: conn.confidence  // 0-1
  })
}

// 3. 生成洞见并存储
for (insight in analysis.insights) {
  intelligentStore({
    content: insight.content,
    memoryType: 'insight',
    importance: 0.9  // 洞见通常很重要
  })
}
```

**关键创新**: 系统不仅存储原始信息，还能主动发现隐藏的关联和模式，生成新的洞见记忆。

## 系统架构图

```
┌─────────────────────────────────────────────────────────────┐
│                      用户查询                                 │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│              分层混合检索层 (OpenViking + LanceDB)            │
│  层级1: /project        → Vector + BM25 → RRF融合            │
│  层级2: /project/src    → Vector + BM25 → RRF融合            │
│  层级3: /project/src/auth → Vector + BM25 → RRF融合          │
│                    ↓ 跨层聚合                                 │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│              12阶段精排管道 (LanceDB + Memory Agent)          │
│  Stage 1-4:  基础检索与融合                                   │
│  Stage 5:    交叉编码器重排                                   │
│  Stage 6-9:  时间/重要性/长度/访问强化调整                     │
│  Stage 10:   关联图谱加权 ⭐ (新增)                           │
│  Stage 11-12: 过滤与多样性                                    │
└────────────────────────┬────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────────┐
│                    返回 Top-K 结果                            │
└─────────────────────────────────────────────────────────────┘

                    [后台异步运行]
┌─────────────────────────────────────────────────────────────┐
│           主动整合层 (Memory Agent)                           │
│  - 每30分钟或10条新记忆触发                                    │
│  - LLM 分析关联关系                                           │
│  - 更新双向连接图谱                                           │
│  - 生成跨记忆洞见                                             │
└─────────────────────────────────────────────────────────────┘
```

## 关键配置参数

```typescript
config = {
  // 存储配置
  consolidationInterval: 30,      // 分钟
  consolidationThreshold: 10,     // 条记忆

  // 检索配置
  hierarchyDepth: 3,              // 层级深度
  layerWeightDecay: 0.8,          // 每层权重衰减
  candidatePoolSize: 20,          // 候选池大小

  // 评分配置
  recencyHalfLife: 14,            // 天
  reinforcementFactor: 0.5,       // 访问强化系数
  connectionBoostWeight: 0.1,     // 关联加权系数
  hardMinScore: 0.35,             // 最低分数阈值

  // 作用域配置
  defaultScope: "global",
  agentAccess: {
    "main": ["global", "agent:main"],
    "discord-bot": ["global", "agent:discord-bot"]
  }
}
```

## 实现文件结构

如果要实现这个算法，建议创建以下文件：

- **src/core/unified-store.ts** - 统一存储层，整合 LanceDB + 结构化元数据 + 层次索引
- **src/retrieval/hierarchical-hybrid.ts** - 分层混合检索引擎，实现 OpenViking 的分层遍历 + LanceDB 的混合检索
- **src/scoring/pipeline.ts** - 12阶段评分管道，包含所有评分因子和关联图谱加权
- **src/consolidation/scheduler.ts** - 异步整合调度器，实现 Memory Agent 的主动整合机制
- **src/graph/connection-manager.ts** - 双向连接图谱管理，支持关系存储和图遍历
- **src/observability/tracer.ts** - 可观测性追踪，记录每个阶段的性能指标

## 核心优势总结

1. **多维度检索**: 同时利用语义向量、关键词匹配、层次结构、作用域隔离
2. **主动思考**: 不仅存储和检索，还能发现关联、生成洞见
3. **精确评分**: 12阶段管道考虑时间、重要性、长度、访问频率、关联关系等多个因子
4. **层次感知**: 利用文件系统结构提供搜索方向，避免全局搜索的噪声
5. **异步处理**: 整合过程在后台运行，不阻塞用户操作
6. **可观测性**: 全链路追踪，便于调试和优化

## 验证方法

实现后可通过以下方式验证：

1. **存储验证**:
   - 存储一条记忆，检查是否正确提取了 entities、topics、importance
   - 验证层次路径是否正确解析
   - 确认作用域标签是否生效

2. **检索验证**:
   - 测试纯语义查询（应该召回语义相似的记忆）
   - 测试精确关键词查询（应该召回包含关键词的记忆）
   - 测试分层检索（在特定目录下搜索应该优先返回该层级的结果）

3. **整合验证**:
   - 存储多条相关记忆，等待整合触发
   - 检查是否生成了连接关系
   - 验证是否产生了洞见记忆

4. **性能验证**:
   - 测量检索延迟（目标 < 500ms）
   - 测量整合时间（应该在后台异步完成）
   - 监控内存和 CPU 使用率

5. **质量验证**:
   - 对比传统 RAG 和 HybridMem-RAG 的召回率
   - 评估生成洞见的质量和相关性
   - 测试作用域隔离是否有效防止信息泄露

## 下一步行动

1. 创建项目骨架和目录结构
2. 实现统一存储层（unified-store.ts）
3. 实现分层混合检索引擎（hierarchical-hybrid.ts）
4. 实现12阶段评分管道（pipeline.ts）
5. 实现异步整合调度器（scheduler.ts）
6. 添加可观测性和监控
7. 编写单元测试和集成测试
8. 性能优化和调优
