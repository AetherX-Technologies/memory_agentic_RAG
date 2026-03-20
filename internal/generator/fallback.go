package generator

import (
	"strings"
	"unicode"
)

// extractL0Fallback generates a simple L0 abstract by extracting the first sentence.
// Used when LLM is unavailable.
func extractL0Fallback(content string) string {
	// Try Chinese sentence ending first
	if idx := strings.IndexRune(content, '。'); idx >= 0 {
		return content[:idx+len("。")]
	}
	// Try English sentence ending
	for i, r := range content {
		if (r == '.' || r == '!' || r == '?') && i > 0 {
			// Check it's followed by space or end (not a decimal/abbreviation)
			rest := content[i+1:]
			if rest == "" || (len(rest) > 0 && unicode.IsSpace(rune(rest[0]))) {
				return content[:i+1]
			}
		}
	}
	// No sentence ending found — truncate
	return truncateRunes(content, 80)
}

// extractL1Fallback generates a simple L1 overview by extracting the first few sentences.
// Used when LLM is unavailable.
func extractL1Fallback(content string) string {
	// Collect up to 5 sentences or 500 chars, whichever comes first
	const maxSentences = 5
	const maxChars = 500

	var result strings.Builder
	sentences := 0
	runes := []rune(content)

	for i := 0; i < len(runes) && result.Len() < maxChars && sentences < maxSentences; i++ {
		r := runes[i]
		result.WriteRune(r)

		if r == '。' || r == '！' || r == '？' {
			sentences++
		} else if (r == '.' || r == '!' || r == '?') && i+1 < len(runes) && unicode.IsSpace(runes[i+1]) {
			sentences++
		}
	}

	s := strings.TrimSpace(result.String())
	if s == "" {
		return truncateRunes(content, maxChars)
	}
	return s
}
