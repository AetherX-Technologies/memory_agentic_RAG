package extractor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseExtraction_ValidJSON(t *testing.T) {
	input := `[
		{"content": "用户是工程师", "type": "fact", "importance": 0.8, "confidence": 0.9, "tags": ["工程师"]},
		{"content": "喜欢简洁回复", "type": "preference", "importance": 0.7, "confidence": 0.85, "tags": ["回复风格"]}
	]`

	memories, err := parseExtraction(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(memories))
	}
	if memories[0].Content != "用户是工程师" {
		t.Errorf("expected '用户是工程师', got %q", memories[0].Content)
	}
	if memories[1].Type != "preference" {
		t.Errorf("expected 'preference', got %q", memories[1].Type)
	}
}

func TestParseExtraction_MarkdownWrapped(t *testing.T) {
	input := "```json\n" + `[{"content": "test", "type": "fact", "importance": 0.5, "confidence": 0.5}]` + "\n```"

	memories, err := parseExtraction(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
}

func TestParseExtraction_TextAroundJSON(t *testing.T) {
	input := `Here are the extracted memories:
[{"content": "用户在北京", "type": "fact", "importance": 0.6, "confidence": 0.8}]
That's all I found.`

	memories, err := parseExtraction(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
}

func TestParseExtraction_EmptyArray(t *testing.T) {
	memories, err := parseExtraction("[]")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("expected 0 memories, got %d", len(memories))
	}
}

func TestParseExtraction_Invalid(t *testing.T) {
	_, err := parseExtraction("this is not json at all")
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
	if _, ok := err.(*ParseError); !ok {
		t.Fatalf("expected ParseError, got %T", err)
	}
}

func TestValidateRaw(t *testing.T) {
	tests := []struct {
		name     string
		input    RawMemory
		wantOK   bool
		wantType string
		wantConf float64
	}{
		{
			name:     "valid",
			input:    RawMemory{Content: "test", Type: "fact", Importance: 0.8, Confidence: 0.9},
			wantOK:   true,
			wantType: "fact",
			wantConf: 0.9,
		},
		{
			name:   "empty content",
			input:  RawMemory{Content: "", Type: "fact"},
			wantOK: false,
		},
		{
			name:     "invalid type defaults to fact",
			input:    RawMemory{Content: "test", Type: "unknown", Importance: 0.5, Confidence: 0.5},
			wantOK:   true,
			wantType: "fact",
		},
		{
			name:     "confidence floor",
			input:    RawMemory{Content: "test", Type: "skill", Importance: 0.5, Confidence: 0.01},
			wantOK:   true,
			wantConf: 0.05,
		},
		{
			name:     "zero confidence gets default",
			input:    RawMemory{Content: "test", Type: "fact", Importance: 0.5, Confidence: 0},
			wantOK:   true,
			wantConf: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := validateRaw(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("validateRaw() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if tt.wantType != "" && result.Type != tt.wantType {
				t.Errorf("type = %q, want %q", result.Type, tt.wantType)
			}
			if tt.wantConf > 0 && result.Confidence != tt.wantConf {
				t.Errorf("confidence = %v, want %v", result.Confidence, tt.wantConf)
			}
		})
	}
}

func TestComputeHash(t *testing.T) {
	h1 := computeHash("hello world")
	h2 := computeHash("hello world")
	h3 := computeHash("different text")

	if h1 != h2 {
		t.Error("same content should produce same hash")
	}
	if h1 == h3 {
		t.Error("different content should produce different hash")
	}
	if len(h1) != 16 {
		t.Errorf("hash should be 16 hex chars, got %d", len(h1))
	}
}

func TestFallbackExtraction(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "我是一名数据科学家，我在北京工作"},
		{Role: "assistant", Content: "好的，了解了"},
		{Role: "user", Content: "我喜欢简洁的回复，以后用英文回复我"},
	}

	results := extractFallback(messages)
	if len(results) == 0 {
		t.Fatal("expected at least one fallback extraction")
	}

	// Check types
	hasInstruction := false
	hasFact := false
	for _, r := range results {
		if r.Type == "instruction" {
			hasInstruction = true
		}
		if r.Type == "fact" {
			hasFact = true
		}
		if r.Confidence != 0.3 {
			t.Errorf("fallback confidence should be 0.3, got %v", r.Confidence)
		}
	}
	if !hasFact {
		t.Error("expected at least one 'fact' extraction")
	}
	if !hasInstruction {
		t.Error("expected at least one 'instruction' extraction")
	}
}

func TestFallbackExtraction_NoMatch(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "今天天气不错"},
		{Role: "assistant", Content: "是啊"},
	}

	results := extractFallback(messages)
	if len(results) != 0 {
		t.Errorf("expected 0 extractions for chitchat, got %d", len(results))
	}
}

func TestExtract_WithMockLLM(t *testing.T) {
	// Mock LLM server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `[{"content": "用户是Go开发者", "type": "skill", "importance": 0.7, "confidence": 0.9, "tags": ["Go", "开发"]}]`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ext := New(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})

	memories, err := ext.Extract(context.Background(), []Message{
		{Role: "user", Content: "我用Go写了3年后端"},
		{Role: "assistant", Content: "了解"},
	}, "conv-123")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}

	m := memories[0]
	if m.Content != "用户是Go开发者" {
		t.Errorf("content = %q", m.Content)
	}
	if m.MemoryType != "skill" {
		t.Errorf("type = %q", m.MemoryType)
	}
	if m.ContentHash == "" {
		t.Error("content hash should not be empty")
	}
	if m.SourceConv != "conv-123" {
		t.Errorf("source_conv = %q", m.SourceConv)
	}
	if len(m.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(m.Tags))
	}
}

func TestExtract_LLMFallback(t *testing.T) {
	// Mock LLM that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ext := New(Config{
		APIKey:   "test-key",
		Endpoint: server.URL,
	})
	// Override retries to 0 for fast test
	ext.config.MaxRetries = 0

	memories, err := ext.Extract(context.Background(), []Message{
		{Role: "user", Content: "我是一名工程师，请以后用中文回复"},
	}, "conv-456")

	if err != nil {
		t.Fatalf("should not error on fallback: %v", err)
	}
	if len(memories) == 0 {
		t.Fatal("expected fallback extraction")
	}
}

func TestExtract_NoAPIKey(t *testing.T) {
	ext := New(Config{}) // no API key

	memories, err := ext.Extract(context.Background(), []Message{
		{Role: "user", Content: "我喜欢Python"},
	}, "conv-789")

	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
	if len(memories) == 0 {
		t.Fatal("expected fallback extraction for '我喜欢'")
	}
	if memories[0].MemoryType != "preference" {
		t.Errorf("expected 'preference', got %q", memories[0].MemoryType)
	}
}

func TestFormatConversation(t *testing.T) {
	result := formatConversation([]Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	})
	if result != "用户：hello\nAI：hi\n" {
		t.Errorf("unexpected format: %q", result)
	}
}

func TestSplitSentences(t *testing.T) {
	input := "我是工程师。我喜欢Go！你呢？"
	sentences := splitSentences(input)
	if len(sentences) != 3 {
		t.Errorf("expected 3 sentences, got %d: %v", len(sentences), sentences)
	}
}
