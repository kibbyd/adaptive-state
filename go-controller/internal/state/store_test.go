package state

import (
	"os"
	"path/filepath"
	"testing"
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
