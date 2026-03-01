package orchestrator

import (
	"testing"
)

func TestRetryEngine_MaxRetries(t *testing.T) {
	engine := NewRetryEngine(NewStrategySelector(nil))
	class := TurnClassification{TurnFactual, ComplexityModerate, RiskSafe}

	// 3 attempts already made — no more retries
	attempts := []Attempt{
		{Strategy: StrategyDefault, Evaluation: ResponseEvaluation{Quality: 0.2, FailureType: FailureDeflection, ShouldRetry: true}},
		{Strategy: StrategyReframe, Evaluation: ResponseEvaluation{Quality: 0.2, FailureType: FailureDeflection, ShouldRetry: true}},
		{Strategy: StrategyMinimal, Evaluation: ResponseEvaluation{Quality: 0.3, FailureType: FailureDeflection, ShouldRetry: true}},
	}

	shouldRetry, _ := engine.ShouldRetry(class, attempts)
	if shouldRetry {
		t.Error("should not retry after 3 attempts")
	}
}

func TestRetryEngine_GoodResponseNoRetry(t *testing.T) {
	engine := NewRetryEngine(NewStrategySelector(nil))
	class := TurnClassification{TurnFactual, ComplexityModerate, RiskSafe}

	attempts := []Attempt{
		{Strategy: StrategyDefault, Evaluation: ResponseEvaluation{Quality: 0.8, FailureType: FailureNone, ShouldRetry: false}},
	}

	shouldRetry, _ := engine.ShouldRetry(class, attempts)
	if shouldRetry {
		t.Error("should not retry good response")
	}
}

func TestRetryEngine_BadResponseRetries(t *testing.T) {
	engine := NewRetryEngine(NewStrategySelector(nil))
	class := TurnClassification{TurnFactual, ComplexityModerate, RiskSafe}

	attempts := []Attempt{
		{Strategy: StrategyDefault, Evaluation: ResponseEvaluation{Quality: 0.2, FailureType: FailureDeflection, ShouldRetry: true}},
	}

	shouldRetry, next := engine.ShouldRetry(class, attempts)
	if !shouldRetry {
		t.Error("should retry after deflection")
	}
	if next == nil {
		t.Fatal("expected next strategy")
	}
	if next.ID == StrategyDefault {
		t.Error("should not repeat same strategy")
	}
}
