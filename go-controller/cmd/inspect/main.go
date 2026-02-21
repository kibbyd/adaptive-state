package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/logging"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	_ "modernc.org/sqlite"
)

// #region main

func main() {
	dbPath := flag.String("db", "", "path to adaptive_state.db")
	last := flag.Int("last", 20, "show N most recent versions")
	version := flag.String("version", "", "show single version detail")
	segment := flag.String("segment", "", "filter segment breakdown to one segment")
	jsonOut := flag.Bool("json", false, "output as JSON instead of table")
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "usage: inspect --db path/to/adaptive_state.db [--last N] [--version id] [--segment name] [--json]")
		os.Exit(2)
	}

	store, err := state.NewStore(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	if *version != "" {
		if err := runDetailMode(store, *version, *segment, *jsonOut); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := runListMode(store, *last, *segment, *jsonOut); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

// #endregion main

// #region list-mode

type listRow struct {
	VersionID string             `json:"version_id"`
	StateNorm float64            `json:"state_norm"`
	DeltaNorm *float64           `json:"delta_norm,omitempty"`
	Decision  string             `json:"decision"`
	Reason    string             `json:"reason,omitempty"`
	Score     float32            `json:"score"`
	CreatedAt string             `json:"created_at"`
	Segments  map[string]float64 `json:"segments"`
	SegNorm   *float64           `json:"seg_norm,omitempty"`
}

func runListMode(store *state.Store, last int, segFilter string, jsonOut bool) error {
	versions, err := store.ListVersionsWithProvenance(last)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		fmt.Fprintln(os.Stderr, "no versions found")
		return nil
	}

	// Build rows (store returns DESC, reverse for chronological)
	listRows := make([]listRow, len(versions))
	for i, vp := range versions {
		segs := computeSegmentNorms(vp.StateVector, vp.SegmentMap)
		lr := listRow{
			VersionID: vp.VersionID,
			StateNorm: fullVectorNorm(vp.StateVector),
			Decision:  vp.Decision,
			Reason:    vp.Reason,
			Score:     verifierScore(vp.Decision, vp.Reason),
			CreatedAt: vp.CreatedAt.Format("2006-01-02T15:04:05Z"),
			Segments:  segs,
		}
		if gr := parseGateRecord(vp.SignalsJSON); gr != nil {
			dn := float64(gr.DeltaNorm)
			lr.DeltaNorm = &dn
		}
		if segFilter != "" {
			if v, ok := segs[segFilter]; ok {
				lr.SegNorm = &v
			}
		}
		listRows[len(versions)-1-i] = lr
	}

	if jsonOut {
		return printJSON(listRows)
	}
	return printListTable(listRows, segFilter)
}

func printListTable(rows []listRow, segFilter string) error {
	if segFilter != "" {
		fmt.Printf("%-12s  %10s  %8s  %-10s  %6s  %-8s  %s\n",
			"Version", "State Norm", "Delta", "Decision", "Score", "Seg Norm", "Time")
		fmt.Printf("%-12s+-%10s+-%8s+-%-10s+-%6s+-%-8s+-%s\n",
			"------------", "----------", "--------", "----------", "------", "--------", "--------------------")
	} else {
		fmt.Printf("%-12s  %10s  %8s  %-10s  %6s  %s\n",
			"Version", "State Norm", "Delta", "Decision", "Score", "Time")
		fmt.Printf("%-12s+-%10s+-%8s+-%-10s+-%6s+-%s\n",
			"------------", "----------", "--------", "----------", "------", "--------------------")
	}

	for _, r := range rows {
		vid := shortID(r.VersionID)
		delta := "—"
		if r.DeltaNorm != nil {
			delta = fmt.Sprintf("%.4f", *r.DeltaNorm)
		}
		if segFilter != "" {
			segVal := "—"
			if r.SegNorm != nil {
				segVal = fmt.Sprintf("%.4f", *r.SegNorm)
			}
			fmt.Printf("%-12s  %10.4f  %8s  %-10s  %6.2f  %-8s  %s\n",
				vid, r.StateNorm, delta, r.Decision, r.Score, segVal, r.CreatedAt)
		} else {
			fmt.Printf("%-12s  %10.4f  %8s  %-10s  %6.2f  %s\n",
				vid, r.StateNorm, delta, r.Decision, r.Score, r.CreatedAt)
		}
	}

	latest := rows[len(rows)-1]
	fmt.Printf("\nSegment norms (latest):\n")
	printSegments(latest.Segments, "")
	return nil
}

// #endregion list-mode

// #region detail-mode

type detailOutput struct {
	VersionID  string             `json:"version_id"`
	ParentID   string             `json:"parent_id"`
	CreatedAt  string             `json:"created_at"`
	StateNorm  float64            `json:"state_norm"`
	Decision   string             `json:"decision"`
	Reason     string             `json:"reason"`
	Score      float32            `json:"score"`
	Segments   map[string]float64 `json:"segments"`
	GateRecord *gateDetail        `json:"gate_record,omitempty"`
}

type gateDetail struct {
	DeltaNorm float32 `json:"delta_norm"`
	Entropy   float32 `json:"entropy"`
	Vetoed    bool    `json:"vetoed"`
	SoftScore float32 `json:"soft_score"`
}

func runDetailMode(store *state.Store, versionID, segFilter string, jsonOut bool) error {
	vp, err := store.GetVersionWithProvenance(versionID)
	if err != nil {
		return err
	}

	segs := computeSegmentNorms(vp.StateVector, vp.SegmentMap)
	out := detailOutput{
		VersionID: vp.VersionID,
		ParentID:  vp.ParentID,
		CreatedAt: vp.CreatedAt.Format("2006-01-02T15:04:05Z"),
		StateNorm: fullVectorNorm(vp.StateVector),
		Decision:  vp.Decision,
		Reason:    vp.Reason,
		Score:     verifierScore(vp.Decision, vp.Reason),
		Segments:  segs,
	}

	if gr := parseGateRecord(vp.SignalsJSON); gr != nil {
		out.GateRecord = &gateDetail{
			DeltaNorm: gr.DeltaNorm,
			Entropy:   gr.Entropy,
			Vetoed:    gr.GateVetoed,
			SoftScore: gr.GateSoftScore,
		}
	}

	if jsonOut {
		return printJSON(out)
	}

	fmt.Printf("Version:    %s\n", out.VersionID)
	fmt.Printf("Parent:     %s\n", out.ParentID)
	fmt.Printf("Created:    %s\n", out.CreatedAt)
	fmt.Printf("State Norm: %.4f\n", out.StateNorm)
	fmt.Printf("Decision:   %s\n", out.Decision)
	fmt.Printf("Reason:     %s\n", out.Reason)
	fmt.Printf("Score:      %.2f\n", out.Score)

	fmt.Printf("\nSegment norms:\n")
	printSegments(segs, segFilter)

	if out.GateRecord != nil {
		fmt.Printf("\nGate Record:\n")
		fmt.Printf("  Delta Norm:  %.4f\n", out.GateRecord.DeltaNorm)
		fmt.Printf("  Entropy:     %.2f\n", out.GateRecord.Entropy)
		fmt.Printf("  Vetoed:      %v\n", out.GateRecord.Vetoed)
		fmt.Printf("  Soft Score:  %.2f\n", out.GateRecord.SoftScore)
	}

	return nil
}

// #endregion detail-mode

// #region metrics

func fullVectorNorm(v [128]float32) float64 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	return math.Sqrt(sum)
}

func segmentNorm(v [128]float32, start, end int) float64 {
	var sum float64
	for i := start; i < end && i < len(v); i++ {
		sum += float64(v[i]) * float64(v[i])
	}
	return math.Sqrt(sum)
}

func computeSegmentNorms(v [128]float32, sm state.SegmentMap) map[string]float64 {
	return map[string]float64{
		"prefs":      segmentNorm(v, sm.Prefs[0], sm.Prefs[1]),
		"goals":      segmentNorm(v, sm.Goals[0], sm.Goals[1]),
		"heuristics": segmentNorm(v, sm.Heuristics[0], sm.Heuristics[1]),
		"risk":       segmentNorm(v, sm.Risk[0], sm.Risk[1]),
	}
}

// #endregion metrics

// #region verifier

func verifierScore(decision, reason string) float32 {
	switch decision {
	case "commit":
		return 1.0
	case "reject":
		if strings.Contains(reason, "eval rollback") {
			return -1.0
		}
		return 0.0
	case "no_op":
		return 0.5
	default:
		return 0.0
	}
}

// #endregion verifier

// #region output

func parseGateRecord(signalsJSON string) *logging.GateRecord {
	if signalsJSON == "" {
		return nil
	}
	var gr logging.GateRecord
	if err := json.Unmarshal([]byte(signalsJSON), &gr); err == nil && gr.TurnID != "" {
		return &gr
	}
	return nil
}

func printSegments(segs map[string]float64, filter string) {
	order := []string{"prefs", "goals", "heuristics", "risk"}
	for _, name := range order {
		if filter != "" && name != filter {
			continue
		}
		fmt.Printf("  %-12s %.4f\n", name, segs[name])
	}
}

func printJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// #endregion output
