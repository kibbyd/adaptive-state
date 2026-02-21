package gate

// #region veto-type
// VetoType enumerates hard veto categories.
type VetoType string

const (
	VetoUserCorrection VetoType = "user_correction"
	VetoToolFailure    VetoType = "tool_failure"
	VetoConstraint     VetoType = "constraint_violation"
	VetoSafety         VetoType = "safety_violation"
)

// #endregion veto-type

// #region veto-signal
// VetoSignal represents a detected hard veto condition.
type VetoSignal struct {
	Type   VetoType
	Reason string
}

// #endregion veto-signal

// #region gate-config
// GateConfig holds thresholds for gate decisions.
type GateConfig struct {
	MaxDeltaNorm   float32 // max L2 norm of state delta per segment
	MaxStateNorm   float32 // max L2 norm of entire state vector
	MinEntropyDrop float32 // soft: prefer updates that reduce entropy
	RiskSegmentCap float32 // hard cap on risk segment norm
}

// DefaultGateConfig returns sensible defaults for Phase 3.
func DefaultGateConfig() GateConfig {
	return GateConfig{
		MaxDeltaNorm:   5.0,
		MaxStateNorm:   50.0,
		MinEntropyDrop: 0.1,
		RiskSegmentCap: 10.0,
	}
}

// #endregion gate-config

// #region gate-decision
// GateDecision is the output of the gate evaluation.
type GateDecision struct {
	Action      string       // "commit" | "reject"
	Reason      string
	Vetoed      bool
	VetoSignals []VetoSignal // non-empty if vetoed
	SoftScore   float32      // 0-1 composite of soft signals (for logging)
}

// #endregion gate-decision
