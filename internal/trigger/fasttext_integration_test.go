package trigger

import (
	"os"
	"testing"
)

const ftModelPath = "/Volumes/SN770Coder/code/fastTextTrain/models/should_capture_best.ftz"

func TestFastTextIntegration(t *testing.T) {
	if _, err := os.Stat(ftModelPath); os.IsNotExist(err) {
		t.Skipf("model not found at %s", ftModelPath)
	}

	if err := InitFastText(ftModelPath); err != nil {
		t.Fatalf("InitFastText: %v", err)
	}

	// Acceptance criteria from the spec
	tests := []struct {
		text string
		want bool
		desc string
	}{
		// Should capture
		{"我是算法架构师", true, "identity statement"},
		{"我家猫叫尼采", true, "relationship fact"},
		{"我最近压力很大", true, "emotional state"},
		{"帮我记一下，我六月要搬家", true, "explicit remember"},

		// Should skip
		{"你觉得我是怎样一个人呀", false, "question about self"},
		{"真棒，话说你觉得我是啥人", false, "question with filler"},
		{"嗯", false, "minimal response"},
		{"好的谢谢", false, "acknowledgment"},
		{"你帮我查一下天气", false, "task request"},
		{"我是不是太敏感了", false, "rhetorical question"},
	}

	for _, tt := range tests {
		got, reason := ShouldCapture(tt.text)
		if got != tt.want {
			t.Errorf("ShouldCapture(%q) [%s] = (%v, %s), want %v",
				tt.text, tt.desc, got, reason, tt.want)
		}
	}
}
