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

	// Strip FTS5 special characters instead of quoting entire query
	// (quoting turns term queries into phrase queries, hurting recall)
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune("+-\"*(){}[]?!@#$%^&:;,.<>~/\\|", r) {
			return ' '
		}
		return r
	}, trimmed)

	// Collapse whitespace
	parts := strings.Fields(cleaned)
	if len(parts) == 0 {
		return "\"\""
	}

	result := strings.Join(parts, " ")

	// Check for FTS5 operators in the cleaned result
	if strings.Contains(result, " AND ") ||
		strings.Contains(result, " OR ") ||
		strings.Contains(result, " NOT ") {
		escaped := strings.ReplaceAll(result, "\"", "\"\"")
		return "\"" + escaped + "\""
	}

	return result
}
