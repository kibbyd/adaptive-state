package gate

import (
	"testing"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
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

func TestGateCommitOnCleanSignals(t *testing.T) {
	g := NewGate(DefaultGateConfig())
	old := makeState(nil)
	proposed := makeState(nil)
	signals := update.Signals{}
	metrics := update.Metrics{DeltaNorm: 0, SegmentsHit: []string{}}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "commit" {
		t.Fatalf("expected commit, got %s: %s", decision.Action, decision.Reason)
	}
	if decision.Vetoed {
		t.Fatal("should not be vetoed")
	}
}

func TestGateRejectOnRiskFlag(t *testing.T) {
	g := NewGate(DefaultGateConfig())
	old := makeState(nil)
	proposed := makeState(nil)
	signals := update.Signals{RiskFlag: true}
	metrics := update.Metrics{}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "reject" {
		t.Fatalf("expected reject, got %s", decision.Action)
	}
	if !decision.Vetoed {
		t.Fatal("should be vetoed")
	}
	if len(decision.VetoSignals) == 0 {
		t.Fatal("expected veto signals")
	}
	if decision.VetoSignals[0].Type != VetoSafety {
		t.Fatalf("expected VetoSafety, got %s", decision.VetoSignals[0].Type)
	}
}

func TestGateRejectOnUserCorrection(t *testing.T) {
	g := NewGate(DefaultGateConfig())
	old := makeState(nil)
	proposed := makeState(nil)
	signals := update.Signals{UserCorrection: true}
	metrics := update.Metrics{}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "reject" {
		t.Fatalf("expected reject, got %s", decision.Action)
	}
	if decision.VetoSignals[0].Type != VetoUserCorrection {
		t.Fatalf("expected VetoUserCorrection, got %s", decision.VetoSignals[0].Type)
	}
}

func TestGateRejectOnToolFailure(t *testing.T) {
	g := NewGate(DefaultGateConfig())
	old := makeState(nil)
	proposed := makeState(nil)
	signals := update.Signals{ToolFailure: true}
	metrics := update.Metrics{}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "reject" {
		t.Fatalf("expected reject, got %s", decision.Action)
	}
	if decision.VetoSignals[0].Type != VetoToolFailure {
		t.Fatalf("expected VetoToolFailure, got %s", decision.VetoSignals[0].Type)
	}
}

func TestGateRejectOnConstraintViolation(t *testing.T) {
	g := NewGate(DefaultGateConfig())
	old := makeState(nil)
	proposed := makeState(nil)
	signals := update.Signals{ConstraintViolation: true}
	metrics := update.Metrics{}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "reject" {
		t.Fatalf("expected reject, got %s", decision.Action)
	}
	if decision.VetoSignals[0].Type != VetoConstraint {
		t.Fatalf("expected VetoConstraint, got %s", decision.VetoSignals[0].Type)
	}
}

func TestGateRejectOnDeltaNormExceedsCap(t *testing.T) {
	config := DefaultGateConfig()
	config.MaxDeltaNorm = 2.0
	g := NewGate(config)

	old := makeState(nil)
	// Create a proposed state with large delta in prefs segment
	proposed := makeState(map[int]float32{0: 3.0, 1: 3.0})
	signals := update.Signals{}
	metrics := update.Metrics{}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "reject" {
		t.Fatalf("expected reject for large delta norm, got %s: %s", decision.Action, decision.Reason)
	}
	if !decision.Vetoed {
		t.Fatal("should be vetoed")
	}
}

func TestGateRejectOnRiskSegmentNormExceedsCap(t *testing.T) {
	config := DefaultGateConfig()
	config.RiskSegmentCap = 2.0
	g := NewGate(config)

	old := makeState(nil)
	// Risk segment is indices 96-127; set several to high values
	proposed := makeState(map[int]float32{96: 2.0, 97: 2.0, 98: 2.0})
	signals := update.Signals{}
	metrics := update.Metrics{}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "reject" {
		t.Fatalf("expected reject for risk segment norm, got %s: %s", decision.Action, decision.Reason)
	}
}

func TestGateMultipleVetoes(t *testing.T) {
	g := NewGate(DefaultGateConfig())
	old := makeState(nil)
	proposed := makeState(nil)
	signals := update.Signals{RiskFlag: true, ToolFailure: true}
	metrics := update.Metrics{}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.5)

	if decision.Action != "reject" {
		t.Fatalf("expected reject, got %s", decision.Action)
	}
	if len(decision.VetoSignals) < 2 {
		t.Fatalf("expected at least 2 veto signals, got %d", len(decision.VetoSignals))
	}
}

func TestGateSoftScoreRange(t *testing.T) {
	g := NewGate(DefaultGateConfig())
	old := makeState(nil)
	proposed := makeState(nil)
	signals := update.Signals{}
	metrics := update.Metrics{DeltaNorm: 0, SegmentsHit: []string{}}

	decision := g.Evaluate(old, proposed, signals, metrics, 0.3)

	if decision.SoftScore < 0 || decision.SoftScore > 1.0 {
		t.Fatalf("soft score %.4f out of [0, 1] range", decision.SoftScore)
	}
}
