# 分层混合检索设计

> 整合 OpenViking 的分层检索思想到 HybridMem-RAG

## 1. 设计目标

将 OpenViking 的分层检索与 Memory LanceDB Pro 的混合检索融合，提升检索精度。

**核心思想**：
- Memory LanceDB Pro：Vector + BM25 + RRF 融合
- OpenViking：分层检索，利用层次结构
- HybridMem-RAG：两者结合，纯 Go 实现

## 2. 数据模型扩展

### 2.1 当前模型

```sql
-- memories 表
CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  text TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT 'other',
  scope TEXT NOT NULL DEFAULT 'global',
  importance REAL NOT NULL DEFAULT 0.7,
  timestamp INTEGER NOT NULL,
  metadata TEXT DEFAULT '{}'
);

-- vectors 表（独立存储）
CREATE TABLE vectors (
  memory_id TEXT PRIMARY KEY,
  vector BLOB NOT NULL,
  dimension INTEGER NOT NULL,
  FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
);
```

### 2.2 扩展后模型

```sql
-- 使用 IF NOT EXISTS 保证幂等性
-- 注意：SQLite 不支持 ALTER TABLE IF NOT EXISTS，需要在代码中检查

-- 方案 1：在 Go 代码中检查列是否存在
func migrateHierarchyColumns(db *sql.DB) error {
    // 检查两个列是否都已存在
    var pathCount, levelCount int
    db.QueryRow(`
        SELECT COUNT(*) FROM pragma_table_info('memories')
        WHERE name = 'hierarchy_path'
    `).Scan(&pathCount)

    db.QueryRow(`
        SELECT COUNT(*) FROM pragma_table_info('memories')
        WHERE name = 'hierarchy_level'
    `).Scan(&levelCount)

    // 只在两个列都不存在时执行完整迁移
    if pathCount == 0 && levelCount == 0 {
        if _, err := db.Exec(`ALTER TABLE memories ADD COLUMN hierarchy_path TEXT DEFAULT NULL`); err != nil {
            return fmt.Errorf("failed to add hierarchy_path: %w", err)
        }
        if _, err := db.Exec(`ALTER TABLE memories ADD COLUMN hierarchy_level INTEGER DEFAULT 0`); err != nil {
            return fmt.Errorf("failed to add hierarchy_level: %w", err)
        }

        // 创建索引
        _, err := db.Exec(`
            CREATE INDEX IF NOT EXISTS idx_hierarchy_path ON memories(hierarchy_path);
            CREATE INDEX IF NOT EXISTS idx_hierarchy_level ON memories(hierarchy_level);
        `)
        return err
    }

    // 处理部分迁移的情况
    if pathCount == 0 {
        if _, err := db.Exec(`ALTER TABLE memories ADD COLUMN hierarchy_path TEXT DEFAULT NULL`); err != nil {
            return fmt.Errorf("failed to add hierarchy_path: %w", err)
        }
    }
    if levelCount == 0 {
        if _, err := db.Exec(`ALTER TABLE memories ADD COLUMN hierarchy_level INTEGER DEFAULT 0`); err != nil {
            return fmt.Errorf("failed to add hierarchy_level: %w", err)
        }
    }

    // 确保索引存在
    _, err := db.Exec(`
        CREATE INDEX IF NOT EXISTS idx_hierarchy_path ON memories(hierarchy_path);
        CREATE INDEX IF NOT EXISTS idx_hierarchy_level ON memories(hierarchy_level);
    `)
    return err
}
```

**说明**：
- 使用 `pragma_table_info` 检查列是否存在
- 只在列不存在时执行 ALTER TABLE
- 保证多次启动不会失败

### 2.3 层次路径规则

| 来源 | hierarchy_path 示例 | hierarchy_level |
|------|---------------------|-----------------|
| 文件 | `/project/src/auth/login.ts` | 4 |
| 浏览器 | `/browser/chatgpt` | 2 |
| 手动 | `/manual/notes` | 2 |
| 无层次 | NULL | 0 |

## 3. 检索算法设计

### 3.1 分层混合检索流程

```
输入：query, currentPath
  ↓
解析层次路径
  ↓
对每一层执行混合检索
  ├─ 向量检索（余弦相似度）
  ├─ BM25 检索（FTS5）
  └─ RRF 融合
  ↓
层级加权
  ↓
跨层聚合
  ↓
12 阶段评分管道
  ↓
输出：Top-K 结果
```

### 3.2 核心算法

```go
// 分层混合检索
func (r *Retriever) HierarchicalHybridSearch(
    query string,
    currentPath string,
    limit int,
    scopes []string,  // 新增：scope 过滤
) ([]SearchResult, error) {
    // 0. 验证参数
    if limit <= 0 || limit > 100 {
        return nil, fmt.Errorf("invalid limit: %d (must be 1-100)", limit)
    }

    // 1. 向量化查询（只执行一次）
    queryVec := r.embedder.Embed(query)

    // 2. 解析层次路径
    levels := parseHierarchyLevels(currentPath)
    // 例："/project/src/auth" → ["/project", "/project/src", "/project/src/auth"]

    var allResults []SearchResult

    // 3. 在每一层执行混合检索
    for i, level := range levels {
        // 3.1 向量检索（传递已计算的 queryVec）
        vectorResults := r.vectorSearchInLevel(queryVec, level, limit*2, scopes)

        // 3.2 BM25 检索（传递 scopes）
        bm25Results := r.bm25SearchInLevel(query, level, limit*2, scopes)

        // 2.3 RRF 融合
        fusedResults := rrfFusion(vectorResults, bm25Results)

        // 2.4 层级加权
        weight := calculateLevelWeight(i, len(levels))
        for j := range fusedResults {
            fusedResults[j].Score *= weight
            // 注：层级信息可通过 HierarchyPath 字段获取，无需修改 Metadata
        }

        allResults = append(allResults, fusedResults...)
    }

    // 3. 跨层聚合去重
    aggregated := aggregateResults(allResults, limit*3)

    // 4. 全局 fallback：如果结果不足，搜索无层次的记忆
    if len(aggregated) < limit {
        globalResults := r.searchGlobalMemories(query, limit-len(aggregated), scopes)
        // 全局记忆降权（权重 0.5）
        for i := range globalResults {
            globalResults[i].Score *= 0.5
        }
        aggregated = append(aggregated, globalResults...)
    }

    // 5. 12 阶段评分管道
    scored := r.scoringPipeline.Process(aggregated)

    // 6. 返回 Top-K（防止切片越界）
    if len(scored) > limit {
        return scored[:limit], nil
    }
    return scored, nil
}

// searchGlobalMemories 搜索无层次路径的记忆
func (r *Retriever) searchGlobalMemories(
    query string,
    limit int,
    scopes []string,
) []SearchResult {
    // 只搜索 hierarchy_path IS NULL 的记忆
    // 实现类似 vectorSearchInLevel，但过滤条件是 IS NULL
    // ...
}
```

### 3.3 层级权重计算

```go
// 层级权重：当前层最高，越远越低
func calculateLevelWeight(levelIndex, totalLevels int) float64 {
    distance := totalLevels - levelIndex - 1
    return math.Pow(0.8, float64(distance))
}
```

**示例**：
- 当前层（distance=0）：权重 = 0.8^0 = 1.0
- 父层（distance=1）：权重 = 0.8^1 = 0.8
- 祖父层（distance=2）：权重 = 0.8^2 = 0.64

## 4. 实现细节

### 4.1 层内检索

```go
// 在指定层级执行向量检索
func (r *Retriever) vectorSearchInLevel(
    queryVec []float32,  // 修改：直接接收向量，避免重复计算
    level string,
    limit int,
    scopes []string,
) []SearchResult {
    // 构建 scope 过滤条件
    scopeFilter := ""
    args := []interface{}{}
    if len(scopes) > 0 {
        scopeFilter = " AND m.scope IN ("
        for i := range scopes {
            if i > 0 {
                scopeFilter += ","
            }
            scopeFilter += "?"
            args = append(args, scopes[i])
        }
        scopeFilter += ")"
    }

    // 查询：先获取所有候选，再计算相似度
    // 注意：hierarchy_path IS NULL 的记忆作为全局 fallback，单独处理
    sql := `
        SELECT m.id, m.text, v.vector, m.hierarchy_path
        FROM memories m
        JOIN vectors v ON m.id = v.memory_id
        WHERE (
            m.hierarchy_path = ? OR
            m.hierarchy_path LIKE ? ESCAPE '\'
        )` + scopeFilter

    args = append([]interface{}{level, escapeLike(level) + "/%"}, args...)
    rows, err := r.db.Query(sql, args...)
    if err != nil {
        return nil
    }
    defer rows.Close()

    var candidates []SearchResult
    for rows.Next() {
        var m Memory
        var vectorBlob []byte
        if err := rows.Scan(&m.ID, &m.Text, &vectorBlob, &m.HierarchyPath); err != nil {
            continue
        }

        // 反序列化向量（关键修复）
        m.Vector = DeserializeVector(vectorBlob)
        score := cosineSimilarity(queryVec, m.Vector)

        candidates = append(candidates, SearchResult{
            Memory: m,
            Score:  score,
        })
    }

    // 按相似度排序
    sort.Slice(candidates, func(i, j int) bool {
        return candidates[i].Score > candidates[j].Score
    })

    // 返回 Top-K
    if len(candidates) > limit {
        return candidates[:limit]
    }
    return candidates
}
```

**关键修复**：
1. 增加 `hierarchy_path IS NULL` 条件，支持无层次的记忆
2. 增加 scope 过滤，防止跨租户数据泄露
3. 先查询所有候选，再计算相似度并排序
4. 使用 `min(len, limit)` 模式避免切片越界

### 4.2 跨层聚合

```go
// 聚合多层结果，去重并保留最高分
func aggregateResults(results []SearchResult, limit int) []SearchResult {
    scoreMap := make(map[string]SearchResult)

    for _, r := range results {
        if existing, ok := scoreMap[r.Memory.ID]; ok {
            // 保留更高分数
            if r.Score > existing.Score {
                scoreMap[r.Memory.ID] = r
            }
        } else {
            scoreMap[r.Memory.ID] = r
        }
    }

    var aggregated []SearchResult
    for _, r := range scoreMap {
        aggregated = append(aggregated, r)
    }

    sort.Slice(aggregated, func(i, j int) bool {
        return aggregated[i].Score > aggregated[j].Score
    })

    if len(aggregated) > limit {
        return aggregated[:limit]
    }
    return aggregated
}
```

### 4.3 辅助函数

```go
// RRF 融合算法（k=60）
func rrfFusion(vectorResults, bm25Results []SearchResult) []SearchResult {
    const k = 60
    scoreMap := make(map[string]float64)
    memoryMap := make(map[string]Memory)

    for rank, r := range vectorResults {
        scoreMap[r.Memory.ID] += 1.0 / float64(k+rank+1)
        memoryMap[r.Memory.ID] = r.Memory
    }

    for rank, r := range bm25Results {
        scoreMap[r.Memory.ID] += 1.0 / float64(k+rank+1)
        memoryMap[r.Memory.ID] = r.Memory
    }

    var results []SearchResult
    for id, score := range scoreMap {
        results = append(results, SearchResult{
            Memory: memoryMap[id],
            Score:  score,
        })
    }

    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })

    return results
}

// BM25 层内检索
func (r *Retriever) bm25SearchInLevel(
    query string,
    level string,
    limit int,
    scopes []string,
) []SearchResult {
    // 构建 scope 过滤
    scopeFilter := ""
    args := []interface{}{query, level, escapeLike(level) + "/%"}
    if len(scopes) > 0 {
        placeholders := make([]string, len(scopes))
        for i := range scopes {
            placeholders[i] = "?"
        }
        scopeFilter = " AND m.scope IN (" + strings.Join(placeholders, ",") + ")"
        args = append(args, convertToInterfaces(scopes)...)
    }

    sql := `
        SELECT m.id, m.text, m.hierarchy_path, bm25(fts) as score
        FROM fts_memories fts
        JOIN memories m ON fts.rowid = m.rowid
        WHERE fts MATCH ?
          AND (m.hierarchy_path = ? OR m.hierarchy_path LIKE ? ESCAPE '\')` + scopeFilter + `
        ORDER BY score DESC
        LIMIT ?`

    args = append(args, limit)
    rows, err := r.db.Query(sql, args...)
    if err != nil {
        return nil
    }
    defer rows.Close()

    var results []SearchResult
    for rows.Next() {
        var m Memory
        var score float64
        if err := rows.Scan(&m.ID, &m.Text, &m.HierarchyPath, &score); err != nil {
            continue
        }
        results = append(results, SearchResult{Memory: m, Score: score})
    }

    return results
}

// 向量反序列化
func DeserializeVector(blob []byte) []float32 {
    if len(blob)%4 != 0 {
        return nil
    }
    vec := make([]float32, len(blob)/4)
    for i := range vec {
        bits := binary.LittleEndian.Uint32(blob[i*4 : (i+1)*4])
        vec[i] = math.Float32frombits(bits)
    }
    return vec
}

// 余弦相似度
func cosineSimilarity(a, b []float32) float64 {
    if len(a) != len(b) {
        return 0
    }
    var dotProduct, normA, normB float64
    for i := range a {
        dotProduct += float64(a[i] * b[i])
        normA += float64(a[i] * a[i])
        normB += float64(b[i] * b[i])
    }
    if normA == 0 || normB == 0 {
        return 0
    }
    return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// SQL LIKE 转义
func escapeLike(s string) string {
    s = strings.ReplaceAll(s, "\\", "\\\\")
    s = strings.ReplaceAll(s, "_", "\\_")
    s = strings.ReplaceAll(s, "%", "\\%")
    return s
}

// 解析层次路径
func parseHierarchyLevels(path string) []string {
    if path == "" {
        return []string{}
    }
    parts := strings.Split(strings.Trim(path, "/"), "/")
    levels := make([]string, len(parts))
    for i := range parts {
        levels[i] = "/" + strings.Join(parts[:i+1], "/")
    }
    return levels
}

// 层级权重计算
func calculateLevelWeight(levelIndex, totalLevels int) float64 {
    distance := totalLevels - levelIndex - 1
    return math.Pow(0.8, float64(distance))
}

// 字符串切片转接口切片
func convertToInterfaces(strs []string) []interface{} {
    result := make([]interface{}, len(strs))
    for i, s := range strs {
        result[i] = s
    }
    return result
}
```

## 5. 配置选项

```go
type HierarchicalConfig struct {
    Enabled          bool    // 是否启用分层检索
    MaxLevels        int     // 最大搜索层级，默认 5
    LevelWeightDecay float64 // 层级权重衰减系数，默认 0.8
    FallbackToGlobal bool    // 无层次时回退到全局搜索
}
```

## 6. 向后兼容

### 6.1 兼容策略

- `hierarchy_path` 为 NULL 时，自动回退到全局搜索
- 现有 API 保持不变，新增可选参数 `current_path`
- 渐进式迁移，不影响现有功能

### 6.2 API 设计

```go
// 检索接口（保持 GET 方法向后兼容）
GET /api/memories/search?q=用户登录流程&limit=10&current_path=/project/src/auth

// 参数说明：
// - q: 查询文本（必需，保持与现有 API 一致）
// - limit: 返回数量（可选，默认 10）
// - current_path: 当前层次路径（可选，为空时使用全局搜索）
// - scope: 作用域过滤（可选，多个用逗号分隔）
```

**实现示例**：
```go
func (h *Handler) SearchMemories(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query().Get("q")  // 保持使用 "q" 参数
    limitStr := r.URL.Query().Get("limit")
    currentPath := r.URL.Query().Get("current_path")  // 新增可选参数

    limit := 10
    if limitStr != "" {
        if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
            limit = parsed
        }
    }

    // 向量化查询
    queryVec := h.embedder.Embed(query)

    // 解析 scope 参数
    scopesParam := r.URL.Query().Get("scope")
    var scopes []string
    if scopesParam != "" {
        scopes = strings.Split(scopesParam, ",")
    }

    var results []SearchResult
    if currentPath != "" {
        // 使用分层检索
        results = h.store.HierarchicalHybridSearch(query, currentPath, limit, scopes)
    } else {
        // 回退到现有的全局检索
        results = h.store.HybridSearch(query, limit, scopes)
    }

    json.NewEncoder(w).Encode(results)
}
```

## 7. 性能分析

### 7.1 时间复杂度

- 全局搜索：O(N)
- 分层搜索：O(N/L × L) ≈ O(N)，其中 L 是层数
- 实际更快：每层候选集更小

### 7.2 预期性能

| 场景 | 全局搜索 | 分层搜索 |
|------|----------|----------|
| 10000 条记忆 | 50ms | 80ms |
| 100000 条记忆 | 500ms | 200ms |

**结论**：记忆越多，分层搜索优势越明显

## 8. 测试验证

### 8.1 单元测试

```go
func TestHierarchicalSearch(t *testing.T) {
    // 准备测试数据
    memories := []Memory{
        {Content: "登录逻辑", HierarchyPath: "/project/src/auth"},
        {Content: "路由配置", HierarchyPath: "/project/src/api"},
        {Content: "项目文档", HierarchyPath: "/project/docs"},
    }

    // 执行分层搜索
    results := retriever.HierarchicalHybridSearch(
        "用户登录",
        "/project/src/auth",
        10,
    )

    // 验证：auth 层的记忆应该排在前面
    assert.Equal(t, "/project/src/auth", results[0].HierarchyPath)
}
```

### 8.2 集成测试

1. 存储不同层级的记忆
2. 从特定层级搜索
3. 验证结果顺序符合层级权重

## 9. 实现路径

### 阶段 1：数据模型（1 天）
- 修改 `internal/store/schema.go`
- 增加 `hierarchy_path` 和 `hierarchy_level` 字段
- 创建索引

### 阶段 2：层次解析（1 天）
- 实现 `parseHierarchyLevels()`
- 实现 `calculateLevelWeight()`
- 单元测试

### 阶段 3：层内检索（2 天）
- 实现 `vectorSearchInLevel()`
- 实现 `bm25SearchInLevel()`
- 单元测试

### 阶段 4：跨层聚合（1 天）
- 实现 `aggregateResults()`
- 去重逻辑
- 单元测试

### 阶段 5：集成（1 天）
- 整合到现有检索引擎
- API 扩展
- 集成测试

**总计**：6 天

---

**参考来源**：
- Memory LanceDB Pro：`../memory-lancedb-pro-main/src/retriever.ts`
- OpenViking：https://github.com/volcengine/OpenViking
- 当前架构：`docs/architecture/INDEX.md`
