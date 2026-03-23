package store

import (
	"math"
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

func TestApplyTimeDecay_Disabled(t *testing.T) {
	now := time.Now().Unix()
	results := []SearchResult{
		{Entry: Memory{Timestamp: now - 86400*100}, Score: 1.0},
	}
	// TimeDecayHalfLifeDays <= 0 means disabled
	applyTimeDecay(results, ScoringConfig{TimeDecayHalfLifeDays: 0}, now)
	if results[0].Score != 1.0 {
		t.Errorf("expected score 1.0 when decay disabled, got %f", results[0].Score)
	}
}

func TestApplyTimeDecay_AtHalfLife(t *testing.T) {
	now := time.Now().Unix()
	halfLife := 60
	floor := 0.5

	results := []SearchResult{
		{Entry: Memory{Timestamp: now - int64(halfLife)*86400}, Score: 1.0},
	}
	applyTimeDecay(results, ScoringConfig{
		TimeDecayHalfLifeDays: halfLife,
		TimeDecayFloor:        floor,
	}, now)

	// At halfLife days: decay = floor + (1-floor)*0.5 = 0.5 + 0.5*0.5 = 0.75
	expected := 0.75
	if math.Abs(results[0].Score-expected) > 0.01 {
		t.Errorf("at halfLife: score = %f, want ~%f", results[0].Score, expected)
	}
}

func TestApplyTimeDecay_Fresh(t *testing.T) {
	now := time.Now().Unix()
	results := []SearchResult{
		{Entry: Memory{Timestamp: now}, Score: 1.0},
	}
	applyTimeDecay(results, ScoringConfig{
		TimeDecayHalfLifeDays: 60,
		TimeDecayFloor:        0.5,
	}, now)

	// Fresh: decay ≈ 1.0
	if math.Abs(results[0].Score-1.0) > 0.01 {
		t.Errorf("fresh: score = %f, want ~1.0", results[0].Score)
	}
}

func TestApplyTimeDecay_VeryOld(t *testing.T) {
	now := time.Now().Unix()
	results := []SearchResult{
		{Entry: Memory{Timestamp: now - 86400*365*10}, Score: 1.0}, // 10 years
	}
	applyTimeDecay(results, ScoringConfig{
		TimeDecayHalfLifeDays: 60,
		TimeDecayFloor:        0.5,
	}, now)

	// Very old: decay ≈ floor (0.5)
	if math.Abs(results[0].Score-0.5) > 0.01 {
		t.Errorf("very old: score = %f, want ~0.5", results[0].Score)
	}
}

func TestApplyScoring_WithMMR(t *testing.T) {
	now := time.Now().Unix()
	v := []float32{1, 0, 0}
	results := []SearchResult{
		{Entry: Memory{Text: "a", Importance: 0.9, Timestamp: now, Vector: v, Confidence: 0.9}, Score: 0.9},
		{Entry: Memory{Text: "a-dup", Importance: 0.9, Timestamp: now, Vector: v, Confidence: 0.9}, Score: 0.85},
		{Entry: Memory{Text: "b", Importance: 0.8, Timestamp: now, Vector: []float32{0, 1, 0}, Confidence: 0.9}, Score: 0.7},
	}
	config := ScoringConfig{
		MMREnabled:      true,
		MMRLambda:       0.7,
		MMRSimThreshold: 0.85,
	}
	scored := ApplyScoring(results, config)
	if len(scored) != 3 {
		t.Fatalf("expected 3 results, got %d", len(scored))
	}
}
