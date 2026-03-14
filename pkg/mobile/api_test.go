package mobile

import (
	"encoding/json"
	"testing"
)

func TestMobileAPI(t *testing.T) {
	db, err := NewMemoryDB(":memory:", 3)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	vector := []float32{0.1, 0.2, 0.3}
	vectorJSON, _ := json.Marshal(vector)

	id, err := db.Insert("test memory", "test", "global", 0.8, string(vectorJSON))
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Error("expected non-empty ID")
	}

	results, err := db.Search(string(vectorJSON), 5, "")
	if err != nil {
		t.Fatal(err)
	}
	if results == "" {
		t.Error("expected non-empty results")
	}

	if err := db.Delete(id); err != nil {
		t.Fatal(err)
	}
}
