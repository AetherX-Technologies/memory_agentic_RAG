package memservice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/dedup"
	"github.com/yourusername/hybridmem-rag/internal/extractor"
	"github.com/yourusername/hybridmem-rag/internal/store"
	"github.com/yourusername/hybridmem-rag/internal/tokutil"
	"github.com/yourusername/hybridmem-rag/internal/trigger"
)

type ErrorCode string

const (
	ErrorCodeInvalidParams ErrorCode = "invalid_params"
	ErrorCodeNotFound      ErrorCode = "not_found"
	ErrorCodeConflict      ErrorCode = "conflict"
	ErrorCodeUnavailable   ErrorCode = "unavailable"
	ErrorCodeInternal      ErrorCode = "internal"
)

type ToolError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *ToolError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *ToolError) Unwrap() error {
	return e.Err
}

type Service struct {
	store        store.Store
	embedder     store.Embedder
	consolidator *consolidate.Consolidator
	dedup        *dedup.Deduplicator
}

type StoreRequest struct {
	Content    string   `json:"content"`
	Type       string   `json:"type"`
	Importance float64  `json:"importance"`
	Tags       []string `json:"tags"`
}

type StoreResponse struct {
	ID     string `json:"id,omitempty"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type RecallRequest struct {
	Query         string   `json:"query"`
	Limit         int      `json:"limit"`
	Types         []string `json:"types"`
	MinImportance float64  `json:"min_importance"`
	MaxTokens     int      `json:"max_tokens"`
	SourceConv    string   `json:"source_conv,omitempty"` // filter by conversation ID
}

type RecallOptions struct {
	AllowMutation      bool
	EnableAutoCapture  bool
	EnableAdaptiveSkip bool
	NormalizeFTSQuery  bool
	Scopes             []string
	CurrentPath        string
}

type RecallResponse struct {
	Count          int                  `json:"count"`
	Context        string               `json:"context"`
	Memories       []store.SearchResult `json:"memories,omitempty"`
	Consolidations int                  `json:"consolidations,omitempty"`
	Skipped        bool                 `json:"skipped,omitempty"`
	Reason         string               `json:"reason,omitempty"`
}

type ForgetRequest struct {
	ID string `json:"id"`
}

type ForgetResponse struct {
	Status string `json:"status"`
	ID     string `json:"id"`
}

type UpdateRequest struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	Importance *float64  `json:"importance"`
	Tags       *[]string `json:"tags"` // nil = keep existing, empty = clear, non-empty = replace
}

type UpdateResponse struct {
	OldID  string `json:"old_id,omitempty"`
	NewID  string `json:"new_id,omitempty"`
	ID     string `json:"id,omitempty"`
	Status string `json:"status"`
}

type ExportRequest struct {
	Types          []string `json:"types"`
	IncludeExpired bool     `json:"include_expired"`
}

type ExportedMemory struct {
	ID         string   `json:"id"`
	Content    string   `json:"content"`
	Abstract   string   `json:"abstract,omitempty"` // Scheme B: preserve generated abstract across export/import
	Type       string   `json:"type"`
	Importance float64  `json:"importance"`
	Confidence float64  `json:"confidence"`
	Timestamp  int64    `json:"timestamp"`
	SourceConv string   `json:"source_conv,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}

type ExportResponse struct {
	Count    int              `json:"count"`
	Memories []ExportedMemory `json:"memories"`
}

type ImportRequest struct {
	Memories []ImportMemory `json:"memories"`
}

type ImportMemory struct {
	Content    string   `json:"content"`
	Abstract   string   `json:"abstract,omitempty"` // Scheme B: restore pre-existing abstract
	Type       string   `json:"type"`
	Importance float64  `json:"importance"`
	Confidence float64  `json:"confidence"`
	Timestamp  int64    `json:"timestamp"`
	SourceConv string   `json:"source_conv"`
	Tags       []string `json:"tags,omitempty"`
}

type ImportResponse struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

type ForgetByTagRequest struct {
	Tag    string `json:"tag"`
	DryRun *bool  `json:"dry_run"`
}

type ForgetByTagResponse struct {
	Tag     string `json:"tag"`
	DryRun  bool   `json:"dry_run,omitempty"`
	Count   int    `json:"count"`
	Deleted int    `json:"deleted,omitempty"`
	Failed  int    `json:"failed,omitempty"`
}

type ConsolidateResponse struct {
	Status         string `json:"status"`
	Unconsolidated int64  `json:"unconsolidated,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Insight        string `json:"insight,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Consolidated   int64  `json:"consolidated,omitempty"`
}

type ShouldCaptureRequest struct {
	Text string `json:"text"`
}

type ShouldCaptureResponse struct {
	ShouldCapture bool    `json:"should_capture"`
	Reason        string  `json:"reason,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

func New(s store.Store, embedder store.Embedder, cons *consolidate.Consolidator) *Service {
	return &Service{
		store:        s,
		embedder:     embedder,
		consolidator: cons,
	}
}

// SetDedup wires in the deduplicator for store-time dedup and connection building.
// Called after construction because dedup depends on store + embedder which Service also holds.
func (s *Service) SetDedup(d *dedup.Deduplicator) {
	s.dedup = d
}

func DefaultToolRecallOptions() RecallOptions {
	return RecallOptions{
		AllowMutation:      true,
		EnableAutoCapture:  true,
		EnableAdaptiveSkip: true,
	}
}

func DefaultLegacyRecallOptions() RecallOptions {
	return RecallOptions{
		AllowMutation:      false,
		EnableAutoCapture:  false,
		EnableAdaptiveSkip: false,
		NormalizeFTSQuery:  false,
	}
}

func (s *Service) CallTool(ctx context.Context, name string, args json.RawMessage) (interface{}, error) {
	switch name {
	case "memory_store":
		var req StoreRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.Store(ctx, req)
	case "memory_recall":
		var req RecallRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.Recall(ctx, req, DefaultToolRecallOptions())
	case "memory_forget":
		var req ForgetRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.Forget(ctx, req)
	case "memory_update":
		var req UpdateRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.Update(ctx, req)
	case "memory_export":
		var req ExportRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.Export(ctx, req)
	case "memory_import":
		var req ImportRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.Import(ctx, req)
	case "memory_forget_by_tag":
		var req ForgetByTagRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.ForgetByTag(ctx, req)
	case "memory_consolidate":
		return s.Consolidate(ctx)
	case "memory_should_capture":
		var req ShouldCaptureRequest
		if err := decodeArgs(args, &req); err != nil {
			return nil, invalidParams("invalid params", err)
		}
		return s.ShouldCapture(ctx, req)
	default:
		return nil, &ToolError{Code: ErrorCodeNotFound, Message: fmt.Sprintf("unknown tool: %s", name)}
	}
}

func (s *Service) Store(_ context.Context, req StoreRequest) (*StoreResponse, error) {
	if req.Content == "" {
		return nil, invalidParams("content is required", nil)
	}

	if isNoise, reason := trigger.IsNoise(req.Content); isNoise {
		return &StoreResponse{
			Action: "filtered",
			Reason: "noise: " + reason,
		}, nil
	}

	if req.Type == "" {
		req.Type = "fact"
	}
	if req.Importance <= 0 {
		req.Importance = 0.7
	}
	if req.Importance > 1 {
		req.Importance = 1
	}

	h := sha256.Sum256([]byte(req.Content))
	contentHash := hex.EncodeToString(h[:8])

	// Route through dedup pipeline when available (enables dedup + connection building)
	if s.dedup != nil {
		result, err := s.dedup.StoreWithDedup(context.Background(), extractor.ExtractedMemory{
			Content:     req.Content,
			MemoryType:  req.Type,
			Importance:  req.Importance,
			Confidence:  0.9,
			ContentHash: contentHash,
		})
		if err != nil {
			return nil, classifyStoreError("store memory", err)
		}
		// Persist tags: on new memory use ID, on duplicate use OldID
		targetID := result.ID
		if targetID == "" {
			targetID = result.OldID
		}
		if targetID != "" && len(req.Tags) > 0 {
			_ = s.store.SetTags(targetID, req.Tags)
		}
		return &StoreResponse{
			ID:     result.ID,
			Action: result.Action,
			Reason: result.Reason,
		}, nil
	}

	// Fallback: direct insert (no dedup, no connections)
	var vec []float32
	if s.embedder != nil {
		if v, err := s.embedder.Embed(req.Content); err == nil {
			vec = v
		}
	}

	mem := &store.Memory{
		Text:        req.Content,
		Category:    "memory",
		Scope:       "global",
		Importance:  req.Importance,
		MemoryType:  req.Type,
		Confidence:  0.9,
		ContentHash: contentHash,
		Vector:      vec,
	}

	id, err := s.store.Insert(mem)
	if err != nil {
		return nil, classifyStoreError("store memory", err)
	}

	if len(req.Tags) > 0 {
		_ = s.store.SetTags(id, req.Tags)
	}

	return &StoreResponse{
		ID:     id,
		Action: "stored",
	}, nil
}

func (s *Service) Recall(_ context.Context, req RecallRequest, opts RecallOptions) (*RecallResponse, error) {
	if req.Query == "" {
		return nil, invalidParams("query is required", nil)
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}
	if req.Limit > 100 {
		req.Limit = 100
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1000
	}
	if req.MaxTokens > 8000 {
		req.MaxTokens = 8000
	}

	if opts.AllowMutation && opts.EnableAutoCapture {
		if shouldCapture, reason := trigger.ShouldCapture(req.Query); shouldCapture && reason == trigger.ReasonExplicit {
			if isNoise, _ := trigger.IsNoise(req.Query); !isNoise && !trigger.IsRetrievalQuestion(req.Query) {
				s.autoStore(req.Query, reason)
			}
		}
	}

	if opts.EnableAdaptiveSkip && !trigger.ShouldRetrieve(req.Query) {
		return &RecallResponse{
			Count:   0,
			Context: "",
			Skipped: true,
			Reason:  "query does not require memory retrieval",
		}, nil
	}

	var queryVec []float32
	if s.embedder != nil {
		if v, err := s.embedder.Embed(req.Query); err == nil {
			queryVec = v
		}
	}

	queryText := req.Query
	if opts.NormalizeFTSQuery {
		queryText = store.EscapeFTS5Query(queryText)
	}

	searchLimit := req.Limit * 3
	if req.SourceConv != "" {
		searchLimit = req.Limit * 10 // widen pool when filtering by conversation
	}
	if searchLimit > 100 {
		searchLimit = 100 // respect hierarchical search limit
	}
	results, err := s.store.Search(queryVec, queryText, opts.CurrentPath, searchLimit, opts.Scopes)
	if err != nil {
		return nil, &ToolError{Code: ErrorCodeInternal, Message: "search failed", Err: err}
	}

	filtered := make([]store.SearchResult, 0, len(results))
	for _, r := range results {
		if req.SourceConv != "" && r.Entry.SourceConv != req.SourceConv {
			continue
		}
		if req.MinImportance > 0 && r.Entry.Importance < req.MinImportance {
			continue
		}
		if len(req.Types) > 0 && !contains(req.Types, r.Entry.MemoryType) {
			continue
		}
		if isNoise, _ := trigger.IsNoise(r.Entry.Text); isNoise {
			continue
		}
		filtered = append(filtered, r)
	}

	if len(filtered) > 1 {
		filtered = store.MMRRerank(filtered, 0.7, 0.85)
	}
	if len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}

	// Step: expand connections from top hits
	connMemories := s.expandConnections(filtered, req.Limit, req.SourceConv, req.Types, req.MinImportance)

	// First check if we have insights to show, then allocate budget accordingly
	consolidations, err := s.store.ListConsolidations(5)
	if err != nil {
		consolidations = nil
	}

	// Build insight lines first to know actual usage
	var insightLines []string
	var insightHeader string
	insightUsed := 0
	if len(consolidations) > 0 {
		insightHeader = "\n🧠 洞察\n"
		insightBudget := req.MaxTokens / 5
		if insightBudget > 0 {
			headerTokens := tokutil.EstimateTokens(insightHeader)
			insightUsed = headerTokens
			for _, c := range consolidations {
				if c.Insight == "" {
					continue
				}
				line := "- " + c.Insight + "\n"
				lineTokens := tokutil.EstimateTokens(line)
				if insightUsed+lineTokens > insightBudget {
					continue
				}
				insightLines = append(insightLines, line)
				insightUsed += lineTokens
			}
			if len(insightLines) == 0 {
				insightUsed = 0 // no insights fit, reclaim budget for memories
			}
		}
	}

	// Budget allocation: connections get 15% if present, insights get their share, rest to memories
	connBudget := 0
	if len(connMemories) > 0 {
		connBudget = req.MaxTokens / 7 // ~15%
	}
	memoryBudget := req.MaxTokens - insightUsed - connBudget
	formatted := formatContext(filtered, memoryBudget)

	// Append connected memories section
	if len(connMemories) > 0 && connBudget > 0 {
		connHeader := "\n🔗 关联记忆\n"
		headerTokens := tokutil.EstimateTokens(connHeader)
		usedTokens := headerTokens
		var connLines []string
		for _, cm := range connMemories {
			// Scheme B: prefer Abstract for connected memory display
			cmText := cm.Text
			if cm.Abstract != "" {
				cmText = cm.Abstract
			}
			line := "- " + strings.ReplaceAll(cmText, "\n", " ") + "\n"
			lineTokens := tokutil.EstimateTokens(line)
			if usedTokens+lineTokens > connBudget {
				continue
			}
			connLines = append(connLines, line)
			usedTokens += lineTokens
		}
		if len(connLines) > 0 {
			formatted += connHeader
			for _, line := range connLines {
				formatted += line
			}
		}
	}

	// Append insights if any
	if len(insightLines) > 0 {
		formatted += insightHeader
		for _, line := range insightLines {
			formatted += line
		}
	}

	return &RecallResponse{
		Count:          len(filtered),
		Context:        formatted,
		Memories:       filtered,
		Consolidations: len(consolidations),
	}, nil
}

func (s *Service) Forget(_ context.Context, req ForgetRequest) (*ForgetResponse, error) {
	if req.ID == "" {
		return nil, invalidParams("id is required", nil)
	}
	if err := s.store.SoftDelete(req.ID, time.Now().Unix()); err != nil {
		return nil, classifyStoreError("delete memory", err)
	}
	return &ForgetResponse{
		Status: "deleted",
		ID:     req.ID,
	}, nil
}

func (s *Service) Update(_ context.Context, req UpdateRequest) (*UpdateResponse, error) {
	if req.ID == "" {
		return nil, invalidParams("id is required", nil)
	}

	existing, err := s.store.Get(req.ID)
	if err != nil {
		return nil, classifyStoreError("get memory for update", err)
	}

	contentChanged := req.Content != "" && req.Content != existing.Text
	if !contentChanged {
		if req.Importance != nil {
			if err := s.store.UpdateImportance(req.ID, *req.Importance); err != nil {
				return nil, &ToolError{Code: ErrorCodeInternal, Message: "failed to update importance", Err: err}
			}
		}
		if req.Tags != nil {
			_ = s.store.SetTags(req.ID, *req.Tags)
		}
		return &UpdateResponse{
			ID:     req.ID,
			Status: "updated",
		}, nil
	}

	// Carry over existing tags if caller doesn't provide new ones
	var tagsToSet []string
	if req.Tags != nil {
		tagsToSet = *req.Tags // explicit: use provided (including empty = clear)
	} else {
		if existingTags, err := s.store.GetTags(req.ID); err == nil {
			tagsToSet = existingTags
		}
	}

	now := time.Now().Unix()
	if err := s.store.SoftDelete(req.ID, now); err != nil {
		return nil, &ToolError{Code: ErrorCodeInternal, Message: "failed to soft-delete old memory", Err: err}
	}

	newImportance := existing.Importance
	if req.Importance != nil {
		newImportance = *req.Importance
	}

	h := sha256.Sum256([]byte(req.Content))
	contentHash := hex.EncodeToString(h[:8])

	// Route through dedup to get connections built for the new version
	if s.dedup != nil {
		result, err := s.dedup.StoreWithDedup(context.Background(), extractor.ExtractedMemory{
			Content:     req.Content,
			MemoryType:  existing.MemoryType,
			Importance:  newImportance,
			Confidence:  existing.Confidence,
			ContentHash: contentHash,
			SourceConv:  existing.SourceConv,
			Scope:       existing.Scope,
		})
		if err != nil {
			_ = s.store.Restore(req.ID)
			return nil, classifyStoreError("insert updated memory", err)
		}
		if result.Action == "duplicate" {
			// New content is a duplicate of something else — restore old and report
			_ = s.store.Restore(req.ID)
			return &UpdateResponse{
				ID:     req.ID,
				Status: "duplicate",
			}, nil
		}
		if result.ID != "" {
			_ = s.store.RecordSupersession(req.ID, result.ID)
			if req.Tags != nil || len(tagsToSet) > 0 {
				_ = s.store.SetTags(result.ID, tagsToSet)
			}
		}
		return &UpdateResponse{
			OldID:  req.ID,
			NewID:  result.ID,
			Status: "updated",
		}, nil
	}

	// Fallback: direct insert without dedup
	var vec []float32
	if s.embedder != nil {
		if v, err := s.embedder.Embed(req.Content); err == nil {
			vec = v
		}
	}

	newMem := &store.Memory{
		Text:        req.Content,
		Category:    "memory",
		Scope:       existing.Scope,
		Importance:  newImportance,
		MemoryType:  existing.MemoryType,
		Confidence:  existing.Confidence,
		ContentHash: contentHash,
		SourceConv:  existing.SourceConv,
		Vector:      vec,
	}

	newID, err := s.store.Insert(newMem)
	if err != nil {
		_ = s.store.Restore(req.ID)
		return nil, classifyStoreError("insert updated memory", err)
	}

	if err := s.store.RecordSupersession(req.ID, newID); err != nil {
		// Non-fatal: old memory is soft-deleted and new one stored; just log the link failure
		fmt.Fprintf(os.Stderr, "[memory] supersession link %s→%s failed: %v\n", req.ID[:8], newID[:8], err)
	}

	if req.Tags != nil || len(tagsToSet) > 0 {
		_ = s.store.SetTags(newID, tagsToSet)
	}

	return &UpdateResponse{
		OldID:  req.ID,
		NewID:  newID,
		Status: "updated",
	}, nil
}

func (s *Service) Export(_ context.Context, req ExportRequest) (*ExportResponse, error) {
	memories, err := s.store.List("global", 1000000)
	if err != nil {
		return nil, &ToolError{Code: ErrorCodeInternal, Message: "list memories", Err: err}
	}

	exported := make([]ExportedMemory, 0, len(memories))
	for _, m := range memories {
		if !req.IncludeExpired && m.Expired {
			continue
		}
		if len(req.Types) > 0 && !contains(req.Types, m.MemoryType) {
			continue
		}
		tags, _ := s.store.GetTags(m.ID)
		exported = append(exported, ExportedMemory{
			ID:         m.ID,
			Content:    m.Text,
			Abstract:   m.Abstract,
			Type:       m.MemoryType,
			Importance: m.Importance,
			Confidence: m.Confidence,
			Timestamp:  m.Timestamp,
			SourceConv: m.SourceConv,
			Tags:       tags,
		})
	}

	return &ExportResponse{
		Count:    len(exported),
		Memories: exported,
	}, nil
}

func (s *Service) Import(_ context.Context, req ImportRequest) (*ImportResponse, error) {
	imported := 0
	skipped := 0

	for _, m := range req.Memories {
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
			Abstract:    m.Abstract, // Scheme B: restore preserved abstract
			Category:    "memory",
			Scope:       "global",
			Importance:  m.Importance,
			MemoryType:  m.Type,
			Confidence:  m.Confidence,
			Timestamp:   m.Timestamp,
			SourceConv:  m.SourceConv,
			ContentHash: hex.EncodeToString(h[:8]),
		}

		// Embed using Abstract if present (matches dedup's long-text behavior)
		if s.embedder != nil {
			embedTarget := m.Content
			if m.Abstract != "" {
				embedTarget = m.Abstract
			}
			if v, err := s.embedder.Embed(embedTarget); err == nil {
				mem.Vector = v
			}
		}

		id, err := s.store.Insert(mem)
		if err != nil {
			skipped++
			continue
		}
		if len(m.Tags) > 0 {
			_ = s.store.SetTags(id, m.Tags)
		}
		imported++
	}

	return &ImportResponse{
		Imported: imported,
		Skipped:  skipped,
	}, nil
}

func (s *Service) ForgetByTag(_ context.Context, req ForgetByTagRequest) (*ForgetByTagResponse, error) {
	if req.Tag == "" {
		return nil, invalidParams("tag is required", nil)
	}

	isDryRun := true
	if req.DryRun != nil {
		isDryRun = *req.DryRun
	}

	ids, err := s.store.GetMemoryIDsByTag(req.Tag)
	if err != nil {
		return nil, &ToolError{Code: ErrorCodeInternal, Message: "failed to query tag", Err: err}
	}

	if isDryRun {
		return &ForgetByTagResponse{
			Tag:    req.Tag,
			DryRun: true,
			Count:  len(ids),
		}, nil
	}

	deleted := 0
	failed := 0
	now := time.Now().Unix()
	for _, id := range ids {
		if err := s.store.SoftDelete(id, now); err != nil {
			failed++
			continue
		}
		deleted++
	}

	return &ForgetByTagResponse{
		Tag:     req.Tag,
		Deleted: deleted,
		Failed:  failed,
	}, nil
}

func (s *Service) Consolidate(ctx context.Context) (*ConsolidateResponse, error) {
	count, err := s.store.CountUnconsolidated()
	if err != nil {
		return nil, &ToolError{Code: ErrorCodeInternal, Message: "count unconsolidated", Err: err}
	}
	if count < 2 {
		return &ConsolidateResponse{
			Status:         "skipped",
			Unconsolidated: count,
			Reason:         "need at least 2 unconsolidated memories",
		}, nil
	}

	if s.consolidator == nil {
		return &ConsolidateResponse{
			Status:         "unavailable",
			Unconsolidated: count,
			Reason:         "LLM not configured - set LLM API key to enable consolidation",
		}, nil
	}

	// Use LeafPass to get per-group results; aggregate for the response.
	// Under A-Phase1, consolidation produces multiple per-topic records.
	results, err := s.consolidator.LeafPass(ctx)
	if err != nil {
		return nil, &ToolError{Code: ErrorCodeInternal, Message: "consolidation failed", Err: err}
	}
	if len(results) == 0 {
		return &ConsolidateResponse{
			Status: "skipped",
			Reason: "insufficient memories in any connected group",
		}, nil
	}

	// Aggregate insights from all groups; use first group's summary as representative.
	var insights []string
	for _, r := range results {
		if r.Insight != "" {
			insights = append(insights, r.Insight)
		}
	}
	aggregatedInsight := strings.Join(insights, " | ")
	if aggregatedInsight == "" {
		aggregatedInsight = results[0].Insight
	}

	newCount, _ := s.store.CountUnconsolidated()
	return &ConsolidateResponse{
		Status:         "completed",
		Insight:        aggregatedInsight,
		Summary:        results[0].Summary,
		Consolidated:   count - newCount,
		Unconsolidated: newCount,
	}, nil
}

func (s *Service) ShouldCapture(_ context.Context, req ShouldCaptureRequest) (*ShouldCaptureResponse, error) {
	if req.Text == "" {
		return nil, invalidParams("text is required", nil)
	}

	shouldCapture, reason := trigger.ShouldCapture(req.Text)
	resp := &ShouldCaptureResponse{
		ShouldCapture: shouldCapture,
	}
	if shouldCapture {
		resp.Reason = string(reason)
		resp.Confidence = trigger.ConfidenceForReason(reason)
	}

	if isNoise, noiseType := trigger.IsNoise(req.Text); isNoise {
		resp.ShouldCapture = false
		resp.Reason = "noise: " + noiseType
		resp.Confidence = 0
	}

	return resp, nil
}

func (s *Service) autoStore(text string, reason trigger.CaptureReason) {
	h := sha256.Sum256([]byte(text))
	contentHash := hex.EncodeToString(h[:8])

	// Route through dedup when available
	if s.dedup != nil {
		result, err := s.dedup.StoreWithDedup(context.Background(), extractor.ExtractedMemory{
			Content:     text,
			MemoryType:  "fact",
			Importance:  0.7,
			Confidence:  trigger.ConfidenceForReason(reason),
			ContentHash: contentHash,
		})
		if err == nil {
			fmt.Fprintf(os.Stderr, "[memory] auto-%s: %s (reason=%s, id=%s)\n", result.Action, text[:min(len(text), 30)], reason, result.ID[:min(len(result.ID), 8)])
		}
		return
	}

	// Fallback: direct insert
	var vec []float32
	if s.embedder != nil {
		if v, err := s.embedder.Embed(text); err == nil {
			vec = v
		}
	}

	mem := &store.Memory{
		Text:        text,
		Category:    "memory",
		Scope:       "global",
		Importance:  0.7,
		MemoryType:  "fact",
		Confidence:  trigger.ConfidenceForReason(reason),
		ContentHash: contentHash,
		Vector:      vec,
	}

	if id, err := s.store.Insert(mem); err == nil {
		fmt.Fprintf(os.Stderr, "[memory] auto-stored: %s (reason=%s, id=%s)\n", text[:min(len(text), 30)], reason, id[:8])
	}
}

// expandConnections fetches connected memories for the top search hits.
// Returns unique connected memories that are not already in the result set.
func (s *Service) expandConnections(results []store.SearchResult, maxConnected int, filterSourceConv string, filterTypes []string, filterMinImportance float64) []*store.Memory {
	if len(results) == 0 || maxConnected <= 0 {
		return nil
	}

	// Collect IDs already in results
	seen := make(map[string]bool, len(results))
	for _, r := range results {
		seen[r.Entry.ID] = true
	}

	// Only expand top 3 hits to limit DB queries
	topN := 3
	if topN > len(results) {
		topN = len(results)
	}

	var connected []*store.Memory
	for _, r := range results[:topN] {
		// Get full memory with connections
		mem, err := s.store.Get(r.Entry.ID)
		if err != nil || mem.Connections == "" || mem.Connections == "[]" {
			continue
		}

		var conns []map[string]string
		if err := json.Unmarshal([]byte(mem.Connections), &conns); err != nil {
			continue
		}

		for _, conn := range conns {
			linkedID := conn["linked_to"]
			if linkedID == "" || seen[linkedID] {
				continue
			}
			seen[linkedID] = true

			linked, err := s.store.Get(linkedID)
			if err != nil || linked.DeletedAt > 0 || linked.Expired {
				continue
			}
			// Skip expired-but-not-yet-cleaned memories
			if linked.ExpiresAt > 0 && linked.ExpiresAt < time.Now().Unix() {
				continue
			}
			// Respect source_conv filter if set
			if filterSourceConv != "" && linked.SourceConv != filterSourceConv {
				continue
			}
			// Respect type and importance filters from the original query
			if len(filterTypes) > 0 && !contains(filterTypes, linked.MemoryType) {
				continue
			}
			if filterMinImportance > 0 && linked.Importance < filterMinImportance {
				continue
			}

			connected = append(connected, linked)
			if len(connected) >= maxConnected {
				return connected
			}
		}
	}

	return connected
}

func decodeArgs(args json.RawMessage, target interface{}) error {
	if len(strings.TrimSpace(string(args))) == 0 {
		return nil
	}
	return json.Unmarshal(args, target)
}

func invalidParams(message string, err error) error {
	return &ToolError{Code: ErrorCodeInvalidParams, Message: message, Err: err}
}

func notFound(message string, err error) error {
	return &ToolError{Code: ErrorCodeNotFound, Message: message, Err: err}
}

func classifyStoreError(message string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notFound(message, err)
	}
	if isConflictErr(err) {
		return &ToolError{Code: ErrorCodeConflict, Message: message, Err: err}
	}
	return &ToolError{Code: ErrorCodeInternal, Message: message, Err: err}
}

func isConflictErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "idx_content_hash_unique")
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
