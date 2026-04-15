package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/dedup"
	"github.com/yourusername/hybridmem-rag/internal/llmutil"
	"github.com/yourusername/hybridmem-rag/internal/memservice"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

type Handler struct {
	store          store.Store
	service        *memservice.Service
	singleToolMode bool
}

func NewHandler(store store.Store) *Handler {
	return NewHandlerWithDeps(store, nil, nil)
}

// NewHandlerWithDeps creates an HTTP handler with optional dependencies.
//
// Deprecated: prefer NewHandlerWithLLM for proper YAML/env LLM resolution.
func NewHandlerWithDeps(st store.Store, embedder store.Embedder, consolidator *consolidate.Consolidator) *Handler {
	svc := memservice.New(st, embedder, consolidator)
	if embedder != nil {
		d := dedup.New(st, embedder, dedup.DefaultConfig())
		svc.SetDedup(d)
	}
	return &Handler{
		store:          st,
		service:        svc,
		singleToolMode: os.Getenv("MEMORY_SINGLE_TOOL") == "1",
	}
}

// NewHandlerWithLLM creates an HTTP handler with explicit resolved LLM config.
// Use this from bootstrap to ensure dedup gets the proper YAML/env-resolved settings.
func NewHandlerWithLLM(st store.Store, embedder store.Embedder, consolidator *consolidate.Consolidator, mainLLM llmutil.ResolvedLLMConfig) *Handler {
	svc := memservice.New(st, embedder, consolidator)
	if embedder != nil {
		dedupCfg := dedup.DefaultConfigFromLLM(mainLLM.APIKey, mainLLM.Model, mainLLM.Endpoint, mainLLM.Timeout)
		d := dedup.New(st, embedder, dedupCfg)
		svc.SetDedup(d)
	}
	return &Handler{
		store:          st,
		service:        svc,
		singleToolMode: os.Getenv("MEMORY_SINGLE_TOOL") == "1",
	}
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type SuccessResponse struct {
	ID string `json:"id"`
}

const maxRequestBodySize = 10 << 20 // 10MB

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

func extractMemoryID(path string) (string, error) {
	id := strings.TrimPrefix(path, "/api/memories/")
	if id == "" || id == "search" || id == "stats" {
		return "", fmt.Errorf("invalid memory id")
	}
	if strings.ContainsAny(id, "/\\..") {
		return "", fmt.Errorf("invalid memory id")
	}
	return id, nil
}

// GET /api/health
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": "1.0.0", // TODO: Use build version from ldflags
	})
}

// POST /api/memories
func (h *Handler) CreateMemory(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var memory store.Memory
	if err := json.NewDecoder(r.Body).Decode(&memory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	id, err := h.store.Insert(&memory)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, SuccessResponse{ID: id})
}

// GET /api/memories/search
// Supports X-API-Version header: "v1" (default, returns full content) or "v2" (returns abstract + contentURL).
func (h *Handler) SearchMemories(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	currentPath := r.URL.Query().Get("current_path")
	scopesParam := r.URL.Query().Get("scope")
	var scopes []string
	if scopesParam != "" {
		for _, s := range strings.Split(scopesParam, ",") {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				scopes = append(scopes, trimmed)
			}
		}
	}

	opts := memservice.DefaultLegacyRecallOptions()
	opts.Scopes = scopes
	opts.CurrentPath = currentPath

	resp, err := h.service.Recall(r.Context(), memservice.RecallRequest{
		Query: query,
		Limit: limit,
	}, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results := resp.Memories

	// API versioning: v2 omits full content, provides lazy-load URL
	apiVersion := r.Header.Get("X-API-Version")
	if apiVersion == "v2" {
		type v2Entry struct {
			ID         string  `json:"id"`
			Abstract   string  `json:"abstract"`
			Overview   string  `json:"overview,omitempty"`
			Score      float64 `json:"score"`
			SourceFile string  `json:"source_file,omitempty"`
			ChunkCount int     `json:"chunk_count,omitempty"`
			ContentURL string  `json:"content_url"`
		}
		v2Results := make([]v2Entry, len(results))
		for i, res := range results {
			v2Results[i] = v2Entry{
				ID:         res.Entry.ID,
				Abstract:   res.Entry.Abstract,
				Overview:   res.Entry.Overview,
				Score:      res.Score,
				SourceFile: res.Entry.SourceFile,
				ChunkCount: res.ChunkCount,
				ContentURL: fmt.Sprintf("/api/memories/%s/content", res.Entry.ID),
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"results": v2Results,
			"version": "v2",
		})
		return
	}

	// v1 (default): return full results with content
	writeJSON(w, http.StatusOK, results)
}

// GET /api/memories/:id/content — lazy-load full L2 content
func (h *Handler) GetMemoryContent(w http.ResponseWriter, r *http.Request) {
	id, err := extractContentID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	content, err := h.store.GetContent(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      id,
		"content": content,
	})
}

// extractContentID extracts and validates the memory ID from a /api/memories/{id}/content path.
func extractContentID(path string) (string, error) {
	// Expected format: /api/memories/{id}/content
	trimmed := strings.TrimPrefix(path, "/api/memories/")
	if !strings.HasSuffix(trimmed, "/content") {
		return "", fmt.Errorf("invalid content path")
	}
	id := strings.TrimSuffix(trimmed, "/content")
	if id == "" {
		return "", fmt.Errorf("missing memory id")
	}
	// Reject path traversal and nested paths
	if strings.ContainsAny(id, "/\\..") {
		return "", fmt.Errorf("invalid memory id")
	}
	return id, nil
}

// DELETE /api/memories/:id
func (h *Handler) DeleteMemory(w http.ResponseWriter, r *http.Request) {
	id, err := extractMemoryID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/memories/:id
func (h *Handler) UpdateMemory(w http.ResponseWriter, r *http.Request) {
	id, err := extractMemoryID(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var memory store.Memory
	if err := json.NewDecoder(r.Body).Decode(&memory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	memory.ID = id
	if _, err := h.store.Insert(&memory); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, SuccessResponse{ID: id})
}

// GET /api/memories/stats
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"total": 0,
	}
	writeJSON(w, http.StatusOK, stats)
}
