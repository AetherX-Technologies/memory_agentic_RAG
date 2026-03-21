package store

import (
	"database/sql"
	"fmt"
	"os"
)

const (
	colHierarchyPath  = "hierarchy_path"
	colHierarchyLevel = "hierarchy_level"
	tableMemories     = "memories"
)

const (
	schemaMemories = `
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    text TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'other',
    scope TEXT NOT NULL DEFAULT 'global',
    importance REAL NOT NULL DEFAULT 0.7,
    timestamp INTEGER NOT NULL,
    metadata TEXT DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_memories_scope ON memories(scope);
CREATE INDEX IF NOT EXISTS idx_memories_timestamp ON memories(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_memories_category ON memories(category);
CREATE INDEX IF NOT EXISTS idx_scope_timestamp ON memories(scope, timestamp DESC);
`

	schemaVectors = `
CREATE TABLE IF NOT EXISTS vectors (
    memory_id TEXT PRIMARY KEY,
    vector BLOB NOT NULL,
    dimension INTEGER NOT NULL,
    FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
);
`

	schemaFTS = `
CREATE VIRTUAL TABLE IF NOT EXISTS fts_memories USING fts5(
    memory_id UNINDEXED,
    content,
    tokenize='simple'
);
`

	schemaTriggers = `
CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO fts_memories(memory_id, content) VALUES (new.id, new.text);
END;

CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    DELETE FROM fts_memories WHERE memory_id = old.id;
END;

CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    UPDATE fts_memories SET content = new.text WHERE memory_id = new.id;
END;
`

	migrationHierarchy = `
ALTER TABLE memories ADD COLUMN hierarchy_path TEXT DEFAULT NULL;
ALTER TABLE memories ADD COLUMN hierarchy_level INTEGER DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_hierarchy_path ON memories(hierarchy_path);
CREATE INDEX IF NOT EXISTS idx_hierarchy_level ON memories(hierarchy_level);
`
)

// migrateHierarchy 添加层次字段（幂等性）
func migrateHierarchy(db *sql.DB) error {
	var pathCount, levelCount int
	if err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, tableMemories), colHierarchyPath).Scan(&pathCount); err != nil {
		return fmt.Errorf("failed to check %s column: %w", colHierarchyPath, err)
	}
	if err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, tableMemories), colHierarchyLevel).Scan(&levelCount); err != nil {
		return fmt.Errorf("failed to check %s column: %w", colHierarchyLevel, err)
	}

	if pathCount == 0 && levelCount == 0 {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT DEFAULT NULL`, tableMemories, colHierarchyPath)); err != nil {
			return fmt.Errorf("failed to add %s: %w", colHierarchyPath, err)
		}
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s INTEGER DEFAULT 0`, tableMemories, colHierarchyLevel)); err != nil {
			return fmt.Errorf("failed to add %s: %w", colHierarchyLevel, err)
		}
		_, err := db.Exec(fmt.Sprintf(`
			CREATE INDEX IF NOT EXISTS idx_%s ON %s(%s);
			CREATE INDEX IF NOT EXISTS idx_%s ON %s(%s);
		`, colHierarchyPath, tableMemories, colHierarchyPath, colHierarchyLevel, tableMemories, colHierarchyLevel))
		return err
	}

	if pathCount == 0 {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s TEXT DEFAULT NULL`, tableMemories, colHierarchyPath)); err != nil {
			return fmt.Errorf("failed to add %s: %w", colHierarchyPath, err)
		}
	}
	if levelCount == 0 {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s INTEGER DEFAULT 0`, tableMemories, colHierarchyLevel)); err != nil {
			return fmt.Errorf("failed to add %s: %w", colHierarchyLevel, err)
		}
	}

	_, err := db.Exec(fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s ON %s(%s);
		CREATE INDEX IF NOT EXISTS idx_%s ON %s(%s);
	`, colHierarchyPath, tableMemories, colHierarchyPath, colHierarchyLevel, tableMemories, colHierarchyLevel))
	return err
}

// openVikingColumns lists the columns added for OpenViking L0/L1/L2 support.
var openVikingColumns = []struct {
	Name    string
	DDL     string
}{
	{"abstract", "TEXT DEFAULT NULL"},
	{"overview", "TEXT DEFAULT NULL"},
	{"parent_id", "TEXT DEFAULT NULL"},
	{"node_type", "TEXT DEFAULT 'chunk'"},
	{"source_file", "TEXT DEFAULT NULL"},
	{"chunk_index", "INTEGER DEFAULT 0"},
	{"token_count", "INTEGER DEFAULT NULL"},
}

// migrateOpenViking adds L0/L1/L2 and tree-structure columns for OpenViking integration (idempotent).
func migrateOpenViking(db *sql.DB) error {
	for _, col := range openVikingColumns {
		var count int
		err := db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, tableMemories),
			col.Name,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check column %s: %w", col.Name, err)
		}
		if count == 0 {
			_, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, tableMemories, col.Name, col.DDL))
			if err != nil {
				return fmt.Errorf("failed to add column %s: %w", col.Name, err)
			}
		}
	}

	// Create indexes (IF NOT EXISTS is idempotent)
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_parent_id ON memories(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_node_type ON memories(node_type)`,
		`CREATE INDEX IF NOT EXISTS idx_source_file ON memories(source_file)`,
		`CREATE INDEX IF NOT EXISTS idx_chunk_index ON memories(source_file, chunk_index)`,
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// memorySystemColumns lists the columns added for the AI memory system.
var memorySystemColumns = []struct {
	Name string
	DDL  string
}{
	{"memory_type", "TEXT DEFAULT 'fact'"},
	{"confidence", "REAL DEFAULT 0.5"},
	{"access_count", "INTEGER DEFAULT 0"},
	{"last_accessed", "INTEGER DEFAULT 0"},
	{"expires_at", "INTEGER DEFAULT 0"},
	{"source_conv", "TEXT DEFAULT NULL"},
	{"content_hash", "TEXT DEFAULT NULL"},
	{"expired", "INTEGER DEFAULT 0"},
	{"deleted_at", "INTEGER DEFAULT 0"},
}

// migrateMemorySystem adds AI memory system columns and tables (idempotent).
func migrateMemorySystem(db *sql.DB) error {
	// Add columns
	for _, col := range memorySystemColumns {
		var count int
		err := db.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, tableMemories),
			col.Name,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check column %s: %w", col.Name, err)
		}
		if count == 0 {
			_, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, tableMemories, col.Name, col.DDL))
			if err != nil {
				return fmt.Errorf("failed to add column %s: %w", col.Name, err)
			}
		}
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_memory_type ON memories(memory_type)`,
		`CREATE INDEX IF NOT EXISTS idx_expires_at ON memories(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_source_conv ON memories(source_conv)`,
		`CREATE INDEX IF NOT EXISTS idx_deleted_at ON memories(deleted_at)`,
	}
	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Unique partial index on content_hash (dedup guard)
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_content_hash_unique ON memories(content_hash) WHERE content_hash IS NOT NULL`); err != nil {
		fmt.Fprintf(os.Stderr, "warning: content_hash unique index creation failed (dedup guard inactive): %v\n", err)
	}

	// Create junction tables
	tables := []string{
		`CREATE TABLE IF NOT EXISTS memory_supersessions (
			old_id TEXT NOT NULL,
			new_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (old_id, new_id),
			FOREIGN KEY (old_id) REFERENCES memories(id) ON DELETE CASCADE,
			FOREIGN KEY (new_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_supersession_old ON memory_supersessions(old_id)`,
		`CREATE TABLE IF NOT EXISTS memory_tags (
			memory_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			PRIMARY KEY (memory_id, tag),
			FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tag ON memory_tags(tag)`,
	}
	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create memory system table: %w", err)
		}
	}

	return nil
}
