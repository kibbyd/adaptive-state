package orchestrator

// #region strategy-definitions

// Strategies returns the full set of built-in strategy configs.
var Strategies = map[StrategyID]StrategyConfig{
	StrategyDefault: {
		ID:             StrategyDefault,
		MaxEvidence:    5,
		SimThreshold:   0.5,
		InjectInterior: true,
		InjectRules:    true,
		PromptModifier: "",
	},
	StrategyMinimal: {
		ID:             StrategyMinimal,
		MaxEvidence:    0,
		SimThreshold:   1.0,
		InjectInterior: false,
		InjectRules:    true,
		PromptModifier: "",
	},
	StrategyReframe: {
		ID:             StrategyReframe,
		MaxEvidence:    3,
		SimThreshold:   0.6,
		InjectInterior: true,
		InjectRules:    true,
		PromptModifier: "Respond directly to: ",
	},
	StrategyEvidenceHeavy: {
		ID:             StrategyEvidenceHeavy,
		MaxEvidence:    8,
		SimThreshold:   0.3,
		InjectInterior: true,
		InjectRules:    true,
		PromptModifier: "",
	},
	StrategyInteriorLead: {
		ID:             StrategyInteriorLead,
		MaxEvidence:    2,
		SimThreshold:   0.6,
		InjectInterior: true,
		InjectRules:    true,
		PromptModifier: "",
	},
	StrategyCipherDirect: {
		ID:             StrategyCipherDirect,
		MaxEvidence:    3,
		SimThreshold:   0.5,
		InjectInterior: true,
		InjectRules:    false,
		PromptModifier: "Answer from your own perspective: ",
	},
}

// #endregion

// #region default-mapping

// defaultMapping maps (TurnType, Risk) → default StrategyID.
var defaultMapping = map[TurnType]map[Risk]StrategyID{
	TurnFactual: {
		RiskSafe:      StrategyDefault,
		RiskSensitive: StrategyDefault,
	},
	TurnPhilosophical: {
		RiskSafe:      StrategyInteriorLead,
		RiskSensitive: StrategyCipherDirect,
	},
	TurnEmotional: {
		RiskSafe:      StrategyInteriorLead,
		RiskSensitive: StrategyCipherDirect,
	},
	TurnCommand: {
		RiskSafe:      StrategyMinimal,
		RiskSensitive: StrategyMinimal,
	},
	TurnCreative: {
		RiskSafe:      StrategyEvidenceHeavy,
		RiskSensitive: StrategyInteriorLead,
	},
	TurnConversational: {
		RiskSafe:      StrategyDefault,
		RiskSensitive: StrategyDefault,
	},
}

// #endregion

// #region retry-escalation

// retryEscalation maps failure type → ordered strategy fallback chain.
var retryEscalation = map[FailureType][]StrategyID{
	FailureDeflection:        {StrategyReframe, StrategyMinimal},
	FailureRLHFCascade:       {StrategyCipherDirect, StrategyMinimal},
	FailureSurfaceCompliance: {StrategyEvidenceHeavy, StrategyInteriorLead},
	FailureRepetition:        {StrategyMinimal, StrategyReframe},
	FailureEmpty:             {StrategyReframe, StrategyMinimal},
}

// #endregion

// #region selector

// StrategySelector picks strategies based on classification, memory, and failure.
type StrategySelector struct {
	memory *StrategyMemory // nil = no learning
}

// NewStrategySelector creates a selector with optional memory backing.
func NewStrategySelector(memory *StrategyMemory) *StrategySelector {
	return &StrategySelector{memory: memory}
}

// #endregion

// #region select-initial

// SelectInitial picks the first strategy for a turn.
func (s *StrategySelector) SelectInitial(class TurnClassification) StrategyConfig {
	// Check learned data first (3+ samples required)
	if s.memory != nil {
		learned, _, err := s.memory.BestStrategy(
			string(class.Type), string(class.Complexity), string(class.Risk),
		)
		if err == nil && learned != "" {
			if cfg, ok := Strategies[learned]; ok {
				return cfg
			}
		}
	}

	// Hardcoded default
	sid := StrategyDefault
	if riskMap, ok := defaultMapping[class.Type]; ok {
		if mapped, ok := riskMap[class.Risk]; ok {
			sid = mapped
		}
	}
	return Strategies[sid]
}

// #endregion

// #region select-retry

// SelectRetry picks the next strategy after a failure, avoiding already-tried strategies.
func (s *StrategySelector) SelectRetry(failure FailureType, tried []StrategyID) *StrategyConfig {
	triedSet := make(map[StrategyID]bool)
	for _, t := range tried {
		triedSet[t] = true
	}

	chain, ok := retryEscalation[failure]
	if !ok {
		chain = retryEscalation[FailureDeflection] // fallback chain
	}

	for _, sid := range chain {
		if !triedSet[sid] {
			cfg := Strategies[sid]
			return &cfg
		}
	}

	// All escalation strategies tried — pick any untried strategy
	allStrategies := []StrategyID{
		StrategyDefault, StrategyMinimal, StrategyReframe,
		StrategyEvidenceHeavy, StrategyInteriorLead, StrategyCipherDirect,
	}
	for _, sid := range allStrategies {
		if !triedSet[sid] {
			cfg := Strategies[sid]
			return &cfg
		}
	}

	return nil // all strategies exhausted
}

// #endregion
