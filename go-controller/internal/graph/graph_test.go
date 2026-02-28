package graph

import (
	"database/sql"
	"math"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// #region test-add-edge
func TestAddEdge(t *testing.T) {
	db := setupTestDB(t)
	gs, err := NewGraphStore(db)
	if err != nil {
		t.Fatalf("new graph store: %v", err)
	}

	// Add edge
	if err := gs.AddEdge("a", "b", "co_retrieval", 0.1); err != nil {
		t.Fatalf("add edge: %v", err)
	}

	// Verify via GetNeighbors
	edges, err := gs.GetNeighbors("a", 0.0)
	if err != nil {
		t.Fatalf("get neighbors: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].TargetID != "b" || edges[0].EdgeType != "co_retrieval" {
		t.Errorf("unexpected edge: %+v", edges[0])
	}
	if math.Abs(edges[0].Weight-0.1) > 0.001 {
		t.Errorf("expected weight 0.1, got %.4f", edges[0].Weight)
	}

	// Duplicate insert should be ignored
	if err := gs.AddEdge("a", "b", "co_retrieval", 0.5); err != nil {
		t.Fatalf("duplicate add: %v", err)
	}
	edges, _ = gs.GetNeighbors("a", 0.0)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge after duplicate, got %d", len(edges))
	}
	// Weight should remain 0.1 (INSERT OR IGNORE)
	if math.Abs(edges[0].Weight-0.1) > 0.001 {
		t.Errorf("weight should not change on ignore, got %.4f", edges[0].Weight)
	}
}

// #endregion test-add-edge

// #region test-increment-edge
func TestIncrementEdge(t *testing.T) {
	db := setupTestDB(t)
	gs, err := NewGraphStore(db)
	if err != nil {
		t.Fatalf("new graph store: %v", err)
	}

	// First increment creates the edge
	if err := gs.IncrementEdge("a", "b", "co_retrieval", 0.1); err != nil {
		t.Fatalf("increment: %v", err)
	}

	edges, _ := gs.GetNeighbors("a", 0.0)
	if len(edges) != 1 || math.Abs(edges[0].Weight-0.1) > 0.001 {
		t.Fatalf("first increment: expected weight 0.1, got %+v", edges)
	}

	// Second increment should add 0.1
	if err := gs.IncrementEdge("a", "b", "co_retrieval", 0.1); err != nil {
		t.Fatalf("increment 2: %v", err)
	}
	edges, _ = gs.GetNeighbors("a", 0.0)
	if math.Abs(edges[0].Weight-0.2) > 0.001 {
		t.Errorf("expected weight 0.2, got %.4f", edges[0].Weight)
	}

	// Cap at 1.0
	if err := gs.IncrementEdge("a", "b", "co_retrieval", 5.0); err != nil {
		t.Fatalf("increment big: %v", err)
	}
	edges, _ = gs.GetNeighbors("a", 0.0)
	if math.Abs(edges[0].Weight-1.0) > 0.001 {
		t.Errorf("expected weight capped at 1.0, got %.4f", edges[0].Weight)
	}
}

// #endregion test-increment-edge

// #region test-walk
func TestWalk(t *testing.T) {
	db := setupTestDB(t)
	gs, err := NewGraphStore(db)
	if err != nil {
		t.Fatalf("new graph store: %v", err)
	}

	// Build a chain: a -> b -> c -> d
	gs.AddEdge("a", "b", "temporal", 0.5)
	gs.AddEdge("b", "c", "temporal", 0.8)
	gs.AddEdge("c", "d", "temporal", 0.3)
	// Add a branch: a -> e
	gs.AddEdge("a", "e", "co_retrieval", 0.2)

	result, err := gs.Walk("a", 5, 0.1, 100)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// Should visit a, b, e (from a), c (from b), d (from c) = 5 nodes
	if len(result.IDs) != 5 {
		t.Fatalf("expected 5 nodes, got %d: %v", len(result.IDs), result.IDs)
	}
	if result.IDs[0] != "a" {
		t.Errorf("first node should be 'a', got %s", result.IDs[0])
	}

	// With minWeight 0.3, 'e' edge (0.2) should be filtered
	result2, err := gs.Walk("a", 5, 0.3, 100)
	if err != nil {
		t.Fatalf("walk filtered: %v", err)
	}
	for _, id := range result2.IDs {
		if id == "e" {
			t.Error("node 'e' should be filtered by minWeight 0.3")
		}
	}

	// Depth limit
	result3, err := gs.Walk("a", 1, 0.1, 100)
	if err != nil {
		t.Fatalf("walk depth 1: %v", err)
	}
	// a + direct neighbors (b, e) = 3
	if len(result3.IDs) != 3 {
		t.Errorf("depth=1 should yield 3 nodes, got %d: %v", len(result3.IDs), result3.IDs)
	}

	// maxNodes cap
	result4, err := gs.Walk("a", 5, 0.1, 3)
	if err != nil {
		t.Fatalf("walk maxNodes 3: %v", err)
	}
	if len(result4.IDs) != 3 {
		t.Errorf("maxNodes=3 should yield 3 nodes, got %d: %v", len(result4.IDs), result4.IDs)
	}
}

// #endregion test-walk

// #region test-decay
func TestDecayAll(t *testing.T) {
	db := setupTestDB(t)
	gs, err := NewGraphStore(db)
	if err != nil {
		t.Fatalf("new graph store: %v", err)
	}

	// Insert an edge with old timestamp
	past := time.Now().UTC().Add(-96 * time.Hour).Format(time.RFC3339) // 96 hours ago
	db.Exec(
		`INSERT INTO evidence_edges (source_id, target_id, edge_type, weight, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"old-a", "old-b", "temporal", 0.1, past, past,
	)

	// Insert a fresh edge
	gs.AddEdge("new-a", "new-b", "temporal", 0.5)

	// Decay with 48h half-life
	deleted, err := gs.DecayAll(48.0)
	if err != nil {
		t.Fatalf("decay: %v", err)
	}

	// Old edge (0.1 * exp(-96h * ln2 / 48h)) = 0.1 * 0.25 = 0.025 > 0.01, should survive
	// Actually: 0.1 * exp(-2 * ln2) = 0.1 * 0.25 = 0.025, survives.
	// But if weight were 0.03: 0.03 * 0.25 = 0.0075, would be deleted.

	// Fresh edge should barely decay
	edges, _ := gs.GetNeighbors("new-a", 0.0)
	if len(edges) != 1 {
		t.Fatalf("fresh edge should survive, got %d", len(edges))
	}
	if edges[0].Weight < 0.49 {
		t.Errorf("fresh edge should barely decay, got %.4f", edges[0].Weight)
	}

	_ = deleted // old edge should survive with 0.025
}

// #endregion test-decay

// #region test-sever
func TestSeverNode(t *testing.T) {
	db := setupTestDB(t)
	gs, err := NewGraphStore(db)
	if err != nil {
		t.Fatalf("new graph store: %v", err)
	}

	gs.AddEdge("a", "b", "temporal", 0.5)
	gs.AddEdge("b", "c", "temporal", 0.5)
	gs.AddEdge("c", "b", "co_retrieval", 0.3)

	// Sever 'b' — should remove a->b, b->c, c->b
	if err := gs.SeverNode("b"); err != nil {
		t.Fatalf("sever: %v", err)
	}

	// No neighbors from a (a->b gone)
	edges, _ := gs.GetNeighbors("a", 0.0)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges from 'a' after sever, got %d", len(edges))
	}

	// No neighbors from b (b->c gone)
	edges, _ = gs.GetNeighbors("b", 0.0)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges from 'b' after sever, got %d", len(edges))
	}

	// No neighbors from c (c->b gone)
	edges, _ = gs.GetNeighbors("c", 0.0)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges from 'c' after sever, got %d", len(edges))
	}
}

// #endregion test-sever
