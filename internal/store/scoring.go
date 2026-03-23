package store

import (
	"math"
	"time"
)

const (
	SecondsPerDay         = 86400.0
	MaxScore              = 1.0
	DefaultImportance     = 0.7
	ImportanceWeightBase  = 0.7
	ImportanceWeightRange = 0.3
)

// ScoringConfig 评分配置
type ScoringConfig struct {
	RecencyHalfLifeDays int     // 新近度半衰期（天）
	RecencyWeight       float64 // 新近度权重
	LengthNormAnchor    int     // 长度归一化锚点
	HardMinScore        float64 // 硬性最低分数

	// AI memory system factors
	ConfidenceWeight float64 // multiplier for confidence (default: 1.0)
	AccessBoostCap   float64 // max access frequency boost (default: 1.5)

	// Multiplicative time decay
	TimeDecayHalfLifeDays int     // half-life in days (default: 60)
	TimeDecayFloor        float64 // minimum decay factor (default: 0.5, range 0-1)

	// MMR diversity reranking
	MMREnabled      bool    // enable MMR reranking
	MMRLambda       float64 // relevance vs diversity (default: 0.7)
	MMRSimThreshold float64 // similarity threshold for hard penalty (default: 0.85)
}

// TypeHalfLife returns the recency half-life override for a memory type.
func TypeHalfLife(memoryType string) int {
	switch memoryType {
	case "instruction":
		return 365
	case "skill":
		return 180
	case "fact", "preference", "relationship":
		return 90
	case "episode":
		return 30
	default:
		return 0 // use config default
	}
}

// ApplyScoring 应用评分管道
func ApplyScoring(results []SearchResult, config ScoringConfig) []SearchResult {
	if len(results) == 0 {
		return results
	}

	now := time.Now().Unix()
	applyRecencyBoost(results, config, now)
	applyImportanceWeight(results)
	applyLengthNormalization(results, config)
	applyConfidenceWeight(results, config)
	applyAccessBoost(results, config)
	applyTimeDecay(results, config, now)

	// 过滤并排序
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if r.Score >= config.HardMinScore {
			filtered = append(filtered, r)
		}
	}

	// MMR diversity reranking
	if config.MMREnabled && len(filtered) > 1 {
		lambda := config.MMRLambda
		if lambda <= 0 {
			lambda = 0.7
		}
		threshold := config.MMRSimThreshold
		if threshold <= 0 {
			threshold = 0.85
		}
		filtered = MMRRerank(filtered, lambda, threshold)
	}

	return topK(filtered, len(filtered))
}

// applyRecencyBoost 新近度提升（半衰期衰减）
func applyRecencyBoost(results []SearchResult, config ScoringConfig, now int64) {
	if config.RecencyHalfLifeDays <= 0 || config.RecencyWeight <= 0 {
		return
	}

	for i := range results {
		ts := results[i].Entry.Timestamp
		if ts <= 0 {
			ts = now
		}
		ageDays := float64(now-ts) / SecondsPerDay

		// Use type-specific half-life if available
		halfLife := config.RecencyHalfLifeDays
		if typeHL := TypeHalfLife(results[i].Entry.MemoryType); typeHL > 0 {
			halfLife = typeHL
		}

		boost := math.Exp(-ageDays/float64(halfLife)) * config.RecencyWeight
		results[i].Score = results[i].Score + boost
		if results[i].Score > MaxScore {
			results[i].Score = MaxScore
		}
	}
}

// applyImportanceWeight 重要性加权
func applyImportanceWeight(results []SearchResult) {
	for i := range results {
		importance := results[i].Entry.Importance
		if importance <= 0 {
			importance = DefaultImportance
		}
		factor := ImportanceWeightBase + ImportanceWeightRange*importance
		results[i].Score = results[i].Score * factor
	}
}

// applyLengthNormalization 长度归一化
func applyLengthNormalization(results []SearchResult, config ScoringConfig) {
	if config.LengthNormAnchor <= 0 {
		return
	}

	for i := range results {
		charLen := float64(len(results[i].Entry.Text))
		ratio := charLen / float64(config.LengthNormAnchor)
		if ratio > 1.0 {
			penalty := 1.0 / (1.0 + math.Log2(ratio))
			results[i].Score = results[i].Score * penalty
		}
	}
}

// applyConfidenceWeight multiplies score by confidence (higher confidence → higher score).
func applyConfidenceWeight(results []SearchResult, config ScoringConfig) {
	weight := config.ConfidenceWeight
	if weight <= 0 {
		weight = 1.0 // default: direct multiply
	}
	for i := range results {
		conf := results[i].Entry.Confidence
		if conf <= 0 {
			conf = 0.5 // default confidence
		}
		results[i].Score *= math.Pow(conf, weight)
	}
}

// applyAccessBoost boosts score based on access frequency (log-scale, capped).
// Formula: score *= min(1 + 0.1*log2(1+access_count), AccessBoostCap)
func applyAccessBoost(results []SearchResult, config ScoringConfig) {
	cap := config.AccessBoostCap
	if cap <= 0 {
		cap = 1.5 // default
	}
	for i := range results {
		ac := float64(results[i].Entry.AccessCount)
		boost := 1.0 + 0.1*math.Log2(1.0+ac)
		if boost > cap {
			boost = cap
		}
		results[i].Score *= boost
	}
}

// applyTimeDecay applies multiplicative time decay.
// Formula: score *= floor + (1-floor) * exp(-ln(2) * ageDays / halfLife)
// At exactly halfLife days, excess above floor halves: decay = floor + (1-floor)*0.5
func applyTimeDecay(results []SearchResult, config ScoringConfig, now int64) {
	halfLife := config.TimeDecayHalfLifeDays
	if halfLife <= 0 {
		return // disabled
	}
	floor := config.TimeDecayFloor
	if floor < 0 {
		floor = 0.5
	}
	if floor > 1 {
		floor = 1
	}

	for i := range results {
		ts := results[i].Entry.Timestamp
		if ts <= 0 {
			ts = now
		}
		ageDays := float64(now-ts) / SecondsPerDay
		if ageDays < 0 {
			ageDays = 0
		}

		decay := floor + (1-floor)*math.Exp(-math.Ln2*ageDays/float64(halfLife))
		results[i].Score *= decay
	}
}
