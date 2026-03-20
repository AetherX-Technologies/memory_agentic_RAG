package parser

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SplitSentences splits text into sentences, handling both Chinese and English punctuation.
// Preserves the sentence-ending punctuation at the end of each sentence.
func SplitSentences(text string) []string {
	if len(text) == 0 {
		return nil
	}

	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	i := 0
	for i < len(runes) {
		r := runes[i]
		current.WriteRune(r)

		if isSentenceEnd(r) && isTrueSentenceEnd(runes, i) {
			// Consume any trailing punctuation (e.g., "。」" or "...")
			for i+1 < len(runes) && isSentenceEndOrClose(runes[i+1]) {
				i++
				current.WriteRune(runes[i])
			}
			// Consume trailing whitespace
			for i+1 < len(runes) && unicode.IsSpace(runes[i+1]) {
				i++
			}

			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
		i++
	}

	// Remaining text as last sentence
	s := strings.TrimSpace(current.String())
	if s != "" {
		sentences = append(sentences, s)
	}

	return sentences
}

// commonAbbreviations that should not cause a sentence split when followed by a period.
var commonAbbreviations = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true,
	"sr": true, "jr": true, "vs": true, "etc": true, "fig": true,
	"vol": true, "dept": true, "est": true, "approx": true,
	"ie": true, "eg": true, "al": true, "st": true, "ave": true,
	"no": true, "op": true, "ch": true, "pp": true,
}

// isTrueSentenceEnd returns false for periods that are likely abbreviations or decimals.
// CJK sentence-enders (。！？；) always return true.
// Ellipsis (…) only returns true when followed by whitespace or end of text.
func isTrueSentenceEnd(runes []rune, pos int) bool {
	r := runes[pos]

	// CJK sentence-enders are always true boundaries
	if r == '\u3002' || r == '\uff01' || r == '\uff1f' || r == '\uff1b' {
		return true
	}

	// Ellipsis: only end sentence if followed by space or end of text
	if r == '\u2026' {
		return pos+1 >= len(runes) || unicode.IsSpace(runes[pos+1])
	}

	// '!' '?' ';' are almost always true sentence ends
	if r != '.' {
		return true
	}

	// Period-specific heuristics:

	// 1. Followed by a digit → decimal number (3.14)
	if pos+1 < len(runes) && unicode.IsDigit(runes[pos+1]) {
		return false
	}

	// 2. Not followed by space or end → likely not a sentence end (URLs, filenames)
	if pos+1 < len(runes) && !unicode.IsSpace(runes[pos+1]) {
		return false
	}

	// 3. Preceded by a single uppercase letter → abbreviation (U. S.)
	if pos >= 1 && unicode.IsUpper(runes[pos-1]) {
		if pos < 2 || !unicode.IsLetter(runes[pos-2]) {
			return false
		}
	}

	// 4. Check common abbreviations (e.g., "Dr." "vs." "etc.")
	if pos >= 1 {
		word := extractWordBefore(runes, pos)
		if commonAbbreviations[strings.ToLower(word)] {
			return false
		}
	}

	return true
}

// extractWordBefore returns the word immediately preceding position pos in runes.
func extractWordBefore(runes []rune, pos int) string {
	end := pos
	start := pos - 1
	for start >= 0 && unicode.IsLetter(runes[start]) {
		start--
	}
	start++
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

// isSentenceEnd returns true for characters that typically end a sentence.
func isSentenceEnd(r rune) bool {
	switch r {
	case '.', '!', '?', ';':
		return true
	case '。', '！', '？', '；', '…':
		return true
	}
	return false
}

// isSentenceEndOrClose returns true for sentence-end chars or closing punctuation
// that may follow a sentence ending (e.g., quotes, brackets).
func isSentenceEndOrClose(r rune) bool {
	if isSentenceEnd(r) {
		return true
	}
	switch r {
	case '"', '\'', ')', ']', '}',
		'\u300d', // 」 right corner bracket
		'\u300f', // 』 right white corner bracket
		'\u201d', // " right double quotation mark
		'\u2019', // ' right single quotation mark
		'\uff09', // ） fullwidth right parenthesis
		'\u3011': // 】 right black lenticular bracket
		return true
	}
	return false
}

// EstimateTokenCount provides a rough token count estimate without a tokenizer.
// Heuristic: CJK characters ≈ 1.5 tokens each, ASCII words ≈ 1.3 tokens each.
func EstimateTokenCount(text string) int {
	if len(text) == 0 {
		return 0
	}

	var cjkChars, asciiWords int
	inWord := false

	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size

		if isCJK(r) {
			cjkChars++
			inWord = false
		} else if unicode.IsSpace(r) || unicode.IsPunct(r) {
			inWord = false
		} else {
			if !inWord {
				asciiWords++
				inWord = true
			}
		}
	}

	// CJK: ~1.5 tokens per character, ASCII: ~1.3 tokens per word
	return int(float64(cjkChars)*1.5 + float64(asciiWords)*1.3)
}

// isCJK returns true if the rune is a CJK unified ideograph.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Extension B
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility
		(r >= 0x3000 && r <= 0x303F) // CJK Symbols
}
