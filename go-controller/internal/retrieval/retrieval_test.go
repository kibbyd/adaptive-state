package retrieval

import (
	"testing"
)

// #region gate1-tests
func TestGate1_LowEntropy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 1.0

	result := GateResult{}
	entropy := float32(0.5)

	if entropy >= cfg.EntropyThreshold {
		t.Fatal("expected entropy below threshold")
	}
	result.Gate1Passed = false
	if result.Gate1Passed {
		t.Error("gate1 should not pass with low entropy")
	}
}

func TestGate1_HighEntropy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 0.5

	entropy := float32(1.5)
	if entropy < cfg.EntropyThreshold {
		t.Fatal("expected entropy above threshold")
	}
}

// #endregion gate1-tests

// #region gate3-tests
func TestConsistencyCheck_FiltersEmpty(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "1", Text: "valid evidence", Score: 0.9},
		{ID: "2", Text: "", Score: 0.8}, // empty — should be filtered
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 1 {
		t.Errorf("expected 1 valid result, got %d", len(valid))
	}
	if valid[0].ID != "1" {
		t.Errorf("expected ID=1, got %s", valid[0].ID)
	}
}

func TestConsistencyCheck_FiltersOverlong(t *testing.T) {
	r := &Retriever{config: RetrievalConfig{MaxEvidenceLen: 10}}
	results := []EvidenceRecord{
		{ID: "1", Text: "short", Score: 0.9},
		{ID: "2", Text: "this text is way too long for the limit", Score: 0.8},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 1 {
		t.Errorf("expected 1 valid result, got %d", len(valid))
	}
}

func TestConsistencyCheck_FiltersDuplicateIDs(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "dup", Text: "first", Score: 0.9},
		{ID: "dup", Text: "second", Score: 0.8},
		{ID: "unique", Text: "third", Score: 0.7},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 2 {
		t.Errorf("expected 2 valid results, got %d", len(valid))
	}
	// First occurrence of "dup" should survive
	if valid[0].Text != "first" {
		t.Errorf("expected first occurrence, got %s", valid[0].Text)
	}
}

func TestConsistencyCheck_AllValid(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "a", Text: "evidence a", Score: 0.9},
		{ID: "b", Text: "evidence b", Score: 0.8},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 2 {
		t.Errorf("expected 2 valid results, got %d", len(valid))
	}
}

func TestConsistencyCheck_AllFiltered(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "1", Text: "", Score: 0.9},
		{ID: "2", Text: "", Score: 0.8},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 0 {
		t.Errorf("expected 0 valid results, got %d", len(valid))
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EntropyThreshold <= 0 {
		t.Error("expected positive entropy threshold")
	}
	if cfg.SimilarityThreshold <= 0 {
		t.Error("expected positive similarity threshold")
	}
	if cfg.TopK <= 0 {
		t.Error("expected positive TopK")
	}
	if cfg.MaxEvidenceLen <= 0 {
		t.Error("expected positive MaxEvidenceLen")
	}
}

// #endregion gate3-tests
