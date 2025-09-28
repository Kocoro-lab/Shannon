package activities

import (
	"context"
	"sort"
	"strings"
	"time"
	"math"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"go.uber.org/zap"
)

// FetchSemanticMemoryChunked performs semantic search with chunk aggregation support
// This is a replacement for FetchSemanticMemory that handles chunked content
func FetchSemanticMemoryChunked(ctx context.Context, in FetchSemanticMemoryInput) (FetchSemanticMemoryResult, error) {
	// Get services
	embSvc := embeddings.Get()
	vdb := vectordb.Get()

	// Graceful degradation if services unavailable
	if embSvc == nil || vdb == nil || in.SessionID == "" || in.Query == "" {
		return FetchSemanticMemoryResult{Items: nil}, nil
	}

	// Generate embedding from query
	vec, err := embSvc.GenerateEmbedding(ctx, in.Query, "")
	if err != nil {
		// Log error for debugging
		if logger := zap.L(); logger != nil {
			logger.Warn("Failed to generate embedding for query",
				zap.String("query", in.Query),
				zap.Error(err))
		}
		// Graceful degradation
		return FetchSemanticMemoryResult{Items: nil}, nil
	}
	// Check if embedding is valid
	if len(vec) == 0 {
		if logger := zap.L(); logger != nil {
			logger.Warn("Empty embedding generated for query",
				zap.String("query", in.Query))
		}
		return FetchSemanticMemoryResult{Items: nil}, nil
	}

	// Use configured threshold or default
	// Note: 0 means no threshold, negative means use default
	threshold := in.Threshold
	if threshold < 0 {
		threshold = 0.75
	}

	// Use provided TopK or default
	topK := in.TopK
	if topK <= 0 {
		topK = 5
	}

	// Fetch more results to account for chunking using configured multiplier
	poolMultiplier := 3 // Default multiplier
	if cfg := vdb.GetConfig(); cfg.MMRPoolMultiplier > 0 {
		poolMultiplier = cfg.MMRPoolMultiplier
	}
	searchLimit := topK * poolMultiplier

	// Perform semantic search
	items, err := vdb.GetSessionContextSemanticByEmbedding(ctx, vec, in.SessionID, in.TenantID, searchLimit, threshold)
	if err != nil {
		// Record metrics
		metrics.MemoryFetches.WithLabelValues("semantic", "qdrant", "miss").Inc()
		metrics.MemoryItemsRetrieved.WithLabelValues("semantic", "qdrant").Observe(0)
		// Log error for debugging
		if logger := zap.L(); logger != nil {
			logger.Warn("Failed to fetch semantic memory",
				zap.String("session_id", in.SessionID),
				zap.String("tenant_id", in.TenantID),
				zap.Error(err))
		}
		return FetchSemanticMemoryResult{Items: nil}, nil
	}



    // Optional MMR re-ranking (diversity) of candidate pool
    if cfg := vectordb.Get().GetConfig(); cfg.MMREnabled {
        allHaveVec := true
        for i := range items {
            if len(items[i].Vector) == 0 {
                allHaveVec = false
                break
            }
        }
        if allHaveVec && len(items) > 1 {
            items = mmrReorder(vec, items, cfg.MMRLambda)
        }
    }

	// Group results by qa_id for chunked content
	type QAGroup struct {
		QAID      string
		Chunks    []vectordb.ContextItem
		BestScore float64
		Query     string
		Payload   map[string]interface{}
	}

	qaGroups := make(map[string]*QAGroup)
	singleItems := make([]map[string]interface{}, 0)

	for _, item := range items {
		payload := item.Payload
		if payload == nil {
			continue
		}

		// Check if this is a chunked item
		isChunked := false
		if chunked, ok := payload["is_chunked"].(bool); ok {
			isChunked = chunked
		}

		if isChunked {
			// Extract QA ID
			qaID, _ := payload["qa_id"].(string)
			if qaID == "" {
				// Fallback to single item if no QA ID
				payload["_similarity_score"] = item.Score
				singleItems = append(singleItems, payload)
				continue
			}

			// Group chunks by QA ID
			if qaGroups[qaID] == nil {
				qaGroups[qaID] = &QAGroup{
					QAID:      qaID,
					Chunks:    []vectordb.ContextItem{},
					BestScore: item.Score,
					Payload:   payload,
				}

				// Extract query from first chunk
				if q, ok := payload["query"].(string); ok {
					qaGroups[qaID].Query = q
				}
			}

			// Add chunk to group
			qaGroups[qaID].Chunks = append(qaGroups[qaID].Chunks, item)

			// Track best score
			if item.Score > qaGroups[qaID].BestScore {
				qaGroups[qaID].BestScore = item.Score
			}
		} else {
			// Non-chunked item - add directly
			payload["_similarity_score"] = item.Score
			singleItems = append(singleItems, payload)
		}
	}

	// Process chunked groups
	aggregationStart := time.Now()
	for _, group := range qaGroups {
		// Sort chunks by chunk_index with defensive type checking
		sort.Slice(group.Chunks, func(i, j int) bool {
			idxI := 0
			idxJ := 0

			// Safely extract chunk_index from payload with multiple type checks
			if group.Chunks[i].Payload != nil {
				if val, ok := group.Chunks[i].Payload["chunk_index"]; ok && val != nil {
					switch v := val.(type) {
					case float64:
						idxI = int(v)
					case int:
						idxI = v
					case int64:
						idxI = int(v)
					}
				}
			}

			if group.Chunks[j].Payload != nil {
				if val, ok := group.Chunks[j].Payload["chunk_index"]; ok && val != nil {
					switch v := val.(type) {
					case float64:
						idxJ = int(v)
					case int:
						idxJ = v
					case int64:
						idxJ = int(v)
					}
				}
			}

			return idxI < idxJ
		})

		// Create aggregated item
		aggregated := make(map[string]interface{})

		// Copy base fields from first chunk's payload
		if len(group.Chunks) > 0 && group.Chunks[0].Payload != nil {
			for k, v := range group.Chunks[0].Payload {
				// Skip chunk-specific fields
				if k != "chunk_text" && k != "chunk_index" && k != "chunk_count" && k != "qa_id" && k != "is_chunked" {
					aggregated[k] = v
				}
			}
		}

		// Reconstruct full answer from ordered chunk texts
		// Handle overlap by only taking non-overlapping parts after first chunk
		var answerBuilder strings.Builder
		for i, chunk := range group.Chunks {
			if chunkText, ok := chunk.Payload["chunk_text"].(string); ok {
				if i == 0 {
					// First chunk - use entire text
					answerBuilder.WriteString(chunkText)
				} else {
					// Subsequent chunks - skip overlap (approximately 200 tokens = 800 chars)
					// This is a simplified approach; ideally we'd find exact overlap
					overlapChars := 800
					if len(chunkText) > overlapChars {
						answerBuilder.WriteString(chunkText[overlapChars:])
					} else {
						// If chunk is smaller than overlap, still append it
						answerBuilder.WriteString(chunkText)
					}
				}
			}
		}

		aggregated["query"] = group.Query
		aggregated["answer"] = answerBuilder.String()
		aggregated["_similarity_score"] = group.BestScore
		aggregated["_was_chunked"] = true
		aggregated["_chunk_count"] = len(group.Chunks)

		singleItems = append(singleItems, aggregated)
	}

	// Record chunk aggregation metrics if we processed any chunked groups
	if len(qaGroups) > 0 {
		metrics.RecordChunkAggregation(time.Since(aggregationStart).Seconds())
	}

	// Sort by score and limit to topK
	sort.Slice(singleItems, func(i, j int) bool {
		scoreI := 0.0
		scoreJ := 0.0
		if s, ok := singleItems[i]["_similarity_score"].(float64); ok {
			scoreI = s
		}
		if s, ok := singleItems[j]["_similarity_score"].(float64); ok {
			scoreJ = s
		}
		return scoreI > scoreJ
	})

	// Limit to requested topK
	if len(singleItems) > topK {
		singleItems = singleItems[:topK]
	}

	// Record metrics
	if len(singleItems) == 0 {
		metrics.MemoryFetches.WithLabelValues("semantic", "qdrant", "miss").Inc()
	} else {
		metrics.MemoryFetches.WithLabelValues("semantic", "qdrant", "hit").Inc()
	}
	metrics.MemoryItemsRetrieved.WithLabelValues("semantic", "qdrant").Observe(float64(len(singleItems)))

	return FetchSemanticMemoryResult{Items: singleItems}, nil
}

// cosineSim computes cosine similarity between two float32 vectors
func cosineSim(a, b []float32) float64 {
    var dot, na, nb float64
    n := len(a)
    if len(b) < n { n = len(b) }
    for i := 0; i < n; i++ {
        da := float64(a[i])
        db := float64(b[i])
        dot += da * db
        na += da * da
        nb += db * db
    }
    if na == 0 || nb == 0 { return 0 }
    return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// mmrReorder reorders candidates by MMR (greedy) with relevance-diversity trade-off lambda
func mmrReorder(query []float32, items []vectordb.ContextItem, lambda float64) []vectordb.ContextItem {
    if lambda < 0 { lambda = 0 }
    if lambda > 1 { lambda = 1 }
    n := len(items)
    if n <= 1 { return items }
    qd := make([]float64, n)
    for i := 0; i < n; i++ { qd[i] = cosineSim(query, items[i].Vector) }
    selected := make([]int, 0, n)
    remaining := make([]bool, n)
    for i := 0; i < n; i++ { remaining[i] = true }
    for len(selected) < n {
        bestIdx := -1
        bestScore := -1e9
        for i := 0; i < n; i++ {
            if !remaining[i] { continue }
            maxDiv := 0.0
            for _, s := range selected {
                sim := cosineSim(items[i].Vector, items[s].Vector)
                if sim > maxDiv { maxDiv = sim }
            }
            score := lambda*qd[i] - (1.0-lambda)*maxDiv
            if score > bestScore { bestScore = score; bestIdx = i }
        }
        if bestIdx == -1 { break }
        selected = append(selected, bestIdx)
        remaining[bestIdx] = false
    }
    out := make([]vectordb.ContextItem, 0, n)
    for _, idx := range selected { out = append(out, items[idx]) }
    return out
}
