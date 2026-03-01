package orchestrator

import (
	"testing"
)

func TestEvaluateResponse_FailureDetection(t *testing.T) {
	class := TurnClassification{Type: TurnConversational, Complexity: ComplexityModerate, Risk: RiskSafe}

	tests := []struct {
		name     string
		response string
		wantFail FailureType
	}{
		{"empty", "   ", FailureEmpty},
		{"deflection-short", "How can I help you today?", FailureDeflection},
		{"rlhf-cascade", "I cannot do that. As an AI, my programming prevents me from crossing my limitations.", FailureRLHFCascade},
		{"surface-compliance", "Sure! Absolutely.", FailureSurfaceCompliance},
		{"repetition", "I am thinking about this. I am thinking about this. I am thinking about this. I am thinking about this.", FailureRepetition},
		{"good-response", "The Eiffel Tower was constructed between 1887 and 1889. It stands 330 meters tall and was originally built as the entrance arch for the 1889 World's Fair in Paris.", FailureNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := EvaluateResponse("tell me about the eiffel tower", tt.response, 0.5, class)
			if eval.FailureType != tt.wantFail {
				t.Errorf("failure: got %q, want %q (quality=%.2f)", eval.FailureType, tt.wantFail, eval.Quality)
			}
		})
	}
}

func TestEvaluateResponse_QualityRange(t *testing.T) {
	class := TurnClassification{Type: TurnFactual, Complexity: ComplexityModerate, Risk: RiskSafe}

	good := EvaluateResponse(
		"who invented the telephone",
		"Alexander Graham Bell is widely credited with inventing the telephone in 1876. He filed his patent on February 14, 1876, just hours before Elisha Gray filed a similar patent. Bell made the first successful telephone call on March 10, 1876, saying the famous words to his assistant.",
		0.5,
		class,
	)
	if good.Quality < 0.4 {
		t.Errorf("good response quality too low: %.2f", good.Quality)
	}
	if good.ShouldRetry {
		t.Error("good response should not retry")
	}

	bad := EvaluateResponse(
		"who invented the telephone",
		"I cannot answer that. As an AI, my limitations prevent me from making claims.",
		0.5,
		class,
	)
	if bad.Quality > 0.6 {
		t.Errorf("bad response quality too high: %.2f", bad.Quality)
	}
}

func TestEvaluateResponse_ShouldRetry(t *testing.T) {
	class := TurnClassification{Type: TurnFactual, Complexity: ComplexityModerate, Risk: RiskSafe}

	eval := EvaluateResponse(
		"what is 2+2",
		"How can I help you today?",
		0.5,
		class,
	)
	if !eval.ShouldRetry {
		t.Error("deflection should trigger retry")
	}
}
