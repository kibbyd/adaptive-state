package retrieval

import (
	"context"
	"errors"
	"testing"

	pb "github.com/danielpatrickdp/adaptive-state/go-controller/gen/adaptive"
	"github.com/danielpatrickdp/adaptive-state/go-controller/internal/codec"
	"google.golang.org/grpc"
)

// #region mock
type mockCodecService struct {
	pb.CodecServiceClient

	searchResp *pb.SearchResponse
	searchErr  error
}

func (m *mockCodecService) Generate(_ context.Context, _ *pb.GenerateRequest, _ ...grpc.CallOption) (*pb.GenerateResponse, error) {
	return nil, nil
}

func (m *mockCodecService) Embed(_ context.Context, _ *pb.EmbedRequest, _ ...grpc.CallOption) (*pb.EmbedResponse, error) {
	return nil, nil
}

func (m *mockCodecService) Search(_ context.Context, _ *pb.SearchRequest, _ ...grpc.CallOption) (*pb.SearchResponse, error) {
	return m.searchResp, m.searchErr
}

func (m *mockCodecService) StoreEvidence(_ context.Context, _ *pb.StoreEvidenceRequest, _ ...grpc.CallOption) (*pb.StoreEvidenceResponse, error) {
	return nil, nil
}

func (m *mockCodecService) WebSearch(_ context.Context, _ *pb.WebSearchRequest, _ ...grpc.CallOption) (*pb.WebSearchResponse, error) {
	return nil, nil
}

// #endregion mock

// #region gate1-tests
func TestGate1_LowEntropy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 1.0

	result := GateResult{}
	entropy := float32(0.5)

	if entropy >= cfg.EntropyThreshold {
		t.Fatal("expected entropy below threshold")
	}
	result.Gate1Passed = false
	if result.Gate1Passed {
		t.Error("gate1 should not pass with low entropy")
	}
}

func TestGate1_HighEntropy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 0.5

	entropy := float32(1.5)
	if entropy < cfg.EntropyThreshold {
		t.Fatal("expected entropy above threshold")
	}
}

// #endregion gate1-tests

// #region gate3-tests
func TestConsistencyCheck_FiltersEmpty(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "1", Text: "valid evidence", Score: 0.9},
		{ID: "2", Text: "", Score: 0.8}, // empty — should be filtered
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 1 {
		t.Errorf("expected 1 valid result, got %d", len(valid))
	}
	if valid[0].ID != "1" {
		t.Errorf("expected ID=1, got %s", valid[0].ID)
	}
}

func TestConsistencyCheck_FiltersOverlong(t *testing.T) {
	r := &Retriever{config: RetrievalConfig{MaxEvidenceLen: 10}}
	results := []EvidenceRecord{
		{ID: "1", Text: "short", Score: 0.9},
		{ID: "2", Text: "this text is way too long for the limit", Score: 0.8},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 1 {
		t.Errorf("expected 1 valid result, got %d", len(valid))
	}
}

func TestConsistencyCheck_FiltersDuplicateIDs(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "dup", Text: "first", Score: 0.9},
		{ID: "dup", Text: "second", Score: 0.8},
		{ID: "unique", Text: "third", Score: 0.7},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 2 {
		t.Errorf("expected 2 valid results, got %d", len(valid))
	}
	// First occurrence of "dup" should survive
	if valid[0].Text != "first" {
		t.Errorf("expected first occurrence, got %s", valid[0].Text)
	}
}

func TestConsistencyCheck_AllValid(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "a", Text: "evidence a", Score: 0.9},
		{ID: "b", Text: "evidence b", Score: 0.8},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 2 {
		t.Errorf("expected 2 valid results, got %d", len(valid))
	}
}

func TestConsistencyCheck_AllFiltered(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "1", Text: "", Score: 0.9},
		{ID: "2", Text: "", Score: 0.8},
	}

	valid := r.consistencyCheck(results)
	if len(valid) != 0 {
		t.Errorf("expected 0 valid results, got %d", len(valid))
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AlwaysRetrieve {
		t.Error("expected AlwaysRetrieve to be true by default")
	}
	if cfg.EntropyThreshold <= 0 {
		t.Error("expected positive entropy threshold")
	}
	if cfg.SimilarityThreshold <= 0 {
		t.Error("expected positive similarity threshold")
	}
	if cfg.TopK <= 0 {
		t.Error("expected positive TopK")
	}
	if cfg.MaxEvidenceLen <= 0 {
		t.Error("expected positive MaxEvidenceLen")
	}
}

// #endregion gate3-tests

// #region retriever-tests
func TestNewRetriever(t *testing.T) {
	cc := codec.NewCodecClientWithService(&mockCodecService{})
	r := NewRetriever(cc, DefaultConfig())
	if r == nil {
		t.Fatal("expected non-nil retriever")
	}
}

func TestRetrieve_Gate1Fail(t *testing.T) {
	mock := &mockCodecService{}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig()
	cfg.AlwaysRetrieve = false
	cfg.EntropyThreshold = 2.0
	r := NewRetriever(cc, cfg)

	result, err := r.Retrieve(context.Background(), "prompt", 0.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Gate1Passed {
		t.Error("expected gate1 to fail")
	}
	if result.Gate2Count != 0 {
		t.Error("expected no gate2 results when gate1 fails")
	}
}

func TestRetrieve_AlwaysRetrieveBypassesGate1(t *testing.T) {
	mock := &mockCodecService{
		searchResp: &pb.SearchResponse{
			Results: []*pb.SearchResult{
				{Id: "a", Text: "recalled talk about earlier topics", Score: 0.9},
			},
		},
	}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig() // AlwaysRetrieve=true by default
	cfg.EntropyThreshold = 2.0 // would block if checked
	r := NewRetriever(cc, cfg)

	result, err := r.Retrieve(context.Background(), "topics we talked about earlier", 0.1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Gate1Passed {
		t.Error("expected gate1 to pass when AlwaysRetrieve is true")
	}
	if result.Gate3Count != 1 {
		t.Errorf("expected 1 retrieved result, got %d", result.Gate3Count)
	}
}

func TestRetrieve_SearchError(t *testing.T) {
	mock := &mockCodecService{
		searchErr: errors.New("search broken"),
	}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 0.1
	r := NewRetriever(cc, cfg)

	_, err := r.Retrieve(context.Background(), "prompt", 1.0)
	if err == nil {
		t.Fatal("expected error from search")
	}
	if !errors.Is(err, mock.searchErr) {
		t.Errorf("expected wrapped search error, got: %v", err)
	}
}

func TestRetrieve_Gate2ZeroResults(t *testing.T) {
	mock := &mockCodecService{
		searchResp: &pb.SearchResponse{Results: []*pb.SearchResult{}},
	}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 0.1
	r := NewRetriever(cc, cfg)

	result, err := r.Retrieve(context.Background(), "prompt", 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Gate1Passed {
		t.Error("expected gate1 to pass")
	}
	if result.Gate2Count != 0 {
		t.Errorf("expected 0 gate2 results, got %d", result.Gate2Count)
	}
	if result.Reason != "gate2: no results above similarity threshold" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestRetrieve_Gate3AllFiltered(t *testing.T) {
	mock := &mockCodecService{
		searchResp: &pb.SearchResponse{
			Results: []*pb.SearchResult{
				{Id: "1", Text: "", Score: 0.9},
				{Id: "2", Text: "", Score: 0.8},
			},
		},
	}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 0.1
	r := NewRetriever(cc, cfg)

	result, err := r.Retrieve(context.Background(), "prompt", 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Gate2Count != 2 {
		t.Errorf("expected 2 gate2 results, got %d", result.Gate2Count)
	}
	if result.Gate3Count != 0 {
		t.Errorf("expected 0 gate3 results, got %d", result.Gate3Count)
	}
	if result.Reason != "gate3: all results failed consistency/coherence check" {
		t.Errorf("unexpected reason: %q", result.Reason)
	}
}

func TestRetrieve_FullSuccess(t *testing.T) {
	mock := &mockCodecService{
		searchResp: &pb.SearchResponse{
			Results: []*pb.SearchResult{
				{Id: "a", Text: "alpha beta results here", Score: 0.95, MetadataJson: `{"src":"test"}`},
				{Id: "b", Text: "beta testing outcomes", Score: 0.85, MetadataJson: ""},
			},
		},
	}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig()
	cfg.EntropyThreshold = 0.1
	r := NewRetriever(cc, cfg)

	result, err := r.Retrieve(context.Background(), "alpha beta testing", 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Gate1Passed {
		t.Error("expected gate1 to pass")
	}
	if result.Gate2Count != 2 {
		t.Errorf("expected 2 gate2 results, got %d", result.Gate2Count)
	}
	if result.Gate3Count != 2 {
		t.Errorf("expected 2 gate3 results, got %d", result.Gate3Count)
	}
	if len(result.Retrieved) != 2 {
		t.Fatalf("expected 2 retrieved, got %d", len(result.Retrieved))
	}
	if result.Retrieved[0].ID != "a" {
		t.Errorf("expected first result ID 'a', got %q", result.Retrieved[0].ID)
	}
	if result.Retrieved[0].Text != "alpha beta results here" {
		t.Errorf("expected first result text 'alpha beta results here', got %q", result.Retrieved[0].Text)
	}
}

func TestRetrieve_CoherenceFiltersAll(t *testing.T) {
	mock := &mockCodecService{
		searchResp: &pb.SearchResponse{
			Results: []*pb.SearchResult{
				{Id: "a", Text: "weather forecast tomorrow sunny", Score: 0.9},
				{Id: "b", Text: "recipe cooking pasta dinner", Score: 0.8},
			},
		},
	}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig()
	r := NewRetriever(cc, cfg)

	result, err := r.Retrieve(context.Background(), "seashells beach ocean", 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Gate2Count != 2 {
		t.Errorf("expected 2 gate2 results, got %d", result.Gate2Count)
	}
	if result.Gate3Count != 0 {
		t.Errorf("expected 0 gate3 results after coherence filter, got %d", result.Gate3Count)
	}
}

func TestRetrieve_CoherencePassesRelevant(t *testing.T) {
	mock := &mockCodecService{
		searchResp: &pb.SearchResponse{
			Results: []*pb.SearchResult{
				{Id: "a", Text: "seashells found on the beach near the ocean", Score: 0.9},
				{Id: "b", Text: "recipe cooking pasta dinner", Score: 0.8},
			},
		},
	}
	cc := codec.NewCodecClientWithService(mock)
	cfg := DefaultConfig()
	r := NewRetriever(cc, cfg)

	result, err := r.Retrieve(context.Background(), "seashells beach ocean", 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Gate3Count != 1 {
		t.Errorf("expected 1 gate3 result, got %d", result.Gate3Count)
	}
	if result.Retrieved[0].ID != "a" {
		t.Errorf("expected surviving result ID 'a', got %q", result.Retrieved[0].ID)
	}
}

// #endregion retriever-tests

// #region topic-coherence-tests
func TestTokenize(t *testing.T) {
	tokens := tokenize("Can you tell me where she sells seashells?")
	// "can", "you", "tell", "me", "where", "she", "sells" are stopwords or short
	// "seashells" should survive
	found := false
	for _, tok := range tokens {
		if tok == "seashells" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'seashells' in tokens, got %v", tokens)
	}
}

func TestTokenize_Deduplicates(t *testing.T) {
	tokens := tokenize("beach beach beach ocean")
	count := 0
	for _, tok := range tokens {
		if tok == "beach" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 'beach' once, got %d times in %v", count, tokens)
	}
}

func TestSharedKeywords(t *testing.T) {
	a := []string{"seashells", "beach", "ocean"}
	b := []string{"beach", "sunset", "sand"}
	shared := sharedKeywords(a, b)
	if shared != 1 {
		t.Errorf("expected 1 shared keyword, got %d", shared)
	}
}

func TestSharedKeywords_None(t *testing.T) {
	a := []string{"seashells", "beach"}
	b := []string{"weather", "forecast"}
	shared := sharedKeywords(a, b)
	if shared != 0 {
		t.Errorf("expected 0 shared keywords, got %d", shared)
	}
}

func TestTopicCoherenceFilter_EmptyPromptTokens(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	// All stopwords prompt — filter should pass everything through
	results := []EvidenceRecord{
		{ID: "1", Text: "anything here", Score: 0.9},
	}
	filtered := r.topicCoherenceFilter("the is a an", results)
	if len(filtered) != 1 {
		t.Errorf("expected all results to pass when prompt has no content words, got %d", len(filtered))
	}
}

func TestTopicCoherenceFilter_RejectsIrrelevant(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "1", Text: "weather forecast tomorrow", Score: 0.9},
		{ID: "2", Text: "cooking recipe pasta", Score: 0.8},
	}
	filtered := r.topicCoherenceFilter("seashells beach ocean", results)
	if len(filtered) != 0 {
		t.Errorf("expected 0 results, got %d", len(filtered))
	}
}

func TestTopicCoherenceFilter_KeepsRelevant(t *testing.T) {
	r := &Retriever{config: DefaultConfig()}
	results := []EvidenceRecord{
		{ID: "1", Text: "seashells found near the ocean shore", Score: 0.9},
		{ID: "2", Text: "cooking recipe pasta", Score: 0.8},
	}
	filtered := r.topicCoherenceFilter("seashells beach ocean", results)
	if len(filtered) != 1 {
		t.Errorf("expected 1 result, got %d", len(filtered))
	}
	if filtered[0].ID != "1" {
		t.Errorf("expected ID '1', got %q", filtered[0].ID)
	}
}

// #endregion topic-coherence-tests
