package eval

import (
	"testing"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
)

func makeState(vals map[int]float32) state.StateRecord {
	rec := state.StateRecord{
		VersionID:  "test-v1",
		SegmentMap: state.DefaultSegmentMap(),
	}
	for i, v := range vals {
		rec.StateVector[i] = v
	}
	return rec
}

func TestEvalPassesOnZeroState(t *testing.T) {
	h := NewEvalHarness(DefaultEvalConfig())
	st := makeState(nil)

	result := h.Run(st, 0.5)

	if !result.Passed {
		t.Fatalf("expected pass on zero state, got fail: %s", result.Reason)
	}
	if len(result.Metrics) == 0 {
		t.Fatal("expected metrics")
	}
}

func TestEvalFailsOnStateNormSpike(t *testing.T) {
	config := DefaultEvalConfig()
	config.MaxStateNorm = 5.0
	h := NewEvalHarness(config)

	// Set many values high to exceed norm
	vals := make(map[int]float32)
	for i := 0; i < 128; i++ {
		vals[i] = 1.0 // L2 norm = sqrt(128) ≈ 11.3
	}
	st := makeState(vals)

	result := h.Run(st, 0.5)

	if result.Passed {
		t.Fatal("expected fail on high state norm")
	}
}

func TestEvalFailsOnSegmentNormSpike(t *testing.T) {
	config := DefaultEvalConfig()
	config.MaxSegmentNorm = 2.0
	h := NewEvalHarness(config)

	// Set risk segment (96-127) to high values
	vals := make(map[int]float32)
	for i := 96; i < 128; i++ {
		vals[i] = 1.0 // L2 norm = sqrt(32) ≈ 5.66
	}
	st := makeState(vals)

	result := h.Run(st, 0.5)

	if result.Passed {
		t.Fatal("expected fail on segment norm spike")
	}

	// Verify the specific segment failed
	foundFail := false
	for _, m := range result.Metrics {
		if m.Name == "segment_risk_norm" && !m.Pass {
			foundFail = true
		}
	}
	if !foundFail {
		t.Fatal("expected segment_risk_norm metric to fail")
	}
}

func TestEvalEntropyInformationalOnly(t *testing.T) {
	config := DefaultEvalConfig()
	config.EntropyBaseline = 1.0
	h := NewEvalHarness(config)

	st := makeState(nil)
	// High entropy above baseline — should still pass (informational only in Phase 3)
	result := h.Run(st, 5.0)

	if !result.Passed {
		t.Fatalf("entropy check should be informational, not blocking: %s", result.Reason)
	}

	// But the metric should show pass=false
	for _, m := range result.Metrics {
		if m.Name == "entropy" && m.Pass {
			t.Fatal("entropy metric should show pass=false when above baseline")
		}
	}
}

func TestEvalMetricCount(t *testing.T) {
	h := NewEvalHarness(DefaultEvalConfig())
	st := makeState(nil)

	result := h.Run(st, 0.5)

	// Expect: state_norm + 4 segments + entropy = 6 metrics
	if len(result.Metrics) != 6 {
		t.Fatalf("expected 6 metrics, got %d", len(result.Metrics))
	}
}

func TestEvalPassesWithModerateValues(t *testing.T) {
	h := NewEvalHarness(DefaultEvalConfig())

	// Set some moderate values across segments
	vals := map[int]float32{
		0: 1.0, 5: 0.5,   // prefs
		32: 0.8, 40: 0.3, // goals
		64: 0.2,           // heuristics
		96: 0.1, 100: 0.4, // risk
	}
	st := makeState(vals)

	result := h.Run(st, 1.0)

	if !result.Passed {
		t.Fatalf("expected pass with moderate values, got fail: %s", result.Reason)
	}
}
