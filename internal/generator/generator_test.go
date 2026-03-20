package generator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// mockLLMServer creates a test HTTP server that mimics OpenAI chat completions API.
func mockLLMServer(t *testing.T, response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Bearer") {
			t.Error("missing Authorization header")
		}

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": response,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestGenerateL0(t *testing.T) {
	server := mockLLMServer(t, "OpenViking实现分层检索以提高文档检索精度。")
	defer server.Close()

	gen, err := New(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
		Model:    "test-model",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	result, err := gen.GenerateL0(context.Background(), "OpenViking是一个分层检索系统，它通过递归搜索和分数传播来提高文档检索精度。")
	if err != nil {
		t.Fatalf("GenerateL0 failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty L0 result")
	}
	t.Logf("L0: %s", result)
}

func TestGenerateL1(t *testing.T) {
	server := mockLLMServer(t, "核心主题：分层检索系统\n主要内容：\n- 递归搜索\n- 分数传播\n- 按需加载")
	defer server.Close()

	gen, err := New(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	result, err := gen.GenerateL1(context.Background(), "OpenViking是一个分层检索系统。")
	if err != nil {
		t.Fatalf("GenerateL1 failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty L1 result")
	}
	t.Logf("L1: %s", result)
}

func TestGenerateBatch(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "summary"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gen, err := New(Config{
		APIKey:      "test-key",
		Endpoint:    server.URL,
		Concurrency: 3,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	contents := []string{"text1", "text2", "text3", "text4", "text5"}
	results, err := gen.GenerateBatch(context.Background(), contents, 0)
	if err != nil {
		t.Fatalf("GenerateBatch failed: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	for i, r := range results {
		if r == "" {
			t.Errorf("result[%d] is empty", i)
		}
	}

	// Verify all were called (none cached initially)
	if atomic.LoadInt32(&callCount) != 5 {
		t.Errorf("expected 5 LLM calls, got %d", callCount)
	}
}

func TestCacheHit(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "cached-result"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gen, err := New(Config{APIKey: "test-key", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	content := "Same content twice."

	// First call — should hit LLM
	r1, _ := gen.GenerateL0(ctx, content)
	// Second call — should hit cache
	r2, _ := gen.GenerateL0(ctx, content)

	if r1 != r2 {
		t.Errorf("cache miss: %q != %q", r1, r2)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 LLM call (cache hit), got %d", callCount)
	}
}

func TestFallbackOnError(t *testing.T) {
	// Server returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	gen, err := New(Config{
		APIKey:     "test-key",
		Endpoint:   server.URL,
		MaxRetries: 0, // no retries for fast test
	})
	if err != nil {
		t.Fatal(err)
	}

	// L0 fallback
	result, err := gen.GenerateL0(context.Background(), "这是一段测试内容。它应该被提取第一句。")
	if err != nil {
		t.Fatalf("GenerateL0 should not return error on fallback: %v", err)
	}
	if result == "" {
		t.Error("expected fallback L0 result")
	}
	t.Logf("L0 fallback: %s", result)

	// L1 fallback
	result, err = gen.GenerateL1(context.Background(), "第一句话。第二句话。第三句话。第四句话。第五句话。第六句话。")
	if err != nil {
		t.Fatalf("GenerateL1 should not return error on fallback: %v", err)
	}
	if result == "" {
		t.Error("expected fallback L1 result")
	}
	t.Logf("L1 fallback: %s", result)
}

func TestExtractL0Fallback(t *testing.T) {
	tests := []struct {
		content string
		wantLen int // minimum expected length
	}{
		{"这是第一句。后面还有更多。", 3},
		{"First sentence. More text.", 5},
		{"No sentence ending here", 5},
		{"", 0},
	}
	for _, tt := range tests {
		result := extractL0Fallback(tt.content)
		if len([]rune(result)) < tt.wantLen {
			t.Errorf("extractL0Fallback(%q) = %q, too short (want >= %d runes)", tt.content, result, tt.wantLen)
		}
	}
}

func TestExtractL1Fallback(t *testing.T) {
	content := "第一句话。第二句话。第三句话。第四句话。第五句话。第六句话。第七句话。"
	result := extractL1Fallback(content)
	if result == "" {
		t.Error("expected non-empty L1 fallback")
	}
	// Should contain at most 5 sentences
	count := strings.Count(result, "。")
	if count > 5 {
		t.Errorf("expected <= 5 sentences, got %d in %q", count, result)
	}
	t.Logf("L1 fallback (%d sentences): %s", count, result)
}

func TestNewRequiresAPIKey(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Error("expected error for empty API key")
	}
}

func TestGenerateEmptyContent(t *testing.T) {
	gen, _ := New(Config{APIKey: "test"})
	r0, _ := gen.GenerateL0(context.Background(), "")
	r1, _ := gen.GenerateL1(context.Background(), "")
	if r0 != "" || r1 != "" {
		t.Errorf("empty content should return empty result, got L0=%q L1=%q", r0, r1)
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 3, "hel"},
		{"你好世界", 2, "你好"},
		{"abc", 10, "abc"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncateRunes(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

func TestCacheKeyDiffersByLevel(t *testing.T) {
	c := NewCache()
	c.Set("same content", 0, "L0 result")
	c.Set("same content", 1, "L1 result")

	v0, _ := c.Get("same content", 0)
	v1, _ := c.Get("same content", 1)

	if v0 == v1 {
		t.Error("L0 and L1 cache should have different keys")
	}
	if v0 != "L0 result" || v1 != "L1 result" {
		t.Errorf("cache mismatch: L0=%q L1=%q", v0, v1)
	}
}
