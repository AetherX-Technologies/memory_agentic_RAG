# HybridMem-RAG 第二轮代码质量审查报告

**审查日期**: 2026-03-17
**审查范围**: 第一轮修复后的关键文件
**审查重点**: 新引入问题、边界条件、并发安全、资源泄漏、类型安全

---

## 严重问题 (Critical)

### 1. **SQL 注入风险 - schema.go:74,77**
**位置**: `internal/store/schema.go` - `migrateHierarchy()`

```go
// 危险：使用 fmt.Sprintf 构造表名，可能导致 SQL 注入
db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, tableMemories), colHierarchyPath)
```

**问题**: 虽然 `tableMemories` 是常量，但使用 `fmt.Sprintf` 构造 SQL 是不安全的模式。

**修复建议**:
```go
// 使用字符串拼接常量
query := "SELECT COUNT(*) FROM pragma_table_info('" + tableMemories + "') WHERE name = ?"
db.QueryRow(query, colHierarchyPath).Scan(&pathCount)
```

---

### 2. **资源泄漏 - hierarchical.go:88-90**
**位置**: `internal/store/hierarchical.go` - `vectorSearchInLevel()`

```go
for rows.Next() {
    // ...
    if err := rows.Scan(...); err != nil {
        continue  // ❌ 错误被静默忽略
    }
    vec, err := DeserializeVector(vectorBlob)
    if err != nil {
        continue  // ❌ 错误被静默忽略
    }
}
```

**问题**:
1. 扫描错误和反序列化错误被静默忽略
2. 可能导致部分结果丢失而用户不知情
3. 违反"快速失败"原则

**修复建议**:
```go
for rows.Next() {
    var m Memory
    var vectorBlob []byte
    if err := rows.Scan(&m.ID, &m.Text, &vectorBlob, &m.HierarchyPath, &m.Category, &m.Scope, &m.Importance, &m.Timestamp, &m.Metadata); err != nil {
        return nil, fmt.Errorf("failed to scan row: %w", err)
    }
    vec, err := DeserializeVector(vectorBlob)
    if err != nil {
        return nil, fmt.Errorf("failed to deserialize vector for memory %s: %w", m.ID, err)
    }
    m.Vector = vec
    score := float64(CosineSimilarity(queryVec, m.Vector))
    candidates = append(candidates, SearchResult{Entry: m, Score: score})
}
```

---

### 3. **相同问题 - hierarchical.go:140-141**
**位置**: `internal/store/hierarchical.go` - `bm25SearchInLevel()`

```go
if err := rows.Scan(...); err != nil {
    continue  // ❌ 静默忽略错误
}
```

**同样的问题**: 应该返回错误而不是静默忽略。

---

### 4. **相同问题 - hierarchical.go:300-308**
**位置**: `internal/store/hierarchical.go` - `searchGlobalMemories()`

```go
if err := rows.Scan(...); err != nil {
    continue  // ❌ 静默忽略
}
vec, err := DeserializeVector(vectorBlob)
if err != nil {
    continue  // ❌ 静默忽略
}
```

**同样的问题**: 应该返回错误。

---

### 5. **相同问题 - hierarchical.go:353**
**位置**: `internal/store/hierarchical.go` - `searchGlobalMemories()` BM25 分支

```go
if err := rows.Scan(...); err != nil {
    continue  // ❌ 静默忽略
}
```

---

## 中等问题 (Medium)

### 6. **边界条件未处理 - fts_utils.go:6-10**
**位置**: `internal/store/fts_utils.go` - `EscapeFTS5Query()`

```go
func EscapeFTS5Query(query string) string {
    trimmed := strings.TrimSpace(query)
    if trimmed == "" {
        return "\"\""  // ⚠️ 返回空引号可能导致 FTS5 错误
    }
```

**问题**:
1. 空查询返回 `""` 可能导致 FTS5 语法错误
2. 调用方应该在调用前验证查询非空

**修复建议**:
```go
func EscapeFTS5Query(query string) string {
    trimmed := strings.TrimSpace(query)
    if trimmed == "" {
        return ""  // 返回空字符串，让调用方处理
    }
    // ... rest of the code
}
```

并在调用方添加验证：
```go
if queryText == "" {
    return nil, fmt.Errorf("query text cannot be empty")
}
escapedQuery := EscapeFTS5Query(queryText)
```

---

### 7. **大小写敏感问题 - fts_utils.go:13-16**
**位置**: `internal/store/fts_utils.go` - `EscapeFTS5Query()`

```go
upper := strings.ToUpper(trimmed)
if upper == "AND" || upper == "OR" || upper == "NOT" {
    return "\"" + trimmed + "\""
}
```

**问题**: 只检查全大写操作符，但 FTS5 操作符不区分大小写。

**测试用例**:
- `"and"` → 应该被转义但不会
- `"And"` → 应该被转义但不会
- `"OR"` → 正确转义

**修复建议**:
```go
if upper == "AND" || upper == "OR" || upper == "NOT" {
    return "\"" + trimmed + "\""
}
```
这个实现是正确的，因为它使用 `upper` 进行比较。但需要添加测试确保覆盖所有情况。

---

### 8. **不完整的特殊字符检测 - fts_utils.go:19-26**
**位置**: `internal/store/fts_utils.go` - `EscapeFTS5Query()`

```go
if strings.ContainsAny(trimmed, "+-\"*()") ||
    strings.Contains(trimmed, " AND ") ||
    strings.Contains(trimmed, " OR ") ||
    strings.Contains(trimmed, " NOT ") {
```

**问题**:
1. 只检查 ` AND ` (带空格)，但 `AND` 单独出现也是操作符
2. 不检查 `NEAR`、`^` (boost) 等其他 FTS5 操作符
3. 大小写敏感检查（应该不区分大小写）

**修复建议**:
```go
upper := strings.ToUpper(trimmed)
if strings.ContainsAny(trimmed, "+-\"*()^") ||
    strings.Contains(upper, " AND ") ||
    strings.Contains(upper, " OR ") ||
    strings.Contains(upper, " NOT ") ||
    strings.Contains(upper, " NEAR ") {
    escaped := strings.ReplaceAll(trimmed, "\"", "\"\"")
    return "\"" + escaped + "\""
}
```

---

### 9. **并发安全问题 - store.go:76-81**
**位置**: `internal/store/store.go` - `New()`

```go
if config.DBPath == ":memory:" {
    db.SetMaxOpenConns(1)
} else {
    db.SetMaxOpenConns(MaxOpenConnections)
}
```

**问题**:
1. 没有设置 `SetMaxIdleConns`，可能导致连接池不稳定
2. 没有设置 `SetConnMaxLifetime`，长连接可能导致问题

**修复建议**:
```go
if config.DBPath == ":memory:" {
    db.SetMaxOpenConns(1)
    db.SetMaxIdleConns(1)
} else {
    db.SetMaxOpenConns(MaxOpenConnections)
    db.SetMaxIdleConns(MaxOpenConnections / 2)
    db.SetConnMaxLifetime(time.Hour)
}
```

---

### 10. **错误处理不一致 - store.go:82-85**
**位置**: `internal/store/store.go` - `New()`

```go
if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
    db.Close()
    return nil, fmt.Errorf("failed to enable WAL: %w", err)
}
```

**问题**: 对于 `:memory:` 数据库，WAL 模式不适用，会返回错误。

**修复建议**:
```go
// WAL 模式仅对文件数据库有效
if config.DBPath != ":memory:" {
    if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
        db.Close()
        return nil, fmt.Errorf("failed to enable WAL: %w", err)
    }
}
```

---

### 11. **向量维度不一致 - store.go:154-156**
**位置**: `internal/store/store.go` - `Insert()`

```go
if s.config.VectorDim > 0 && len(memory.Vector) != s.config.VectorDim {
    return "", fmt.Errorf("vector dimension mismatch: expected %d, got %d", s.config.VectorDim, len(memory.Vector))
}
```

**问题**:
1. 当 `VectorDim == 0` 时不验证维度
2. 可能导致数据库中存储不同维度的向量
3. 后续检索会失败

**修复建议**:
```go
if len(memory.Vector) > 0 {
    if s.config.VectorDim > 0 && len(memory.Vector) != s.config.VectorDim {
        return "", fmt.Errorf("vector dimension mismatch: expected %d, got %d", s.config.VectorDim, len(memory.Vector))
    }
    // 如果是第一个向量，记录维度
    if s.config.VectorDim == 0 {
        s.config.VectorDim = len(memory.Vector)
    }
    // ... rest of the code
}
```

**注意**: 这需要 `config.VectorDim` 可变，或者在初始化时强制要求设置维度。

---

### 12. **数据竞争风险 - store.go:158-160**
**位置**: `internal/store/store.go` - `Insert()`

```go
normalized := make([]float32, len(memory.Vector))
copy(normalized, memory.Vector)
NormalizeVector(normalized)
```

**问题**: 虽然复制了向量，但如果 `memory.Vector` 在其他 goroutine 中被修改，仍可能有数据竞争。

**建议**: 在文档中明确说明 `Memory` 对象在传递给 `Insert` 后不应被修改。

---

### 13. **错误处理缺失 - handler.go:34**
**位置**: `internal/api/handler.go` - `writeJSON()`

```go
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)  // ❌ 忽略错误
}
```

**问题**: JSON 编码错误被忽略，客户端可能收到不完整的响应。

**修复建议**:
```go
func writeJSON(w http.ResponseWriter, status int, data interface{}) error {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    return json.NewEncoder(w).Encode(data)
}
```

或者至少记录错误：
```go
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    if err := json.NewEncoder(w).Encode(data); err != nil {
        // 已经写入了 header，无法返回错误响应
        // 只能记录日志
        fmt.Fprintf(os.Stderr, "failed to encode JSON: %v\n", err)
    }
}
```

---

### 14. **路径提取逻辑脆弱 - handler.go:41-46**
**位置**: `internal/api/handler.go` - `extractMemoryID()`

```go
func extractMemoryID(path string) (string, error) {
    id := strings.TrimPrefix(path, "/api/memories/")
    if id == "" || id == "search" || id == "stats" {
        return "", fmt.Errorf("invalid memory id")
    }
    return id, nil
}
```

**问题**:
1. 如果路径是 `/api/memories/search/123`，会返回 `search/123`
2. 没有验证 ID 格式（应该是 UUID）
3. 没有处理尾部斜杠

**修复建议**:
```go
func extractMemoryID(path string) (string, error) {
    path = strings.TrimSuffix(path, "/")
    id := strings.TrimPrefix(path, "/api/memories/")

    if id == "" || id == "search" || id == "stats" || strings.Contains(id, "/") {
        return "", fmt.Errorf("invalid memory id")
    }

    // 可选：验证 UUID 格式
    if _, err := uuid.Parse(id); err != nil {
        return "", fmt.Errorf("invalid UUID format: %w", err)
    }

    return id, nil
}
```

---

### 15. **参数验证不完整 - handler.go:86-89**
**位置**: `internal/api/handler.go` - `SearchMemories()`

```go
if limitStr != "" {
    if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
        limit = parsed
    }
}
```

**问题**:
1. 无效的 limit 参数被静默忽略，使用默认值
2. 用户不知道参数被忽略
3. 可能导致困惑

**修复建议**:
```go
if limitStr != "" {
    parsed, err := strconv.Atoi(limitStr)
    if err != nil {
        writeError(w, http.StatusBadRequest, "invalid limit parameter: must be an integer")
        return
    }
    if parsed <= 0 || parsed > 100 {
        writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
        return
    }
    limit = parsed
}
```

---

## 轻微问题 (Minor)

### 16. **魔法数字 - handler.go:29**
**位置**: `internal/api/handler.go`

```go
const maxRequestBodySize = 10 << 20 // 10MB
```

**建议**: 应该可配置，不同部署环境可能需要不同的限制。

---

### 17. **TODO 未处理 - handler.go:102-104**
**位置**: `internal/api/handler.go` - `SearchMemories()`

```go
// TODO: 需要添加 embedder 支持
// 临时方案：使用空向量会导致无意义的相似度，应该只使用 BM25
queryVec := []float32{} // 空向量表示未向量化
```

**问题**: 这是一个已知的功能缺失，应该在 issue tracker 中跟踪。

---

### 18. **硬编码版本 - handler.go:53**
**位置**: `internal/api/handler.go` - `HealthCheck()`

```go
"version": "1.0.0", // TODO: Use build version from ldflags
```

**建议**: 使用构建时注入的版本号。

---

### 19. **不一致的错误消息 - hierarchical.go:221,230**
**位置**: `internal/store/hierarchical.go` - `HierarchicalHybridSearch()`

```go
return nil, fmt.Errorf("vector search failed at level %s: %w", level, err)
// vs
return nil, fmt.Errorf("BM25 search failed at level %s: %w", level, err)
```

**建议**: 统一错误消息格式，便于日志分析。

---

### 20. **性能问题 - hierarchical.go:254**
**位置**: `internal/store/hierarchical.go` - `HierarchicalHybridSearch()`

```go
combined := append(aggregated, globalResults...)
aggregated = aggregateResults(combined, limit)
```

**问题**:
1. `aggregateResults` 会重新排序所有结果
2. 如果 `aggregated` 已经满了，不需要添加 `globalResults`

**优化建议**:
```go
if len(aggregated) < limit {
    needed := limit - len(aggregated)
    globalResults, _ := s.searchGlobalMemories(queryVec, query, needed*2, scopes)
    for i := range globalResults {
        globalResults[i].Score *= globalFallbackWeight
    }
    combined := append(aggregated, globalResults...)
    aggregated = aggregateResults(combined, limit)
}
```

---

### 21. **错误被忽略 - hierarchical.go:249,376**
**位置**: `internal/store/hierarchical.go`

```go
globalResults, _ := s.searchGlobalMemories(...)  // ❌ 错误被忽略
// ...
scored, _ = s.reranker.Rerank(query, scored)  // ❌ 错误被忽略
```

**建议**: 至少记录错误到日志。

---

### 22. **不必要的类型转换 - vector_search.go:26**
**位置**: `internal/store/vector_search.go` - `CosineSimilarity()`

```go
result := dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
```

**优化建议**:
```go
// 使用 float32 版本的 sqrt
import "math"

func sqrt32(x float32) float32 {
    return float32(math.Sqrt(float64(x)))
}

result := dot / (sqrt32(normA) * sqrt32(normB))
```

---

### 23. **重复代码 - hierarchical.go 和 vector_search.go**
**位置**: 多处

**问题**: `CosineSimilarity` 和 `CosineSimilarityNormalized` 在多个地方使用，但实现分散。

**建议**: 统一到 `vector_opt.go` 中。

---

### 24. **缺少上下文取消支持**
**位置**: 所有数据库操作

**问题**: 所有数据库查询都没有使用 `context.Context`，无法取消长时间运行的查询。

**建议**:
```go
func (s *sqliteStore) Search(ctx context.Context, queryVector []float32, ...) ([]SearchResult, error) {
    rows, err := s.db.QueryContext(ctx, query, args...)
    // ...
}
```

---

### 25. **内存分配优化 - vector_opt.go:125**
**位置**: `internal/store/vector_opt.go` - `parallelVectorSearch()`

```go
items := make([]item, 0, 1024)  // 硬编码容量
```

**建议**: 根据实际数据量动态调整，或者使用配置。

---

## 总结

### 严重问题统计
- SQL 注入风险: 1
- 资源泄漏/错误处理: 5

### 中等问题统计
- 边界条件: 3
- 并发安全: 2
- 错误处理: 4
- 参数验证: 2

### 轻微问题统计
- 代码质量: 10

### 优先修复顺序
1. **立即修复**: 所有 `continue` 静默忽略错误的地方 (问题 2-5)
2. **高优先级**: FTS5 转义逻辑 (问题 6-8)、并发安全 (问题 9-10)
3. **中优先级**: 参数验证 (问题 14-15)、错误处理 (问题 13)
4. **低优先级**: 代码优化和重构 (问题 16-25)

### 建议的下一步
1. 添加单元测试覆盖所有边界条件
2. 添加集成测试验证并发安全性
3. 使用 `go vet` 和 `staticcheck` 进行静态分析
4. 添加 fuzzing 测试 FTS5 转义逻辑
5. 添加 benchmark 验证性能优化效果
