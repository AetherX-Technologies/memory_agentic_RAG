# HybridMem-RAG 安全审查报告（第二轮）

**审查日期**: 2026-03-17  
**审查范围**: SQL注入、路径遍历、输入验证、资源限制、信息泄露

---

## 🔴 高危漏洞

### 1. SQL扩展加载路径遍历漏洞 (CRITICAL)

**文件**: `internal/store/store.go:29-34`

```go
extPath := filepath.Join("lib", extName)
absPath, err := filepath.Abs(extPath)
if err != nil {
    return fmt.Errorf("failed to get extension path: %w", err)
}
return conn.LoadExtension(absPath, "sqlite3_simple_init")
```

**风险**:
- 使用相对路径 `lib/` 构建扩展路径
- `filepath.Abs()` 基于当前工作目录，攻击者可通过修改 CWD 加载任意 .so/.dylib/.dll
- 可导致任意代码执行

**攻击场景**:
```bash
cd /tmp/malicious
ln -s /tmp/evil.dylib lib/libsimple.dylib
./hybridmem-rag  # 加载恶意扩展
```

**修复建议**:
```go
// 使用可执行文件目录作为基准
execPath, err := os.Executable()
if err != nil {
    return fmt.Errorf("failed to get executable path: %w", err)
}
baseDir := filepath.Dir(execPath)
extPath := filepath.Join(baseDir, "lib", extName)

// 验证路径在允许目录内
if !strings.HasPrefix(filepath.Clean(extPath), filepath.Clean(baseDir)) {
    return fmt.Errorf("extension path outside allowed directory")
}

// 验证文件存在且为常规文件
info, err := os.Stat(extPath)
if err != nil || !info.Mode().IsRegular() {
    return fmt.Errorf("invalid extension file")
}
```

---

## 🟡 中危漏洞

### 2. API 路径提取逻辑缺陷

**文件**: `internal/api/handler.go:41-47`

```go
func extractMemoryID(path string) (string, error) {
    id := strings.TrimPrefix(path, "/api/memories/")
    if id == "" || id == "search" || id == "stats" {
        return "", fmt.Errorf("invalid memory id")
    }
    return id, nil
}
```

**风险**:
- 未验证 ID 格式（应为 UUID）
- 允许路径遍历字符 `../`
- 允许特殊字符注入

**攻击场景**:
```bash
DELETE /api/memories/../../../etc/passwd
DELETE /api/memories/<script>alert(1)</script>
```

**修复建议**:
```go
func extractMemoryID(path string) (string, error) {
    id := strings.TrimPrefix(path, "/api/memories/")
    if id == "" || id == "search" || id == "stats" {
        return "", fmt.Errorf("invalid memory id")
    }
    
    // 验证 UUID 格式
    if _, err := uuid.Parse(id); err != nil {
        return "", fmt.Errorf("invalid UUID format")
    }
    
    return id, nil
}
```

### 3. 层级路径解析未验证

**文件**: `internal/store/hierarchical.go:22-32`

```go
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
```

**风险**:
- 未验证路径格式
- 允许 `..` 路径遍历
- 允许空段和特殊字符

**攻击场景**:
```go
parseHierarchyLevels("/../../etc/passwd")
parseHierarchyLevels("//admin//secret")
parseHierarchyLevels("/a/../b")
```

**修复建议**:
```go
func parseHierarchyLevels(path string) ([]string, error) {
    if path == "" {
        return []string{}, nil
    }
    
    // 清理路径
    cleaned := filepath.Clean("/" + strings.Trim(path, "/"))
    if cleaned == "/" {
        return []string{}, nil
    }
    
    // 检测路径遍历
    if strings.Contains(cleaned, "..") {
        return nil, fmt.Errorf("path traversal detected")
    }
    
    parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
    
    // 验证每个段
    for _, part := range parts {
        if part == "" || part == "." || part == ".." {
            return nil, fmt.Errorf("invalid path segment: %s", part)
        }
        // 可选：限制字符集
        if !isValidPathSegment(part) {
            return nil, fmt.Errorf("invalid characters in path")
        }
    }
    
    levels := make([]string, len(parts))
    for i := range parts {
        levels[i] = "/" + strings.Join(parts[:i+1], "/")
    }
    return levels, nil
}

func isValidPathSegment(s string) bool {
    // 只允许字母、数字、下划线、连字符
    for _, r := range s {
        if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || 
             (r >= '0' && r <= '9') || r == '_' || r == '-') {
            return false
        }
    }
    return true
}
```

### 4. Scope 参数未验证

**文件**: `internal/api/handler.go:92-100`

```go
scopesParam := r.URL.Query().Get("scope")
var scopes []string
if scopesParam != "" {
    for _, s := range strings.Split(scopesParam, ",") {
        if trimmed := strings.TrimSpace(s); trimmed != "" {
            scopes = append(scopes, trimmed)
        }
    }
}
```

**风险**:
- 未限制 scope 数量（DoS）
- 未验证 scope 格式
- 可能导致 SQL 性能问题

**修复建议**:
```go
const maxScopes = 10
const maxScopeLength = 64

scopesParam := r.URL.Query().Get("scope")
var scopes []string
if scopesParam != "" {
    parts := strings.Split(scopesParam, ",")
    if len(parts) > maxScopes {
        writeError(w, http.StatusBadRequest, "too many scopes")
        return
    }
    
    for _, s := range parts {
        trimmed := strings.TrimSpace(s)
        if trimmed == "" {
            continue
        }
        if len(trimmed) > maxScopeLength {
            writeError(w, http.StatusBadRequest, "scope too long")
            return
        }
        // 验证格式（字母数字+下划线）
        if !isValidScope(trimmed) {
            writeError(w, http.StatusBadRequest, "invalid scope format")
            return
        }
        scopes = append(scopes, trimmed)
    }
}
```

---

## 🟢 低危问题

### 5. FTS5 转义不完整

**文件**: `internal/store/fts_utils.go:6-29`

**问题**:
- 未处理 `^` 列前缀操作符
- 未处理 NEAR 操作符
- 边界情况：连续引号 `""`

**建议**: 当前实现已足够，但可添加单元测试覆盖边界情况。

### 6. LIKE 转义正确但可优化

**文件**: `internal/store/hierarchical.go:41-46`

```go
func escapeLike(s string) string {
    s = strings.ReplaceAll(s, "\\", "\\\\")
    s = strings.ReplaceAll(s, "_", "\\_")
    s = strings.ReplaceAll(s, "%", "\\%")
    return s
}
```

**状态**: ✅ 正确实现，使用 `ESCAPE '\'` 子句

### 7. 资源限制不足

**问题**:
- `handler.go:29` 限制请求体 10MB，但未限制向量维度
- `hierarchical.go:201` 限制 limit 1-100，但未限制层级深度

**建议**:
```go
const maxHierarchyDepth = 10

func parseHierarchyLevels(path string) ([]string, error) {
    // ... 现有验证 ...
    
    if len(parts) > maxHierarchyDepth {
        return nil, fmt.Errorf("hierarchy too deep (max %d)", maxHierarchyDepth)
    }
    
    // ...
}
```

---

## ✅ 已正确实现的安全措施

1. **SQL 参数化查询**: 所有 SQL 使用 `?` 占位符，无字符串拼接
2. **FTS5 转义**: `EscapeFTS5Query()` 正确处理特殊字符
3. **LIKE 转义**: `escapeLike()` + `ESCAPE '\'` 防止通配符注入
4. **请求体大小限制**: `MaxBytesReader(10MB)`
5. **Limit 范围验证**: 1-100 限制
6. **向量维度验证**: `store.go:154` 检查维度匹配
7. **事务使用**: Insert 操作使用事务保证一致性

---

## 修复优先级

### P0 (立即修复)
1. ✅ SQL扩展加载路径遍历 → 使用可执行文件目录 + 路径验证

### P1 (本周修复)
2. ✅ API 路径提取 → 添加 UUID 验证
3. ✅ 层级路径解析 → 添加路径遍历检测

### P2 (下周修复)
4. ✅ Scope 参数验证 → 添加数量和格式限制
5. ✅ 层级深度限制 → 防止深度遍历 DoS

---

## 测试建议

创建安全测试套件：

```go
// internal/store/security_test.go
func TestPathTraversalPrevention(t *testing.T) {
    malicious := []string{
        "/../../../etc/passwd",
        "/a/../b",
        "//admin",
        "/a//b",
    }
    for _, path := range malicious {
        _, err := parseHierarchyLevels(path)
        if err == nil {
            t.Errorf("should reject malicious path: %s", path)
        }
    }
}

func TestSQLInjectionPrevention(t *testing.T) {
    queries := []string{
        "'; DROP TABLE memories; --",
        "1' OR '1'='1",
        "UNION SELECT * FROM sqlite_master",
    }
    for _, q := range queries {
        escaped := EscapeFTS5Query(q)
        // 验证转义后无法执行 SQL
    }
}
```

---

**审查结论**: 发现 1 个高危漏洞、4 个中危漏洞、3 个低危问题。建议立即修复扩展加载路径问题。
