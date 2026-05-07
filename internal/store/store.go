package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
)

func init() {
	sql.Register("sqlite3_with_extensions",
		&sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				var extName string
				switch runtime.GOOS {
				case "darwin":
					extName = "libsimple.dylib"
				case "linux":
					extName = "libsimple.so"
				case "windows":
					extName = "simple.dll"
				default:
					return nil
				}

				// 尝试多个路径：1. 可执行文件目录 2. 当前工作目录
				var extPath string
				execPath, err := filepath.Abs(filepath.Dir(os.Args[0]))
				if err == nil {
					candidate := filepath.Join(execPath, "lib", extName)
					if _, err := os.Stat(candidate); err == nil {
						extPath = candidate
					}
				}

				// 如果可执行文件目录找不到，向上搜索 lib/ 目录（支持 go test 子目录运行）
				if extPath == "" {
					cwd, err := os.Getwd()
					if err == nil {
						dir := cwd
						for i := 0; i < 10; i++ {
							candidate := filepath.Join(dir, "lib", extName)
							if _, err := os.Stat(candidate); err == nil {
								extPath = candidate
								break
							}
							if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
								break
							}
							parent := filepath.Dir(dir)
							if parent == dir {
								break
							}
							dir = parent
						}
					}
				}

				// 扩展不存在时不报错
				if extPath == "" {
					return nil
				}

				// 验证路径安全性
				cleanPath := filepath.Clean(extPath)
				if !filepath.IsAbs(cleanPath) || strings.Contains(cleanPath, "..") {
					return fmt.Errorf("invalid extension path: %s", cleanPath)
				}

				return conn.LoadExtension(cleanPath, "sqlite3_simple_init")
			},
		})
}

// Store 存储接口
type Store interface {
	Insert(memory *Memory) (string, error)
	Get(id string) (*Memory, error)
	Delete(id string) error
	List(scope string, limit int) ([]*Memory, error)
	Search(queryVector []float32, queryText string, currentPath string, limit int, scopes []string) ([]SearchResult, error)
	VectorSearch(query []float32, limit int, scopes []string) ([]SearchResult, error)
	HybridSearch(queryVector []float32, queryText string, limit int, scopes []string) ([]SearchResult, error)
	HierarchicalHybridSearch(queryVector []float32, queryText string, currentPath string, limit int, scopes []string) ([]SearchResult, error)
	GetChildren(parentID string) ([]*Memory, error)
	HasChildren(id string) (bool, error)
	GetContent(id string) (string, error)
	UpdateConfidence(id string, delta float64) error
	UpdateImportance(id string, importance float64) error
	RecordSupersession(oldID, newID string) error
	SoftDelete(id string, now int64) error
	Restore(id string) error
	ListTrash(limit int) ([]*Memory, error)
	PermanentDelete(id string) error
	RunCleanup(now int64) error
	SetTags(memoryID string, tags []string) error
	GetTags(memoryID string) ([]string, error)
	// BatchGetTags returns tags for many memory IDs in a single query.
	// IDs without tags appear in the map with an empty/nil slice.
	BatchGetTags(memoryIDs []string) (map[string][]string, error)
	GetMemoryIDsByTag(tag string) ([]string, error)
	// Phase G: Consolidation
	ListUnconsolidated(limit int) ([]*Memory, error)
	CountUnconsolidated() (int64, error)
	MarkConsolidated(ids []string) error
	CreateConsolidation(c *Consolidation) (string, error)
	ListConsolidations(limit int) ([]*Consolidation, error)
	AddConnection(memoryID, linkedTo, relationship string) error
	Close() error
}

const (
	MaxOpenConnections = 25
)

type sqliteStore struct {
	db       *sql.DB
	config   Config
	reranker Reranker
}

// New 创建新的存储实例
func New(config Config) (Store, error) {
	dbPath := config.DBPath
	if dbPath == ":memory:" {
		dbPath += "?_load_extension=1"
	} else {
		dbPath += "?_pragma=foreign_keys(1)&_load_extension=1"
	}
	db, err := sql.Open("sqlite3_with_extensions", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 配置并发安全
	// 对于 :memory: 数据库，必须使用单连接，否则每个连接会创建独立的数据库
	if config.DBPath == ":memory:" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(MaxOpenConnections)
	}
	if config.DBPath != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to enable WAL: %w", err)
		}
	}
	// codex round 4 low: 显式 PRAGMA 兜底。原 DSN `_pragma=foreign_keys(1)`
	// 是 mattn/go-sqlite3 的合法写法，但只在新建连接时执行，连接池里轮换
	// 出来的连接如果走了不同 ConnectHook 路径不一定都生效。每次 Open 后再
	// 显式 PRAGMA 一次，确保 memory_sweep_judgements / memory_supersessions
	// 等表的 ON DELETE CASCADE 在 hard delete / RunCleanup 时一定触发。
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign_keys: %w", err)
	}

	// 初始化表结构
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	store := &sqliteStore{
		db:     db,
		config: config,
	}

	// 初始化 reranker（如果启用）
	if config.RerankConfig.Enabled {
		store.reranker = NewReranker(config.RerankConfig)
	}

	return store, nil
}

func initSchema(db *sql.DB) error {
	// Base tables (always required)
	for _, schema := range []string{schemaMemories, schemaVectors} {
		if _, err := db.Exec(schema); err != nil {
			return err
		}
	}

	// FTS5: drop and recreate with unicode61 + rebuild index with CJK segmentation.
	// Old tables may use 'simple' tokenizer which requires an external extension.
	db.Exec("DROP TABLE IF EXISTS fts_memories")
	db.Exec("DROP TRIGGER IF EXISTS memories_ai")
	db.Exec("DROP TRIGGER IF EXISTS memories_au")

	if _, err := db.Exec(schemaFTS); err != nil {
		return fmt.Errorf("FTS5 create failed: %w", err)
	}

	// Delete trigger only (insert/update handled in Go for CJK segmentation)
	if _, err := db.Exec(schemaTriggers); err != nil {
		return err
	}

	// 执行层次字段迁移
	if err := migrateHierarchy(db); err != nil {
		return fmt.Errorf("failed to migrate hierarchy: %w", err)
	}

	// 执行 OpenViking L0/L1/L2 字段迁移
	if err := migrateOpenViking(db); err != nil {
		return fmt.Errorf("failed to migrate openviking: %w", err)
	}

	// 执行 AI 记忆系统字段迁移
	if err := migrateMemorySystem(db); err != nil {
		return fmt.Errorf("failed to migrate memory system: %w", err)
	}

	// Rebuild FTS index from active memories with CJK segmentation.
	// Must run after migrations so deleted_at/expired columns exist.
	// Trashed/expired memories excluded to keep BM25 scoring accurate.
	// Restore() re-inserts FTS entries when memories are un-trashed.
	rows, err := db.Query("SELECT id, text FROM memories WHERE deleted_at = 0 AND expired = 0")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, text string
			if err := rows.Scan(&id, &text); err == nil {
				db.Exec("INSERT OR IGNORE INTO fts_memories(memory_id, content) VALUES (?, ?)", id, segmentCJK(text))
			}
		}
	}

	return nil
}

func (s *sqliteStore) Insert(memory *Memory) (string, error) {
	if memory.ID == "" {
		memory.ID = uuid.New().String()
	}
	if memory.Timestamp == 0 {
		memory.Timestamp = time.Now().Unix()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Ensure confidence default and floor
	if memory.Confidence == 0 {
		memory.Confidence = 0.5 // column default
	} else if memory.Confidence < 0.05 {
		memory.Confidence = 0.05
	}

	// 插入记忆
	// Note: content_hash is NOT auto-computed here. Callers (e.g. memory extractor in Phase B)
	// should set ContentHash explicitly when dedup is desired. Document chunks with duplicate
	// text across different files/scopes are legitimate and should not be rejected.
	_, err = tx.Exec(`
		INSERT INTO memories (id, text, abstract, overview, category, scope, importance, timestamp, metadata,
			hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count,
			memory_type, confidence, access_count, last_accessed, expires_at, source_conv, content_hash, expired)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		memory.ID, memory.Text,
		nullIfEmpty(memory.Abstract), nullIfEmpty(memory.Overview),
		memory.Category, memory.Scope,
		memory.Importance, memory.Timestamp, memory.Metadata,
		nullIfEmpty(memory.HierarchyPath), memory.HierarchyLevel,
		nullIfEmpty(memory.ParentID),
		defaultIfEmpty(memory.NodeType, "chunk"),
		nullIfEmpty(memory.SourceFile),
		memory.ChunkIndex, nullIfZero(memory.TokenCount),
		defaultIfEmpty(memory.MemoryType, "fact"),
		memory.Confidence,
		memory.AccessCount, memory.LastAccessed, memory.ExpiresAt,
		nullIfEmpty(memory.SourceConv), nullIfEmpty(memory.ContentHash),
		boolToInt(memory.Expired))
	if err != nil {
		return "", err
	}

	// 插入向量
	if len(memory.Vector) > 0 {
		if s.config.VectorDim > 0 && len(memory.Vector) != s.config.VectorDim {
			return "", fmt.Errorf("vector dimension mismatch: expected %d, got %d", s.config.VectorDim, len(memory.Vector))
		}
		// 复制并归一化向量，避免修改原始数据
		normalized := make([]float32, len(memory.Vector))
		copy(normalized, memory.Vector)
		NormalizeVector(normalized)

		vectorData, err := SerializeVector(normalized)
		if err != nil {
			return "", err
		}
		_, err = tx.Exec(`
			INSERT INTO vectors (memory_id, vector, dimension)
			VALUES (?, ?, ?)`,
			memory.ID, vectorData, len(memory.Vector))
		if err != nil {
			return "", err
		}
	}

	// Insert into FTS with CJK character segmentation
	segmented := segmentCJK(memory.Text)
	_, err = tx.Exec(`INSERT INTO fts_memories(memory_id, content) VALUES (?, ?)`, memory.ID, segmented)
	if err != nil {
		// FTS insert failure is non-fatal (search degrades but store works)
		fmt.Fprintf(os.Stderr, "[store] FTS insert warning: %v\n", err)
	}

	return memory.ID, tx.Commit()
}

// segmentCJK inserts spaces around CJK characters for FTS5 indexing.
func segmentCJK(text string) string {
	var buf strings.Builder
	buf.Grow(len(text) * 2)
	for _, r := range text {
		if isCJKCharStore(r) {
			buf.WriteRune(' ')
			buf.WriteRune(r)
			buf.WriteRune(' ')
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func isCJKCharStore(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified
		(r >= 0x3400 && r <= 0x4DBF) || // Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // Extension B
		(r >= 0xF900 && r <= 0xFAFF) || // Compatibility
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols
		(r >= 0x3040 && r <= 0x309F) || // Hiragana
		(r >= 0x30A0 && r <= 0x30FF) // Katakana
}

func (s *sqliteStore) Get(id string) (*Memory, error) {
	memory := &Memory{}
	var hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string
	var tokenCount *int
	var sourceConv, contentHash *string
	var expired int
	var connections *string
	err := s.db.QueryRow(`
		SELECT id, text, abstract, overview, category, scope, importance, timestamp, metadata,
			hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count,
			memory_type, confidence, access_count, last_accessed, expires_at, source_conv, content_hash, expired,
			deleted_at, connections
		FROM memories WHERE id = ?`, id).Scan(
		&memory.ID, &memory.Text, &abstract, &overview,
		&memory.Category, &memory.Scope,
		&memory.Importance, &memory.Timestamp, &memory.Metadata,
		&hierarchyPath, &memory.HierarchyLevel,
		&parentID, &nodeType, &sourceFile, &memory.ChunkIndex, &tokenCount,
		&memory.MemoryType, &memory.Confidence, &memory.AccessCount, &memory.LastAccessed, &memory.ExpiresAt,
		&sourceConv, &contentHash, &expired,
		&memory.DeletedAt, &connections)
	if err != nil {
		return nil, err
	}
	assignNullableFields(memory, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
	assignMemSysNullable(memory, sourceConv, contentHash, expired)
	if connections != nil {
		memory.Connections = *connections
	}

	// 读取向量
	var vectorData []byte
	err = s.db.QueryRow(`SELECT vector FROM vectors WHERE memory_id = ?`, id).Scan(&vectorData)
	if err == nil {
		memory.Vector, err = DeserializeVector(vectorData)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize vector: %w", err)
		}
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	return memory, nil
}

func (s *sqliteStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

func (s *sqliteStore) List(scope string, limit int) ([]*Memory, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}

	query := `SELECT id, text, abstract, overview, category, scope, importance, timestamp, metadata,
		hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count,
		memory_type, confidence, access_count, last_accessed, expires_at, source_conv, content_hash, expired
		FROM memories WHERE scope = ? AND deleted_at = 0 ORDER BY timestamp DESC LIMIT ?`
	rows, err := s.db.Query(query, scope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := make([]*Memory, 0, limit)
	for rows.Next() {
		m := &Memory{}
		var hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string
		var tokenCount *int
		var sourceConv, contentHash *string
		var expired int
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel,
			&parentID, &nodeType, &sourceFile, &m.ChunkIndex, &tokenCount,
			&m.MemoryType, &m.Confidence, &m.AccessCount, &m.LastAccessed, &m.ExpiresAt,
			&sourceConv, &contentHash, &expired); err != nil {
			return nil, err
		}
		assignNullableFields(m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
		assignMemSysNullable(m, sourceConv, contentHash, expired)
		memories = append(memories, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return memories, nil
}

func (s *sqliteStore) Search(queryVector []float32, queryText string, currentPath string, limit int, scopes []string) ([]SearchResult, error) {
	if currentPath != "" {
		return s.HierarchicalHybridSearch(queryVector, queryText, currentPath, limit, scopes)
	}
	return s.HybridSearch(queryVector, queryText, limit, scopes)
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

// GetChildren returns all memories whose parent_id matches the given ID, including their vectors.
func (s *sqliteStore) GetChildren(parentID string) ([]*Memory, error) {
	if parentID == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT m.id, m.text, m.abstract, m.overview,
			m.category, m.scope, m.importance, m.timestamp, m.metadata,
			m.hierarchy_path, m.hierarchy_level, m.parent_id, m.node_type,
			m.source_file, m.chunk_index, m.token_count,
			m.memory_type, m.confidence, m.access_count, m.last_accessed, m.expires_at,
			m.source_conv, m.content_hash, m.expired,
			v.vector
		FROM memories m
		LEFT JOIN vectors v ON m.id = v.memory_id
		WHERE m.parent_id = ? AND m.expired = 0 AND m.deleted_at = 0 AND (m.expires_at = 0 OR m.expires_at > strftime('%s','now'))
		ORDER BY m.chunk_index`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoriesWithVector(rows)
}

// HasChildren returns true if the given memory ID has any child nodes.
func (s *sqliteStore) HasChildren(id string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE parent_id = ?`, id).Scan(&count)
	return count > 0, err
}

// GetContent returns only the full L2 content for a given memory ID (for lazy loading).
func (s *sqliteStore) GetContent(id string) (string, error) {
	var content string
	err := s.db.QueryRow(`SELECT text FROM memories WHERE id = ?`, id).Scan(&content)
	return content, err
}

// UpdateConfidence adjusts a memory's confidence by delta, clamped to [0.05, 1.0].
func (s *sqliteStore) UpdateConfidence(id string, delta float64) error {
	_, err := s.db.Exec(`
		UPDATE memories SET confidence = MIN(1.0, MAX(0.05, confidence + ?))
		WHERE id = ?`, delta, id)
	return err
}

// UpdateImportance sets the importance of a memory, clamped to [0, 1].
func (s *sqliteStore) UpdateImportance(id string, importance float64) error {
	if importance < 0 {
		importance = 0
	}
	if importance > 1 {
		importance = 1
	}
	_, err := s.db.Exec(`UPDATE memories SET importance = ? WHERE id = ?`, importance, id)
	return err
}

// RecordSupersession records that newID supersedes oldID, and reduces oldID's importance.
func (s *sqliteStore) RecordSupersession(oldID, newID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT OR IGNORE INTO memory_supersessions (old_id, new_id, created_at)
		VALUES (?, ?, ?)`, oldID, newID, time.Now().Unix())
	if err != nil {
		return err
	}

	// Only reduce importance if a new row was actually inserted (idempotent)
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 0 {
		_, err = tx.Exec(`UPDATE memories SET importance = importance * 0.3 WHERE id = ?`, oldID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// SoftDelete moves a memory to the trash bin.
func (s *sqliteStore) SoftDelete(id string, now int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE memories SET deleted_at = ? WHERE id = ? AND deleted_at = 0`, now, id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM fts_memories WHERE memory_id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// Restore recovers a memory from the trash bin.
// If expires_at has already passed, it's also cleared to avoid immediate re-expiration.
func (s *sqliteStore) Restore(id string) error {
	now := time.Now().Unix()
	_, err := s.db.Exec(`UPDATE memories SET deleted_at = 0, expired = 0,
		expires_at = CASE WHEN expires_at > 0 AND expires_at < ? THEN 0 ELSE expires_at END
		WHERE id = ? AND deleted_at > 0`, now, id)
	if err != nil {
		return err
	}
	// Re-insert FTS entry for restored memory (in transaction to avoid partial state)
	var text string
	if err := s.db.QueryRow("SELECT text FROM memories WHERE id = ?", id).Scan(&text); err == nil {
		tx2, err := s.db.Begin()
		if err == nil {
			tx2.Exec("DELETE FROM fts_memories WHERE memory_id = ?", id)
			tx2.Exec("INSERT INTO fts_memories(memory_id, content) VALUES (?, ?)", id, segmentCJK(text))
			if err := tx2.Commit(); err != nil {
				fmt.Fprintf(os.Stderr, "[store] FTS restore warning: %v\n", err)
			}
		}
	}
	return nil
}

// ListTrash returns soft-deleted memories ordered by deletion time (newest first).
func (s *sqliteStore) ListTrash(limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, text, category, scope, importance, timestamp, metadata,
		memory_type, confidence, deleted_at
		FROM memories WHERE deleted_at > 0 ORDER BY deleted_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*Memory
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.Text, &m.Category, &m.Scope, &m.Importance, &m.Timestamp,
			&m.Metadata, &m.MemoryType, &m.Confidence, &m.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// PermanentDelete removes a memory from the database permanently.
func (s *sqliteStore) PermanentDelete(id string) error {
	_, err := s.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

// RunCleanup performs periodic maintenance: expire, soft-delete low-value, purge old trash.
func (s *sqliteStore) RunCleanup(now int64) error {
	// 1. Mark expired memories
	if _, err := s.db.Exec(`UPDATE memories SET expired = 1
		WHERE expires_at > 0 AND expires_at < ? AND expired = 0 AND deleted_at = 0`, now); err != nil {
		return fmt.Errorf("cleanup: mark expired: %w", err)
	}

	// 2. Soft-delete low-value memories (>180 days, low importance+confidence, never accessed)
	cutoff := now - 180*86400
	if _, err := s.db.Exec(`UPDATE memories SET deleted_at = ?
		WHERE deleted_at = 0
		  AND importance < 0.1 AND confidence < 0.1
		  AND access_count = 0 AND timestamp < ?
		  AND id NOT IN (SELECT new_id FROM memory_supersessions)
		  AND id NOT IN (SELECT old_id FROM memory_supersessions)`, now, cutoff); err != nil {
		return fmt.Errorf("cleanup: soft-delete low-value: %w", err)
	}

	// 3. Soft-delete expired memories created >30 days ago
	expiredCutoff := now - 30*86400
	if _, err := s.db.Exec(`UPDATE memories SET deleted_at = ?
		WHERE deleted_at = 0 AND expired = 1 AND timestamp < ?`, now, expiredCutoff); err != nil {
		return fmt.Errorf("cleanup: soft-delete expired: %w", err)
	}

	// 4. Permanently delete trash items >30 days old
	trashCutoff := now - 30*86400
	if _, err := s.db.Exec(`DELETE FROM memories
		WHERE deleted_at > 0 AND deleted_at < ?`, trashCutoff); err != nil {
		return fmt.Errorf("cleanup: purge trash: %w", err)
	}

	return nil
}

// SetTags replaces all tags for a memory.
func (s *sqliteStore) SetTags(memoryID string, tags []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM memory_tags WHERE memory_id = ?`, memoryID); err != nil {
		return err
	}
	for _, tag := range tags {
		if tag != "" {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO memory_tags (memory_id, tag) VALUES (?, ?)`, memoryID, tag); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GetTags returns all tags for a memory.
func (s *sqliteStore) GetTags(memoryID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT tag FROM memory_tags WHERE memory_id = ?`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// batchGetTagsChunkSize keeps each query well under SQLite's default
// SQLITE_LIMIT_VARIABLE_NUMBER (999 on older builds, 32766 on newer).
// 500 leaves comfortable headroom and stays cache-friendly.
const batchGetTagsChunkSize = 500

// BatchGetTags fetches tags for many memory IDs.
// Returns a map keyed by memory_id; IDs without tags are absent from the map
// (caller treats lookup miss as "no tags"). Internally chunks the IN(...) list
// to stay below SQLite's variable limit even for very large input lists
// (codex round 2 low: BatchGetTags variable limit hardening).
func (s *sqliteStore) BatchGetTags(memoryIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(memoryIDs))
	if len(memoryIDs) == 0 {
		return out, nil
	}
	for start := 0; start < len(memoryIDs); start += batchGetTagsChunkSize {
		end := start + batchGetTagsChunkSize
		if end > len(memoryIDs) {
			end = len(memoryIDs)
		}
		chunk := memoryIDs[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]interface{}, len(chunk))
		for i, id := range chunk {
			placeholders[i] = "?"
			args[i] = id
		}
		query := `SELECT memory_id, tag FROM memory_tags WHERE memory_id IN (` +
			strings.Join(placeholders, ",") + `)`
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var mid, tag string
			if err := rows.Scan(&mid, &tag); err != nil {
				rows.Close()
				return nil, err
			}
			out[mid] = append(out[mid], tag)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// GetMemoryIDsByTag returns memory IDs that have the given tag.
func (s *sqliteStore) GetMemoryIDsByTag(tag string) ([]string, error) {
	rows, err := s.db.Query(`SELECT t.memory_id FROM memory_tags t
		JOIN memories m ON t.memory_id = m.id
		WHERE t.tag = ? AND m.deleted_at = 0`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListUnconsolidated returns memories not yet consolidated (category=memory, not deleted).
func (s *sqliteStore) ListUnconsolidated(limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, text, memory_type, importance, confidence, timestamp
		FROM memories WHERE consolidated = 0 AND deleted_at = 0 AND expired = 0 AND category = 'memory'
		ORDER BY timestamp DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Memory
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.Text, &m.MemoryType, &m.Importance, &m.Confidence, &m.Timestamp); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// CountUnconsolidated returns the number of unconsolidated memories.
func (s *sqliteStore) CountUnconsolidated() (int64, error) {
	var count int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE consolidated = 0 AND deleted_at = 0 AND expired = 0 AND category = 'memory'`).Scan(&count)
	return count, err
}

// MarkConsolidated marks memories as consolidated.
func (s *sqliteStore) MarkConsolidated(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE memories SET consolidated = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateConsolidation stores a consolidation result.
func (s *sqliteStore) CreateConsolidation(c *Consolidation) (string, error) {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = time.Now().Unix()
	}
	_, err := s.db.Exec(`INSERT INTO consolidations (id, source_ids, summary, insight, patterns, connections, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.SourceIDs, c.Summary, c.Insight, c.Patterns, c.ConnectionsJSON, c.CreatedAt)
	return c.ID, err
}

// ListConsolidations returns recent consolidations.
func (s *sqliteStore) ListConsolidations(limit int) ([]*Consolidation, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(`SELECT id, source_ids, summary, insight, patterns, connections, created_at
		FROM consolidations ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Consolidation
	for rows.Next() {
		c := &Consolidation{}
		if err := rows.Scan(&c.ID, &c.SourceIDs, &c.Summary, &c.Insight, &c.Patterns, &c.ConnectionsJSON, &c.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// AddConnection adds a connection to a memory (safe JSON encoding).
// Uses a transaction to prevent lost updates under concurrent writes.
func (s *sqliteStore) AddConnection(memoryID, linkedTo, relationship string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var connJSON string
	err = tx.QueryRow(`SELECT connections FROM memories WHERE id = ?`, memoryID).Scan(&connJSON)
	if err != nil {
		return err
	}
	if connJSON == "" {
		connJSON = "[]"
	}

	var conns []map[string]string
	if err := json.Unmarshal([]byte(connJSON), &conns); err != nil {
		return fmt.Errorf("failed to parse existing connections: %w", err)
	}
	// Prevent duplicate links; update relationship if new one is more specific
	for i, c := range conns {
		if c["linked_to"] == linkedTo {
			oldIsGeneric := strings.HasPrefix(c["relationship"], "related (sim=")
			newIsGeneric := strings.HasPrefix(relationship, "related (sim=")
			// Upgrade: replace generic label with specific one from consolidation
			if oldIsGeneric && !newIsGeneric && relationship != "" {
				conns[i]["relationship"] = relationship
				updated, err := json.Marshal(conns)
				if err != nil {
					return fmt.Errorf("failed to marshal connections: %w", err)
				}
				_, err = tx.Exec(`UPDATE memories SET connections = ? WHERE id = ?`, string(updated), memoryID)
				if err != nil {
					return err
				}
				return tx.Commit()
			}
			return nil // already connected with adequate label
		}
	}
	conns = append(conns, map[string]string{
		"linked_to":    linkedTo,
		"relationship": relationship,
	})
	updated, err := json.Marshal(conns)
	if err != nil {
		return fmt.Errorf("failed to marshal connections: %w", err)
	}
	_, err = tx.Exec(`UPDATE memories SET connections = ? WHERE id = ?`, string(updated), memoryID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// scanMemoriesWithVector scans rows from a query that includes a trailing v.vector column (nullable).
// Expected column order: id, text, abstract, overview, category, scope, importance, timestamp, metadata,
// hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count,
// memory_type, confidence, access_count, last_accessed, expires_at, source_conv, content_hash, expired,
// vector
func scanMemoriesWithVector(rows *sql.Rows) ([]*Memory, error) {
	var result []*Memory
	for rows.Next() {
		m := &Memory{}
		var hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string
		var tokenCount *int
		var sourceConv, contentHash *string
		var expired int
		var vectorData []byte
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel,
			&parentID, &nodeType, &sourceFile, &m.ChunkIndex, &tokenCount,
			&m.MemoryType, &m.Confidence, &m.AccessCount, &m.LastAccessed, &m.ExpiresAt,
			&sourceConv, &contentHash, &expired,
			&vectorData); err != nil {
			return nil, err
		}
		assignNullableFields(m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
		assignMemSysNullable(m, sourceConv, contentHash, expired)
		if len(vectorData) > 0 {
			vec, err := DeserializeVector(vectorData)
			if err == nil {
				m.Vector = vec
			}
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// scanMemories scans rows into Memory structs from queries that SELECT the standard 24-column set:
// id, text, abstract, overview, category, scope, importance, timestamp, metadata,
// hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count,
// memory_type, confidence, access_count, last_accessed, expires_at, source_conv, content_hash, expired
func scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var result []*Memory
	for rows.Next() {
		m := &Memory{}
		var hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string
		var tokenCount *int
		var sourceConv, contentHash *string
		var expired int
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel,
			&parentID, &nodeType, &sourceFile, &m.ChunkIndex, &tokenCount,
			&m.MemoryType, &m.Confidence, &m.AccessCount, &m.LastAccessed, &m.ExpiresAt,
			&sourceConv, &contentHash, &expired); err != nil {
			return nil, err
		}
		assignNullableFields(m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
		assignMemSysNullable(m, sourceConv, contentHash, expired)
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// assignNullableFields copies nullable scan results into the Memory struct.
func assignNullableFields(m *Memory, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string, tokenCount *int) {
	if hierarchyPath != nil {
		m.HierarchyPath = *hierarchyPath
	}
	if abstract != nil {
		m.Abstract = *abstract
	}
	if overview != nil {
		m.Overview = *overview
	}
	if parentID != nil {
		m.ParentID = *parentID
	}
	if nodeType != nil {
		m.NodeType = *nodeType
	}
	if sourceFile != nil {
		m.SourceFile = *sourceFile
	}
	if tokenCount != nil {
		m.TokenCount = *tokenCount
	}
}

// assignMemSysNullable copies nullable memory-system scan results into the Memory struct.
func assignMemSysNullable(m *Memory, sourceConv, contentHash *string, expired int) {
	if sourceConv != nil {
		m.SourceConv = *sourceConv
	}
	if contentHash != nil {
		m.ContentHash = *contentHash
	}
	m.Expired = expired != 0
}

// nullIfEmpty returns nil for empty strings, or the string value for SQL insertion.
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// defaultIfEmpty returns the default value if the string is empty.
func defaultIfEmpty(s, defaultVal string) string {
	if s == "" {
		return defaultVal
	}
	return s
}

// nullIfZero returns nil for zero values, or the int value for SQL insertion.
func nullIfZero(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}

// boolToInt converts a bool to SQLite integer (0/1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
