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
	_ "modernc.org/sqlite"
)

// #region main

func main() {
	dbPath := flag.String("db", "", "path to adaptive_state.db")
	last := flag.Int("last", 4, "number of most recent provenance rows to export")
	outPath := flag.String("out", "", "output fixture JSON path")
	flag.Parse()

	if *dbPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: fixture-export --db path/to/db --out path/to/fixture.json [--last N]")
		os.Exit(2)
	}

	if err := run(*dbPath, *last, *outPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// #endregion main

// #region extract

// gateRow holds a parsed provenance row with its GateRecord.
type gateRow struct {
	Record   logging.GateRecord
	Decision string
	Reason   string
}

func run(dbPath string, last int, outPath string) error {
	store, err := state.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	db := store.DB()

	// Get initial state (first version with no parent)
	var initVersionID string
	err = db.QueryRow(
		`SELECT version_id FROM state_versions WHERE parent_id IS NULL ORDER BY created_at ASC LIMIT 1`,
	).Scan(&initVersionID)
	if err != nil {
		return fmt.Errorf("find initial state: %w", err)
	}

	startState, err := store.GetVersion(initVersionID)
	if err != nil {
		return fmt.Errorf("get initial state: %w", err)
	}

	// Query last N user_turn rows (DESC then reverse for chronological order)
	rows, err := db.Query(
		`SELECT signals_json, decision, reason FROM (
			SELECT signals_json, decision, reason, created_at FROM provenance_log
			WHERE trigger_type = 'user_turn'
			ORDER BY created_at DESC LIMIT ?
		) sub ORDER BY created_at ASC`, last,
	)
	if err != nil {
		return fmt.Errorf("query provenance: %w", err)
	}
	defer rows.Close()

	var gateRows []gateRow
	for rows.Next() {
		var sigJSON sql.NullString
		var decision string
		var reason sql.NullString
		if err := rows.Scan(&sigJSON, &decision, &reason); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		if !sigJSON.Valid || sigJSON.String == "" {
			continue
		}

		var gr logging.GateRecord
		if err := json.Unmarshal([]byte(sigJSON.String), &gr); err != nil {
			continue
		}
		if gr.TurnID == "" {
			continue // not GateRecord format
		}

		reasonStr := ""
		if reason.Valid {
			reasonStr = reason.String
		}

		gateRows = append(gateRows, gateRow{
			Record:   gr,
			Decision: decision,
			Reason:   reasonStr,
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	if len(gateRows) == 0 {
		return fmt.Errorf("no GateRecord-format rows found in last %d user_turn entries", last)
	}

	fmt.Printf("Found %d GateRecord rows\n", len(gateRows))

	// Build fixture
	fixture := buildFixture(startState, gateRows)

	return writeFixture(fixture, outPath)
}

// #endregion extract

// #region output

func buildFixture(startState state.StateRecord, rows []gateRow) replay.Fixture {
	interactions := make([]replay.FixtureInteraction, len(rows))
	expected := make([]replay.FixtureExpectedResult, len(rows))

	for i, r := range rows {
		interactions[i] = replay.FixtureInteraction{
			TurnID:       r.Record.TurnID,
			Prompt:       r.Record.Prompt,
			ResponseText: r.Record.Response,
			Entropy:      r.Record.Entropy,
			Signals: replay.FixtureSignals{
				SentimentScore:      r.Record.Signals.SentimentScore,
				NoveltyScore:        r.Record.Signals.NoveltyScore,
				CoherenceScore:      r.Record.Signals.CoherenceScore,
				RiskFlag:            r.Record.Signals.RiskFlag,
				UserCorrection:      r.Record.Signals.UserCorrection,
				ToolFailure:         r.Record.Signals.ToolFailure,
				ConstraintViolation: r.Record.Signals.ConstraintViolation,
			},
			Evidence: []string{},
		}

		expected[i] = replay.FixtureExpectedResult{
			TurnID: r.Record.TurnID,
			Action: mapAction(r.Decision, r.Reason),
		}
	}

	// Build config from first row's thresholds + default update config
	th := rows[0].Record.Thresholds
	fixture := replay.Fixture{
		Description: fmt.Sprintf("Real session export: %d GateRecord turns from production DB", len(rows)),
		StartState: replay.FixtureStartState{
			VersionID:   startState.VersionID,
			StateVector: startState.StateVector,
			SegmentMap:  startState.SegmentMap,
		},
		Config: replay.FixtureConfig{
			UpdateConfig: replay.FixtureUpdateConfig{
				LearningRate:           0.01,
				DecayRate:              0.005,
				MaxDeltaNormPerSegment: 1.0,
			},
			GateConfig: replay.FixtureGateConfig{
				MaxDeltaNorm:   th.MaxDeltaNorm,
				MaxStateNorm:   th.MaxStateNorm,
				MinEntropyDrop: 0.1,
				RiskSegmentCap: th.RiskSegmentCap,
			},
			EvalConfig: replay.FixtureEvalConfig{
				MaxStateNorm:    th.MaxStateNorm,
				MaxSegmentNorm:  th.MaxSegmentNorm,
				EntropyBaseline: 2.0,
			},
		},
		Interactions:    interactions,
		ExpectedResults: expected,
	}

	return fixture
}

// mapAction converts DB decision + reason to fixture action string.
func mapAction(decision, reason string) string {
	switch decision {
	case "commit":
		return "commit"
	case "reject":
		if strings.Contains(reason, "eval rollback") {
			return "eval_rollback"
		}
		return "gate_reject"
	case "no_op":
		return "no_op"
	default:
		return decision
	}
}

func writeFixture(fixture replay.Fixture, outPath string) error {
	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal fixture: %w", err)
	}

	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}

	fmt.Printf("Wrote fixture to %s (%d bytes, %d interactions)\n", outPath, len(data), len(fixture.Interactions))
	return nil
}

// #endregion output
