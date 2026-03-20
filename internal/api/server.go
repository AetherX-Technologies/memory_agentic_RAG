package api

import (
	"net/http"
	"strings"
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/api/health":
		h.HealthCheck(w, r)
	case path == "/api/memories" && r.Method == http.MethodPost:
		h.CreateMemory(w, r)
	case path == "/api/memories/search":
		h.SearchMemories(w, r)
	case path == "/api/memories/stats":
		h.GetStats(w, r)
	case strings.HasSuffix(path, "/content") && strings.HasPrefix(path, "/api/memories/") && r.Method == http.MethodGet:
		h.GetMemoryContent(w, r)
	case strings.HasSuffix(path, "/content") && strings.HasPrefix(path, "/api/memories/"):
		writeError(w, http.StatusMethodNotAllowed, "only GET is allowed for /content")
	case strings.HasPrefix(path, "/api/memories/") && r.Method == http.MethodDelete:
		h.DeleteMemory(w, r)
	case strings.HasPrefix(path, "/api/memories/") && r.Method == http.MethodPut:
		h.UpdateMemory(w, r)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}
