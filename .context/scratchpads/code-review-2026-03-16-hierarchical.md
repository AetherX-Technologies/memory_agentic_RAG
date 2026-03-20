# 分层检索代码审查报告

**审查日期**: 2026-03-16
**审查范围**: 分层混合检索实现
**审查文件**:
- internal/store/hierarchical.go (新文件)
- internal/store/schema.go (迁移函数)
- internal/store/types.go (数据模型)
- internal/api/handler.go (API 处理)
- internal/store/store.go (初始化)

---

## 1. 设计一致性检查 ✅

### 1.1 数据模型 ✅
- **types.go**: 正确添加了 `HierarchyPath` 和 `HierarchyLevel` 字段
- **schema.go**: 迁移函数 `migrateHierarchy()` 实现了幂等性检查
- **一致性**: 与设计文档完全一致

### 1.2 核心算法 ✅
- **parseHierarchyLevels()**: 正确实现层次路径解析
- **calculateLevelWeight()**: 权重计算符合设计（0.8 衰减系数）
- **HierarchicalHybridSearch()**: 主流程与设计文档一致

---

## 2. 编译错误检查 ✅

### 2.1 Store 包编译 ✅
```bash
go build ./internal/store
# 无错误输出
```

### 2.2 其他包编译 ⚠️
```
cmd/realtest/main.go:15:2: missing init expr for jinaAPIKey
cmd/reranktest/real_api.go:16:2: missing init expr for jinaAPIKey
```
**结论**: 这些错误与分层检索无关，是测试代码的问题

---

## 3. 逻辑错误与 Bug 🔴

### 3.1 严重问题

#### 问题 1: 向量检索返回类型不一致 🔴
**位置**: `hierarchical.go:86`
```go
score := float64(CosineSimilarity(queryVec, m.Vector))
```
**问题**: `CosineSimilarity()` 返回 `float32`，但在 `hierarchical.go` 中转换为 `float64`，而在 `vector_search.go:93` 中也有类似转换。这是正确的，但需要确保一致性。

**实际检查**:
- `vector_search.go:10`: `func CosineSimilarity(a, b []float32) float32`
- 转换是必要的，因为 `SearchResult.Score` 是 `float64`

**结论**: ✅ 无问题

#### 问题 2: 空向量处理 🔴
**位置**: `handler.go:100`
```go
queryVec := []float32{} // 空向量表示未向量化
```
**问题**:
1. 空向量会导致 `CosineSimilarity()` 返回 0（因为 `len(a) == 0` 检查）
2. 所有向量检索结果的分数都是 0
3. 这会导致分层检索完全失效

**影响**:
- 向量检索无效，只有 BM25 检索有效
- 违反了设计文档的要求（需要向量化查询）

**建议**:
```go
// 方案 1: 如果没有 embedder，返回错误
if len(queryVec) == 0 {
    writeError(w, http.StatusNotImplemented, "embedder not configured")
    return
}

// 方案 2: 如果没有 embedder，只使用 BM25
if len(queryVec) == 0 {
    results, err = h.store.BM25Search(query, limit, scopes)
}
```

#### 问题 3: Insert 未处理层次字段 🔴
**位置**: `store.go:103-107`
```go
_, err = tx.Exec(`
    INSERT INTO memories (id, text, category, scope, importance, timestamp, metadata)
    VALUES (?, ?, ?, ?, ?, ?, ?)`,
    memory.ID, memory.Text, memory.Category, memory.Scope,
    memory.Importance, memory.Timestamp, memory.Metadata)
```
**问题**:
- `hierarchy_path` 和 `hierarchy_level` 字段未包含在 INSERT 语句中
- 即使 `Memory` 结构体有这些字段，也不会被存储

**影响**:
- 无法存储层次信息
- 分层检索无法工作

**修复**:
```go
_, err = tx.Exec(`
    INSERT INTO memories (id, text, category, scope, importance, timestamp, metadata, hierarchy_path, hierarchy_level)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    memory.ID, memory.Text, memory.Category, memory.Scope,
    memory.Importance, memory.Timestamp, memory.Metadata,
    memory.HierarchyPath, memory.HierarchyLevel)
```

#### 问题 4: Get 未读取层次字段 🔴
**位置**: `store.go:140-144`
```go
err := s.db.QueryRow(`
    SELECT id, text, category, scope, importance, timestamp, metadata
    FROM memories WHERE id = ?`, id).Scan(
    &memory.ID, &memory.Text, &memory.Category, &memory.Scope,
    &memory.Importance, &memory.Timestamp, &memory.Metadata)
```
**问题**: 未读取 `hierarchy_path` 和 `hierarchy_level`

**修复**:
```go
err := s.db.QueryRow(`
    SELECT id, text, category, scope, importance, timestamp, metadata, hierarchy_path, hierarchy_level
    FROM memories WHERE id = ?`, id).Scan(
    &memory.ID, &memory.Text, &memory.Category, &memory.Scope,
    &memory.Importance, &memory.Timestamp, &memory.Metadata,
    &memory.HierarchyPath, &memory.HierarchyLevel)
```

### 3.2 中等问题

#### 问题 5: BM25 查询语法错误 ⚠️
**位置**: `hierarchical.go:116`
```go
WHERE fts.content MATCH ?
```
**问题**: FTS5 表的查询应该使用表名而不是列名

**检查**: 查看 `bm25.go:27`
```go
WHERE fts_memories MATCH ?
```
**结论**: `bm25.go` 是正确的，但 `hierarchical.go` 使用了 `fts.content MATCH`，这可能导致错误

**修复**:
```go
WHERE fts MATCH ?
```

#### 问题 6: 错误处理不完整 ⚠️
**位置**: `hierarchical.go:209-216`
```go
vectorResults, err := s.vectorSearchInLevel(queryVec, level, limit*2, scopes)
if err != nil {
    continue  // 静默忽略错误
}
```
**问题**:
- 错误被静默忽略，难以调试
- 如果所有层都失败，会返回空结果而不是错误

**建议**: 至少记录错误日志

### 3.3 轻微问题

#### 问题 7: 代码重复 ℹ️
**位置**: `hierarchical.go:142-167` vs `hybrid.go:69-106`
- `rrfFusion()` 函数在两个文件中实现不同
- `hierarchical.go` 使用简单的 RRF 算法
- `hybrid.go` 使用 BM25 加成算法

**问题**:
- 设计文档要求使用 RRF 融合（k=60）
- `hierarchical.go` 的实现是正确的
- `hybrid.go` 的实现不是标准 RRF

**建议**: 统一融合算法，或明确两者的使用场景

#### 问题 8: 性能优化缺失 ℹ️
**位置**: `hierarchical.go:208`
```go
for i, level := range levels {
    vectorResults, err := s.vectorSearchInLevel(queryVec, level, limit*2, scopes)
    // ...
}
```
**问题**: 层级检索是串行的，可以并行化

**建议**: 使用 goroutine 并行搜索各层

---

## 4. 错误处理检查 ⚠️

### 4.1 完整性
- ✅ 参数验证: `limit` 范围检查
- ⚠️ 错误传播: 部分错误被静默忽略
- ✅ 数据库错误: 正确处理
- ⚠️ 空结果处理: 未明确处理所有层都失败的情况

### 4.2 建议
```go
// 在 HierarchicalHybridSearch 中添加
errorCount := 0
for i, level := range levels {
    vectorResults, err := s.vectorSearchInLevel(...)
    if err != nil {
        errorCount++
        if errorCount == len(levels) {
            return nil, fmt.Errorf("all levels failed")
        }
        continue
    }
    // ...
}
```

---

## 5. 性能问题 ⚠️

### 5.1 时间复杂度
- **层级解析**: O(L)，L 是层数 ✅
- **层内检索**: O(N/L)，N 是总记忆数 ✅
- **跨层聚合**: O(K log K)，K 是候选数 ✅
- **总体**: O(N + K log K) ✅

### 5.2 潜在瓶颈
1. **串行层级搜索**: 可并行化
2. **重复向量反序列化**: 每层都要反序列化相同的向量
3. **SQL 查询**: 每层一次查询，可以合并

### 5.3 优化建议
```go
// 优化 1: 并行层级搜索
var wg sync.WaitGroup
resultsChan := make(chan []SearchResult, len(levels))

for i, level := range levels {
    wg.Add(1)
    go func(i int, level string) {
        defer wg.Done()
        // 执行检索
        resultsChan <- fusedResults
    }(i, level)
}

wg.Wait()
close(resultsChan)

// 优化 2: 合并 SQL 查询
WHERE m.hierarchy_path IN (?, ?, ?) OR m.hierarchy_path LIKE ...
```

---

## 6. 代码重复检查 ⚠️

### 6.1 重复代码

#### 重复 1: Scope 过滤构建
**位置**:
- `hierarchical.go:48-57`
- `hierarchical.go:101-110`
- `vector_search.go:47-58`
- `bm25.go:5-18`

**建议**: 提取为公共函数
```go
func buildScopeFilter(scopes []string) (string, []interface{}) {
    if len(scopes) == 0 {
        return "", nil
    }
    placeholders := make([]string, len(scopes))
    args := make([]interface{}, len(scopes))
    for i, s := range scopes {
        placeholders[i] = "?"
        args[i] = s
    }
    return " AND m.scope IN (" + strings.Join(placeholders, ",") + ")", args
}
```

#### 重复 2: 相似度计算
**位置**:
- `hierarchical.go:85`: `CosineSimilarity(queryVec, m.Vector)`
- `vector_search.go:93`: `CosineSimilarityNormalized(queryNorm, vector)`
- `vector_opt.go:183`: `CosineSimilarityNormalized(queryNorm, items[i].vector)`

**问题**:
- `hierarchical.go` 使用 `CosineSimilarity`（未归一化）
- 其他地方使用 `CosineSimilarityNormalized`（已归一化）
- 不一致可能导致分数不可比

**建议**: 统一使用归一化版本

---

## 7. 与设计文档对比

### 7.1 完全实现 ✅
- ✅ 数据模型扩展（hierarchy_path, hierarchy_level）
- ✅ 层次路径解析
- ✅ 层级权重计算
- ✅ RRF 融合
- ✅ 跨层聚合
- ✅ 全局 fallback
- ✅ API 扩展（current_path 参数）

### 7.2 部分实现 ⚠️
- ⚠️ 评分管道: 调用了 `applyScoring()`，但未验证是否是 12 阶段
- ⚠️ 向量化查询: handler 中使用空向量

### 7.3 未实现 ❌
- ❌ 配置选项 `HierarchicalConfig`（设计文档第 5 节）
- ❌ 单元测试（设计文档第 8.1 节）
- ❌ 集成测试（设计文档第 8.2 节）

---

## 8. 关键 Bug 总结 🔴

### 必须修复（阻塞性）
1. **Insert 未存储层次字段** - 导致功能完全不可用
2. **Get 未读取层次字段** - 导致数据丢失
3. **Handler 使用空向量** - 导致向量检索失效

### 应该修复（影响功能）
4. **BM25 查询语法** - 可能导致 FTS5 查询失败
5. **错误处理不完整** - 难以调试

### 建议修复（优化）
6. **代码重复** - 降低可维护性
7. **性能优化** - 串行层级搜索
8. **相似度计算不一致** - 可能影响结果质量

---

## 9. 修复优先级

### P0 (立即修复)
1. 修复 `store.go` 的 Insert/Get 方法，添加层次字段
2. 修复 `handler.go` 的空向量问题

### P1 (尽快修复)
3. 修复 `hierarchical.go` 的 BM25 查询语法
4. 统一相似度计算方法

### P2 (后续优化)
5. 提取公共函数，减少代码重复
6. 添加单元测试
7. 并行化层级搜索

---

## 10. 测试建议

### 10.1 单元测试
```go
func TestParseHierarchyLevels(t *testing.T) {
    tests := []struct {
        input    string
        expected []string
    }{
        {"/project/src/auth", []string{"/project", "/project/src", "/project/src/auth"}},
        {"", []string{}},
        {"/", []string{"/"}},
    }
    // ...
}

func TestCalculateLevelWeight(t *testing.T) {
    // 验证权重衰减
}

func TestHierarchicalSearch(t *testing.T) {
    // 端到端测试
}
```

### 10.2 集成测试
1. 创建多层次记忆
2. 从不同层级搜索
3. 验证结果顺序符合权重

---

## 11. 总体评价

### 优点 ✅
- 架构设计清晰，与文档一致
- 核心算法实现正确
- 代码结构良好，易于理解
- 幂等性迁移设计优秀

### 缺点 ❌
- 存在 3 个阻塞性 Bug
- 缺少单元测试
- 部分代码重复
- 错误处理不够完善

### 建议
1. **立即修复 P0 Bug**，否则功能无法使用
2. 添加单元测试，确保核心逻辑正确
3. 重构公共代码，减少重复
4. 考虑性能优化（并行化）

---

**审查结论**: 代码整体质量良好，但存在 3 个必须修复的阻塞性 Bug。修复后可以进入测试阶段。
