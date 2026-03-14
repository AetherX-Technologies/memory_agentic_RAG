package mobile

import (
	"encoding/json"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

type MemoryDB struct {
	store store.Store
}

func NewMemoryDB(dbPath string, vectorDim int) (*MemoryDB, error) {
	st, err := store.New(store.Config{
		DBPath:    dbPath,
		VectorDim: vectorDim,
	})
	if err != nil {
		return nil, err
	}
	return &MemoryDB{store: st}, nil
}

func (db *MemoryDB) Insert(text, category, scope string, importance float64, vectorJSON string) (string, error) {
	var vector []float32
	if vectorJSON != "" {
		if err := json.Unmarshal([]byte(vectorJSON), &vector); err != nil {
			return "", err
		}
	}

	memory := &store.Memory{
		Text:       text,
		Category:   category,
		Scope:      scope,
		Importance: importance,
		Vector:     vector,
	}

	return db.store.Insert(memory)
}

func (db *MemoryDB) Search(vectorJSON string, limit int, scopesJSON string) (string, error) {
	var vector []float32
	if err := json.Unmarshal([]byte(vectorJSON), &vector); err != nil {
		return "", err
	}

	var scopes []string
	if scopesJSON != "" {
		if err := json.Unmarshal([]byte(scopesJSON), &scopes); err != nil {
			return "", err
		}
	}

	results, err := db.store.VectorSearch(vector, limit, scopes)
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(results)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (db *MemoryDB) Delete(id string) error {
	return db.store.Delete(id)
}

func (db *MemoryDB) Close() error {
	return db.store.Close()
}
