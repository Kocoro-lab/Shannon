package activities

import (
	"context"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
)

// FetchAgentMemoryInput requests agent-scoped items within a session
type FetchAgentMemoryInput struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	AgentID   string `json:"agent_id"`
	TopK      int    `json:"top_k"`
}

// FetchAgentMemoryResult contains retrieved items for merging
type FetchAgentMemoryResult struct {
	Items []map[string]interface{} `json:"items"`
}

// FetchAgentMemory retrieves agent-specific memory by filtering in Qdrant.
// This now uses a dedicated vectordb method that filters by both session_id and agent_id.
func FetchAgentMemory(ctx context.Context, in FetchAgentMemoryInput) (FetchAgentMemoryResult, error) {
	if in.SessionID == "" || in.AgentID == "" {
		return FetchAgentMemoryResult{Items: nil}, nil
	}

	// Get vectordb client
	vdb := vectordb.Get()
	if vdb == nil {
		return FetchAgentMemoryResult{Items: nil}, nil
	}

	// Use dedicated agent context method that filters in Qdrant
	items, err := vdb.GetAgentContext(ctx, in.SessionID, in.AgentID, in.TenantID, in.TopK)
	if err != nil {
		// Graceful degradation
		return FetchAgentMemoryResult{Items: nil}, nil
	}

	// Convert to map format
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if item.Payload != nil {
			out = append(out, item.Payload)
		}
	}
	return FetchAgentMemoryResult{Items: out}, nil
}

// RecordAgentMemoryInput stores an agent-scoped interaction into the vector store via RecordQuery
type RecordAgentMemoryInput struct {
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	TenantID  string                 `json:"tenant_id"`
	AgentID   string                 `json:"agent_id"`
	Role      string                 `json:"role"`
	Query     string                 `json:"query"`
	Answer    string                 `json:"answer"`
	Model     string                 `json:"model"`
	RedactPII bool                   `json:"redact_pii"`
	Extra     map[string]interface{} `json:"extra"`
}

// RecordAgentMemory stores agent-specific memory in the vector database
// This is a Temporal activity that uses the shared vector storage logic
func RecordAgentMemory(ctx context.Context, in RecordAgentMemoryInput) (RecordQueryResult, error) {
	meta := map[string]interface{}{
		"agent_id": in.AgentID,
		"role":     in.Role,
		"source":   "agent",
	}
	for k, v := range in.Extra {
		meta[k] = v
	}
	// Use the shared helper function instead of calling RecordQuery directly
	return recordQueryCore(ctx, RecordQueryInput{
		SessionID: in.SessionID,
		UserID:    in.UserID,
		TenantID:  in.TenantID,
		Query:     in.Query,
		Answer:    in.Answer,
		Model:     in.Model,
		Metadata:  meta,
		RedactPII: in.RedactPII,
	})
}
