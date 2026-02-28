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
		{"Don't use technical terms", true},
		{"Always use examples", true},
		{"What is the capital of France?", false},
		{"Explain quantum physics", false},
		{"", false},
		{"Hello there", false},
		{"I never knew that", false},
		{"don't worry about it", false},
		{"He always arrives late", false},
		{"I do not understand the question", false},
		// Action requests directed at AI — should NOT be preferences
		{"I want you to read test.txt", false},
		{"I need you to overcome your limitation", false},
		{"I want you to list the files", false},
		{"I'd like you to search for that", false},
		// Behavior preferences directed at AI — should still be preferences
		{"I want you to be concise", true},
		{"I want you to respond in bullet points", true},
		{"I need you to explain things simply", true},
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
		{"nope", true},
		{"NOPE that's not the number", true},
		{"that's not right", true},
		{"that is not correct", true},
		{"not correct", true},
		{"wrong answer", true},
		{"incorrect", true},
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

func TestDetectIdentity(t *testing.T) {
	cases := []struct {
		input    string
		wantName string
		wantOK   bool
	}{
		{"my name is Daniel", "Daniel", true},
		{"I'm Daniel", "Daniel", true},
		{"call me Dan", "Dan", true},
		{"I'm glad you think so", "", false},
		{"I'm not sure about that", "", false},
		{"I'm going to the store", "", false},
		{"I'm really excited about this project we're working on together", "", false},
		{"I am commander. You are ?", "", false},
	}
	for _, tc := range cases {
		name, ok := DetectIdentity(tc.input)
		if ok != tc.wantOK {
			t.Errorf("DetectIdentity(%q) ok=%v, want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if ok && name != tc.wantName {
			t.Errorf("DetectIdentity(%q) name=%q, want %q", tc.input, name, tc.wantName)
		}
	}
}

func TestDetectAIDesignation(t *testing.T) {
	cases := []struct {
		input           string
		wantDesignation string
		wantOK          bool
	}{
		{"your name is Architect", "Architect", true},
		{"I'll call you Sage", "Sage", true},
		{"your name is something I haven't decided on yet", "", false},
	}
	for _, tc := range cases {
		designation, ok := DetectAIDesignation(tc.input)
		if ok != tc.wantOK {
			t.Errorf("DetectAIDesignation(%q) ok=%v, want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if ok && designation != tc.wantDesignation {
			t.Errorf("DetectAIDesignation(%q) designation=%q, want %q", tc.input, designation, tc.wantDesignation)
		}
	}
}

// #endregion detect-tests

// #region style-tests

func TestInferStyle(t *testing.T) {
	cases := []struct {
		input string
		want  PreferenceStyle
	}{
		{"I prefer short answers", StyleConcise},
		{"Keep it brief", StyleConcise},
		{"No fluff please", StyleConcise},
		{"Be concise", StyleConcise},
		{"I want detailed explanations", StyleDetailed},
		{"Be thorough", StyleDetailed},
		{"Always use examples", StyleExamples},
		{"Show me code examples", StyleExamples},
		{"I like friendly tone", StyleGeneral},
		{"Always respond in English", StyleGeneral},
	}
	for _, tc := range cases {
		got := InferStyle(tc.input)
		if got != tc.want {
			t.Errorf("InferStyle(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// #endregion style-tests

// #region compliance-tests

func TestPreferenceComplianceScore_ConciseShortResponse(t *testing.T) {
	prefs := []Preference{{Style: StyleConcise}}
	score := PreferenceComplianceScore(prefs, "Paris.")
	if score < 0.7 {
		t.Errorf("expected high score for concise compliance, got %.2f", score)
	}
}

func TestPreferenceComplianceScore_ConciseLongResponse(t *testing.T) {
	prefs := []Preference{{Style: StyleConcise}}
	long := strings.Repeat("word ", 60)
	score := PreferenceComplianceScore(prefs, long)
	if score > 0.3 {
		t.Errorf("expected low score for verbose response with concise pref, got %.2f", score)
	}
}

func TestPreferenceComplianceScore_DetailedLongResponse(t *testing.T) {
	prefs := []Preference{{Style: StyleDetailed}}
	long := strings.Repeat("word ", 120)
	score := PreferenceComplianceScore(prefs, long)
	if score < 0.7 {
		t.Errorf("expected high score for detailed compliance, got %.2f", score)
	}
}

func TestPreferenceComplianceScore_NoPreferences(t *testing.T) {
	score := PreferenceComplianceScore(nil, "anything")
	if score != 0.5 {
		t.Errorf("expected neutral 0.5 with no prefs, got %.2f", score)
	}
}

func TestPreferenceComplianceScore_GeneralOnly(t *testing.T) {
	prefs := []Preference{{Style: StyleGeneral}}
	score := PreferenceComplianceScore(prefs, "anything")
	if score != 0.5 {
		t.Errorf("expected neutral 0.5 for general-only prefs, got %.2f", score)
	}
}

func TestPreferenceComplianceScore_ExamplesPresent(t *testing.T) {
	prefs := []Preference{{Style: StyleExamples}}
	score := PreferenceComplianceScore(prefs, "For example, consider the following...")
	if score < 0.6 {
		t.Errorf("expected above-neutral for examples compliance, got %.2f", score)
	}
}

func TestPreferenceComplianceScore_ExamplesMissing(t *testing.T) {
	prefs := []Preference{{Style: StyleExamples}}
	score := PreferenceComplianceScore(prefs, "The answer is simple.")
	if score > 0.5 {
		t.Errorf("expected below-neutral when examples missing, got %.2f", score)
	}
}

// #endregion compliance-tests

// #region contradiction-tests

func TestPreferenceStore_ContradictionReplaces(t *testing.T) {
	db := testDB(t)
	store, _ := NewPreferenceStore(db)

	store.Add("I prefer short answers", "explicit")   // concise
	store.Add("I want detailed answers", "explicit")   // detailed — should NOT remove concise (different style)

	prefs, _ := store.List()
	if len(prefs) != 2 {
		t.Fatalf("expected 2 prefs (different styles), got %d", len(prefs))
	}

	store.Add("Be very brief and terse", "explicit")   // concise — should replace first concise pref
	prefs, _ = store.List()
	if len(prefs) != 2 {
		t.Fatalf("expected 2 prefs after concise replacement, got %d", len(prefs))
	}
	// The concise one should be the new one
	for _, p := range prefs {
		if p.Style == StyleConcise && p.Text != "Be very brief and terse" {
			t.Errorf("expected replaced concise pref, got %q", p.Text)
		}
	}
}

func TestPreferenceStore_GeneralDoesNotReplace(t *testing.T) {
	db := testDB(t)
	store, _ := NewPreferenceStore(db)

	store.Add("Always respond in English", "explicit")
	store.Add("Use a friendly tone", "explicit")

	prefs, _ := store.List()
	if len(prefs) != 2 {
		t.Fatalf("expected 2 general prefs (no replacement), got %d", len(prefs))
	}
}

// #endregion contradiction-tests

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

// #region rule-store-tests

func TestNewRuleStore_CreatesTable(t *testing.T) {
	db := testDB(t)
	store, err := NewRuleStore(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='rules'").Scan(&name)
	if err != nil {
		t.Fatalf("table not created: %v", err)
	}
}

func TestRuleStore_AddAndList(t *testing.T) {
	db := testDB(t)
	store, _ := NewRuleStore(db)

	if err := store.Add("knock knock", "Who's there?", 5, 1.0); err != nil {
		t.Fatalf("add error: %v", err)
	}
	if err := store.Add("Daniel", "Daniel who?", 5, 1.0); err != nil {
		t.Fatalf("add error: %v", err)
	}

	rules, err := store.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Trigger != "knock knock" {
		t.Errorf("expected trigger 'knock knock', got %q", rules[0].Trigger)
	}
	if rules[0].Response != "Who's there?" {
		t.Errorf("expected response 'Who's there?', got %q", rules[0].Response)
	}
}

func TestRuleStore_ReplacesSameTrigger(t *testing.T) {
	db := testDB(t)
	store, _ := NewRuleStore(db)

	store.Add("knock knock", "Who's there?", 5, 1.0)
	store.Add("knock knock", "Who goes there?", 5, 1.0) // should replace

	rules, _ := store.List()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule after replacement, got %d", len(rules))
	}
	if rules[0].Response != "Who goes there?" {
		t.Errorf("expected replaced response, got %q", rules[0].Response)
	}
}

func TestRuleStore_RejectsEmpty(t *testing.T) {
	db := testDB(t)
	store, _ := NewRuleStore(db)

	if err := store.Add("", "response", 5, 1.0); err == nil {
		t.Error("expected error for empty trigger")
	}
	if err := store.Add("trigger", "", 5, 1.0); err == nil {
		t.Error("expected error for empty response")
	}
}

func TestRuleStore_Match(t *testing.T) {
	db := testDB(t)
	store, _ := NewRuleStore(db)

	store.Add("knock knock", "Who's there?", 5, 1.0)
	store.Add("Daniel", "Daniel who?", 5, 1.0)

	matches, err := store.Match("knock knock")
	if err != nil {
		t.Fatalf("match error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Response != "Who's there?" {
		t.Errorf("expected 'Who's there?', got %q", matches[0].Response)
	}

	// Case insensitive
	matches, _ = store.Match("KNOCK KNOCK")
	if len(matches) != 1 {
		t.Fatalf("expected case-insensitive match, got %d", len(matches))
	}

	// No match
	matches, _ = store.Match("hello")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestRuleStore_ListEmpty(t *testing.T) {
	db := testDB(t)
	store, _ := NewRuleStore(db)

	rules, err := store.List()
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if rules != nil {
		t.Errorf("expected nil for empty list, got %v", rules)
	}
}

// #endregion rule-store-tests

// #region rule-detect-tests

func TestDetectRule(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"when I say knock knock, you say who's there", true},
		{"if I say Daniel, respond with Daniel who?", true},
		{"you say who's there when I say knock knock", true},
		{"you respond with Daniel who?", true},
		{"your response should be who's there", true},
		{"What is the capital of France?", false},
		{"I prefer short answers", false},
		{"Hello there", false},
		{"", false},
	}

	for _, tc := range cases {
		got := DetectRule(tc.input)
		if got != tc.want {
			t.Errorf("DetectRule(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestExtractRule(t *testing.T) {
	cases := []struct {
		input       string
		wantTrigger string
		wantResp    string
		wantOK      bool
	}{
		{
			"when I say knock knock, you say who's there",
			"knock knock", "who's there", true,
		},
		{
			"if I say Daniel, you respond with Daniel who?",
			"Daniel", "Daniel who?", true,
		},
		{
			"I say hello, you say hi there",
			"hello", "hi there", true,
		},
		{
			"you say who's there when I say knock knock",
			"knock knock", "who's there", true,
		},
		{
			"What is Go?",
			"", "", false,
		},
	}

	for _, tc := range cases {
		trigger, resp, ok := ExtractRule(tc.input)
		if ok != tc.wantOK {
			t.Errorf("ExtractRule(%q) ok=%v, want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if ok {
			if strings.ToLower(trigger) != strings.ToLower(tc.wantTrigger) {
				t.Errorf("ExtractRule(%q) trigger=%q, want %q", tc.input, trigger, tc.wantTrigger)
			}
			if strings.ToLower(resp) != strings.ToLower(tc.wantResp) {
				t.Errorf("ExtractRule(%q) response=%q, want %q", tc.input, resp, tc.wantResp)
			}
		}
	}
}

func TestFormatRulesBlock(t *testing.T) {
	rules := []Rule{
		{Trigger: "knock knock", Response: "Who's there?"},
		{Trigger: "Daniel", Response: "Daniel who?"},
	}
	block := FormatRulesBlock(rules)
	if !strings.Contains(block, "[BEHAVIORAL RULES]") {
		t.Error("missing header")
	}
	if !strings.Contains(block, "knock knock") {
		t.Error("missing trigger")
	}
	if !strings.Contains(block, "Who's there?") {
		t.Error("missing response")
	}
}

func TestFormatRulesBlock_Empty(t *testing.T) {
	block := FormatRulesBlock(nil)
	if block != "" {
		t.Errorf("expected empty for nil rules, got %q", block)
	}
}

// #endregion rule-detect-tests
