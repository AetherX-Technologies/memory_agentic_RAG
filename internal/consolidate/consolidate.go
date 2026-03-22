// Package consolidate implements memory consolidation — discovering patterns,
// connections, and insights across stored memories using LLM analysis.
package consolidate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
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
	client *http.Client
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
		client: &http.Client{Timeout: time.Duration(cfg.LLMTimeout) * time.Second},
	}
}

// Consolidate analyzes unconsolidated memories and creates a consolidation.
// Returns nil if not enough memories to consolidate.
func (c *Consolidator) Consolidate(ctx context.Context) (*store.Consolidation, error) {
	memories, err := c.store.ListUnconsolidated(c.config.MaxMemories)
	if err != nil {
		return nil, fmt.Errorf("list unconsolidated: %w", err)
	}
	if len(memories) < 2 {
		return nil, nil // not enough memories
	}

	// Format for LLM
	var sb strings.Builder
	ids := make([]string, len(memories))
	for i, m := range memories {
		ids[i] = m.ID
		sb.WriteString(fmt.Sprintf("- Memory #%s: %s (type=%s, importance=%.1f)\n",
			m.ID, m.Text, m.MemoryType, m.Importance))
	}

	// Call LLM
	result, err := c.callLLM(ctx, sb.String())
	if err != nil {
		return nil, fmt.Errorf("LLM consolidation: %w", err)
	}

	// Use LLM-provided source_ids if available, otherwise all
	sourceIDs := result.SourceIDs
	if len(sourceIDs) == 0 {
		sourceIDs = ids
	}

	// Serialize to JSON
	sourceIDsJSON, _ := json.Marshal(sourceIDs)
	patternsJSON, _ := json.Marshal(result.Patterns)
	connsJSON, _ := json.Marshal(result.Connections)

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

	// Update connections in individual memories (bidirectional)
	var connErrors []string
	for _, conn := range result.Connections {
		fromID := fmt.Sprint(conn["from_id"])
		toID := fmt.Sprint(conn["to_id"])
		rel := fmt.Sprint(conn["relationship"])
		if fromID != "" && toID != "" && fromID != "<nil>" && toID != "<nil>" {
			if err := c.store.AddConnection(fromID, toID, rel); err != nil {
				connErrors = append(connErrors, fmt.Sprintf("%s→%s: %v", fromID, toID, err))
			}
			if err := c.store.AddConnection(toID, fromID, rel); err != nil {
				connErrors = append(connErrors, fmt.Sprintf("%s→%s: %v", toID, fromID, err))
			}
		}
	}
	// Log connection errors (don't fail consolidation for partial graph issues)
	if len(connErrors) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d connection update errors during consolidation\n", len(connErrors))
	}

	// Mark source memories as consolidated
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
	reqBody := map[string]interface{}{
		"model": c.config.LLMModel,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  1024,
		"temperature": 0.2,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", c.config.LLMEndpoint, bytes.NewReader(body))
	if err != nil { return nil, err }
	req.Header.Set("Authorization", "Bearer "+c.config.LLMAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("LLM API error %d: %s", resp.StatusCode, string(b))
	}

	data, _ := io.ReadAll(resp.Body)
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil { return nil, err }
	if len(apiResp.Choices) == 0 { return nil, fmt.Errorf("no choices") }

	return parseResult(apiResp.Choices[0].Message.Content)
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
