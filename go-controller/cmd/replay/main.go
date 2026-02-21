package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/replay"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
	_ "modernc.org/sqlite"
)

// #region main

func main() {
	dbPath := flag.String("db", "", "path to adaptive_state.db (DB mode)")
	fixturePath := flag.String("fixture", "", "path to fixture JSON (fixture mode)")
	flag.Parse()

	if (*dbPath == "" && *fixturePath == "") || (*dbPath != "" && *fixturePath != "") {
		fmt.Fprintln(os.Stderr, "usage: replay --db path/to/adaptive_state.db")
		fmt.Fprintln(os.Stderr, "       replay --fixture path/to/fixture.json")
		os.Exit(2)
	}

	var exitCode int
	if *fixturePath != "" {
		exitCode = runFixtureMode(*fixturePath)
	} else {
		exitCode = runDBMode(*dbPath)
	}
	os.Exit(exitCode)
}

// #endregion main

// #region db-extract

// provenanceRow represents a row from the provenance_log table.
type provenanceRow struct {
	TurnID      string // version_id used as turn identifier
	SignalsJSON string
	Decision    string
}

// signalsJSON mirrors the JSON structure stored in provenance_log.signals_json.
type signalsJSON struct {
	Prompt       string  `json:"prompt"`
	Response     string  `json:"response"`
	Entropy      float32 `json:"entropy"`
	Sentiment    float32 `json:"sentiment_score"`
	Coherence    float32 `json:"coherence_score"`
	Novelty      float32 `json:"novelty_score"`
	RiskFlag     bool    `json:"risk_flag"`
	UserCorrect  bool    `json:"user_correction"`
	ToolFail     bool    `json:"tool_failure"`
	Constraint   bool    `json:"constraint_violation"`
}

func runDBMode(dbPath string) int {
	store, err := state.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		return 2
	}
	defer store.Close()

	db := store.DB()

	// Get initial state (first version with no parent)
	var initVersionID string
	err = db.QueryRow(
		`SELECT version_id FROM state_versions WHERE parent_id IS NULL ORDER BY created_at ASC LIMIT 1`,
	).Scan(&initVersionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "find initial state: %v\n", err)
		return 2
	}

	startState, err := store.GetVersion(initVersionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get initial state: %v\n", err)
		return 2
	}

	// Query provenance_log for user_turn entries
	rows, err := db.Query(
		`SELECT version_id, signals_json, decision FROM provenance_log
		 WHERE trigger_type = 'user_turn' ORDER BY created_at ASC`,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query provenance: %v\n", err)
		return 2
	}
	defer rows.Close()

	var provRows []provenanceRow
	for rows.Next() {
		var r provenanceRow
		var sigJSON sql.NullString
		if err := rows.Scan(&r.TurnID, &sigJSON, &r.Decision); err != nil {
			fmt.Fprintf(os.Stderr, "scan row: %v\n", err)
			return 2
		}
		if sigJSON.Valid {
			r.SignalsJSON = sigJSON.String
		}
		provRows = append(provRows, r)
	}
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "iterate rows: %v\n", err)
		return 2
	}

	if len(provRows) == 0 {
		fmt.Fprintln(os.Stderr, "no user_turn entries found in provenance_log")
		return 2
	}

	// Convert to replay interactions with heuristic signals
	interactions := make([]replay.Interaction, len(provRows))
	dbDecisions := make([]string, len(provRows))
	for i, r := range provRows {
		interactions[i] = toInteraction(r)
		dbDecisions[i] = r.Decision
	}

	// Replay with default config
	config := replay.DefaultReplayConfig()
	results := replay.Replay(startState, interactions, config)

	// Print comparison table
	return printComparison(results, dbDecisions, nil)
}

// toInteraction converts a provenance row to a replay Interaction using heuristic signals.
func toInteraction(r provenanceRow) replay.Interaction {
	inter := replay.Interaction{
		TurnID: r.TurnID,
	}

	if r.SignalsJSON != "" {
		var sig signalsJSON
		if err := json.Unmarshal([]byte(r.SignalsJSON), &sig); err == nil {
			inter.Prompt = sig.Prompt
			inter.ResponseText = sig.Response
			inter.Entropy = sig.Entropy
			inter.Signals = heuristicSignals(sig)
			return inter
		}
	}

	// Fallback: minimal signals
	return inter
}

// #endregion db-extract

// #region heuristic-signals

// heuristicSignals extracts update.Signals from parsed provenance JSON.
// If explicit signal fields are present, use them; otherwise approximate.
func heuristicSignals(sig signalsJSON) update.Signals {
	s := update.Signals{
		SentimentScore:      sig.Sentiment,
		CoherenceScore:      sig.Coherence,
		NoveltyScore:        sig.Novelty,
		RiskFlag:            sig.RiskFlag,
		UserCorrection:      sig.UserCorrect,
		ToolFailure:         sig.ToolFail,
		ConstraintViolation: sig.Constraint,
	}

	// Heuristic approximations when explicit values are zero
	if s.SentimentScore == 0 && sig.Response != "" {
		wordCount := float32(len(strings.Fields(sig.Response)))
		s.SentimentScore = wordCount / 100.0
		if s.SentimentScore > 1.0 {
			s.SentimentScore = 1.0
		}
	}
	if s.NoveltyScore == 0 && sig.Entropy > 0 {
		s.NoveltyScore = sig.Entropy
	}

	return s
}

// #endregion heuristic-signals

// #region output

func runFixtureMode(path string) int {
	f, err := replay.LoadFixture(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load fixture: %v\n", err)
		return 2
	}

	startState := f.StartState.ToStateRecord()
	config := f.Config.ToReplayConfig()

	interactions := make([]replay.Interaction, len(f.Interactions))
	for i := range f.Interactions {
		interactions[i] = f.Interactions[i].ToInteraction()
	}

	results := replay.Replay(startState, interactions, config)

	expected := make([]string, len(f.ExpectedResults))
	for i, e := range f.ExpectedResults {
		expected[i] = e.Action
	}

	return printComparison(results, expected, nil)
}

// printComparison outputs a comparison table and returns exit code.
// expected holds the reference actions (from DB or fixture).
// turnIDs can be nil (uses result TurnIDs).
func printComparison(results []replay.ReplayResult, expected []string, turnIDs []string) int {
	fmt.Printf("%-12s| %-15s| %-15s| %s\n", "Turn", "Expected", "Replayed", "Match")
	fmt.Printf("%-12s+%-15s+%-15s+%s\n",
		"------------", "----------------", "----------------", "------")

	matches := 0
	total := len(results)
	if len(expected) < total {
		total = len(expected)
	}

	for i := 0; i < total; i++ {
		turnID := results[i].TurnID
		if turnIDs != nil && i < len(turnIDs) {
			turnID = turnIDs[i]
		}

		exp := expected[i]
		got := results[i].Action
		match := "DIFF"

		if actionsMatch(exp, got) {
			match = "OK"
			matches++
		}

		fmt.Printf("%-12s| %-15s| %-15s| %s\n", turnID, exp, got, match)
	}

	diverge := total - matches
	fmt.Printf("\nSummary: %d total, %d match, %d diverge\n", total, matches, diverge)

	if diverge > 0 {
		return 1
	}
	return 0
}

// actionsMatch compares expected vs replayed action.
// DB "reject" matches either "gate_reject" or "eval_rollback".
func actionsMatch(expected, replayed string) bool {
	if expected == replayed {
		return true
	}
	if expected == "reject" && (replayed == "gate_reject" || replayed == "eval_rollback") {
		return true
	}
	return false
}

// #endregion output
