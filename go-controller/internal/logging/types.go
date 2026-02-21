package logging

import "time"

// #region provenance-entry
// ProvenanceEntry is a single row in the provenance_log table.
type ProvenanceEntry struct {
	VersionID    string
	ContextHash  string
	TriggerType  string
	SignalsJSON  string
	EvidenceRefs string
	Decision     string // "commit" | "reject" | "no_op"
	Reason       string
	CreatedAt    time.Time
}
// #endregion provenance-entry
