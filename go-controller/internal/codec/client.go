package codec

import (
	"context"
	"fmt"

	pb "github.com/danielpatrickdp/adaptive-state/go-controller/gen/adaptive"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// #region types
// GenerateResult holds the response from a Generate RPC call.
type GenerateResult struct {
	Text    string
	Entropy float32
	Logits  []float32
	Context []int64
}

// SearchResult holds a single result from a Search RPC call.
type SearchResult struct {
	ID           string
	Text         string
	Score        float32
	MetadataJSON string
}

// WebSearchResult holds a single web search result from a WebSearch RPC call.
type WebSearchResult struct {
	Title   string
	Snippet string
	URL     string
}
// #endregion types

// #region client-struct
// CodecClient wraps the gRPC connection to the Python inference service.
type CodecClient struct {
	conn   *grpc.ClientConn
	client pb.CodecServiceClient
}
// #endregion client-struct

// #region constructor
// NewCodecClient connects to the Python inference gRPC server.
func NewCodecClient(addr string) (*CodecClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", addr, err)
	}
	return &CodecClient{
		conn:   conn,
		client: pb.NewCodecServiceClient(conn),
	}, nil
}
// NewCodecClientWithService creates a CodecClient with an injected service implementation.
// Used for testing without a real gRPC connection.
func NewCodecClientWithService(svc pb.CodecServiceClient) *CodecClient {
	return &CodecClient{client: svc}
}

// #endregion constructor

// #region close
// Close shuts down the gRPC connection.
func (c *CodecClient) Close() error {
	return c.conn.Close()
}
// #endregion close

// #region generate
// Generate sends a prompt with state context to the inference service.
func (c *CodecClient) Generate(ctx context.Context, prompt string, stateVec [128]float32, evidence []string, ollamaCtx []int64) (GenerateResult, error) {
	vecSlice := make([]float32, 128)
	copy(vecSlice, stateVec[:])

	resp, err := c.client.Generate(ctx, &pb.GenerateRequest{
		Prompt:      prompt,
		StateVector: vecSlice,
		Evidence:    evidence,
		Context:     ollamaCtx,
	})
	if err != nil {
		return GenerateResult{}, fmt.Errorf("generate rpc: %w", err)
	}

	return GenerateResult{
		Text:    resp.Text,
		Entropy: resp.Entropy,
		Logits:  resp.Logits,
		Context: resp.Context,
	}, nil
}
// #endregion generate

// #region embed
// Embed sends text to the inference service for embedding.
func (c *CodecClient) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := c.client.Embed(ctx, &pb.EmbedRequest{
		Text: text,
	})
	if err != nil {
		return nil, fmt.Errorf("embed rpc: %w", err)
	}
	return resp.Embedding, nil
}
// #endregion embed

// #region search
// Search queries the evidence memory store via the Python service.
func (c *CodecClient) Search(ctx context.Context, queryText string, topK int, similarityThreshold float32) ([]SearchResult, error) {
	resp, err := c.client.Search(ctx, &pb.SearchRequest{
		QueryText:           queryText,
		TopK:                int32(topK),
		SimilarityThreshold: similarityThreshold,
	})
	if err != nil {
		return nil, fmt.Errorf("search rpc: %w", err)
	}

	results := make([]SearchResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = SearchResult{
			ID:           r.Id,
			Text:         r.Text,
			Score:        r.Score,
			MetadataJSON: r.MetadataJson,
		}
	}
	return results, nil
}
// #endregion search

// #region store-evidence
// StoreEvidence stores text as evidence in the Python-side memory store.
func (c *CodecClient) StoreEvidence(ctx context.Context, text string, metadataJSON string) (string, error) {
	resp, err := c.client.StoreEvidence(ctx, &pb.StoreEvidenceRequest{
		Text:         text,
		MetadataJson: metadataJSON,
	})
	if err != nil {
		return "", fmt.Errorf("store evidence rpc: %w", err)
	}
	return resp.Id, nil
}
// #endregion store-evidence

// #region delete-evidence
// DeleteEvidence batch-deletes evidence items by ID via the Python service.
func (c *CodecClient) DeleteEvidence(ctx context.Context, ids []string) (int, error) {
	resp, err := c.client.DeleteEvidence(ctx, &pb.DeleteEvidenceRequest{
		Ids: ids,
	})
	if err != nil {
		return 0, fmt.Errorf("delete evidence rpc: %w", err)
	}
	return int(resp.DeletedCount), nil
}
// #endregion delete-evidence

// #region web-search
// WebSearch queries the web via the Python DDGS service.
func (c *CodecClient) WebSearch(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error) {
	resp, err := c.client.WebSearch(ctx, &pb.WebSearchRequest{
		Query:      query,
		MaxResults: int32(maxResults),
	})
	if err != nil {
		return nil, fmt.Errorf("web search rpc: %w", err)
	}

	results := make([]WebSearchResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = WebSearchResult{
			Title:   r.Title,
			Snippet: r.Snippet,
			URL:     r.Url,
		}
	}
	return results, nil
}
// #endregion web-search
