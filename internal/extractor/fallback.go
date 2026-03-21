package extractor

import (
	"strings"
)

// extractFallback performs rule-based memory extraction when LLM is unavailable.
// It detects patterns like "我是/我在/我喜欢/记住/以后/请/不要" in user messages.
func extractFallback(messages []Message) []RawMemory {
	var results []RawMemory
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		results = append(results, extractFromText(m.Content)...)
	}
	return results
}

// Pattern rules for rule-based extraction.
var rules = []struct {
	Prefixes []string
	Type     string
	Importance float64
}{
	{[]string{"我是", "我在", "我叫", "我的名字", "我的职业", "我的工作"}, "fact", 0.7},
	{[]string{"我喜欢", "我偏好", "我习惯", "我更喜欢"}, "preference", 0.6},
	{[]string{"我不喜欢", "我讨厌", "我不想"}, "preference", 0.6},
	{[]string{"不要", "别", "记住", "以后", "每次", "总是", "一直", "请以后", "请用", "请不要", "请记住"}, "instruction", 0.8},
	{[]string{"我会", "我擅长", "我学过", "我用过", "我做过"}, "skill", 0.5},
}

// extractFromText applies pattern matching to a single text.
func extractFromText(text string) []RawMemory {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// Split by Chinese/English punctuation
	sentences := splitSentences(text)
	var results []RawMemory

	for _, sent := range sentences {
		sent = strings.TrimSpace(sent)
		if len([]rune(sent)) < 4 {
			continue
		}

		for _, rule := range rules {
			if matchesAny(sent, rule.Prefixes) {
				results = append(results, RawMemory{
					Content:    sent,
					Type:       rule.Type,
					Importance: rule.Importance,
					Confidence: 0.3, // Low confidence for rule-based extraction
				})
				break // one match per sentence
			}
		}
	}

	return results
}

// matchesAny returns true if text starts with any of the prefixes.
// Uses HasPrefix to avoid false positives on common tokens like "请" appearing mid-sentence.
func matchesAny(text string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// splitSentences splits text by common sentence delimiters.
func splitSentences(text string) []string {
	// Split by Chinese and English sentence-ending punctuation
	var result []string
	var current strings.Builder

	for _, r := range text {
		current.WriteRune(r)
		if r == '。' || r == '！' || r == '？' || r == '；' || r == '，' ||
			r == '.' || r == '!' || r == '?' || r == ';' || r == ',' || r == '\n' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				result = append(result, s)
			}
			current.Reset()
		}
	}

	// Remaining text
	s := strings.TrimSpace(current.String())
	if s != "" {
		result = append(result, s)
	}

	return result
}
