package projection

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// #region types

// PreferenceStyle categorizes what a preference controls.
type PreferenceStyle string

const (
	StyleConcise  PreferenceStyle = "concise"
	StyleDetailed PreferenceStyle = "detailed"
	StyleExamples PreferenceStyle = "examples"
	StyleGeneral  PreferenceStyle = "general"
)

// Preference is a stored user preference with metadata.
type Preference struct {
	ID        int
	Text      string
	Style     PreferenceStyle
	Source    string // "explicit" | "correction" | "inferred"
	CreatedAt time.Time
}

// #endregion types

// #region store

// PreferenceStore manages persistent user preferences in SQLite.
type PreferenceStore struct {
	db *sql.DB
}

// NewPreferenceStore creates the preferences table if needed and returns a store.
func NewPreferenceStore(db *sql.DB) (*PreferenceStore, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS preferences (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		text TEXT NOT NULL,
		style TEXT NOT NULL DEFAULT 'general',
		source TEXT NOT NULL DEFAULT 'explicit',
		created_at DATETIME NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create preferences table: %w", err)
	}
	// Migrate: add style column if missing (pre-existing tables lack it)
	_, _ = db.Exec(`ALTER TABLE preferences ADD COLUMN style TEXT NOT NULL DEFAULT 'general'`)
	return &PreferenceStore{db: db}, nil
}

// Add stores a new preference. Infers style from text.
// Contradiction handling: if a new preference has the same style as an existing one
// (and the style is not "general"), the old one is replaced.
func (s *PreferenceStore) Add(text, source string) error {
	style := InferStyle(text)

	// Exact duplicate check (case-insensitive)
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM preferences WHERE LOWER(text) = LOWER(?)", text).Scan(&count)
	if err != nil {
		return fmt.Errorf("check duplicate preference: %w", err)
	}
	if count > 0 {
		return nil
	}

	// Contradiction handling: replace existing preference of same non-general style
	if style != StyleGeneral {
		_, err = s.db.Exec("DELETE FROM preferences WHERE style = ?", string(style))
		if err != nil {
			return fmt.Errorf("remove contradicting preference: %w", err)
		}
	}

	_, err = s.db.Exec(
		"INSERT INTO preferences (text, style, source, created_at) VALUES (?, ?, ?, ?)",
		text, string(style), source, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert preference: %w", err)
	}
	return nil
}

// List returns all stored preferences.
func (s *PreferenceStore) List() ([]Preference, error) {
	rows, err := s.db.Query("SELECT id, text, style, source, created_at FROM preferences ORDER BY created_at")
	if err != nil {
		return nil, fmt.Errorf("list preferences: %w", err)
	}
	defer rows.Close()

	var prefs []Preference
	for rows.Next() {
		var p Preference
		var ts, style string
		if err := rows.Scan(&p.ID, &p.Text, &style, &p.Source, &ts); err != nil {
			return nil, fmt.Errorf("scan preference: %w", err)
		}
		p.Style = PreferenceStyle(style)
		p.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		prefs = append(prefs, p)
	}
	return prefs, nil
}

// DeleteByPrefix removes all preferences whose text starts with the given prefix (case-insensitive).
func (s *PreferenceStore) DeleteByPrefix(prefix string) {
	_, _ = s.db.Exec("DELETE FROM preferences WHERE LOWER(text) LIKE LOWER(?) || '%'", prefix)
}

// #endregion store

// #region rule-types

// Rule is a stored behavioral rule: when trigger is matched, respond with response.
type Rule struct {
	ID         int
	Trigger    string
	Response   string
	Priority   int
	Confidence float64
	CreatedAt  time.Time
}

// #endregion rule-types

// #region rule-store

// RuleStore manages persistent behavioral rules in SQLite.
type RuleStore struct {
	db *sql.DB
}

// NewRuleStore creates the rules table if needed and returns a store.
func NewRuleStore(db *sql.DB) (*RuleStore, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trigger TEXT NOT NULL,
		response TEXT NOT NULL,
		priority INTEGER NOT NULL DEFAULT 5,
		confidence REAL NOT NULL DEFAULT 1.0,
		created_at DATETIME NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create rules table: %w", err)
	}
	return &RuleStore{db: db}, nil
}

// Add stores a new behavioral rule. Replaces existing rule with same trigger (case-insensitive).
func (s *RuleStore) Add(trigger, response string, priority int, confidence float64) error {
	trigger = strings.TrimSpace(trigger)
	response = strings.TrimSpace(response)
	if trigger == "" || response == "" {
		return fmt.Errorf("rule trigger and response must be non-empty")
	}

	// Replace existing rule with same trigger (case-insensitive)
	_, err := s.db.Exec("DELETE FROM rules WHERE LOWER(trigger) = LOWER(?)", trigger)
	if err != nil {
		return fmt.Errorf("remove existing rule: %w", err)
	}

	_, err = s.db.Exec(
		"INSERT INTO rules (trigger, response, priority, confidence, created_at) VALUES (?, ?, ?, ?, ?)",
		trigger, response, priority, confidence, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert rule: %w", err)
	}
	return nil
}

// List returns all stored rules ordered by priority (highest first), then creation time.
func (s *RuleStore) List() ([]Rule, error) {
	rows, err := s.db.Query("SELECT id, trigger, response, priority, confidence, created_at FROM rules ORDER BY priority DESC, created_at")
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		var ts string
		if err := rows.Scan(&r.ID, &r.Trigger, &r.Response, &r.Priority, &r.Confidence, &ts); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		rules = append(rules, r)
	}
	return rules, nil
}

// Match returns all rules whose trigger matches the input (case-insensitive substring match).
// Returns matches ordered by priority (highest first).
func (s *RuleStore) Match(input string) ([]Rule, error) {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return nil, nil
	}

	rules, err := s.List()
	if err != nil {
		return nil, err
	}

	var matched []Rule
	for _, r := range rules {
		if strings.ToLower(r.Trigger) == lower {
			matched = append(matched, r)
		}
	}
	return matched, nil
}

// #endregion rule-store

// #region detect

// containsPatterns match anywhere in the text (low false-positive risk).
var containsPatterns = []string{
	"i prefer",
	"i like",
	"i want",
	"i need",
	"i'd like",
	"i would like",
	"please always",
	"keep it",
	"make it",
	"be more",
	"be less",
	"no fluff",
	"no filler",
	"short answers",
	"brief answers",
	"concise answers",
	"detailed answers",
	"verbose answers",
}

// startPatterns only match at the start of input (high false-positive risk if used as contains).
var startPatterns = []string{
	"always ",
	"never ",
	"don't use",
	"don't give",
	"don't include",
	"don't add",
	"don't ever",
	"don't be",
	"do not use",
	"do not give",
	"do not include",
	"do not add",
	"do not ever",
	"do not be",
}

// DetectPreference checks if a prompt contains an explicit preference statement.
// Returns the normalized preference text and true if detected, empty and false otherwise.
func DetectPreference(prompt string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	if lower == "" {
		return "", false
	}

	for _, pat := range containsPatterns {
		if strings.Contains(lower, pat) {
			cleaned := strings.TrimSpace(prompt)
			cleaned = strings.TrimRight(cleaned, ".!?")
			return cleaned, true
		}
	}
	for _, pat := range startPatterns {
		if strings.HasPrefix(lower, pat) {
			cleaned := strings.TrimSpace(prompt)
			cleaned = strings.TrimRight(cleaned, ".!?")
			return cleaned, true
		}
	}
	return "", false
}

// DetectCorrection checks if a prompt is a correction of the previous response.
// Returns true for phrases like "try again", "that's wrong", "no, I meant".
func DetectCorrection(prompt string) bool {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	correctionPatterns := []string{
		"try again",
		"that's wrong",
		"that is wrong",
		"that's not",
		"that is not",
		"not correct",
		"incorrect",
		"wrong ",
		"nope",
		"no,",
		"no i meant",
		"not what i",
		"i said ",
		"remember i said",
		"like i said",
		"as i said",
		"i already said",
		"i told you",
	}
	for _, pat := range correctionPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// DetectIdentity checks if a prompt contains an identity statement like
// "my name is Daniel", "I'm Daniel", "call me Daniel".
// Returns the extracted name and true if detected.
func DetectIdentity(prompt string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(prompt))

	identityPatterns := []struct {
		prefix string
		strip  bool // true = prefix is followed by the name
	}{
		{"my name is ", true},
		{"i'm ", true},
		{"i am ", true},
		{"call me ", true},
		{"you can call me ", true},
		{"people call me ", true},
	}

	for _, pat := range identityPatterns {
		if strings.HasPrefix(lower, pat.prefix) {
			name := strings.TrimSpace(prompt[len(pat.prefix):])
			name = strings.TrimRight(name, ".!?,;")
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

// #endregion detect

// #region style

// conciseKeywords and detailedKeywords drive style inference.
var conciseKeywords = []string{
	"short", "brief", "concise", "terse", "minimal", "no fluff",
	"no filler", "direct", "to the point", "succinct",
}
var detailedKeywords = []string{
	"detailed", "verbose", "thorough", "explain in detail",
	"in depth", "comprehensive", "elaborate",
}
var examplesKeywords = []string{
	"example", "examples", "code example", "show me",
	"demonstrate", "illustration",
}

// InferStyle determines the PreferenceStyle from free-text preference.
func InferStyle(text string) PreferenceStyle {
	lower := strings.ToLower(text)
	for _, kw := range conciseKeywords {
		if strings.Contains(lower, kw) {
			return StyleConcise
		}
	}
	for _, kw := range detailedKeywords {
		if strings.Contains(lower, kw) {
			return StyleDetailed
		}
	}
	for _, kw := range examplesKeywords {
		if strings.Contains(lower, kw) {
			return StyleExamples
		}
	}
	return StyleGeneral
}

// #endregion style

// #region compliance

// PreferenceComplianceScore measures how well a response matches stored preferences.
// Returns 0.5 (neutral) when no preferences match. Never returns >0.5 without evidence.
// Response word count is used as the primary compliance metric for style preferences.
func PreferenceComplianceScore(prefs []Preference, response string) float32 {
	if len(prefs) == 0 {
		return 0.5
	}

	wordCount := len(strings.Fields(response))
	score := float32(0.5)
	matched := false

	for _, p := range prefs {
		switch p.Style {
		case StyleConcise:
			matched = true
			if wordCount <= 20 {
				score += 0.3
			} else if wordCount <= 50 {
				score += 0.1
			} else {
				score -= 0.3
			}
		case StyleDetailed:
			matched = true
			if wordCount >= 100 {
				score += 0.3
			} else if wordCount >= 50 {
				score += 0.1
			} else {
				score -= 0.3
			}
		case StyleExamples:
			matched = true
			lower := strings.ToLower(response)
			if strings.Contains(lower, "example") || strings.Contains(lower, "e.g.") ||
				strings.Contains(lower, "for instance") || strings.Contains(lower, "```") {
				score += 0.2
			} else {
				score -= 0.1
			}
		}
	}

	if !matched {
		return 0.5
	}
	return clamp(score)
}

// clamp restricts v to [0, 1].
func clamp(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// #endregion compliance

// #region rule-detect

// rulePatterns matches phrases that teach conditional response behavior.
var rulePatterns = []struct {
	pattern    string // contains-match in lowercased input
	hasCapture bool   // true if pattern implies trigger→response structure
}{
	{"when i say", true},
	{"if i say", true},
	{"you say", true},
	{"you respond with", true},
	{"you should say", true},
	{"you should respond", true},
	{"respond with", true},
	{"reply with", true},
	{"your response should be", true},
}

// DetectRule checks if a prompt is teaching a behavioral rule.
// Returns true if the prompt matches rule-teaching patterns.
func DetectRule(prompt string) bool {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	for _, rp := range rulePatterns {
		if strings.Contains(lower, rp.pattern) {
			return true
		}
	}
	return false
}

// ExtractRule attempts to extract trigger→response pairs from a rule-teaching prompt.
// Looks for patterns like:
//   - "when I say X, you say Y"
//   - "if I say X, respond with Y"
//   - "I say X, you say Y"
//
// Returns trigger, response, ok.
func ExtractRule(prompt string) (string, string, bool) {
	lower := strings.ToLower(strings.TrimSpace(prompt))

	// Pattern: "when I say <trigger>, you say/respond with <response>"
	// Also: "if I say <trigger>, you say/respond with <response>"
	separators := []string{
		", you say ",
		", you respond with ",
		", you should say ",
		", you should respond with ",
		", respond with ",
		", reply with ",
		" you say ",
		" you respond with ",
		" respond with ",
	}
	prefixes := []string{
		"when i say ",
		"if i say ",
		"i say ",
	}

	for _, prefix := range prefixes {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := prompt[len(prefix):]
		restLower := lower[len(prefix):]

		for _, sep := range separators {
			idx := strings.Index(restLower, sep)
			if idx < 0 {
				continue
			}
			trigger := strings.TrimSpace(rest[:idx])
			response := strings.TrimSpace(rest[idx+len(sep):])
			// Clean up quotes and trailing punctuation
			trigger = strings.Trim(trigger, `"'`)
			response = strings.Trim(response, `"'.!`)
			if trigger != "" && response != "" {
				return trigger, response, true
			}
		}
	}

	// Pattern: "you say <response> when I say <trigger>"
	if strings.HasPrefix(lower, "you say ") {
		rest := prompt[len("you say "):]
		restLower := lower[len("you say "):]
		whenParts := []string{" when i say ", " if i say "}
		for _, wp := range whenParts {
			idx := strings.Index(restLower, wp)
			if idx < 0 {
				continue
			}
			response := strings.TrimSpace(rest[:idx])
			trigger := strings.TrimSpace(rest[idx+len(wp):])
			trigger = strings.Trim(trigger, `"'.!`)
			response = strings.Trim(response, `"'.!`)
			if trigger != "" && response != "" {
				return trigger, response, true
			}
		}
	}

	return "", "", false
}

// FormatRulesBlock builds the behavioral rules block for system prompt injection.
// Returns empty string if no rules exist.
func FormatRulesBlock(rules []Rule) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[BEHAVIORAL RULES]\n")
	b.WriteString("Follow these rules EXACTLY. They override all other behavior.\n")
	for _, r := range rules {
		b.WriteString(fmt.Sprintf("- If user says: %s → You respond with: %s\n", r.Trigger, r.Response))
	}
	return b.String()
}

// #endregion rule-detect

// #region project

// ProjectToPrompt builds the [ADAPTIVE STATE] block to prepend to the user's prompt.
// prefsNorm is the L2 norm of the prefs segment — used as confidence weight.
// If no preferences exist or prefsNorm is near zero, returns empty string.
func ProjectToPrompt(preferences []Preference, prefsNorm float32) string {
	if len(preferences) == 0 {
		return ""
	}

	// Confidence from prefs segment norm: 0 → no injection, >0.05 → inject
	// Exception: identity preferences always project regardless of norm
	confidence := float64(prefsNorm)
	hasIdentity := false
	for _, p := range preferences {
		if strings.HasPrefix(p.Text, "The user's name is") {
			hasIdentity = true
			break
		}
	}
	if confidence < 0.05 && !hasIdentity {
		return ""
	}
	// Cap confidence display at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	var b strings.Builder
	b.WriteString("[ADAPTIVE STATE]\n")
	for _, p := range preferences {
		b.WriteString(fmt.Sprintf("- %s\n", p.Text))
	}
	b.WriteString(fmt.Sprintf("(confidence: %.0f%%)\n", math.Round(confidence*100)))
	return b.String()
}

// WrapPrompt prepends the adaptive state block to the user's prompt.
// If stateBlock is empty, returns prompt unchanged.
func WrapPrompt(stateBlock, prompt string) string {
	if stateBlock == "" {
		return prompt
	}
	return stateBlock + "\n[USER PROMPT]\n" + prompt
}

// #endregion project
