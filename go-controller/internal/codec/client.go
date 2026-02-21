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
// #endregion constructor

// #region close
// Close shuts down the gRPC connection.
func (c *CodecClient) Close() error {
	return c.conn.Close()
}
// #endregion close

// #region generate
// Generate sends a prompt with state context to the inference service.
func (c *CodecClient) Generate(ctx context.Context, prompt string, stateVec [128]float32, evidence []string) (GenerateResult, error) {
	vecSlice := make([]float32, 128)
	copy(vecSlice, stateVec[:])

	resp, err := c.client.Generate(ctx, &pb.GenerateRequest{
		Prompt:      prompt,
		StateVector: vecSlice,
		Evidence:    evidence,
	})
	if err != nil {
		return GenerateResult{}, fmt.Errorf("generate rpc: %w", err)
	}

	return GenerateResult{
		Text:    resp.Text,
		Entropy: resp.Entropy,
		Logits:  resp.Logits,
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
