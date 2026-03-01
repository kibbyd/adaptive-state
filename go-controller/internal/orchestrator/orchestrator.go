package orchestrator

// #region imports
import (
	"database/sql"
	"log"
	"os"
	"time"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/interior"
)

// #endregion

// #region orchestrator-struct

// Orchestrator is the top-level coordinator for turn classification,
// strategy selection, response evaluation, and retry decisions.
type Orchestrator struct {
	selector       *StrategySelector
	retry          *RetryEngine
	memory         *StrategyMemory
	enabled        bool
	lastClass      *TurnClassification // previous turn's classification for context inheritance
}

// #endregion

// #region constructor

// NewOrchestrator creates a fully wired orchestrator.
// Pass db for strategy memory persistence.
// Kill switch: set ORCHESTRATOR_ENABLED=false to disable.
func NewOrchestrator(db *sql.DB) (*Orchestrator, error) {
	enabled := true
	if v := os.Getenv("ORCHESTRATOR_ENABLED"); v == "false" {
		enabled = false
	}

	mem, err := NewStrategyMemory(db)
	if err != nil {
		return nil, err
	}

	selector := NewStrategySelector(mem)
	retry := NewRetryEngine(selector)

	return &Orchestrator{
		selector: selector,
		retry:    retry,
		memory:   mem,
		enabled:  enabled,
	}, nil
}

// #endregion

// #region enabled

// Enabled returns whether the orchestrator is active.
func (o *Orchestrator) Enabled() bool {
	return o.enabled
}

// #endregion

// #region pre-generate

// PreGenerate classifies the turn and selects the initial strategy.
// Always runs (even when disabled) for logging; caller checks Enabled() for behavior changes.
func (o *Orchestrator) PreGenerate(prompt string, lastReflection *interior.Reflection) PreGenerateResult {
	var class TurnClassification
	if o.lastClass != nil {
		class = ClassifyTurn(prompt, lastReflection, *o.lastClass)
	} else {
		class = ClassifyTurn(prompt, lastReflection)
	}

	var strategy StrategyConfig
	if o.enabled {
		strategy = o.selector.SelectInitial(class)
	} else {
		strategy = Strategies[StrategyDefault]
	}

	// Store classification for next turn's context inheritance
	o.lastClass = &class

	log.Printf("[ORCH] classify: type=%s complexity=%s risk=%s → strategy=%s",
		class.Type, class.Complexity, class.Risk, strategy.ID)

	return PreGenerateResult{
		Classification: class,
		Strategy:       strategy,
	}
}

// #endregion

// #region post-generate

// PostGenerate evaluates a response and decides whether to retry.
// wasTruncated indicates the degeneration guard fired before evaluation.
func (o *Orchestrator) PostGenerate(
	prompt, response string,
	entropy float32,
	class TurnClassification,
	attempts []Attempt,
	wasTruncated bool,
) PostGenerateResult {
	eval := EvaluateResponse(prompt, response, entropy, class, wasTruncated)

	log.Printf("[ORCH] evaluate: quality=%.2f failure=%s shouldRetry=%v",
		eval.Quality, eval.FailureType, eval.ShouldRetry)

	if !o.enabled || !eval.ShouldRetry {
		return PostGenerateResult{
			Evaluation: eval,
			Accept:     true,
		}
	}

	// Add current attempt to list for retry decision
	currentAttempt := Attempt{
		Strategy:   attempts[len(attempts)-1].Strategy,
		Response:   response,
		Entropy:    entropy,
		Evaluation: eval,
	}
	allAttempts := append(attempts[:len(attempts)-1], currentAttempt)

	shouldRetry, nextCfg := o.retry.ShouldRetry(class, allAttempts)
	if !shouldRetry || nextCfg == nil {
		log.Printf("[ORCH] no retry available, accepting current response")
		return PostGenerateResult{
			Evaluation: eval,
			Accept:     true,
		}
	}

	log.Printf("[ORCH] retry → strategy=%s", nextCfg.ID)
	return PostGenerateResult{
		Evaluation:   eval,
		Accept:       false,
		NextStrategy: nextCfg,
	}
}

// #endregion

// #region record-final-outcome

// RecordFinalOutcome persists all attempts for a completed turn.
func (o *Orchestrator) RecordFinalOutcome(
	turnID string,
	class TurnClassification,
	attempts []Attempt,
	acceptedIdx int,
	gateScore float32,
) {
	for i, a := range attempts {
		accepted := i == acceptedIdx
		rec := OutcomeRecord{
			TurnID:      turnID,
			TurnType:    class.Type,
			Complexity:  class.Complexity,
			Risk:        class.Risk,
			StrategyID:  a.Strategy,
			AttemptNum:  i,
			Quality:     a.Evaluation.Quality,
			FailureType: a.Evaluation.FailureType,
			Entropy:     a.Entropy,
			GateScore:   gateScore,
			Accepted:    accepted,
			CreatedAt:   time.Now(),
		}
		if err := o.memory.RecordOutcome(rec); err != nil {
			log.Printf("[ORCH] failed to record outcome: %v", err)
		}
	}

	log.Printf("[ORCH] recorded %d attempts for turn %s (accepted_idx=%d)",
		len(attempts), turnID, acceptedIdx)
}

// #endregion
