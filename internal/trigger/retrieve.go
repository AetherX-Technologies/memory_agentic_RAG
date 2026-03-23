package trigger

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Force-retrieve patterns — even short queries must trigger retrieval.
var forceRetrievePatterns = []*regexp.Regexp{
	// Explicit recall intent
	regexp.MustCompile(`(?i)\b(remember|recall|forgot|memory)\b`),
	regexp.MustCompile(`(你记得|还记得|之前|上次|以前|提到过|说过)`),
	// Time references
	regexp.MustCompile(`(?i)\b(last time|before|previously|yesterday|ago)\b`),
	// Personal info queries
	regexp.MustCompile(`(?i)\b(my (name|email|phone|address|birthday|preference))\b`),
	regexp.MustCompile(`(我的(名字|邮箱|电话|地址|生日|偏好))`),
	// Rhetorical recall
	regexp.MustCompile(`(?i)\b(what did (i|we)|did i (tell|say|mention))\b`),
	regexp.MustCompile(`(我(说过|提到过|告诉过你))`),
}

// questionPatterns match queries that are asking about past memories, not stating new facts.
// Much narrower than forceRetrievePatterns — only matches actual questions.
var questionPatterns = []*regexp.Regexp{
	// "do you remember" / "can you recall" / "did I tell you" — questions, not statements
	regexp.MustCompile(`(?i)\b(do you remember|can you recall|did i (tell|say|mention))\b`),
	regexp.MustCompile(`(你记得.{0,10}吗|你还记得|我告诉过你吗)`),
	// "what is my X" / "我的X是什么"
	regexp.MustCompile(`(?i)\b(what (is|was) my|what did (i|we))\b`),
	regexp.MustCompile(`(我的\S+是什么|我(说过|提到过)什么)`),
	// Ends with question mark
	regexp.MustCompile(`[?？]\s*$`),
}

// IsRetrievalQuestion returns true if the text is asking about past memories
// rather than stating new information. Deliberately narrow to avoid blocking
// declarative inputs like "remember I have a cat" or "my name is John".
func IsRetrievalQuestion(text string) bool {
	for _, r := range questionPatterns {
		if r.MatchString(text) {
			return true
		}
	}
	return false
}

// Skip patterns — queries that never need memory lookup.
var skipPatterns = []*regexp.Regexp{
	// Greetings
	regexp.MustCompile(`(?i)^(hi|hello|hey|你好|嗨|早上好|晚上好|good\s*(morning|afternoon|evening))\b`),
	// Shell commands
	regexp.MustCompile(`(?i)^(run|build|test|ls|cd|git|npm|pip|docker|curl|grep|find|make|sudo)\b`),
	// Confirmations & acknowledgements
	regexp.MustCompile(`(?i)^(yes|no|yep|nope|ok|okay|sure|fine|好的|是的|不是|继续|嗯|谢谢|再见|拜拜|thx|bye|thanks|thank you)\s*[.!。！]?$`),
	// Action directives
	regexp.MustCompile(`(?i)^(go ahead|continue|proceed|do it|start|begin|next|实施|开始|继续)\s*[.!。！]?$`),
	// Pure emoji (Unicode ranges — Go regexp doesn't support \p{Emoji})
	regexp.MustCompile(`^[\x{1F300}-\x{1F9FF}\x{2600}-\x{27BF}\x{FE00}-\x{FE0F}\x{200D}\s]+$`),
	// Slash commands
	regexp.MustCompile(`^/`),
	// Pure code blocks
	regexp.MustCompile("(?s)^```.*```$"),
}

// ShouldRetrieve returns true if the query warrants memory retrieval.
func ShouldRetrieve(text string) bool {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return false
	}

	// 1. Force-retrieve (highest priority, no length restriction)
	for _, r := range forceRetrievePatterns {
		if r.MatchString(cleaned) {
			return true
		}
	}

	// 2. Length filter (after force-retrieve to avoid blocking short recall queries)
	// Thresholds are kept low to allow short keyword queries like "工程师", "Go", "email"
	runeLen := utf8.RuneCountInString(cleaned)
	if textIsCJK(cleaned) {
		if runeLen < 1 {
			return false
		}
	} else {
		if runeLen < 2 {
			return false
		}
	}

	// 3. Skip patterns
	for _, r := range skipPatterns {
		if r.MatchString(cleaned) {
			return false
		}
	}

	// 4. Default: retrieve
	return true
}
