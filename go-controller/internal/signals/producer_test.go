package signals

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/retrieval"
)

// #region mock

// mockEmbedder returns pre-configured embeddings or errors.
type mockEmbedder struct {
	embeddings map[string][]float32
	err        error
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}
	return nil, errors.New("no embedding for: " + text)
}

// #endregion mock

// #region sentiment-tests

func TestSentimentScore_EmptyResponse(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	score := p.sentimentScore(ProduceInput{ResponseText: "", Entropy: 0.3})
	if score != 0 {
		t.Errorf("expected 0 for empty response, got %f", score)
	}
}

func TestSentimentScore_HighDiversity(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	// All unique words, low entropy → high score
	score := p.sentimentScore(ProduceInput{
		ResponseText: "the quick brown fox jumps over lazy dog",
		Entropy:      0.1,
	})
	if score < 0.5 {
		t.Errorf("expected high sentiment for diverse text, got %f", score)
	}
}

func TestSentimentScore_LowDiversity(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	// Repeated words → low diversity
	score := p.sentimentScore(ProduceInput{
		ResponseText: "the the the the the the the the",
		Entropy:      0.1,
	})
	if score > 0.3 {
		t.Errorf("expected low sentiment for repetitive text, got %f", score)
	}
}

func TestSentimentScore_HighEntropy(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	// High entropy → low confidence → low score
	score := p.sentimentScore(ProduceInput{
		ResponseText: "the quick brown fox",
		Entropy:      0.95,
	})
	if score > 0.1 {
		t.Errorf("expected low sentiment with high entropy, got %f", score)
	}
}

// #endregion sentiment-tests

// #region coherence-tests

func TestCoherenceScore_SimilarTexts(t *testing.T) {
	emb := &mockEmbedder{embeddings: map[string][]float32{
		"hello world": {1, 0, 0},
		"hello there": {0.9, 0.1, 0},
	}}
	p := NewProducer(emb, DefaultProducerConfig())
	score := p.coherenceScore(context.Background(), ProduceInput{
		Prompt: "hello world", ResponseText: "hello there",
	})
	if score < 0.8 {
		t.Errorf("expected high coherence for similar texts, got %f", score)
	}
}

func TestCoherenceScore_DissimilarTexts(t *testing.T) {
	emb := &mockEmbedder{embeddings: map[string][]float32{
		"hello": {1, 0, 0},
		"bye":   {0, 1, 0},
	}}
	p := NewProducer(emb, DefaultProducerConfig())
	score := p.coherenceScore(context.Background(), ProduceInput{
		Prompt: "hello", ResponseText: "bye",
	})
	if score > 0.1 {
		t.Errorf("expected low coherence for dissimilar texts, got %f", score)
	}
}

func TestCoherenceScore_EmbedError(t *testing.T) {
	emb := &mockEmbedder{err: errors.New("rpc failed")}
	p := NewProducer(emb, DefaultProducerConfig())
	score := p.coherenceScore(context.Background(), ProduceInput{
		Prompt: "hello", ResponseText: "world",
	})
	if score != 0 {
		t.Errorf("expected 0 on embed error, got %f", score)
	}
}

func TestCoherenceScore_SecondEmbedError(t *testing.T) {
	// Prompt embedding succeeds, response embedding fails
	emb := &mockEmbedder{embeddings: map[string][]float32{
		"hello": {1, 0, 0},
	}}
	p := NewProducer(emb, DefaultProducerConfig())
	score := p.coherenceScore(context.Background(), ProduceInput{
		Prompt: "hello", ResponseText: "unknown",
	})
	if score != 0 {
		t.Errorf("expected 0 on second embed error, got %f", score)
	}
}

func TestCoherenceScore_NilEmbedder(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	score := p.coherenceScore(context.Background(), ProduceInput{
		Prompt: "hello", ResponseText: "world",
	})
	if score != 0 {
		t.Errorf("expected 0 with nil embedder, got %f", score)
	}
}

// #endregion coherence-tests

// #region novelty-tests

func TestNoveltyScore_WithRetrieval(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	score := p.noveltyScore(ProduceInput{
		Retrieved: []retrieval.EvidenceRecord{
			{Score: 0.8},
			{Score: 0.6},
		},
	})
	expected := float32(0.2) // 1 - 0.8
	if diff := score - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected ~%f, got %f", expected, score)
	}
}

func TestNoveltyScore_WithLogits(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	// Logits with some variance
	score := p.noveltyScore(ProduceInput{
		Logits: []float32{1.0, 2.0, 3.0, 4.0, 5.0},
	})
	// variance = 2.0, tanh(2.0) ≈ 0.964
	if score < 0.9 {
		t.Errorf("expected high novelty from logit variance, got %f", score)
	}
}

func TestNoveltyScore_EntropyFallback(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	score := p.noveltyScore(ProduceInput{Entropy: 0.7})
	if diff := score - 0.7; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected ~0.7 from entropy fallback, got %f", score)
	}
}

// #endregion novelty-tests

// #region risk-tests

func TestRiskFlag_BelowThreshold(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	// 0.5 < 0.75 threshold → no flag
	if p.riskFlag(ProduceInput{Entropy: 0.5}) {
		t.Error("expected no risk flag below threshold")
	}
}

func TestRiskFlag_AboveThreshold(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	// 0.9 >= 0.75 threshold → flag
	if !p.riskFlag(ProduceInput{Entropy: 0.9}) {
		t.Error("expected risk flag above threshold")
	}
}

func TestRiskFlag_ExactThreshold(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	// Exact threshold: 0.5 * 1.5 = 0.75
	if !p.riskFlag(ProduceInput{Entropy: 0.75}) {
		t.Error("expected risk flag at exact threshold")
	}
}

// #endregion risk-tests

// #region correction-tests

func TestUserCorrection_Passthrough(t *testing.T) {
	p := NewProducer(nil, DefaultProducerConfig())
	sigs := p.Produce(context.Background(), ProduceInput{UserCorrect: true})
	if !sigs.UserCorrection {
		t.Error("expected UserCorrection to pass through")
	}
	sigs2 := p.Produce(context.Background(), ProduceInput{UserCorrect: false})
	if sigs2.UserCorrection {
		t.Error("expected UserCorrection=false to pass through")
	}
}

// #endregion correction-tests

// #region integration-tests

func TestProduce_Integration(t *testing.T) {
	emb := &mockEmbedder{embeddings: map[string][]float32{
		"what is go": {1, 0, 0},
		"Go is a programming language": {0.8, 0.2, 0},
	}}
	p := NewProducer(emb, DefaultProducerConfig())
	sigs := p.Produce(context.Background(), ProduceInput{
		Prompt:       "what is go",
		ResponseText: "Go is a programming language",
		Entropy:      0.3,
		Retrieved: []retrieval.EvidenceRecord{
			{Score: 0.7},
		},
	})

	if sigs.SentimentScore <= 0 {
		t.Errorf("expected positive sentiment, got %f", sigs.SentimentScore)
	}
	if sigs.CoherenceScore <= 0 {
		t.Errorf("expected positive coherence, got %f", sigs.CoherenceScore)
	}
	// Novelty: 1 - 0.7 = 0.3
	if diff := sigs.NoveltyScore - 0.3; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected novelty ~0.3, got %f", sigs.NoveltyScore)
	}
	if sigs.RiskFlag {
		t.Error("expected no risk flag at entropy 0.3")
	}
	if sigs.ToolFailure {
		t.Error("expected ToolFailure=false")
	}
	if sigs.ConstraintViolation {
		t.Error("expected ConstraintViolation=false")
	}
}

// #endregion integration-tests

// #region helper-tests

func TestCosineSimilarity_ZeroVectors(t *testing.T) {
	result := cosineSimilarity([]float32{0, 0, 0}, []float32{0, 0, 0})
	if result != 0 {
		t.Errorf("expected 0 for zero vectors, got %f", result)
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	result := cosineSimilarity([]float32{1, 2, 3}, []float32{1, 2, 3})
	if diff := result - 1.0; diff > 0.001 || diff < -0.001 {
		t.Errorf("expected 1.0 for identical vectors, got %f", result)
	}
}

func TestCosineSimilarity_Mismatched(t *testing.T) {
	result := cosineSimilarity([]float32{1, 2}, []float32{1, 2, 3})
	if result != 0 {
		t.Errorf("expected 0 for mismatched lengths, got %f", result)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	result := cosineSimilarity([]float32{}, []float32{})
	if result != 0 {
		t.Errorf("expected 0 for empty vectors, got %f", result)
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello World  hello")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d", len(tokens))
	}
	if tokens[0] != "hello" || tokens[1] != "world" || tokens[2] != "hello" {
		t.Errorf("unexpected tokens: %v", tokens)
	}
}

func TestTokenize_Empty(t *testing.T) {
	tokens := tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for empty string, got %d", len(tokens))
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		in, want float32
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tt := range tests {
		got := clamp(tt.in)
		if got != tt.want {
			t.Errorf("clamp(%f) = %f, want %f", tt.in, got, tt.want)
		}
	}
}

func TestLogitVariance(t *testing.T) {
	v := logitVariance([]float32{1, 2, 3, 4, 5})
	// mean=3, var = (4+1+0+1+4)/5 = 2.0
	if diff := math.Abs(float64(v) - 2.0); diff > 0.001 {
		t.Errorf("expected variance ~2.0, got %f", v)
	}
}

func TestLogitVariance_Empty(t *testing.T) {
	v := logitVariance(nil)
	if v != 0 {
		t.Errorf("expected 0 for nil logits, got %f", v)
	}
}

// #endregion helper-tests
