package eval

// #region eval-config
// EvalConfig holds thresholds for post-commit validation.
type EvalConfig struct {
	MaxStateNorm   float32 // reject if state norm exceeds this
	MaxSegmentNorm float32 // reject if any segment norm exceeds this
	EntropyBaseline float32 // warn if entropy rises above baseline
}

// DefaultEvalConfig returns sensible defaults for Phase 3.
func DefaultEvalConfig() EvalConfig {
	return EvalConfig{
		MaxStateNorm:    50.0,
		MaxSegmentNorm:  15.0,
		EntropyBaseline: 2.0,
	}
}

// #endregion eval-config

// #region eval-metric
// EvalMetric captures a single validation check result.
type EvalMetric struct {
	Name  string
	Value float32
	Pass  bool
}

// #endregion eval-metric

// #region eval-result
// EvalResult is the output of post-commit validation.
type EvalResult struct {
	Passed  bool
	Metrics []EvalMetric
	Reason  string
}

// #endregion eval-result
