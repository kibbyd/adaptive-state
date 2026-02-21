package gate

import (
	"fmt"
	"math"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
)

// #region gate
// Gate evaluates whether a proposed state update should be committed or rejected.
type Gate struct {
	config GateConfig
}

// NewGate creates a gate with the given configuration.
func NewGate(config GateConfig) *Gate {
	return &Gate{config: config}
}

// Evaluate checks hard vetoes first, then scores soft signals.
// Takes the old state, proposed new state, context signals, update metrics, and entropy.
func (g *Gate) Evaluate(
	old state.StateRecord,
	proposed state.StateRecord,
	signals update.Signals,
	metrics update.Metrics,
	entropy float32,
) GateDecision {
	var vetoes []VetoSignal

	// --- Hard veto pass ---

	// 1. Safety: RiskFlag from signals
	if signals.RiskFlag {
		vetoes = append(vetoes, VetoSignal{
			Type:   VetoSafety,
			Reason: "risk flag set in signals",
		})
	}

	// 2. User correction contradicts update
	if signals.UserCorrection {
		vetoes = append(vetoes, VetoSignal{
			Type:   VetoUserCorrection,
			Reason: "user explicitly corrected prior response",
		})
	}

	// 3. Tool/verifier failure
	if signals.ToolFailure {
		vetoes = append(vetoes, VetoSignal{
			Type:   VetoToolFailure,
			Reason: "tool or verifier reported failure",
		})
	}

	// 4. Constraint violation
	if signals.ConstraintViolation {
		vetoes = append(vetoes, VetoSignal{
			Type:   VetoConstraint,
			Reason: "detected contradiction with constraints",
		})
	}

	// 5. Delta norm exceeds cap
	deltaNorm := vectorNorm(vectorDelta(old.StateVector, proposed.StateVector))
	if deltaNorm > g.config.MaxDeltaNorm {
		vetoes = append(vetoes, VetoSignal{
			Type:   VetoConstraint,
			Reason: fmt.Sprintf("delta norm %.4f exceeds cap %.4f", deltaNorm, g.config.MaxDeltaNorm),
		})
	}

	// 6. Risk segment norm exceeds cap
	riskNorm := segmentNorm(proposed.StateVector, proposed.SegmentMap.Risk)
	if riskNorm > g.config.RiskSegmentCap {
		vetoes = append(vetoes, VetoSignal{
			Type:   VetoSafety,
			Reason: fmt.Sprintf("risk segment norm %.4f exceeds cap %.4f", riskNorm, g.config.RiskSegmentCap),
		})
	}

	// If any hard vetoes, reject immediately
	if len(vetoes) > 0 {
		return GateDecision{
			Action:      "reject",
			Reason:      fmt.Sprintf("hard veto: %s", vetoes[0].Reason),
			Vetoed:      true,
			VetoSignals: vetoes,
			SoftScore:   0,
		}
	}

	// --- Soft scoring ---
	softScore := computeSoftScore(old, proposed, metrics, entropy, g.config.MinEntropyDrop)

	return GateDecision{
		Action:      "commit",
		Reason:      fmt.Sprintf("passed gate: soft_score=%.4f", softScore),
		Vetoed:      false,
		VetoSignals: nil,
		SoftScore:   softScore,
	}
}

// #endregion gate

// #region helpers
// vectorDelta computes proposed - old element-wise.
func vectorDelta(old, proposed [128]float32) [128]float32 {
	var delta [128]float32
	for i := range delta {
		delta[i] = proposed[i] - old[i]
	}
	return delta
}

// vectorNorm computes the L2 norm of a 128-dim vector.
func vectorNorm(v [128]float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

// segmentNorm computes the L2 norm of a segment slice of the state vector.
func segmentNorm(v [128]float32, seg [2]int) float32 {
	var sum float64
	for i := seg[0]; i < seg[1]; i++ {
		sum += float64(v[i]) * float64(v[i])
	}
	return float32(math.Sqrt(sum))
}

// computeSoftScore produces a 0-1 composite from entropy drop, delta stability,
// and segments hit count. Logged but does not block in Phase 3.
func computeSoftScore(
	old state.StateRecord,
	proposed state.StateRecord,
	metrics update.Metrics,
	entropy float32,
	minEntropyDrop float32,
) float32 {
	var score float32

	// Entropy component: reward entropy drop (weight 0.4)
	oldNorm := vectorNorm(old.StateVector)
	if oldNorm > 0 {
		// Use entropy as proxy — lower entropy after update is better
		if entropy < 1.0 {
			score += 0.4 * (1.0 - entropy)
		}
	} else {
		score += 0.2 // neutral when no prior state
	}

	// Delta stability component: smaller deltas are more stable (weight 0.3)
	deltaNorm := metrics.DeltaNorm
	if deltaNorm == 0 {
		score += 0.3 // no change = perfectly stable
	} else if deltaNorm < 1.0 {
		score += 0.3 * (1.0 - deltaNorm)
	}

	// Segments hit component: fewer segments changed = more focused (weight 0.3)
	hitCount := len(metrics.SegmentsHit)
	switch {
	case hitCount == 0:
		score += 0.3
	case hitCount == 1:
		score += 0.2
	case hitCount == 2:
		score += 0.1
	}

	return score
}

// #endregion helpers
