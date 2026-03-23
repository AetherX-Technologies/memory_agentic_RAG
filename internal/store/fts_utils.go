package store

import (
	"strings"
	"unicode"
)

// EscapeFTS5Query 转义 FTS5 特殊字符 + 中文单字切分（兼容无中文分词器的场景）
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

	// Strip FTS5 special characters
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune("+-\"*(){}[]?!@#$%^&:;,.<>~/\\|", r) {
			return ' '
		}
		return r
	}, trimmed)

	// For CJK characters: split into individual characters with spaces
	// This enables FTS5 default tokenizer to match Chinese text
	var buf strings.Builder
	for _, r := range cleaned {
		if isCJKChar(r) {
			buf.WriteRune(' ')
			buf.WriteRune(r)
			buf.WriteRune(' ')
		} else {
			buf.WriteRune(r)
		}
	}

	parts := strings.Fields(buf.String())
	if len(parts) == 0 {
		return "\"\""
	}

	result := strings.Join(parts, " ")

	// Check for FTS5 operators
	if strings.Contains(result, " AND ") ||
		strings.Contains(result, " OR ") ||
		strings.Contains(result, " NOT ") {
		escaped := strings.ReplaceAll(result, "\"", "\"\"")
		return "\"" + escaped + "\""
	}

	return result
}

func isCJKChar(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) // Katakana
}
