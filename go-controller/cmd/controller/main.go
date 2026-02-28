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
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/graph"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/interior"
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

	// Initialize interior store — persists Orac's self-reflections (uses same DB)
	interiorStore, err := interior.NewInteriorStore(store.DB())
	if err != nil {
		log.Fatalf("failed to init interior store: %v", err)
	}

	// Initialize graph store — associative evidence edges (uses same DB)
	graphStore, err := graph.NewGraphStore(store.DB())
	if err != nil {
		log.Fatalf("failed to init graph store: %v", err)
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
	var lastGateSummary string
	var lastPrompt string
	var lastResponse string
	var recentEvidenceIDs []string // last 3 stored evidence IDs for temporal edges
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
		// Detect and store AI designation (e.g. "your name is Architect")
		if designation, detected := projection.DetectAIDesignation(prompt); detected {
			designPref := fmt.Sprintf("The AI's designation is %s", designation)
			prefStore.DeleteByPrefix("The AI's designation is")
			if err := prefStore.Add(designPref, "explicit"); err != nil {
				log.Printf("AI designation store error: %v", err)
			} else {
				log.Printf("AI designation stored: %q", designation)
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

		// Memory correction: Commander wants to review and delete bad evidence
		if projection.DetectMemoryCorrection(prompt) && lastPrompt != "" {
			log.Printf("memory correction triggered — reviewing evidence")
			// Search for evidence similar to the previous exchange
			searchQuery := lastPrompt + "\n" + lastResponse
			searchCtx, searchCancel := context.WithTimeout(context.Background(), timeoutSearch)
			searchResults, searchErr := codecClient.Search(searchCtx, searchQuery, 10, 0.1)
			searchCancel()
			if searchErr != nil {
				log.Printf("memory review search error: %v", searchErr)
				fmt.Println("Could not search evidence for review.")
				continue
			}
			if len(searchResults) == 0 {
				fmt.Println("No related evidence found to review.")
				continue
			}

			// Build review prompt showing evidence items + gate feedback
			var reviewLines []string
			reviewLines = append(reviewLines, "Commander flagged your last response as junk.")
			if lastGateSummary != "" {
				reviewLines = append(reviewLines, fmt.Sprintf("Gate feedback from that turn: %s", lastGateSummary))
			}
			reviewLines = append(reviewLines, fmt.Sprintf("Your last exchange was:\n  Commander: %s\n  You: %s", lastPrompt, lastResponse))
			reviewLines = append(reviewLines, "\nRelated evidence items in your memory:")
			var validIDs []string
			for _, sr := range searchResults {
				text := sr.Text
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				reviewLines = append(reviewLines, fmt.Sprintf("  ID: %s\n  Text: %s\n  Score: %.4f\n", sr.ID, text, sr.Score))
				validIDs = append(validIDs, sr.ID)
			}
			reviewLines = append(reviewLines, "Which IDs should be deleted? List one per line, or NONE.")
			reviewPrompt := strings.Join(reviewLines, "\n")

			// Send to Orac in review mode (no tools, no state wrapping)
			reviewState, _ := store.GetCurrent()
			reviewCtx, reviewCancel := context.WithTimeout(context.Background(), timeoutGenerate)
			reviewResult, reviewErr := codecClient.Generate(reviewCtx, reviewPrompt, reviewState.StateVector, []string{"[REVIEW MODE]"}, nil)
			reviewCancel()
			if reviewErr != nil {
				log.Printf("memory review generate error: %v", reviewErr)
				fmt.Println("Could not complete evidence review.")
				continue
			}

			// Parse and validate IDs from Orac's response
			deleteIDs := parseDeleteIDs(reviewResult.Text, validIDs)
			if len(deleteIDs) == 0 {
				fmt.Println("Reviewed memory: nothing to delete.")
				continue
			}

			// Execute deletions
			delCtx, delCancel := context.WithTimeout(context.Background(), timeoutStore)
			deleted, delErr := codecClient.DeleteEvidence(delCtx, deleteIDs)
			delCancel()
			if delErr != nil {
				log.Printf("delete evidence error: %v", delErr)
				fmt.Println("Error deleting evidence.")
			} else {
				// Sever graph edges for deleted evidence nodes
				for _, id := range deleteIDs {
					if severErr := graphStore.SeverNode(id); severErr != nil {
						log.Printf("graph sever error for %s: %v", id, severErr)
					}
				}
				fmt.Printf("Reviewed memory: deleted %d junk items.\n", deleted)
				log.Printf("memory review: deleted %d/%d items (edges severed)", deleted, len(deleteIDs))
			}
			continue
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

		// Load Orac's last reflection for interior state injection (non-rule turns only)
		lastReflection, _ := interiorStore.Latest()
		var interiorEvidence []string
		if lastReflection != nil && len(matchedRules) == 0 {
			interiorEvidence = []string{"[ORAC INTERIOR STATE]\n" + lastReflection.ReflectionText}
			log.Printf("[%s] interior state: reflection from %s injected", turnID, lastReflection.TurnID)
		}

		// Variables that may be populated by generation or skipped for instruction-only prompts
		var result codec.GenerateResult
		var evidenceStrings []string
		var evidenceRefs []string
		var gateResult retrieval.GateResult
		var curiosity []string

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
			result, err = codecClient.Generate(ctx, generatePrompt, current.StateVector, append(ruleEvidence, interiorEvidence...), nil)
			cancel()
			if err != nil {
				log.Printf("codec error: %v", err)
				continue
			}

			// Step 3: Triple-gated retrieval with goals-adjusted threshold
			// Command gate: skip retrieval for direct tool commands
			if retrieval.IsDirectCommand(prompt) {
				log.Printf("[%s] command gate: skipping retrieval for direct command", turnID)
			} else {
			retCfg := retrieval.DefaultConfig()
			retCfg.SimilarityThreshold = retrieval.AdjustedThreshold(retCfg.SimilarityThreshold, goalsNorm)
			adjustedRetriever := retrieval.NewRetriever(codecClient, retCfg)
			graphRetriever := retrieval.NewGraphRetriever(adjustedRetriever, graphStore, codecClient)

			ctx2, cancel2 := context.WithTimeout(context.Background(), timeoutSearch)
			gateResult, err = graphRetriever.Retrieve(ctx2, prompt, result.Entropy)
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

				// Filter out evidence containing rule response patterns
				// Matches both exact responses ("Daniel who?") and continuations ("Daniel who built...")
				allRules, _ := ruleStore.List()
				if len(allRules) > 0 {
					var rulePatterns []string
					for _, r := range allRules {
						// Strip trailing punctuation to get the stem ("Daniel who?" → "daniel who")
						stem := strings.ToLower(strings.TrimRight(r.Response, "?.!"))
						if stem != "" {
							rulePatterns = append(rulePatterns, stem)
						}
					}
					var filtered []string
					for _, ev := range evidenceStrings {
						evLower := strings.ToLower(ev)
						contaminated := false
						for _, pat := range rulePatterns {
							if strings.Contains(evLower, pat) {
								contaminated = true
								break
							}
						}
						if !contaminated {
							filtered = append(filtered, ev)
						}
					}
					if removed := len(evidenceStrings) - len(filtered); removed > 0 {
						log.Printf("[%s] evidence filter: removed %d rule-contaminated items", turnID, removed)
					}
					evidenceStrings = filtered
				}

				// Re-generate with evidence injected (rules prepended, interior state included)
				allEvidence := append(append(ruleEvidence, interiorEvidence...), evidenceStrings...)
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

			// Co-retrieval edge formation: link evidence items retrieved together
			if len(evidenceRefs) >= 2 {
				for i := 0; i < len(evidenceRefs); i++ {
					for j := i + 1; j < len(evidenceRefs); j++ {
						graphStore.IncrementEdge(evidenceRefs[i], evidenceRefs[j], "co_retrieval", 0.1)
						graphStore.IncrementEdge(evidenceRefs[j], evidenceRefs[i], "co_retrieval", 0.1)
					}
				}
				log.Printf("[%s] graph: %d co-retrieval edges formed", turnID, len(evidenceRefs)*(len(evidenceRefs)-1))
			}
			} // end command gate else

			fmt.Printf("\n%s\n\n", result.Text)

			// Reflection: Orac speaks from inside himself about this exchange
			gateFeedback := ""
			if lastGateSummary != "" {
				gateFeedback = fmt.Sprintf("\n[GATE FEEDBACK from your previous turn: %s]", lastGateSummary)
			}
			reflectionPrompt := fmt.Sprintf(
				"Commander said: %s\nYou responded: %s%s\n\nNow speak from inside yourself. What did you notice in this exchange? What don't you know that this opened? What do you want to understand?",
				prompt, result.Text, gateFeedback,
			)
			reflectCtx, reflectCancel := context.WithTimeout(context.Background(), timeoutGenerate)
			reflectResult, reflectErr := codecClient.Generate(reflectCtx, reflectionPrompt, current.StateVector, []string{"[REFLECTION MODE]"}, nil)
			reflectCancel()
			if reflectErr != nil {
				log.Printf("[%s] reflection error (non-fatal): %v", turnID, reflectErr)
			} else if reflectResult.Text != "" {
				if saveErr := interiorStore.Save(turnID, reflectResult.Text); saveErr != nil {
					log.Printf("[%s] interior store error: %v", turnID, saveErr)
				}
				curiosity = interior.ExtractCuriosity(reflectResult.Text)
				if len(curiosity) > 0 {
					log.Printf("[%s] curiosity signals: %v", turnID, curiosity)
				}
				log.Printf("[%s] reflection stored (%d words)", turnID, len(strings.Fields(reflectResult.Text)))
			}
		}

		// Step 4: Evidence storage — deferred until after gate decision (see Step 6b)

		// Periodic graph decay (every 50 turns)
		if turnNum%50 == 0 {
			deleted, decayErr := graphStore.DecayAll(48.0)
			if decayErr != nil {
				log.Printf("[%s] graph decay error: %v", turnID, decayErr)
			} else if deleted > 0 {
				log.Printf("[%s] graph decay: removed %d weak edges", turnID, deleted)
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

		// Store gate summary for next turn's reflection + memory review
		lastGateSummary = fmt.Sprintf("soft_score=%.4f entropy=%.4f delta_norm=%.4f segments=%v vetoed=%v",
			gateDecision.SoftScore, result.Entropy, updateResult.Metrics.DeltaNorm,
			updateResult.Metrics.SegmentsHit, gateDecision.Vetoed)

		if gateDecision.Action == "reject" {
			// Gate rejected: log rejection, keep old state, skip evidence storage, continue
			log.Printf("[%s] gate rejected: %s", turnID, gateDecision.Reason)
			log.Printf("[%s] evidence skipped: gate rejected", turnID)
			_ = logging.LogDecision(store.DB(), logging.ProvenanceEntry{
				VersionID:    current.VersionID,
				TriggerType:  "user_turn",
				SignalsJSON:  string(signalsJSON),
				EvidenceRefs: strings.Join(evidenceRefs, ","),
				Decision:     "reject",
				Reason:       fmt.Sprintf("gate: %s", gateDecision.Reason),
				CreatedAt:    time.Now().UTC(),
			})
			// Track previous turn even on rejection
			lastPrompt = prompt
			lastResponse = result.Text

			fmt.Printf("[%s] decision=reject (gate) entropy=%.4f evidence=%d\n",
				turnID, result.Entropy, len(evidenceStrings))
			continue
		}

		// Step 6b: Reflection-gated evidence storage — Orac's reflection decides what's worth keeping.
		// No curiosity signals = the exchange didn't open anything new = don't store it.
		// Gate rejection = don't store. Low entropy = stalling pattern = don't store.
		if !isPreferenceOnly && len(matchedRules) == 0 && !session.RuleActive {
			if len(curiosity) == 0 {
				log.Printf("[%s] evidence skipped: reflection found nothing worth keeping", turnID)
			} else if result.Entropy < 0.03 {
				log.Printf("[%s] evidence skipped: entropy %.4f (stalling pattern)", turnID, result.Entropy)
			} else {
				storeText := prompt + "\n" + result.Text
				now := time.Now().UTC()
				metadataJSON := fmt.Sprintf(`{"turn_id":"%s","entropy":%.4f,"stored_at":"%s"}`,
					turnID, result.Entropy, now.Format(time.RFC3339))
				ctx4, cancel4 := context.WithTimeout(context.Background(), timeoutStore)
				storedID, storeErr := codecClient.StoreEvidence(ctx4, storeText, metadataJSON)
				cancel4()
				if storeErr != nil {
					log.Printf("store evidence error (non-fatal): %v", storeErr)
				} else if storedID != "" {
					// Temporal edge formation: link to recent evidence IDs
					for _, prevID := range recentEvidenceIDs {
						graphStore.AddEdge(prevID, storedID, "temporal", 0.05)
					}
					if len(recentEvidenceIDs) > 0 {
						log.Printf("[%s] graph: %d temporal edges formed", turnID, len(recentEvidenceIDs))
					}

					// Reflection edge formation: link retrieved evidence to new stored evidence
					if len(evidenceRefs) > 0 {
						for _, refID := range evidenceRefs {
							graphStore.AddEdge(refID, storedID, "reflection", 0.3)
						}
						log.Printf("[%s] graph: %d reflection edges formed", turnID, len(evidenceRefs))
					}

					// Track recent evidence IDs (last 3)
					recentEvidenceIDs = append(recentEvidenceIDs, storedID)
					if len(recentEvidenceIDs) > 3 {
						recentEvidenceIDs = recentEvidenceIDs[len(recentEvidenceIDs)-3:]
					}
				}
			}
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
			// Track previous turn even on rollback
			lastPrompt = prompt
			lastResponse = result.Text

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

		// Track previous turn for memory review context
		lastPrompt = prompt
		lastResponse = result.Text

		fmt.Printf("[%s] decision=commit gate_score=%.4f entropy=%.4f evidence=%d\n",
			turnID, gateDecision.SoftScore, result.Entropy, len(evidenceStrings))
	}
}

// #endregion main

// #region parse-delete-ids

// parseDeleteIDs extracts evidence IDs from Orac's review response.
// Only accepts IDs that exist in the validIDs whitelist (prevents hallucinated deletions).
func parseDeleteIDs(response string, validIDs []string) []string {
	if strings.TrimSpace(strings.ToUpper(response)) == "NONE" {
		return nil
	}

	validSet := make(map[string]bool, len(validIDs))
	for _, id := range validIDs {
		validSet[id] = true
	}

	var result []string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip common prefixes like "ID: " or "- "
		line = strings.TrimPrefix(line, "ID: ")
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimSpace(line)
		if validSet[line] {
			result = append(result, line)
		}
	}
	return result
}

// #endregion parse-delete-ids

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
