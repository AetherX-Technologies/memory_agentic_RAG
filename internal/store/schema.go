package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
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

	// FTS5 with unicode61 tokenizer.
	// CJK text is pre-segmented into single characters before insertion,
	// so unicode61 can match individual Chinese characters.
	schemaFTS = `
CREATE VIRTUAL TABLE IF NOT EXISTS fts_memories USING fts5(
    memory_id UNINDEXED,
    content,
    tokenize='unicode61'
);
`

	// Triggers removed — FTS insertion is now handled in Go code
	// to support CJK character segmentation before indexing.
	schemaTriggers = `
CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    DELETE FROM fts_memories WHERE memory_id = old.id;
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
	{"consolidated", "INTEGER DEFAULT 0"},
	{"connections", "TEXT DEFAULT '[]'"},
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
		`CREATE INDEX IF NOT EXISTS idx_consolidated ON memories(consolidated)`,
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
		`CREATE TABLE IF NOT EXISTS consolidations (
			id TEXT PRIMARY KEY,
			source_ids TEXT NOT NULL DEFAULT '[]',
			summary TEXT NOT NULL DEFAULT '',
			insight TEXT NOT NULL DEFAULT '',
			patterns TEXT NOT NULL DEFAULT '[]',
			connections TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_consolidation_created ON consolidations(created_at DESC)`,
		// warmFriend v3.2 fact-fusion §C round 3 fix：
		// sweep 把已经判过但不建链的 (old, new) pair 记下来，避免反复重判同一批；
		// 只持久化 LLM 稳定结论（same / new_less_specific / unrelated）。
		// 如果 fact 被删 → CASCADE 自动清理；fact 被改写 → 不主动 invalidate
		// （sweep 重抽时即便结论变了，也只是漏掉一次融合机会，不伤数据）。
		// codex round 4 low：state 列加 CHECK 白名单，挡住外部/旧 bug 写入
		// "error"/"unknown"/任意值导致后续 sweep 永久跳过该 pair。
		// v3.3 round 1：新增 complementary_merged（同对象异属性合一条新 fact
		// 已成功，原 source pair 不要再判）和 merge_failed（generator 返空/
		// LLM 明确不可合并，避免无限重试烧 LLM）。
		`CREATE TABLE IF NOT EXISTS memory_sweep_judgements (
			old_id TEXT NOT NULL,
			new_id TEXT NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('same','new_less_specific','unrelated','complementary_merged','merge_failed')),
			judged_at INTEGER NOT NULL,
			PRIMARY KEY (old_id, new_id),
			FOREIGN KEY (old_id) REFERENCES memories(id) ON DELETE CASCADE,
			FOREIGN KEY (new_id) REFERENCES memories(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sweep_judgements_old ON memory_sweep_judgements(old_id)`,
	}
	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("failed to create memory system table: %w", err)
		}
	}

	// codex round 5 low: round-3 已经创建过 memory_sweep_judgements 但没
	// CHECK 约束的老库，CREATE TABLE IF NOT EXISTS 不会升级 schema。这里
	// 用 sqlite_master.sql 探测；缺 CHECK 时重建表（合法旧数据迁过去，
	// 非法 state 的行被丢弃——读侧白名单已经在过滤这些行了）。
	if err := upgradeSweepJudgementsCheck(db); err != nil {
		return fmt.Errorf("failed to upgrade memory_sweep_judgements CHECK: %w", err)
	}

	return nil
}

// upgradeSweepJudgementsCheck rebuilds memory_sweep_judgements when the existing
// table predates the round-5 CHECK constraint (round-3 created it without).
// No-op when the table either doesn't exist (fresh install — CREATE TABLE IF
// NOT EXISTS above already used the correct schema) or already has CHECK.
func upgradeSweepJudgementsCheck(db *sql.DB) error {
	var sqlText sql.NullString
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='memory_sweep_judgements'`,
	).Scan(&sqlText)
	if err == sql.ErrNoRows {
		return nil // never created — CREATE TABLE IF NOT EXISTS above will run on next call
	}
	if err != nil {
		return fmt.Errorf("read sqlite_master: %w", err)
	}
	if !sqlText.Valid {
		return nil
	}
	// codex v3.3 round 2 critical: 老的判断 "包含 CHECK 就跳过" 让 v3.2 round-3
	// 已建过 3 态 CHECK 的库不会升级到 5 态，complementary_merged / merge_failed
	// 写入会被 CHECK 拒。改成检测两个新增 state 是否都已存在 schema 里——只要
	// 缺任一都重建。
	upper := strings.ToUpper(sqlText.String)
	if strings.Contains(upper, "COMPLEMENTARY_MERGED") &&
		strings.Contains(upper, "MERGE_FAILED") {
		return nil // 已在 5 态 schema
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TABLE memory_sweep_judgements_new (
		old_id TEXT NOT NULL,
		new_id TEXT NOT NULL,
		state TEXT NOT NULL CHECK (state IN ('same','new_less_specific','unrelated','complementary_merged','merge_failed')),
		judged_at INTEGER NOT NULL,
		PRIMARY KEY (old_id, new_id),
		FOREIGN KEY (old_id) REFERENCES memories(id) ON DELETE CASCADE,
		FOREIGN KEY (new_id) REFERENCES memories(id) ON DELETE CASCADE
	)`); err != nil {
		return err
	}
	// codex round 6 high: 旧库可能有 orphan judgement（FK 之前不可靠时
	// memories 的 hard delete 没 cascade 删 judgement），新表带 FK 时
	// INSERT 会因 orphan 触发 FK 失败导致整个迁移回滚阻断启动。这里
	// 在 SELECT 阶段就 EXISTS 双侧校验把 orphan 过滤掉。
	// v3.3 round 1：state 白名单同步扩到 5 个（complementary_merged/merge_failed
	// 在新表的 CHECK 里允许，但旧表里不可能有这两个值，所以 IN 列表实际
	// 只过滤老 4 态）。
	if _, err := tx.Exec(`INSERT INTO memory_sweep_judgements_new
		SELECT j.old_id, j.new_id, j.state, j.judged_at
		FROM memory_sweep_judgements j
		WHERE j.state IN ('same','new_less_specific','unrelated','complementary_merged','merge_failed')
		  AND EXISTS (SELECT 1 FROM memories m WHERE m.id = j.old_id)
		  AND EXISTS (SELECT 1 FROM memories m WHERE m.id = j.new_id)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE memory_sweep_judgements`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE memory_sweep_judgements_new RENAME TO memory_sweep_judgements`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_sweep_judgements_old ON memory_sweep_judgements(old_id)`); err != nil {
		return err
	}
	return tx.Commit()
}
