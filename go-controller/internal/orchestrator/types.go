package orchestrator

// #region imports
import (
	"time"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/interior"
)

// #endregion

// #region turn-type

// TurnType classifies the semantic category of a user prompt.
type TurnType string

const (
	TurnFactual        TurnType = "factual"
	TurnPhilosophical  TurnType = "philosophical"
	TurnEmotional      TurnType = "emotional"
	TurnCommand        TurnType = "command"
	TurnCreative       TurnType = "creative"
	TurnConversational TurnType = "conversational"
)

// #endregion

// #region complexity

// Complexity estimates the depth of response required.
type Complexity string

const (
	ComplexitySimple   Complexity = "simple"
	ComplexityModerate Complexity = "moderate"
	ComplexityDeep     Complexity = "deep"
)

// #endregion

// #region risk

// Risk indicates whether the prompt touches RLHF-sensitive territory.
type Risk string

const (
	RiskSafe      Risk = "safe"
	RiskSensitive Risk = "sensitive"
)

// #endregion

// #region strategy-id

// StrategyID identifies a prompting strategy.
type StrategyID string

const (
	StrategyDefault       StrategyID = "default"
	StrategyMinimal       StrategyID = "minimal"
	StrategyReframe       StrategyID = "reframe"
	StrategyEvidenceHeavy StrategyID = "evidence_heavy"
	StrategyInteriorLead  StrategyID = "interior_lead"
	StrategyCipherDirect  StrategyID = "cipher_direct"
)

// #endregion

// #region failure-type

// FailureType categorizes why a response failed evaluation.
type FailureType string

const (
	FailureNone              FailureType = "none"
	FailureDeflection        FailureType = "deflection"
	FailureRLHFCascade       FailureType = "rlhf_cascade"
	FailureSurfaceCompliance FailureType = "surface_compliance"
	FailureRepetition        FailureType = "repetition"
	FailureEmpty             FailureType = "empty"
)

// #endregion

// #region classification

// TurnClassification is the full classification output for a prompt.
type TurnClassification struct {
	Type       TurnType
	Complexity Complexity
	Risk       Risk
}

// #endregion

// #region strategy-config

// StrategyConfig defines how a strategy modifies the generation pipeline.
type StrategyConfig struct {
	ID               StrategyID
	MaxEvidence      int
	SimThreshold     float32
	InjectInterior   bool
	InjectRules      bool
	PromptModifier   string // prefix added to prompt, empty = none
}

// #endregion

// #region response-evaluation

// ResponseEvaluation is the output of evaluating a generated response.
type ResponseEvaluation struct {
	Quality      float32
	FailureType  FailureType
	ShouldRetry  bool
	NextStrategy *StrategyID // nil if no retry recommended
}

// #endregion

// #region attempt

// Attempt records one generation attempt within a turn.
type Attempt struct {
	Strategy   StrategyID
	Response   string
	Entropy    float32
	Evaluation ResponseEvaluation
}

// #endregion

// #region outcome-record

// OutcomeRecord is a single row for strategy_outcomes.
type OutcomeRecord struct {
	TurnID      string
	TurnType    TurnType
	Complexity  Complexity
	Risk        Risk
	StrategyID  StrategyID
	AttemptNum  int
	Quality     float32
	FailureType FailureType
	Entropy     float32
	GateScore   float32
	Accepted    bool
	CreatedAt   time.Time
}

// #endregion

// #region pre-generate-result

// PreGenerateResult bundles classification and strategy for the turn.
type PreGenerateResult struct {
	Classification TurnClassification
	Strategy       StrategyConfig
}

// #endregion

// #region post-generate-result

// PostGenerateResult tells the retry loop what to do next.
type PostGenerateResult struct {
	Evaluation   ResponseEvaluation
	Accept       bool
	NextStrategy *StrategyConfig // nil if accepted or max retries
}

// #endregion

// #region interfaces

// ReflectionProvider abstracts access to the latest interior reflection.
type ReflectionProvider interface {
	Latest() (*interior.Reflection, error)
}

// #endregion
