// Package dedup: auto-calibration for embedder-specific thresholds.
// Runs a small set of text pairs through the embedder to measure cosine distribution,
// then derives optimal DupThreshold, ConflictThreshold, ConnectionMinSim, ConnectionMaxSim.
// Results are cached to ~/.hybridmem/calibration.json keyed by model name.
package dedup

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// CalibrationResult holds computed thresholds from a calibration run.
type CalibrationResult struct {
	ModelName        string  `json:"model_name"`
	CalibratedAt     string  `json:"calibrated_at"`
	DupThreshold     float64 `json:"dup_threshold"`
	ConflictThreshold float64 `json:"conflict_threshold"`
	ConnectionMinSim float64 `json:"connection_min_sim"`
	ConnectionMaxSim float64 `json:"connection_max_sim"`

	// Raw statistics for debugging
	DupMin     float64 `json:"dup_min"`
	DupMax     float64 `json:"dup_max"`
	RelMin     float64 `json:"rel_min"`
	RelMax     float64 `json:"rel_max"`
	UnrelMin   float64 `json:"unrel_min"`
	UnrelMax   float64 `json:"unrel_max"`
	NumPairs   int     `json:"num_pairs"`
}

// calibrationPair defines a text pair with expected category.
type calibrationPair struct {
	cat  string // "dup", "rel", "unrel"
	a, b string
}

// Standard calibration corpus — small, diverse, multilingual.
var calibrationCorpus = []calibrationPair{
	// DUPLICATE: same fact, slight rephrasing
	{"dup", "用户是Go后端工程师", "用户是一名Go后端开发者"},
	{"dup", "我喜欢周末爬山", "周末我喜欢去爬山"},
	{"dup", "项目用PostgreSQL数据库", "该项目使用PostgreSQL作为数据库"},
	{"dup", "I work at OpenAI", "I am employed at OpenAI"},
	{"dup", "User prefers dark mode", "User likes dark theme"},
	{"dup", "服务部署在AWS上", "这个服务运行在AWS云平台"},
	{"dup", "每天喝三杯咖啡", "每天要喝3杯咖啡"},

	// RELATED: same topic, different angles
	{"rel", "用户擅长Go语言", "用户有5年Python开发经验"},
	{"rel", "项目用gRPC做服务间通信", "项目采用微服务架构"},
	{"rel", "数据库是PostgreSQL", "缓存层用Redis"},
	{"rel", "用户在北京工作", "用户是水利工程师"},
	{"rel", "I prefer Vim", "I use tmux for terminal multiplexing"},
	{"rel", "用户养了一只猫", "猫的名字叫小黑"},
	{"rel", "使用Prometheus监控", "用Grafana做可视化"},
	{"rel", "用户喜欢喝咖啡", "用户常去星巴克"},
	{"rel", "前端用React", "状态管理用Redux"},

	// UNRELATED: completely different topics
	{"unrel", "用户擅长Go开发", "今天天气很好"},
	{"unrel", "项目用PostgreSQL", "我喜欢吃火锅"},
	{"unrel", "用户养了一只猫", "项目使用Kubernetes部署"},
	{"unrel", "I prefer dark mode", "The meeting is at 3pm"},
	{"unrel", "用户喜欢摄影", "数据库索引优化"},
	{"unrel", "昨天看了一部电影", "Redis缓存失效"},
	{"unrel", "妈妈的生日是5月", "使用OAuth认证"},
}

// Calibrate runs the standard corpus through the embedder and computes optimal thresholds.
func Calibrate(embedder store.Embedder, modelName string) (*CalibrationResult, error) {
	type scored struct {
		cat string
		sim float64
	}

	var results []scored
	for _, p := range calibrationCorpus {
		vecA, err := embedder.Embed(p.a)
		if err != nil {
			return nil, fmt.Errorf("embed %q: %w", p.a[:min(len(p.a), 20)], err)
		}
		vecB, err := embedder.Embed(p.b)
		if err != nil {
			return nil, fmt.Errorf("embed %q: %w", p.b[:min(len(p.b), 20)], err)
		}
		sim := float64(store.CosineSimilarity(vecA, vecB))
		results = append(results, scored{cat: p.cat, sim: sim})
	}

	// Compute stats per category
	stats := map[string]struct{ min, max, sum float64; n int }{}
	for _, r := range results {
		s := stats[r.cat]
		if s.n == 0 {
			s.min = r.sim
			s.max = r.sim
		} else {
			if r.sim < s.min { s.min = r.sim }
			if r.sim > s.max { s.max = r.sim }
		}
		s.sum += r.sim
		s.n++
		stats[r.cat] = s
	}

	dup := stats["dup"]
	rel := stats["rel"]
	unrel := stats["unrel"]

	// Derive thresholds
	// DupThreshold: just below the minimum duplicate score
	dupThresh := dup.min - 0.01
	if dupThresh < 0.80 { dupThresh = 0.80 } // safety floor

	// ConnectionMaxSim: same as dup threshold (connections stop where dups begin)
	connMax := dupThresh

	// ConflictThreshold: midpoint between RELATED max and DUP min
	conflictThresh := (rel.max + dup.min) / 2
	if conflictThresh > dupThresh { conflictThresh = dupThresh - 0.02 }

	// ConnectionMinSim: just above unrelated max, but not higher than related min
	connMin := unrel.max + 0.01
	if connMin > rel.min { connMin = rel.min - 0.01 }
	if connMin < 0.40 { connMin = 0.40 } // safety floor

	// Sanity: ensure connMin < connMax
	if connMin >= connMax {
		connMin = connMax - 0.05
	}

	result := &CalibrationResult{
		ModelName:        modelName,
		CalibratedAt:     time.Now().Format(time.RFC3339),
		DupThreshold:     round3(dupThresh),
		ConflictThreshold: round3(conflictThresh),
		ConnectionMinSim: round3(connMin),
		ConnectionMaxSim: round3(connMax),
		DupMin:           round3(dup.min),
		DupMax:           round3(dup.max),
		RelMin:           round3(rel.min),
		RelMax:           round3(rel.max),
		UnrelMin:         round3(unrel.min),
		UnrelMax:         round3(unrel.max),
		NumPairs:         len(results),
	}

	return result, nil
}

// ApplyCalibration applies calibration results to a Config.
func ApplyCalibration(cfg *Config, cal *CalibrationResult) {
	cfg.DupThreshold = cal.DupThreshold
	cfg.ConflictThreshold = cal.ConflictThreshold
	cfg.ConnectionMinSim = cal.ConnectionMinSim
	cfg.ConnectionMaxSim = cal.ConnectionMaxSim
}

// LoadCachedCalibration loads a cached calibration for the model.
// Returns nil if no cache exists.
func LoadCachedCalibration(modelName string) *CalibrationResult {
	path := calibrationCachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var cache map[string]*CalibrationResult
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}

	return cache[modelName]
}

// SaveCalibration persists a calibration result to the cache.
func SaveCalibration(cal *CalibrationResult) error {
	path := calibrationCachePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Load existing
	var cache map[string]*CalibrationResult
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &cache)
	}
	if cache == nil {
		cache = make(map[string]*CalibrationResult)
	}

	cache[cal.ModelName] = cal

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: write to unique temp file then rename
	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func calibrationCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hybridmem", "calibration.json")
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// PrintCalibration logs calibration results to stderr.
func PrintCalibration(cal *CalibrationResult) {
	fmt.Fprintf(os.Stderr, "[calibration] model=%s pairs=%d\n", cal.ModelName, cal.NumPairs)
	fmt.Fprintf(os.Stderr, "[calibration]   DUP:   min=%.3f max=%.3f\n", cal.DupMin, cal.DupMax)
	fmt.Fprintf(os.Stderr, "[calibration]   REL:   min=%.3f max=%.3f\n", cal.RelMin, cal.RelMax)
	fmt.Fprintf(os.Stderr, "[calibration]   UNREL: min=%.3f max=%.3f\n", cal.UnrelMin, cal.UnrelMax)
	fmt.Fprintf(os.Stderr, "[calibration]   → DupThreshold=%.3f ConflictThreshold=%.3f\n", cal.DupThreshold, cal.ConflictThreshold)
	fmt.Fprintf(os.Stderr, "[calibration]   → ConnectionBand=[%.3f, %.3f)\n", cal.ConnectionMinSim, cal.ConnectionMaxSim)
}

// SortedProfiles returns all cached profile names sorted.
func SortedProfiles() []string {
	path := calibrationCachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache map[string]*CalibrationResult
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	names := make([]string, 0, len(cache))
	for k := range cache {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
