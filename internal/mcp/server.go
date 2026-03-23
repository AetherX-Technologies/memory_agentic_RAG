// Package mcp implements a Model Context Protocol (MCP) server for the memory system.
// It exposes memory operations as JSON-RPC tools over stdio, compatible with
// Claude Code and other MCP-aware AI agents.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

// Server is the MCP server that handles JSON-RPC requests over stdio.
type Server struct {
	store        store.Store
	embedder     store.Embedder
	consolidator *consolidate.Consolidator // nil if LLM not configured
	handlers     map[string]ToolHandler
	config       Config
	mu           sync.Mutex
	stdoutBuf    *bufio.Writer // reusable buffered writer for stdout
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

// New creates a new MCP server. Consolidator is optional (nil if LLM not configured).
func New(s store.Store, embedder store.Embedder, cfg Config, cons ...*consolidate.Consolidator) *Server {
	srv := &Server{
		store:    s,
		embedder: embedder,
		handlers: make(map[string]ToolHandler),
		config:   cfg,
	}
	if len(cons) > 0 {
		srv.consolidator = cons[0]
	}
	srv.registerTools()
	return srv
}

// Run starts the MCP server using standard MCP stdio framing (Content-Length headers).
// Also supports legacy newline-delimited JSON for backward compatibility with Claude Code.
func (s *Server) Run(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	s.stdoutBuf = bufio.NewWriter(os.Stdout)

	for {
		// Peek at the first bytes to detect framing mode
		peek, err := reader.Peek(1)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var body []byte
		useCL := false // use Content-Length framing for response?
		if peek[0] == 'C' || peek[0] == 'c' {
			body, err = readContentLengthMessage(reader)
			useCL = true
		} else if peek[0] == '{' {
			body, err = readLineMessage(reader)
		} else {
			reader.ReadByte()
			continue
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			if _, fatal := err.(*fatalFrameError); fatal {
				s.writeResp(os.Stdout, &JSONRPCResponse{
					JSONRPC: "2.0",
					Error:   &JSONRPCError{Code: -32700, Message: err.Error()},
				}, useCL)
				fmt.Fprintf(os.Stderr, "[memory] fatal frame error: %v\n", err)
				return nil
			}
			s.writeResp(os.Stdout, &JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: err.Error()},
			}, useCL)
			continue
		}
		if len(body) == 0 {
			continue
		}

		fmt.Fprintf(os.Stderr, "[mcp] recv %d bytes: %.80s\n", len(body), string(body))

		var req JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			s.writeResp(os.Stdout, &JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
			}, useCL)
			continue
		}

		resp := s.handleRequest(ctx, &req)
		if resp != nil {
			fmt.Fprintf(os.Stderr, "[mcp] send method=%s id=%v cl=%v\n", req.Method, req.ID, useCL)
			s.writeResp(os.Stdout, resp, useCL)
		}
	}
}

const maxContentLength = 64 * 1024 * 1024 // 64MB cap, same as legacy scanner

// fatalFrameError indicates the stdio stream is unrecoverable (e.g. oversized frame).
type fatalFrameError struct{ msg string }

func (e *fatalFrameError) Error() string { return e.msg }

// readContentLengthMessage reads a message framed with Content-Length header.
func readContentLengthMessage(reader *bufio.Reader) ([]byte, error) {
	// Read headers until empty line
	contentLength := 0
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			val := strings.TrimSpace(line[len("content-length:"):])
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %s", val)
			}
			contentLength = n
		}
	}
	if contentLength <= 0 {
		return nil, nil
	}
	if contentLength > maxContentLength {
		return nil, &fatalFrameError{msg: fmt.Sprintf("Content-Length %d exceeds max %d", contentLength, maxContentLength)}
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, err
}

// readLineMessage reads a single line (newline-delimited JSON).
func readLineMessage(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, err
	}
	return bytes.TrimRight(line, "\r\n"), nil
}

// writeResp dispatches to the correct framing based on how the request was received.
func (s *Server) writeResp(w io.Writer, resp *JSONRPCResponse, useContentLength bool) {
	if useContentLength {
		s.writeContentLength(w, resp)
	} else {
		s.writeLineJSON(w, resp)
	}
}

// writeLineJSON writes a JSON-RPC response as a single line (legacy mode).
func (s *Server) writeLineJSON(w io.Writer, resp *JSONRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if s.stdoutBuf != nil && w == os.Stdout {
		s.stdoutBuf.Write(body)
		s.stdoutBuf.WriteByte('\n')
		s.stdoutBuf.Flush()
	} else {
		w.Write(body)
		w.Write([]byte("\n"))
	}
}

// writeContentLength writes a JSON-RPC response with Content-Length framing.
// Uses reusable buffered writer + Flush to deliver complete frames without extra allocation.
func (s *Server) writeContentLength(w io.Writer, resp *JSONRPCResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if s.stdoutBuf != nil && w == os.Stdout {
		fmt.Fprintf(s.stdoutBuf, "Content-Length: %d\r\n\r\n", len(body))
		s.stdoutBuf.Write(body)
		s.stdoutBuf.Flush()
	} else {
		fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body))
		w.Write(body)
	}
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
	// Echo back the client's protocolVersion for compatibility.
	// MCP spec: server should return the highest version it supports
	// that is also supported by the client.
	clientVersion := "2024-11-05"
	if req.Params != nil {
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(req.Params, &params); err == nil && params.ProtocolVersion != "" {
			clientVersion = params.ProtocolVersion
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": clientVersion,
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
			"content": []map[string]interface{}{
				{"type": "text", "text": string(text)},
			},
			"isError": false,
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
