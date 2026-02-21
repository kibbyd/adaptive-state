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
	SentimentScore float32
	NoveltyScore   float32
	CoherenceScore float32
	RiskFlag       bool
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
// Metrics captures telemetry from an update cycle.
type Metrics struct {
	DeltaNorm    float32
	SegmentsHit  []string
	UpdateTimeMs int64
}
// #endregion metrics

// #region update-result
// UpdateResult bundles everything returned by Update().
type UpdateResult struct {
	NewState state.StateRecord
	Decision Decision
	Metrics  Metrics
}
// #endregion update-result
