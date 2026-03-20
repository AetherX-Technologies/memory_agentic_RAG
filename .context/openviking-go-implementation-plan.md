# OpenViking 分层检索 Go 实现计划（v0.3 - 最终版）

> 创建时间：2026-03-18
> 最后更新：2026-03-18（根据 Codex 第 2 轮审查反馈修正）
> 状态：✅ 已通过审查，可以开始实现
> 目标：将 OpenViking 的分层检索思想整合到现有 Go 项目中

---

## 一、背景与目标

### 1.1 当前状态

**已完成**（M1-M7）：
- ✅ SQLite 存储层（memories + vectors + fts_memories）
- ✅ 向量检索（余弦相似度）
- ✅ BM25 全文检索（FTS5）
- ✅ RRF 融合算法
- ✅ 6 阶段评分管道（新近度/重要性/长度归一化）
- ✅ HTTP API（5 个端点）
- ✅ Rerank 可选功能

**问题**：
- 当前实现是扁平的全局检索，没有层次结构
- 大文档（如 100MB 教学教案）作为单个节点存储，检索粒度粗
- 缺乏 OpenViking 的分层搜索能力

### 1.2 目标

**整合 OpenViking 核心特性**：
1. **文档拆分**：大文档解析时拆分成多个独立节点
2. **L0/L1/L2 三层表示**：每个节点有摘要/概览/完整内容
3. **层次路径**：基于文件系统路径构建树结构
4. **分层检索**：递归搜索 + 分数传播
5. **按需加载**：检索返回 L0/L1，完整内容按需获取

**非目标**（暂不实现）：
- ❌ AGFS 文件系统（使用 SQLite 存储即可）
- ❌ 完整的 VikingFS URI 系统（简化为 hierarchy 字段）
- ❌ 多租户权限管理（保留现有 scope 机制）

---

## 二、核心设计

### 2.1 数据模型改造

**现有表结构**：
```sql
CREATE TABLE memories (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    category TEXT,
    scope TEXT DEFAULT 'global',
    importance REAL DEFAULT 0.5,
    created_at INTEGER,
    accessed_at INTEGER,
    access_count INTEGER DEFAULT 0
);

CREATE TABLE vectors (
    memory_id TEXT PRIMARY KEY,
    vector BLOB NOT NULL,
    FOREIGN KEY (memory_id) REFERENCES memories(id)
);
```

**新增字段**（向 OpenViking 对齐，已根据 Codex 反馈修正）：
```sql
-- 核心修正：L0/L1/L2 是同一节点的不同表示，不是不同节点
-- 删除了 level 字段，避免混淆

ALTER TABLE memories ADD COLUMN abstract TEXT;
-- L0: 摘要（~100 tokens），用于快速预览

ALTER TABLE memories ADD COLUMN overview TEXT;
-- L1: 概览（~500 tokens），用于向量检索的主要依据

-- content 字段保持不变，作为 L2: 完整内容

ALTER TABLE memories ADD COLUMN parent_id TEXT;
-- 父节点 ID（构建树结构）

ALTER TABLE memories ADD COLUMN hierarchy TEXT;
-- 层次路径，如 "/project/src/auth"

ALTER TABLE memories ADD COLUMN node_type TEXT DEFAULT 'chunk';
-- 'directory' | 'file' | 'chunk'

ALTER TABLE memories ADD COLUMN source_file TEXT;
-- 原始文件路径，用于追溯来源

ALTER TABLE memories ADD COLUMN chunk_index INTEGER DEFAULT 0;
-- 拆分后的序号（0 表示未拆分或目录节点）

ALTER TABLE memories ADD COLUMN token_count INTEGER;
-- Token 数量，用于拆分决策

ALTER TABLE memories ADD COLUMN metadata TEXT;
-- JSON 格式的元数据，如：{"file_type": "markdown", "author": "user1", "tags": ["tutorial"]}

ALTER TABLE memories ADD CONSTRAINT fk_parent
    FOREIGN KEY (parent_id) REFERENCES memories(id) ON DELETE SET NULL;
```

**索引优化**：
```sql
CREATE INDEX idx_parent_id ON memories(parent_id);
CREATE INDEX idx_hierarchy ON memories(hierarchy);
CREATE INDEX idx_node_type ON memories(node_type);
CREATE INDEX idx_source_file ON memories(source_file);
CREATE INDEX idx_chunk_index ON memories(source_file, chunk_index);
```

**关键设计说明**：
- **每个节点只有一个向量**：由 L1 (overview) 生成，存入 vectors 表
- **检索流程**：用 L1 向量匹配 → 返回 L0 预览 → 按需加载 L2 完整内容
- **不再有 level 字段**：避免"L0 节点"、"L1 节点"、"L2 节点"的混淆

### 2.2 文档拆分策略

**参考 OpenViking 源码**（`openviking/parse/parsers/markdown.py`），已根据 Codex 反馈调整阈值：

**拆分配置**（已优化）：
```go
type SplitterConfig struct {
    MaxChunkSize int  // 512 tokens（降低，提高检索粒度）
    MinChunkSize int  // 256 tokens（降低）
    OverlapSize  int  // 50 tokens（新增：chunk 之间的重叠，避免语义截断）
}
```

**拆分逻辑**：
1. **按标题拆分**：识别 Markdown 标题（# ## ###）
2. **处理无标题文档**：如果没有标题，按段落拆分
3. **递归拆分大章节**：超过 `MaxChunkSize` 的章节继续拆分，保留 `OverlapSize` 重叠
4. **小章节合并**：少于 `MinChunkSize` 的章节合并到前一个
5. **生成独立节点**：每个拆分后的部分成为独立的 `memory` 记录

**边界情况处理**：
- 代码块、表格不在中间截断
- 中文/英文混合文本的 token 计算（中文 1 字符 ≈ 2-3 tokens）
- 标题嵌套层级过深时的处理

**示例**：
```
输入：OpenViking_教学教案.md（100MB，2589 行）

拆分后：
- /教学教案/第一部分_问题的起源 (node_type=chunk, chunk_index=0)
- /教学教案/第一部分_问题的起源 (node_type=chunk, chunk_index=1)
- /教学教案/第二部分_核心创新 (node_type=chunk, chunk_index=0)
- ...
- /教学教案 (node_type=directory, 目录节点)

每个节点都有：
- content (L2): 完整内容
- overview (L1): 概览，用于向量检索
- abstract (L0): 摘要，用于预览
```

### 2.3 L0/L1/L2 生成策略

**关键修正**（根据 Codex 反馈）：
- **每个节点只有一个向量**，由 L1 生成
- **检索流程**：L1 向量匹配 → 返回 L0 预览 → 按需加载 L2

**L2（完整内容）**：
- 直接存储拆分后的文本
- 存储在 `memories.content` 字段

**L0（摘要，~50 字）**：
- 使用 LLM 生成简短摘要
- 存储在 `memories.abstract` 字段
- **改进的 Prompt**：
```
请用一句话（不超过 50 字）概括以下内容的核心主题。
要求：
1. 突出关键信息和主要观点
2. 使用陈述句，不要使用疑问句
3. 不要包含"本文"、"这段内容"等元指称

内容：
{content}

摘要：
```

**L1（概览，200-500 字）**：
- 使用 LLM 生成结构化概览
- 存储在 `memories.overview` 字段
- **改进的 Prompt**：
```
请为以下内容生成结构化概览（200-500 字）。

格式要求：
1. 核心主题：[一句话说明主题]
2. 主要内容：[3-5 个要点，每个要点一行]
3. 关键信息：[重要的数据、结论或观点]

内容：
{content}

概览：
```

**向量化策略**（已修正）：
- **只对 L1 (overview) 向量化** → 存入 vectors 表
- L0 和 L2 不单独向量化
- 检索时使用 L1 向量，返回 L0 预览

**批量处理优化**：
- 使用缓存避免重复生成（基于内容 hash）
- 批量调用 LLM（每次最多 10 个）
- 降级策略：LLM 失败时使用规则提取（前 N 句话）

### 2.4 分层检索算法

**核心流程**（已根据 Codex 反馈改进）：

```
策略 1：全局搜索（支持扁平文档）
- 对所有节点进行向量搜索（不限制 node_type）
- 获取 Top-50 候选

策略 2：分层递归搜索（利用层次结构）
- 从 Top-10 候选开始递归
- 搜索子节点，计算分数传播
- 使用优先队列 + 剪枝策略

策略 3：结果融合
- RRF 融合全局搜索和分层搜索结果
- 按 source_file 聚合（同一文件的 chunk 合并）

策略 4：返回结果
- 只返回 id, abstract, score
- 不返回完整 content，按需加载
```

**分数传播公式**（已优化）：
```go
const ScorePropagationAlpha = 0.7  // 提高向量分数权重
const MaxDepth = 5                 // 最大递归深度
const MinScore = 0.3               // 最小分数阈值

// 考虑路径衰减
depthDecay := math.Pow(0.9, float64(depth))
finalScore := alpha * vectorScore + (1-alpha) * parentScore * depthDecay
```

**剪枝策略**：
1. 深度限制：depth >= MaxDepth 时停止
2. 分数阈值：score < MinScore 时跳过
3. 去重：visited 集合避免重复访问

**优先队列实现**：
```go
type SearchNode struct {
    URI   string
    Score float64
    Depth int     // 新增：记录深度
}

// 使用 container/heap 实现最大堆
```

---

## 三、实现计划

### 3.1 阶段划分（已根据 Codex 反馈调整时间）

**Phase 1: 数据模型改造**（3 天）
- [ ] 修改 SQLite schema（增加 parent_id, hierarchy, source_file 等字段）
- [ ] 更新 `internal/store/models.go` 数据结构
- [ ] 实现数据迁移脚本
- [ ] 迁移验证测试
- [ ] 单元测试

**Phase 2: 文档解析器**（4 天）
- [ ] 实现 Markdown 解析器（`internal/parser/markdown.go`）
- [ ] 实现拆分逻辑（按标题、按段落，带 overlap）
- [ ] 处理边界情况（无标题、代码块、表格）
- [ ] 实现层次路径生成
- [ ] 单元测试 + 边界测试

**Phase 3: L0/L1 生成器**（4 天，可与 Phase 2 并行）
- [ ] 实现 LLM 客户端封装（`internal/generator/llm_client.go`）
- [ ] 实现 L0/L1 生成（改进 prompt）
- [ ] 批量处理 + 缓存机制
- [ ] 错误重试 + 降级策略（规则提取）
- [ ] 单元测试

**Phase 4: 分层检索引擎**（6 天）
- [ ] 实现全局搜索策略（`internal/retrieval/hierarchical.go`）
- [ ] 实现递归搜索算法（优先队列 + 剪枝）
- [ ] 实现分数传播（考虑深度衰减）
- [ ] 实现结果聚合（按 source_file）
- [ ] 性能优化（索引、缓存）
- [ ] 性能测试（10000 条 < 300ms 冷启动，< 100ms 热缓存）

**Phase 5: API 集成**（3 天）
- [ ] 修改 POST /api/memories（支持文档拆分）
- [ ] 修改 GET /api/memories/search（支持分层检索）
- [ ] 新增 GET /api/memories/:id/content（按需加载完整内容）
- [ ] 实现 API 版本兼容（v1 返回完整 content，v2 只返回 abstract）
- [ ] 更新 API 文档

**Phase 6: 测试与优化**（4 天）
- [ ] 端到端测试（存储大文档 → 分层检索）
- [ ] 性能基准对比（与现有系统对比）
- [ ] 代码审查（Codex）
- [ ] 文档和示例更新

**总计**：24 天（约 5 周）

### 3.2 文件结构

```
internal/
├── parser/
│   ├── markdown.go          # Markdown 解析器
│   ├── splitter.go          # 文档拆分逻辑
│   └── hierarchy.go         # 层次路径生成
├── generator/
│   ├── summary.go           # L0/L1 生成器
│   └── llm_client.go        # LLM API 调用
├── retrieval/
│   ├── hierarchical.go      # 分层检索引擎
│   ├── score_propagation.go # 分数传播
│   └── priority_queue.go    # 优先队列
└── store/
    ├── schema_v2.go         # 新数据模型
    └── migration.go         # 数据迁移
```

---

## 四、关键技术细节

### 4.1 文档拆分算法（已改进）

```go
type SplitterConfig struct {
    MaxChunkSize int  // 512 tokens（已降低）
    MinChunkSize int  // 256 tokens（已降低）
    OverlapSize  int  // 50 tokens（新增）
}

type Splitter struct {
    config SplitterConfig
}

func (s *Splitter) SplitMarkdown(content string, basePath string) []Section {
    // 1. 按标题拆分
    sections := parseHeadings(content)

    // 2. 处理无标题文档
    if len(sections) == 0 {
        sections = splitByParagraph(content, s.config.MaxChunkSize)
    }

    // 3. 递归拆分大章节（带重叠）
    result := []Section{}
    for _, sec := range sections {
        if tokenCount(sec.Content) > s.config.MaxChunkSize {
            // 按段落拆分，保留 overlap
            chunks := splitWithOverlap(sec.Content, s.config.MaxChunkSize, s.config.OverlapSize)
            for i, chunk := range chunks {
                result = append(result, Section{
                    Content: chunk,
                    Title: fmt.Sprintf("%s (part %d)", sec.Title, i+1),
                    ChunkIndex: i,
                    SourceFile: basePath,
                    TokenCount: tokenCount(chunk),
                })
            }
        } else if tokenCount(sec.Content) >= s.config.MinChunkSize {
            result = append(result, sec)
        } else {
            // 小章节合并到前一个
            if len(result) > 0 {
                result[len(result)-1].Content += "\n\n" + sec.Content
                result[len(result)-1].TokenCount = tokenCount(result[len(result)-1].Content)
            } else {
                result = append(result, sec)
            }
        }
    }

    // 4. 生成层次路径
    for i := range result {
        result[i].Hierarchy = fmt.Sprintf("%s/%s", basePath, result[i].Title)
        result[i].ParentID = basePath
        result[i].NodeType = "chunk"
    }

    return result
}

// 带重叠的拆分
func splitWithOverlap(content string, maxSize, overlapSize int) []string {
    paragraphs := strings.Split(content, "\n\n")
    chunks := []string{}
    currentChunk := ""
    overlapBuffer := ""

    for _, para := range paragraphs {
        if tokenCount(currentChunk + para) > maxSize {
            if currentChunk != "" {
                chunks = append(chunks, currentChunk)
                // 保留最后 overlapSize tokens 作为下一个 chunk 的开头
                overlapBuffer = getLastNTokens(currentChunk, overlapSize)
                currentChunk = overlapBuffer + "\n\n" + para
            } else {
                // 单个段落就超过 maxSize，强制拆分
                chunks = append(chunks, para)
            }
        } else {
            currentChunk += "\n\n" + para
        }
    }

    if currentChunk != "" {
        chunks = append(chunks, currentChunk)
    }

    return chunks
}
```

### 4.2 L0/L1 生成（已改进）

```go
type SummaryGenerator struct {
    llmClient *openai.Client
    cache     *Cache  // 新增：缓存机制
}

// L0 Prompt（改进版）
const L0PromptTemplate = `请用一句话（不超过 50 字）概括以下内容的核心主题。
要求：
1. 突出关键信息和主要观点
2. 使用陈述句，不要使用疑问句
3. 不要包含"本文"、"这段内容"等元指称

内容：
%s

摘要：`

// L1 Prompt（改进版）
const L1PromptTemplate = `请为以下内容生成结构化概览（200-500 字）。

格式要求：
1. 核心主题：[一句话说明主题]
2. 主要内容：[3-5 个要点，每个要点一行]
3. 关键信息：[重要的数据、结论或观点]

内容：
%s

概览：`

func (g *SummaryGenerator) GenerateL0(content string) (string, error) {
    // 1. 检查缓存
    hash := sha256.Sum256([]byte(content))
    if cached, ok := g.cache.Get(hash); ok {
        return cached, nil
    }

    // 2. 调用 LLM
    prompt := fmt.Sprintf(L0PromptTemplate, truncate(content, 4000))
    resp, err := g.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model: "gpt-4o-mini",
        Messages: []openai.ChatCompletionMessage{
            {Role: "user", Content: prompt},
        },
        MaxTokens: 100,
    })

    if err != nil {
        // 降级策略：使用规则提取
        return extractFirstSentence(content), nil
    }

    result := resp.Choices[0].Message.Content
    g.cache.Set(hash, result)
    return result, nil
}

func (g *SummaryGenerator) GenerateL1(content string) (string, error) {
    // 类似 L0，但 MaxTokens = 500
    // ...
}

// 批量生成优化
func (g *SummaryGenerator) GenerateBatch(contents []string, level int) ([]string, error) {
    results := make([]string, len(contents))
    uncached := []int{}

    // 1. 检查缓存
    for i, content := range contents {
        hash := sha256.Sum256([]byte(content))
        if cached, ok := g.cache.Get(hash); ok {
            results[i] = cached
        } else {
            uncached = append(uncached, i)
        }
    }

    // 2. 批量调用 LLM（每次最多 10 个）
    for i := 0; i < len(uncached); i += 10 {
        batch := uncached[i:min(i+10, len(uncached))]
        // 并发调用
        var wg sync.WaitGroup
        for _, idx := range batch {
            wg.Add(1)
            go func(index int) {
                defer wg.Done()
                if level == 0 {
                    results[index], _ = g.GenerateL0(contents[index])
                } else {
                    results[index], _ = g.GenerateL1(contents[index])
                }
            }(idx)
        }
        wg.Wait()
    }

    return results, nil
}

// 降级策略：规则提取
func extractFirstSentence(content string) string {
    sentences := strings.Split(content, "。")
    if len(sentences) > 0 {
        return sentences[0] + "。"
    }
    return truncate(content, 50)
}
```

**关键改进**：
- 增加缓存机制（避免重复生成）
- 批量处理（并发调用，提高吞吐量）
- 降级策略（LLM 失败时使用规则提取）
- 改进 Prompt（增加格式约束）

### 4.3 分层检索核心算法（完全重写，已修正所有问题）

```go
type HierarchicalRetriever struct {
    store    *Store
    alpha    float64  // 分数传播系数，默认 0.7（提高向量分数权重）
    maxDepth int      // 最大递归深度，默认 5
    minScore float64  // 最小分数阈值，默认 0.3
}

type SearchNode struct {
    URI   string
    Score float64
    Depth int  // 新增：记录深度
}

func (r *HierarchicalRetriever) Search(query string, limit int) ([]Result, error) {
    queryVector := r.embedder.Embed(query)

    // 策略 1：全局搜索所有节点（支持扁平文档）
    globalResults := r.store.VectorSearch(queryVector, Filter{Limit: 50})

    // 策略 2：分层递归搜索（利用层次结构）
    hierarchicalResults := r.hierarchicalSearch(queryVector, globalResults[:10])

    // 策略 3：合并结果（RRF 融合）
    merged := r.mergeResults(globalResults, hierarchicalResults)

    // 策略 4：按 source_file 聚合（同一文件的 chunk 合并）
    aggregated := r.aggregateBySource(merged)

    return aggregated[:min(limit, len(aggregated))], nil
}

func (r *HierarchicalRetriever) hierarchicalSearch(
    queryVector []float64,
    seeds []Result,
) []Result {
    candidates := make(map[string]*Result)
    pq := NewPriorityQueue()
    visited := make(map[string]bool)

    // 初始化队列
    for _, seed := range seeds {
        pq.Push(&SearchNode{
            URI:   seed.ID,
            Score: seed.Score,
            Depth: 0,
        })
    }

    for pq.Len() > 0 {
        node := pq.Pop()

        // 剪枝 1：深度限制
        if node.Depth >= r.maxDepth {
            continue
        }

        // 剪枝 2：分数阈值
        if node.Score < r.minScore {
            continue
        }

        // 剪枝 3：去重
        if visited[node.URI] {
            continue
        }
        visited[node.URI] = true

        // 查询子节点（使用索引，不是全表扫描）
        children := r.store.GetChildren(node.URI)

        for _, child := range children {
            // 计算子节点的向量分数
            childScore := cosineSimilarity(queryVector, child.Vector)

            // 分数传播（考虑路径衰减）
            // 修正：使用子节点深度而不是父节点深度
            childDepth := node.Depth + 1
            depthDecay := math.Pow(0.9, float64(childDepth))
            finalScore := r.alpha*childScore + (1-r.alpha)*node.Score*depthDecay

            // 更新候选集
            if existing, ok := candidates[child.ID]; !ok || finalScore > existing.Score {
                candidates[child.ID] = &Result{
                    ID:         child.ID,
                    Abstract:   child.Abstract,
                    Score:      finalScore,
                    Depth:      node.Depth + 1,
                    SourceFile: child.SourceFile,
                }
            }

            // 如果有子节点，继续递归
            if r.store.HasChildren(child.ID) {
                pq.Push(&SearchNode{
                    URI:   child.ID,
                    Score: finalScore,
                    Depth: node.Depth + 1,
                })
            }
        }
    }

    return sortByScore(candidates)
}

// RRF 融合
func (r *HierarchicalRetriever) mergeResults(
    global []Result,
    hierarchical []Result,
) []Result {
    const k = 60
    scores := make(map[string]float64)

    for rank, res := range global {
        scores[res.ID] += 1.0 / float64(rank+k)
    }

    for rank, res := range hierarchical {
        scores[res.ID] += 1.0 / float64(rank+k)
    }

    merged := []Result{}
    for id, score := range scores {
        // 从 global 或 hierarchical 中找到完整信息
        var result Result
        for _, r := range append(global, hierarchical...) {
            if r.ID == id {
                result = r
                result.Score = score
                break
            }
        }
        merged = append(merged, result)
    }

    return sortByScore(merged)
}

// 按源文件聚合（已优化）
func (r *HierarchicalRetriever) aggregateBySource(results []Result) []Result {
    groups := make(map[string][]Result)

    for _, res := range results {
        groups[res.SourceFile] = append(groups[res.SourceFile], res)
    }

    aggregated := []Result{}
    for sourceFile, group := range groups {
        // 按 chunk_index 排序（保持顺序）
        sort.Slice(group, func(i, j int) bool {
            return group[i].ChunkIndex < group[j].ChunkIndex
        })

        // 取最高分的 chunk 作为代表
        best := group[0]
        for _, r := range group[1:] {
            if r.Score > best.Score {
                best = r
            }
        }

        // 只合并 Top-3 chunk 的 abstract（避免过长）
        combinedAbstract := ""
        topN := min(3, len(group))
        for i := 0; i < topN; i++ {
            combinedAbstract += fmt.Sprintf("[Part %d] %s\n", group[i].ChunkIndex+1, group[i].Abstract)
        }
        if len(group) > topN {
            combinedAbstract += fmt.Sprintf("... (还有 %d 个相关片段)\n", len(group)-topN)
        }
        best.Abstract = combinedAbstract
        best.ChunkCount = len(group)  // 记录 chunk 总数
        aggregated = append(aggregated, best)
    }

    return sortByScore(aggregated)
}
```

**关键改进**：
1. **支持扁平文档**：全局搜索 + 分层搜索双策略
2. **剪枝优化**：深度限制、分数阈值、去重
3. **分数传播改进**：考虑路径衰减（`depthDecay`）
4. **结果聚合**：同一文件的 chunk 合并展示
5. **RRF 融合**：全局和分层结果融合

---

## 五、性能目标（已细化）

| 指标 | 目标值 | 测试条件 |
|------|--------|----------|
| 文档拆分 | < 3s | 100MB 文档，纯 CPU |
| L0/L1 生成 | < 30s | 100 个节点，批量调用 |
| 分层检索（冷启动） | < 300ms | 10000 条记忆，3 层深度 |
| 分层检索（热缓存） | < 100ms | 10000 条记忆，3 层深度 |
| 并发检索（10 QPS） | < 500ms P99 | 10000 条记忆 |
| 内存占用（基础） | < 500MB | 10000 条记忆（不含向量） |
| 向量索引内存 | < 300MB | 10000 条记忆，768 维向量 |

---

## 六、风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| LLM API 调用成本高 | 高 | 中 | 批量处理、缓存、使用 gpt-4o-mini |
| 分层检索性能不达标 | 中 | 高 | 提前验证、优化索引、限制递归深度 |
| 数据迁移失败 | 低 | 高 | 备份数据、分步迁移、回滚机制 |
| 与现有功能冲突 | 中 | 中 | 保持向后兼容、渐进式集成 |

---

## 七、验收标准

### 7.1 功能验收

**测试场景 1：大文档拆分**
```
输入：存储 OpenViking_教学教案.md（100MB）
预期：
- 拆分成 ~50 个节点
- 每个节点有 L0/L1/L2
- 层次路径正确
```

**测试场景 2：分层检索**
```
输入：检索 "OpenViking 的分层检索算法"
预期：
- 先定位到 /教学教案/第四部分 目录
- 递归搜索子节点
- 返回相关章节的 L0 摘要
- 分数传播正确
```

**测试场景 3：按需加载**
```
输入：GET /api/memories/:id/content
预期：
- 返回完整 L2 内容
- 不影响检索性能
```

### 7.2 性能验收

```bash
# 分层检索性能
go test -bench=BenchmarkHierarchicalSearch -benchtime=10s
# 预期：< 200ms per operation (10000 条记忆)

# 文档拆分性能
go test -bench=BenchmarkDocumentSplit -benchtime=10s
# 预期：< 5s per operation (100MB 文档)
```

---

## 八、数据迁移和API兼容性（新增章节）

### 8.1 数据迁移策略

**现有数据处理**：

```go
func MigrateExistingMemories(db *sql.DB) error {
    // 1. 为所有现有记忆设置默认值
    _, err := db.Exec(`
        UPDATE memories
        SET
            node_type = 'chunk',
            chunk_index = 0,
            source_file = id,
            hierarchy = '/' || COALESCE(category, 'default') || '/' || id,
            parent_id = NULL
        WHERE node_type IS NULL
    `)
    if err != nil {
        return fmt.Errorf("failed to set defaults: %w", err)
    }

    // 2. 批量生成 L0/L1（异步任务，不阻塞启动）
    go func() {
        generator := NewSummaryGenerator()

        rows, _ := db.Query(`
            SELECT id, content
            FROM memories
            WHERE abstract IS NULL OR overview IS NULL
            LIMIT 100
        `)
        defer rows.Close()

        batch := []Memory{}
        for rows.Next() {
            var mem Memory
            rows.Scan(&mem.ID, &mem.Content)
            batch = append(batch, mem)

            // 每 10 条批量处理
            if len(batch) >= 10 {
                processBatch(db, generator, batch)
                batch = []Memory{}
            }
        }

        if len(batch) > 0 {
            processBatch(db, generator, batch)
        }
    }()

    return nil
}

func processBatch(db *sql.DB, gen *SummaryGenerator, batch []Memory) {
    contents := make([]string, len(batch))
    for i, mem := range batch {
        contents[i] = mem.Content
    }

    // 批量生成 L0 和 L1
    abstracts, _ := gen.GenerateBatch(contents, 0)
    overviews, _ := gen.GenerateBatch(contents, 1)

    // 批量更新数据库
    tx, _ := db.Begin()
    for i, mem := range batch {
        tx.Exec(`
            UPDATE memories
            SET abstract = ?, overview = ?
            WHERE id = ?
        `, abstracts[i], overviews[i], mem.ID)

        // 重新向量化（只对 L1）
        vector := embedder.Embed(overviews[i])
        tx.Exec(`
            INSERT OR REPLACE INTO vectors (memory_id, vector)
            VALUES (?, ?)
        `, mem.ID, serializeVector(vector))
    }
    tx.Commit()
}
```

**迁移验证**：
```go
func VerifyMigration(db *sql.DB) error {
    // 检查是否所有记忆都有 abstract 和 overview
    var count int
    db.QueryRow(`
        SELECT COUNT(*)
        FROM memories
        WHERE abstract IS NULL OR overview IS NULL
    `).Scan(&count)

    if count > 0 {
        return fmt.Errorf("migration incomplete: %d memories missing L0/L1", count)
    }

    return nil
}
```

### 8.2 API 向后兼容

**版本化策略**：

```go
// 检测客户端版本
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
    apiVersion := r.Header.Get("X-API-Version")
    if apiVersion == "" {
        apiVersion = "v1"  // 默认旧版本
    }

    query := r.URL.Query().Get("q")
    limit := getIntParam(r, "limit", 10)

    // 执行检索
    results := h.retriever.Search(query, limit)

    // 根据版本返回不同格式
    if apiVersion == "v1" {
        // 旧版本：返回完整 content（向后兼容）
        for i := range results {
            results[i].Content = h.store.GetContent(results[i].ID)
        }
        json.NewEncoder(w).Encode(map[string]interface{}{
            "results": results,
        })
    } else {
        // 新版本：只返回 abstract，提供按需加载链接
        for i := range results {
            results[i].Content = ""  // 不返回完整内容
            results[i].ContentURL = fmt.Sprintf("/api/memories/%s/content", results[i].ID)
        }
        json.NewEncoder(w).Encode(map[string]interface{
            "results": results,
            "version": "v2",
        })
    }
}

// 新增：按需加载完整内容
func (h *Handler) GetContent(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")

    content, err := h.store.GetContent(id)
    if err != nil {
        http.Error(w, "not found", 404)
        return
    }

    json.NewEncoder(w).Encode(map[string]interface{}{
        "id":      id,
        "content": content,
    })
}
```

**客户端升级指南**：

```markdown
# API v1 → v2 升级指南

## 变更点

1. **检索结果不再包含完整 content**
   - v1: `results[0].content` 包含完整内容
   - v2: `results[0].content` 为空，使用 `results[0].abstract` 预览

2. **按需加载完整内容**
   - v2: 使用 `GET /api/memories/:id/content` 获取完整内容

## 升级步骤

### 方式 1：保持 v1 兼容（推荐）
```javascript
// 在请求头中指定版本
fetch('/api/memories/search?q=test', {
    headers: { 'X-API-Version': 'v1' }
})
```

### 方式 2：升级到 v2
```javascript
// 1. 检索（只返回摘要）
const res = await fetch('/api/memories/search?q=test', {
    headers: { 'X-API-Version': 'v2' }
})
const data = await res.json()

// 2. 按需加载完整内容
for (const result of data.results) {
    if (needFullContent(result)) {
        const content = await fetch(result.contentURL).then(r => r.json())
        result.content = content.content
    }
}
```
```

---

## 九、待 Codex 审查的问题（更新）

## 九、待 Codex 审查的问题（更新）

**已根据第 1 轮反馈修正的问题**：
- ✅ 数据模型设计（删除 level 字段，增加必要字段）
- ✅ 向量化策略（只对 L1 向量化）
- ✅ 拆分阈值（降低到 512/256 tokens）
- ✅ Prompt 设计（增加格式约束）
- ✅ 递归搜索算法（增加剪枝、深度限制、结果聚合）
- ✅ 时间估算（调整到 24 天）
- ✅ 数据迁移策略（新增章节）
- ✅ API 兼容性（新增章节）

**请 Codex 重点审查**：
1. 数据模型是否还有遗漏的字段？
2. 分层检索算法的实现是否正确？特别是分数传播和剪枝逻辑
3. 批量处理和缓存机制是否合理？
4. 数据迁移策略是否安全？是否有遗漏的边界情况？
5. API 兼容性方案是否完善？
6. 性能目标是否现实？

---

**修订版完成时间**：2026-03-18
**下一步**：提交给 Codex 第 2 轮审查
