package trigger

import (
	"os"
	"testing"
)

const (
	ftModelPath   = "/Volumes/SN770Coder/code/fastTextTrain/models/should_capture_best.ftz"
	ftModelPathEN = "/Volumes/SN770Coder/code/fastTextTrain/models/should_capture_en_best.ftz"
)

func initBothModels(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(ftModelPath); os.IsNotExist(err) {
		t.Skipf("CJK model not found at %s", ftModelPath)
	}
	if _, err := os.Stat(ftModelPathEN); os.IsNotExist(err) {
		t.Skipf("EN model not found at %s", ftModelPathEN)
	}
	if err := InitFastText(ftModelPath); err != nil {
		t.Fatalf("InitFastText CJK: %v", err)
	}
	if err := InitFastTextEN(ftModelPathEN); err != nil {
		t.Fatalf("InitFastText EN: %v", err)
	}
}

func TestFastTextIntegration(t *testing.T) {
	initBothModels(t)

	// CJK acceptance criteria
	tests := []struct {
		text string
		want bool
		desc string
	}{
		// Should capture (CJK)
		{"我是算法架构师", true, "CJK identity statement"},
		{"我家猫叫尼采", true, "CJK relationship fact"},
		{"我最近压力很大", true, "CJK emotional state"},
		{"帮我记一下，我六月要搬家", true, "CJK explicit remember"},

		// Should skip (CJK)
		{"你觉得我是怎样一个人呀", false, "CJK question about self"},
		{"真棒，话说你觉得我是啥人", false, "CJK question with filler"},
		{"嗯", false, "CJK minimal response"},
		{"好的谢谢", false, "CJK acknowledgment"},
		{"你帮我查一下天气", false, "CJK task request"},
		{"我是不是太敏感了", false, "CJK rhetorical question"},

		// Should capture (English)
		{"I am a backend developer", true, "EN identity statement"},
		{"my cat is called nietzsche", true, "EN relationship fact"},
		{"I prefer dark mode for coding", true, "EN preference"},
		{"I have been feeling stressed lately", true, "EN emotional state"},
		{"remember I am moving in June", true, "EN explicit remember"},

		// Should skip (English)
		{"do you think I am too sensitive", false, "EN question about self"},
		{"ok thanks", false, "EN acknowledgment"},
		{"can you check the weather for me", false, "EN task request"},
	}

	for _, tt := range tests {
		got, reason := ShouldCapture(tt.text)
		if got != tt.want {
			t.Errorf("ShouldCapture(%q) [%s] = (%v, %s), want %v",
				tt.text, tt.desc, got, reason, tt.want)
		}
	}
}
