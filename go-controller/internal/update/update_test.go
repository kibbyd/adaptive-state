package update

import (
	"math"
	"testing"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
)

// zeroConfig matches Phase 3 no-op behavior.
func zeroConfig() UpdateConfig {
	return UpdateConfig{LearningRate: 0, DecayRate: 0, MaxDeltaNormPerSegment: 1.0}
}

func TestUpdateNoOp(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	old.StateVector[0] = 0.5
	old.StateVector[64] = 1.0

	ctx := UpdateContext{TurnID: "turn-1", Prompt: "hello"}
	sig := Signals{}
	evidence := []string{"some evidence"}

	result := Update(old, ctx, sig, evidence, zeroConfig())

	// Decision should be no_op
	if result.Decision.Action != "no_op" {
		t.Fatalf("expected no_op, got %s", result.Decision.Action)
	}

	// State vector should be identical
	for i := range old.StateVector {
		if result.NewState.StateVector[i] != old.StateVector[i] {
			t.Fatalf("state changed at index %d: %f != %f", i, result.NewState.StateVector[i], old.StateVector[i])
		}
	}

	// New version should have different ID and parent = old ID
	if result.NewState.VersionID == old.VersionID {
		t.Fatal("new version should have different ID")
	}
	if result.NewState.ParentID != old.VersionID {
		t.Fatalf("expected parent %s, got %s", old.VersionID, result.NewState.ParentID)
	}

	// Delta norm should be zero
	if result.Metrics.DeltaNorm != 0 {
		t.Fatalf("expected zero delta norm, got %f", result.Metrics.DeltaNorm)
	}
}

func TestUpdateDeterministic(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}

	ctx := UpdateContext{TurnID: "turn-1"}
	sig := Signals{}
	cfg := zeroConfig()

	r1 := Update(old, ctx, sig, nil, cfg)
	r2 := Update(old, ctx, sig, nil, cfg)

	// Both should produce identical state vectors (even if version IDs differ)
	for i := range r1.NewState.StateVector {
		if r1.NewState.StateVector[i] != r2.NewState.StateVector[i] {
			t.Fatalf("non-deterministic at index %d", i)
		}
	}
}

func TestDeltaProposer(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	// Seed some values in prefs segment
	for i := 0; i < 32; i++ {
		old.StateVector[i] = 0.1
	}

	ctx := UpdateContext{TurnID: "turn-1"}
	sig := Signals{SentimentScore: 0.8}
	cfg := DefaultUpdateConfig()

	result := Update(old, ctx, sig, nil, cfg)

	if result.Decision.Action != "commit" {
		t.Fatalf("expected commit, got %s", result.Decision.Action)
	}

	// Prefs segment (0-31) should have changed
	prefsChanged := false
	for i := 0; i < 32; i++ {
		if result.NewState.StateVector[i] != old.StateVector[i] {
			prefsChanged = true
			break
		}
	}
	if !prefsChanged {
		t.Fatal("prefs segment should have changed with SentimentScore > 0")
	}

	// Goals segment (32-63) should be unchanged (no signal, but may decay — zero values don't decay)
	for i := 32; i < 64; i++ {
		if result.NewState.StateVector[i] != old.StateVector[i] {
			t.Fatalf("goals segment changed at index %d without signal", i)
		}
	}

	// Verify segments hit includes prefs
	found := false
	for _, s := range result.Metrics.SegmentsHit {
		if s == "prefs" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected 'prefs' in SegmentsHit")
	}
}

func TestDecayUnreinforced(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	// Set all elements to 1.0
	for i := 0; i < 128; i++ {
		old.StateVector[i] = 1.0
	}

	ctx := UpdateContext{TurnID: "turn-1", Entropy: 0} // no entropy → risk not reinforced
	sig := Signals{}                                     // all zero → no segment reinforced
	cfg := UpdateConfig{LearningRate: 0, DecayRate: 0.1, MaxDeltaNormPerSegment: 1.0}

	result := Update(old, ctx, sig, nil, cfg)

	// Every element should have decayed: 1.0 * (1 - 0.1) = 0.9
	for i := 0; i < 128; i++ {
		expected := float32(0.9)
		if math.Abs(float64(result.NewState.StateVector[i]-expected)) > 1e-6 {
			t.Fatalf("index %d: expected %.4f, got %.4f", i, expected, result.NewState.StateVector[i])
		}
	}

	// Decision should be commit (state changed)
	if result.Decision.Action != "commit" {
		t.Fatalf("expected commit after decay, got %s", result.Decision.Action)
	}
}

func TestDecayReinforcedSegmentPreserved(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	// Set all elements to 1.0
	for i := 0; i < 128; i++ {
		old.StateVector[i] = 1.0
	}

	ctx := UpdateContext{TurnID: "turn-1", Entropy: 0.5} // risk segment reinforced
	sig := Signals{SentimentScore: 0.5}                   // prefs segment reinforced
	cfg := UpdateConfig{LearningRate: 0, DecayRate: 0.1, MaxDeltaNormPerSegment: 1.0}

	result := Update(old, ctx, sig, nil, cfg)

	// Prefs (0-31): reinforced → no decay, and LearningRate=0 so no delta either → unchanged
	for i := 0; i < 32; i++ {
		if result.NewState.StateVector[i] != 1.0 {
			t.Fatalf("prefs index %d should be preserved (reinforced), got %.4f", i, result.NewState.StateVector[i])
		}
	}

	// Goals (32-63): NOT reinforced → should decay
	for i := 32; i < 64; i++ {
		expected := float32(0.9)
		if math.Abs(float64(result.NewState.StateVector[i]-expected)) > 1e-6 {
			t.Fatalf("goals index %d should have decayed, got %.4f", i, result.NewState.StateVector[i])
		}
	}

	// Risk (96-127): reinforced → no decay
	for i := 96; i < 128; i++ {
		if result.NewState.StateVector[i] != 1.0 {
			t.Fatalf("risk index %d should be preserved (reinforced), got %.4f", i, result.NewState.StateVector[i])
		}
	}
}

func TestDeltaClamp(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	// Seed prefs with values so direction is defined
	for i := 0; i < 32; i++ {
		old.StateVector[i] = 0.5
	}

	ctx := UpdateContext{TurnID: "turn-1"}
	sig := Signals{SentimentScore: 100.0} // huge signal
	cfg := UpdateConfig{LearningRate: 1.0, DecayRate: 0, MaxDeltaNormPerSegment: 0.5}

	result := Update(old, ctx, sig, nil, cfg)

	// Find the prefs segment metric
	var prefsDeltaNorm float32
	for _, sm := range result.Metrics.SegmentMetrics {
		if sm.Name == "prefs" {
			prefsDeltaNorm = sm.DeltaNorm
			break
		}
	}

	// Delta norm for prefs should be clamped to MaxDeltaNormPerSegment
	if prefsDeltaNorm > cfg.MaxDeltaNormPerSegment+1e-6 {
		t.Fatalf("prefs delta norm %.6f exceeds cap %.6f", prefsDeltaNorm, cfg.MaxDeltaNormPerSegment)
	}
}

func TestZeroSignalsZeroState(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	// State vector is all zeros by default

	ctx := UpdateContext{TurnID: "turn-1"}
	sig := Signals{}
	cfg := DefaultUpdateConfig()

	result := Update(old, ctx, sig, nil, cfg)

	// Zero state + zero signals → decay of zero is zero, no delta → no_op
	if result.Decision.Action != "no_op" {
		t.Fatalf("expected no_op with zero state and zero signals, got %s", result.Decision.Action)
	}

	if result.Metrics.DeltaNorm != 0 {
		t.Fatalf("expected zero delta norm, got %f", result.Metrics.DeltaNorm)
	}
}

func TestDeterministicWithSignals(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	for i := 0; i < 128; i++ {
		old.StateVector[i] = 0.3
	}

	ctx := UpdateContext{TurnID: "turn-1", Entropy: 0.7}
	sig := Signals{SentimentScore: 0.5, CoherenceScore: 0.3}
	cfg := DefaultUpdateConfig()

	r1 := Update(old, ctx, sig, nil, cfg)
	r2 := Update(old, ctx, sig, nil, cfg)

	for i := range r1.NewState.StateVector {
		if r1.NewState.StateVector[i] != r2.NewState.StateVector[i] {
			t.Fatalf("non-deterministic at index %d: %f vs %f", i, r1.NewState.StateVector[i], r2.NewState.StateVector[i])
		}
	}
}

func TestEntropyDrivesRiskSegment(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	// Seed risk segment so direction is defined
	for i := 96; i < 128; i++ {
		old.StateVector[i] = 0.2
	}

	ctx := UpdateContext{TurnID: "turn-1", Entropy: 0.7}
	sig := Signals{}
	cfg := DefaultUpdateConfig()

	result := Update(old, ctx, sig, nil, cfg)

	// Risk segment (96-127) should have changed (reinforced by entropy + delta applied)
	riskChanged := false
	for i := 96; i < 128; i++ {
		if result.NewState.StateVector[i] != old.StateVector[i] {
			riskChanged = true
			break
		}
	}
	if !riskChanged {
		t.Fatal("risk segment should have changed with entropy > 0")
	}

	found := false
	for _, s := range result.Metrics.SegmentsHit {
		if s == "risk" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected 'risk' in SegmentsHit")
	}
}

func TestNegativeEntropyClamped(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	for i := 96; i < 128; i++ {
		old.StateVector[i] = 0.5
	}

	ctx := UpdateContext{TurnID: "turn-1", Entropy: -0.5}
	sig := Signals{}
	cfg := DefaultUpdateConfig()

	result := Update(old, ctx, sig, nil, cfg)

	// Negative entropy clamped to 0 → risk segment not reinforced, no delta applied
	// Only decay should occur on non-zero segments
	if result.Decision.Action != "commit" {
		t.Fatalf("expected commit from decay, got %s", result.Decision.Action)
	}
}

func TestHighEntropyClamped(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	for i := 96; i < 128; i++ {
		old.StateVector[i] = 0.5
	}

	ctx := UpdateContext{TurnID: "turn-1", Entropy: 5.0} // > 1, should clamp
	sig := Signals{}
	cfg := DefaultUpdateConfig()

	result := Update(old, ctx, sig, nil, cfg)

	// Risk segment should be hit (entropy > 0 reinforces + delta applied with clamped strength = 1.0)
	found := false
	for _, s := range result.Metrics.SegmentsHit {
		if s == "risk" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected 'risk' in SegmentsHit with high entropy")
	}
}

func TestNegativeStateDirection(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	// Seed prefs with negative values
	for i := 0; i < 32; i++ {
		old.StateVector[i] = -0.5
	}

	ctx := UpdateContext{TurnID: "turn-1"}
	sig := Signals{SentimentScore: 0.8}
	cfg := DefaultUpdateConfig()

	result := Update(old, ctx, sig, nil, cfg)

	// Delta direction should be -1 for negative values → values become more negative
	for i := 0; i < 32; i++ {
		if result.NewState.StateVector[i] > old.StateVector[i] {
			t.Fatalf("index %d: expected value to decrease (negative direction), old=%.4f new=%.4f",
				i, old.StateVector[i], result.NewState.StateVector[i])
		}
	}
}

func TestMultipleSignals(t *testing.T) {
	old := state.StateRecord{
		VersionID:  "v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	for i := 0; i < 128; i++ {
		old.StateVector[i] = 0.5
	}

	ctx := UpdateContext{TurnID: "turn-1", Entropy: 0.6}
	sig := Signals{SentimentScore: 0.4, CoherenceScore: 0.3, NoveltyScore: 0.5}
	cfg := DefaultUpdateConfig()

	result := Update(old, ctx, sig, nil, cfg)

	if len(result.Metrics.SegmentsHit) != 4 {
		t.Fatalf("expected 4 segments hit, got %d: %v", len(result.Metrics.SegmentsHit), result.Metrics.SegmentsHit)
	}

	if result.Decision.Action != "commit" {
		t.Fatalf("expected commit with multiple signals, got %s", result.Decision.Action)
	}
}
