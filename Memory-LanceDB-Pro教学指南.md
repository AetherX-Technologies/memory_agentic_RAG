# Memory-LanceDB-Pro 深度教学指南

> **教授与学生的对话式教学**
> 目标受众：希望深入理解向量数据库与混合检索系统的大学生

---

## 课程大纲

1. [第一课：问题与动机](#第一课问题与动机)
2. [第二课：向量数据库基础](#第二课向量数据库基础)
3. [第三课：混合检索理论](#第三课混合检索理论)
4. [第四课：评分管道架构](#第四课评分管道架构)
5. [第五课：作用域隔离](#第五课作用域隔离)
6. [第六课：实战案例](#第六课实战案例)
7. [第七课：配置与调优](#第七课配置与调优)

---

## 第一课：问题与动机

### 场景：教授办公室，下午2点

**学生**：教授您好！我看到这个 memory-lancedb-pro 项目，但不太理解为什么需要这么复杂的记忆系统。AI 不是已经有上下文窗口了吗？

**教授**：很好的问题！让我们从一个实际场景开始。假设你有一个 AI 助手，你告诉它："我喜欢用 TypeScript 而不是 JavaScript"。第二天，你问它："帮我写个函数"，你希望它记得用 TypeScript，对吧？

**学生**：对，这很合理。

**教授**：但问题来了。如果这个对话发生在一个月前，而你的上下文窗口只能容纳最近的 100 条消息，那么这个偏好就丢失了。这就是**长期记忆**的需求。

**学生**：明白了！但为什么不直接把所有历史对话都存起来，每次都搜索一遍呢？

**教授**：成本问题！假设你有 10 万条历史对话，每次都要：
1. 把查询转换成向量（embedding）
2. 计算与 10 万条记录的相似度
3. 找出最相关的几条

这在计算上非常昂贵。更糟糕的是，**简单的向量搜索会遗漏关键信息**。

**学生**：为什么会遗漏？向量不是能捕捉语义相似性吗？

**教授**：让我举个例子。用户问："我的 API key 是什么？"

- 向量搜索可能找到："API 密钥存储在环境变量中"（语义相似）
- 但真正需要的是："你的 API key 是 sk-proj-abc123"（包含精确关键词）

向量搜索擅长**语义理解**，但不擅长**精确匹配**。这就是为什么我们需要**混合检索**。

### 核心问题总结

**教授**：让我总结一下这个项目要解决的三大核心问题：

```
问题1：长期记忆存储
├─ 上下文窗口有限（通常几千到几万 tokens）
├─ 历史对话会被遗忘
└─ 解决方案：持久化存储到向量数据库

问题2：检索质量
├─ 纯向量搜索：语义相似但可能遗漏精确信息
├─ 纯关键词搜索：精确但不理解语义
└─ 解决方案：混合检索（Vector + BM25）

问题3：检索效率
├─ 海量数据中快速找到相关信息
├─ 避免无关记忆污染上下文
└─ 解决方案：多阶段评分管道 + 噪声过滤
```

**学生**：所以这个项目本质上是一个**智能记忆检索系统**？

**教授**：完全正确！它不仅存储记忆，更重要的是**智能地检索**最相关的记忆。就像人脑一样，我们不会记住所有细节，但能在需要时快速回忆起相关信息。

---

## 第二课：向量数据库基础

### 场景：实验室白板前

**教授**：现在让我们深入理解向量数据库的工作原理。首先，什么是**向量**？

**学生**：就是一个数字数组吧？比如 `[0.2, -0.5, 0.8, ...]`？

**教授**：对！但这些数字不是随机的，它们代表**语义空间中的位置**。让我画个图：

```
二维语义空间示例（实际是 1024 维）：

    猫 •
        ↘
          • 小猫
            ↘
              • 宠物
                  ↘
                    • 动物

    汽车 •
         ↘
           • 交通工具
```

**教授**：相似的概念在向量空间中距离更近。这就是**embedding（嵌入）**的核心思想。

### Embedding 的生成过程

**学生**：这些向量是怎么生成的？

**教授**：通过**预训练的神经网络模型**。让我展示代码流程：

```typescript
// src/embedder.ts 的核心逻辑

// 1. 用户输入文本
const text = "我喜欢用 TypeScript";

// 2. 调用 embedding API（如 OpenAI、Jina）
const response = await fetch('https://api.jina.ai/v1/embeddings', {
  method: 'POST',
  body: JSON.stringify({
    model: 'jina-embeddings-v5-text-small',
    input: text,
    task: 'retrieval.passage',  // 任务类型
    normalized: true             // 归一化向量
  })
});

// 3. 得到 1024 维向量
const vector = response.data[0].embedding;
// [0.023, -0.145, 0.089, ..., 0.234]  // 1024 个数字
```

**学生**：为什么要区分 `retrieval.query` 和 `retrieval.passage`？

**教授**：非常敏锐的观察！这是**任务感知嵌入**的概念：

- **Query（查询）**："我的 TypeScript 配置是什么？" → 优化为搜索意图
- **Passage（文档）**："你的 tsconfig.json 使用 strict 模式" → 优化为被搜索内容

同一个模型，针对不同任务生成略有不同的向量，提高检索精度。

### 向量相似度计算

**教授**：有了向量，如何判断两段文本相似？最常用的是**余弦相似度**：

```
余弦相似度公式：
similarity = (A · B) / (||A|| × ||B||)

其中：
- A · B 是向量点积
- ||A|| 是向量 A 的模长
```

**学生**：能举个具体例子吗？

**教授**：当然。假设我们有两个简化的 3 维向量：

```python
# 查询："TypeScript 配置"
query_vector = [0.8, 0.2, 0.1]

# 记忆1："你的 tsconfig.json 使用 strict 模式"
memory1_vector = [0.7, 0.3, 0.15]

# 记忆2："今天天气不错"
memory2_vector = [0.1, 0.1, 0.9]

# 计算相似度
similarity(query, memory1) = 0.92  # 高度相关
similarity(query, memory2) = 0.23  # 不相关
```

**学生**：所以向量搜索就是找到余弦相似度最高的记忆？

**教授**：基本正确！但实际系统中还有很多优化。

### LanceDB 的角色

**教授**：现在问题来了：如果有 10 万条记忆，难道要计算 10 万次余弦相似度吗？

**学生**：那太慢了！

**教授**：这就是**向量数据库**的价值。LanceDB 使用**近似最近邻搜索（ANN）**算法：

```
传统暴力搜索：O(n) - 线性时间
ANN 索引搜索：O(log n) - 对数时间

示例：
- 100,000 条记忆
- 暴力搜索：100,000 次计算
- ANN 搜索：~17 次计算（log₂ 100000）
```

**学生**：ANN 是怎么做到的？

**教授**：简化来说，它构建了一个**多层索引结构**，类似于二叉搜索树，但针对高维向量优化。LanceDB 使用的是 **IVF（倒排文件索引）+ PQ（乘积量化）** 算法。

```
ANN 索引结构（简化）：

Level 3:  [Cluster A] [Cluster B] [Cluster C]
              ↓           ↓           ↓
Level 2:   [A1][A2]   [B1][B2]   [C1][C2]
              ↓           ↓           ↓
Level 1:  [记忆1-100] [记忆101-200] ...

搜索过程：
1. 找到最相关的 Cluster（如 Cluster A）
2. 在 Cluster A 中找到最相关的子集（如 A2）
3. 只在 A2 中进行精确计算
```

**学生**：这样会不会遗漏一些相关记忆？

**教授**：会的！这就是"近似"的含义。但实践中，准确率通常在 95% 以上，而速度提升是数百倍。这是**精度与效率的权衡**。

---

## 第三课：混合检索理论

### 场景：图书馆讨论室

**学生**：教授，我现在理解向量搜索了。但您之前说还需要 BM25，这是什么？

**教授**：BM25 是一个经典的**全文检索算法**，基于关键词匹配。让我对比一下两种方法：

```
场景：用户查询 "我的 Jina API key 是什么？"

向量搜索结果：
1. "API 密钥应该保存在环境变量中" (相似度 0.85)
2. "Jina 提供免费的 embedding 服务" (相似度 0.78)
3. "你的 OpenAI key 是 sk-xxx" (相似度 0.72)

BM25 搜索结果：
1. "你的 Jina API key 是 jina_abc123" (BM25 分数 8.5)
2. "Jina API 文档：https://jina.ai/docs" (BM25 分数 6.2)
3. "记得设置 JINA_API_KEY 环境变量" (BM25 分数 5.8)
```

**学生**：我看出来了！向量搜索理解了"API 密钥"的概念，但没有精确匹配"Jina"；BM25 精确匹配了"Jina API key"，但不理解语义。

**教授**：完美的总结！这就是为什么我们需要**混合检索**。

### BM25 算法原理

**教授**：BM25 的核心思想是：**关键词越稀有，匹配时权重越高**。

```
BM25 公式（简化版）：

score = Σ IDF(qi) × (f(qi) × (k1 + 1)) / (f(qi) + k1 × (1 - b + b × |D| / avgdl))

其中：
- qi: 查询中的第 i 个词
- IDF(qi): 逆文档频率（词的稀有程度）
- f(qi): 词在文档中的出现频率
- |D|: 文档长度
- avgdl: 平均文档长度
- k1, b: 调节参数
```

**学生**：这个公式看起来很复杂...

**教授**：让我用直觉解释：

1. **IDF（逆文档频率）**：
   - "的"、"是" 这些常见词 → IDF 低 → 权重低
   - "Jina"、"API" 这些特定词 → IDF 高 → 权重高

2. **词频（TF）**：
   - 词出现越多，越相关
   - 但有饱和效应（出现 10 次和 100 次差别不大）

3. **长度归一化**：
   - 长文档不应仅因为长就得高分
   - 需要按文档长度归一化

**学生**：所以 BM25 本质上是**加权关键词匹配**？

**教授**：正确！它比简单的关键词计数聪明得多，但仍然是基于词汇匹配，不理解语义。

### 混合检索：RRF 融合策略

**教授**：现在关键问题：如何结合向量搜索和 BM25 的结果？

**学生**：直接把分数加起来？

**教授**：不行！两种算法的分数量纲不同：
- 向量相似度：0 到 1
- BM25 分数：0 到无穷大

我们使用 **RRF（Reciprocal Rank Fusion，倒数排名融合）**：

```typescript
// src/retriever.ts 中的融合逻辑

// 1. 向量搜索得到排名
vectorResults = [
  { id: 'mem1', rank: 1, score: 0.92 },
  { id: 'mem2', rank: 2, score: 0.85 },
  { id: 'mem3', rank: 3, score: 0.78 }
]

// 2. BM25 搜索得到排名
bm25Results = [
  { id: 'mem4', rank: 1, score: 8.5 },
  { id: 'mem1', rank: 2, score: 7.2 },
  { id: 'mem5', rank: 3, score: 6.8 }
]

// 3. RRF 融合（k=60 是常用参数）
function rrfScore(rank) {
  return 1 / (rank + 60);
}

fusedScores = {
  'mem1': rrfScore(1) + rrfScore(2) = 0.0164 + 0.0161 = 0.0325,
  'mem2': rrfScore(2) + 0 = 0.0161,
  'mem3': rrfScore(3) + 0 = 0.0159,
  'mem4': 0 + rrfScore(1) = 0.0164,
  'mem5': 0 + rrfScore(3) = 0.0159
}

// 4. 最终排名：mem1 > mem4 > mem2 > mem3 ≈ mem5
```

**学生**：为什么用倒数排名而不是原始分数？

**教授**：因为**排名是归一化的**！无论原始分数是 0.9 还是 9.0，排名第一就是第一。这样两种算法的贡献是平等的。

### 本项目的改进：加权融合

**教授**：但这个项目做了进一步优化。看 `src/retriever.ts` 的实现：

```typescript
// 不是简单的 RRF，而是加权融合
const vectorWeight = 0.7;  // 向量搜索权重
const bm25Weight = 0.3;    // BM25 权重

// 向量分数作为基础
let fusedScore = vectorScore * vectorWeight;

// BM25 命中则增加 15% 的提升
if (hasBM25Hit) {
  fusedScore += 0.15 * bm25Weight;
}
```

**学生**：为什么向量搜索权重更高？

**教授**：因为在大多数场景下，**语义理解比精确匹配更重要**。但当 BM25 命中时，说明有精确关键词匹配，这是强信号，所以给予额外提升。

**学生**：这个 15% 的提升是怎么确定的？

**教授**：通过**实验调优**。作者在真实数据集上测试了不同的权重组合，发现这个配置在准确率和召回率之间达到最佳平衡。这就是工程中的**超参数调优**。

---

## 第四课：评分管道架构

### 场景：计算机实验室，屏幕上显示代码

**教授**：现在我们进入最核心的部分：**多阶段评分管道**。这是这个项目最精妙的设计。

**学生**：为什么需要多个阶段？直接用混合检索的分数不行吗？

**教授**：让我举个例子。假设你问："上次我们讨论的 TypeScript 配置是什么？"

混合检索可能返回：
1. "TypeScript 配置应该用 strict 模式"（3 个月前，重要性 0.5）
2. "你的 tsconfig.json 已更新为 ES2022"（昨天，重要性 0.9）
3. "TypeScript 官方文档：https://..."（1 年前，重要性 0.3，5000 字长文）

哪个最相关？

**学生**：应该是第 2 条，因为最新且重要！

**教授**：完全正确！但混合检索只看**文本相似度**，不考虑**时间、重要性、长度**等因素。这就是为什么需要多阶段评分。

### 评分管道全景图

**教授**：让我画出完整的管道：

```
┌─────────────────────────────────────────────────────────────┐
│                    查询："上次的 TS 配置"                      │
└────────────────────┬────────────────────────────────────────┘
                     │
        ┌────────────▼────────────┐
        │  1. 自适应检索判断        │  ← 跳过问候语、命令
        │  shouldSkipRetrieval()  │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  2. 向量化查询           │  ← embedQuery()
        │  [0.23, -0.45, ...]    │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  3. 并行检索             │
        │  ├─ 向量搜索 (top 20)   │
        │  └─ BM25 搜索 (top 20)  │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  4. RRF 融合             │  ← 合并结果，去重
        │  候选池：20 条           │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  5. 交叉编码器重排序      │  ← 可选，使用 Jina Reranker
        │  score = 0.6×rerank     │
        │        + 0.4×fused      │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  6. 新近度提升           │  ← exp(-days/14) × 0.1
        │  score += recencyBoost  │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  7. 重要性加权           │  ← score × (0.7 + 0.3×imp)
        │  importance: 0-1        │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  8. 长度归一化           │  ← 惩罚过长文本
        │  score × 1/(1+0.5×log)  │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  9. 时间衰减             │  ← 0.5 + 0.5×exp(-days/60)
        │  floor at 0.5×          │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  10. 硬性最低分过滤      │  ← score < 0.35 → 丢弃
        │  hardMinScore           │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  11. 噪声过滤            │  ← 移除拒绝、元问题
        │  filterNoise()          │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  12. MMR 多样性          │  ← 降权相似结果
        │  cosine > 0.85 → 降权   │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │  返回 Top-K (默认 3 条)  │
        └─────────────────────────┘
```

**学生**：天哪，这么多步骤！每一步都必要吗？

**教授**：让我逐一解释每个阶段的必要性。

### 阶段详解

#### 阶段 1：自适应检索判断

**教授**：看 `src/adaptive-retrieval.ts`：

```typescript
export function shouldSkipRetrieval(query: string): boolean {
  // 跳过问候语
  if (/^(hi|hello|hey|你好|嗨)\s*[!.?]*$/i.test(query)) {
    return true;
  }

  // 跳过斜杠命令
  if (query.trim().startsWith('/')) {
    return true;
  }

  // 跳过纯表情
  if (/^[\u{1F300}-\u{1F9FF}\s]+$/u.test(query)) {
    return true;
  }

  // 太短的查询（英文 < 15 字符，中文 < 6 字符）
  const hasCJK = /[\u4e00-\u9fff]/.test(query);
  const minLen = hasCJK ? 6 : 15;
  if (query.length < minLen) {
    return true;
  }

  return false;
}
```

**学生**：为什么要跳过这些？

**教授**：**效率和质量**！如果用户只是说"hi"，检索记忆毫无意义，还浪费 API 调用（embedding 是要花钱的）。这是**成本优化**。

#### 阶段 5：交叉编码器重排序

**学生**：这个"交叉编码器"是什么？和 embedding 有什么区别？

**教授**：非常好的问题！这是两种不同的模型架构：

```
Bi-Encoder（双编码器）- 用于 Embedding：
┌─────────┐                    ┌─────────┐
│ Query   │ → Encoder → [vec1] │ Memory  │ → Encoder → [vec2]
└─────────┘                    └─────────┘
                ↓                          ↓
            相似度 = cosine(vec1, vec2)

优点：可以预先计算所有 memory 的向量，搜索快
缺点：query 和 memory 独立编码，无法交互

Cross-Encoder（交叉编码器）- 用于 Reranking：
┌─────────────────────────────┐
│ [Query] [SEP] [Memory]      │ → Encoder → 相关性分数
└─────────────────────────────┘

优点：query 和 memory 联合编码，捕捉细微交互
缺点：无法预计算，必须实时计算每对组合
```

**学生**：所以先用快速的 Bi-Encoder 筛选候选，再用精确的 Cross-Encoder 重排序？

**教授**：完美！这是**粗排 + 精排**的经典策略。看代码实现：

```typescript
// src/retriever.ts

// 1. 粗排：向量搜索 + BM25，得到 20 个候选
const candidates = await hybridSearch(query, 20);

// 2. 精排：交叉编码器重排序
if (config.rerank === 'cross-encoder') {
  const rerankScores = await callRerankerAPI(query, candidates);

  // 混合分数：60% 重排序 + 40% 原始分数
  for (let i = 0; i < candidates.length; i++) {
    candidates[i].score =
      0.6 * rerankScores[i] +
      0.4 * candidates[i].originalScore;
  }
}
```

**学生**：为什么不是 100% 用重排序分数？

**教授**：因为重排序模型也不是完美的！保留 40% 的原始分数是**保险策略**，防止重排序模型出现异常。这是工程中的**鲁棒性设计**。

#### 阶段 6-9：时间与重要性因素

**教授**：现在看时间相关的调整：

```typescript
// 6. 新近度提升（Recency Boost）
const ageDays = (now - memory.timestamp) / (24 * 3600 * 1000);
const recencyBoost = Math.exp(-ageDays / 14) * 0.1;
score += recencyBoost;

// 示例：
// 1 天前：exp(-1/14) × 0.1 = 0.093  → +9.3%
// 14 天前：exp(-1) × 0.1 = 0.037   → +3.7%
// 60 天前：exp(-60/14) × 0.1 = 0.001 → +0.1%
```

**学生**：为什么用指数衰减？

**教授**：因为**记忆的价值随时间非线性衰减**。昨天和今天的记忆差别不大，但 1 年前和 2 年前的记忆差别也不大。指数函数完美捕捉这种特性。

```typescript
// 7. 重要性加权
score *= (0.7 + 0.3 * memory.importance);

// 示例：
// importance = 1.0 → ×1.0（无变化）
// importance = 0.5 → ×0.85（轻微降权）
// importance = 0.0 → ×0.7（降权 30%）
```

**学生**：为什么不是直接乘以 importance？

**教授**：因为即使 importance=0，记忆也不应该完全无效。0.7 是**保底权重**，确保所有记忆都有基本价值。

```typescript
// 8. 长度归一化
const lengthPenalty = 1 / (1 + 0.5 * Math.log2(length / 500));
score *= lengthPenalty;

// 示例：
// 500 字：log2(1) = 0 → ×1.0（无惩罚）
// 1000 字：log2(2) = 1 → ×0.67（降权 33%）
// 2000 字：log2(4) = 2 → ×0.5（降权 50%）
```

**学生**：为什么要惩罚长文本？

**教授**：因为**长文本会稀释上下文**。如果注入一个 2000 字的记忆，AI 可能找不到关键信息。短小精悍的记忆更有价值。

```typescript
// 9. 时间衰减（Time Decay）
const timeDecayFactor = 0.5 + 0.5 * Math.exp(-ageDays / 60);
score *= timeDecayFactor;

// 示例：
// 1 天前：0.5 + 0.5×0.98 = 0.99  → ×0.99
// 60 天前：0.5 + 0.5×0.37 = 0.68 → ×0.68
// 365 天前：0.5 + 0.5×0.001 = 0.50 → ×0.50（最低 50%）
```

**学生**：这和新近度提升有什么区别？

**教授**：
- **新近度提升**：加法，给新记忆额外奖励
- **时间衰减**：乘法，让旧记忆逐渐贬值，但保底 50%

两者结合，确保**新记忆优先，但旧记忆不会完全失效**。


#### 阶段 10-12：质量控制

**教授**：最后三个阶段是**质量把关**：

```typescript
// 10. 硬性最低分过滤
if (score < 0.35) {
  continue;  // 丢弃这条记忆
}
```

**学生**：为什么是 0.35？

**教授**：这是经验值。经过前面所有调整后，分数低于 0.35 的记忆通常是**噪声**。与其注入无关信息污染上下文，不如直接丢弃。

```typescript
// 11. 噪声过滤（src/noise-filter.ts）
function isNoise(text: string): boolean {
  // 拒绝回复
  if (/I don't have|I cannot|我没有|我不能/.test(text)) {
    return true;
  }

  // 元问题
  if (/do you remember|你记得吗/.test(text)) {
    return true;
  }

  // 问候语
  if (/^(hi|hello|HEARTBEAT)$/i.test(text)) {
    return true;
  }

  return false;
}
```

**学生**：为什么要过滤这些？

**教授**：想象一下，如果 AI 的记忆里存了"我不知道你的 API key"，下次用户问 API key 时，这条记忆会被检索出来，但毫无帮助！这是**负面记忆污染**。

```typescript
// 12. MMR 多样性（Maximum Marginal Relevance）
const selected = [];
for (const candidate of sortedCandidates) {
  let maxSimilarity = 0;

  // 计算与已选记忆的相似度
  for (const s of selected) {
    const sim = cosineSimilarity(candidate.vector, s.vector);
    maxSimilarity = Math.max(maxSimilarity, sim);
  }

  // 如果太相似，降权
  if (maxSimilarity > 0.85) {
    candidate.score *= 0.5;
  }

  selected.push(candidate);
}
```

**学生**：这是为了避免返回重复的记忆？

**教授**：正确！如果用户存了 3 条几乎相同的"我喜欢 TypeScript"，我们不应该返回 3 条，而应该返回 1 条，然后给其他主题留空间。这是**多样性优化**。

---

## 第五课：作用域隔离

### 场景：咖啡厅，讨论多用户系统

**学生**：教授，如果多个 AI 助手共享同一个记忆数据库，会不会串台？

**教授**：非常好的问题！这就是**作用域隔离（Scope Isolation）**要解决的问题。

### 作用域的概念

**教授**：看 `src/scopes.ts` 的设计：

```typescript
// 内置作用域模式
const SCOPE_PATTERNS = {
  global: 'global',                    // 全局共享
  agent: 'agent:<agentId>',           // 特定助手私有
  custom: 'custom:<name>',            // 自定义作用域
  project: 'project:<projectId>',     // 项目级别
  user: 'user:<userId>'               // 用户级别
};

// 示例：
// - "global": 所有助手都能访问
// - "agent:main": 只有 main 助手能访问
// - "agent:discord-bot": 只有 discord-bot 能访问
// - "custom:team-shared": 团队共享作用域
```

**学生**：能举个实际场景吗？

**教授**：当然。假设你有两个 AI 助手：

1. **主助手（main）**：帮你写代码、回答问题
2. **Discord 机器人（discord-bot）**：在 Discord 服务器上回复用户

你告诉主助手："我的 GitHub token 是 ghp_xxx"。你**不希望** Discord 机器人知道这个信息，因为它是公开服务器上的机器人。

```typescript
// 配置作用域访问控制
{
  "scopes": {
    "agentAccess": {
      "main": ["global", "agent:main"],
      "discord-bot": ["global", "agent:discord-bot"]
    }
  }
}

// 存储记忆时指定作用域
await memory_store({
  text: "我的 GitHub token 是 ghp_xxx",
  scope: "agent:main",  // 只有 main 能访问
  importance: 0.9
});

await memory_store({
  text: "Discord 服务器规则：禁止广告",
  scope: "global",  // 所有助手都能访问
  importance: 0.7
});
```

**学生**：所以作用域是**访问控制**的机制？

**教授**：完全正确！它确保**隐私隔离**和**记忆分区**。

### 作用域管理器实现

**教授**：看 `src/scopes.ts` 的核心逻辑：

```typescript
export function createScopeManager(config) {
  return {
    // 获取助手可访问的作用域列表
    getAccessibleScopes(agentId: string): string[] {
      const customAccess = config.agentAccess?.[agentId];
      if (customAccess) {
        return customAccess;  // 使用自定义配置
      }

      // 默认：global + 自己的 agent 作用域
      return ['global', `agent:${agentId}`];
    },

    // 获取默认存储作用域
    getDefaultScope(agentId: string): string {
      return config.default || 'global';
    },

    // 验证作用域访问权限
    canAccess(agentId: string, scope: string): boolean {
      const accessible = this.getAccessibleScopes(agentId);
      return accessible.includes(scope);
    }
  };
}
```

**学生**：如果我想让两个助手共享某些记忆，但不是全部，怎么办？

**教授**：使用**自定义作用域**！

```typescript
{
  "scopes": {
    "definitions": {
      "coding-shared": {
        "description": "编程相关的共享知识"
      }
    },
    "agentAccess": {
      "main": ["global", "agent:main", "custom:coding-shared"],
      "code-reviewer": ["global", "agent:code-reviewer", "custom:coding-shared"]
    }
  }
}

// 存储到共享作用域
await memory_store({
  text: "项目使用 TypeScript strict 模式",
  scope: "custom:coding-shared",  // 两个助手都能访问
  importance: 0.8
});
```

**学生**：这样设计的好处是什么？

**教授**：
1. **隐私保护**：敏感信息不会泄露给不该访问的助手
2. **记忆清晰**：每个助手有自己的"私人笔记本"
3. **灵活共享**：可以精确控制哪些记忆共享
4. **扩展性**：支持多租户、多项目场景

---

## 第六课：实战案例

### 场景：实验室，运行实际查询

**教授**：现在让我们跟踪一个完整的查询流程，看看所有组件如何协同工作。

### 案例：用户查询 "我的 TypeScript 配置是什么？"

**学生**：好的，我准备好了！

**教授**：首先，看查询进入系统：

```typescript
// 1. 自适应检索判断
const query = "我的 TypeScript 配置是什么？";
const shouldSkip = shouldSkipRetrieval(query);
// 结果：false（不跳过，因为是有效查询）

// 2. 向量化查询
const queryVector = await embedder.embedQuery(query);
// 调用 Jina API，得到 1024 维向量
// [0.023, -0.145, 0.089, ..., 0.234]
```

**学生**：这一步需要多长时间？

**教授**：通常 50-200ms，取决于网络延迟。这就是为什么我们有**缓存机制**：

```typescript
// embedder.ts 中的缓存
const cache = new LRUCache({
  max: 1000,  // 最多缓存 1000 个 embedding
  ttl: 3600000  // 1 小时过期
});

// 如果查询相同，直接返回缓存
const cacheKey = `query:${text}`;
if (cache.has(cacheKey)) {
  return cache.get(cacheKey);  // 命中缓存，<1ms
}
```

**教授**：继续，进入检索阶段：

```typescript
// 3. 并行检索
const [vectorResults, bm25Results] = await Promise.all([
  // 向量搜索
  store.vectorSearch(queryVector, 20, 0.1, ['global', 'agent:main']),

  // BM25 搜索
  store.bm25Search('TypeScript 配置', 20, ['global', 'agent:main'])
]);

// 向量搜索结果（示例）：
vectorResults = [
  { id: 'mem1', text: '你的 tsconfig.json 使用 strict 模式', score: 0.89 },
  { id: 'mem2', text: 'TypeScript 配置文档：https://...', score: 0.82 },
  { id: 'mem3', text: '项目使用 ES2022 target', score: 0.78 },
  ...
]

// BM25 搜索结果（示例）：
bm25Results = [
  { id: 'mem1', text: '你的 tsconfig.json 使用 strict 模式', score: 8.5 },
  { id: 'mem4', text: 'TypeScript 编译器选项说明', score: 7.2 },
  { id: 'mem5', text: 'TS 配置最佳实践', score: 6.8 },
  ...
]
```

**学生**：我看到 mem1 在两个结果中都出现了！

**教授**：没错！这说明 mem1 既**语义相关**又有**精确关键词匹配**，是高质量结果。

```typescript
// 4. RRF 融合
const fusedResults = fuseResults(vectorResults, bm25Results);

// mem1 的融合分数：
// - 向量排名：1 → RRF = 1/61 = 0.0164
// - BM25 排名：1 → RRF = 1/61 = 0.0164
// - 总分：0.0164 + 0.0164 = 0.0328（最高）

// mem2 的融合分数：
// - 向量排名：2 → RRF = 1/62 = 0.0161
// - BM25 排名：无 → RRF = 0
// - 总分：0.0161（第二）
```

**教授**：现在进入评分管道：

```typescript
// 5. 交叉编码器重排序（假设启用）
const rerankScores = await callRerankerAPI(query, fusedResults);

// Jina Reranker 返回：
rerankScores = [
  { index: 0, score: 0.95 },  // mem1
  { index: 1, score: 0.72 },  // mem2
  { index: 2, score: 0.68 },  // mem3
  ...
]

// 混合分数：
mem1.score = 0.6 × 0.95 + 0.4 × 0.89 = 0.926
mem2.score = 0.6 × 0.72 + 0.4 × 0.82 = 0.760
```

**学生**：重排序提升了 mem1 的分数！

**教授**：对，因为交叉编码器能更精确地判断相关性。继续：

```typescript
// 6-9. 时间与重要性调整
// 假设 mem1 的元数据：
mem1 = {
  text: '你的 tsconfig.json 使用 strict 模式',
  timestamp: Date.now() - 7 * 24 * 3600 * 1000,  // 7 天前
  importance: 0.9,
  length: 45
}

// 6. 新近度提升
const ageDays = 7;
const recencyBoost = Math.exp(-7/14) * 0.1 = 0.061;
mem1.score += 0.061;  // 0.926 → 0.987

// 7. 重要性加权
mem1.score *= (0.7 + 0.3 * 0.9) = 0.97;
// 0.987 × 0.97 = 0.957

// 8. 长度归一化
const lengthPenalty = 1 / (1 + 0.5 * Math.log2(45/500)) = 1.14;
mem1.score *= 1.14;  // 0.957 → 1.09（短文本获得奖励）

// 9. 时间衰减
const timeDecay = 0.5 + 0.5 * Math.exp(-7/60) = 0.94;
mem1.score *= 0.94;  // 1.09 → 1.02
```

**学生**：最终分数超过 1 了？

**教授**：是的！这是允许的。分数只是相对排名的依据，不需要归一化到 0-1。

```typescript
// 10. 硬性最低分过滤
if (mem1.score >= 0.35) {
  // 通过！
}

// 11. 噪声过滤
if (!isNoise(mem1.text)) {
  // 通过！
}

// 12. MMR 多样性
// 假设 mem2 和 mem1 相似度 0.92（很相似）
mem2.score *= 0.5;  // 降权

// 最终返回 Top 3：
results = [
  { text: '你的 tsconfig.json 使用 strict 模式', score: 1.02 },
  { text: '项目使用 ES2022 target', score: 0.85 },
  { text: 'TypeScript 编译器选项说明', score: 0.72 }
]
```

**学生**：整个流程大概需要多长时间？

**教授**：让我们分解：

```
1. 自适应判断：<1ms
2. 向量化查询：50-200ms（或 <1ms 如果缓存命中）
3. 并行检索：20-50ms（LanceDB ANN 很快）
4. RRF 融合：<5ms
5. 交叉编码器：100-300ms（可选，网络调用）
6-12. 评分管道：<10ms

总计：200-600ms（无缓存）或 100-300ms（有缓存）
```

**学生**：这对实时对话来说可以接受吗？

**教授**：完全可以！用户通常感知不到 300ms 的延迟。而且，这是**一次性成本**——检索完成后，AI 就可以基于这些记忆生成回复，不需要重复检索。

---

## 第七课：配置与调优

### 场景：性能优化讨论

**学生**：教授，如果我想针对自己的场景优化这个系统，应该调整哪些参数？

**教授**：非常好的问题！让我们看关键的配置参数。

### 核心配置参数

**教授**：打开 `openclaw.plugin.json`，看配置结构：

```json
{
  "embedding": {
    "model": "jina-embeddings-v5-text-small",
    "dimensions": 1024,
    "taskQuery": "retrieval.query",
    "taskPassage": "retrieval.passage"
  },
  "retrieval": {
    "mode": "hybrid",
    "vectorWeight": 0.7,
    "bm25Weight": 0.3,
    "minScore": 0.3,
    "rerank": "cross-encoder",
    "candidatePoolSize": 20,
    "recencyHalfLifeDays": 14,
    "recencyWeight": 0.1,
    "lengthNormAnchor": 500,
    "hardMinScore": 0.35,
    "timeDecayHalfLifeDays": 60
  }
}
```

### 参数调优指南

**教授**：让我逐一解释如何调优：

#### 1. vectorWeight vs bm25Weight

```
场景A：技术文档检索（精确匹配重要）
vectorWeight: 0.5
bm25Weight: 0.5

场景B：对话记忆（语义理解重要）
vectorWeight: 0.8
bm25Weight: 0.2

场景C：混合场景（默认）
vectorWeight: 0.7
bm25Weight: 0.3
```

**学生**：如何判断我的场景属于哪一类？

**教授**：看你的记忆内容：
- 如果包含大量**专有名词、代码、配置**：提高 bm25Weight
- 如果主要是**自然语言对话、偏好**：提高 vectorWeight

#### 2. candidatePoolSize

```
小数据集（< 1000 条记忆）：
candidatePoolSize: 10

中等数据集（1000-10000 条）：
candidatePoolSize: 20（默认）

大数据集（> 10000 条）：
candidatePoolSize: 50
```

**教授**：更大的候选池意味着：
- ✅ 更高的召回率（不容易遗漏相关记忆）
- ❌ 更高的计算成本（重排序更慢）

#### 3. recencyHalfLifeDays

```
快速迭代项目（配置经常变）：
recencyHalfLifeDays: 7

稳定项目（配置很少变）：
recencyHalfLifeDays: 30

长期知识库（历史同样重要）：
recencyHalfLifeDays: 180
```

**学生**：半衰期是什么意思？

**教授**：半衰期是指**新近度提升减半的时间**。

```
recencyHalfLifeDays = 14：

0 天前：boost = 0.1 × exp(0) = 0.1
14 天前：boost = 0.1 × exp(-1) = 0.037（减半）
28 天前：boost = 0.1 × exp(-2) = 0.014（再减半）
```

#### 4. hardMinScore

```
高精度场景（宁缺毋滥）：
hardMinScore: 0.5

平衡场景（默认）：
hardMinScore: 0.35

高召回场景（尽量多返回）：
hardMinScore: 0.2
```

**教授**：这是**精度-召回率权衡**：
- 高阈值：返回的都很相关，但可能遗漏一些
- 低阈值：返回更多结果，但可能包含噪声

### 性能优化技巧

**学生**：如果系统变慢了，怎么优化？

**教授**：几个方向：

#### 1. 禁用重排序（如果不需要极致精度）

```json
{
  "retrieval": {
    "rerank": "none"  // 节省 100-300ms
  }
}
```

#### 2. 减小候选池

```json
{
  "retrieval": {
    "candidatePoolSize": 10  // 从 20 降到 10
  }
}
```

#### 3. 使用更小的 embedding 模型

```json
{
  "embedding": {
    "model": "text-embedding-3-small",  // 1536 维
    "dimensions": 512  // 降维到 512
  }
}
```

**学生**：降维会损失精度吗？

**教授**：会，但通常很小。实验表明，从 1536 维降到 512 维，精度损失约 2-3%，但速度提升 3 倍。

#### 4. 启用 embedding 缓存

```typescript
// embedder.ts 已经内置了缓存
// 确保相同查询不会重复调用 API

// 如果你的查询模式重复性高，可以增大缓存：
const cache = new LRUCache({
  max: 5000,  // 从 1000 增加到 5000
  ttl: 7200000  // 从 1 小时增加到 2 小时
});
```

### 成本优化

**学生**：这个系统的主要成本在哪里？

**教授**：三个方面：

```
1. Embedding API 调用：
   - Jina: $0.02 / 1M tokens
   - OpenAI: $0.13 / 1M tokens
   - 优化：使用缓存，启用 autoRecall: false

2. Reranker API 调用：
   - Jina: $0.002 / 1K requests
   - 优化：减小 candidatePoolSize 或禁用重排序

3. 存储成本：
   - LanceDB 本地存储，几乎免费
   - 1M 条记忆约 10GB（1024 维向量）
```

**教授**：如果你每天有 1000 次查询：

```
无缓存：
- Embedding: 1000 × $0.02/1M = $0.02/天
- Reranker: 1000 × $0.002/1K = $2/天
- 总计：~$60/月

有缓存（50% 命中率）：
- Embedding: 500 × $0.02/1M = $0.01/天
- Reranker: 500 × $0.002/1K = $1/天
- 总计：~$30/月
```

---

## 总结与展望

### 场景：课程结束，办公室

**学生**：教授，这门课让我对整个系统有了深入理解。能总结一下核心要点吗？

**教授**：当然！让我总结这个项目的**核心设计哲学**：

### 核心设计原则

```
1. 混合检索（Hybrid Retrieval）
   └─ 语义理解（Vector）+ 精确匹配（BM25）= 最佳召回

2. 多阶段评分（Multi-Stage Scoring）
   └─ 粗排（快速筛选）+ 精排（精确排序）= 效率与质量平衡

3. 时间感知（Time-Aware）
   └─ 新近度提升 + 时间衰减 = 动态记忆价值

4. 作用域隔离（Scope Isolation）
   └─ 隐私保护 + 灵活共享 = 多租户支持

5. 自适应优化（Adaptive Optimization）
   └─ 跳过无效查询 + 噪声过滤 = 成本与质量优化
```

**学生**：这个系统还有哪些可以改进的地方？

**教授**：很好的问题！几个研究方向：

### 未来改进方向

```
1. 动态参数调优
   - 根据查询类型自动调整 vectorWeight/bm25Weight
   - 基于用户反馈的强化学习

2. 层次化记忆
   - 短期记忆（会话级）+ 长期记忆（持久化）
   - 自动归档不常用记忆

3. 知识图谱集成
   - 记忆之间的关联关系
   - 基于图的推理检索

4. 多模态支持
   - 图片、代码、表格的向量化
   - 跨模态检索

5. 联邦学习
   - 多用户共享模型，但数据隔离
   - 隐私保护的协同学习
```

**学生**：太感谢了，教授！我现在不仅理解了这个项目，还学到了很多检索系统的通用原理。

**教授**：这正是我希望达到的效果。记住，**好的系统设计不是追求复杂，而是在多个目标之间找到最佳平衡**：

- 精度 vs 召回率
- 速度 vs 质量
- 成本 vs 性能
- 隐私 vs 共享

这个项目在每个权衡点都做出了深思熟虑的选择。现在，去实践吧！

---

## 附录：快速参考

### 关键文件速查

```
index.ts          - 插件入口，生命周期钩子
src/store.ts      - LanceDB 存储层
src/embedder.ts   - Embedding 抽象
src/retriever.ts  - 混合检索引擎
src/scopes.ts     - 作用域管理
src/tools.ts      - Agent 工具定义
cli.ts            - CLI 命令实现
```

### 常用命令

```bash
# 查看记忆统计
openclaw memory-pro stats

# 搜索记忆
openclaw memory-pro search "TypeScript"

# 列出记忆
openclaw memory-pro list --scope global --limit 10

# 导出记忆
openclaw memory-pro export --output backup.json

# 重新嵌入（更换模型后）
openclaw memory-pro reembed --source-db /old/path
```

### 调试技巧

```bash
# 1. 检查插件是否加载
openclaw plugins list | grep memory-lancedb-pro

# 2. 查看插件日志
tail -f ~/.openclaw/logs/gateway.log | grep memory-lancedb-pro

# 3. 测试 embedding API
curl -X POST https://api.jina.ai/v1/embeddings \
  -H "Authorization: Bearer $JINA_API_KEY" \
  -d '{"model":"jina-embeddings-v5-text-small","input":"test"}'

# 4. 清除 jiti 缓存（修改代码后）
rm -rf /tmp/jiti/ && openclaw gateway restart
```

### 性能基准

```
典型性能指标（1000 条记忆）：

向量搜索：10-30ms
BM25 搜索：5-15ms
RRF 融合：<5ms
重排序：100-300ms（网络调用）
总检索时间：200-600ms

内存占用：
- 1000 条记忆：~10MB
- 10000 条记忆：~100MB
- 100000 条记忆：~1GB
```

---

**课程结束。祝学习愉快！**


---

## 第八课：新版本特性（v1.1.0）

### 场景：版本更新讨论会

**学生**：教授，我听说这个项目有了重大更新，从 1.0.23 升级到了 1.1.0-beta.6。有哪些新功能？

**教授**：非常好的问题！这次更新引入了六个核心新功能，让系统具备了**自我改进能力**。让我逐一讲解。

---

### 新功能 1：Memory Reflection（记忆反思）

**学生**：什么是记忆反思？

**教授**：想象一下，AI 助手在每次对话结束时，会**反思这次对话学到了什么**，然后把这些经验总结成规则，在下次对话时自动应用。

```
传统模式：
会话1：用户："记住我喜欢 TypeScript"
会话2：用户："帮我写代码" → AI 可能忘记用 TypeScript

反思模式：
会话1：用户："记住我喜欢 TypeScript"
  → 反思：总结规则 "用户偏好 TypeScript"
会话2：开始时自动注入 <inherited-rules>
  → AI 自动使用 TypeScript
```

**配置示例**：
```json
{
  "memoryReflection": {
    "enabled": true,
    "sessionStrategy": "inheritance+derived",
    "storeToLanceDB": true
  }
}
```

**会话策略**：
- `inheritance-only`: 只继承规则
- `inheritance+derived`: 继承规则 + 派生新见解
- `fixed`: 固定反思内容
- `dynamic`: 动态生成反思

**学生**：这和普通的记忆存储有什么区别？

**教授**：
- **普通记忆**：存储具体事实（"你的 API key 是 xxx"）
- **反思记忆**：存储抽象规则（"用户偏好简洁的代码风格"）

反思记忆是**元认知**层面的，帮助 AI 理解"如何更好地服务用户"。

---

### 新功能 2：Self-Improvement Governance（自我改进治理）

**教授**：这是一个**结构化的学习框架**，让 AI 系统能够系统地记录和回顾学习内容。

**核心工具**：

#### 1. `self_improvement_log` - 记录学习

```typescript
// 记录一个学习经验
await self_improvement_log({
  type: "learning",
  title: "BM25 权重调优经验",
  content: "在技术文档检索场景下，bm25Weight 应该提高到 0.5，因为精确关键词匹配更重要",
  tags: ["retrieval", "tuning", "bm25"]
});

// 记录一个错误
await self_improvement_log({
  type: "error",
  title: "忘记清除 jiti 缓存",
  content: "修改 TypeScript 代码后必须运行 rm -rf /tmp/jiti/，否则加载的是旧代码",
  tags: ["deployment", "cache", "jiti"]
});
```

#### 2. `self_improvement_review` - 回顾积压

```typescript
const summary = await self_improvement_review();
// 返回：
// - LEARNINGS.md 中的所有学习条目
// - ERRORS.md 中的所有错误记录
// - 按标签分类的统计
```

#### 3. `self_improvement_extract_skill` - 提取技能

```typescript
await self_improvement_extract_skill({
  skillName: "optimize-retrieval",
  description: "根据场景自动优化检索参数",
  learningIds: ["learning-001", "learning-005"]
});
// 生成可复用的技能脚本
```

**文件结构**：
```
.learnings/
├── LEARNINGS.md       # 学习日志
├── ERRORS.md          # 错误日志
└── skills/
    └── optimize-retrieval.md
```

**学生**：这和 Iron Rules 有什么关系？

**教授**：Iron Rules 是**人工编写的规则**，而 Self-Improvement Governance 是**AI 自动积累的经验**。两者结合：

```
Iron Rules（人工）：
- Rule 5: 修改 .ts 文件后必须清除 jiti 缓存

Self-Improvement（AI 学习）：
- Error-001: 2026-03-10 忘记清除缓存导致加载旧代码
- Learning-015: 发现可以用脚本自动化这个步骤
```

---

### 新功能 3：Access Reinforcement（访问强化）

**教授**：这是基于**间隔重复**原理的记忆强化机制，类似 Anki 记忆卡片。

**核心思想**：
- 频繁被手动召回的记忆 → 更重要 → 衰减更慢
- 很少被访问的记忆 → 不重要 → 正常衰减

**数学模型**：
```typescript
// 原始时间衰减半衰期
const baseHalfLife = 60;  // 60 天

// 手动召回次数
const manualRecallCount = 5;

// 强化因子（配置）
const reinforcementFactor = 0.5;

// 最大半衰期倍数（配置）
const maxMultiplier = 3;

// 计算有效半衰期
const effectiveHalfLife = baseHalfLife * (
  1 + reinforcementFactor * Math.min(manualRecallCount, maxMultiplier)
);

// 结果：60 × (1 + 0.5 × 3) = 60 × 2.5 = 150 天
```

**配置示例**：
```json
{
  "retrieval": {
    "reinforcementFactor": 0.5,      // 0-2 范围
    "maxHalfLifeMultiplier": 3       // 1-10 范围
  }
}
```

**学生**：为什么要限制 `maxMultiplier`？

**教授**：防止**过度强化**。如果一个记忆被召回 100 次，半衰期变成 6000 天（16 年），这不合理。限制在 3 倍意味着最多 180 天，这是合理的长期记忆周期。

**实际效果对比**：
```
记忆A：从未手动召回
- 半衰期：60 天
- 60 天后权重：0.5×

记忆B：手动召回 5 次
- 半衰期：150 天
- 60 天后权重：0.76×（衰减更慢）
```

---

### 新功能 4：Markdown Mirror（Markdown 镜像）

**教授**：这个功能将记忆**双写**到人类可读的 Markdown 文件。

**为什么需要？**

**学生**：LanceDB 不是已经存储了吗？

**教授**：LanceDB 存储的是**二进制向量**，人类无法直接阅读。Markdown Mirror 提供：

1. **人工审查**：可以直接打开文件查看记忆内容
2. **版本控制**：可以用 Git 追踪记忆变化
3. **备份迁移**：纯文本格式，易于备份和迁移
4. **调试**：快速定位问题记忆

**文件格式**：
```markdown
# 2026-03-12

## [fact] 你的 TypeScript 配置
- **Scope**: agent:main
- **Importance**: 0.9
- **Timestamp**: 2026-03-12T10:30:00Z
- **ID**: mem-abc123

你的 tsconfig.json 使用 strict 模式，target 为 ES2022。

---

## [preference] 代码风格偏好
- **Scope**: global
- **Importance**: 0.7
- **Timestamp**: 2026-03-12T14:20:00Z
- **ID**: mem-def456

我喜欢使用 TypeScript 而不是 JavaScript，函数名使用 camelCase。
```

**配置示例**：
```json
{
  "mdMirror": {
    "enabled": true,
    "dir": "~/.openclaw/memory/markdown"
  }
}
```

**学生**：这会不会影响性能？

**教授**：影响很小。写入 Markdown 是**异步操作**，不阻塞主流程。而且文件 I/O 比网络 API 调用快得多。

---

### 新功能 5：Long Context Chunking（长上下文分块）

**教授**：这个功能解决了**超长文本无法嵌入**的问题。

**问题场景**：
```
用户想存储一篇 5000 字的技术文档摘要
↓
调用 embedding API
↓
错误："Input length exceeds context length (max 8192 tokens)"
```

**解决方案**：
```
5000 字文档
↓
自动检测超长
↓
在句子边界分块：
- 块1: 0-2000 字（重叠 200 字）
- 块2: 1800-3800 字（重叠 200 字）
- 块3: 3600-5000 字
↓
分别嵌入：
- 块1 → [0.2, 0.5, 0.8, ...]
- 块2 → [0.3, 0.4, 0.7, ...]
- 块3 → [0.25, 0.45, 0.75, ...]
↓
平均向量：
[(0.2+0.3+0.25)/3, (0.5+0.4+0.45)/3, ...]
= [0.25, 0.45, 0.77, ...]
↓
存储到 LanceDB
```

**为什么要重叠？**

**学生**：重叠 200 字有什么用？

**教授**：防止**语义断裂**。如果一个重要句子刚好被切在边界上：

```
无重叠：
块1: "...TypeScript 配置应该使用"
块2: "strict 模式以确保类型安全..."
→ 两个块都不完整

有重叠：
块1: "...TypeScript 配置应该使用 strict 模式..."
块2: "...使用 strict 模式以确保类型安全..."
→ 两个块都包含完整信息
```

**配置示例**：
```json
{
  "embedding": {
    "chunking": true,
    "chunkOverlap": 200
  }
}
```

---

### 新功能 6：Generic Auto-Recall Selection（通用自动召回选择）

**教授**：这是对自动召回算法的优化，提供两种选择模式。

**模式对比**：

#### MMR 模式（原有）
```typescript
// 直接截断 + MMR 多样性
const candidates = await retrieve(query, 20);
const selected = mmrDiversity(candidates, 3);
// 返回 Top 3
```

#### SetWise-v2 模式（新增）
```typescript
// 集合式最终选择器
const candidates = await retrieve(query, 20);

// 第一步：词汇去重
const lexicalUnique = deduplicateLexical(candidates);

// 第二步：语义去重
const semanticUnique = deduplicateSemantic(lexicalUnique);

// 第三步：集合式选择（优化覆盖率和多样性）
const selected = setwiseSelect(semanticUnique, 3);
// 返回 Top 3
```

**配置示例**：
```json
{
  "autoRecallSelectionMode": "setwise-v2"  // 或 "mmr"
}
```

**学生**：什么时候用哪个模式？

**教授**：
- **MMR 模式**：速度优先，适合实时对话
- **SetWise-v2 模式**：质量优先，适合复杂查询

**性能对比**：
```
MMR 模式：
- 耗时：<5ms
- 多样性：中等
- 覆盖率：中等

SetWise-v2 模式：
- 耗时：10-20ms
- 多样性：高
- 覆盖率：高
```

---

### 版本升级实战

**学生**：如果我想升级到新版本，应该怎么做？

**教授**：让我给你一个完整的升级流程：

#### 步骤 1：备份现有数据
```bash
# 导出所有记忆
openclaw memory-pro export --output backup-$(date +%Y%m%d).json

# 备份配置
cp ~/.openclaw/openclaw.json ~/.openclaw/openclaw.json.backup
```

#### 步骤 2：更新代码
```bash
cd /path/to/memory-lancedb-pro
git pull origin main
npm install
```

#### 步骤 3：清除缓存（重要！）
```bash
rm -rf /tmp/jiti/
openclaw gateway restart
```

#### 步骤 4：验证版本
```bash
openclaw plugins info memory-lancedb-pro
# 应该显示：version: 1.1.0-beta.6
```

#### 步骤 5：启用新功能（可选）
```json
{
  "plugins": {
    "entries": {
      "memory-lancedb-pro": {
        "config": {
          "memoryReflection": {
            "enabled": true,
            "sessionStrategy": "inheritance+derived"
          },
          "mdMirror": {
            "enabled": true
          },
          "retrieval": {
            "reinforcementFactor": 0.5,
            "maxHalfLifeMultiplier": 3
          },
          "autoRecallSelectionMode": "setwise-v2"
        }
      }
    }
  }
}
```

#### 步骤 6：测试新功能
```bash
# 测试记忆反思
openclaw chat "测试反思功能"

# 测试 Markdown 镜像
ls ~/.openclaw/memory/markdown/

# 测试自我改进
# 在对话中使用 self_improvement_log 工具
```

---

### 配置建议

**教授**：根据不同场景，我推荐不同的配置策略。

#### 保守配置（稳定优先）
```json
{
  "memoryReflection": {
    "enabled": false  // Beta 功能，暂不启用
  },
  "mdMirror": {
    "enabled": true   // 低风险，建议启用
  },
  "retrieval": {
    "reinforcementFactor": 0.3,      // 较低的强化
    "maxHalfLifeMultiplier": 2       // 保守的倍数
  },
  "autoRecallSelectionMode": "mmr"   // 使用成熟的算法
}
```

#### 平衡配置（推荐）
```json
{
  "memoryReflection": {
    "enabled": true,
    "sessionStrategy": "inheritance-only"  // 只继承，不派生
  },
  "mdMirror": {
    "enabled": true
  },
  "retrieval": {
    "reinforcementFactor": 0.5,
    "maxHalfLifeMultiplier": 3
  },
  "autoRecallSelectionMode": "setwise-v2"
}
```

#### 激进配置（功能优先）
```json
{
  "memoryReflection": {
    "enabled": true,
    "sessionStrategy": "dynamic"  // 动态反思
  },
  "mdMirror": {
    "enabled": true
  },
  "retrieval": {
    "reinforcementFactor": 0.8,      // 强力强化
    "maxHalfLifeMultiplier": 5       // 更长的半衰期
  },
  "autoRecallSelectionMode": "setwise-v2",
  "embedding": {
    "chunking": true,
    "chunkOverlap": 300  // 更大的重叠
  }
}
```

---

### 已知问题与注意事项

**学生**：新版本有什么需要注意的吗？

**教授**：有几个重要的注意事项：

#### 1. Beta 版本稳定性
```
⚠️ 1.1.0-beta.6 是测试版本
- 可能存在未发现的 bug
- API 可能在正式版中变化
- 建议在非生产环境测试
```

#### 2. 性能影响
```
启用所有新功能后：
- 内存使用：+10-20%（Markdown 镜像缓存）
- CPU 使用：+5-10%（反思计算）
- 磁盘使用：+50%（Markdown 文件）
```

#### 3. 配置复杂度
```
新增配置项：15+
- 需要仔细阅读文档
- 建议从默认配置开始
- 逐步启用新功能
```

#### 4. 向后兼容性
```
✅ 配置向后兼容
✅ 数据格式兼容
✅ API 接口兼容
⚠️ 新功能需要手动启用
```

---

### 总结

**学生**：这次更新真的很大！能总结一下核心价值吗？

**教授**：当然。这次更新的核心主题是**自我改进**：

```
1.0.x 版本：
- 存储记忆
- 检索记忆
- 管理记忆

1.1.0 版本：
- 存储记忆
- 检索记忆
- 管理记忆
+ 反思记忆（从经验中学习）
+ 强化记忆（重要的记得更久）
+ 治理记忆（系统化积累知识）
+ 镜像记忆（人类可读备份）
+ 分块记忆（处理长文本）
+ 优化选择（更好的召回算法）
```

**核心价值**：
1. **自我改进能力**：AI 可以从过去的错误中学习
2. **长期记忆强化**：重要信息保留更久
3. **人类可读性**：Markdown 镜像便于审查
4. **长文本支持**：不再受限于 token 限制
5. **更好的检索**：优化的选择算法

**学生**：太棒了！我现在完全理解这次更新的意义了。

**教授**：记住，**好的系统不仅能存储信息，还能从信息中学习和进化**。这就是 1.1.0 版本的核心理念。

---

**第八课结束。**

