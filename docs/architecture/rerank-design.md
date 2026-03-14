# 交叉编码器重排（Cross-Encoder Reranking）设计方案

> 参考：../memory-lancedb-pro-main/src/retriever.ts (line 484-583)
> 日期：2026-03-14

---

## 1. 功能概述

在混合检索（Vector + BM25 + RRF）之后，使用交叉编码器模型对候选结果重新排序，提高检索准确率。

---

## 2. 原版实现分析

### 2.1 工作流程
```
混合检索 → 取Top 2N候选 → 交叉编码器重排 → 时间衰减 → 其他评分因子
```

### 2.2 核心逻辑（retriever.ts:484-583）

**两种模式：**
1. **Cross-encoder API**（主模式）
   - 调用 Jina Reranker API
   - 传入：query + documents[]
   - 返回：relevance_score[]
   - 混合分数：`0.6 × rerank_score + 0.4 × original_score`
   - 超时：5秒

2. **Cosine Similarity**（fallback）
   - 本地计算余弦相似度
   - 混合分数：`0.7 × original_score + 0.3 × cosine_score`

### 2.3 API 调用细节

**请求格式（Jina）：**
```json
{
  "model": "jina-reranker-v2-base-multilingual",
  "query": "查询文本",
  "documents": ["文档1", "文档2", ...],
  "top_n": 20
}
```

**响应格式：**
```json
{
  "results": [
    {"index": 0, "relevance_score": 0.95},
    {"index": 2, "relevance_score": 0.87},
    ...
  ]
}
```

---

## 3. Go 实现方案

### 3.1 文件结构
```
internal/store/
  ├── rerank.go          # 重排核心逻辑
  ├── rerank_test.go     # 单元测试
  └── scoring.go         # 现有评分管道（需修改）
```

### 3.2 核心接口设计

```go
// RerankConfig 重排配置
type RerankConfig struct {
    Enabled           bool
    Provider          string  // "jina", "voyage", "pinecone", "siliconflow"
    APIKey            string
    Model             string
    Endpoint          string
    Timeout           int     // 秒，默认5
    BlendWeight       float64 // 重排分数权重，默认0.6
    MaxCandidates     int     // 最大候选数，默认50
    MaxDocLength      int     // 文档最大长度，默认2000
    UnreturnedPenalty float64 // 未返回结果惩罚，默认0.8
    MinBlendedScore   float64 // 混合分数下限比例，默认0.5
}

// ProviderAdapter Provider适配器接口
type ProviderAdapter interface {
    BuildRequest(query string, docs []string, topN int) (*http.Request, error)
    ParseResponse(body []byte) ([]RerankResult, error)
}

// Reranker 重排器接口
type Reranker interface {
    Rerank(query string, results []SearchResult) ([]SearchResult, error)
}

// jinaReranker Jina实现
type jinaReranker struct {
    config  RerankConfig
    client  *http.Client
    adapter ProviderAdapter
}
```

### 3.3 实现步骤

**Step 1: 创建 rerank.go**
- 实现 Jina API 调用
- 实现余弦相似度 fallback
- 实现分数混合逻辑

**Step 2: 集成到 HybridSearch**
- 在 `internal/store/hybrid.go` 中调用重排
- 位置：RRF融合之后，ApplyScoring之前

**Step 3: 配置管理**
- 在 `Config` 结构体中添加 `RerankConfig`
- 支持环境变量配置

---

## 4. 代码量估算

| 文件 | 行数 | 说明 |
|------|------|------|
| rerank.go | ~150 | API调用 + fallback |
| rerank_test.go | ~80 | 单元测试 |
| hybrid.go修改 | ~20 | 集成调用 |
| types.go修改 | ~15 | 配置结构体 |
| **总计** | **~265** | **约2-3小时工作量** |

---

## 5. 性能影响

### 5.1 延迟增加
- API调用：50-200ms（取决于网络）
- Fallback（本地）：<5ms

### 5.2 准确率提升
- 原版测试：提升5-15%（取决于查询类型）
- 对复杂语义查询效果更明显

---

## 6. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| API超时 | 检索延迟 | 5秒超时 + fallback |
| API失败 | 检索质量下降 | 自动降级到余弦相似度 |
| API费用 | 成本增加 | 可配置开关，默认关闭 |

---

## 7. 建议

### 7.1 实现优先级
- **高**：基础实现（Jina API + fallback）
- **中**：多provider支持（Voyage, Pinecone）
- **低**：缓存优化

### 7.2 配置建议
```go
// 默认配置（保守）
DefaultRerankConfig = RerankConfig{
    Enabled:     false,  // 默认关闭
    Provider:    "jina",
    Model:       "jina-reranker-v2-base-multilingual",
    Endpoint:    "https://api.jina.ai/v1/rerank",
    Timeout:     5,
    BlendWeight: 0.6,
}
```

---

## 8. 验收标准

1. ✅ API调用成功率 > 95%
2. ✅ 超时自动fallback
3. ✅ 准确率提升 > 5%（对比测试）
4. ✅ 延迟增加 < 200ms（P95）

---

**结论**：实现复杂度适中，收益明显，建议实现。
