// Package mcp implements a Model Context Protocol (MCP) server for the memory system.
// It exposes memory operations as JSON-RPC tools over stdio, compatible with
// Claude Code and other MCP-aware AI agents.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// Server is the MCP server that handles JSON-RPC requests over stdio.
type Server struct {
	store    store.Store
	embedder store.Embedder
	handlers map[string]ToolHandler
	config   Config
	mu       sync.Mutex
}

// ToolHandler processes a tool call and returns the result content.
type ToolHandler func(ctx context.Context, params json.RawMessage) (interface{}, error)

// Config holds MCP server configuration.
type Config struct {
	ServerName    string
	ServerVersion string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		ServerName:    "hybridmem-rag",
		ServerVersion: "1.0.0",
	}
}

// New creates a new MCP server.
func New(s store.Store, embedder store.Embedder, cfg Config) *Server {
	srv := &Server{
		store:    s,
		embedder: embedder,
		handlers: make(map[string]ToolHandler),
		config:   cfg,
	}
	srv.registerTools()
	return srv
}

// Run starts the MCP server, reading JSON-RPC from stdin and writing to stdout.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024) // 64MB max for large imports
	writer := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(writer, nil, -32700, "Parse error")
			continue
		}

		resp := s.handleRequest(ctx, &req)
		if resp != nil {
			writer.Encode(resp)
		}
	}

	return scanner.Err()
}

// RunWithIO runs the server with custom reader/writer (for testing).
func (s *Server) RunWithIO(ctx context.Context, reader io.Reader, writer io.Writer) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	enc := json.NewEncoder(writer)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeError(enc, nil, -32700, "Parse error")
			continue
		}

		resp := s.handleRequest(ctx, &req)
		if resp != nil {
			enc.Encode(resp)
		}
	}

	return scanner.Err()
}

func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "notifications/initialized":
		return nil // notification, no response
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    s.config.ServerName,
				"version": s.config.ServerVersion,
			},
		},
	}
}

func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": toolDefinitions,
		},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32602, Message: "Invalid params"},
		}
	}

	handler, ok := s.handlers[call.Name]
	if !ok {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32602, Message: fmt.Sprintf("Unknown tool: %s", call.Name)},
		}
	}

	result, err := handler(ctx, call.Arguments)
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]string{
					{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
				},
				"isError": true,
			},
		}
	}

	text, _ := json.Marshal(result)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]string{
				{"type": "text", "text": string(text)},
			},
		},
	}
}

func (s *Server) writeError(enc *json.Encoder, id interface{}, code int, msg string) {
	enc.Encode(&JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: msg},
	})
}
