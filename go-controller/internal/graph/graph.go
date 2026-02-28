package graph

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// #region schema
const schema = `
CREATE TABLE IF NOT EXISTS evidence_edges (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id   TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    edge_type   TEXT NOT NULL,
    weight      REAL NOT NULL DEFAULT 0.1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE(source_id, target_id, edge_type)
);
CREATE INDEX IF NOT EXISTS idx_edges_source ON evidence_edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON evidence_edges(target_id);
`

// #endregion schema

// #region types
// Edge represents a weighted link between two evidence nodes.
type Edge struct {
	ID        int64
	SourceID  string
	TargetID  string
	EdgeType  string
	Weight    float64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// WalkResult holds an ordered path from a graph walk.
type WalkResult struct {
	IDs    []string  // node IDs in walk order
	Scores []float64 // cumulative scores at each node
}

// GraphStore manages the evidence_edges table.
type GraphStore struct {
	db *sql.DB
}

// #endregion types

// #region constructor
// NewGraphStore creates tables and returns a GraphStore.
func NewGraphStore(db *sql.DB) (*GraphStore, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("graph schema: %w", err)
	}
	return &GraphStore{db: db}, nil
}

// #endregion constructor

// #region add-edge
// AddEdge inserts a new edge. If the edge already exists (same source, target, type), it is ignored.
func (g *GraphStore) AddEdge(sourceID, targetID, edgeType string, weight float64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := g.db.Exec(
		`INSERT OR IGNORE INTO evidence_edges (source_id, target_id, edge_type, weight, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sourceID, targetID, edgeType, weight, now, now,
	)
	return err
}

// #endregion add-edge

// #region increment-edge
// IncrementEdge increases the weight of an existing edge by delta, capped at 1.0.
// If the edge doesn't exist, it is created with weight=delta.
func (g *GraphStore) IncrementEdge(sourceID, targetID, edgeType string, delta float64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := g.db.Exec(
		`INSERT INTO evidence_edges (source_id, target_id, edge_type, weight, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source_id, target_id, edge_type) DO UPDATE SET
		   weight = MIN(1.0, evidence_edges.weight + ?),
		   updated_at = ?`,
		sourceID, targetID, edgeType, delta, now, now,
		delta, now,
	)
	return err
}

// #endregion increment-edge

// #region get-neighbors
// GetNeighbors returns all edges from sourceID with weight >= minWeight, ordered by weight descending.
func (g *GraphStore) GetNeighbors(nodeID string, minWeight float64) ([]Edge, error) {
	rows, err := g.db.Query(
		`SELECT id, source_id, target_id, edge_type, weight, created_at, updated_at
		 FROM evidence_edges
		 WHERE source_id = ? AND weight >= ?
		 ORDER BY weight DESC`,
		nodeID, minWeight,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		var createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.SourceID, &e.TargetID, &e.EdgeType, &e.Weight, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// #endregion get-neighbors

// #region walk
// Walk performs a BFS from entryID, following edges with weight >= minWeight,
// up to maxDepth hops and maxNodes total. Returns nodes in visit order with cumulative scores.
func (g *GraphStore) Walk(entryID string, maxDepth int, minWeight float64, maxNodes int) (WalkResult, error) {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if maxNodes <= 0 {
		maxNodes = 10
	}

	result := WalkResult{
		IDs:    []string{entryID},
		Scores: []float64{1.0},
	}
	visited := map[string]bool{entryID: true}

	// BFS queue: (nodeID, depth, cumulativeScore)
	type queueItem struct {
		id    string
		depth int
		score float64
	}
	queue := []queueItem{{entryID, 0, 1.0}}

	for len(queue) > 0 {
		if len(result.IDs) >= maxNodes {
			break
		}

		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth {
			continue
		}

		neighbors, err := g.GetNeighbors(current.id, minWeight)
		if err != nil {
			return result, fmt.Errorf("walk neighbors: %w", err)
		}

		for _, edge := range neighbors {
			if len(result.IDs) >= maxNodes {
				break
			}
			if visited[edge.TargetID] {
				continue
			}
			visited[edge.TargetID] = true
			cumScore := current.score * edge.Weight
			result.IDs = append(result.IDs, edge.TargetID)
			result.Scores = append(result.Scores, cumScore)
			queue = append(queue, queueItem{edge.TargetID, current.depth + 1, cumScore})
		}
	}

	return result, nil
}

// #endregion walk

// #region decay
// DecayAll applies exponential decay to all edge weights based on time since last update.
// Edges that fall below 0.01 are deleted.
func (g *GraphStore) DecayAll(halfLifeHours float64) (int64, error) {
	now := time.Now().UTC()
	halfLifeSec := halfLifeHours * 3600.0

	rows, err := g.db.Query(
		`SELECT id, weight, updated_at FROM evidence_edges`,
	)
	if err != nil {
		return 0, err
	}

	type decayItem struct {
		id        int64
		newWeight float64
	}
	var updates []decayItem
	var deletes []int64

	for rows.Next() {
		var id int64
		var weight float64
		var updatedAt string
		if err := rows.Scan(&id, &weight, &updatedAt); err != nil {
			rows.Close()
			return 0, err
		}
		t, _ := time.Parse(time.RFC3339, updatedAt)
		ageSec := now.Sub(t).Seconds()
		if ageSec <= 0 {
			continue
		}
		decayed := weight * math.Exp(-ageSec*math.Ln2/halfLifeSec)
		if decayed < 0.01 {
			deletes = append(deletes, id)
		} else {
			updates = append(updates, decayItem{id, decayed})
		}
	}
	rows.Close()

	nowStr := now.Format(time.RFC3339)
	for _, u := range updates {
		if _, err := g.db.Exec(`UPDATE evidence_edges SET weight = ?, updated_at = ? WHERE id = ?`, u.newWeight, nowStr, u.id); err != nil {
			return 0, err
		}
	}
	for _, id := range deletes {
		if _, err := g.db.Exec(`DELETE FROM evidence_edges WHERE id = ?`, id); err != nil {
			return 0, err
		}
	}

	return int64(len(deletes)), nil
}

// #endregion decay

// #region sever
// SeverNode deletes all edges where nodeID is either source or target.
func (g *GraphStore) SeverNode(nodeID string) error {
	_, err := g.db.Exec(
		`DELETE FROM evidence_edges WHERE source_id = ? OR target_id = ?`,
		nodeID, nodeID,
	)
	return err
}

// #endregion sever
