package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Store 存储接口
type Store interface {
	Insert(memory *Memory) (string, error)
	Get(id string) (*Memory, error)
	Delete(id string) error
	List(scope string, limit int) ([]*Memory, error)
	VectorSearch(query []float32, limit int, scopes []string) ([]SearchResult, error)
	HybridSearch(queryVector []float32, queryText string, limit int, scopes []string) ([]SearchResult, error)
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
	db, err := sql.Open("sqlite", config.DBPath)
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
		INSERT INTO memories (id, text, category, scope, importance, timestamp, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		memory.ID, memory.Text, memory.Category, memory.Scope,
		memory.Importance, memory.Timestamp, memory.Metadata)
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
	err := s.db.QueryRow(`
		SELECT id, text, category, scope, importance, timestamp, metadata
		FROM memories WHERE id = ?`, id).Scan(
		&memory.ID, &memory.Text, &memory.Category, &memory.Scope,
		&memory.Importance, &memory.Timestamp, &memory.Metadata)
	if err != nil {
		return nil, err
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

	query := `SELECT id, text, category, scope, importance, timestamp, metadata
		FROM memories WHERE scope = ? ORDER BY timestamp DESC LIMIT ?`
	rows, err := s.db.Query(query, scope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memories := make([]*Memory, 0, limit)
	for rows.Next() {
		m := &Memory{}
		if err := rows.Scan(&m.ID, &m.Text, &m.Category, &m.Scope,
			&m.Importance, &m.Timestamp, &m.Metadata); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return memories, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
