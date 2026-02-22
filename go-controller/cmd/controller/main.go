package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/codec"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/eval"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/gate"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/logging"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/projection"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/retrieval"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/signals"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/websearch"
)

// #region main
func main() {
	dbPath := envOr("ADAPTIVE_DB", "adaptive_state.db")
	grpcAddr := envOr("CODEC_ADDR", "localhost:50051")

	// Configurable gRPC timeouts
	timeoutGenerate := envDuration("TIMEOUT_GENERATE", 60)
	timeoutSearch := envDuration("TIMEOUT_SEARCH", 30)
	timeoutStore := envDuration("TIMEOUT_STORE", 15)
	timeoutEmbed := envDuration("TIMEOUT_EMBED", 15)

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

	// Initialize preference store (uses same DB)
	prefStore, err := projection.NewPreferenceStore(store.DB())
	if err != nil {
		log.Fatalf("failed to init preference store: %v", err)
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

	// Phase 5: Heuristic signal producer
	signalProducer := signals.NewProducer(codecClient, signals.DefaultProducerConfig())
	var userCorrected bool
	var ollamaCtx []int64

	// Web search fallback config
	webSearchCfg := websearch.DefaultConfig()

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
		if prompt == "/correct" {
			userCorrected = true
			fmt.Println("Noted. Next update will carry UserCorrection veto.")
			continue
		}

		// Detect and store explicit preferences
		if prefText, detected := projection.DetectPreference(prompt); detected {
			if err := prefStore.Add(prefText, "explicit"); err != nil {
				log.Printf("preference store error: %v", err)
			} else {
				log.Printf("preference stored: %q", prefText)
			}
		}
		// Detect corrections — also flag for gate veto
		if projection.DetectCorrection(prompt) {
			userCorrected = true
			log.Printf("correction detected in prompt")
		}

		turnNum++
		turnID := fmt.Sprintf("turn-%d", turnNum)

		// Step 1: Get current state
		current, err := store.GetCurrent()
		if err != nil {
			log.Printf("error getting current state: %v", err)
			continue
		}

		// State norm warning (logging only)
		stateNorm := float32(0)
		for _, v := range current.StateVector {
			stateNorm += v * v
		}
		stateNorm = float32(math.Sqrt(float64(stateNorm)))
		if stateNorm > 4.0 {
			log.Printf("[%s] WARN state_norm=%.4f > 4.0 — approaching over-bias zone", turnID, stateNorm)
		}

		// Build adaptive state prompt block from stored preferences + prefs segment norm
		prefsNorm := float32(0)
		for i := current.SegmentMap.Prefs[0]; i < current.SegmentMap.Prefs[1]; i++ {
			prefsNorm += current.StateVector[i] * current.StateVector[i]
		}
		prefsNorm = float32(math.Sqrt(float64(prefsNorm)))
		storedPrefs, _ := prefStore.List()
		stateBlock := projection.ProjectToPrompt(storedPrefs, prefsNorm)
		wrappedPrompt := projection.WrapPrompt(stateBlock, prompt)
		if stateBlock != "" {
			log.Printf("[%s] state projection: %d prefs, prefs_norm=%.4f", turnID, len(storedPrefs), prefsNorm)
		}

		// Step 2: First-pass Generate to get entropy
		ctx, cancel := context.WithTimeout(context.Background(), timeoutGenerate)
		result, err := codecClient.Generate(ctx, wrappedPrompt, current.StateVector, nil, ollamaCtx)
		cancel()
		if err != nil {
			log.Printf("codec error: %v", err)
			continue
		}
		ollamaCtx = result.Context

		// Step 3: Triple-gated retrieval (if entropy warrants it)
		var evidenceStrings []string
		var evidenceRefs []string
		ctx2, cancel2 := context.WithTimeout(context.Background(), timeoutSearch)
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
			ctx3, cancel3 := context.WithTimeout(context.Background(), timeoutGenerate)
			result, err = codecClient.Generate(ctx3, wrappedPrompt, current.StateVector, evidenceStrings, ollamaCtx)
			cancel3()
			if err != nil {
				log.Printf("re-generate error: %v", err)
				continue
			}
			ollamaCtx = result.Context
		} else {
			log.Printf("[%s] retrieval: %s", turnID, gateResult.Reason)
		}

		// Web search fallback: search when no usable evidence exists
		// - coherenceFiltered: gate2 found candidates but gate3 rejected all → search regardless of entropy
		// - noUsableEvidence + highEntropy: no candidates at all and model is uncertain
		noUsableEvidence := len(evidenceStrings) == 0
		coherenceFiltered := gateResult.Gate2Count > 0 && gateResult.Gate3Count == 0
		highEntropy := float64(result.Entropy) > webSearchCfg.EntropyThreshold
		if (coherenceFiltered || (noUsableEvidence && highEntropy)) && webSearchCfg.Enabled {
			log.Printf("[%s] web search triggered: noEvidence=%v coherenceFiltered=%v entropy=%.4f",
				turnID, noUsableEvidence, coherenceFiltered, result.Entropy)
			webCtx, webCancel := context.WithTimeout(context.Background(), webSearchCfg.Timeout)
			grpcResults, webErr := codecClient.WebSearch(webCtx, prompt, webSearchCfg.MaxResults)
			webCancel()
			if webErr != nil {
				log.Printf("[%s] web search error (non-fatal): %v", turnID, webErr)
			} else if len(grpcResults) > 0 {
				// Convert codec.WebSearchResult → websearch.Result for formatting
				webResults := make([]websearch.Result, len(grpcResults))
				for i, r := range grpcResults {
					webResults[i] = websearch.Result{Title: r.Title, Snippet: r.Snippet, URL: r.URL}
				}
				webEvidence := websearch.FormatAsEvidence(webResults)
				evidenceStrings = append(evidenceStrings, webEvidence)
				log.Printf("[%s] web search: injected %d results", turnID, len(grpcResults))

				// Re-generate with web search evidence
				webGenCtx, webGenCancel := context.WithTimeout(context.Background(), timeoutGenerate)
				result, err = codecClient.Generate(webGenCtx, wrappedPrompt, current.StateVector, evidenceStrings, ollamaCtx)
				webGenCancel()
				if err != nil {
					log.Printf("web re-generate error: %v", err)
					continue
				}
				ollamaCtx = result.Context
			}
		}

		fmt.Printf("\n%s\n\n", result.Text)

		// Step 4: Store this interaction as evidence for future retrieval
		storeText := prompt + "\n" + result.Text
		metadataJSON := fmt.Sprintf(`{"turn_id":"%s","entropy":%.4f}`, turnID, result.Entropy)
		ctx4, cancel4 := context.WithTimeout(context.Background(), timeoutStore)
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
		// Phase 5: Compute heuristic signals from loop data
		signalInput := signals.ProduceInput{
			Prompt:       prompt,
			ResponseText: result.Text,
			Entropy:      result.Entropy,
			Logits:       result.Logits,
			Retrieved:    gateResult.Retrieved,
			Gate2Count:   gateResult.Gate2Count,
			UserCorrect:  userCorrected,
		}
		ctx5, cancel5 := context.WithTimeout(context.Background(), timeoutEmbed)
		sigs := signalProducer.Produce(ctx5, signalInput)
		cancel5()
		userCorrected = false
		updateResult := update.Update(current, updateCtx, sigs, evidenceStrings, updateConfig)

		// Step 6: Gate evaluation — hard vetoes + soft scoring
		gateDecision := stateGate.Evaluate(
			current, updateResult.NewState, sigs, updateResult.Metrics, result.Entropy,
		)

		// Build gate record for provenance logging (used by all 3 decision paths)
		gateRecord := logging.GateRecord{
			TurnID:   turnID,
			Prompt:   prompt,
			Response: result.Text,
			Entropy:  result.Entropy,
			Signals: logging.GateRecordSignals{
				SentimentScore:      sigs.SentimentScore,
				CoherenceScore:      sigs.CoherenceScore,
				NoveltyScore:        sigs.NoveltyScore,
				RiskFlag:            sigs.RiskFlag,
				UserCorrection:      sigs.UserCorrection,
				ToolFailure:         sigs.ToolFailure,
				ConstraintViolation: sigs.ConstraintViolation,
			},
			DeltaNorm:     updateResult.Metrics.DeltaNorm,
			SegmentsHit:   updateResult.Metrics.SegmentsHit,
			Thresholds: logging.GateRecordThresholds{
				MaxDeltaNorm:   gate.DefaultGateConfig().MaxDeltaNorm,
				MaxStateNorm:   gate.DefaultGateConfig().MaxStateNorm,
				RiskSegmentCap: gate.DefaultGateConfig().RiskSegmentCap,
				MaxSegmentNorm: eval.DefaultEvalConfig().MaxSegmentNorm,
			},
			GateAction:    gateDecision.Action,
			GateSoftScore: gateDecision.SoftScore,
			GateVetoed:    gateDecision.Vetoed,
			GateReason:    gateDecision.Reason,
		}
		signalsJSON, _ := json.Marshal(gateRecord)

		if gateDecision.Action == "reject" {
			// Gate rejected: log rejection, keep old state, continue
			log.Printf("[%s] gate rejected: %s", turnID, gateDecision.Reason)
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

func envDuration(key string, defaultSec int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return time.Duration(defaultSec) * time.Second
}

// #endregion helpers
