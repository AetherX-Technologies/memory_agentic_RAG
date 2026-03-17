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

				// 如果可执行文件目录找不到，尝试当前工作目录
				if extPath == "" {
					cwd, err := os.Getwd()
					if err == nil {
						candidate := filepath.Join(cwd, "lib", extName)
						if _, err := os.Stat(candidate); err == nil {
							extPath = candidate
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
	schemas := []string{schemaMemories, schemaVectors, schemaFTS, schemaTriggers}
	for _, schema := range schemas {
		if _, err := db.Exec(schema); err != nil {
			return err
		}
	}

	// 执行层次字段迁移
	if err := migrateHierarchy(db); err != nil {
		return fmt.Errorf("failed to migrate hierarchy: %w", err)
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
	var hierarchyPathVal interface{}
	if memory.HierarchyPath == "" {
		hierarchyPathVal = nil
	} else {
		hierarchyPathVal = memory.HierarchyPath
	}
	_, err = tx.Exec(`
		INSERT INTO memories (id, text, category, scope, importance, timestamp, metadata, hierarchy_path, hierarchy_level)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		memory.ID, memory.Text, memory.Category, memory.Scope,
		memory.Importance, memory.Timestamp, memory.Metadata, hierarchyPathVal, memory.HierarchyLevel)
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
	var hierarchyPath *string
	err := s.db.QueryRow(`
		SELECT id, text, category, scope, importance, timestamp, metadata, hierarchy_path, hierarchy_level
		FROM memories WHERE id = ?`, id).Scan(
		&memory.ID, &memory.Text, &memory.Category, &memory.Scope,
		&memory.Importance, &memory.Timestamp, &memory.Metadata, &hierarchyPath, &memory.HierarchyLevel)
	if err != nil {
		return nil, err
	}
	if hierarchyPath != nil {
		memory.HierarchyPath = *hierarchyPath
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

	query := `SELECT id, text, category, scope, importance, timestamp, metadata, hierarchy_path, hierarchy_level
		FROM memories WHERE scope = ? ORDER BY timestamp DESC LIMIT ?`
	rows, err := s.db.Query(query, scope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := make([]*Memory, 0, limit)
	for rows.Next() {
		m := &Memory{}
		var hierarchyPath *string
		if err := rows.Scan(&m.ID, &m.Text, &m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata, &hierarchyPath, &m.HierarchyLevel); err != nil {
			return nil, err
		}
		if hierarchyPath != nil {
			m.HierarchyPath = *hierarchyPath
		}
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
