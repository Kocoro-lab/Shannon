package activities

import (
	"context"
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

// FetchAgentMemory filters existing session memory by agent_id.
// This minimal shim builds on FetchSessionMemory without changing the vector DB API.
func FetchAgentMemory(ctx context.Context, in FetchAgentMemoryInput) (FetchAgentMemoryResult, error) {
	if in.SessionID == "" || in.AgentID == "" {
		return FetchAgentMemoryResult{Items: nil}, nil
	}
	// Reuse existing session memory retrieval
	sm, err := FetchSessionMemory(ctx, FetchSessionMemoryInput{SessionID: in.SessionID, TenantID: in.TenantID, TopK: in.TopK})
	if err != nil {
		return FetchAgentMemoryResult{Items: nil}, nil
	}
	if len(sm.Items) == 0 {
		return FetchAgentMemoryResult{Items: nil}, nil
	}
	out := make([]map[string]interface{}, 0, len(sm.Items))
	for _, it := range sm.Items {
		if it == nil {
			continue
		}
		if v, ok := it["agent_id"]; ok {
			if sid, ok2 := v.(string); ok2 && sid == in.AgentID {
				out = append(out, it)
			}
		}
	}
	return FetchAgentMemoryResult{Items: out}, nil
}

// RecordAgentMemoryInput stores an agent-scoped interaction into the vector store via RecordQuery
type RecordAgentMemoryInput struct {
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
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
		Query:     in.Query,
		Answer:    in.Answer,
		Model:     in.Model,
		Metadata:  meta,
		RedactPII: in.RedactPII,
	})
}
