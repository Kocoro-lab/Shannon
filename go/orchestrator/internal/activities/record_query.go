package activities

import (
	"context"
	"regexp"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
)

// RecordQueryInput carries information to store a query and its result
type RecordQueryInput struct {
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	Query     string                 `json:"query"`
	Answer    string                 `json:"answer"`
	Model     string                 `json:"model"`
	Metadata  map[string]interface{} `json:"metadata"`
	RedactPII bool                   `json:"redact_pii"`
}

type RecordQueryResult struct {
	Stored bool   `json:"stored"`
	Error  string `json:"error,omitempty"`
}

var (
	emailRe = regexp.MustCompile(`([a-zA-Z0-9_.%+-]+)@([a-zA-Z0-9.-]+)`)
	phoneRe = regexp.MustCompile(`\b\+?[0-9][0-9\-\s]{6,}[0-9]\b`)
)

func redact(text string) string {
	if text == "" {
		return text
	}
	out := emailRe.ReplaceAllString(text, "***@***")
	out = phoneRe.ReplaceAllString(out, "***PHONE***")
	return out
}

// recordQueryCore contains the shared logic for storing queries in the vector database
// This is used by both RecordQuery and RecordAgentMemory activities to avoid
// activities calling other activities directly
func recordQueryCore(ctx context.Context, in RecordQueryInput) (RecordQueryResult, error) {
	svc := embeddings.Get()
	vdb := vectordb.Get()
	if svc == nil || vdb == nil {
		return RecordQueryResult{Stored: false, Error: "vector services unavailable"}, nil
	}
	q := in.Query
	a := in.Answer
	if in.RedactPII {
		q = redact(q)
		a = redact(a)
	}

	// Always use the default embedding model (not the chat model)
	// Chat models (like gpt-4) are different from embedding models (like text-embedding-3-small)
	vec, err := svc.GenerateEmbedding(ctx, q, "")
	if err != nil {
		return RecordQueryResult{Stored: false, Error: err.Error()}, nil
	}
	// Build payload
	payload := map[string]interface{}{
		"query":      q,
		"answer":     a,
		"session_id": in.SessionID,
		"user_id":    in.UserID,
		"model":      in.Model,
		"timestamp":  time.Now().Unix(),
	}
	for k, v := range in.Metadata {
		payload[k] = v
	}
	// Upsert point
	if _, err := vdb.UpsertTaskEmbedding(ctx, vec, payload); err != nil {
		return RecordQueryResult{Stored: false, Error: err.Error()}, nil
	}
	return RecordQueryResult{Stored: true}, nil
}

// RecordQuery generates an embedding and upserts to Qdrant (TaskEmbeddings)
// This is a Temporal activity that wraps the core logic
func RecordQuery(ctx context.Context, in RecordQueryInput) (RecordQueryResult, error) {
	return recordQueryCore(ctx, in)
}
