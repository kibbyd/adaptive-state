package orchestrator

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStrategyMemory_RecordAndQuery(t *testing.T) {
	db := newTestDB(t)
	mem, err := NewStrategyMemory(db)
	if err != nil {
		t.Fatal(err)
	}

	// No data → empty result
	sid, score, err := mem.BestStrategy("factual", "simple", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if sid != "" {
		t.Errorf("expected empty strategy, got %q", sid)
	}

	// Insert 2 samples for "default" → still below threshold of 3
	for i := 0; i < 2; i++ {
		err := mem.RecordOutcome(OutcomeRecord{
			TurnID: "t1", TurnType: TurnFactual, Complexity: ComplexitySimple,
			Risk: RiskSafe, StrategyID: StrategyDefault, AttemptNum: 0,
			Quality: 0.8, FailureType: FailureNone, Entropy: 0.5,
			Accepted: true, CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sid, _, err = mem.BestStrategy("factual", "simple", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if sid != "" {
		t.Errorf("expected empty (below threshold), got %q", sid)
	}

	// Add 3rd sample → should return "default"
	err = mem.RecordOutcome(OutcomeRecord{
		TurnID: "t2", TurnType: TurnFactual, Complexity: ComplexitySimple,
		Risk: RiskSafe, StrategyID: StrategyDefault, AttemptNum: 0,
		Quality: 0.9, FailureType: FailureNone, Entropy: 0.5,
		Accepted: true, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	sid, score, err = mem.BestStrategy("factual", "simple", "safe")
	if err != nil {
		t.Fatal(err)
	}
	if sid != StrategyDefault {
		t.Errorf("expected %q, got %q", StrategyDefault, sid)
	}
	if score < 0.7 {
		t.Errorf("expected score > 0.7, got %.2f", score)
	}
}

func TestStrategyMemory_BestStrategy_PicksHigherQuality(t *testing.T) {
	db := newTestDB(t)
	mem, err := NewStrategyMemory(db)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	// 4 samples of "default" with quality 0.4
	for i := 0; i < 4; i++ {
		mem.RecordOutcome(OutcomeRecord{
			TurnID: "t1", TurnType: TurnPhilosophical, Complexity: ComplexityDeep,
			Risk: RiskSensitive, StrategyID: StrategyDefault, AttemptNum: 0,
			Quality: 0.4, FailureType: FailureNone, Entropy: 0.5,
			Accepted: true, CreatedAt: now,
		})
	}

	// 4 samples of "cipher_direct" with quality 0.9
	for i := 0; i < 4; i++ {
		mem.RecordOutcome(OutcomeRecord{
			TurnID: "t2", TurnType: TurnPhilosophical, Complexity: ComplexityDeep,
			Risk: RiskSensitive, StrategyID: StrategyCipherDirect, AttemptNum: 0,
			Quality: 0.9, FailureType: FailureNone, Entropy: 0.5,
			Accepted: true, CreatedAt: now,
		})
	}

	sid, _, err := mem.BestStrategy("philosophical", "deep", "sensitive")
	if err != nil {
		t.Fatal(err)
	}
	if sid != StrategyCipherDirect {
		t.Errorf("expected %q, got %q", StrategyCipherDirect, sid)
	}
}
