package store

import (
	"testing"
	"time"
)

func TestApplyScoring(t *testing.T) {
	now := time.Now().Unix()

	results := []SearchResult{
		{
			Entry: Memory{
				ID:         "1",
				Text:       "短文本",
				Importance: 0.9,
				Timestamp:  now - 86400, // 1天前
			},
			Score: 0.8,
		},
		{
			Entry: Memory{
				ID:         "2",
				Text:       "这是一个很长的文本" + string(make([]byte, 1000)),
				Importance: 0.5,
				Timestamp:  now - 86400*30, // 30天前
			},
			Score: 0.7,
		},
	}

	config := ScoringConfig{
		RecencyHalfLifeDays: 14,
		RecencyWeight:       0.1,
		LengthNormAnchor:    500,
		HardMinScore:        0.35,
	}

	scored := ApplyScoring(results, config)

	if len(scored) == 0 {
		t.Error("Expected scored results")
	}

	// 第一个结果应该得分更高（更新、更重要、更短）
	if len(scored) >= 2 && scored[0].Score < scored[1].Score {
		t.Errorf("Expected first result to have higher score")
	}
}
