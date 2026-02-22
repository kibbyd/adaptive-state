package projection

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// #region helpers

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// #endregion helpers

// #region store-tests

func TestNewPreferenceStore_CreatesTable(t *testing.T) {
	db := testDB(t)
	store, err := NewPreferenceStore(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	// Table should exist
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='preferences'").Scan(&name)
	if err != nil {
		t.Fatalf("table not created: %v", err)
	}
}

func TestPreferenceStore_AddAndList(t *testing.T) {
	db := testDB(t)
	store, _ := NewPreferenceStore(db)

	if err := store.Add("I prefer short answers", "explicit"); err != nil {
		t.Fatalf("add error: %v", err)
	}
	if err := store.Add("Always use examples", "explicit"); err != nil {
		t.Fatalf("add error: %v", err)
	}

	prefs, err := store.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(prefs) != 2 {
		t.Fatalf("expected 2 preferences, got %d", len(prefs))
	}
	if prefs[0].Text != "I prefer short answers" {
		t.Errorf("expected first pref text, got %q", prefs[0].Text)
	}
	if prefs[0].Source != "explicit" {
		t.Errorf("expected source 'explicit', got %q", prefs[0].Source)
	}
}

func TestPreferenceStore_SkipsDuplicates(t *testing.T) {
	db := testDB(t)
	store, _ := NewPreferenceStore(db)

	store.Add("I prefer short answers", "explicit")
	store.Add("i prefer short answers", "explicit") // case-insensitive duplicate
	store.Add("I PREFER SHORT ANSWERS", "explicit") // another duplicate

	prefs, _ := store.List()
	if len(prefs) != 1 {
		t.Fatalf("expected 1 preference (deduped), got %d", len(prefs))
	}
}

func TestPreferenceStore_ListEmpty(t *testing.T) {
	db := testDB(t)
	store, _ := NewPreferenceStore(db)

	prefs, err := store.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if prefs != nil {
		t.Errorf("expected nil for empty list, got %v", prefs)
	}
}

// #endregion store-tests

// #region detect-tests

func TestDetectPreference_ExplicitStatements(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"I prefer short, direct answers", true},
		{"I like detailed explanations", true},
		{"I want bullet points", true},
		{"Please always use examples", true},
		{"Never use jargon", true},
		{"Keep it brief", true},
		{"No fluff please", true},
		{"What is the capital of France?", false},
		{"Explain quantum physics", false},
		{"", false},
		{"Hello there", false},
	}

	for _, tc := range cases {
		text, got := DetectPreference(tc.input)
		if got != tc.want {
			t.Errorf("DetectPreference(%q) = %v, want %v", tc.input, got, tc.want)
		}
		if got && text == "" {
			t.Errorf("DetectPreference(%q) returned true but empty text", tc.input)
		}
	}
}

func TestDetectPreference_StripsTrailingPunctuation(t *testing.T) {
	text, ok := DetectPreference("I prefer short answers.")
	if !ok {
		t.Fatal("expected detection")
	}
	if strings.HasSuffix(text, ".") {
		t.Errorf("expected trailing period stripped, got %q", text)
	}
}

func TestDetectCorrection(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"Try again", true},
		{"That's wrong", true},
		{"No, I meant something else", true},
		{"Remember I said short answers", true},
		{"I told you to be brief", true},
		{"What is the capital of France?", false},
		{"Thanks, that's great", false},
		{"", false},
	}

	for _, tc := range cases {
		got := DetectCorrection(tc.input)
		if got != tc.want {
			t.Errorf("DetectCorrection(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// #endregion detect-tests

// #region project-tests

func TestProjectToPrompt_WithPreferences(t *testing.T) {
	prefs := []Preference{
		{Text: "I prefer short answers"},
		{Text: "Always use examples"},
	}
	out := ProjectToPrompt(prefs, 0.3)
	if !strings.Contains(out, "[ADAPTIVE STATE]") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "- I prefer short answers") {
		t.Error("missing first preference")
	}
	if !strings.Contains(out, "- Always use examples") {
		t.Error("missing second preference")
	}
	if !strings.Contains(out, "confidence: 30%") {
		t.Errorf("expected confidence 30%%, got: %s", out)
	}
}

func TestProjectToPrompt_Empty(t *testing.T) {
	out := ProjectToPrompt(nil, 0.5)
	if out != "" {
		t.Errorf("expected empty for nil preferences, got %q", out)
	}
}

func TestProjectToPrompt_LowConfidence(t *testing.T) {
	prefs := []Preference{{Text: "something"}}
	out := ProjectToPrompt(prefs, 0.01) // below 0.05 threshold
	if out != "" {
		t.Errorf("expected empty for low confidence, got %q", out)
	}
}

func TestProjectToPrompt_CapsConfidenceAt100(t *testing.T) {
	prefs := []Preference{{Text: "something"}}
	out := ProjectToPrompt(prefs, 2.5) // above 1.0
	if !strings.Contains(out, "confidence: 100%") {
		t.Errorf("expected confidence capped at 100%%, got: %s", out)
	}
}

func TestWrapPrompt_WithState(t *testing.T) {
	block := "[ADAPTIVE STATE]\n- Be concise\n(confidence: 50%)\n"
	wrapped := WrapPrompt(block, "What is Go?")
	if !strings.HasPrefix(wrapped, "[ADAPTIVE STATE]") {
		t.Error("expected state block at start")
	}
	if !strings.Contains(wrapped, "[USER PROMPT]\nWhat is Go?") {
		t.Error("expected user prompt after label")
	}
}

func TestWrapPrompt_EmptyState(t *testing.T) {
	wrapped := WrapPrompt("", "What is Go?")
	if wrapped != "What is Go?" {
		t.Errorf("expected unchanged prompt, got %q", wrapped)
	}
}

// #endregion project-tests
