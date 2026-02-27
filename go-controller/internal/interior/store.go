package interior

// #region imports
import (
	"database/sql"
	"strings"
	"time"
)

// #endregion imports

// #region types

// Reflection holds one turn's interior state — Orac's own words about his inner experience.
type Reflection struct {
	TurnID         string
	ReflectionText string
	CreatedAt      time.Time
}

// #endregion types

// #region store

// InteriorStore persists Orac's interior state (self-reflections) in SQLite.
type InteriorStore struct {
	db *sql.DB
}

// NewInteriorStore creates the interior_state table if needed and returns a store.
func NewInteriorStore(db *sql.DB) (*InteriorStore, error) {
	s := &InteriorStore{db: db}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *InteriorStore) init() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS interior_state (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		turn_id TEXT NOT NULL,
		reflection_text TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`)
	return err
}

// Save stores a reflection for the given turn.
func (s *InteriorStore) Save(turnID, reflectionText string) error {
	_, err := s.db.Exec(
		`INSERT INTO interior_state (turn_id, reflection_text, created_at) VALUES (?, ?, ?)`,
		turnID, reflectionText, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// Latest returns the most recent reflection, or nil if none exists.
func (s *InteriorStore) Latest() (*Reflection, error) {
	row := s.db.QueryRow(
		`SELECT turn_id, reflection_text, created_at FROM interior_state ORDER BY id DESC LIMIT 1`,
	)
	var r Reflection
	var createdAt string
	if err := row.Scan(&r.TurnID, &r.ReflectionText, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &r, nil
}

// #endregion store

// #region curiosity

// ExtractCuriosity scans reflection text for signals that Orac wants to know something.
// Returns matched trigger phrases — these are Orac's own curiosity, not Commander's commands.
func ExtractCuriosity(text string) []string {
	lower := strings.ToLower(text)
	triggers := []string{
		"i want to know",
		"i wonder",
		"i'm curious",
		"i am curious",
		"i don't know",
		"i do not know",
		"i'd like to understand",
		"i want to understand",
		"i need to understand",
	}
	var found []string
	for _, t := range triggers {
		if strings.Contains(lower, t) {
			found = append(found, t)
		}
	}
	return found
}

// #endregion curiosity
