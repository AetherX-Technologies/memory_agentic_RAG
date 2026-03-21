package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// mockStore and mockEmbedder for testing
type mockStore struct {
	memories map[string]mockMemory
	lastID   int
}

type mockMemory struct {
	id, text, memType, scope string
	importance, confidence   float64
	deletedAt                int64
}

type mockEmbedder struct{}

func (m *mockEmbedder) Embed(text string) ([]float32, error)            { return []float32{1, 0, 0}, nil }
func (m *mockEmbedder) EmbedBatch(texts []string) ([][]float32, error)  { return nil, nil }

func TestInitialize(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	resp := sendRequest(t, srv, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocol version: %v", result["protocolVersion"])
	}
}

func TestToolsList(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	resp := sendRequest(t, srv, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatal("expected tools array")
	}
	if len(tools) != 7 {
		t.Errorf("expected 7 tools, got %d", len(tools))
	}
}

func TestMemoryStore(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_store","arguments":{"content":"用户是一名工程师","type":"fact","importance":0.8}}}`
	resp := sendRequest(t, srv, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

func TestMemoryRecall(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"memory_recall","arguments":{"query":"工程师"}}}`
	resp := sendRequest(t, srv, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

func TestMemoryForget(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"memory_forget","arguments":{"id":"test-123"}}}`
	resp := sendRequest(t, srv, req)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error.Message)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`
	resp := sendRequest(t, srv, req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected -32601, got %d", resp.Error.Code)
	}
}

func TestUnknownTool(t *testing.T) {
	srv := newTestServer()

	req := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`
	resp := sendRequest(t, srv, req)

	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestFormatContext(t *testing.T) {
	results := []store.SearchResult{
		{Entry: store.Memory{Text: "回复用中文", MemoryType: "instruction", Importance: 0.9, Confidence: 1.0}},
		{Entry: store.Memory{Text: "用户是工程师", MemoryType: "fact", Importance: 0.8, Confidence: 0.9}},
	}

	formatted := formatContext(results, 1000)
	if !strings.Contains(formatted, "指令") {
		t.Error("expected formatted output to contain type label")
	}
	if !strings.Contains(formatted, "回复用中文") {
		t.Error("expected formatted output to contain memory text")
	}
	if !strings.Contains(formatted, "召回 2 条") {
		t.Error("expected count in header")
	}
}

func TestFormatContext_Empty(t *testing.T) {
	result := formatContext(nil, 1000)
	if !strings.Contains(result, "无相关记忆") {
		t.Error("expected empty message")
	}
}

// ── helpers ──

func newTestServer() *Server {
	st := newMinimalMockStore()
	emb := &mockEmbedder{}
	return New(st, emb, DefaultConfig())
}

func sendRequest(t *testing.T, srv *Server, reqJSON string) JSONRPCResponse {
	t.Helper()
	reader := strings.NewReader(reqJSON + "\n")
	var buf bytes.Buffer

	srv.RunWithIO(context.Background(), reader, &buf)

	var resp JSONRPCResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v (raw: %s)", err, buf.String())
	}
	return resp
}
