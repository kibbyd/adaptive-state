package projection

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// #region types

// Preference is a stored user preference with metadata.
type Preference struct {
	ID        int
	Text      string
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
		source TEXT NOT NULL DEFAULT 'explicit',
		created_at DATETIME NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create preferences table: %w", err)
	}
	return &PreferenceStore{db: db}, nil
}

// Add stores a new preference. Skips duplicates (case-insensitive).
func (s *PreferenceStore) Add(text, source string) error {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM preferences WHERE LOWER(text) = LOWER(?)", text).Scan(&count)
	if err != nil {
		return fmt.Errorf("check duplicate preference: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, err = s.db.Exec(
		"INSERT INTO preferences (text, source, created_at) VALUES (?, ?, ?)",
		text, source, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert preference: %w", err)
	}
	return nil
}

// List returns all stored preferences.
func (s *PreferenceStore) List() ([]Preference, error) {
	rows, err := s.db.Query("SELECT id, text, source, created_at FROM preferences ORDER BY created_at")
	if err != nil {
		return nil, fmt.Errorf("list preferences: %w", err)
	}
	defer rows.Close()

	var prefs []Preference
	for rows.Next() {
		var p Preference
		var ts string
		if err := rows.Scan(&p.ID, &p.Text, &p.Source, &ts); err != nil {
			return nil, fmt.Errorf("scan preference: %w", err)
		}
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
