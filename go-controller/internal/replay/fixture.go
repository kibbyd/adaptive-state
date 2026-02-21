package replay

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/eval"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/gate"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
)

// #region fixture-types

// Fixture is the top-level JSON structure for a replay fixture.
type Fixture struct {
	Description     string                `json:"description"`
	StartState      FixtureStartState     `json:"start_state"`
	Config          FixtureConfig         `json:"config"`
	Interactions    []FixtureInteraction  `json:"interactions"`
	ExpectedResults []FixtureExpectedResult `json:"expected_results"`
}

// FixtureStartState is the JSON-serializable initial state.
type FixtureStartState struct {
	VersionID   string              `json:"version_id"`
	StateVector [128]float32        `json:"state_vector"`
	SegmentMap  state.SegmentMap    `json:"segment_map"`
}

// FixtureSignals mirrors update.Signals with JSON tags.
type FixtureSignals struct {
	SentimentScore      float32 `json:"sentiment_score"`
	NoveltyScore        float32 `json:"novelty_score"`
	CoherenceScore      float32 `json:"coherence_score"`
	RiskFlag            bool    `json:"risk_flag"`
	UserCorrection      bool    `json:"user_correction"`
	ToolFailure         bool    `json:"tool_failure"`
	ConstraintViolation bool    `json:"constraint_violation"`
}

// FixtureInteraction mirrors replay.Interaction with JSON tags.
type FixtureInteraction struct {
	TurnID       string         `json:"turn_id"`
	Prompt       string         `json:"prompt"`
	ResponseText string         `json:"response_text"`
	Entropy      float32        `json:"entropy"`
	Signals      FixtureSignals `json:"signals"`
	Evidence     []string       `json:"evidence"`
}

// FixtureExpectedResult captures the expected action per turn.
type FixtureExpectedResult struct {
	TurnID string `json:"turn_id"`
	Action string `json:"action"`
}

// FixtureConfig bundles all sub-configs for a replay run.
type FixtureConfig struct {
	UpdateConfig FixtureUpdateConfig `json:"update_config"`
	GateConfig   FixtureGateConfig   `json:"gate_config"`
	EvalConfig   FixtureEvalConfig   `json:"eval_config"`
}

// FixtureUpdateConfig mirrors update.UpdateConfig with JSON tags.
type FixtureUpdateConfig struct {
	LearningRate           float32 `json:"learning_rate"`
	DecayRate              float32 `json:"decay_rate"`
	MaxDeltaNormPerSegment float32 `json:"max_delta_norm_per_segment"`
}

// FixtureGateConfig mirrors gate.GateConfig with JSON tags.
type FixtureGateConfig struct {
	MaxDeltaNorm   float32 `json:"max_delta_norm"`
	MaxStateNorm   float32 `json:"max_state_norm"`
	MinEntropyDrop float32 `json:"min_entropy_drop"`
	RiskSegmentCap float32 `json:"risk_segment_cap"`
}

// FixtureEvalConfig mirrors eval.EvalConfig with JSON tags.
type FixtureEvalConfig struct {
	MaxStateNorm    float32 `json:"max_state_norm"`
	MaxSegmentNorm  float32 `json:"max_segment_norm"`
	EntropyBaseline float32 `json:"entropy_baseline"`
}

// #endregion fixture-types

// #region fixture-loader

// LoadFixture reads and parses a JSON fixture file.
func LoadFixture(path string) (*Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture %s: %w", path, err)
	}
	var f Fixture
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse fixture %s: %w", path, err)
	}
	return &f, nil
}

// ToStateRecord converts a FixtureStartState to a domain StateRecord.
func (s *FixtureStartState) ToStateRecord() state.StateRecord {
	return state.StateRecord{
		VersionID:   s.VersionID,
		StateVector: s.StateVector,
		SegmentMap:  s.SegmentMap,
	}
}

// ToInteraction converts a FixtureInteraction to a domain Interaction.
func (fi *FixtureInteraction) ToInteraction() Interaction {
	return Interaction{
		TurnID:       fi.TurnID,
		Prompt:       fi.Prompt,
		ResponseText: fi.ResponseText,
		Entropy:      fi.Entropy,
		Signals: update.Signals{
			SentimentScore:      fi.Signals.SentimentScore,
			NoveltyScore:        fi.Signals.NoveltyScore,
			CoherenceScore:      fi.Signals.CoherenceScore,
			RiskFlag:            fi.Signals.RiskFlag,
			UserCorrection:      fi.Signals.UserCorrection,
			ToolFailure:         fi.Signals.ToolFailure,
			ConstraintViolation: fi.Signals.ConstraintViolation,
		},
		Evidence: fi.Evidence,
	}
}

// ToReplayConfig converts a FixtureConfig to a domain ReplayConfig.
func (fc *FixtureConfig) ToReplayConfig() ReplayConfig {
	return ReplayConfig{
		UpdateConfig: update.UpdateConfig{
			LearningRate:           fc.UpdateConfig.LearningRate,
			DecayRate:              fc.UpdateConfig.DecayRate,
			MaxDeltaNormPerSegment: fc.UpdateConfig.MaxDeltaNormPerSegment,
		},
		GateConfig: gate.GateConfig{
			MaxDeltaNorm:   fc.GateConfig.MaxDeltaNorm,
			MaxStateNorm:   fc.GateConfig.MaxStateNorm,
			MinEntropyDrop: fc.GateConfig.MinEntropyDrop,
			RiskSegmentCap: fc.GateConfig.RiskSegmentCap,
		},
		EvalConfig: eval.EvalConfig{
			MaxStateNorm:    fc.EvalConfig.MaxStateNorm,
			MaxSegmentNorm:  fc.EvalConfig.MaxSegmentNorm,
			EntropyBaseline: fc.EvalConfig.EntropyBaseline,
		},
	}
}

// #endregion fixture-loader
