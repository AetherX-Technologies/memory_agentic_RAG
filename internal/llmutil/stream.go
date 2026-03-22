// Package llmutil provides shared LLM utilities including streaming SSE response handling.
package llmutil

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config holds LLM call configuration.
type Config struct {
	APIKey   string
	Model    string
	Endpoint string
	Timeout  int // seconds
}

// CallLLM makes an OpenAI-compatible chat completion request with SSE streaming support.
// Returns the full assembled response text.
func CallLLM(ctx context.Context, cfg Config, prompt string, maxTokens int, temperature float64) (string, error) {
	reqBody := map[string]interface{}{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"stream":      true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	client := &http.Client{Timeout: time.Duration(cfg.Timeout) * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	// Detect response type: SSE stream vs regular JSON
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") || strings.Contains(contentType, "stream") {
		return parseSSEStream(resp.Body)
	}

	// Fallback: try regular JSON response (for non-streaming endpoints and tests)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil {
		// Maybe it's SSE after all (some servers don't set content-type correctly)
		return parseSSEStream(strings.NewReader(string(data)))
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	content := apiResp.Choices[0].Message.Content
	if content == "" {
		content = apiResp.Choices[0].Delta.Content
	}
	if content == "" {
		// Try SSE parse as last resort
		return parseSSEStream(strings.NewReader(string(data)))
	}
	return content, nil
}

// parseSSEStream reads an SSE stream and assembles the content from delta chunks.
func parseSSEStream(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line
	var content strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {json}" or "data: [DONE]"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}
		if len(chunk.Choices) > 0 {
			content.WriteString(chunk.Choices[0].Delta.Content)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("SSE stream read error: %w", err)
	}
	result := content.String()
	if result == "" {
		return "", fmt.Errorf("empty response from SSE stream")
	}
	return result, nil
}
