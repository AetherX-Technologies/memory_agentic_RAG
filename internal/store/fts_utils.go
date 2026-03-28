package store

import (
	"strings"
	"unicode"
)

// Chinese stop words — filtered from queries to improve BM25 recall.
// Includes pronouns, particles, common auxiliaries, prepositions, and question words.
// These high-frequency single characters pollute BM25 scoring for character-level CJK tokenization.
var cjkStopWords = map[string]bool{
	// 代词
	"我": true, "你": true, "他": true, "她": true, "它": true, "们": true, "这": true, "那": true, "其": true,
	// 助词/语气词
	"的": true, "了": true, "着": true, "过": true, "么": true, "呢": true, "吧": true,
	"啊": true, "哦": true, "嗯": true, "哈": true, "呀": true, "吗": true,
	// 判断/存在
	"是": true, "在": true, "有": true,
	// 连词/介词（仅保留纯虚词，避免误杀 "对齐/前端/后端/时间" 等含实义字的词）
	"和": true, "与": true, "跟": true, "从": true, "把": true, "被": true,
	"让": true, "给": true, "将": true, "而": true, "以": true, "于": true, "之": true,
	// 副词
	"不": true, "也": true, "都": true, "就": true, "还": true, "再": true, "又": true, "已": true,
	"很": true, "太": true, "更": true,
	// 常用动词（语义弱）
	"要": true, "会": true, "能": true, "可": true, "去": true, "到": true, "说": true,
	// 量词/方位（仅保留纯虚义）
	"个": true, "一": true, "上": true, "下": true,
	// 疑问词
	"什": true, "啥": true,
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

	// Strip all punctuation (ASCII + full-width + CJK punctuation), keep letters/digits/spaces
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			return r
		}
		return ' '
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

	// Filter stop words, empty tokens, and deduplicate
	rawParts := strings.Fields(buf.String())
	seen := make(map[string]bool, len(rawParts))
	var parts []string
	for _, p := range rawParts {
		if !cjkStopWords[p] && len(p) > 0 && !seen[p] {
			seen[p] = true
			parts = append(parts, p)
		}
	}

	if len(parts) == 0 {
		// All tokens were stop words, fall back to original (deduplicated) without stop word filter
		seen2 := make(map[string]bool, len(rawParts))
		for _, p := range rawParts {
			if len(p) > 0 && !seen2[p] {
				seen2[p] = true
				parts = append(parts, p)
			}
		}
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
