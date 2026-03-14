package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

type Handler struct {
	store store.Store
}

func NewHandler(store store.Store) *Handler {
	return &Handler{store: store}
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
func (h *Handler) SearchMemories(w http.ResponseWriter, r *http.Request) {
	results := []store.SearchResult{}
	writeJSON(w, http.StatusOK, results)
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
