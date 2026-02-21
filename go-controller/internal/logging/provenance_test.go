package logging

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// #region helpers
func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE provenance_log (
		version_id   TEXT NOT NULL,
		context_hash TEXT,
		trigger_type TEXT NOT NULL,
		signals_json TEXT,
		evidence_refs TEXT,
		decision     TEXT NOT NULL,
		reason       TEXT,
		created_at   TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

// #endregion helpers

// #region log-decision-tests
func TestLogDecision_Success(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	entry := ProvenanceEntry{
		VersionID:    "v1",
		ContextHash:  "abc123",
		TriggerType:  "cycle",
		SignalsJSON:  `{"entropy":1.5}`,
		EvidenceRefs: "ev1,ev2",
		Decision:     "commit",
		Reason:       "high confidence",
		CreatedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	err := LogDecision(db, entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM provenance_log").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}

	var versionID, decision string
	db.QueryRow("SELECT version_id, decision FROM provenance_log").Scan(&versionID, &decision)
	if versionID != "v1" {
		t.Errorf("expected version_id 'v1', got %q", versionID)
	}
	if decision != "commit" {
		t.Errorf("expected decision 'commit', got %q", decision)
	}
}

func TestLogDecision_ZeroCreatedAt(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	entry := ProvenanceEntry{
		VersionID:   "v2",
		TriggerType: "manual",
		Decision:    "no_op",
	}

	before := time.Now().UTC()
	err := LogDecision(db, entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var createdAtStr string
	db.QueryRow("SELECT created_at FROM provenance_log").Scan(&createdAtStr)
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		t.Fatalf("parse created_at: %v", err)
	}
	if createdAt.Before(before) {
		t.Error("expected auto-filled created_at to be >= test start time")
	}
}

func TestLogDecision_EmptyOptionalFields(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	entry := ProvenanceEntry{
		VersionID:   "v3",
		ContextHash: "",
		TriggerType: "cycle",
		SignalsJSON:  "",
		EvidenceRefs: "",
		Decision:    "reject",
		Reason:      "",
		CreatedAt:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	err := LogDecision(db, entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var contextHash, signalsJSON, evidenceRefs, reason sql.NullString
	db.QueryRow("SELECT context_hash, signals_json, evidence_refs, reason FROM provenance_log").Scan(
		&contextHash, &signalsJSON, &evidenceRefs, &reason,
	)
	if contextHash.Valid {
		t.Error("expected NULL context_hash for empty string")
	}
	if signalsJSON.Valid {
		t.Error("expected NULL signals_json for empty string")
	}
	if evidenceRefs.Valid {
		t.Error("expected NULL evidence_refs for empty string")
	}
	if reason.Valid {
		t.Error("expected NULL reason for empty string")
	}
}

func TestLogDecision_Error(t *testing.T) {
	db := setupDB(t)
	db.Close() // close to force error

	entry := ProvenanceEntry{
		VersionID:   "v4",
		TriggerType: "cycle",
		Decision:    "commit",
	}

	err := LogDecision(db, entry)
	if err == nil {
		t.Fatal("expected error on closed db")
	}
}

// #endregion log-decision-tests

// #region null-if-empty-tests
func TestNullIfEmpty_Empty(t *testing.T) {
	result := nullIfEmpty("")
	if result != nil {
		t.Errorf("expected nil for empty string, got %v", result)
	}
}

func TestNullIfEmpty_NonEmpty(t *testing.T) {
	result := nullIfEmpty("hello")
	if result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}
}

// #endregion null-if-empty-tests
