// Package generator provides L0/L1 summary generation for the OpenViking hierarchical retrieval system.
//
// L0 (abstract): A one-sentence summary (~50 chars) for quick preview.
// L1 (overview): A structured overview (200-500 chars) used as the primary vector search target.
// L2 (content):  The full original text (stored directly, not generated).
package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Config holds configuration for the summary generator.
type Config struct {
	APIKey      string // LLM API key
	Model       string // Model name (default: "gpt-4o-mini")
	Endpoint    string // OpenAI-compatible endpoint (default: "https://api.openai.com/v1/chat/completions")
	Timeout     int    // Request timeout in seconds (default: 30)
	Concurrency int    // Max concurrent LLM calls in batch mode (default: 5)
	MaxRetries  int    // Max retries on failure (default: 2)
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Model:       "gpt-4o-mini",
		Endpoint:    "https://api.openai.com/v1/chat/completions",
		Timeout:     30,
		Concurrency: 5,
		MaxRetries:  2,
	}
}

// L0 prompt: one-sentence summary, ~50 chars.
const l0Prompt = `请用一句话（不超过 50 字）概括以下内容的核心主题。
要求：
1. 突出关键信息和主要观点
2. 使用陈述句，不要使用疑问句
3. 不要包含"本文"、"这段内容"等元指称

内容：
%s

摘要：`

// L1 prompt: structured overview, 200-500 chars.
const l1Prompt = `请为以下内容生成结构化概览（200-500 字）。

格式要求：
1. 核心主题：[一句话说明主题]
2. 主要内容：[3-5 个要点，每个要点一行]
3. 关键信息：[重要的数据、结论或观点]

内容：
%s

概览：`

// Generator generates L0 and L1 summaries using an LLM with caching and fallback.
type Generator struct {
	config Config
	client *http.Client
	cache  *Cache
}

// New creates a new Generator. Returns an error if the API key is empty.
func New(config Config) (*Generator, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("LLM API key is required")
	}
	if config.Model == "" {
		config.Model = "gpt-4o-mini"
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://api.openai.com/v1/chat/completions"
	}
	if config.Timeout <= 0 {
		config.Timeout = 30
	}
	if config.Concurrency <= 0 {
		config.Concurrency = 5
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 2
	}

	return &Generator{
		config: config,
		client: &http.Client{Timeout: time.Duration(config.Timeout) * time.Second},
		cache:  NewCache(),
	}, nil
}

// GenerateL0 generates a one-sentence abstract (~50 chars) for the given content.
// Uses cache if available. Falls back to rule-based extraction on LLM failure.
func (g *Generator) GenerateL0(ctx context.Context, content string) (string, error) {
	if content == "" {
		return "", nil
	}

	// Check cache
	if cached, ok := g.cache.Get(content, 0); ok {
		return cached, nil
	}

	// Call LLM
	prompt := fmt.Sprintf(l0Prompt, truncateRunes(content, 4000))
	result, err := g.callLLMWithRetry(ctx, prompt, 100)
	if err != nil {
		// Fallback: rule-based extraction
		result = extractL0Fallback(content)
	}

	g.cache.Set(content, 0, result)
	return result, nil
}

// GenerateL1 generates a structured overview (200-500 chars) for the given content.
// Uses cache if available. Falls back to rule-based extraction on LLM failure.
func (g *Generator) GenerateL1(ctx context.Context, content string) (string, error) {
	if content == "" {
		return "", nil
	}

	// Check cache
	if cached, ok := g.cache.Get(content, 1); ok {
		return cached, nil
	}

	// Call LLM
	prompt := fmt.Sprintf(l1Prompt, truncateRunes(content, 4000))
	result, err := g.callLLMWithRetry(ctx, prompt, 800)
	if err != nil {
		// Fallback: rule-based extraction
		result = extractL1Fallback(content)
	}

	g.cache.Set(content, 1, result)
	return result, nil
}

// GenerateBatch generates L0 or L1 summaries for multiple contents concurrently.
// level: 0 for L0 (abstract), 1 for L1 (overview).
func (g *Generator) GenerateBatch(ctx context.Context, contents []string, level int) ([]string, error) {
	results := make([]string, len(contents))

	// Phase 1: fill from cache, collect uncached indices
	var uncached []int
	for i, content := range contents {
		if cached, ok := g.cache.Get(content, level); ok {
			results[i] = cached
		} else {
			uncached = append(uncached, i)
		}
	}

	if len(uncached) == 0 {
		return results, nil
	}

	// Phase 2: concurrent LLM calls with concurrency limit
	sem := make(chan struct{}, g.config.Concurrency)
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup

	for _, idx := range uncached {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			var result string
			var err error
			if level == 0 {
				result, err = g.GenerateL0(ctx, contents[i])
			} else {
				result, err = g.GenerateL1(ctx, contents[i])
			}

			mu.Lock()
			results[i] = result
			if err != nil && firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
		}(idx)
	}

	wg.Wait()
	// Note: firstErr is informational. Results always have values (fallback on error).
	return results, firstErr
}

// callLLMWithRetry calls the LLM API with retry logic.
func (g *Generator) callLLMWithRetry(ctx context.Context, prompt string, maxTokens int) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= g.config.MaxRetries; attempt++ {
		result, err := g.callLLM(ctx, prompt, maxTokens)
		if err == nil {
			return result, nil
		}
		lastErr = err
		// Brief backoff before retry
		if attempt < g.config.MaxRetries {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return "", fmt.Errorf("LLM call failed after %d attempts: %w", g.config.MaxRetries+1, lastErr)
}

// callLLM makes a single OpenAI-compatible chat completion request.
func (g *Generator) callLLM(ctx context.Context, prompt string, maxTokens int) (string, error) {
	reqBody := map[string]interface{}{
		"model": g.config.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  maxTokens,
		"temperature": 0.3,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", g.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("LLM API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("LLM API error %d: %s", resp.StatusCode, string(respBody))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}

	return apiResp.Choices[0].Message.Content, nil
}

// truncateRunes truncates a string to at most n runes.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
