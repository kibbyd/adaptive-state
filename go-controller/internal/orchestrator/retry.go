package orchestrator

// #region constants

const maxRetries = 2 // max 2 retries = 3 total attempts

// #endregion

// #region engine

// RetryEngine decides whether to retry and with which strategy.
type RetryEngine struct {
	selector *StrategySelector
}

// NewRetryEngine creates a retry engine backed by the given selector.
func NewRetryEngine(selector *StrategySelector) *RetryEngine {
	return &RetryEngine{selector: selector}
}

// #endregion

// #region should-retry

// ShouldRetry returns whether to retry and the next strategy to use.
// attempts contains all attempts so far (including the one just evaluated).
func (r *RetryEngine) ShouldRetry(class TurnClassification, attempts []Attempt) (bool, *StrategyConfig) {
	if len(attempts) == 0 {
		return false, nil
	}

	// Max retries reached
	if len(attempts) > maxRetries {
		return false, nil
	}

	latest := attempts[len(attempts)-1]
	if !latest.Evaluation.ShouldRetry {
		return false, nil
	}

	// Collect tried strategies
	tried := make([]StrategyID, len(attempts))
	for i, a := range attempts {
		tried[i] = a.Strategy
	}

	next := r.selector.SelectRetry(latest.Evaluation.FailureType, tried)
	if next == nil {
		return false, nil
	}

	return true, next
}

// #endregion
