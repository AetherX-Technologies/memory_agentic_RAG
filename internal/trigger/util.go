package trigger

// textIsCJK returns true if the majority of the text is CJK characters.
func textIsCJK(text string) bool {
	cjk, total := 0, 0
	for _, r := range text {
		total++
		if isCJKRune(r) {
			cjk++
		}
	}
	if total == 0 {
		return false
	}
	return float64(cjk)/float64(total) > 0.3
}

// isCJKRune returns true if r is a CJK unified ideograph.
func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Extension B
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility
		(r >= 0x3000 && r <= 0x303F) // CJK Symbols
}
