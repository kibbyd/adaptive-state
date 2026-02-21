package retrieval

import (
	"context"
	"fmt"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/codec"
)

// #region retriever
// Retriever orchestrates triple-gated evidence retrieval.
type Retriever struct {
	codec  *codec.CodecClient
	config RetrievalConfig
}

// NewRetriever creates a Retriever with the given codec client and config.
func NewRetriever(codec *codec.CodecClient, config RetrievalConfig) *Retriever {
	return &Retriever{codec: codec, config: config}
}

// #endregion retriever

// #region retrieve
// Retrieve runs the 3-gate retrieval pipeline:
//  1. Gate 1 — Confidence: skip retrieval if entropy is below threshold
//  2. Gate 2 — Similarity: search with threshold (enforced server-side by ChromaDB)
//  3. Gate 3 — Consistency: validate results (non-empty, reasonable length, no dupes)
func (r *Retriever) Retrieve(ctx context.Context, prompt string, entropy float32) (GateResult, error) {
	result := GateResult{}

	// Gate 1: entropy check (skipped when AlwaysRetrieve is set)
	if !r.config.AlwaysRetrieve && entropy < r.config.EntropyThreshold {
		result.Gate1Passed = false
		result.Reason = fmt.Sprintf("gate1: entropy %.4f < threshold %.4f", entropy, r.config.EntropyThreshold)
		return result, nil
	}
	result.Gate1Passed = true

	// Gate 2: similarity search (threshold enforced server-side)
	searchResults, err := r.codec.Search(ctx, prompt, r.config.TopK, r.config.SimilarityThreshold)
	if err != nil {
		return result, fmt.Errorf("retrieval search: %w", err)
	}

	// Convert codec results to EvidenceRecords
	gate2Results := make([]EvidenceRecord, len(searchResults))
	for i, sr := range searchResults {
		gate2Results[i] = EvidenceRecord{
			ID:           sr.ID,
			Text:         sr.Text,
			Score:        sr.Score,
			MetadataJSON: sr.MetadataJSON,
		}
	}
	result.Gate2Count = len(gate2Results)

	if result.Gate2Count == 0 {
		result.Reason = "gate2: no results above similarity threshold"
		return result, nil
	}

	// Gate 3: consistency check
	gate3Results := r.consistencyCheck(gate2Results)
	result.Gate3Count = len(gate3Results)
	result.Retrieved = gate3Results

	if result.Gate3Count == 0 {
		result.Reason = "gate3: all results failed consistency check"
	} else {
		result.Reason = fmt.Sprintf("retrieved %d evidence items (gate2=%d, gate3=%d)",
			result.Gate3Count, result.Gate2Count, result.Gate3Count)
	}

	return result, nil
}

// #endregion retrieve

// #region consistency-check
// consistencyCheck validates retrieved evidence against basic constraints:
//   - Non-empty text
//   - Text within MaxEvidenceLen
//   - No duplicate IDs
func (r *Retriever) consistencyCheck(results []EvidenceRecord) []EvidenceRecord {
	seen := make(map[string]bool)
	var valid []EvidenceRecord

	for _, rec := range results {
		// Skip empty text
		if rec.Text == "" {
			continue
		}
		// Skip overlong text
		if r.config.MaxEvidenceLen > 0 && len(rec.Text) > r.config.MaxEvidenceLen {
			continue
		}
		// Skip duplicate IDs
		if seen[rec.ID] {
			continue
		}
		seen[rec.ID] = true
		valid = append(valid, rec)
	}

	return valid
}

// #endregion consistency-check
