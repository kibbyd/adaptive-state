package replay

import (
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/eval"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/gate"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
)

// #region types
// Interaction represents a single recorded turn for replay.
type Interaction struct {
	TurnID       string
	Prompt       string
	ResponseText string
	Entropy      float32
	Signals      update.Signals
	Evidence     []string
}

// ReplayConfig bundles update, gate, and eval configs for a replay run.
type ReplayConfig struct {
	UpdateConfig update.UpdateConfig
	GateConfig   gate.GateConfig
	EvalConfig   eval.EvalConfig
}

// DefaultReplayConfig returns sensible defaults for all three pipeline stages.
func DefaultReplayConfig() ReplayConfig {
	return ReplayConfig{
		UpdateConfig: update.DefaultUpdateConfig(),
		GateConfig:   gate.DefaultGateConfig(),
		EvalConfig:   eval.DefaultEvalConfig(),
	}
}

// ReplayResult captures the outcome of replaying one interaction through the full pipeline.
type ReplayResult struct {
	TurnID string
	Action string // "commit" | "gate_reject" | "eval_rollback" | "no_op"
	Reason string

	// Update stage
	UpdateDecision update.Decision
	UpdateMetrics  update.Metrics

	// Gate stage (nil if update was no_op)
	GateDecision *gate.GateDecision

	// Eval stage (nil if gate rejected or update was no_op)
	EvalResult *eval.EvalResult

	// Final state after this turn (may equal previous if rejected/rolled back)
	FinalVersionID string
}

// ReplaySummary provides aggregate stats from a replay run.
type ReplaySummary struct {
	TotalTurns    int
	Commits       int
	GateRejects   int
	EvalRollbacks int
	NoOps         int
	FinalState    state.StateRecord
}

// #endregion types

// #region replay
// Replay iterates through interactions, applying the full pipeline per turn:
// update → gate → eval → commit/reject. Operates entirely in-memory.
func Replay(startState state.StateRecord, interactions []Interaction, config ReplayConfig) []ReplayResult {
	current := startState
	results := make([]ReplayResult, 0, len(interactions))

	gateInst := gate.NewGate(config.GateConfig)
	evalInst := eval.NewEvalHarness(config.EvalConfig)

	for _, inter := range interactions {
		ctx := update.UpdateContext{
			TurnID:       inter.TurnID,
			Prompt:       inter.Prompt,
			ResponseText: inter.ResponseText,
			Entropy:      inter.Entropy,
		}

		// 1. Update
		updateResult := update.Update(current, ctx, inter.Signals, inter.Evidence, config.UpdateConfig)

		// 2. No-op check
		if updateResult.Decision.Action == "no_op" {
			results = append(results, ReplayResult{
				TurnID:         inter.TurnID,
				Action:         "no_op",
				Reason:         updateResult.Decision.Reason,
				UpdateDecision: updateResult.Decision,
				UpdateMetrics:  updateResult.Metrics,
				FinalVersionID: current.VersionID,
			})
			continue
		}

		// 3. Gate
		gateDecision := gateInst.Evaluate(current, updateResult.NewState, inter.Signals, updateResult.Metrics, inter.Entropy)
		if gateDecision.Action == "reject" {
			results = append(results, ReplayResult{
				TurnID:         inter.TurnID,
				Action:         "gate_reject",
				Reason:         gateDecision.Reason,
				UpdateDecision: updateResult.Decision,
				UpdateMetrics:  updateResult.Metrics,
				GateDecision:   &gateDecision,
				FinalVersionID: current.VersionID,
			})
			continue
		}

		// 4. Eval
		evalResult := evalInst.Run(updateResult.NewState, inter.Entropy)
		if !evalResult.Passed {
			results = append(results, ReplayResult{
				TurnID:         inter.TurnID,
				Action:         "eval_rollback",
				Reason:         evalResult.Reason,
				UpdateDecision: updateResult.Decision,
				UpdateMetrics:  updateResult.Metrics,
				GateDecision:   &gateDecision,
				EvalResult:     &evalResult,
				FinalVersionID: current.VersionID,
			})
			continue
		}

		// 5. Commit — advance current state
		current = updateResult.NewState
		results = append(results, ReplayResult{
			TurnID:         inter.TurnID,
			Action:         "commit",
			Reason:         gateDecision.Reason,
			UpdateDecision: updateResult.Decision,
			UpdateMetrics:  updateResult.Metrics,
			GateDecision:   &gateDecision,
			EvalResult:     &evalResult,
			FinalVersionID: current.VersionID,
		})
	}

	return results
}

// Summarize computes aggregate stats from replay results.
func Summarize(results []ReplayResult, finalState state.StateRecord) ReplaySummary {
	s := ReplaySummary{
		TotalTurns: len(results),
		FinalState: finalState,
	}
	for _, r := range results {
		switch r.Action {
		case "commit":
			s.Commits++
		case "gate_reject":
			s.GateRejects++
		case "eval_rollback":
			s.EvalRollbacks++
		case "no_op":
			s.NoOps++
		}
	}
	return s
}

// #endregion replay
