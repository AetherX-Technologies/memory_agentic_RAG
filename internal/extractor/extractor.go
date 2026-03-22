// Package extractor implements AI memory extraction from conversations.
// It uses an LLM to identify long-term-valuable information from user/AI exchanges,
// and falls back to rule-based extraction when the LLM is unavailable.
package extractor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/llmutil"
)

// Config holds configuration for the memory extractor.
type Config struct {
	APIKey     string // LLM API key (required)
	Model      string // Model name (default: "gpt-4o-mini")
	Endpoint   string // OpenAI-compatible endpoint
	Timeout    int    // Request timeout in seconds (default: 30)
	MaxRetries int    // Max retries on LLM failure (default: 2)
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Model:      "gpt-4o-mini",
		Endpoint:   "https://api.openai.com/v1/chat/completions",
		Timeout:    30,
		MaxRetries: 2,
	}
}

// RawMemory represents a single memory extracted by the LLM before validation.
type RawMemory struct {
	Content    string   `json:"content"`
	Type       string   `json:"type"`
	Importance float64  `json:"importance"`
	Confidence float64  `json:"confidence"`
	Tags       []string `json:"tags"`
}

// ExtractedMemory is a validated, ready-to-store memory with content hash.
type ExtractedMemory struct {
	Content     string
	MemoryType  string
	Importance  float64
	Confidence  float64
	Tags        []string
	ContentHash string // SHA256[:16] for dedup
	SourceConv  string // Conversation ID
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Extractor extracts memories from conversations using an LLM with fallback.
type Extractor struct {
	config Config
}

// New creates a new Extractor. If APIKey is empty, only fallback extraction is available.
func New(config Config) *Extractor {
	if config.Model == "" {
		config.Model = "gpt-4o-mini"
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://api.openai.com/v1/chat/completions"
	}
	if config.Timeout <= 0 {
		config.Timeout = 30
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 2
	}

	return &Extractor{
		config: config,
	}
}

// Extract extracts memories from a conversation.
// Returns validated memories with content hashes, ready for dedup and insertion.
// Falls back to rule-based extraction if LLM is unavailable.
func (e *Extractor) Extract(ctx context.Context, messages []Message, convID string) ([]ExtractedMemory, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	var raws []RawMemory
	var err error

	if e.config.APIKey != "" {
		raws, err = e.extractWithLLM(ctx, messages)
		if err != nil {
			// Fallback on LLM failure
			raws = extractFallback(messages)
		}
	} else {
		raws = extractFallback(messages)
	}

	if len(raws) == 0 {
		return nil, nil
	}

	// Validate, convert, and deduplicate by content hash
	seen := make(map[string]bool)
	var result []ExtractedMemory
	for _, raw := range raws {
		validated, ok := validateRaw(raw)
		if !ok {
			continue
		}
		hash := computeHash(validated.Content)
		if seen[hash] {
			continue // skip duplicate within same extraction batch
		}
		seen[hash] = true
		result = append(result, ExtractedMemory{
			Content:     validated.Content,
			MemoryType:  validated.Type,
			Importance:  validated.Importance,
			Confidence:  validated.Confidence,
			Tags:        validated.Tags,
			ContentHash: hash,
			SourceConv:  convID,
		})
	}

	return result, nil
}

// extractWithLLM calls the LLM to extract memories.
func (e *Extractor) extractWithLLM(ctx context.Context, messages []Message) ([]RawMemory, error) {
	conversation := formatConversation(messages)
	prompt := fmt.Sprintf(extractionPrompt, conversation)

	var lastErr error
	for attempt := 0; attempt <= e.config.MaxRetries; attempt++ {
		result, err := e.callLLM(ctx, prompt)
		if err != nil {
			lastErr = err
			if attempt < e.config.MaxRetries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt+1) * time.Second):
				}
			}
			continue
		}

		memories, err := parseExtraction(result)
		if err != nil {
			lastErr = err
			continue
		}
		return memories, nil
	}

	return nil, fmt.Errorf("extraction failed after %d attempts: %w", e.config.MaxRetries+1, lastErr)
}

// callLLM makes an OpenAI-compatible chat completion request (with SSE streaming support).
func (e *Extractor) callLLM(ctx context.Context, prompt string) (string, error) {
	cfg := llmutil.Config{
		APIKey:   e.config.APIKey,
		Model:    e.config.Model,
		Endpoint: e.config.Endpoint,
		Timeout:  e.config.Timeout,
	}
	return llmutil.CallLLM(ctx, cfg, prompt, 1024, 0.2)
}

// formatConversation formats messages into a readable string for the prompt.
func formatConversation(messages []Message) string {
	var buf bytes.Buffer
	for _, m := range messages {
		role := "用户"
		if m.Role == "assistant" {
			role = "AI"
		}
		buf.WriteString(role)
		buf.WriteString("：")
		buf.WriteString(m.Content)
		buf.WriteByte('\n')
	}
	return buf.String()
}

// computeHash returns SHA256[:16] hex of the content.
func computeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:8])
}

// Extraction prompt with few-shot examples.
const extractionPrompt = `你是一个记忆提取器。从以下对话中提取值得长期记住的信息。

规则：
1. 只提取有长期价值的信息，忽略寒暄和临时内容
2. 每条记忆是独立的陈述句
3. 分类为：fact / preference / skill / episode / instruction / relationship
4. 评估 importance (0.0-1.0) 和 confidence (0.0-1.0)（系统会自动下限到 0.05）
5. 提取标签（辅助检索）
6. 没有值得记住的信息就返回 []

示例输入：
用户："我是西北院的水利工程师，Python 用了5年，以后回复请用中文"
AI："好的，我记住了。"

示例输出：
[
  {"content": "用户是西北院的水利工程师", "type": "fact", "importance": 0.8, "confidence": 0.9, "tags": ["西北院", "水利", "工程师"]},
  {"content": "用户有5年Python使用经验", "type": "skill", "importance": 0.7, "confidence": 0.9, "tags": ["Python", "编程"]},
  {"content": "回复请使用中文", "type": "instruction", "importance": 0.9, "confidence": 1.0, "tags": ["语言", "中文"]}
]

示例输入：
用户："今天天气不错"
AI："是啊，春天到了。"

示例输出：
[]

---
对话内容：
%s`
