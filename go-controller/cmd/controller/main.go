package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/codec"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/eval"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/gate"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/logging"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/retrieval"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
)

// #region main
func main() {
	dbPath := envOr("ADAPTIVE_DB", "adaptive_state.db")
	grpcAddr := envOr("CODEC_ADDR", "localhost:50051")

	// Initialize state store
	store, err := state.NewStore(dbPath)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Ensure initial state exists
	_, err = store.GetCurrent()
	if err != nil {
		log.Println("No active state found, creating initial state...")
		_, err = store.CreateInitialState(state.DefaultSegmentMap())
		if err != nil {
			log.Fatalf("failed to create initial state: %v", err)
		}
	}

	// Connect to Python inference service
	codecClient, err := codec.NewCodecClient(grpcAddr)
	if err != nil {
		log.Fatalf("failed to connect to codec service at %s: %v", grpcAddr, err)
	}
	defer codecClient.Close()

	// Initialize retriever with triple-gated config
	retriever := retrieval.NewRetriever(codecClient, retrieval.DefaultConfig())

	// Phase 3: Initialize gate and eval harness
	stateGate := gate.NewGate(gate.DefaultGateConfig())
	evalHarness := eval.NewEvalHarness(eval.DefaultEvalConfig())

	// Phase 4: Update config for learning + decay
	updateConfig := update.DefaultUpdateConfig()

	fmt.Println("Adaptive State Controller ready.")
	fmt.Printf("  DB: %s | Codec: %s\n", dbPath, grpcAddr)
	fmt.Println("Type a prompt (or 'quit' to exit):")

	scanner := bufio.NewScanner(os.Stdin)
	turnNum := 0

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		prompt := strings.TrimSpace(scanner.Text())
		if prompt == "" {
			continue
		}
		if prompt == "quit" || prompt == "exit" {
			break
		}

		turnNum++
		turnID := fmt.Sprintf("turn-%d", turnNum)

		// Step 1: Get current state
		current, err := store.GetCurrent()
		if err != nil {
			log.Printf("error getting current state: %v", err)
			continue
		}

		// Step 2: First-pass Generate to get entropy
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result, err := codecClient.Generate(ctx, prompt, current.StateVector, nil)
		cancel()
		if err != nil {
			log.Printf("codec error: %v", err)
			continue
		}

		// Step 3: Triple-gated retrieval (if entropy warrants it)
		var evidenceStrings []string
		var evidenceRefs []string
		ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
		gateResult, err := retriever.Retrieve(ctx2, prompt, result.Entropy)
		cancel2()
		if err != nil {
			log.Printf("retrieval error (non-fatal): %v", err)
		} else if len(gateResult.Retrieved) > 0 {
			for _, ev := range gateResult.Retrieved {
				evidenceStrings = append(evidenceStrings, ev.Text)
				evidenceRefs = append(evidenceRefs, ev.ID)
			}
			log.Printf("[%s] retrieval: %s", turnID, gateResult.Reason)

			// Re-generate with evidence injected
			ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
			result, err = codecClient.Generate(ctx3, prompt, current.StateVector, evidenceStrings)
			cancel3()
			if err != nil {
				log.Printf("re-generate error: %v", err)
				continue
			}
		} else {
			log.Printf("[%s] retrieval: %s", turnID, gateResult.Reason)
		}

		fmt.Printf("\n%s\n\n", result.Text)

		// Step 4: Store this interaction as evidence for future retrieval
		storeText := fmt.Sprintf("Q: %s\nA: %s", prompt, result.Text)
		metadataJSON := fmt.Sprintf(`{"turn_id":"%s","entropy":%.4f}`, turnID, result.Entropy)
		ctx4, cancel4 := context.WithTimeout(context.Background(), 10*time.Second)
		_, storeErr := codecClient.StoreEvidence(ctx4, storeText, metadataJSON)
		cancel4()
		if storeErr != nil {
			log.Printf("store evidence error (non-fatal): %v", storeErr)
		}

		// Step 5: Run update function (produces proposed state + metrics)
		updateCtx := update.UpdateContext{
			TurnID:       turnID,
			Prompt:       prompt,
			ResponseText: result.Text,
			Entropy:      result.Entropy,
		}
		// Phase 4: Entropy drives Risk segment; other signals remain 0 until producers exist
		signals := update.Signals{}
		updateResult := update.Update(current, updateCtx, signals, evidenceStrings, updateConfig)

		// Step 6: Gate evaluation — hard vetoes + soft scoring
		gateDecision := stateGate.Evaluate(
			current, updateResult.NewState, signals, updateResult.Metrics, result.Entropy,
		)

		if gateDecision.Action == "reject" {
			// Gate rejected: log rejection, keep old state, continue
			log.Printf("[%s] gate rejected: %s", turnID, gateDecision.Reason)
			signalsJSON, _ := json.Marshal(updateCtx)
			_ = logging.LogDecision(store.DB(), logging.ProvenanceEntry{
				VersionID:    current.VersionID,
				TriggerType:  "user_turn",
				SignalsJSON:  string(signalsJSON),
				EvidenceRefs: strings.Join(evidenceRefs, ","),
				Decision:     "reject",
				Reason:       fmt.Sprintf("gate: %s", gateDecision.Reason),
				CreatedAt:    time.Now().UTC(),
			})
			fmt.Printf("[%s] decision=reject (gate) entropy=%.4f evidence=%d\n",
				turnID, result.Entropy, len(evidenceStrings))
			continue
		}

		// Step 7: Tentative commit
		if err := store.CommitState(updateResult.NewState); err != nil {
			log.Printf("commit error: %v", err)
			continue
		}

		// Step 8: Post-commit eval
		evalResult := evalHarness.Run(updateResult.NewState, result.Entropy)

		if !evalResult.Passed {
			// Eval failed: rollback to previous version
			log.Printf("[%s] eval failed: %s — rolling back", turnID, evalResult.Reason)
			if rbErr := store.Rollback(current.VersionID); rbErr != nil {
				log.Printf("[%s] rollback error: %v", turnID, rbErr)
			}
			signalsJSON, _ := json.Marshal(updateCtx)
			_ = logging.LogDecision(store.DB(), logging.ProvenanceEntry{
				VersionID:    updateResult.NewState.VersionID,
				TriggerType:  "user_turn",
				SignalsJSON:  string(signalsJSON),
				EvidenceRefs: strings.Join(evidenceRefs, ","),
				Decision:     "reject",
				Reason:       fmt.Sprintf("eval rollback: %s", evalResult.Reason),
				CreatedAt:    time.Now().UTC(),
			})
			fmt.Printf("[%s] decision=rollback (eval) entropy=%.4f evidence=%d\n",
				turnID, result.Entropy, len(evidenceStrings))
			continue
		}

		// Step 9: Eval passed — state stays committed. Log provenance.
		signalsJSON, _ := json.Marshal(updateCtx)
		reason := fmt.Sprintf("gate: %s | eval: %s", gateDecision.Reason, evalResult.Reason)
		err = logging.LogDecision(store.DB(), logging.ProvenanceEntry{
			VersionID:    updateResult.NewState.VersionID,
			TriggerType:  "user_turn",
			SignalsJSON:  string(signalsJSON),
			EvidenceRefs: strings.Join(evidenceRefs, ","),
			Decision:     "commit",
			Reason:       reason,
			CreatedAt:    time.Now().UTC(),
		})
		if err != nil {
			log.Printf("logging error: %v", err)
		}

		fmt.Printf("[%s] decision=commit gate_score=%.4f entropy=%.4f evidence=%d\n",
			turnID, gateDecision.SoftScore, result.Entropy, len(evidenceStrings))
	}
}

// #endregion main

// #region helpers
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// #endregion helpers
