package activities

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
)

// RecordQueryInput carries information to store a query and its result
type RecordQueryInput struct {
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	TenantID  string                 `json:"tenant_id"`
	Query     string                 `json:"query"`
	Answer    string                 `json:"answer"`
	Model     string                 `json:"model"`
	Metadata  map[string]interface{} `json:"metadata"`
	RedactPII bool                   `json:"redact_pii"`
}

type RecordQueryResult struct {
	Stored        bool   `json:"stored"`
	Error         string `json:"error,omitempty"`
	ChunksCreated int    `json:"chunks_created,omitempty"`
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

// containsError checks if the response is likely an error message
func containsError(text string) bool {
	lowerText := strings.ToLower(text)
	errorPhrases := []string{
		"error:",
		"failed to",
		"unable to",
		"could not",
		"exception",
		"invalid",
		"not found",
		"denied",
		"unauthorized",
	}
	for _, phrase := range errorPhrases {
		if strings.Contains(lowerText, phrase) {
			return true
		}
	}
	return false
}

// recordQueryCore contains the shared logic for storing queries in the vector database
// This is used by both RecordQuery and RecordAgentMemory activities to avoid
// activities calling other activities directly
func recordQueryCore(ctx context.Context, in RecordQueryInput) (RecordQueryResult, error) {
	logger := activity.GetLogger(ctx)
	svc := embeddings.Get()
	vdb := vectordb.Get()
	if svc == nil || vdb == nil {
		logger.Warn("Vector services unavailable for RecordQuery",
			"session_id", in.SessionID,
			"user_id", in.UserID)
		return RecordQueryResult{Stored: false, Error: "vector services unavailable"}, nil
	}
	q := in.Query
	a := in.Answer
	if in.RedactPII {
		q = redact(q)
		a = redact(a)
	}

	// Skip storing very short or error responses
	if len(a) < 50 || containsError(a) {
		metrics.MemoryWritesSkipped.WithLabelValues("low_value").Inc()
		return RecordQueryResult{Stored: false, Error: "response too short or error message"}, nil
	}

	// Generate embedding for the query to check for duplicates
	queryEmbedding, err := svc.GenerateEmbedding(ctx, q, "")
	if err != nil {
		logger.Warn("Failed to generate query embedding",
			"session_id", in.SessionID,
			"error", err)
		return RecordQueryResult{Stored: false, Error: err.Error()}, nil
	}

	// Check for near-duplicates with high similarity threshold (95%)
	const duplicateThreshold = 0.95

	// Search for similar existing memories in this session
	similarItems, err := vdb.GetSessionContextSemanticByEmbedding(ctx, queryEmbedding, in.SessionID, in.TenantID, 1, duplicateThreshold)
	if err == nil && len(similarItems) > 0 {
		// Check if the similar item has the same query (near exact match)
		for _, item := range similarItems {
			if _, ok := item.Payload["query"].(string); ok {
				// If query is very similar (>95% similarity), skip storing
				if item.Score > duplicateThreshold {
					metrics.MemoryWritesSkipped.WithLabelValues("duplicate").Inc()
					return RecordQueryResult{Stored: false, Error: "near-duplicate memory already exists"}, nil
				}
			}
		}
	}

	// Get embedding service config for chunking settings
	config := svc.GetConfig()

	// Check if chunking is needed
	if config.Chunking.Enabled {
		chunker := embeddings.NewChunker(config.Chunking)

		// Try to chunk the answer (long answers are the main issue)
		chunks := chunker.ChunkText(a)

		if len(chunks) > 0 {
			// Batch embed all chunk texts for efficiency
			chunkTexts := make([]string, len(chunks))
			for i, chunk := range chunks {
				chunkTexts[i] = chunk.Text
			}

			// Generate embeddings in batch
			embeddings, err := svc.GenerateBatchEmbeddings(ctx, chunkTexts, "")
			if err != nil {
				logger.Warn("Failed to generate batch embeddings for chunks",
					"session_id", in.SessionID,
					"chunk_count", len(chunkTexts),
					"error", err)
				return RecordQueryResult{Stored: false, Error: err.Error()}, nil
			}

			// Create points for each chunk
			points := make([]vectordb.UpsertItem, 0, len(chunks))
			timestamp := time.Now().Unix()

			for i, chunk := range chunks {
				// Build payload with chunk metadata
				payload := map[string]interface{}{
					"query":        q,                  // Original query
					"chunk_text":   chunk.Text,         // The actual chunk (no full answer stored)
					"qa_id":        chunk.QAID,         // UUID for this Q&A pair
					"chunk_index":  chunk.Index,        // 0-based chunk position
					"chunk_count":  chunk.TotalCount,   // Total chunks for this Q&A
					"is_chunked":   true,               // Flag for retrieval
					"session_id":   in.SessionID,
					"user_id":      in.UserID,
					"tenant_id":    in.TenantID,
					"model":        in.Model,
					"timestamp":    timestamp,
				}

				// Add any additional metadata
				for k, v := range in.Metadata {
					payload[k] = v
				}

				// Create point with UUID for Qdrant compatibility
				// Store qa_id and chunk_index in payload for idempotency checks
				point := vectordb.UpsertItem{
					ID:      uuid.New().String(),
					Vector:  embeddings[i],
					Payload: payload,
				}
				points = append(points, point)
			}

			// Batch upsert all chunks
			if _, err := vdb.Upsert(ctx, vdb.GetConfig().TaskEmbeddings, points); err != nil {
				logger.Warn("Failed to batch upsert chunks to vector store",
					"session_id", in.SessionID,
					"chunk_count", len(points),
					"error", err)
				return RecordQueryResult{Stored: false, Error: err.Error()}, nil
			}

			// Record chunking metrics
			if len(chunks) > 0 {
				totalTokens := 0
				for _, chunk := range chunks {
					// Estimate tokens (rough approximation: 1 token ≈ 4 chars)
					totalTokens += len(chunk.Text) / 4
				}
				avgTokensPerChunk := float64(totalTokens) / float64(len(chunks))
				metrics.RecordChunkingMetrics(in.SessionID, len(chunks), avgTokensPerChunk)
			}

			return RecordQueryResult{Stored: true, ChunksCreated: len(chunks)}, nil
		}
	}

	// Single vector path (no chunking needed or chunking disabled)
	// Embed the answer for consistency with chunked path
	vec, err := svc.GenerateEmbedding(ctx, a, "")
	if err != nil {
		logger.Warn("Failed to generate answer embedding",
			"session_id", in.SessionID,
			"error", err)
		return RecordQueryResult{Stored: false, Error: err.Error()}, nil
	}

	// Build payload
	payload := map[string]interface{}{
		"query":      q,
		"answer":     a,
		"is_chunked": false,  // Flag for retrieval
		"session_id": in.SessionID,
		"user_id":    in.UserID,
		"tenant_id":  in.TenantID,
		"model":      in.Model,
		"timestamp":  time.Now().Unix(),
	}
	for k, v := range in.Metadata {
		payload[k] = v
	}

	// Upsert point
	if _, err := vdb.UpsertTaskEmbedding(ctx, vec, payload); err != nil {
		logger.Warn("Failed to upsert task embedding to vector store",
			"session_id", in.SessionID,
			"error", err)
		return RecordQueryResult{Stored: false, Error: err.Error()}, nil
	}
	return RecordQueryResult{Stored: true}, nil
}

// RecordQuery generates an embedding and upserts to Qdrant (TaskEmbeddings)
// This is a Temporal activity that wraps the core logic
func RecordQuery(ctx context.Context, in RecordQueryInput) (RecordQueryResult, error) {
	return recordQueryCore(ctx, in)
}
