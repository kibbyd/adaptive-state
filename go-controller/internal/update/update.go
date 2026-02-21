package update

import (
	"time"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/google/uuid"
)

// #region update-function
// Update is a pure function that computes the next state from the current state,
// context, signals, and evidence. Phase 1: no-op delta — returns state unchanged.
func Update(old state.StateRecord, ctx UpdateContext, signals Signals, evidence []string) UpdateResult {
	// Phase 1: no-op — state vector unchanged
	newRec := state.StateRecord{
		VersionID:   uuid.New().String(),
		ParentID:    old.VersionID,
		StateVector: old.StateVector,
		SegmentMap:  old.SegmentMap,
		CreatedAt:   time.Now().UTC(),
	}

	decision := Decision{
		Action: "no_op",
		Reason: "phase 1: no-op delta, state unchanged",
	}

	metrics := Metrics{
		DeltaNorm:    0.0,
		SegmentsHit:  []string{},
		UpdateTimeMs: 0,
	}

	return UpdateResult{
		NewState: newRec,
		Decision: decision,
		Metrics:  metrics,
	}
}
// #endregion update-function
