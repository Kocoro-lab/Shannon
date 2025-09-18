package vectordb

import (
	"context"
	"testing"
)

func TestClientDisabled(t *testing.T) {
	Initialize(Config{Enabled: false})
	c := Get()
	if c == nil {
		t.Skip("client not initialized")
	}
	if _, err := c.FindSimilarQueries(context.Background(), []float32{0.1, 0.2}, 3); err == nil {
		t.Fatalf("expected error when vectordb disabled")
	}
}
