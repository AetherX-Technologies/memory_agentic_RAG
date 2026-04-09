// Package dedup implements memory deduplication and conflict resolution.
// It prevents duplicate memories from being stored and resolves contradictions
// between new and existing memories using semantic similarity and LLM judgment.
package dedup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yourusername/hybridmem-rag/internal/extractor"
	"github.com/yourusername/hybridmem-rag/internal/store"
)

// Config holds configuration for the dedup module.
type Config struct {
	// Semantic dedup thresholds
	DupThreshold      float64 // similarity > this → duplicate (default: 0.93)
	ConflictThreshold float64 // similarity > this → candidate conflict (default: 0.85)
	ShortTextTokens   int     // below this, lower thresholds by 0.05 (default: 20)

	// LLM conflict detection
	LLMAPIKey  string
	LLMModel   string
	LLMEndpoint string
	LLMTimeout int // seconds

	// Search
	CandidateLimit int // how many existing memories to check (default: 20)

	// Connection building
	ConnectionMinSim float64 // minimum similarity for connection (default: 0.7)
	ConnectionMaxSim float64 // maximum similarity for connection (below ConflictThreshold, default: 0.85)
	MaxConnections   int     // max connections per store operation (default: 3)
}

// DefaultConfig returns sensible defaults.
// LLM settings are read from environment variables if available.
func DefaultConfig() Config {
	cfg := Config{
		DupThreshold:      0.93,
		ConflictThreshold: 0.85,
		ShortTextTokens:   20,
		LLMModel:          "gpt-4o-mini",
		LLMEndpoint:       "https://api.openai.com/v1/chat/completions",
		LLMTimeout:        15,
		CandidateLimit:    20,
		ConnectionMinSim:  0.70,
		ConnectionMaxSim:  0.85,
		MaxConnections:    3,
	}
	// Pick up LLM config from environment (same vars as consolidation)
	if key := os.Getenv("MEMORY_LLM_KEY"); key != "" {
		cfg.LLMAPIKey = key
	}
	if ep := os.Getenv("MEMORY_LLM_ENDPOINT"); ep != "" {
		cfg.LLMEndpoint = ep
	}
	if m := os.Getenv("MEMORY_LLM_MODEL"); m != "" {
		cfg.LLMModel = m
	}
	return cfg
}

// Result describes what happened when storing a memory.
type Result struct {
	Action  string // "stored", "duplicate", "superseded", "conflict_pending"
	ID      string // new memory ID (if stored)
	OldID   string // existing memory ID (if duplicate or superseded)
	Reason  string // human-readable explanation
}

// Deduplicator handles memory dedup, conflict detection, and storage.
type Deduplicator struct {
	store    store.Store
	embedder store.Embedder
	config   Config
	client   *http.Client
	mu       sync.Mutex // serializes check-then-insert
}

// New creates a new Deduplicator. Embedder must not be nil.
func New(s store.Store, embedder store.Embedder, config Config) *Deduplicator {
	if embedder == nil {
		panic("dedup: embedder must not be nil")
	}
	if config.DupThreshold <= 0 {
		config.DupThreshold = 0.93
	}
	if config.ConflictThreshold <= 0 {
		config.ConflictThreshold = 0.85
	}
	if config.ShortTextTokens <= 0 {
		config.ShortTextTokens = 20
	}
	if config.CandidateLimit <= 0 {
		config.CandidateLimit = 20
	}
	if config.LLMModel == "" {
		config.LLMModel = "gpt-4o-mini"
	}
	if config.LLMEndpoint == "" {
		config.LLMEndpoint = "https://api.openai.com/v1/chat/completions"
	}
	if config.LLMTimeout <= 0 {
		config.LLMTimeout = 15
	}

	return &Deduplicator{
		store:    s,
		embedder: embedder,
		config:   config,
		client:   &http.Client{Timeout: time.Duration(config.LLMTimeout) * time.Second},
	}
}

// StoreWithDedup checks for duplicates and conflicts before storing a memory.
// Thread-safe: serializes the check-then-insert sequence.
func (d *Deduplicator) StoreWithDedup(ctx context.Context, mem extractor.ExtractedMemory) (Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Step 1: Embed the new memory
	vec, err := d.embedder.Embed(mem.Content)
	if err != nil {
		return Result{}, fmt.Errorf("failed to embed memory: %w", err)
	}

	// Step 2: Search for similar existing memories (only in "memory" category)
	candidates, err := d.store.VectorSearch(vec, d.config.CandidateLimit, nil)
	// Filter to memory category only (exclude document chunks)
	var memCandidates []store.SearchResult
	for _, c := range candidates {
		if c.Entry.Category == "memory" {
			memCandidates = append(memCandidates, c)
		}
	}
	candidates = memCandidates
	if err != nil {
		return Result{}, fmt.Errorf("vector search failed: %w", err)
	}

	// Check for exact content_hash collision across ALL types first
	for _, c := range candidates {
		if c.Entry.ContentHash != "" && c.Entry.ContentHash == mem.ContentHash {
			// Exact duplicate (any type) — boost confidence, skip new
			if err := d.store.UpdateConfidence(c.Entry.ID, 0.1); err != nil {
				return Result{}, fmt.Errorf("failed to update confidence: %w", err)
			}
			return Result{
				Action: "duplicate",
				OldID:  c.Entry.ID,
				Reason: "exact content_hash match",
			}, nil
		}
	}

	// Filter to same memory_type for semantic dedup
	var sametype []store.SearchResult
	for _, c := range candidates {
		if c.Entry.MemoryType == mem.MemoryType {
			sametype = append(sametype, c)
		}
	}

	// Adaptive threshold for short text
	dupThresh := d.config.DupThreshold
	conflictThresh := d.config.ConflictThreshold
	if estimateTokens(mem.Content) < d.config.ShortTextTokens {
		dupThresh -= 0.05
		conflictThresh -= 0.05
	}

	// Step 3: Check for duplicates and conflicts
	for _, c := range sametype {
		sim := float64(c.Score)

		if sim > dupThresh {
			// Semantic duplicate — boost existing confidence, skip new
			if err := d.store.UpdateConfidence(c.Entry.ID, 0.1); err != nil {
				return Result{}, fmt.Errorf("failed to update confidence: %w", err)
			}
			return Result{
				Action: "duplicate",
				OldID:  c.Entry.ID,
				Reason: fmt.Sprintf("semantic duplicate (sim=%.3f) of existing memory", sim),
			}, nil
		}

		if sim > conflictThresh {
			// Candidate conflict — check with LLM
			contradicts := d.detectConflict(ctx, mem.Content, c.Entry.Text)

			if contradicts {
				// Store new, supersede old
				id, err := d.insertMemory(mem, vec)
				if err != nil {
					return Result{}, err
				}
				if err := d.store.RecordSupersession(c.Entry.ID, id); err != nil {
					// New memory already inserted — return stored with warning
					return Result{
						Action: "stored",
						ID:     id,
						Reason: fmt.Sprintf("stored but supersession recording failed: %v", err),
					}, nil
				}
				// Build connections for the new memory, excluding the superseded one
				d.buildConnectionsFiltered(id, candidates, conflictThresh, map[string]bool{c.Entry.ID: true})
				return Result{
					Action: "superseded",
					ID:     id,
					OldID:  c.Entry.ID,
					Reason: fmt.Sprintf("contradicts existing memory (sim=%.3f)", sim),
				}, nil
			}
			// Not contradictory — fall through and store normally
		}
	}

	// Step 4: No duplicate or conflict — store normally
	id, err := d.insertMemory(mem, vec)
	if err != nil {
		// Check if it's a content_hash UNIQUE constraint violation
		if strings.Contains(err.Error(), "UNIQUE constraint") && strings.Contains(err.Error(), "content_hash") {
			// Try to find the existing memory ID for tag persistence
			var existingID string
			for _, c := range candidates {
				if c.Entry.ContentHash == mem.ContentHash {
					existingID = c.Entry.ID
					break
				}
			}
			return Result{
				Action: "duplicate",
				OldID:  existingID,
				Reason: "content_hash UNIQUE constraint (exact duplicate not in vector index)",
			}, nil
		}
		return Result{}, err
	}

	// Step 5: Build connections to related memories
	d.buildConnectionsFiltered(id, candidates, conflictThresh, nil)

	return Result{
		Action: "stored",
		ID:     id,
		Reason: "new memory",
	}, nil
}

// buildConnectionsFiltered creates bidirectional links to semantically related memories.
// Uses the candidates already fetched during dedup search (no extra DB query).
// maxSim is set to the effective conflict threshold to avoid linking conflict-zone candidates.
// excludeIDs are skipped (e.g. the just-superseded memory).
func (d *Deduplicator) buildConnectionsFiltered(newID string, candidates []store.SearchResult, effectiveConflictThresh float64, excludeIDs map[string]bool) {
	minSim := d.config.ConnectionMinSim
	maxSim := effectiveConflictThresh // use adaptive threshold, not static config
	maxConns := d.config.MaxConnections
	if maxConns <= 0 {
		maxConns = 3
	}

	linked := 0
	for _, c := range candidates {
		if linked >= maxConns {
			break
		}
		if excludeIDs != nil && excludeIDs[c.Entry.ID] {
			continue
		}
		sim := float64(c.Score)
		if sim < minSim || sim >= maxSim {
			continue
		}
		rel := fmt.Sprintf("related (sim=%.2f)", sim)
		if err := d.store.AddConnection(newID, c.Entry.ID, rel); err != nil {
			fmt.Fprintf(os.Stderr, "[dedup] connection %s→%s failed: %v\n", newID[:8], c.Entry.ID[:8], err)
		}
		if err := d.store.AddConnection(c.Entry.ID, newID, rel); err != nil {
			fmt.Fprintf(os.Stderr, "[dedup] connection %s→%s failed: %v\n", c.Entry.ID[:8], newID[:8], err)
		}
		linked++
	}
}

// insertMemory converts ExtractedMemory to store.Memory and inserts it.
func (d *Deduplicator) insertMemory(mem extractor.ExtractedMemory, vec []float32) (string, error) {
	m := &store.Memory{
		Text:        mem.Content,
		Category:    "memory",
		Scope:       "global",
		Importance:  mem.Importance,
		MemoryType:  mem.MemoryType,
		Confidence:  mem.Confidence,
		ContentHash: mem.ContentHash,
		SourceConv:  mem.SourceConv,
		Vector:      vec,
	}
	return d.store.Insert(m)
}

// detectConflict uses LLM to determine if two memories contradict each other.
// Falls back to keyword overlap when LLM is unavailable.
func (d *Deduplicator) detectConflict(ctx context.Context, newText, oldText string) bool {
	if d.config.LLMAPIKey == "" {
		return fallbackConflict(newText, oldText)
	}

	result, err := d.callConflictLLM(ctx, newText, oldText)
	if err != nil {
		return fallbackConflict(newText, oldText)
	}
	return result
}

// callConflictLLM asks the LLM if two memories contradict.
func (d *Deduplicator) callConflictLLM(ctx context.Context, newText, oldText string) (bool, error) {
	prompt := fmt.Sprintf(conflictPrompt, oldText, newText)

	reqBody := map[string]interface{}{
		"model": d.config.LLMModel,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  50,
		"temperature": 0.0,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.config.LLMEndpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+d.config.LLMAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("LLM API error: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return false, err
	}
	if len(apiResp.Choices) == 0 {
		return false, fmt.Errorf("no choices returned")
	}

	answer := strings.ToLower(strings.TrimSpace(apiResp.Choices[0].Message.Content))
	return strings.HasPrefix(answer, "yes"), nil
}

// fallbackConflict uses keyword overlap as a heuristic for potential conflict.
// Returns false (never auto-supersedes without LLM) — treats high-overlap as "maybe conflict"
// but stores the new memory normally to avoid incorrect supersession.
func fallbackConflict(newText, oldText string) bool {
	// Without LLM judgment, we cannot reliably determine contradiction.
	// Store both and let future LLM pass resolve it.
	return false
}

// keywordOverlap calculates the keyword overlap ratio between two texts.
// Exported for testing/diagnostics. Not used for automatic supersession.
func keywordOverlap(newText, oldText string) float64 {
	newWords := extractKeywords(newText)
	oldWords := extractKeywords(oldText)

	if len(newWords) == 0 || len(oldWords) == 0 {
		return 0
	}

	overlap := 0
	for w := range newWords {
		if oldWords[w] {
			overlap++
		}
	}

	smaller := len(newWords)
	if len(oldWords) < smaller {
		smaller = len(oldWords)
	}
	if smaller == 0 {
		return 0
	}

	return float64(overlap) / float64(smaller)
}

// extractKeywords returns a set of meaningful words (>1 rune, no stopwords).
func extractKeywords(text string) map[string]bool {
	words := make(map[string]bool)
	for _, field := range strings.Fields(text) {
		// Simple: split by whitespace and punctuation
		cleaned := strings.Trim(field, `，。！？、；：""''（）【】,.!?;:\"'()[]{}` )
		if utf8.RuneCountInString(cleaned) > 1 {
			words[cleaned] = true
		}
	}
	// For Chinese text without spaces, extract 2-gram characters
	runes := []rune(text)
	for i := 0; i < len(runes)-1; i++ {
		bigram := string(runes[i : i+2])
		if !strings.ContainsAny(bigram, " \t\n，。！？、；：") {
			words[bigram] = true
		}
	}
	return words
}

// estimateTokens gives a rough token count (Chinese: ~1.5 chars/token, English: ~4 chars/token).
func estimateTokens(s string) int {
	runes := utf8.RuneCountInString(s)
	// Rough heuristic: Chinese-heavy text
	return runes * 2 / 3
}

const conflictPrompt = `判断以下两条记忆是否矛盾。只回答 yes 或 no，然后用一句话解释。

记忆A（旧）：%s
记忆B（新）：%s

是否矛盾？`
