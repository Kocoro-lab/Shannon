package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
)

// PersistDebateConsensusInput stores the outcome of a debate
type PersistDebateConsensusInput struct {
	SessionID        string                 `json:"session_id"`
	Topic            string                 `json:"topic"`           // Original query/topic
	WinningPosition  string                 `json:"winning_position"` // The consensus or winning argument
	ConsensusReached bool                   `json:"consensus_reached"`
	Confidence       float64                `json:"confidence"`
	Positions        []string               `json:"positions"`       // All debate positions
	Metadata         map[string]interface{} `json:"metadata"`
}

// PersistDebateConsensusResult indicates storage success
type PersistDebateConsensusResult struct {
	Stored bool   `json:"stored"`
	Error  string `json:"error,omitempty"`
}

// PersistDebateConsensus stores debate outcomes in the Summaries collection
// Uses type="consensus" to distinguish from other summaries (Phase 3)
func PersistDebateConsensus(ctx context.Context, in PersistDebateConsensusInput) (PersistDebateConsensusResult, error) {
	svc := embeddings.Get()
	vdb := vectordb.Get()

	if svc == nil || vdb == nil {
		return PersistDebateConsensusResult{Stored: false, Error: "services unavailable"}, nil
	}

	// Create a searchable text from the consensus
	searchableText := fmt.Sprintf("Topic: %s\nConsensus: %s", in.Topic, in.WinningPosition)

	// Generate embedding for semantic retrieval
	vec, err := svc.GenerateEmbedding(ctx, searchableText, "")
	if err != nil {
		return PersistDebateConsensusResult{Stored: false, Error: err.Error()}, nil
	}

	// Build payload with consensus metadata
	payload := map[string]interface{}{
		"type":              "consensus",  // IMPORTANT: Identifies this as consensus memory
		"session_id":        in.SessionID,
		"topic":             in.Topic,
		"winning_position":  in.WinningPosition,
		"consensus_reached": in.ConsensusReached,
		"confidence":        in.Confidence,
		"timestamp":         time.Now().Unix(),
		"searchable_text":   searchableText,
	}

	// Add positions if provided
	if len(in.Positions) > 0 {
		payload["positions"] = in.Positions
		payload["num_positions"] = len(in.Positions)
	}

	// Merge additional metadata
	for k, v := range in.Metadata {
		if k != "type" && k != "session_id" { // Protect critical keys
			payload[k] = v
		}
	}

	// Store in Summaries collection (reusing existing infrastructure)
	// This avoids creating a new collection and maintains compatibility
	if _, err := vdb.UpsertSummaryEmbedding(ctx, vec, payload); err != nil {
		return PersistDebateConsensusResult{Stored: false, Error: err.Error()}, nil
	}

	return PersistDebateConsensusResult{Stored: true}, nil
}

// FetchConsensusMemoryInput retrieves past consensus decisions
type FetchConsensusMemoryInput struct {
	Query     string  `json:"query"`      // Current topic/query
	SessionID string  `json:"session_id"` // Optional: filter by session
	Threshold float64 `json:"threshold"`  // Similarity threshold
	TopK      int     `json:"top_k"`      // Max results
}

// FetchConsensusMemoryResult contains relevant past consensus decisions
type FetchConsensusMemoryResult struct {
	Items []ConsensusItem `json:"items"`
}

// ConsensusItem represents a past consensus decision
type ConsensusItem struct {
	Topic            string  `json:"topic"`
	WinningPosition  string  `json:"winning_position"`
	Confidence       float64 `json:"confidence"`
	ConsensusReached bool    `json:"consensus_reached"`
	Similarity       float64 `json:"similarity"`
	Timestamp        int64   `json:"timestamp"`
}

// FetchConsensusMemory retrieves similar past consensus decisions
// This helps inform future debates with historical context (Phase 3)
func FetchConsensusMemory(ctx context.Context, in FetchConsensusMemoryInput) (FetchConsensusMemoryResult, error) {
	svc := embeddings.Get()
	vdb := vectordb.Get()

	if svc == nil || vdb == nil || in.Query == "" {
		return FetchConsensusMemoryResult{Items: nil}, nil
	}

	// Generate embedding for the query
	_, err := svc.GenerateEmbedding(ctx, in.Query, "")
	if err != nil {
		return FetchConsensusMemoryResult{Items: nil}, nil
	}

	// Search with type filter for consensus items
	filter := map[string]interface{}{
		"type": "consensus",
	}

	// Add session filter if provided
	if in.SessionID != "" {
		filter["session_id"] = in.SessionID
	}

	// Use threshold or default
	threshold := in.Threshold
	if threshold <= 0 {
		threshold = 0.7 // Lower threshold for consensus (more lenient)
	}

	// Determine TopK
	topK := in.TopK
	if topK <= 0 {
		topK = 3 // Default to top 3 consensus items
	}

	// Search in the summaries collection (where consensus is stored)
	// This would need a small enhancement to the search method to support
	// searching in summaries collection, or we store in task_embeddings
	// For now, we'll document this as a future enhancement

	// TODO: Add search method for summaries collection or use task_embeddings
	// For MVP, we can store consensus in task_embeddings with type="consensus"

	return FetchConsensusMemoryResult{Items: nil}, nil
}