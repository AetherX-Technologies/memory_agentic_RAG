package store

import (
	"database/sql"
	"fmt"
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
