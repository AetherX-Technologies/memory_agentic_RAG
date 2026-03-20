package store

import (
	"database/sql"
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
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL: %w", err)
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

	// FTS5 with tokenizer fallback: try 'simple' (Chinese), fall back to 'unicode61' (built-in)
	if _, err := db.Exec(schemaFTS); err != nil {
		ftsUnicode := `CREATE VIRTUAL TABLE IF NOT EXISTS fts_memories USING fts5(
			memory_id UNINDEXED, content, tokenize='unicode61');`
		if _, err2 := db.Exec(ftsUnicode); err2 != nil {
			return fmt.Errorf("FTS5 unavailable (tried 'simple' and 'unicode61'): %w", err2)
		}
		fmt.Fprintf(os.Stderr, "warning: FTS5 using unicode61 fallback tokenizer (Chinese segmentation degraded)\n")
	}

	// FTS triggers
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

	// 插入记忆
	_, err = tx.Exec(`
		INSERT INTO memories (id, text, abstract, overview, category, scope, importance, timestamp, metadata,
			hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		memory.ID, memory.Text,
		nullIfEmpty(memory.Abstract), nullIfEmpty(memory.Overview),
		memory.Category, memory.Scope,
		memory.Importance, memory.Timestamp, memory.Metadata,
		nullIfEmpty(memory.HierarchyPath), memory.HierarchyLevel,
		nullIfEmpty(memory.ParentID),
		defaultIfEmpty(memory.NodeType, "chunk"),
		nullIfEmpty(memory.SourceFile),
		memory.ChunkIndex, nullIfZero(memory.TokenCount))
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

	return memory.ID, tx.Commit()
}

func (s *sqliteStore) Get(id string) (*Memory, error) {
	memory := &Memory{}
	var hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string
	var tokenCount *int
	err := s.db.QueryRow(`
		SELECT id, text, abstract, overview, category, scope, importance, timestamp, metadata,
			hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count
		FROM memories WHERE id = ?`, id).Scan(
		&memory.ID, &memory.Text, &abstract, &overview,
		&memory.Category, &memory.Scope,
		&memory.Importance, &memory.Timestamp, &memory.Metadata,
		&hierarchyPath, &memory.HierarchyLevel,
		&parentID, &nodeType, &sourceFile, &memory.ChunkIndex, &tokenCount)
	if err != nil {
		return nil, err
	}
	assignNullableFields(memory, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)

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
		hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count
		FROM memories WHERE scope = ? ORDER BY timestamp DESC LIMIT ?`
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
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel,
			&parentID, &nodeType, &sourceFile, &m.ChunkIndex, &tokenCount); err != nil {
			return nil, err
		}
		assignNullableFields(m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
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
			v.vector
		FROM memories m
		LEFT JOIN vectors v ON m.id = v.memory_id
		WHERE m.parent_id = ? ORDER BY m.chunk_index`, parentID)
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

// scanMemoriesWithVector scans rows from a query that includes a trailing v.vector column (nullable).
func scanMemoriesWithVector(rows *sql.Rows) ([]*Memory, error) {
	var result []*Memory
	for rows.Next() {
		m := &Memory{}
		var hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string
		var tokenCount *int
		var vectorData []byte
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel,
			&parentID, &nodeType, &sourceFile, &m.ChunkIndex, &tokenCount,
			&vectorData); err != nil {
			return nil, err
		}
		assignNullableFields(m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
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

// scanMemories scans rows into Memory structs from queries that SELECT the standard 16-column set:
// id, text, abstract, overview, category, scope, importance, timestamp, metadata,
// hierarchy_path, hierarchy_level, parent_id, node_type, source_file, chunk_index, token_count
func scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var result []*Memory
	for rows.Next() {
		m := &Memory{}
		var hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string
		var tokenCount *int
		if err := rows.Scan(&m.ID, &m.Text, &abstract, &overview,
			&m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata,
			&hierarchyPath, &m.HierarchyLevel,
			&parentID, &nodeType, &sourceFile, &m.ChunkIndex, &tokenCount); err != nil {
			return nil, err
		}
		assignNullableFields(m, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile, tokenCount)
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// assignNullableFields copies nullable scan results into the Memory struct.
func assignNullableFields(m *Memory, hierarchyPath, abstract, overview, parentID, nodeType, sourceFile *string, tokenCount *int) {
	if hierarchyPath != nil { m.HierarchyPath = *hierarchyPath }
	if abstract != nil { m.Abstract = *abstract }
	if overview != nil { m.Overview = *overview }
	if parentID != nil { m.ParentID = *parentID }
	if nodeType != nil { m.NodeType = *nodeType }
	if sourceFile != nil { m.SourceFile = *sourceFile }
	if tokenCount != nil { m.TokenCount = *tokenCount }
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
