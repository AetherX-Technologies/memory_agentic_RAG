package consolidate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseResult(t *testing.T) {
	raw := `{"source_ids":["a","b"],"summary":"test summary","insight":"key insight","patterns":["p1"],"connections":[{"from_id":"a","to_id":"b","relationship":"related"}]}`
	result, err := parseResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "test summary" {
		t.Errorf("summary = %q", result.Summary)
	}
	if result.Insight != "key insight" {
		t.Errorf("insight = %q", result.Insight)
	}
	if len(result.SourceIDs) != 2 {
		t.Errorf("source_ids count = %d", len(result.SourceIDs))
	}
	if len(result.Connections) != 1 {
		t.Errorf("connections count = %d", len(result.Connections))
	}
}

func TestParseResult_Markdown(t *testing.T) {
	raw := "```json\n" + `{"summary":"s","insight":"i","patterns":[],"connections":[]}` + "\n```"
	result, err := parseResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary != "s" {
		t.Errorf("summary = %q", result.Summary)
	}
}

func TestConsolidate_WithMockLLM(t *testing.T) {
	// Mock LLM
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{
					"content": `{"source_ids":["m1","m2"],"summary":"用户是Go开发者","insight":"Go+北京","patterns":["技术人员"],"connections":[{"from_id":"m1","to_id":"m2","relationship":"同一用户"}]}`,
				}},
			},
		})
	}))
	defer server.Close()

	ms := &mockConsolidateStore{
		unconsolidated: []*mockMem{
			{id: "m1", text: "用户是Go开发者", memType: "fact"},
			{id: "m2", text: "用户在北京工作", memType: "fact"},
		},
	}

	c := New(ms, Config{
		LLMAPIKey:   "test",
		LLMEndpoint: server.URL,
		MaxMemories: 50,
	})

	result, err := c.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected consolidation result")
	}
	if result.Insight != "Go+北京" {
		t.Errorf("insight = %q", result.Insight)
	}
	if !ms.consolidated {
		t.Error("memories should be marked as consolidated")
	}
	if ms.consolidationCount != 1 {
		t.Errorf("expected 1 consolidation stored, got %d", ms.consolidationCount)
	}
}

func TestConsolidate_NotEnoughMemories(t *testing.T) {
	ms := &mockConsolidateStore{
		unconsolidated: []*mockMem{
			{id: "m1", text: "only one"},
		},
	}
	c := New(ms, DefaultConfig())
	result, err := c.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for insufficient memories")
	}
}
