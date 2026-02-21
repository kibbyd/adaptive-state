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
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/logging"
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

		// Get current state
		current, err := store.GetCurrent()
		if err != nil {
			log.Printf("error getting current state: %v", err)
			continue
		}

		// Call inference service
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result, err := codecClient.Generate(ctx, prompt, current.StateVector, nil)
		cancel()
		if err != nil {
			log.Printf("codec error: %v", err)
			continue
		}

		fmt.Printf("\n%s\n\n", result.Text)

		// Run update function (no-op in Phase 1)
		updateCtx := update.UpdateContext{
			TurnID:       turnID,
			Prompt:       prompt,
			ResponseText: result.Text,
			Entropy:      result.Entropy,
		}
		updateResult := update.Update(current, updateCtx, update.Signals{}, nil)

		// Commit new state version
		if err := store.CommitState(updateResult.NewState); err != nil {
			log.Printf("commit error: %v", err)
		}

		// Log provenance
		signalsJSON, _ := json.Marshal(updateCtx)
		err = logging.LogDecision(store.DB(), logging.ProvenanceEntry{
			VersionID:   updateResult.NewState.VersionID,
			TriggerType: "user_turn",
			SignalsJSON: string(signalsJSON),
			Decision:    updateResult.Decision.Action,
			Reason:      updateResult.Decision.Reason,
			CreatedAt:   time.Now().UTC(),
		})
		if err != nil {
			log.Printf("logging error: %v", err)
		}

		fmt.Printf("[%s] decision=%s entropy=%.4f\n", turnID, updateResult.Decision.Action, result.Entropy)
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
