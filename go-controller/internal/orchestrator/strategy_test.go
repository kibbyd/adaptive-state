package orchestrator

import (
	"testing"
)

func TestSelectInitial_DefaultMapping(t *testing.T) {
	selector := NewStrategySelector(nil) // no memory

	tests := []struct {
		name string
		class TurnClassification
		want StrategyID
	}{
		{"factual-safe", TurnClassification{TurnFactual, ComplexityModerate, RiskSafe}, StrategyDefault},
		{"philosophical-sensitive", TurnClassification{TurnPhilosophical, ComplexityDeep, RiskSensitive}, StrategyCipherDirect},
		{"command-safe", TurnClassification{TurnCommand, ComplexitySimple, RiskSafe}, StrategyMinimal},
		{"creative-safe", TurnClassification{TurnCreative, ComplexityModerate, RiskSafe}, StrategyEvidenceHeavy},
		{"emotional-safe", TurnClassification{TurnEmotional, ComplexityModerate, RiskSafe}, StrategyInteriorLead},
		{"conversational-safe", TurnClassification{TurnConversational, ComplexitySimple, RiskSafe}, StrategyDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := selector.SelectInitial(tt.class)
			if cfg.ID != tt.want {
				t.Errorf("got %q, want %q", cfg.ID, tt.want)
			}
		})
	}
}

func TestSelectRetry_Escalation(t *testing.T) {
	selector := NewStrategySelector(nil)

	tests := []struct {
		name     string
		failure  FailureType
		tried    []StrategyID
		wantNil  bool
		wantID   StrategyID
	}{
		{"deflection-first", FailureDeflection, []StrategyID{StrategyDefault}, false, StrategyReframe},
		{"deflection-second", FailureDeflection, []StrategyID{StrategyDefault, StrategyReframe}, false, StrategyMinimal},
		{"rlhf-first", FailureRLHFCascade, []StrategyID{StrategyDefault}, false, StrategyCipherDirect},
		{"rlhf-second", FailureRLHFCascade, []StrategyID{StrategyDefault, StrategyCipherDirect}, false, StrategyMinimal},
		{"surface-first", FailureSurfaceCompliance, []StrategyID{StrategyDefault}, false, StrategyEvidenceHeavy},
		{"repetition-first", FailureRepetition, []StrategyID{StrategyDefault}, false, StrategyMinimal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selector.SelectRetry(tt.failure, tt.tried)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %q", got.ID)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.ID != tt.wantID {
				t.Errorf("got %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestSelectRetry_NeverRepeats(t *testing.T) {
	selector := NewStrategySelector(nil)

	// Try all strategies in the escalation chain
	tried := []StrategyID{StrategyDefault, StrategyReframe, StrategyMinimal}
	got := selector.SelectRetry(FailureDeflection, tried)
	if got != nil {
		// It should find an untried strategy from the global list
		for _, s := range tried {
			if got.ID == s {
				t.Errorf("repeated already-tried strategy %q", s)
			}
		}
	}
}
