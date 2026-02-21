package state

import (
	"database/sql"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func tempDB(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateInitialAndGetCurrent(t *testing.T) {
	s := tempDB(t)
	seg := DefaultSegmentMap()

	rec, err := s.CreateInitialState(seg)
	if err != nil {
		t.Fatalf("CreateInitialState: %v", err)
	}
	if rec.VersionID == "" {
		t.Fatal("expected non-empty version ID")
	}
	if rec.ParentID != "" {
		t.Fatalf("expected empty parent, got %s", rec.ParentID)
	}

	// All zeros
	for i, v := range rec.StateVector {
		if v != 0 {
			t.Fatalf("expected zero at index %d, got %f", i, v)
		}
	}

	cur, err := s.GetCurrent()
	if err != nil {
		t.Fatalf("GetCurrent: %v", err)
	}
	if cur.VersionID != rec.VersionID {
		t.Fatalf("expected %s, got %s", rec.VersionID, cur.VersionID)
	}
}

func TestCommitAndRollback(t *testing.T) {
	s := tempDB(t)
	seg := DefaultSegmentMap()

	v1, err := s.CreateInitialState(seg)
	if err != nil {
		t.Fatalf("CreateInitialState: %v", err)
	}

	// Commit a second version with a modified vector
	v2 := StateRecord{
		VersionID:   "v2-test",
		ParentID:    v1.VersionID,
		StateVector: v1.StateVector,
		SegmentMap:  seg,
		CreatedAt:   v1.CreatedAt,
	}
	v2.StateVector[0] = 1.5

	if err := s.CommitState(v2); err != nil {
		t.Fatalf("CommitState: %v", err)
	}

	cur, _ := s.GetCurrent()
	if cur.VersionID != "v2-test" {
		t.Fatalf("expected v2-test, got %s", cur.VersionID)
	}
	if cur.StateVector[0] != 1.5 {
		t.Fatalf("expected 1.5, got %f", cur.StateVector[0])
	}

	// Rollback to v1
	if err := s.Rollback(v1.VersionID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	cur, _ = s.GetCurrent()
	if cur.VersionID != v1.VersionID {
		t.Fatalf("expected %s after rollback, got %s", v1.VersionID, cur.VersionID)
	}
}

func TestRollbackNonExistent(t *testing.T) {
	s := tempDB(t)
	s.CreateInitialState(DefaultSegmentMap())

	err := s.Rollback("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for non-existent version")
	}
}

func TestListVersions(t *testing.T) {
	s := tempDB(t)
	seg := DefaultSegmentMap()

	v1, _ := s.CreateInitialState(seg)

	v2 := StateRecord{
		VersionID:   "v2",
		ParentID:    v1.VersionID,
		StateVector: v1.StateVector,
		SegmentMap:  seg,
		CreatedAt:   v1.CreatedAt,
	}
	s.CommitState(v2)

	versions, err := s.ListVersions(10)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
}

func TestVectorRoundTrip(t *testing.T) {
	var original [128]float32
	for i := range original {
		original[i] = float32(i) * 0.1
	}
	encoded := encodeVector(original)
	decoded := decodeVector(encoded)
	for i := range original {
		if original[i] != decoded[i] {
			t.Fatalf("mismatch at %d: %f != %f", i, original[i], decoded[i])
		}
	}
}

func TestNewStoreInvalidPath(t *testing.T) {
	_, err := NewStore(filepath.Join(string(os.PathSeparator), "nonexistent", "deep", "path", "test.db"))
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestDBAccessor(t *testing.T) {
	s := tempDB(t)
	db := s.DB()
	if db == nil {
		t.Fatal("expected non-nil *sql.DB")
	}
}

func TestGetVersionNotFound(t *testing.T) {
	s := tempDB(t)
	s.CreateInitialState(DefaultSegmentMap())

	_, err := s.GetVersion("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent version")
	}
}

func TestGetCurrentNoActiveState(t *testing.T) {
	// Fresh DB with schema but no active_state row
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "empty.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	_, err = s.GetCurrent()
	if err == nil {
		t.Fatal("expected error when no active state exists")
	}
}

func TestCommitStateWithMetricsJSON(t *testing.T) {
	s := tempDB(t)
	seg := DefaultSegmentMap()

	v1, err := s.CreateInitialState(seg)
	if err != nil {
		t.Fatalf("CreateInitialState: %v", err)
	}

	v2 := StateRecord{
		VersionID:   "v2-metrics",
		ParentID:    v1.VersionID,
		StateVector: v1.StateVector,
		SegmentMap:  seg,
		CreatedAt:   v1.CreatedAt,
		MetricsJSON: `{"delta_norm":0.5,"segments_hit":["prefs"]}`,
	}

	if err := s.CommitState(v2); err != nil {
		t.Fatalf("CommitState: %v", err)
	}

	// Verify MetricsJSON round-trips
	got, err := s.GetVersion("v2-metrics")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got.MetricsJSON != v2.MetricsJSON {
		t.Fatalf("MetricsJSON mismatch: got %q, want %q", got.MetricsJSON, v2.MetricsJSON)
	}
	if got.ParentID != v1.VersionID {
		t.Fatalf("ParentID mismatch: got %q, want %q", got.ParentID, v1.VersionID)
	}
}

func TestCommitStateNoParent(t *testing.T) {
	s := tempDB(t)
	seg := DefaultSegmentMap()

	v1, _ := s.CreateInitialState(seg)

	// Commit with empty ParentID
	v2 := StateRecord{
		VersionID:   "v2-no-parent",
		StateVector: v1.StateVector,
		SegmentMap:  seg,
		CreatedAt:   v1.CreatedAt,
	}

	if err := s.CommitState(v2); err != nil {
		t.Fatalf("CommitState: %v", err)
	}

	got, err := s.GetVersion("v2-no-parent")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got.ParentID != "" {
		t.Fatalf("expected empty ParentID, got %q", got.ParentID)
	}
}

func TestCreateInitialStateOnClosedDB(t *testing.T) {
	s := tempDB(t)
	s.Close()

	_, err := s.CreateInitialState(DefaultSegmentMap())
	if err == nil {
		t.Fatal("expected error on closed DB")
	}
}

func TestCommitStateOnClosedDB(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "test.db"))
	v1, _ := s.CreateInitialState(DefaultSegmentMap())
	s.Close()

	err := s.CommitState(StateRecord{
		VersionID:   "v2",
		ParentID:    v1.VersionID,
		StateVector: v1.StateVector,
		SegmentMap:  v1.SegmentMap,
		CreatedAt:   v1.CreatedAt,
	})
	if err == nil {
		t.Fatal("expected error on closed DB")
	}
}

func TestRollbackOnClosedDB(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "test.db"))
	v1, _ := s.CreateInitialState(DefaultSegmentMap())
	s.Close()

	err := s.Rollback(v1.VersionID)
	if err == nil {
		t.Fatal("expected error on closed DB")
	}
}

func TestListVersionsOnClosedDB(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "test.db"))
	s.CreateInitialState(DefaultSegmentMap())
	s.Close()

	_, err := s.ListVersions(10)
	if err == nil {
		t.Fatal("expected error on closed DB")
	}
}

func TestGetCurrentOnClosedDB(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "test.db"))
	s.CreateInitialState(DefaultSegmentMap())
	s.Close()

	_, err := s.GetCurrent()
	if err == nil {
		t.Fatal("expected error on closed DB")
	}
}

// corruptDB opens an in-memory SQLite with full schema via NewStoreWithDB.
// Returns the Store and raw *sql.DB so tests can drop tables / insert bad data.
func corruptDB(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	s := NewStoreWithDB(db)
	t.Cleanup(func() { db.Close() })
	return s, db
}

// seedVersion inserts a valid state_versions row and active_state pointer directly.
func seedVersion(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	vec := make([]byte, 128*4)
	for i := 0; i < 128; i++ {
		binary.LittleEndian.PutUint32(vec[i*4:], math.Float32bits(0))
	}
	segJSON := `{"prefs":[0,32],"goals":[32,64],"heuristics":[64,96],"risk":[96,128]}`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(
		`INSERT INTO state_versions (version_id, parent_id, state_vector, segment_map, created_at)
		 VALUES (?, NULL, ?, ?, ?)`, id, vec, segJSON, now,
	)
	if err != nil {
		t.Fatalf("seed version: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO active_state (id, version_id) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET version_id = excluded.version_id`, id,
	)
	if err != nil {
		t.Fatalf("seed active: %v", err)
	}
}

func TestCreateInitialState_InsertFails(t *testing.T) {
	s, db := corruptDB(t)
	db.Exec("DROP TABLE state_versions")

	_, err := s.CreateInitialState(DefaultSegmentMap())
	if err == nil {
		t.Fatal("expected error when state_versions table is missing")
	}
}

func TestCreateInitialState_SetActiveFails(t *testing.T) {
	s, db := corruptDB(t)
	db.Exec("DROP TABLE active_state")

	_, err := s.CreateInitialState(DefaultSegmentMap())
	if err == nil {
		t.Fatal("expected error when active_state table is missing")
	}
}

func TestGetVersion_BadSegmentJSON(t *testing.T) {
	s, db := corruptDB(t)
	vec := make([]byte, 128*4)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.Exec(
		`INSERT INTO state_versions (version_id, parent_id, state_vector, segment_map, created_at)
		 VALUES (?, NULL, ?, ?, ?)`, "bad-json", vec, "not-json", now,
	)

	_, err := s.GetVersion("bad-json")
	if err == nil {
		t.Fatal("expected unmarshal error for bad segment JSON")
	}
}

func TestCommitState_InsertFails(t *testing.T) {
	s, db := corruptDB(t)
	seedVersion(t, db, "v1")
	db.Exec("DROP TABLE state_versions")

	err := s.CommitState(StateRecord{
		VersionID:   "v2",
		ParentID:    "v1",
		StateVector: [128]float32{},
		SegmentMap:  DefaultSegmentMap(),
		CreatedAt:   time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error when state_versions table is missing")
	}
}

func TestCommitState_UpdateActiveFails(t *testing.T) {
	s, db := corruptDB(t)
	seedVersion(t, db, "v1")
	db.Exec("DROP TABLE active_state")

	err := s.CommitState(StateRecord{
		VersionID:   "v2",
		ParentID:    "v1",
		StateVector: [128]float32{},
		SegmentMap:  DefaultSegmentMap(),
		CreatedAt:   time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error when active_state table is missing")
	}
}

func TestRollback_ExecFails(t *testing.T) {
	s, db := corruptDB(t)
	seedVersion(t, db, "v1")
	db.Exec("DROP TABLE active_state")

	err := s.Rollback("v1")
	if err == nil {
		t.Fatal("expected error when active_state table is missing")
	}
}

func TestListVersions_BadSegmentJSON(t *testing.T) {
	s, db := corruptDB(t)
	vec := make([]byte, 128*4)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.Exec(
		`INSERT INTO state_versions (version_id, parent_id, state_vector, segment_map, created_at)
		 VALUES (?, NULL, ?, ?, ?)`, "bad-list", vec, "%%%bad-json", now,
	)

	_, err := s.ListVersions(10)
	if err == nil {
		t.Fatal("expected unmarshal error for bad segment JSON in ListVersions")
	}
}

func TestListVersions_WithMetricsJSON(t *testing.T) {
	s := tempDB(t)
	seg := DefaultSegmentMap()

	v1, err := s.CreateInitialState(seg)
	if err != nil {
		t.Fatalf("CreateInitialState: %v", err)
	}

	v2 := StateRecord{
		VersionID:   "v2-list-metrics",
		ParentID:    v1.VersionID,
		StateVector: v1.StateVector,
		SegmentMap:  seg,
		CreatedAt:   v1.CreatedAt.Add(time.Second),
		MetricsJSON: `{"delta_norm":0.3}`,
	}
	if err := s.CommitState(v2); err != nil {
		t.Fatalf("CommitState: %v", err)
	}

	versions, err := s.ListVersions(10)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}

	// Find the version with metrics
	var found bool
	for _, v := range versions {
		if v.VersionID == "v2-list-metrics" {
			found = true
			if v.MetricsJSON != `{"delta_norm":0.3}` {
				t.Errorf("expected metrics JSON, got %q", v.MetricsJSON)
			}
		}
	}
	if !found {
		t.Fatal("expected to find v2-list-metrics in list")
	}
}


func TestNewStore_CorruptDB(t *testing.T) {
	// Corrupted DB file — sql.Open succeeds but first PRAGMA fails.
	// Covers the PRAGMA journal_mode=WAL error path (L62-63).
	dir, err := os.MkdirTemp("", "state-corrupt-test-*")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	dbPath := filepath.Join(dir, "corrupt.db")
	os.WriteFile(dbPath, []byte("not a sqlite database"), 0644)

	_, err = NewStore(dbPath)
	if err == nil {
		t.Fatal("expected error for corrupted DB file")
	}
	// Best-effort cleanup; may fail on Windows due to leaked DB handle
	os.RemoveAll(dir)
}

func TestNewStore_PragmaFails(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("os.Chmod(0444) does not prevent writes on Windows")
	}

	// Create a read-only DB file to trigger PRAGMA journal_mode=WAL failure
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "readonly.db")

	// Create a valid DB first — must exec something to force file creation
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE dummy (id INTEGER)"); err != nil {
		t.Fatalf("seed db: %v", err)
	}
	db.Close()

	// Make it read-only
	if err := os.Chmod(dbPath, 0444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dbPath, 0644) })

	_, err = NewStore(dbPath)
	if err == nil {
		t.Fatal("expected error for read-only DB pragma")
	}
}
