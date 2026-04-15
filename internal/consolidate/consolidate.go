// Package consolidate implements memory consolidation — discovering patterns,
// connections, and insights across stored memories using LLM analysis.
package consolidate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yourusername/hybridmem-rag/internal/llmutil"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

// Config holds consolidation configuration.
type Config struct {
	LLMAPIKey   string
	LLMModel    string
	LLMEndpoint string
	LLMTimeout  int // seconds
	MaxMemories int // max memories per consolidation batch (default: 50)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		LLMModel:    "gpt-4o-mini",
		LLMEndpoint: "https://api.openai.com/v1/chat/completions",
		LLMTimeout:  30,
		MaxMemories: 50,
	}
}

// Result is the parsed LLM consolidation output.
type Result struct {
	SourceIDs   []string                 `json:"source_ids"`
	Summary     string                   `json:"summary"`
	Insight     string                   `json:"insight"`
	Patterns    []string                 `json:"patterns"`
	Connections []map[string]interface{} `json:"connections"`
}

// Consolidator performs memory consolidation.
type Consolidator struct {
	store  store.Store
	config Config
}

// New creates a new Consolidator.
func New(s store.Store, cfg Config) *Consolidator {
	if cfg.LLMModel == "" { cfg.LLMModel = "gpt-4o-mini" }
	if cfg.LLMEndpoint == "" { cfg.LLMEndpoint = "https://api.openai.com/v1/chat/completions" }
	if cfg.LLMTimeout <= 0 { cfg.LLMTimeout = 30 }
	if cfg.MaxMemories <= 0 { cfg.MaxMemories = 50 }

	return &Consolidator{
		store:  s,
		config: cfg,
	}
}

// MaxGroupSize is the soft cap on memories per consolidation group (Scheme A Phase 1).
// Groups are formed by BFS over connections; when a group reaches this size, BFS stops.
const MaxGroupSize = 10

// MinGroupSize is the minimum memory count to consolidate a group.
const MinGroupSize = 2

// Consolidate analyzes unconsolidated memories and creates a consolidation.
// Returns the first produced consolidation (nil if nothing to consolidate),
// preserving the old single-result API for backward compatibility.
//
// Under the hood, this now delegates to LeafPass which groups by semantic
// connections rather than consolidating all unconsolidated memories at once.
func (c *Consolidator) Consolidate(ctx context.Context) (*store.Consolidation, error) {
	results, err := c.LeafPass(ctx)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

// LeafPass groups unconsolidated memories using the connections graph
// and consolidates each group independently. This produces higher-quality
// insights because each LLM call processes a semantically coherent set.
//
// Algorithm:
//  1. Fetch up to MaxMemories unconsolidated memories
//  2. Load their connections (via store.Get since ListUnconsolidated doesn't return them)
//  3. Build groups via BFS starting from each uncovered memory, following
//     connections to other unconsolidated memories (cap group size at MaxGroupSize)
//  4. Consolidate each group of size >= MinGroupSize via LLM
//  5. Return all successful consolidations
//
// Returns consolidations produced (may be empty if no groups meet MinGroupSize).
func (c *Consolidator) LeafPass(ctx context.Context) ([]*store.Consolidation, error) {
	memories, err := c.store.ListUnconsolidated(c.config.MaxMemories)
	if err != nil {
		return nil, fmt.Errorf("list unconsolidated: %w", err)
	}
	if len(memories) < MinGroupSize {
		return nil, nil
	}

	// Build a set of unconsolidated IDs for quick lookup during BFS
	uncID := make(map[string]bool, len(memories))
	for _, m := range memories {
		uncID[m.ID] = true
	}

	// Load full memories (with connections) via Get — ListUnconsolidated lacks connections
	fullByID := make(map[string]*store.Memory, len(memories))
	for _, m := range memories {
		full, err := c.store.Get(m.ID)
		if err != nil || full == nil {
			full = m // fall back to the partial memory
		}
		fullByID[full.ID] = full
	}

	groups := groupByConnections(memories, fullByID, uncID, MaxGroupSize)

	// Consolidate each eligible group
	var results []*store.Consolidation
	for _, group := range groups {
		if len(group) < MinGroupSize {
			continue
		}
		res, err := c.consolidateGroup(ctx, group)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[consolidate] group of %d failed: %v\n", len(group), err)
			continue
		}
		if res != nil {
			results = append(results, res)
		}
	}
	return results, nil
}

// groupByConnections partitions memories into semantically related groups via BFS.
// A memory and all transitively-connected unconsolidated memories form one group.
// Groups are capped at maxSize to keep LLM prompts focused.
//
// Memories without connections to other unconsolidated ones (or that only form
// singletons) are batched into "orphan groups" capped at maxSize each, so
// isolated memories still get consolidated (fallback for cold-start scenarios
// where connections haven't been established yet).
func groupByConnections(memories []*store.Memory, fullByID map[string]*store.Memory, uncID map[string]bool, maxSize int) [][]*store.Memory {
	visited := make(map[string]bool, len(memories))
	var connectedGroups [][]*store.Memory
	var orphans []*store.Memory

	for _, seed := range memories {
		if visited[seed.ID] {
			continue
		}
		// BFS from this seed
		group := []*store.Memory{fullByID[seed.ID]}
		visited[seed.ID] = true
		queue := []string{seed.ID}

		for len(queue) > 0 && len(group) < maxSize {
			curID := queue[0]
			queue = queue[1:]
			cur := fullByID[curID]
			if cur == nil || cur.Connections == "" || cur.Connections == "[]" {
				continue
			}
			var conns []map[string]string
			if err := json.Unmarshal([]byte(cur.Connections), &conns); err != nil {
				continue
			}
			for _, conn := range conns {
				linkedID := conn["linked_to"]
				if linkedID == "" || visited[linkedID] {
					continue
				}
				if !uncID[linkedID] {
					continue // only link to other unconsolidated memories
				}
				visited[linkedID] = true
				if full, ok := fullByID[linkedID]; ok {
					group = append(group, full)
					queue = append(queue, linkedID)
					if len(group) >= maxSize {
						break
					}
				}
			}
		}
		// Singletons go to the orphan bucket; real connected groups kept as-is
		if len(group) == 1 {
			orphans = append(orphans, group[0])
		} else {
			connectedGroups = append(connectedGroups, group)
		}
	}

	// Batch orphans into groups of up to maxSize
	for i := 0; i < len(orphans); i += maxSize {
		end := i + maxSize
		if end > len(orphans) {
			end = len(orphans)
		}
		connectedGroups = append(connectedGroups, orphans[i:end])
	}
	return connectedGroups
}

// consolidateGroup runs LLM consolidation on a single semantically-coherent group.
func (c *Consolidator) consolidateGroup(ctx context.Context, memories []*store.Memory) (*store.Consolidation, error) {
	if len(memories) < MinGroupSize {
		return nil, nil
	}

	// Format for LLM
	var sb strings.Builder
	ids := make([]string, len(memories))
	for i, m := range memories {
		ids[i] = m.ID
		sb.WriteString(fmt.Sprintf("- Memory #%s: %s (type=%s, importance=%.1f)\n",
			m.ID, m.Text, m.MemoryType, m.Importance))
	}

	result, err := c.callLLM(ctx, sb.String())
	if err != nil {
		return nil, fmt.Errorf("LLM consolidation: %w", err)
	}

	sourceIDs := result.SourceIDs
	if len(sourceIDs) == 0 {
		sourceIDs = ids
	}

	sourceIDsJSON, err := json.Marshal(sourceIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal source_ids: %w", err)
	}
	patternsJSON, err := json.Marshal(result.Patterns)
	if err != nil {
		return nil, fmt.Errorf("marshal patterns: %w", err)
	}
	connsJSON, err := json.Marshal(result.Connections)
	if err != nil {
		return nil, fmt.Errorf("marshal connections: %w", err)
	}

	consolidation := &store.Consolidation{
		ID:              uuid.New().String(),
		SourceIDs:       string(sourceIDsJSON),
		Summary:         result.Summary,
		Insight:         result.Insight,
		Patterns:        string(patternsJSON),
		ConnectionsJSON: string(connsJSON),
		CreatedAt:       time.Now().Unix(),
	}

	if _, err := c.store.CreateConsolidation(consolidation); err != nil {
		return nil, fmt.Errorf("store consolidation: %w", err)
	}

	// Update LLM-discovered connections bidirectionally
	var connErrors []string
	for _, conn := range result.Connections {
		fromID, _ := conn["from_id"].(string)
		toID, _ := conn["to_id"].(string)
		rel, _ := conn["relationship"].(string)
		if fromID == "" || toID == "" || rel == "" {
			continue
		}
		if err := c.store.AddConnection(fromID, toID, rel); err != nil {
			connErrors = append(connErrors, fmt.Sprintf("%s→%s: %v", fromID, toID, err))
		}
		if err := c.store.AddConnection(toID, fromID, rel); err != nil {
			connErrors = append(connErrors, fmt.Sprintf("%s→%s: %v", toID, fromID, err))
		}
	}
	if len(connErrors) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d connection update errors during consolidation\n", len(connErrors))
	}

	if err := c.store.MarkConsolidated(sourceIDs); err != nil {
		return nil, fmt.Errorf("mark consolidated: %w", err)
	}

	return consolidation, nil
}

func (c *Consolidator) callLLM(ctx context.Context, memoriesText string) (*Result, error) {
	if c.config.LLMAPIKey == "" {
		return nil, fmt.Errorf("LLM API key required for consolidation")
	}

	prompt := fmt.Sprintf(consolidatePrompt, memoriesText)
	cfg := llmutil.Config{
		APIKey:   c.config.LLMAPIKey,
		Model:    c.config.LLMModel,
		Endpoint: c.config.LLMEndpoint,
		Timeout:  c.config.LLMTimeout,
	}
	raw, err := llmutil.CallLLM(ctx, cfg, prompt, 1024, 0.2)
	if err != nil {
		return nil, err
	}
	return parseResult(raw)
}

func parseResult(raw string) (*Result, error) {
	// Strip markdown code blocks
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		if idx := strings.Index(cleaned, "\n"); idx > 0 {
			cleaned = cleaned[idx+1:]
		}
		if i := strings.LastIndex(cleaned, "```"); i >= 0 {
			cleaned = cleaned[:i]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var result Result
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parse consolidation result: %w", err)
	}
	return &result, nil
}

const consolidatePrompt = `你是一个记忆合并分析器。分析以下记忆，发现它们之间的关联、模式和洞察。

要求：
1. 找出记忆之间的关联关系（因果、补充、对比、时序等）
2. 提炼跨记忆的模式和规律
3. 生成一句话核心洞察（insight）
4. 只返回 JSON，不要其他文字

输出格式：
{
  "source_ids": ["id1", "id2"],
  "summary": "跨记忆综合摘要",
  "insight": "最有价值的一句话洞察",
  "patterns": ["发现的模式1", "模式2"],
  "connections": [
    {"from_id": "id1", "to_id": "id2", "relationship": "关系描述"}
  ]
}

记忆列表：
%s`
