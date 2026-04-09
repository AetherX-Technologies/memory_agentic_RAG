// Package tokutil provides CJK-aware token estimation for context budget management.
// Ported from lossless-claw-enhanced's estimate-tokens.ts with per-character-class weights.
package tokutil

import "unicode"

// Per-character token weights (empirically validated against cl100k_base tokenizer):
//   ASCII/Latin: ~0.25 tokens/char (4 chars per token)
//   CJK ideographs: ~1.5 tokens/char (most CJK chars become 1-2 tokens)
//   Emoji/supplementary: ~2.0 tokens/char (encoded as multiple tokens)
//   Whitespace/punctuation: ~0.25 tokens/char
const (
	asciiWeight = 0.25
	cjkWeight   = 1.5
	emojiWeight = 2.0
)

// EstimateTokens returns an approximate token count for the given text.
// Uses character-class-aware heuristics that are accurate to within ~10%
// for mixed CJK/ASCII text against the cl100k_base tokenizer.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}

	var total float64
	for _, r := range s {
		switch {
		case isCJK(r):
			total += cjkWeight
		case isEmoji(r):
			total += emojiWeight
		default:
			total += asciiWeight
		}
	}

	result := int(total + 0.5) // round
	if result < 1 && len(s) > 0 {
		return 1
	}
	return result
}

// isCJK returns true for CJK Unified Ideographs, Hiragana, Katakana, Hangul,
// CJK symbols/punctuation, and fullwidth forms.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		(r >= 0x3000 && r <= 0x303F) || // CJK symbols and punctuation (。、「」〈〉　etc.)
		(r >= 0xFF00 && r <= 0xFFEF) // fullwidth forms
}

// isEmoji returns true for common emoji ranges.
func isEmoji(r rune) bool {
	return (r >= 0x1F300 && r <= 0x1F9FF) || // misc symbols, emoticons, supplemental
		(r >= 0x1FA00 && r <= 0x1FAFF) || // symbols and pictographs extended-A
		(r >= 0x1F1E6 && r <= 0x1F1FF) || // regional indicator symbols (flags)
		(r >= 0x2600 && r <= 0x27BF) || // misc symbols, dingbats
		(r >= 0xFE00 && r <= 0xFE0F) || // variation selectors
		r == 0x200D // zero-width joiner (emoji sequences)
}
