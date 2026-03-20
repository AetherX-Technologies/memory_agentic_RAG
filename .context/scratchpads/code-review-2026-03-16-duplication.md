# 代码重复审查报告

**日期**: 2026-03-16
**审查范围**: internal/api, internal/store

---

## 1. escapeFTS5Query 函数重复（3处）

### 位置
- `internal/api/handler.go:42-64`
- `internal/store/hierarchical.go:21-39`
- `internal/store/bm25.go:6-23` (命名为 escapeFTS5QueryBM25)

### 问题
完全相同的逻辑在 3 个文件中重复定义，违反 DRY 原则。

### 重构方案
创建 `internal/store/fts_utils.go`，提供统一的 FTS5 工具函数：

```go
package store

import "strings"

// EscapeFTS5Query 转义 FTS5 特殊字符，防止语法错误
func EscapeFTS5Query(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "\"\""
	}
	upper := strings.ToUpper(trimmed)
	if upper == "AND" || upper == "OR" || upper == "NOT" {
		return "\"" + trimmed + "\""
	}
	if strings.ContainsAny(trimmed, "+-\"*()") ||
		strings.Contains(trimmed, " AND ") ||
		strings.Contains(trimmed, " OR ") ||
		strings.Contains(trimmed, " NOT ") {
		escaped := strings.ReplaceAll(trimmed, "\"", "\"\"")
		return "\"" + escaped + "\""
	}
	return trimmed
}
```

**修改点**：
- handler.go: 删除本地函数，导入 `store.EscapeFTS5Query`
- hierarchical.go: 删除本地函数，使用 `EscapeFTS5Query`
- bm25.go: 删除 `escapeFTS5QueryBM25`，使用 `EscapeFTS5Query`

---

## 2. Memory 结构体构建逻辑重复（3处）

### 位置
- `internal/store/vector_search.go:96-108`
- `internal/store/bm25.go:81-93`
- `internal/store/hierarchical.go:318-325` (部分)

### 问题
从数据库行扫描并构建 Memory 结构体的代码模式重复，特别是 `hierarchy_path` 空值处理。

### 重复模式
```go
m := Memory{
	ID:             memoryID,
	Text:           text,
	Category:       category,
	Scope:          scope,
	Importance:     importance,
	Timestamp:      timestamp,
	Metadata:       metadata,
	HierarchyLevel: hierarchyLevel,
}
if hierarchyPath != nil {
	m.HierarchyPath = *hierarchyPath
}
```

### 重构方案
在 `internal/store/types.go` 添加构造函数：

```go
// NewMemoryFromRow 从数据库行构建 Memory 对象
func NewMemoryFromRow(id, text, category, scope, metadata string,
	hierarchyPath *string, importance float64, timestamp int64, hierarchyLevel int) Memory {
	m := Memory{
		ID:             id,
		Text:           text,
		Category:       category,
		Scope:          scope,
		Importance:     importance,
		Timestamp:      timestamp,
		Metadata:       metadata,
		HierarchyLevel: hierarchyLevel,
	}
	if hierarchyPath != nil {
		m.HierarchyPath = *hierarchyPath
	}
	return m
}
```

**修改点**：
- vector_search.go:96-108 → 调用 `NewMemoryFromRow`
- bm25.go:81-93 → 调用 `NewMemoryFromRow`
- hierarchical.go:318-325 → 调用 `NewMemoryFromRow`

---

## 3. Scope 过滤条件构建重复（2处）

### 位置
- `internal/store/vector_search.go:48-58`
- `internal/store/bm25.go:32-42`

### 问题
构建 SQL `IN (?, ?, ...)` 子句的逻辑重复。

### 重复模式
```go
scopeFilter := ""
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
```

### 重构方案
在 `internal/store/fts_utils.go` 添加（或创建 `sql_utils.go`）：

```go
// BuildScopeFilter 构建 scope IN 子句
// 返回 SQL 片段和参数列表
func BuildScopeFilter(scopes []string, prefix string) (string, []interface{}) {
	if len(scopes) == 0 {
		return "", nil
	}
	placeholders := strings.Repeat("?,", len(scopes)-1) + "?"
	args := make([]interface{}, len(scopes))
	for i, s := range scopes {
		args[i] = s
	}
	return prefix + " IN (" + placeholders + ")", args
}
```

**注意**: hierarchical.go 已有 `buildScopeFilter` 函数（第78行），可以统一使用。

---

## 4. 其他发现

### 4.1 convertToInterfaces 工具函数
`hierarchical.go:69-75` 已实现，可复用到其他文件。

### 4.2 escapeLike 函数
`hierarchical.go:61-66` 仅在 hierarchical.go 使用，暂不需要提取。

---

## 重构优先级

**P0 - 立即修复**:
1. escapeFTS5Query 函数重复（影响维护性）

**P1 - 高优先级**:
2. Memory 构建逻辑重复（代码量大，易出错）

**P2 - 中优先级**:
3. Scope 过滤条件构建（已有部分实现，需统一）

---

## 实施步骤

1. 创建 `internal/store/fts_utils.go`
2. 移动 `EscapeFTS5Query` 到新文件
3. 在 `types.go` 添加 `NewMemoryFromRow`
4. 统一 `buildScopeFilter` 实现
5. 更新所有调用点
6. 运行测试验证

---

**预计收益**:
- 减少 ~80 行重复代码
- 统一 FTS5 转义逻辑，降低 SQL 注入风险
- 简化 Memory 对象构建，减少空指针错误
