package tokutil

import (
	"math"
	"testing"
)

func TestEstimateTokens_ASCII(t *testing.T) {
	// "Hello, world!" = 4 tokens with cl100k_base
	text := "Hello, world!"
	got := EstimateTokens(text)
	// 13 chars * 0.25 = 3.25 → 3
	// Real: 4. Within 25% tolerance.
	if got < 2 || got > 6 {
		t.Errorf("ASCII: EstimateTokens(%q) = %d, want ~4", text, got)
	}
}

func TestEstimateTokens_CJK(t *testing.T) {
	// "这个项目的架构设计非常优秀" = 14 CJK chars
	// Real tokens (cl100k_base): ~19
	text := "这个项目的架构设计非常优秀"
	got := EstimateTokens(text)
	// 14 * 1.5 = 21
	if got < 15 || got > 25 {
		t.Errorf("CJK: EstimateTokens(%q) = %d, want ~19-21", text, got)
	}
}

func TestEstimateTokens_Mixed(t *testing.T) {
	// "用户是Go后端工程师，3年经验" — mixed CJK + ASCII
	text := "用户是Go后端工程师，3年经验"
	got := EstimateTokens(text)
	// 10 CJK * 1.5 + 4 ASCII * 0.25 = 15 + 1 = 16
	if got < 10 || got > 22 {
		t.Errorf("Mixed: EstimateTokens(%q) = %d, want ~14-18", text, got)
	}
}

func TestEstimateTokens_Emoji(t *testing.T) {
	text := "Hello 🌍🎉"
	got := EstimateTokens(text)
	// "Hello " = 6 * 0.25 = 1.5, "🌍🎉" = 2 * 2.0 = 4.0 → total ~6
	if got < 4 || got > 8 {
		t.Errorf("Emoji: EstimateTokens(%q) = %d, want ~5-6", text, got)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("Empty: got %d, want 0", got)
	}
}

func TestEstimateTokens_SingleChar(t *testing.T) {
	// Minimum guard: single char should return 1
	if got := EstimateTokens("a"); got != 1 {
		t.Errorf("single ASCII: got %d, want 1", got)
	}
	if got := EstimateTokens("中"); got < 1 {
		t.Errorf("single CJK: got %d, want >= 1", got)
	}
}

func TestEstimateTokens_Korean(t *testing.T) {
	// "안녕하세요" = 5 Hangul chars
	text := "안녕하세요"
	got := EstimateTokens(text)
	// 5 * 1.5 = 7.5 → 8
	if got < 5 || got > 12 {
		t.Errorf("Korean: EstimateTokens(%q) = %d, want ~8", text, got)
	}
}

func TestEstimateTokens_Japanese(t *testing.T) {
	// "こんにちは" = 5 Hiragana chars
	text := "こんにちは"
	got := EstimateTokens(text)
	if got < 5 || got > 12 {
		t.Errorf("Hiragana: EstimateTokens(%q) = %d, want ~8", text, got)
	}
	// "カタカナ" = 4 Katakana chars
	text2 := "カタカナ"
	got2 := EstimateTokens(text2)
	if got2 < 4 || got2 > 10 {
		t.Errorf("Katakana: EstimateTokens(%q) = %d, want ~6", text2, got2)
	}
}

func TestEstimateTokens_Fullwidth(t *testing.T) {
	// "ＡＢＣ" = 3 fullwidth Latin chars
	text := "ＡＢＣ"
	got := EstimateTokens(text)
	// Fullwidth treated as CJK: 3 * 1.5 = 4.5 → 5
	if got < 3 || got > 8 {
		t.Errorf("Fullwidth: EstimateTokens(%q) = %d, want ~5", text, got)
	}
}

func TestEstimateTokens_FlagEmoji(t *testing.T) {
	// 🇺🇸 = two regional indicator symbols
	text := "🇺🇸"
	got := EstimateTokens(text)
	// 2 regional indicators * 2.0 = 4
	if got < 2 {
		t.Errorf("Flag emoji: EstimateTokens(%q) = %d, want >= 2", text, got)
	}
}

func TestEstimateTokens_Whitespace(t *testing.T) {
	text := "   \t\n  "
	got := EstimateTokens(text)
	// 7 whitespace chars * 0.25 = 1.75 → 2
	if got < 1 || got > 4 {
		t.Errorf("Whitespace: EstimateTokens(%q) = %d, want ~2", text, got)
	}
}

func BenchmarkEstimateTokens(b *testing.B) {
	// ~500 char mixed CJK/ASCII string
	text := "用户是一名Go后端工程师，在北京工作，喜欢简洁的代码风格。" +
		"The project uses SQLite with FTS5 for full-text search. " +
		"性能目标是10000条记忆检索小于50ms。"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func TestEstimateTokens_VsCrudeEstimator(t *testing.T) {
	// Verify CJK-aware estimator is more accurate than the old runes*2/3 approach
	tests := []struct {
		name     string
		text     string
		realToks int // approximate real token count
	}{
		{"pure_ascii", "The quick brown fox jumps over the lazy dog", 9},
		{"pure_cjk", "这是一个非常好的项目设计方案", 20},
		{"mixed", "我用Go写了一个REST API服务", 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newEst := EstimateTokens(tt.text)
			// Old estimator: runes * 2 / 3
			runeCount := 0
			for range tt.text {
				runeCount++
			}
			oldEst := runeCount * 2 / 3

			newErr := math.Abs(float64(newEst-tt.realToks)) / float64(tt.realToks)
			oldErr := math.Abs(float64(oldEst-tt.realToks)) / float64(tt.realToks)

			t.Logf("%s: real=%d, new=%d (%.0f%% err), old=%d (%.0f%% err)",
				tt.name, tt.realToks, newEst, newErr*100, oldEst, oldErr*100)

			// New estimator should be at least as good as old for CJK
			if tt.name == "pure_cjk" && newErr > oldErr+0.1 {
				t.Errorf("CJK: new estimator worse than old (new=%.0f%%, old=%.0f%%)", newErr*100, oldErr*100)
			}
		})
	}
}
