package replay

import (
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/state"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
)

// #region types
// Interaction represents a single recorded turn for replay.
type Interaction struct {
	TurnID       string
	Prompt       string
	ResponseText string
	Entropy      float32
	Signals      update.Signals
	Evidence     []string
}

// ReplayResult captures the outcome of replaying one interaction.
type ReplayResult struct {
	TurnID   string
	Decision update.Decision
	Metrics  update.Metrics
	VersionID string
}
// #endregion types

// #region replay
// Replay iterates through a sequence of interactions, applying Update() to each
// and collecting results. This is a scaffold — Phase 1 uses no-op updates.
func Replay(store *state.Store, fromVersionID string, interactions []Interaction) ([]ReplayResult, error) {
	current, err := store.GetVersion(fromVersionID)
	if err != nil {
		return nil, err
	}

	results := make([]ReplayResult, 0, len(interactions))

	for _, inter := range interactions {
		ctx := update.UpdateContext{
			TurnID:       inter.TurnID,
			Prompt:       inter.Prompt,
			ResponseText: inter.ResponseText,
			Entropy:      inter.Entropy,
		}

		result := update.Update(current, ctx, inter.Signals, inter.Evidence)

		results = append(results, ReplayResult{
			TurnID:    inter.TurnID,
			Decision:  result.Decision,
			Metrics:   result.Metrics,
			VersionID: result.NewState.VersionID,
		})

		current = result.NewState
	}

	return results, nil
}
// #endregion replay
