package replay

import (
	"os"
	"path/filepath"
	"testing"
)

// #region fixture-tests

// TestFixture_LiveSession loads the live_session fixture, runs Replay(), and
// compares each turn's Action against the expected action. This is the primary
// regression test — if gate/decay/update parameters change, this catches drift.
func TestFixture_LiveSession(t *testing.T) {
	fixturePath := filepath.Join("testdata", "live_session.json")
	f, err := LoadFixture(fixturePath)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}

	// Convert fixture types to domain types
	startState := f.StartState.ToStateRecord()
	config := f.Config.ToReplayConfig()

	interactions := make([]Interaction, len(f.Interactions))
	for i := range f.Interactions {
		interactions[i] = f.Interactions[i].ToInteraction()
	}

	// Run replay
	results := Replay(startState, interactions, config)

	if len(results) != len(f.ExpectedResults) {
		t.Fatalf("expected %d results, got %d", len(f.ExpectedResults), len(results))
	}

	for i, expected := range f.ExpectedResults {
		actual := results[i]
		if actual.TurnID != expected.TurnID {
			t.Errorf("turn %d: expected turn_id=%s, got %s", i, expected.TurnID, actual.TurnID)
		}
		if actual.Action != expected.Action {
			t.Errorf("turn %d (%s): expected action=%s, got action=%s (reason: %s)",
				i, expected.TurnID, expected.Action, actual.Action, actual.Reason)
		}
	}
}

// TestFixture_RealSession loads the real_session fixture (exported from production DB),
// runs Replay(), and compares each turn's Action against the expected action.
// This is the second regression baseline — real GateRecord data, not synthetic.
func TestFixture_RealSession(t *testing.T) {
	fixturePath := filepath.Join("testdata", "real_session.json")
	f, err := LoadFixture(fixturePath)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}

	// Convert fixture types to domain types
	startState := f.StartState.ToStateRecord()
	config := f.Config.ToReplayConfig()

	interactions := make([]Interaction, len(f.Interactions))
	for i := range f.Interactions {
		interactions[i] = f.Interactions[i].ToInteraction()
	}

	// Run replay
	results := Replay(startState, interactions, config)

	if len(results) != len(f.ExpectedResults) {
		t.Fatalf("expected %d results, got %d", len(f.ExpectedResults), len(results))
	}

	for i, expected := range f.ExpectedResults {
		actual := results[i]
		if actual.TurnID != expected.TurnID {
			t.Errorf("turn %d: expected turn_id=%s, got %s", i, expected.TurnID, actual.TurnID)
		}
		if actual.Action != expected.Action {
			t.Errorf("turn %d (%s): expected action=%s, got action=%s (reason: %s)",
				i, expected.TurnID, expected.Action, actual.Action, actual.Reason)
		}
	}
}

// TestLoadFixture_NotFound verifies error on missing file.
func TestLoadFixture_NotFound(t *testing.T) {
	_, err := LoadFixture("testdata/nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestLoadFixture_Malformed verifies error on invalid JSON.
func TestLoadFixture_Malformed(t *testing.T) {
	// Write a temp file with invalid JSON
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json}"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := LoadFixture(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// #endregion fixture-tests
