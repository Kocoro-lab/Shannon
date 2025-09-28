package activities

import (
	"context"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
)

// FetchSessionMemoryInput requests session-scoped context items
type FetchSessionMemoryInput struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	TopK      int    `json:"top_k"`
}

// FetchSessionMemoryResult contains retrieved items for merging
type FetchSessionMemoryResult struct {
	Items []map[string]interface{} `json:"items"`
}

// FetchSessionMemory fetches recent items for a session from Qdrant
func FetchSessionMemory(ctx context.Context, in FetchSessionMemoryInput) (FetchSessionMemoryResult, error) {
	vdb := vectordb.Get()
	if vdb == nil || in.SessionID == "" {
		return FetchSessionMemoryResult{Items: nil}, nil
	}
	items, err := vdb.GetSessionContext(ctx, in.SessionID, in.TenantID, in.TopK)
	if err != nil {
		// Record metrics for failed fetch (miss)
		metrics.MemoryFetches.WithLabelValues("session", "qdrant", "miss").Inc()
		metrics.MemoryItemsRetrieved.WithLabelValues("session", "qdrant").Observe(0)
		return FetchSessionMemoryResult{Items: nil}, nil
	}

	// Record metrics based on whether we found items
	if len(items) == 0 {
		metrics.MemoryFetches.WithLabelValues("session", "qdrant", "miss").Inc()
	} else {
		metrics.MemoryFetches.WithLabelValues("session", "qdrant", "hit").Inc()
	}
	metrics.MemoryItemsRetrieved.WithLabelValues("session", "qdrant").Observe(float64(len(items)))

	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, it.Payload)
	}
	return FetchSessionMemoryResult{Items: out}, nil
}
