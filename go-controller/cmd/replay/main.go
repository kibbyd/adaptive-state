package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/logging"
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

// legacySignalsJSON mirrors the legacy JSON structure from json.Marshal(updateCtx).
// Legacy format uses Go default PascalCase keys: TurnID, Prompt, ResponseText, Entropy.
type legacySignalsJSON struct {
	TurnID       string  `json:"TurnID"`
	Prompt       string  `json:"Prompt"`
	ResponseText string  `json:"ResponseText"`
	Entropy      float32 `json:"Entropy"`
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

// toInteraction converts a provenance row to a replay Interaction.
// Tries GateRecord format first (full fidelity); falls back to legacy heuristics.
func toInteraction(r provenanceRow) replay.Interaction {
	inter := replay.Interaction{
		TurnID: r.TurnID,
	}

	if r.SignalsJSON == "" {
		return inter
	}

	// Try GateRecord format first (new full-fidelity logging).
	// Discriminator: GateRecord has "turn_id" (snake_case), legacy has "TurnID" (PascalCase).
	var gr logging.GateRecord
	if err := json.Unmarshal([]byte(r.SignalsJSON), &gr); err == nil && gr.TurnID != "" {
		inter.TurnID = gr.TurnID
		inter.Prompt = gr.Prompt
		inter.ResponseText = gr.Response
		inter.Entropy = gr.Entropy
		inter.Signals = update.Signals{
			SentimentScore:      gr.Signals.SentimentScore,
			CoherenceScore:      gr.Signals.CoherenceScore,
			NoveltyScore:        gr.Signals.NoveltyScore,
			RiskFlag:            gr.Signals.RiskFlag,
			UserCorrection:      gr.Signals.UserCorrection,
			ToolFailure:         gr.Signals.ToolFailure,
			ConstraintViolation: gr.Signals.ConstraintViolation,
		}
		return inter
	}

	// Legacy format: heuristic reconstruction from json.Marshal(UpdateContext)
	var legacy legacySignalsJSON
	if err := json.Unmarshal([]byte(r.SignalsJSON), &legacy); err == nil && legacy.Prompt != "" {
		inter.TurnID = legacy.TurnID
		inter.Prompt = legacy.Prompt
		inter.ResponseText = legacy.ResponseText
		inter.Entropy = legacy.Entropy
		inter.Signals = heuristicSignals(legacy)
		return inter
	}

	return inter
}

// #endregion db-extract

// #region heuristic-signals

// heuristicSignals approximates update.Signals from legacy provenance data.
// Legacy rows only stored UpdateContext (prompt, response, entropy) — no signal values.
func heuristicSignals(legacy legacySignalsJSON) update.Signals {
	var s update.Signals

	// Approximate sentiment from response word count
	if legacy.ResponseText != "" {
		wordCount := float32(len(strings.Fields(legacy.ResponseText)))
		s.SentimentScore = wordCount / 100.0
		if s.SentimentScore > 1.0 {
			s.SentimentScore = 1.0
		}
	}

	// Approximate novelty from entropy
	if legacy.Entropy > 0 {
		s.NoveltyScore = legacy.Entropy
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
