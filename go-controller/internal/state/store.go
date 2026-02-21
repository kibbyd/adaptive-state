package state

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// #region schema
const schema = `
CREATE TABLE IF NOT EXISTS state_versions (
	version_id    TEXT PRIMARY KEY,
	parent_id     TEXT,
	state_vector  BLOB NOT NULL,
	segment_map   TEXT NOT NULL,
	created_at    TEXT NOT NULL,
	metrics_json  TEXT,
	FOREIGN KEY (parent_id) REFERENCES state_versions(version_id)
);

CREATE TABLE IF NOT EXISTS provenance_log (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	version_id    TEXT NOT NULL,
	context_hash  TEXT,
	trigger_type  TEXT NOT NULL,
	signals_json  TEXT,
	evidence_refs TEXT,
	decision      TEXT NOT NULL,
	reason        TEXT,
	created_at    TEXT NOT NULL,
	FOREIGN KEY (version_id) REFERENCES state_versions(version_id)
);

CREATE TABLE IF NOT EXISTS active_state (
	id            INTEGER PRIMARY KEY CHECK (id = 1),
	version_id    TEXT NOT NULL,
	FOREIGN KEY (version_id) REFERENCES state_versions(version_id)
);
`
// #endregion schema

// #region store-struct
// Store manages versioned state in SQLite.
type Store struct {
	db *sql.DB
}
// #endregion store-struct

// #region constructor
// NewStore opens a SQLite database and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("pragma: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("pragma fk: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}
// #endregion constructor

// #region close
// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
// #endregion close

// #region db-accessor
// DB returns the underlying *sql.DB for use by other packages (e.g. logging).
func (s *Store) DB() *sql.DB {
	return s.db
}
// #endregion db-accessor

// #region create-initial
// CreateInitialState creates a zero-vector initial state version.
func (s *Store) CreateInitialState(segMap SegmentMap) (StateRecord, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	vec := [128]float32{}

	rec := StateRecord{
		VersionID:   id,
		ParentID:    "",
		StateVector: vec,
		SegmentMap:  segMap,
		CreatedAt:   now,
	}

	segJSON, err := json.Marshal(segMap)
	if err != nil {
		return StateRecord{}, fmt.Errorf("marshal segment map: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return StateRecord{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO state_versions (version_id, parent_id, state_vector, segment_map, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, nil, encodeVector(vec), string(segJSON), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return StateRecord{}, fmt.Errorf("insert version: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO active_state (id, version_id) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET version_id = excluded.version_id`,
		id,
	)
	if err != nil {
		return StateRecord{}, fmt.Errorf("set active: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return StateRecord{}, fmt.Errorf("commit: %w", err)
	}

	return rec, nil
}
// #endregion create-initial

// #region get-current
// GetCurrent reads the active state version.
func (s *Store) GetCurrent() (StateRecord, error) {
	var versionID string
	err := s.db.QueryRow(`SELECT version_id FROM active_state WHERE id = 1`).Scan(&versionID)
	if err != nil {
		return StateRecord{}, fmt.Errorf("get active: %w", err)
	}
	return s.GetVersion(versionID)
}
// #endregion get-current

// #region get-version
// GetVersion retrieves a specific state version by ID.
func (s *Store) GetVersion(id string) (StateRecord, error) {
	var rec StateRecord
	var parentID sql.NullString
	var vecBlob []byte
	var segJSON string
	var createdStr string
	var metricsJSON sql.NullString

	err := s.db.QueryRow(
		`SELECT version_id, parent_id, state_vector, segment_map, created_at, metrics_json
		 FROM state_versions WHERE version_id = ?`, id,
	).Scan(&rec.VersionID, &parentID, &vecBlob, &segJSON, &createdStr, &metricsJSON)
	if err != nil {
		return StateRecord{}, fmt.Errorf("get version %s: %w", id, err)
	}

	if parentID.Valid {
		rec.ParentID = parentID.String
	}
	rec.StateVector = decodeVector(vecBlob)
	if err := json.Unmarshal([]byte(segJSON), &rec.SegmentMap); err != nil {
		return StateRecord{}, fmt.Errorf("unmarshal segment map: %w", err)
	}
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	if metricsJSON.Valid {
		rec.MetricsJSON = metricsJSON.String
	}

	return rec, nil
}
// #endregion get-version

// #region commit-state
// CommitState inserts a new version and updates the active pointer atomically.
func (s *Store) CommitState(rec StateRecord) error {
	segJSON, err := json.Marshal(rec.SegmentMap)
	if err != nil {
		return fmt.Errorf("marshal segment map: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var parentPtr interface{}
	if rec.ParentID != "" {
		parentPtr = rec.ParentID
	}

	var metricsPtr interface{}
	if rec.MetricsJSON != "" {
		metricsPtr = rec.MetricsJSON
	}

	_, err = tx.Exec(
		`INSERT INTO state_versions (version_id, parent_id, state_vector, segment_map, created_at, metrics_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		rec.VersionID, parentPtr, encodeVector(rec.StateVector), string(segJSON),
		rec.CreatedAt.Format(time.RFC3339Nano), metricsPtr,
	)
	if err != nil {
		return fmt.Errorf("insert version: %w", err)
	}

	_, err = tx.Exec(
		`UPDATE active_state SET version_id = ? WHERE id = 1`, rec.VersionID,
	)
	if err != nil {
		return fmt.Errorf("update active: %w", err)
	}

	return tx.Commit()
}
// #endregion commit-state

// #region rollback
// Rollback sets the active pointer to a previous version.
func (s *Store) Rollback(targetVersionID string) error {
	// Verify the target version exists
	var exists int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM state_versions WHERE version_id = ?`, targetVersionID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check version: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("version %s not found", targetVersionID)
	}

	_, err = s.db.Exec(`UPDATE active_state SET version_id = ? WHERE id = 1`, targetVersionID)
	if err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}
// #endregion rollback

// #region list-versions
// ListVersions returns the most recent state versions.
func (s *Store) ListVersions(limit int) ([]StateRecord, error) {
	rows, err := s.db.Query(
		`SELECT version_id, parent_id, state_vector, segment_map, created_at, metrics_json
		 FROM state_versions ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	var records []StateRecord
	for rows.Next() {
		var rec StateRecord
		var parentID sql.NullString
		var vecBlob []byte
		var segJSON string
		var createdStr string
		var metricsJSON sql.NullString

		if err := rows.Scan(&rec.VersionID, &parentID, &vecBlob, &segJSON, &createdStr, &metricsJSON); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if parentID.Valid {
			rec.ParentID = parentID.String
		}
		rec.StateVector = decodeVector(vecBlob)
		if err := json.Unmarshal([]byte(segJSON), &rec.SegmentMap); err != nil {
			return nil, fmt.Errorf("unmarshal segment map: %w", err)
		}
		rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		if metricsJSON.Valid {
			rec.MetricsJSON = metricsJSON.String
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}
// #endregion list-versions

// #region vector-encoding
func encodeVector(v [128]float32) []byte {
	buf := make([]byte, 128*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(b []byte) [128]float32 {
	var v [128]float32
	for i := range v {
		if i*4+4 <= len(b) {
			v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
		}
	}
	return v
}
// #endregion vector-encoding
