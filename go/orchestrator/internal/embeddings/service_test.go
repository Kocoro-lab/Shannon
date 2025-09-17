package embeddings

import (
	"context"
	"testing"
)

func TestUninitializedService(t *testing.T) {
	var s *Service
	if _, err := s.GenerateEmbedding(context.Background(), "hello", ""); err == nil {
		t.Fatalf("expected error when service is nil")
	}
}
