# Go 重构参考资源清单

## 一、主要参考：本地 Memory LanceDB Pro

### 1.1 核心算法文件

**文件：`../memory-lancedb-pro-main/src/retriever.ts`**

这是最重要的文件，包含所有核心算法：

#### RRF 融合算法（第 200-220 行左右）
```typescript
function rrfFusion(
  vectorResults: SearchResult[],
  bm25Results: SearchResult[],
  k = 60
): Map<string, number> {
  const scores = new Map<string, number>();

  vectorResults.forEach((result, rank) => {
    const currentScore = scores.get(result.id) || 0;
    scores.set(result.id, currentScore + 1 / (rank + k));
  });

  bm25Results.forEach((result, rank) => {
    const currentScore = scores.get(result.id) || 0;
    scores.set(result.id, currentScore + 1 / (rank + k));
  });

  return scores;
}
```

**Go 实现参考**：
```go
func RRFFusion(vectorResults, bm25Results []SearchResult, k int) map[string]float64 {
    scores := make(map[string]float64)

    for rank, result := range vectorResults {
        scores[result.ID] += 1.0 / float64(rank+k)
    }

    for rank, result := range bm25Results {
        scores[result.ID] += 1.0 / float64(rank+k)
    }

    return scores
}
```

#### 新近度衰减（第 300 行左右）
```typescript
const recencyBoost = Math.exp(-daysSince / recencyHalfLifeDays) * recencyWeight;
finalScore = baseScore * (1 + recencyBoost);
```

**公式**：`boost = e^(-天数/半衰期) × 权重`

#### 重要性加权（第 320 行左右）
```typescript
const importanceMultiplier = 0.7 + 0.3 * entry.importance;
finalScore = baseScore * importanceMultiplier;
```

**公式**：`分数 = 基础分 × (0.7 + 0.3 × 重要性)`

#### 长度归一化（第 340 行左右）
```typescript
const lengthRatio = entry.text.length / lengthNormAnchor;
const lengthPenalty = lengthRatio > 1 ? 1 / Math.sqrt(lengthRatio) : 1;
finalScore = baseScore * lengthPenalty;
```

**公式**：
- 如果文本长度 > 锚点：`惩罚 = 1 / √(长度比)`
- 否则：不惩罚

---

### 1.2 噪声过滤

**文件：`../memory-lancedb-pro-main/src/noise-filter.ts`**

```typescript
const DENIAL_PATTERNS = [
  /i don'?t have (any )?(information|data|memory|record)/i,
  /i'?m not sure about/i,
  /i don'?t recall/i,
  // ...
];

const META_QUESTION_PATTERNS = [
  /\bdo you (remember|recall|know about)\b/i,
  /\bcan you (remember|recall)\b/i,
  // ...
];
```

**Go 实现**：直接翻译正则表达式即可

---

### 1.3 作用域管理

**文件：`../memory-lancedb-pro-main/src/scopes.ts`**

```typescript
class MemoryScopeManager {
  getAccessibleScopes(agentId: string): string[] {
    return ["global", `agent:${agentId}`];
  }

  isAccessible(scope: string, agentId: string): boolean {
    return this.getAccessibleScopes(agentId).includes(scope);
  }
}
```

**Go 实现**：纯逻辑，直接翻译

---

## 二、补充参考：成熟的 Go 项目

### 2.1 向量检索相关

#### 项目1：Weaviate Go Client
- 仓库：`github.com/weaviate/weaviate-go-client`
- 用途：参考向量数据结构和接口设计
- 不需要：它是客户端，不是实现

#### 项目2：Milvus Go SDK
- 仓库：`github.com/milvus-io/milvus-sdk-go`
- 用途：参考向量操作的 API 设计
- 不需要：同样是客户端

#### 项目3：Go Vector（推荐）
- 仓库：`github.com/viterin/vek`
- 用途：向量数学运算库
- **重点**：余弦相似度、点积、归一化等实现

```go
// 参考 vek 的实现
import "github.com/viterin/vek/vek32"

// 余弦相似度
similarity := vek32.CosineSimilarity(vec1, vec2)

// 点积
dot := vek32.Dot(vec1, vec2)

// L2 归一化
normalized := vek32.Unit(vec)
```

### 2.2 SQLite 相关

#### 项目：modernc.org/sqlite（必用）
- 仓库：`modernc.org/sqlite`
- 用途：纯 Go 的 SQLite 实现
- **重点**：FTS5 全文检索

```go
import (
    "database/sql"
    _ "modernc.org/sqlite"
)

db, _ := sql.Open("sqlite", "memory.db")

// FTS5 全文检索
db.Exec(`CREATE VIRTUAL TABLE fts USING fts5(content)`)
rows, _ := db.Query(`SELECT * FROM fts WHERE fts MATCH ?`, query)
```

### 2.3 数学计算

#### 项目：Gonum（推荐）
- 仓库：`gonum.org/v1/gonum`
- 用途：科学计算库
- **重点**：矩阵运算、统计函数

```go
import "gonum.org/v1/gonum/floats"

// 点积
dot := floats.Dot(vec1, vec2)

// 归一化
norm := floats.Norm(vec, 2)
floats.Scale(1/norm, vec)
```

---

## 三、关键算法实现参考

### 3.1 余弦相似度（必须实现）

**数学公式**：
```
cos(A, B) = (A · B) / (||A|| × ||B||)
```

**Go 实现（纯手写）**：
```go
func CosineSimilarity(a, b []float32) float32 {
    if len(a) != len(b) {
        return 0
    }

    var dot, normA, normB float32
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }

    if normA == 0 || normB == 0 {
        return 0
    }

    return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
```

**优化版（使用 Gonum）**：
```go
import "gonum.org/v1/gonum/floats"

func CosineSimilarity(a, b []float64) float64 {
    dot := floats.Dot(a, b)
    normA := floats.Norm(a, 2)
    normB := floats.Norm(b, 2)
    return dot / (normA * normB)
}
```

### 3.2 RRF 融合（直接参考 LanceDB Pro）

**位置**：`../memory-lancedb-pro-main/src/retriever.ts` 第 200 行

**公式**：
```
score(d) = Σ 1 / (k + rank_i(d))
```

其中：
- `d` 是文档
- `k` 是常数（通常是 60）
- `rank_i(d)` 是文档在第 i 个检索结果中的排名

### 3.3 BM25（使用 SQLite FTS5）

**不需要自己实现**，SQLite FTS5 内置了 BM25 算法。

```sql
-- 创建 FTS5 表
CREATE VIRTUAL TABLE fts_memories USING fts5(
    memory_id,
    content,
    tokenize='unicode61'  -- 支持中文
);

-- BM25 检索（自动使用 BM25 算法）
SELECT memory_id, rank
FROM fts_memories
WHERE fts_memories MATCH '搜索关键词'
ORDER BY rank;
```

### 3.4 IVF 索引（可选，性能优化）

**参考项目**：Faiss 的 IVF 实现思路

**核心思想**：
1. K-means 聚类：将向量分成 N 个桶
2. 搜索时：只在最近的几个桶中搜索

**Go 实现参考**：
```go
// 1. K-means 聚类（可以用 gonum/stat/cluster）
import "gonum.org/v1/gonum/stat/cluster"

// 2. 构建倒排索引
type IVFIndex struct {
    centroids [][]float32
    buckets   map[int][]string  // 桶ID -> 记忆ID列表
}

// 3. 搜索
func (idx *IVFIndex) Search(query []float32, nprobe int) []string {
    // 找到最近的 nprobe 个桶
    nearestBuckets := idx.findNearestCentroids(query, nprobe)

    // 只在这些桶中搜索
    candidates := []string{}
    for _, bucketID := range nearestBuckets {
        candidates = append(candidates, idx.buckets[bucketID]...)
    }

    return candidates
}
```

---

## 四、开发路线图

### 阶段1：基础功能（参考 LanceDB Pro）
- [ ] 数据模型：参考 `store.ts`
- [ ] 向量检索：实现余弦相似度
- [ ] 全文检索：使用 SQLite FTS5
- [ ] RRF 融合：参考 `retriever.ts`

### 阶段2：评分管道（参考 LanceDB Pro）
- [ ] 新近度衰减：参考 `retriever.ts` 第 300 行
- [ ] 重要性加权：参考 `retriever.ts` 第 320 行
- [ ] 长度归一化：参考 `retriever.ts` 第 340 行
- [ ] 噪声过滤：参考 `noise-filter.ts`

### 阶段3：高级功能
- [ ] 作用域管理：参考 `scopes.ts`
- [ ] 自适应检索：参考 `adaptive-retrieval.ts`
- [ ] MCP Tools：参考 `tools.ts`

### 阶段4：性能优化（可选）
- [ ] IVF 索引：参考 Faiss 思路
- [ ] 并行计算：使用 goroutine
- [ ] 缓存优化

---

## 五、推荐的 Go 依赖

```go
// go.mod
module your-project

go 1.22

require (
    modernc.org/sqlite v1.29.0           // SQLite 数据库
    github.com/sashabaranov/go-openai v1.20.0  // OpenAI API
    gonum.org/v1/gonum v0.14.0           // 数学计算（可选）
    github.com/viterin/vek v0.4.2        // 向量运算（可选）
    github.com/spf13/cobra v1.8.0        // CLI 工具
    github.com/spf13/viper v1.18.0       // 配置管理
    go.uber.org/zap v1.26.0              // 日志
)
```

---

## 六、总结

**主要参考**：
1. ✅ 本地 `../memory-lancedb-pro-main` - 所有算法都在这里
2. ✅ `github.com/viterin/vek` - 向量数学运算
3. ✅ `modernc.org/sqlite` - 数据库
4. ✅ `gonum.org/v1/gonum` - 科学计算（可选）

**不需要参考**：
- ❌ LanceDB 源码（太复杂，而且是 Rust）
- ❌ 其他向量数据库（过度设计）

**开发策略**：
1. 先照着 LanceDB Pro 的 TypeScript 代码翻译成 Go
2. 遇到数学计算用 `vek` 或 `gonum`
3. 数据库用 `modernc.org/sqlite`
4. 其他都是纯逻辑，直接翻译

