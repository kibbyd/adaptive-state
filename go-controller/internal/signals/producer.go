package signals

import (
	"context"
	"math"
	"strings"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/update"
)

// #region producer

// Producer computes heuristic signals from REPL loop data.
type Producer struct {
	embedder Embedder
	config   ProducerConfig
}

// NewProducer creates a Producer. embedder may be nil (coherence degrades to 0).
func NewProducer(embedder Embedder, config ProducerConfig) *Producer {
	return &Producer{embedder: embedder, config: config}
}

// #endregion producer

// #region produce

// Produce computes all signals from the given input.
func (p *Producer) Produce(ctx context.Context, input ProduceInput) update.Signals {
	return update.Signals{
		SentimentScore:      p.sentimentScore(input),
		CoherenceScore:      p.coherenceScore(ctx, input),
		NoveltyScore:        p.noveltyScore(input),
		RiskFlag:            p.riskFlag(input),
		UserCorrection:      input.UserCorrect,
		ToolFailure:         false,
		ConstraintViolation: false,
	}
}

// #endregion produce

// #region sentiment

// sentimentScore approximates sentiment as lexical diversity * confidence.
// Confidence proxy: 1 - clamp(entropy).
func (p *Producer) sentimentScore(input ProduceInput) float32 {
	tokens := tokenize(input.ResponseText)
	if len(tokens) == 0 {
		return 0
	}
	unique := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		unique[t] = struct{}{}
	}
	diversity := float32(len(unique)) / float32(len(tokens))
	confidence := 1.0 - clamp(input.Entropy)
	return clamp(diversity * confidence)
}

// #endregion sentiment

// #region coherence

// coherenceScore computes cosine similarity between prompt and response embeddings.
// Degrades to 0 on error or nil embedder.
func (p *Producer) coherenceScore(ctx context.Context, input ProduceInput) float32 {
	if p.embedder == nil {
		return 0
	}
	promptEmb, err := p.embedder.Embed(ctx, input.Prompt)
	if err != nil {
		return 0
	}
	responseEmb, err := p.embedder.Embed(ctx, input.ResponseText)
	if err != nil {
		return 0
	}
	return clamp(cosineSimilarity(promptEmb, responseEmb))
}

// #endregion coherence

// #region novelty

// noveltyScore uses a 3-tier fallback: retrieval-inverse → logit variance → entropy.
func (p *Producer) noveltyScore(input ProduceInput) float32 {
	// Tier 1: retrieval-inverse
	if len(input.Retrieved) > 0 {
		var maxScore float32
		for _, ev := range input.Retrieved {
			if ev.Score > maxScore {
				maxScore = ev.Score
			}
		}
		return clamp(1 - maxScore)
	}
	// Tier 2: logit variance
	if len(input.Logits) > 0 {
		v := logitVariance(input.Logits)
		return clamp(float32(math.Tanh(float64(v))))
	}
	// Tier 3: entropy fallback
	return clamp(input.Entropy)
}

// #endregion novelty

// #region risk

// riskFlag returns true when entropy exceeds the configured threshold.
func (p *Producer) riskFlag(input ProduceInput) bool {
	return input.Entropy >= p.config.EntropyThreshold*p.config.RiskEntropyMultiplier
}

// #endregion risk

// #region helpers

// tokenize splits text into lowercase whitespace-delimited tokens.
func tokenize(text string) []string {
	fields := strings.Fields(strings.ToLower(text))
	return fields
}

// cosineSimilarity computes cosine similarity between two vectors.
// Returns 0 for zero-length or mismatched vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

// logitVariance computes the variance of a logit slice.
func logitVariance(logits []float32) float32 {
	if len(logits) == 0 {
		return 0
	}
	var sum float64
	for _, v := range logits {
		sum += float64(v)
	}
	mean := sum / float64(len(logits))
	var variance float64
	for _, v := range logits {
		d := float64(v) - mean
		variance += d * d
	}
	return float32(variance / float64(len(logits)))
}

// clamp restricts v to [0, 1].
func clamp(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// #endregion helpers
