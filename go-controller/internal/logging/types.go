package logging

import "time"

// #region provenance-entry
// ProvenanceEntry is a single row in the provenance_log table.
type ProvenanceEntry struct {
	VersionID    string
	ContextHash  string
	TriggerType  string
	SignalsJSON  string
	EvidenceRefs string
	Decision     string // "commit" | "reject" | "no_op"
	Reason       string
	CreatedAt    time.Time
}
// #endregion provenance-entry

// #region gate-record
// GateRecord captures the complete gate evaluation inputs for a single turn.
// Serialized as JSON into provenance_log.signals_json for deterministic replay.
type GateRecord struct {
	TurnID  string  `json:"turn_id"`
	Prompt  string  `json:"prompt"`
	Response string `json:"response"`
	Entropy float32 `json:"entropy"`

	// Exact signals as evaluated at runtime
	Signals GateRecordSignals `json:"signals"`

	// Update metrics
	DeltaNorm float32  `json:"delta_norm"`
	SegmentsHit []string `json:"segments_hit"`

	// Gate thresholds active at decision time
	Thresholds GateRecordThresholds `json:"thresholds"`

	// Direction vector metadata (for replay interpretability)
	DirectionSource  string   `json:"direction_source,omitempty"`  // "embedding" | "" (sign fallback)
	DirectionSegments []string `json:"direction_segments,omitempty"` // which segments used embedding direction

	// Gate output
	GateAction  string  `json:"gate_action"`
	GateSoftScore float32 `json:"gate_soft_score"`
	GateVetoed  bool    `json:"gate_vetoed"`
	GateReason  string  `json:"gate_reason"`
}

// GateRecordSignals captures the exact signal values that fed the gate.
type GateRecordSignals struct {
	SentimentScore      float32 `json:"sentiment_score"`
	CoherenceScore      float32 `json:"coherence_score"`
	NoveltyScore        float32 `json:"novelty_score"`
	RiskFlag            bool    `json:"risk_flag"`
	UserCorrection      bool    `json:"user_correction"`
	ToolFailure         bool    `json:"tool_failure"`
	ConstraintViolation bool    `json:"constraint_violation"`
}

// GateRecordThresholds captures the gate/eval config active at decision time.
type GateRecordThresholds struct {
	MaxDeltaNorm   float32 `json:"max_delta_norm"`
	MaxStateNorm   float32 `json:"max_state_norm"`
	RiskSegmentCap float32 `json:"risk_segment_cap"`
	MaxSegmentNorm float32 `json:"max_segment_norm"`
}
// #endregion gate-record
