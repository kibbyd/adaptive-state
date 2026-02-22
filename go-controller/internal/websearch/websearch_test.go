package websearch

import (
	"testing"
)

// #region format_tests

func TestFormatAsEvidence_MultipleResults(t *testing.T) {
	results := []Result{
		{Title: "Title A", Snippet: "Snippet A", URL: "https://a.com"},
		{Title: "Title B", Snippet: "Snippet B", URL: "https://b.com"},
	}
	out := FormatAsEvidence(results)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !contains(out, "[Web Search Results]") {
		t.Error("missing header")
	}
	if !contains(out, "1. Title A") {
		t.Error("missing result 1")
	}
	if !contains(out, "2. Title B") {
		t.Error("missing result 2")
	}
	if !contains(out, "Source: https://a.com") {
		t.Error("missing source URL")
	}
}

func TestFormatAsEvidence_Empty(t *testing.T) {
	out := FormatAsEvidence(nil)
	if out != "" {
		t.Errorf("expected empty string for nil results, got %q", out)
	}
}

func TestFormatAsEvidence_NoURL(t *testing.T) {
	results := []Result{{Title: "T", Snippet: "S", URL: ""}}
	out := FormatAsEvidence(results)
	if contains(out, "Source:") {
		t.Error("should not include Source line when URL is empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// #endregion format_tests

// #region config_tests

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxResults != 3 {
		t.Errorf("expected MaxResults=3, got %d", cfg.MaxResults)
	}
	if !cfg.Enabled {
		t.Error("expected Enabled=true by default")
	}
	if cfg.EntropyThreshold != 0.3 {
		t.Errorf("expected EntropyThreshold=0.3, got %f", cfg.EntropyThreshold)
	}
}

// #endregion config_tests
