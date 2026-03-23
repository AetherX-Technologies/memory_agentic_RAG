package trigger

import "regexp"

// AI denial patterns — should not be stored as memories.
var denialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(I don't have (any )?information|I wasn't able to find|no relevant memories)`),
	regexp.MustCompile(`(我没有(任何)?相关信息|我找不到|没有相关记忆|我不确定)`),
	regexp.MustCompile(`(?i)(I cannot recall|I don't recall|I'm not sure)`),
}

// Meta-question patterns — questions about the memory system itself.
var metaPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(do you remember|can you recall|did I tell you)`),
	regexp.MustCompile(`(你记得吗|你还记得|我告诉过你吗)`),
}

// Boilerplate patterns — trivial or repetitive content.
var boilerplatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(hello|hi|hey|thanks|thank you|ok|okay)[\s.!]*$`),
	regexp.MustCompile(`^(你好|谢谢|好的|嗯)[\s。！]*$`),
	regexp.MustCompile(`(?i)HEARTBEAT`),
	regexp.MustCompile(`(?i)fresh session`),
}

// IsNoise returns true if the text is noise that should not be stored,
// along with the noise category.
func IsNoise(text string) (bool, string) {
	for _, r := range denialPatterns {
		if r.MatchString(text) {
			return true, "ai_denial"
		}
	}
	for _, r := range metaPatterns {
		if r.MatchString(text) {
			return true, "meta_question"
		}
	}
	for _, r := range boilerplatePatterns {
		if r.MatchString(text) {
			return true, "boilerplate"
		}
	}
	return false, ""
}
