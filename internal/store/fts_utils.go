package store

import "strings"

// EscapeFTS5Query 转义 FTS5 特殊字符，防止语法错误
func EscapeFTS5Query(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "\"\""
	}

	// 检查是否为 FTS5 操作符
	upper := strings.ToUpper(trimmed)
	if upper == "AND" || upper == "OR" || upper == "NOT" {
		return "\"" + trimmed + "\""
	}

	// 检查是否包含 FTS5 特殊字符
	if strings.ContainsAny(trimmed, "+-\"*()") ||
		strings.Contains(trimmed, " AND ") ||
		strings.Contains(trimmed, " OR ") ||
		strings.Contains(trimmed, " NOT ") {
		// 转义内部引号并用引号包裹
		escaped := strings.ReplaceAll(trimmed, "\"", "\"\"")
		return "\"" + escaped + "\""
	}

	return trimmed
}
