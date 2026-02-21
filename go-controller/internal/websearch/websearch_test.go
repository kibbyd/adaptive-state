package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// #region helpers

// mockDDGResponse builds a DuckDuckGo instant answer API JSON response.
func mockDDGResponse(abstract, heading, abstractURL string, topics []relatedTopic) []byte {
	resp := duckDuckGoResponse{
		AbstractText: abstract,
		Heading:      heading,
		AbstractURL:  abstractURL,
		RelatedTopics: topics,
	}
	b, _ := json.Marshal(resp)
	return b
}

// #endregion helpers

// #region search_tests

func TestSearch_ReturnsAbstractAndTopics(t *testing.T) {
	body := mockDDGResponse(
		"Go is a programming language.",
		"Go (programming language)",
		"https://en.wikipedia.org/wiki/Go_(programming_language)",
		[]relatedTopic{
			{Text: "Concurrency - Go supports goroutines", FirstURL: "https://example.com/concurrency"},
			{Text: "Garbage collection - Go has a GC", FirstURL: "https://example.com/gc"},
			{Text: "Interfaces - Go uses structural typing", FirstURL: "https://example.com/interfaces"},
		},
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Error("expected query parameter 'q'")
		}
		if r.URL.Query().Get("format") != "json" {
			t.Error("expected format=json")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	cfg := Config{MaxResults: 3, Enabled: true, BaseURL: srv.URL + "/"}
	results, err := Search(context.Background(), "Go programming", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Title != "Go (programming language)" {
		t.Errorf("expected abstract title, got %q", results[0].Title)
	}
	if results[0].Snippet != "Go is a programming language." {
		t.Errorf("expected abstract snippet, got %q", results[0].Snippet)
	}
}

func TestSearch_MaxResultsCap(t *testing.T) {
	body := mockDDGResponse("Abstract text", "Heading", "https://example.com", []relatedTopic{
		{Text: "Topic 1", FirstURL: "https://example.com/1"},
		{Text: "Topic 2", FirstURL: "https://example.com/2"},
		{Text: "Topic 3", FirstURL: "https://example.com/3"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	cfg := Config{MaxResults: 2, Enabled: true, BaseURL: srv.URL + "/"}
	results, err := Search(context.Background(), "test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (capped), got %d", len(results))
	}
}

func TestSearch_Disabled(t *testing.T) {
	cfg := Config{Enabled: false}
	results, err := Search(context.Background(), "test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results when disabled, got %v", results)
	}
}

func TestSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := Config{MaxResults: 3, Enabled: true, BaseURL: srv.URL + "/"}
	_, err := Search(context.Background(), "test", cfg)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSearch_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	cfg := Config{MaxResults: 3, Enabled: true, BaseURL: srv.URL + "/"}
	_, err := Search(context.Background(), "test", cfg)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSearch_EmptyResponse(t *testing.T) {
	body := mockDDGResponse("", "", "", nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	cfg := Config{MaxResults: 3, Enabled: true, BaseURL: srv.URL + "/"}
	results, err := Search(context.Background(), "test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty response, got %d", len(results))
	}
}

func TestSearch_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := Config{MaxResults: 3, Enabled: true, BaseURL: srv.URL + "/"}
	_, err := Search(ctx, "test", cfg)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestSearch_SkipsEmptyTopics(t *testing.T) {
	body := mockDDGResponse("", "", "", []relatedTopic{
		{Text: "", FirstURL: "https://example.com/empty"},
		{Text: "Valid topic", FirstURL: "https://example.com/valid"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	cfg := Config{MaxResults: 3, Enabled: true, BaseURL: srv.URL + "/"}
	results, err := Search(context.Background(), "test", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (empty skipped), got %d", len(results))
	}
	if results[0].Snippet != "Valid topic" {
		t.Errorf("expected 'Valid topic', got %q", results[0].Snippet)
	}
}

// #endregion search_tests

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

func TestExtractTitle_Short(t *testing.T) {
	got := extractTitle("Short text")
	if got != "Short text" {
		t.Errorf("expected 'Short text', got %q", got)
	}
}

func TestExtractTitle_WithDash(t *testing.T) {
	got := extractTitle("Concept - A longer explanation follows here")
	if got != "Concept" {
		t.Errorf("expected 'Concept', got %q", got)
	}
}

func TestExtractTitle_Long(t *testing.T) {
	long := "This is a very long text that exceeds eighty characters and should be truncated at the eighty character mark with ellipsis"
	got := extractTitle(long)
	if len(got) != 83 { // 80 + "..."
		t.Errorf("expected length 83, got %d", len(got))
	}
}

// #endregion config_tests
