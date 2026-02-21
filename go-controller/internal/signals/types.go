package signals

import (
	"context"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/retrieval"
)

// #region embedder-interface

// Embedder abstracts the embedding RPC so Producer can be tested without gRPC.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// #endregion embedder-interface

// #region config

// ProducerConfig holds tuning knobs for signal computation.
type ProducerConfig struct {
	RiskEntropyMultiplier float32 // entropy >= EntropyThreshold * this → RiskFlag
	EntropyThreshold      float32 // baseline entropy threshold (matches retrieval default)
}

// DefaultProducerConfig returns sensible defaults.
func DefaultProducerConfig() ProducerConfig {
	return ProducerConfig{
		RiskEntropyMultiplier: 1.5,
		EntropyThreshold:      0.5,
	}
}

// #endregion config

// #region input

// ProduceInput bundles all data available in the REPL loop for signal computation.
type ProduceInput struct {
	Prompt       string
	ResponseText string
	Entropy      float32
	Logits       []float32
	Retrieved    []retrieval.EvidenceRecord
	Gate2Count   int
	UserCorrect  bool
}

// #endregion input
