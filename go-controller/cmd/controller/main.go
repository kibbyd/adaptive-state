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
)

// #region session-state
type SessionState struct {
	RuleActive   bool
	LastRuleTurn int
}

func isRuleContinuation(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	// Direct knock-knock continuation
	if strings.Contains(lower, "knock") {
		return true
	}
	// Punchline pattern: "<name> who <punchline>" (e.g. "Daniel who codes all night")
	// Must start with a word followed by "who" — not question-word "who is..."
	if !strings.HasPrefix(lower, "who") && strings.Contains(lower, " who ") && len(lower) < 60 {
		return true
	}
	// Very short reactions only (e.g. "haha", "good one", "lol", "nice one")
	// Exclude question-word starts ("who is...", "what is...")
	words := strings.Fields(lower)
	if len(words) <= 3 && !strings.HasPrefix(lower, "who") && !strings.HasPrefix(lower, "what") && !strings.HasPrefix(lower, "how") && !strings.HasPrefix(lower, "why") {
		return true
	}
	return false
}

// #endregion session-state

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

	// Initialize rule store (uses same DB)
	ruleStore, err := projection.NewRuleStore(store.DB())
	if err != nil {
		log.Fatalf("failed to init rule store: %v", err)
	}

	// Connect to Python inference service
	codecClient, err := codec.NewCodecClient(grpcAddr)
	if err != nil {
		log.Fatalf("failed to connect to codec service at %s: %v", grpcAddr, err)
	}
	defer codecClient.Close()

	// Phase 3: Initialize gate and eval harness
	stateGate := gate.NewGate(gate.DefaultGateConfig())
	evalHarness := eval.NewEvalHarness(eval.DefaultEvalConfig())

	// Phase 4: Update config for learning + decay
	updateConfig := update.DefaultUpdateConfig()

	// Phase 5: Heuristic signal producer
	signalProducer := signals.NewProducer(codecClient, signals.DefaultProducerConfig())
	var userCorrected bool
	session := SessionState{}

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
		isPreferenceOnly := false
		if prefText, detected := projection.DetectPreference(prompt); detected {
			if err := prefStore.Add(prefText, "explicit"); err != nil {
				log.Printf("preference store error: %v", err)
			} else {
				log.Printf("preference stored: %q", prefText)
			}
			isPreferenceOnly = true
		}
		// Detect and store identity statements as preferences (replaces previous identity)
		if name, detected := projection.DetectIdentity(prompt); detected {
			identityPref := fmt.Sprintf("The user's name is %s", name)
			prefStore.DeleteByPrefix("The user's name is")
			if err := prefStore.Add(identityPref, "general"); err != nil {
				log.Printf("identity store error: %v", err)
			} else {
				log.Printf("identity stored: %q (replaced previous)", name)
			}
		}
		// Detect and extract behavioral rules
		if projection.DetectRule(prompt) {
			if trigger, response, ok := projection.ExtractRule(prompt); ok {
				if err := ruleStore.Add(trigger, response, 5, 1.0); err != nil {
					log.Printf("rule store error: %v", err)
				} else {
					log.Printf("rule stored: %q → %q", trigger, response)
				}
				isPreferenceOnly = true // rule-teaching doesn't need generation
			}
		}
		// Detect corrections — also flag for gate veto
		if projection.DetectCorrection(prompt) {
			userCorrected = true
			log.Printf("correction detected in prompt")
			isPreferenceOnly = false // corrections need generation
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

		// Compute goals segment norm for retrieval threshold adjustment
		goalsNorm := float32(0)
		for i := current.SegmentMap.Goals[0]; i < current.SegmentMap.Goals[1]; i++ {
			goalsNorm += current.StateVector[i] * current.StateVector[i]
		}
		goalsNorm = float32(math.Sqrt(float64(goalsNorm)))

		// Load behavioral rules matching current input (contextual injection, bypasses retrieval)
		matchedRules, _ := ruleStore.Match(prompt)
		var ruleEvidence []string
		if len(matchedRules) > 0 {
			rulesBlock := projection.FormatRulesBlock(matchedRules)
			ruleEvidence = append(ruleEvidence, rulesBlock)
			session.RuleActive = true
			session.LastRuleTurn = turnNum
			log.Printf("[%s] rules matched: %d for input %q (rule context locked)", turnID, len(matchedRules), prompt)
		} else if session.RuleActive {
			// Release lock when input no longer matches rule continuation pattern
			if !isRuleContinuation(prompt) {
				session.RuleActive = false
				log.Printf("[%s] rule context released (non-continuation input)", turnID)
			} else {
				log.Printf("[%s] rule context active (continuation detected)", turnID)
			}
		}

		// Variables that may be populated by generation or skipped for instruction-only prompts
		var result codec.GenerateResult
		var evidenceStrings []string
		var evidenceRefs []string
		var gateResult retrieval.GateResult

		if isPreferenceOnly {
			// Instruction-only prompt: skip generation, provide canned acknowledgment
			fmt.Print("\nGot it. I'll keep that in mind.\n\n")
			log.Printf("[%s] preference-only prompt — skipped generation", turnID)
			// Set minimal result for learning loop
			result = codec.GenerateResult{
				Text:    "Got it. I'll keep that in mind.",
				Entropy: 0.0,
			}
		} else {
			// Step 2: First-pass Generate to get entropy (rules always injected)
			// On rule turns, use bare prompt — no state block to avoid identity/preference bleed
			generatePrompt := wrappedPrompt
			if len(matchedRules) > 0 {
				generatePrompt = prompt
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeoutGenerate)
			result, err = codecClient.Generate(ctx, generatePrompt, current.StateVector, ruleEvidence, nil)
			cancel()
			if err != nil {
				log.Printf("codec error: %v", err)
				continue
			}

			// Step 3: Triple-gated retrieval with goals-adjusted threshold
			retCfg := retrieval.DefaultConfig()
			retCfg.SimilarityThreshold = retrieval.AdjustedThreshold(retCfg.SimilarityThreshold, goalsNorm)
			adjustedRetriever := retrieval.NewRetriever(codecClient, retCfg)

			ctx2, cancel2 := context.WithTimeout(context.Background(), timeoutSearch)
			gateResult, err = adjustedRetriever.Retrieve(ctx2, prompt, result.Entropy)
			cancel2()
			if err != nil {
				log.Printf("retrieval error (non-fatal): %v", err)
			} else if len(gateResult.Retrieved) > 0 {
				for _, ev := range gateResult.Retrieved {
					evidenceStrings = append(evidenceStrings, ev.Text)
					evidenceRefs = append(evidenceRefs, ev.ID)
				}
				log.Printf("[%s] retrieval: %s (adjusted_threshold=%.4f, goals_norm=%.4f)",
					turnID, gateResult.Reason, retCfg.SimilarityThreshold, goalsNorm)

				// Re-generate with evidence injected (rules prepended)
				allEvidence := append(ruleEvidence, evidenceStrings...)
				ctx3, cancel3 := context.WithTimeout(context.Background(), timeoutGenerate)
				result, err = codecClient.Generate(ctx3, generatePrompt, current.StateVector, allEvidence, nil)
				cancel3()
				if err != nil {
					log.Printf("re-generate error: %v", err)
					continue
				}
			} else {
				log.Printf("[%s] retrieval: %s", turnID, gateResult.Reason)
			}

			fmt.Printf("\n%s\n\n", result.Text)
		}

		// Step 4: Store this interaction as evidence for future retrieval
		// Skip storage for rule-triggered, preference-only, and rule-context turns
		if !isPreferenceOnly && len(matchedRules) == 0 && !session.RuleActive {
			storeText := prompt + "\n" + result.Text
			now := time.Now().UTC()
			metadataJSON := fmt.Sprintf(`{"turn_id":"%s","entropy":%.4f,"stored_at":"%s"}`,
				turnID, result.Entropy, now.Format(time.RFC3339))
			ctx4, cancel4 := context.WithTimeout(context.Background(), timeoutStore)
			_, storeErr := codecClient.StoreEvidence(ctx4, storeText, metadataJSON)
			cancel4()
			if storeErr != nil {
				log.Printf("store evidence error (non-fatal): %v", storeErr)
			}
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

		// Priority 1: Override SentimentScore with preference compliance
		complianceScore := projection.PreferenceComplianceScore(storedPrefs, result.Text)
		sigs.SentimentScore = complianceScore
		log.Printf("[%s] compliance_score=%.4f (overrides sentiment)", turnID, complianceScore)

		// Priority 2: Compute direction vectors from preference embeddings
		directionSource := ""
		var directionSegments []string
		if len(storedPrefs) > 0 {
			// Concatenate preference texts for embedding
			var prefTexts []string
			for _, p := range storedPrefs {
				prefTexts = append(prefTexts, p.Text)
			}
			prefConcat := strings.Join(prefTexts, "; ")
			embedCtx, embedCancel := context.WithTimeout(context.Background(), timeoutEmbed)
			embedding, embedErr := codecClient.Embed(embedCtx, prefConcat)
			embedCancel()
			if embedErr != nil {
				log.Printf("[%s] direction embed error (non-fatal, using sign fallback): %v", turnID, embedErr)
			} else if len(embedding) >= 32 {
				// Truncate to 32 dims (prefs segment size)
				prefsDir := embedding[:32]
				if sigs.DirectionVectors == nil {
					sigs.DirectionVectors = make(map[string][]float32)
				}
				sigs.DirectionVectors["prefs"] = prefsDir
				directionSource = "embedding"
				directionSegments = append(directionSegments, "prefs")
				log.Printf("[%s] direction vector: prefs from embedding (%d dims → 32)", turnID, len(embedding))
			}
		}

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
			DirectionSource:   directionSource,
			DirectionSegments: directionSegments,
			GateAction:        gateDecision.Action,
			GateSoftScore:     gateDecision.SoftScore,
			GateVetoed:        gateDecision.Vetoed,
			GateReason:        gateDecision.Reason,
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
