package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

func TestCreateMemory(t *testing.T) {
	config := store.Config{DBPath: ":memory:", VectorDim: 3}
	st, err := store.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	handler := NewHandler(st)

	memory := store.Memory{
		Text:       "test memory",
		Category:   "test",
		Scope:      "global",
		Importance: 0.8,
		Vector:     []float32{0.1, 0.2, 0.3},
	}

	body, _ := json.Marshal(memory)
	req := httptest.NewRequest(http.MethodPost, "/api/memories", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.CreateMemory(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	var resp SuccessResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestDeleteMemory(t *testing.T) {
	config := store.Config{DBPath: ":memory:", VectorDim: 3}
	st, err := store.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	handler := NewHandler(st)

	memory := &store.Memory{
		Text:   "test",
		Scope:  "global",
		Vector: []float32{0.1, 0.2, 0.3},
	}
	id, _ := st.Insert(memory)

	req := httptest.NewRequest(http.MethodDelete, "/api/memories/"+id, nil)
	w := httptest.NewRecorder()

	handler.DeleteMemory(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestGetStats(t *testing.T) {
	config := store.Config{DBPath: ":memory:", VectorDim: 3}
	st, err := store.New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	handler := NewHandler(st)

	req := httptest.NewRequest(http.MethodGet, "/api/memories/stats", nil)
	w := httptest.NewRecorder()

	handler.GetStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
