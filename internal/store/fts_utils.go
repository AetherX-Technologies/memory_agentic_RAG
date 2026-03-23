package store

import (
	"strings"
	"unicode"
)

// Chinese stop words — filtered from queries to improve BM25 recall
var cjkStopWords = map[string]bool{
	"的": true, "了": true, "是": true, "在": true, "我": true, "有": true,
	"和": true, "就": true, "不": true, "人": true, "都": true, "一": true,
	"个": true, "上": true, "也": true, "很": true, "到": true, "说": true,
	"要": true, "去": true, "你": true, "会": true, "着": true, "没": true,
	"那": true, "他": true, "她": true, "它": true, "们": true, "这": true,
	"么": true, "什": true, "啥": true, "吗": true, "呢": true, "吧": true,
	"啊": true, "哦": true, "嗯": true, "哈": true, "呀": true,
}

// EscapeFTS5Query 转义 FTS5 特殊字符 + 中文单字切分 + OR 语义 + 停用词过滤
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

	// CJK character splitting
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

	// Filter stop words and empty tokens
	rawParts := strings.Fields(buf.String())
	var parts []string
	for _, p := range rawParts {
		if !cjkStopWords[p] && len(p) > 0 {
			parts = append(parts, p)
		}
	}

	if len(parts) == 0 {
		// All tokens were stop words, fall back to original without stop word filter
		parts = rawParts
	}
	if len(parts) == 0 {
		return "\"\""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	// Use OR to match any token (instead of default AND which requires all)
	return strings.Join(parts, " OR ")
}

func isCJKChar(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) // Katakana
}
