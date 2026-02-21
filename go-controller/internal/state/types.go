package state

import "time"

// #region state-record
// StateRecord represents a versioned snapshot of the disposition state vector.
type StateRecord struct {
	VersionID   string
	ParentID    string
	StateVector [128]float32
	SegmentMap  SegmentMap
	CreatedAt   time.Time
	MetricsJSON string
}
// #endregion state-record

// #region segment-map
// SegmentMap defines named ranges within the 128-dimensional state vector.
type SegmentMap struct {
	Prefs      [2]int `json:"prefs"`      // [0, 32)
	Goals      [2]int `json:"goals"`      // [32, 64)
	Heuristics [2]int `json:"heuristics"` // [64, 96)
	Risk       [2]int `json:"risk"`       // [96, 128)
}

// DefaultSegmentMap returns the standard 4-segment layout.
func DefaultSegmentMap() SegmentMap {
	return SegmentMap{
		Prefs:      [2]int{0, 32},
		Goals:      [2]int{32, 64},
		Heuristics: [2]int{64, 96},
		Risk:       [2]int{96, 128},
	}
}
// #endregion segment-map

// #region provenance-tag
// ProvenanceTag links a state version to its decision context.
type ProvenanceTag struct {
	VersionID    string
	ContextHash  string
	TriggerType  string
	SignalsJSON  string
	EvidenceRefs string
	Decision     string // "commit" | "reject" | "no_op"
	Reason       string
	CreatedAt    time.Time
}
// #endregion provenance-tag

// #region version-with-provenance
// VersionWithProvenance pairs a state version with its provenance row fields.
type VersionWithProvenance struct {
	StateRecord
	Decision    string
	Reason      string
	SignalsJSON string
}
// #endregion version-with-provenance
