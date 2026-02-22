package update

import "github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"

// #region update-context
// UpdateContext carries per-turn context into the pure update function.
type UpdateContext struct {
	TurnID      string
	Prompt      string
	ResponseText string
	Entropy     float32
}
// #endregion update-context

// #region signals
// Signals carries derived signals that inform disposition updates.
type Signals struct {
	SentimentScore      float32
	NoveltyScore        float32
	CoherenceScore      float32
	RiskFlag            bool
	UserCorrection      bool // Phase 3: user explicitly corrected prior response
	ToolFailure         bool // Phase 3: tool/verifier reported failure
	ConstraintViolation bool // Phase 3: detected contradiction with constraints

	// DirectionVectors provides semantic delta directions per segment.
	// Keys: "prefs", "goals", "heuristics", "risk".
	// Each slice must match the segment size (32 elements).
	// When present, used instead of sign(existing) for delta direction.
	// Must be L2-normalized before setting.
	DirectionVectors map[string][]float32
}
// #endregion signals

// #region decision
// Decision records what the update function decided.
type Decision struct {
	Action string // "commit" | "reject" | "no_op"
	Reason string
}
// #endregion decision

// #region metrics
// SegmentMetric captures per-segment telemetry from an update cycle.
type SegmentMetric struct {
	Name      string
	DeltaNorm float32
	DecayNorm float32 // L2 norm of decay applied this turn
}

// Metrics captures telemetry from an update cycle.
type Metrics struct {
	DeltaNorm      float32
	SegmentsHit    []string
	SegmentMetrics []SegmentMetric
	UpdateTimeMs   int64
}
// #endregion metrics

// #region update-config
// UpdateConfig holds learning and decay parameters for the update function.
type UpdateConfig struct {
	LearningRate           float32 // magnitude of signal-driven deltas (default 0.01)
	DecayRate              float32 // per-element multiplicative decay (default 0.005)
	MaxDeltaNormPerSegment float32 // L2 clamp per segment (default 1.0)
	MaxStateNorm           float32 // post-update L2 cap on full state vector (0 = disabled)
}

// DefaultUpdateConfig returns sensible defaults for Phase 4.
func DefaultUpdateConfig() UpdateConfig {
	return UpdateConfig{
		LearningRate:           0.01,
		DecayRate:              0.005,
		MaxDeltaNormPerSegment: 1.0,
		MaxStateNorm:           3.0,
	}
}
// #endregion update-config

// #region update-result
// UpdateResult bundles everything returned by Update().
type UpdateResult struct {
	NewState state.StateRecord
	Decision Decision
	Metrics  Metrics
}
// #endregion update-result
