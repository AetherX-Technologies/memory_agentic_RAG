package store

// BM25Search 使用 FTS5 进行全文检索
func (s *sqliteStore) BM25Search(query string, limit int, scopes []string) ([]SearchResult, error) {
	scopeFilter := ""
	args := []interface{}{query}

	if len(scopes) > 0 {
		scopeFilter = " AND m.scope IN ("
		for i := range scopes {
			if i > 0 {
				scopeFilter += ","
			}
			scopeFilter += "?"
			args = append(args, scopes[i])
		}
		scopeFilter += ")"
	}

	args = append(args, limit)

	queryStr := `
		SELECT f.memory_id, f.rank, m.text, m.category, m.scope,
			m.importance, m.timestamp, m.metadata
		FROM fts_memories f
		JOIN memories m ON f.memory_id = m.id
		WHERE fts_memories MATCH ?` + scopeFilter + `
		ORDER BY f.rank
		LIMIT ?`

	rows, err := s.db.Query(queryStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0, limit)
	for rows.Next() {
		var memoryID, text, category, scope, metadata string
		var importance float64
		var timestamp int64
		var ftsRank float64

		if err := rows.Scan(&memoryID, &ftsRank, &text, &category, &scope,
			&importance, &timestamp, &metadata); err != nil {
			return nil, err
		}

		// FTS5 rank 是负数（越小越相关），取绝对值作为分数
		score := -ftsRank
		if score < 0 {
			score = 0
		}

		results = append(results, SearchResult{
			Entry: Memory{
				ID:         memoryID,
				Text:       text,
				Category:   category,
				Scope:      scope,
				Importance: importance,
				Timestamp:  timestamp,
				Metadata:   metadata,
			},
			Score: score,
		})
	}

	return results, nil
}
