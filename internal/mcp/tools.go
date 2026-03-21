package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// registerTools registers all MCP tool handlers.
func (s *Server) registerTools() {
	s.handlers["memory_store"] = s.handleMemoryStore
	s.handlers["memory_recall"] = s.handleMemoryRecall
	s.handlers["memory_forget"] = s.handleMemoryForget
	s.handlers["memory_update"] = s.handleMemoryUpdate
	s.handlers["memory_export"] = s.handleMemoryExport
	s.handlers["memory_import"] = s.handleMemoryImport
	s.handlers["memory_forget_by_tag"] = s.handleMemoryForgetByTag
}

// ── memory_store ──

func (s *Server) handleMemoryStore(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		Content    string   `json:"content"`
		Type       string   `json:"type"`
		Importance float64  `json:"importance"`
		Tags       []string `json:"tags"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if p.Type == "" {
		p.Type = "fact"
	}
	if p.Importance <= 0 {
		p.Importance = 0.7
	}
	if p.Importance > 1 {
		p.Importance = 1
	}

	// Compute content hash
	h := sha256.Sum256([]byte(p.Content))
	contentHash := hex.EncodeToString(h[:8])

	// Embed
	var vec []float32
	if s.embedder != nil {
		v, err := s.embedder.Embed(p.Content)
		if err == nil {
			vec = v
		}
	}

	mem := &store.Memory{
		Text:        p.Content,
		Category:    "memory",
		Scope:       "global",
		Importance:  p.Importance,
		MemoryType:  p.Type,
		Confidence:  0.9,
		ContentHash: contentHash,
		Vector:      vec,
	}

	id, err := s.store.Insert(mem)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":     id,
		"action": "stored",
	}, nil
}

// ── memory_recall ──

func (s *Server) handleMemoryRecall(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		Query         string   `json:"query"`
		Limit         int      `json:"limit"`
		Types         []string `json:"types"`
		MinImportance float64  `json:"min_importance"`
		MaxTokens     int      `json:"max_tokens"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}
	if p.MaxTokens <= 0 {
		p.MaxTokens = 1000
	}

	// Embed query
	var queryVec []float32
	if s.embedder != nil {
		v, err := s.embedder.Embed(p.Query)
		if err == nil {
			queryVec = v
		}
	}

	results, err := s.store.HybridSearch(queryVec, p.Query, p.Limit*3, nil)
	if err != nil {
		return nil, err
	}

	// Filter by types and importance
	var filtered []store.SearchResult
	for _, r := range results {
		if p.MinImportance > 0 && r.Entry.Importance < p.MinImportance {
			continue
		}
		if len(p.Types) > 0 && !contains(p.Types, r.Entry.MemoryType) {
			continue
		}
		filtered = append(filtered, r)
		if len(filtered) >= p.Limit {
			break
		}
	}

	// Format context
	formatted := formatContext(filtered, p.MaxTokens)

	return map[string]interface{}{
		"count":   len(filtered),
		"context": formatted,
		"memories": filtered,
	}, nil
}

// ── memory_forget ──

func (s *Server) handleMemoryForget(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	// Soft delete instead of hard delete
	if err := s.store.SoftDelete(p.ID, time.Now().Unix()); err != nil {
		return nil, err
	}

	return map[string]string{"status": "deleted", "id": p.ID}, nil
}

// ── memory_update ──

func (s *Server) handleMemoryUpdate(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		ID         string   `json:"id"`
		Content    string   `json:"content"`
		Importance *float64 `json:"importance"`
		Tags       []string `json:"tags"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	// Get existing memory
	existing, err := s.store.Get(p.ID)
	if err != nil {
		return nil, fmt.Errorf("memory not found: %w", err)
	}

	contentChanged := p.Content != "" && p.Content != existing.Text

	if !contentChanged {
		// Metadata-only update: persist importance directly
		if p.Importance != nil {
			if err := s.store.UpdateImportance(p.ID, *p.Importance); err != nil {
				return nil, fmt.Errorf("failed to update importance: %w", err)
			}
		}
		return map[string]string{
			"id":     p.ID,
			"status": "updated",
		}, nil
	}

	// Content changed: soft-delete old, insert new with new hash + embedding
	now := time.Now().Unix()
	if err := s.store.SoftDelete(p.ID, now); err != nil {
		return nil, fmt.Errorf("failed to soft-delete old memory: %w", err)
	}

	newImportance := existing.Importance
	if p.Importance != nil {
		newImportance = *p.Importance
	}

	var vec []float32
	if s.embedder != nil {
		v, err := s.embedder.Embed(p.Content)
		if err == nil {
			vec = v
		}
	}

	h := sha256.Sum256([]byte(p.Content))
	newMem := &store.Memory{
		Text:        p.Content,
		Category:    "memory",
		Scope:       existing.Scope,
		Importance:  newImportance,
		MemoryType:  existing.MemoryType,
		Confidence:  existing.Confidence,
		ContentHash: hex.EncodeToString(h[:8]),
		SourceConv:  existing.SourceConv,
		Vector:      vec,
	}

	newID, err := s.store.Insert(newMem)
	if err != nil {
		// Rollback soft-delete on insert failure
		s.store.Restore(p.ID)
		return nil, err
	}

	s.store.RecordSupersession(p.ID, newID)

	return map[string]string{
		"old_id": p.ID,
		"new_id": newID,
		"status": "updated",
	}, nil
}

// ── memory_export ──

func (s *Server) handleMemoryExport(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		Types          []string `json:"types"`
		IncludeExpired bool     `json:"include_expired"`
	}
	if params != nil {
		json.Unmarshal(params, &p)
	}

	// Use List with max limit to export all memories
	memories, err := s.store.List("global", 1000000)
	if err != nil {
		return nil, err
	}

	var exported []map[string]interface{}
	for _, m := range memories {
		if !p.IncludeExpired && m.Expired {
			continue
		}
		if len(p.Types) > 0 && !contains(p.Types, m.MemoryType) {
			continue
		}
		exported = append(exported, map[string]interface{}{
			"id":          m.ID,
			"content":     m.Text,
			"type":        m.MemoryType,
			"importance":  m.Importance,
			"confidence":  m.Confidence,
			"timestamp":   m.Timestamp,
			"source_conv": m.SourceConv,
		})
	}

	return map[string]interface{}{
		"count":    len(exported),
		"memories": exported,
	}, nil
}

// ── memory_import ──

func (s *Server) handleMemoryImport(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		Memories []struct {
			Content    string  `json:"content"`
			Type       string  `json:"type"`
			Importance float64 `json:"importance"`
			Confidence float64 `json:"confidence"`
			Timestamp  int64   `json:"timestamp"`
			SourceConv string  `json:"source_conv"`
		} `json:"memories"`
		// Note: overwrite is not yet implemented; collisions are skipped
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	imported, skipped := 0, 0
	for _, m := range p.Memories {
		if m.Content == "" {
			skipped++
			continue
		}
		if m.Type == "" {
			m.Type = "fact"
		}
		if m.Importance <= 0 {
			m.Importance = 0.5
		}
		if m.Importance > 1 {
			m.Importance = 1
		}
		if m.Confidence <= 0 {
			m.Confidence = 0.5
		}
		if m.Confidence > 1 {
			m.Confidence = 1
		}

		h := sha256.Sum256([]byte(m.Content))
		mem := &store.Memory{
			Text:        m.Content,
			Category:    "memory",
			Scope:       "global",
			Importance:  m.Importance,
			MemoryType:  m.Type,
			Confidence:  m.Confidence,
			Timestamp:   m.Timestamp,
			SourceConv:  m.SourceConv,
			ContentHash: hex.EncodeToString(h[:8]),
		}

		// Embed if possible
		if s.embedder != nil {
			if v, err := s.embedder.Embed(m.Content); err == nil {
				mem.Vector = v
			}
		}

		_, err := s.store.Insert(mem)
		if err != nil {
			skipped++ // content_hash collision or other error
		} else {
			imported++
		}
	}

	return map[string]int{
		"imported": imported,
		"skipped":  skipped,
	}, nil
}

// ── memory_forget_by_tag ──

func (s *Server) handleMemoryForgetByTag(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		Tag    string `json:"tag"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.Tag == "" {
		return nil, fmt.Errorf("tag is required")
	}

	// For now, tag-based operations are not yet wired to memory_tags table.
	// This is a placeholder that will be connected when tags are stored during extraction.
	return map[string]interface{}{
		"tag":     p.Tag,
		"dry_run": p.DryRun,
		"count":   0,
		"note":    "tag-based deletion requires memory_tags integration (Phase F)",
	}, nil
}

// ── helpers ──

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
