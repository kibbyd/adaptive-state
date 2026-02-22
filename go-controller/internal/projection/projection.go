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

// #endregion store

// #region detect

// prefixPatterns are phrases that signal an explicit preference statement.
var prefixPatterns = []string{
	"i prefer",
	"i like",
	"i want",
	"i need",
	"i'd like",
	"i would like",
	"please always",
	"always ",
	"never ",
	"don't ",
	"do not ",
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

// DetectPreference checks if a prompt contains an explicit preference statement.
// Returns the normalized preference text and true if detected, empty and false otherwise.
func DetectPreference(prompt string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(prompt))
	if lower == "" {
		return "", false
	}

	for _, pat := range prefixPatterns {
		if strings.Contains(lower, pat) {
			// Use the original prompt text, cleaned up
			cleaned := strings.TrimSpace(prompt)
			// Remove trailing punctuation for storage
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

// #region project

// ProjectToPrompt builds the [ADAPTIVE STATE] block to prepend to the user's prompt.
// prefsNorm is the L2 norm of the prefs segment — used as confidence weight.
// If no preferences exist or prefsNorm is near zero, returns empty string.
func ProjectToPrompt(preferences []Preference, prefsNorm float32) string {
	if len(preferences) == 0 {
		return ""
	}

	// Confidence from prefs segment norm: 0 → no injection, >0.1 → inject
	confidence := float64(prefsNorm)
	if confidence < 0.05 {
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
