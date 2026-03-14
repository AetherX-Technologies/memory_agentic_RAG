package store

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
    tokenize='unicode61'
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
)
