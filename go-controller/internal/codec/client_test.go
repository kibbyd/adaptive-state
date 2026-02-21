package codec

import (
	"testing"
)

func TestNewCodecClientInvalidAddr(t *testing.T) {
	// NewClient with grpc.NewClient doesn't eagerly connect, so this should succeed.
	// The actual connection failure happens on first RPC call.
	client, err := NewCodecClient("localhost:0")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	defer client.Close()
}
