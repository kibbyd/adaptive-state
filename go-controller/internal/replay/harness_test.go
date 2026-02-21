package replay

import (
	"testing"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
)

// helper: zero state with default segment map and a version ID.
func zeroState(versionID string) state.StateRecord {
	return state.StateRecord{
		VersionID:   versionID,
		StateVector: [128]float32{},
		SegmentMap:  state.DefaultSegmentMap(),
	}
}

// helper: state with nonzero values in prefs segment.
func seededState(versionID string, val float32) state.StateRecord {
	s := zeroState(versionID)
	for i := 0; i < 32; i++ {
		s.StateVector[i] = val
	}
	return s
}

// helper: interaction with positive signals that will produce a delta.
func commitInteraction(turnID string) Interaction {
	return Interaction{
		TurnID:       turnID,
		Prompt:       "test prompt",
		ResponseText: "test response",
		Entropy:      0.5,
		Signals: update.Signals{
			SentimentScore: 0.8,
			CoherenceScore: 0.6,
			NoveltyScore:   0.4,
		},
		Evidence: []string{"e1"},
	}
}

// 1. Full commit path: nonzero signals + valid state → action="commit", state advances.
func TestReplay_FullCommitPath(t *testing.T) {
	start := seededState("v0", 0.1)
	interactions := []Interaction{commitInteraction("turn-1")}
	config := DefaultReplayConfig()

	results := Replay(start, interactions, config)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Action != "commit" {
		t.Errorf("expected action=commit, got %s", r.Action)
	}
	if r.FinalVersionID == start.VersionID {
		t.Error("expected state to advance (new VersionID)")
	}
	if r.GateDecision == nil {
		t.Error("expected GateDecision to be populated")
	}
	if r.EvalResult == nil {
		t.Error("expected EvalResult to be populated")
	}
	if !r.EvalResult.Passed {
		t.Error("expected EvalResult.Passed=true")
	}
}

// 2. Gate rejection: UserCorrection=true → action="gate_reject", state unchanged.
func TestReplay_GateRejection(t *testing.T) {
	start := seededState("v0", 0.1)
	inter := commitInteraction("turn-1")
	inter.Signals.UserCorrection = true
	interactions := []Interaction{inter}
	config := DefaultReplayConfig()

	results := Replay(start, interactions, config)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Action != "gate_reject" {
		t.Errorf("expected action=gate_reject, got %s", r.Action)
	}
	if r.FinalVersionID != start.VersionID {
		t.Error("expected state unchanged after gate rejection")
	}
	if r.GateDecision == nil {
		t.Error("expected GateDecision to be populated")
	}
	if !r.GateDecision.Vetoed {
		t.Error("expected GateDecision.Vetoed=true")
	}
	if r.EvalResult != nil {
		t.Error("expected EvalResult to be nil after gate rejection")
	}
}

// 3. Eval rollback: very low MaxStateNorm + large state → action="eval_rollback".
func TestReplay_EvalRollback(t *testing.T) {
	start := seededState("v0", 2.0) // large initial values
	inter := commitInteraction("turn-1")
	config := DefaultReplayConfig()
	config.EvalConfig.MaxStateNorm = 0.001 // impossibly tight threshold

	results := Replay(start, interactions(inter), config)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Action != "eval_rollback" {
		t.Errorf("expected action=eval_rollback, got %s", r.Action)
	}
	if r.FinalVersionID != start.VersionID {
		t.Error("expected state unchanged after eval rollback")
	}
	if r.EvalResult == nil {
		t.Fatal("expected EvalResult to be populated")
	}
	if r.EvalResult.Passed {
		t.Error("expected EvalResult.Passed=false")
	}
}

// 4. No-op: zero signals + zero state → action="no_op".
func TestReplay_NoOp(t *testing.T) {
	start := zeroState("v0")
	inter := Interaction{
		TurnID:       "turn-1",
		Prompt:       "test",
		ResponseText: "resp",
		Entropy:      0,
		Signals:      update.Signals{}, // all zero
	}
	config := DefaultReplayConfig()

	results := Replay(start, []Interaction{inter}, config)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Action != "no_op" {
		t.Errorf("expected action=no_op, got %s", r.Action)
	}
	if r.FinalVersionID != start.VersionID {
		t.Error("expected state unchanged for no_op")
	}
	if r.GateDecision != nil {
		t.Error("expected GateDecision=nil for no_op")
	}
	if r.EvalResult != nil {
		t.Error("expected EvalResult=nil for no_op")
	}
}

// 5. Multi-turn progression: state accumulates across committed turns, resets on rejections.
func TestReplay_MultiTurn(t *testing.T) {
	start := seededState("v0", 0.1)
	inters := []Interaction{
		commitInteraction("turn-1"),
		commitInteraction("turn-2"),
		func() Interaction {
			i := commitInteraction("turn-3")
			i.Signals.UserCorrection = true // gate reject
			return i
		}(),
		commitInteraction("turn-4"),
		commitInteraction("turn-5"),
	}
	config := DefaultReplayConfig()

	results := Replay(start, inters, config)

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	// turn-1: commit
	if results[0].Action != "commit" {
		t.Errorf("turn-1: expected commit, got %s", results[0].Action)
	}
	vid1 := results[0].FinalVersionID

	// turn-2: commit, state should advance from turn-1
	if results[1].Action != "commit" {
		t.Errorf("turn-2: expected commit, got %s", results[1].Action)
	}
	vid2 := results[1].FinalVersionID
	if vid2 == vid1 {
		t.Error("turn-2: expected different version from turn-1")
	}

	// turn-3: gate_reject, state should match turn-2's final version
	if results[2].Action != "gate_reject" {
		t.Errorf("turn-3: expected gate_reject, got %s", results[2].Action)
	}
	if results[2].FinalVersionID != vid2 {
		t.Error("turn-3: expected state to remain at turn-2's version after rejection")
	}

	// turn-4: commit, state continues from turn-2 (not turn-3's rejected proposal)
	if results[3].Action != "commit" {
		t.Errorf("turn-4: expected commit, got %s", results[3].Action)
	}

	// turn-5: commit
	if results[4].Action != "commit" {
		t.Errorf("turn-5: expected commit, got %s", results[4].Action)
	}
}

// 6. Config passthrough: high learning rate produces different results than defaults.
func TestReplay_ConfigPassthrough(t *testing.T) {
	start := seededState("v0", 0.5)
	inters := []Interaction{commitInteraction("turn-1")}

	defaultResults := Replay(start, inters, DefaultReplayConfig())

	highLR := DefaultReplayConfig()
	highLR.UpdateConfig.LearningRate = 0.5
	highLRResults := Replay(start, inters, highLR)

	if len(defaultResults) != 1 || len(highLRResults) != 1 {
		t.Fatal("expected 1 result each")
	}

	// Both should commit but with different delta norms
	if defaultResults[0].Action != "commit" || highLRResults[0].Action != "commit" {
		t.Skip("one config didn't commit; can't compare deltas")
	}
	if defaultResults[0].UpdateMetrics.DeltaNorm == highLRResults[0].UpdateMetrics.DeltaNorm {
		t.Error("expected different delta norms with different learning rates")
	}
	if highLRResults[0].UpdateMetrics.DeltaNorm <= defaultResults[0].UpdateMetrics.DeltaNorm {
		t.Error("expected higher learning rate to produce larger delta")
	}
}

// 7. Summarize: counts match result actions.
func TestReplay_Summarize(t *testing.T) {
	start := seededState("v0", 0.1)
	inters := []Interaction{
		commitInteraction("turn-1"),
		func() Interaction {
			i := commitInteraction("turn-2")
			i.Signals.UserCorrection = true
			return i
		}(),
		commitInteraction("turn-3"),
		{TurnID: "turn-4", Signals: update.Signals{}}, // no-op (requires DecayRate=0)
	}
	config := DefaultReplayConfig()
	config.UpdateConfig.DecayRate = 0 // ensure zero signals = true no-op

	results := Replay(start, inters, config)

	// Find final state from last commit
	var finalState state.StateRecord
	finalState = start
	for _, r := range results {
		if r.Action == "commit" {
			// We need to track state — use the version ID
			finalState.VersionID = r.FinalVersionID
		}
	}

	summary := Summarize(results, finalState)

	if summary.TotalTurns != 4 {
		t.Errorf("expected TotalTurns=4, got %d", summary.TotalTurns)
	}
	if summary.Commits != 2 {
		t.Errorf("expected Commits=2, got %d", summary.Commits)
	}
	if summary.GateRejects != 1 {
		t.Errorf("expected GateRejects=1, got %d", summary.GateRejects)
	}
	if summary.NoOps != 1 {
		t.Errorf("expected NoOps=1, got %d", summary.NoOps)
	}
	if summary.FinalState.VersionID != finalState.VersionID {
		t.Error("expected FinalState to match provided final state")
	}
}

// 8. Deterministic: same inputs → same outputs.
func TestReplay_Deterministic(t *testing.T) {
	start := seededState("v0", 0.3)
	inters := []Interaction{
		commitInteraction("turn-1"),
		commitInteraction("turn-2"),
	}
	config := DefaultReplayConfig()

	results1 := Replay(start, inters, config)
	results2 := Replay(start, inters, config)

	if len(results1) != len(results2) {
		t.Fatalf("result lengths differ: %d vs %d", len(results1), len(results2))
	}
	for i := range results1 {
		if results1[i].Action != results2[i].Action {
			t.Errorf("turn %d: action differs: %s vs %s", i, results1[i].Action, results2[i].Action)
		}
		if results1[i].UpdateMetrics.DeltaNorm != results2[i].UpdateMetrics.DeltaNorm {
			t.Errorf("turn %d: delta norm differs: %f vs %f", i, results1[i].UpdateMetrics.DeltaNorm, results2[i].UpdateMetrics.DeltaNorm)
		}
	}
}

// helper: wrap single interaction in slice.
func interactions(i Interaction) []Interaction {
	return []Interaction{i}
}
