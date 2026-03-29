package fasttext

import (
	"os"
	"testing"
)

const testModelPath = "/Volumes/SN770Coder/code/fastTextTrain/models/should_capture_best.ftz"

func TestPrepareInputCJK(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"我是算法架构师", "我 是 算 法 架 构 师"},
		{"", ""},
		{"真棒，你好", "真 棒 你 好"}, // punctuation stripped
	}
	for _, tt := range tests {
		if got := PrepareInputCJK(tt.input); got != tt.want {
			t.Errorf("PrepareInputCJK(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPrepareInputEN(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"I am a developer", "i am a developer"},
		{"  Hello World  ", "hello world"},
	}
	for _, tt := range tests {
		if got := PrepareInputEN(tt.input); got != tt.want {
			t.Errorf("PrepareInputEN(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoadAndPredict(t *testing.T) {
	if _, err := os.Stat(testModelPath); os.IsNotExist(err) {
		t.Skipf("model not found at %s", testModelPath)
	}

	m, err := Load(testModelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	tests := []struct {
		text       string
		wantLabel  string
		wantMinCon float64
	}{
		{"我 是 算 法 架 构 师", "capture", 0.9},
		{"我 家 猫 叫 尼 采", "capture", 0.9},
		{"我 最 近 压 力 很 大", "capture", 0.9},
		{"你 觉 得 我 是 怎 样 一 个 人 呀", "skip", 0.9},
		{"嗯", "skip", 0.9},
		{"好 的 谢 谢", "skip", 0.9},
		{"你 帮 我 查 一 下 天 气", "skip", 0.9},
		{"我 是 不 是 太 敏 感 了", "skip", 0.9},
	}

	for _, tt := range tests {
		pred, err := m.Predict(tt.text)
		if err != nil {
			t.Errorf("Predict(%q) error: %v", tt.text, err)
			continue
		}
		if pred.Label != tt.wantLabel {
			t.Errorf("Predict(%q) label=%s, want %s (conf=%.4f)", tt.text, pred.Label, tt.wantLabel, pred.Confidence)
		}
		if pred.Confidence < tt.wantMinCon {
			t.Errorf("Predict(%q) confidence=%.4f, want >= %.2f", tt.text, pred.Confidence, tt.wantMinCon)
		}
	}
}
