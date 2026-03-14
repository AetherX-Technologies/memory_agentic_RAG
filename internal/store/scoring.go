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

	// 过滤并排序
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if r.Score >= config.HardMinScore {
			filtered = append(filtered, r)
		}
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
		boost := math.Exp(-ageDays/float64(config.RecencyHalfLifeDays)) * config.RecencyWeight
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
