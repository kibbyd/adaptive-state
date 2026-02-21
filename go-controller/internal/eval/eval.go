package eval

import (
	"fmt"
	"math"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
)

// #region eval-harness
// EvalHarness runs lightweight post-commit validation on state.
type EvalHarness struct {
	config EvalConfig
}

// NewEvalHarness creates an eval harness with the given configuration.
func NewEvalHarness(config EvalConfig) *EvalHarness {
	return &EvalHarness{config: config}
}

// Run performs lightweight post-commit validation on the new state.
// Returns pass/fail with metrics. Single-response, no extra Generate calls.
func (h *EvalHarness) Run(newState state.StateRecord, entropy float32) EvalResult {
	var metrics []EvalMetric
	passed := true
	var failReasons []string

	// 1. State norm bounds: L2 norm of full 128-dim vector
	stateNorm := fullVectorNorm(newState.StateVector)
	stateNormPass := stateNorm <= h.config.MaxStateNorm
	metrics = append(metrics, EvalMetric{
		Name:  "state_norm",
		Value: stateNorm,
		Pass:  stateNormPass,
	})
	if !stateNormPass {
		passed = false
		failReasons = append(failReasons, fmt.Sprintf("state norm %.4f exceeds %.4f", stateNorm, h.config.MaxStateNorm))
	}

	// 2. Segment norm bounds: each of the 4 segments
	segments := []struct {
		name string
		seg  [2]int
	}{
		{"prefs", newState.SegmentMap.Prefs},
		{"goals", newState.SegmentMap.Goals},
		{"heuristics", newState.SegmentMap.Heuristics},
		{"risk", newState.SegmentMap.Risk},
	}

	for _, s := range segments {
		norm := segNorm(newState.StateVector, s.seg)
		segPass := norm <= h.config.MaxSegmentNorm
		metrics = append(metrics, EvalMetric{
			Name:  fmt.Sprintf("segment_%s_norm", s.name),
			Value: norm,
			Pass:  segPass,
		})
		if !segPass {
			passed = false
			failReasons = append(failReasons, fmt.Sprintf("%s segment norm %.4f exceeds %.4f", s.name, norm, h.config.MaxSegmentNorm))
		}
	}

	// 3. Entropy check: informational, not blocking in Phase 3
	entropyPass := entropy <= h.config.EntropyBaseline
	metrics = append(metrics, EvalMetric{
		Name:  "entropy",
		Value: entropy,
		Pass:  entropyPass,
	})
	// Note: entropy check is informational only in Phase 3, does not fail

	reason := "all checks passed"
	if !passed {
		reason = fmt.Sprintf("eval failed: %s", failReasons[0])
		if len(failReasons) > 1 {
			reason = fmt.Sprintf("eval failed: %d checks: %s", len(failReasons), failReasons[0])
		}
	}

	return EvalResult{
		Passed:  passed,
		Metrics: metrics,
		Reason:  reason,
	}
}

// #endregion eval-harness

// #region helpers
// fullVectorNorm computes the L2 norm of a 128-dim vector.
func fullVectorNorm(v [128]float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

// segNorm computes the L2 norm of a segment slice.
func segNorm(v [128]float32, seg [2]int) float32 {
	var sum float64
	for i := seg[0]; i < seg[1]; i++ {
		sum += float64(v[i]) * float64(v[i])
	}
	return float32(math.Sqrt(sum))
}

// #endregion helpers
