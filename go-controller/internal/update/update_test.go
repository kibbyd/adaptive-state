package update

import (
	"testing"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
)

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

	result := Update(old, ctx, sig, evidence)

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

	r1 := Update(old, ctx, sig, nil)
	r2 := Update(old, ctx, sig, nil)

	// Both should produce identical state vectors (even if version IDs differ)
	for i := range r1.NewState.StateVector {
		if r1.NewState.StateVector[i] != r2.NewState.StateVector[i] {
			t.Fatalf("non-deterministic at index %d", i)
		}
	}
}
