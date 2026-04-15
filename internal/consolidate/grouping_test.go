package consolidate

import (
	"testing"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

func TestGroupByConnections_AllOrphansBatched(t *testing.T) {
	// 5 memories, no connections — should be batched into one orphan group of 5
	mems := []*store.Memory{
		{ID: "m1", Text: "a"},
		{ID: "m2", Text: "b"},
		{ID: "m3", Text: "c"},
		{ID: "m4", Text: "d"},
		{ID: "m5", Text: "e"},
	}
	fullByID := make(map[string]*store.Memory)
	uncID := make(map[string]bool)
	for _, m := range mems {
		fullByID[m.ID] = m
		uncID[m.ID] = true
	}

	groups := groupByConnections(mems, fullByID, uncID, 10)
	if len(groups) != 1 {
		t.Fatalf("expected 1 orphan group, got %d", len(groups))
	}
	if len(groups[0]) != 5 {
		t.Errorf("orphan group size = %d, want 5", len(groups[0]))
	}
}

func TestGroupByConnections_ConnectedCluster(t *testing.T) {
	// 3 connected memories + 2 orphans
	mems := []*store.Memory{
		{ID: "m1", Text: "Go skill", Connections: `[{"linked_to":"m2","relationship":"r"},{"linked_to":"m3","relationship":"r"}]`},
		{ID: "m2", Text: "Go project", Connections: `[{"linked_to":"m1","relationship":"r"}]`},
		{ID: "m3", Text: "Go style", Connections: `[{"linked_to":"m1","relationship":"r"}]`},
		{ID: "m4", Text: "orphan-a"},
		{ID: "m5", Text: "orphan-b"},
	}
	fullByID := make(map[string]*store.Memory)
	uncID := make(map[string]bool)
	for _, m := range mems {
		fullByID[m.ID] = m
		uncID[m.ID] = true
	}

	groups := groupByConnections(mems, fullByID, uncID, 10)
	// Expect 2 groups: 1 connected (m1/m2/m3) + 1 orphan batch (m4, m5)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// First group should have all 3 connected
	connGroup := groups[0]
	if len(connGroup) != 3 {
		t.Errorf("connected group size = %d, want 3", len(connGroup))
	}
	// Second group should be orphan batch
	if len(groups[1]) != 2 {
		t.Errorf("orphan group size = %d, want 2", len(groups[1]))
	}
}

func TestGroupByConnections_CapAtMaxSize(t *testing.T) {
	// 15 memories, all chained to m1 — connected group should cap at maxSize
	mems := []*store.Memory{{ID: "m1", Text: "seed"}}
	for i := 2; i <= 15; i++ {
		id := "m" + string(rune('0'+i%10))
		if i >= 10 {
			id = "m1" + string(rune('0'+i%10))
		}
		conn := `[{"linked_to":"m1","relationship":"r"}]`
		mems = append(mems, &store.Memory{ID: id, Text: "leaf", Connections: conn})
	}
	// Update m1's connections to link to all leaves
	var links string = "["
	for i := 1; i < len(mems); i++ {
		if i > 1 {
			links += ","
		}
		links += `{"linked_to":"` + mems[i].ID + `","relationship":"r"}`
	}
	links += "]"
	mems[0].Connections = links

	fullByID := make(map[string]*store.Memory)
	uncID := make(map[string]bool)
	for _, m := range mems {
		fullByID[m.ID] = m
		uncID[m.ID] = true
	}

	groups := groupByConnections(mems, fullByID, uncID, 10)
	// First group should cap at 10
	if len(groups[0]) > 10 {
		t.Errorf("group 0 size %d exceeds maxSize 10", len(groups[0]))
	}
}

func TestGroupByConnections_LargeClusterSplitsIntoMultipleGroups(t *testing.T) {
	// 12 memories all connected in a clique; with maxSize=5, should produce
	// 3 groups (5+5+2), NOT 1 capped group + 7 stranded singletons.
	const N = 12
	mems := make([]*store.Memory, N)
	for i := 0; i < N; i++ {
		id := "m"
		if i < 10 {
			id += string(rune('0' + i))
		} else {
			id += "1" + string(rune('0'+i%10))
		}
		// Each memory connects to all others
		var links string = "["
		first := true
		for j := 0; j < N; j++ {
			if j == i {
				continue
			}
			otherID := "m"
			if j < 10 {
				otherID += string(rune('0' + j))
			} else {
				otherID += "1" + string(rune('0'+j%10))
			}
			if !first {
				links += ","
			}
			first = false
			links += `{"linked_to":"` + otherID + `","relationship":"r"}`
		}
		links += "]"
		mems[i] = &store.Memory{ID: id, Text: "x", Connections: links}
	}

	fullByID := make(map[string]*store.Memory)
	uncID := make(map[string]bool)
	for _, m := range mems {
		fullByID[m.ID] = m
		uncID[m.ID] = true
	}

	groups := groupByConnections(mems, fullByID, uncID, 5)

	// Count memories consolidated (groups with size >= 2)
	consolidated := 0
	for _, g := range groups {
		if len(g) >= 2 {
			consolidated += len(g)
		}
	}
	if consolidated < N {
		t.Errorf("large cluster stranded %d memories (got %d/%d consolidated)", N-consolidated, consolidated, N)
	}

	// Verify no group exceeds maxSize
	for i, g := range groups {
		if len(g) > 5 {
			t.Errorf("group %d has %d nodes, exceeds maxSize=5", i, len(g))
		}
	}
}

func TestGroupByConnections_ExcludesConsolidated(t *testing.T) {
	// m1 connects to m2 (unconsolidated) and m3 (already consolidated)
	// m3 should be filtered out
	mems := []*store.Memory{
		{ID: "m1", Text: "a", Connections: `[{"linked_to":"m2","relationship":"r"},{"linked_to":"m3","relationship":"r"}]`},
		{ID: "m2", Text: "b"},
	}
	fullByID := map[string]*store.Memory{
		"m1": mems[0], "m2": mems[1],
		// m3 exists in store but NOT in uncID
	}
	uncID := map[string]bool{"m1": true, "m2": true}

	groups := groupByConnections(mems, fullByID, uncID, 10)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0]) != 2 {
		t.Errorf("group should contain m1+m2 only (m3 filtered), got %d", len(groups[0]))
	}
}
