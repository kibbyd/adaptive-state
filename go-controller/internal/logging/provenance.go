package logging

import (
	"database/sql"
	"fmt"
	"time"
)

// #region log-decision
// LogDecision writes a provenance entry to the provenance_log table.
func LogDecision(db *sql.DB, entry ProvenanceEntry) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}

	_, err := db.Exec(
		`INSERT INTO provenance_log (version_id, context_hash, trigger_type, signals_json, evidence_refs, decision, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.VersionID,
		nullIfEmpty(entry.ContextHash),
		entry.TriggerType,
		nullIfEmpty(entry.SignalsJSON),
		nullIfEmpty(entry.EvidenceRefs),
		entry.Decision,
		nullIfEmpty(entry.Reason),
		entry.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("log decision: %w", err)
	}
	return nil
}
// #endregion log-decision

// #region helpers
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
// #endregion helpers
