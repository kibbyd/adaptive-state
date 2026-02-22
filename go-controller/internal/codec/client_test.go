package codec

import (
	"context"
	"errors"
	"testing"

	pb "github.com/danielpatrickdp/adaptive-state/go-controller/gen/adaptive"
	"google.golang.org/grpc"
)

// #region mock
type mockCodecService struct {
	pb.CodecServiceClient

	generateResp *pb.GenerateResponse
	generateErr  error

	embedResp *pb.EmbedResponse
	embedErr  error

	searchResp *pb.SearchResponse
	searchErr  error

	storeResp *pb.StoreEvidenceResponse
	storeErr  error

	webSearchResp *pb.WebSearchResponse
	webSearchErr  error
}

func (m *mockCodecService) Generate(_ context.Context, _ *pb.GenerateRequest, _ ...grpc.CallOption) (*pb.GenerateResponse, error) {
	return m.generateResp, m.generateErr
}

func (m *mockCodecService) Embed(_ context.Context, _ *pb.EmbedRequest, _ ...grpc.CallOption) (*pb.EmbedResponse, error) {
	return m.embedResp, m.embedErr
}

func (m *mockCodecService) Search(_ context.Context, _ *pb.SearchRequest, _ ...grpc.CallOption) (*pb.SearchResponse, error) {
	return m.searchResp, m.searchErr
}

func (m *mockCodecService) StoreEvidence(_ context.Context, _ *pb.StoreEvidenceRequest, _ ...grpc.CallOption) (*pb.StoreEvidenceResponse, error) {
	return m.storeResp, m.storeErr
}

func (m *mockCodecService) WebSearch(_ context.Context, _ *pb.WebSearchRequest, _ ...grpc.CallOption) (*pb.WebSearchResponse, error) {
	return m.webSearchResp, m.webSearchErr
}

// #endregion mock

// #region constructor-tests
func TestNewCodecClientInvalidAddr(t *testing.T) {
	client, err := NewCodecClient("localhost:0")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	defer client.Close()
}

func TestNewCodecClientWithService(t *testing.T) {
	c := NewCodecClientWithService(&mockCodecService{})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.client == nil {
		t.Fatal("expected non-nil internal client")
	}
}

// #endregion constructor-tests

// #region generate-tests
func TestGenerate_Success(t *testing.T) {
	mock := &mockCodecService{
		generateResp: &pb.GenerateResponse{
			Text:    "hello world",
			Entropy: 1.5,
			Logits:  []float32{0.1, 0.2, 0.3},
		},
	}
	c := &CodecClient{client: mock}

	result, err := c.Generate(context.Background(), "prompt", [128]float32{}, []string{"ev1"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "hello world" {
		t.Errorf("expected text 'hello world', got %q", result.Text)
	}
	if result.Entropy != 1.5 {
		t.Errorf("expected entropy 1.5, got %f", result.Entropy)
	}
	if len(result.Logits) != 3 {
		t.Errorf("expected 3 logits, got %d", len(result.Logits))
	}
}

func TestGenerate_Error(t *testing.T) {
	mock := &mockCodecService{
		generateErr: errors.New("rpc failed"),
	}
	c := &CodecClient{client: mock}

	_, err := c.Generate(context.Background(), "prompt", [128]float32{}, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mock.generateErr) {
		t.Errorf("expected wrapped rpc error, got: %v", err)
	}
}

// #endregion generate-tests

// #region embed-tests
func TestEmbed_Success(t *testing.T) {
	mock := &mockCodecService{
		embedResp: &pb.EmbedResponse{
			Embedding: []float32{0.5, 0.6, 0.7},
		},
	}
	c := &CodecClient{client: mock}

	emb, err := c.Embed(context.Background(), "some text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(emb) != 3 {
		t.Errorf("expected 3 floats, got %d", len(emb))
	}
	if emb[0] != 0.5 {
		t.Errorf("expected first element 0.5, got %f", emb[0])
	}
}

func TestEmbed_Error(t *testing.T) {
	mock := &mockCodecService{
		embedErr: errors.New("embed failed"),
	}
	c := &CodecClient{client: mock}

	_, err := c.Embed(context.Background(), "text")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mock.embedErr) {
		t.Errorf("expected wrapped embed error, got: %v", err)
	}
}

// #endregion embed-tests

// #region search-tests
func TestSearch_Success(t *testing.T) {
	mock := &mockCodecService{
		searchResp: &pb.SearchResponse{
			Results: []*pb.SearchResult{
				{Id: "r1", Text: "result one", Score: 0.95, MetadataJson: `{"k":"v"}`},
				{Id: "r2", Text: "result two", Score: 0.80, MetadataJson: ""},
			},
		},
	}
	c := &CodecClient{client: mock}

	results, err := c.Search(context.Background(), "query", 5, 0.3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "r1" {
		t.Errorf("expected ID 'r1', got %q", results[0].ID)
	}
	if results[0].Text != "result one" {
		t.Errorf("expected text 'result one', got %q", results[0].Text)
	}
	if results[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", results[0].Score)
	}
	if results[0].MetadataJSON != `{"k":"v"}` {
		t.Errorf("expected metadata JSON, got %q", results[0].MetadataJSON)
	}
}

func TestSearch_Error(t *testing.T) {
	mock := &mockCodecService{
		searchErr: errors.New("search failed"),
	}
	c := &CodecClient{client: mock}

	_, err := c.Search(context.Background(), "query", 5, 0.3)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mock.searchErr) {
		t.Errorf("expected wrapped search error, got: %v", err)
	}
}

// #endregion search-tests

// #region store-evidence-tests
func TestStoreEvidence_Success(t *testing.T) {
	mock := &mockCodecService{
		storeResp: &pb.StoreEvidenceResponse{
			Id: "stored-123",
		},
	}
	c := &CodecClient{client: mock}

	id, err := c.StoreEvidence(context.Background(), "evidence text", `{"source":"test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "stored-123" {
		t.Errorf("expected id 'stored-123', got %q", id)
	}
}

func TestStoreEvidence_Error(t *testing.T) {
	mock := &mockCodecService{
		storeErr: errors.New("store failed"),
	}
	c := &CodecClient{client: mock}

	_, err := c.StoreEvidence(context.Background(), "text", "{}")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mock.storeErr) {
		t.Errorf("expected wrapped store error, got: %v", err)
	}
}

// #endregion store-evidence-tests

// #region web-search-tests
func TestWebSearch_Success(t *testing.T) {
	mock := &mockCodecService{
		webSearchResp: &pb.WebSearchResponse{
			Results: []*pb.WebSearchResult{
				{Title: "Result 1", Snippet: "Snippet 1", Url: "https://example.com/1"},
				{Title: "Result 2", Snippet: "Snippet 2", Url: "https://example.com/2"},
			},
		},
	}
	c := &CodecClient{client: mock}

	results, err := c.WebSearch(context.Background(), "test query", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Result 1" {
		t.Errorf("expected title 'Result 1', got %q", results[0].Title)
	}
	if results[0].Snippet != "Snippet 1" {
		t.Errorf("expected snippet 'Snippet 1', got %q", results[0].Snippet)
	}
	if results[0].URL != "https://example.com/1" {
		t.Errorf("expected URL 'https://example.com/1', got %q", results[0].URL)
	}
}

func TestWebSearch_Error(t *testing.T) {
	mock := &mockCodecService{
		webSearchErr: errors.New("web search failed"),
	}
	c := &CodecClient{client: mock}

	_, err := c.WebSearch(context.Background(), "test", 3)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mock.webSearchErr) {
		t.Errorf("expected wrapped web search error, got: %v", err)
	}
}

// #endregion web-search-tests
