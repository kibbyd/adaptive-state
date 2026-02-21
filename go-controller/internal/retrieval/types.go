package retrieval

// #region config
// RetrievalConfig holds thresholds and limits for the 3-gate retrieval pipeline.
type RetrievalConfig struct {
	EntropyThreshold    float32 // Gate 1: min entropy to trigger retrieval
	SimilarityThreshold float32 // Gate 2: min cosine similarity
	TopK                int     // Max results from vector search
	MaxEvidenceLen      int     // Max chars per evidence string
}

// DefaultConfig returns sensible defaults for retrieval gating.
func DefaultConfig() RetrievalConfig {
	return RetrievalConfig{
		EntropyThreshold:    0.5,
		SimilarityThreshold: 0.3,
		TopK:                5,
		MaxEvidenceLen:       2000,
	}
}

// #endregion config

// #region evidence-record
// EvidenceRecord represents a single piece of retrieved evidence.
type EvidenceRecord struct {
	ID           string
	Text         string
	Score        float32
	MetadataJSON string
}

// #endregion evidence-record

// #region gate-result
// GateResult captures the outcome of the 3-gate retrieval pipeline.
type GateResult struct {
	Gate1Passed bool             // entropy check passed
	Gate2Count  int              // results above similarity threshold
	Gate3Count  int              // results passing consistency check
	Retrieved   []EvidenceRecord // final evidence after all gates
	Reason      string           // human-readable explanation
}

// #endregion gate-result
