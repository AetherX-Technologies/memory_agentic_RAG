package trigger

import "testing"

// ════════════════════════════════════════
// ShouldCapture tests
// ════════════════════════════════════════

func TestShouldCapture_Explicit(t *testing.T) {
	tests := []struct {
		text   string
		want   bool
		reason CaptureReason
	}{
		{"记住，我是工程师", true, ReasonExplicit},
		{"请记住我的名字叫张伟", true, ReasonExplicit},
		{"Please remember my email", true, ReasonExplicit},
		{"don't forget I like Go", true, ReasonExplicit},
		{"keep in mind that I prefer dark mode", true, ReasonExplicit},
	}
	for _, tt := range tests {
		got, reason := ShouldCapture(tt.text)
		if got != tt.want || reason != tt.reason {
			t.Errorf("ShouldCapture(%q) = (%v, %v), want (%v, %v)", tt.text, got, reason, tt.want, tt.reason)
		}
	}
}

func TestShouldCapture_Implicit(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"我是西北院的水利工程师", true},
		{"我在北京工作三年了", true},
		{"我喜欢简洁的代码风格", true},
		{"I am a backend developer", true},
		{"I prefer dark mode", true},
		{"I've been using Go for 3 years", true},
		{"以后请用中文回复我", true},
		{"my boss is John", true},
	}
	for _, tt := range tests {
		got, _ := ShouldCapture(tt.text)
		if got != tt.want {
			t.Errorf("ShouldCapture(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestShouldCapture_Pattern(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"我的邮箱是 test@example.com", true},
		{"电话号码 +8613812345678 请记下", true},
		{"截止日期是 2024-03-15 别忘了", true},
	}
	for _, tt := range tests {
		got, _ := ShouldCapture(tt.text)
		if got != tt.want {
			t.Errorf("ShouldCapture(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestShouldCapture_Reject(t *testing.T) {
	tests := []string{
		"",           // empty
		"hi",         // too short EN
		"你好",        // too short CJK
		"今天天气不错",  // no triggers
	}
	for _, text := range tests {
		if got, _ := ShouldCapture(text); got {
			t.Errorf("ShouldCapture(%q) = true, want false", text)
		}
	}
}

func TestShouldCapture_TooLong(t *testing.T) {
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	if got, _ := ShouldCapture(string(long)); got {
		t.Error("ShouldCapture(501 chars) = true, want false")
	}
}

// ════════════════════════════════════════
// ShouldRetrieve tests
// ════════════════════════════════════════

func TestShouldRetrieve_ForceRetrieve(t *testing.T) {
	tests := []string{
		"你记得吗",
		"还记得我说过什么",
		"remember?",
		"my name?",
		"what did I tell you",
		"last time we discussed",
		"my email?",
		"我的偏好是什么",
	}
	for _, text := range tests {
		if !ShouldRetrieve(text) {
			t.Errorf("ShouldRetrieve(%q) = false, want true (force-retrieve)", text)
		}
	}
}

func TestShouldRetrieve_Skip(t *testing.T) {
	tests := []string{
		"",
		"hi",
		"ok",
		"yes",
		"好的",
		"继续",
		"git status",
		"npm install",
		"/help",
		"🎉🎉🎉",
	}
	for _, text := range tests {
		if ShouldRetrieve(text) {
			t.Errorf("ShouldRetrieve(%q) = true, want false (skip)", text)
		}
	}
}

func TestShouldRetrieve_Normal(t *testing.T) {
	tests := []string{
		"帮我写一个Go HTTP服务器的代码",
		"what's the best way to implement caching in Go",
		"我最近在做一个水利监测系统",
	}
	for _, text := range tests {
		if !ShouldRetrieve(text) {
			t.Errorf("ShouldRetrieve(%q) = false, want true (normal)", text)
		}
	}
}

// ════════════════════════════════════════
// IsNoise tests
// ════════════════════════════════════════

func TestIsNoise_Denial(t *testing.T) {
	tests := []struct {
		text string
		want bool
		kind string
	}{
		{"I don't have any information about that", true, "ai_denial"},
		{"没有相关记忆", true, "ai_denial"},
		{"I'm not sure about that", true, "ai_denial"},
	}
	for _, tt := range tests {
		got, kind := IsNoise(tt.text)
		if got != tt.want || kind != tt.kind {
			t.Errorf("IsNoise(%q) = (%v, %q), want (%v, %q)", tt.text, got, kind, tt.want, tt.kind)
		}
	}
}

func TestIsNoise_Meta(t *testing.T) {
	tests := []string{
		"do you remember what I said",
		"你记得吗我告诉过你",
		"did I tell you my name",
	}
	for _, text := range tests {
		got, kind := IsNoise(text)
		if !got || kind != "meta_question" {
			t.Errorf("IsNoise(%q) = (%v, %q), want (true, meta_question)", text, got, kind)
		}
	}
}

func TestIsNoise_Boilerplate(t *testing.T) {
	tests := []string{
		"hello",
		"thanks!",
		"ok",
		"你好",
		"谢谢",
		"HEARTBEAT",
	}
	for _, text := range tests {
		got, kind := IsNoise(text)
		if !got || kind != "boilerplate" {
			t.Errorf("IsNoise(%q) = (%v, %q), want (true, boilerplate)", text, got, kind)
		}
	}
}

func TestIsNoise_Normal(t *testing.T) {
	tests := []string{
		"我是西北院的工程师，用Go写后端",
		"The project uses SQLite for storage",
		"请用中文回复",
	}
	for _, text := range tests {
		if got, _ := IsNoise(text); got {
			t.Errorf("IsNoise(%q) = true, want false", text)
		}
	}
}

// ════════════════════════════════════════
// textIsCJK tests
// ════════════════════════════════════════

func TestTextIsCJK(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"你好世界", true},
		{"hello world", false},
		{"我用Go写代码在北京办公室", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := textIsCJK(tt.text); got != tt.want {
			t.Errorf("textIsCJK(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// ════════════════════════════════════════
// ConfidenceForReason tests
// ════════════════════════════════════════

func TestConfidenceForReason(t *testing.T) {
	if c := ConfidenceForReason(ReasonExplicit); c != 0.95 {
		t.Errorf("ConfidenceForReason(explicit) = %v, want 0.95", c)
	}
	if c := ConfidenceForReason(ReasonImplicit); c != 0.7 {
		t.Errorf("ConfidenceForReason(implicit) = %v, want 0.7", c)
	}
	if c := ConfidenceForReason(ReasonPattern); c != 0.6 {
		t.Errorf("ConfidenceForReason(pattern) = %v, want 0.6", c)
	}
	if c := ConfidenceForReason("unknown"); c != 0.5 {
		t.Errorf("ConfidenceForReason(unknown) = %v, want 0.5", c)
	}
}
