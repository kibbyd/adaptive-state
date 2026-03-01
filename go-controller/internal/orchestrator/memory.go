package orchestrator

// #region imports
import (
	"database/sql"
	"math"
	"time"
)

// #endregion

// #region schema

const strategyOutcomesSchema = `
CREATE TABLE IF NOT EXISTS strategy_outcomes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    turn_id       TEXT NOT NULL,
    turn_type     TEXT NOT NULL,
    complexity    TEXT NOT NULL,
    risk          TEXT NOT NULL,
    strategy_id   TEXT NOT NULL,
    attempt_num   INTEGER NOT NULL,
    quality       REAL NOT NULL,
    failure_type  TEXT NOT NULL DEFAULT 'none',
    entropy       REAL NOT NULL,
    gate_score    REAL NOT NULL DEFAULT 0,
    accepted      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
);
`

const strategyOutcomesIndex = `
CREATE INDEX IF NOT EXISTS idx_strategy_outcomes_lookup
ON strategy_outcomes(turn_type, complexity, risk, strategy_id);
`

// #endregion

// #region memory-struct

// StrategyMemory persists strategy outcomes in SQLite and queries decay-weighted results.
type StrategyMemory struct {
	db *sql.DB
}

// NewStrategyMemory initializes the strategy_outcomes table and returns a StrategyMemory.
func NewStrategyMemory(db *sql.DB) (*StrategyMemory, error) {
	if _, err := db.Exec(strategyOutcomesSchema); err != nil {
		return nil, err
	}
	if _, err := db.Exec(strategyOutcomesIndex); err != nil {
		return nil, err
	}
	return &StrategyMemory{db: db}, nil
}

// #endregion

// #region record-outcome

// RecordOutcome persists a single strategy outcome row.
func (m *StrategyMemory) RecordOutcome(rec OutcomeRecord) error {
	accepted := 0
	if rec.Accepted {
		accepted = 1
	}
	_, err := m.db.Exec(`
		INSERT INTO strategy_outcomes
		(turn_id, turn_type, complexity, risk, strategy_id, attempt_num,
		 quality, failure_type, entropy, gate_score, accepted, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.TurnID,
		string(rec.TurnType),
		string(rec.Complexity),
		string(rec.Risk),
		string(rec.StrategyID),
		rec.AttemptNum,
		rec.Quality,
		string(rec.FailureType),
		rec.Entropy,
		rec.GateScore,
		accepted,
		rec.CreatedAt.Format(time.RFC3339),
	)
	return err
}

// #endregion

// #region best-strategy

// BestStrategy returns the strategy with the highest decay-weighted quality
// for the given turn classification. Returns ("", 0, nil) if fewer than 3 samples.
func (m *StrategyMemory) BestStrategy(turnType, complexity, risk string) (StrategyID, float32, error) {
	rows, err := m.db.Query(`
		SELECT strategy_id, quality, created_at
		FROM strategy_outcomes
		WHERE turn_type = ? AND complexity = ? AND risk = ? AND accepted = 1`,
		turnType, complexity, risk,
	)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()

	type stratAccum struct {
		weightedSum float64
		totalWeight float64
		count       int
	}

	now := time.Now()
	halfLife := 7.0 * 24.0 // 7 days in hours
	accum := make(map[StrategyID]*stratAccum)

	for rows.Next() {
		var sid string
		var quality float64
		var createdAtStr string
		if err := rows.Scan(&sid, &quality, &createdAtStr); err != nil {
			return "", 0, err
		}
		createdAt, err := time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			continue
		}
		ageHours := now.Sub(createdAt).Hours()
		weight := math.Exp(-ageHours / halfLife)

		stratID := StrategyID(sid)
		if _, ok := accum[stratID]; !ok {
			accum[stratID] = &stratAccum{}
		}
		accum[stratID].weightedSum += quality * weight
		accum[stratID].totalWeight += weight
		accum[stratID].count++
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}

	var bestID StrategyID
	var bestScore float64 = -1

	for sid, a := range accum {
		if a.count < 3 {
			continue
		}
		avg := a.weightedSum / a.totalWeight
		if avg > bestScore {
			bestScore = avg
			bestID = sid
		}
	}

	return bestID, float32(bestScore), nil
}

// #endregion
