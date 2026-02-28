package retrieval

import (
	"context"
	"fmt"
	"log"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/codec"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/graph"
)

// #region graph-retriever
// GraphRetriever wraps the base Retriever with graph-walk augmentation.
// It uses the top retrieval result as an entry node, walks the evidence graph,
// and fetches full text for walked nodes via GetByIDs.
type GraphRetriever struct {
	base       *Retriever
	graphStore *graph.GraphStore
	codec      *codec.CodecClient
	maxDepth   int
	minWeight  float64
}

// NewGraphRetriever creates a GraphRetriever wrapping a base retriever.
func NewGraphRetriever(base *Retriever, gs *graph.GraphStore, codec *codec.CodecClient) *GraphRetriever {
	return &GraphRetriever{
		base:       base,
		graphStore: gs,
		codec:      codec,
		maxDepth:   5,
		minWeight:  0.1,
	}
}

// Retrieve runs base retrieval, then augments with graph walk.
// Falls back to base results if walk produces <2 nodes.
func (gr *GraphRetriever) Retrieve(ctx context.Context, prompt string, entropy float32) (GateResult, error) {
	baseResult, err := gr.base.Retrieve(ctx, prompt, entropy)
	if err != nil {
		return baseResult, err
	}

	// No base results — nothing to walk from
	if len(baseResult.Retrieved) == 0 {
		return baseResult, nil
	}

	// Walk from top result
	entryID := baseResult.Retrieved[0].ID
	walkResult, err := gr.graphStore.Walk(entryID, gr.maxDepth, gr.minWeight)
	if err != nil {
		log.Printf("graph walk error (non-fatal, using base): %v", err)
		return baseResult, nil
	}

	// Fallback: walk produced <2 nodes (just entry), use base results
	if len(walkResult.IDs) < 2 {
		return baseResult, nil
	}

	// Collect IDs we don't already have from base results
	baseIDs := make(map[string]EvidenceRecord)
	for _, rec := range baseResult.Retrieved {
		baseIDs[rec.ID] = rec
	}

	var fetchIDs []string
	for _, id := range walkResult.IDs {
		if _, exists := baseIDs[id]; !exists {
			fetchIDs = append(fetchIDs, id)
		}
	}

	// Fetch missing nodes via GetByIDs
	var fetchedRecords map[string]EvidenceRecord
	if len(fetchIDs) > 0 {
		fetched, fetchErr := gr.codec.GetByIDs(ctx, fetchIDs)
		if fetchErr != nil {
			log.Printf("graph GetByIDs error (non-fatal, using base): %v", fetchErr)
			return baseResult, nil
		}
		fetchedRecords = make(map[string]EvidenceRecord, len(fetched))
		for _, sr := range fetched {
			fetchedRecords[sr.ID] = EvidenceRecord{
				ID:           sr.ID,
				Text:         sr.Text,
				Score:        sr.Score,
				MetadataJSON: sr.MetadataJSON,
			}
		}
	}

	// Build ordered result following walk path
	var graphRetrieved []EvidenceRecord
	for i, id := range walkResult.IDs {
		if rec, ok := baseIDs[id]; ok {
			rec.Score = float32(walkResult.Scores[i]) // use walk score
			graphRetrieved = append(graphRetrieved, rec)
		} else if rec, ok := fetchedRecords[id]; ok {
			rec.Score = float32(walkResult.Scores[i])
			graphRetrieved = append(graphRetrieved, rec)
		}
		// Skip IDs that weren't found (deleted evidence)
	}

	if len(graphRetrieved) < 2 {
		// Walk resolved to <2 usable nodes, fall back
		return baseResult, nil
	}

	return GateResult{
		Gate1Passed: baseResult.Gate1Passed,
		Gate2Count:  baseResult.Gate2Count,
		Gate3Count:  len(graphRetrieved),
		Retrieved:   graphRetrieved,
		Reason: fmt.Sprintf("graph walk: %d nodes from entry %s (base=%d, walked=%d)",
			len(graphRetrieved), entryID[:8], len(baseResult.Retrieved), len(walkResult.IDs)),
	}, nil
}

// #endregion graph-retriever
