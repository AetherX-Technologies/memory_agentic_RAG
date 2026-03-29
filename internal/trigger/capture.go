// Package trigger provides automatic memory capture/retrieve/noise decisions.
package trigger

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yourusername/hybridmem-rag/internal/fasttext"
)

// CaptureReason describes why a text was flagged for capture.
type CaptureReason string

const (
	ReasonExplicit        CaptureReason = "explicit"
	ReasonImplicit        CaptureReason = "implicit"
	ReasonPattern         CaptureReason = "pattern"
	ReasonFastTextSkip    CaptureReason = "fasttext_skip"
	ReasonFastTextCapture CaptureReason = "fasttext_capture"

	// FastText skip confidence threshold — only skip when model is very sure.
	// Conservative: prefer false positives (capture noise) over false negatives (miss memories).
	FastTextSkipThreshold = 0.61
)

// Dual FastText models: CJK (Chinese) and EN (English).
var (
	ftModelCJK *fasttext.Model
	ftModelEN  *fasttext.Model
	ftMu       sync.RWMutex
)

// InitFastText loads the CJK FastText should_capture model.
func InitFastText(modelPath string) error {
	ftMu.Lock()
	defer ftMu.Unlock()
	if ftModelCJK != nil {
		return nil
	}
	m, err := fasttext.Load(modelPath)
	if err != nil {
		return fmt.Errorf("fasttext CJK init: %w", err)
	}
	ftModelCJK = m
	fmt.Fprintf(os.Stderr, "[memory] fasttext: loaded CJK model %s\n", modelPath)
	return nil
}

// InitFastTextEN loads the English FastText should_capture model.
func InitFastTextEN(modelPath string) error {
	ftMu.Lock()
	defer ftMu.Unlock()
	if ftModelEN != nil {
		return nil
	}
	m, err := fasttext.Load(modelPath)
	if err != nil {
		return fmt.Errorf("fasttext EN init: %w", err)
	}
	ftModelEN = m
	fmt.Fprintf(os.Stderr, "[memory] fasttext: loaded EN model %s\n", modelPath)
	return nil
}

// fastTextPredict routes to the appropriate model based on text language.
// Holds RLock through the entire Predict call to prevent use-after-free.
func fastTextPredict(text string, isCJK bool) (fasttext.Prediction, bool) {
	ftMu.RLock()
	defer ftMu.RUnlock()

	var m *fasttext.Model
	var prepared string
	if isCJK {
		m = ftModelCJK
		prepared = fasttext.PrepareInputCJK(text)
	} else {
		m = ftModelEN
		prepared = fasttext.PrepareInputEN(text)
	}
	if m == nil {
		return fasttext.Prediction{}, false
	}
	pred, err := m.Predict(prepared)
	if err != nil {
		return fasttext.Prediction{}, false
	}
	return pred, true
}

// CloseFastText releases all FastText model resources.
func CloseFastText() {
	ftMu.Lock()
	defer ftMu.Unlock()
	if ftModelCJK != nil {
		ftModelCJK.Close()
		ftModelCJK = nil
	}
	if ftModelEN != nil {
		ftModelEN.Close()
		ftModelEN = nil
	}
}

// ConfidenceForReason returns the default confidence for a capture reason.
func ConfidenceForReason(r CaptureReason) float64 {
	switch r {
	case ReasonExplicit:
		return 0.95
	case ReasonFastTextCapture:
		return 0.85
	case ReasonImplicit:
		return 0.7
	case ReasonPattern:
		return 0.6
	default:
		return 0.5
	}
}

// Explicit triggers — user explicitly asks to remember something.
var explicitTriggers = []string{
	// Chinese
	"记住", "请记住", "记下", "别忘了", "帮我记", "你记一下",
	// English
	"remember", "don't forget", "keep in mind", "note that",
}

// Implicit triggers — user is describing themselves or giving instructions.
var implicitTriggers = []string{
	// Identity / facts
	"我是", "我在", "我叫", "我的名字", "我的职业", "我的工作",
	"I am", "I'm a", "my name is", "I work at",
	// Preferences
	"我喜欢", "我偏好", "我习惯", "我更喜欢", "我觉得", "我认为",
	"I like", "I prefer", "I think", "I believe",
	// Negative preferences
	"我不喜欢", "我讨厌", "我不想", "不要",
	"I don't like", "I hate", "don't",
	// Skills
	"我会", "我擅长", "我学过", "我用过", "我做过",
	"I know", "I can", "I've been using",
	// Instructions
	"以后", "每次", "总是", "一直", "请以后", "请用",
	"from now on", "always", "never",
	// Relationships
	"是我的", "我的同事", "我的老板", "我的团队",
	"my colleague", "my boss", "my team",
}

// Regex triggers — structured information patterns.
var regexTriggers = []*regexp.Regexp{
	regexp.MustCompile(`\+\d{10,}`),                      // phone
	regexp.MustCompile(`[\w.-]+@[\w.-]+\.\w+`),           // email
	regexp.MustCompile(`\d{4}[-/]\d{1,2}[-/]\d{1,2}`),   // date
}

// ShouldCapture returns true if the text contains memory-worthy content,
// along with the reason for capture.
func ShouldCapture(text string) (bool, CaptureReason) {
	runeLen := utf8.RuneCountInString(text)

	// Length filter: too short or too long
	if textIsCJK(text) {
		if runeLen < 4 {
			return false, ""
		}
	} else {
		if runeLen < 10 {
			return false, ""
		}
	}
	if runeLen > 500 {
		return false, ""
	}

	// 1. Explicit triggers (highest priority — always capture)
	lower := strings.ToLower(text)
	for _, t := range explicitTriggers {
		if strings.Contains(lower, strings.ToLower(t)) {
			return true, ReasonExplicit
		}
	}

	// 2. FastText classifier — route to CJK or EN model based on text language
	isCJK := textIsCJK(text)
	if pred, ok := fastTextPredict(text, isCJK); ok {
		if pred.Label == "skip" && pred.Confidence >= FastTextSkipThreshold {
			return false, ReasonFastTextSkip
		}
		if pred.Label == "capture" && pred.Confidence >= FastTextSkipThreshold {
			return true, ReasonFastTextCapture
		}
		// Low confidence (either label) → fall through to rule-based logic
	}

	// 3. Implicit triggers (rule-based fallback)
	for _, t := range implicitTriggers {
		if strings.Contains(lower, strings.ToLower(t)) {
			return true, ReasonImplicit
		}
	}

	// 4. Regex patterns
	for _, r := range regexTriggers {
		if r.MatchString(text) {
			return true, ReasonPattern
		}
	}

	return false, ""
}
