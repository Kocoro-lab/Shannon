package activities

import (
	"context"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
)

// FetchSemanticMemoryInput requests semantic search within a session
type FetchSemanticMemoryInput struct {
	Query     string  `json:"query"`      // The query to generate embedding from
	SessionID string  `json:"session_id"` // Session to filter by
	TenantID  string  `json:"tenant_id"`  // Tenant ID (for future multi-tenancy)
	Threshold float64 `json:"threshold"`  // Similarity threshold (0.0-1.0)
	TopK      int     `json:"top_k"`      // Maximum results to return
}

// FetchSemanticMemoryResult contains semantically similar items
type FetchSemanticMemoryResult struct {
	Items []map[string]interface{} `json:"items"`
}

// FetchSemanticMemory performs semantic search for session memories
// This activity generates the embedding from the query and calls the vectordb layer
// It now supports chunked content aggregation for better handling of long Q&A pairs
func FetchSemanticMemory(ctx context.Context, in FetchSemanticMemoryInput) (FetchSemanticMemoryResult, error) {
	// Delegate to the chunked version which handles both chunked and non-chunked content
	return FetchSemanticMemoryChunked(ctx, in)
}

// FetchHierarchicalMemoryInput combines recent and semantic retrieval
type FetchHierarchicalMemoryInput struct {
	Query        string  `json:"query"`          // For semantic search
	SessionID    string  `json:"session_id"`     // Session identifier
	TenantID     string  `json:"tenant_id"`      // Tenant ID
	RecentTopK   int     `json:"recent_top_k"`   // Recent items from session
	SemanticTopK int     `json:"semantic_top_k"` // Semantic items
	SummaryTopK  int     `json:"summary_top_k"`  // Summary items (default: 3)
	Threshold    float64 `json:"threshold"`      // Semantic similarity threshold
}

// FetchHierarchicalMemoryResult contains deduplicated memory items
type FetchHierarchicalMemoryResult struct {
	Items   []map[string]interface{} `json:"items"`
	Sources map[string]int           `json:"sources"` // Count by source type
}

// FetchHierarchicalMemory combines recent session memory with semantic search
// This provides both temporal relevance (recent) and semantic relevance
func FetchHierarchicalMemory(ctx context.Context, in FetchHierarchicalMemoryInput) (FetchHierarchicalMemoryResult, error) {
	result := FetchHierarchicalMemoryResult{
		Items:   make([]map[string]interface{}, 0),
		Sources: make(map[string]int),
	}

	// Fetch recent items from session
	if in.RecentTopK > 0 {
		recentResult, _ := FetchSessionMemory(ctx, FetchSessionMemoryInput{
			SessionID: in.SessionID,
			TenantID:  in.TenantID,
			TopK:      in.RecentTopK,
		})

		// Record metrics for recent fetch (from Qdrant via FetchSessionMemory)
		if len(recentResult.Items) == 0 {
			metrics.MemoryFetches.WithLabelValues("hierarchical-recent", "qdrant", "miss").Inc()
		} else {
			metrics.MemoryFetches.WithLabelValues("hierarchical-recent", "qdrant", "hit").Inc()
		}
		metrics.MemoryItemsRetrieved.WithLabelValues("hierarchical-recent", "qdrant").Observe(float64(len(recentResult.Items)))

		// Add recent items with source marker
		for _, item := range recentResult.Items {
			if item != nil {
				item["_source"] = "recent"
				result.Items = append(result.Items, item)
				result.Sources["recent"]++
			}
		}
	}

	// Build dedup keys from recent items (prefer point ID, fallback to composite key)
	seen := make(map[string]bool)

	// Fetch semantic items if query provided
	if in.SemanticTopK > 0 && in.Query != "" {
		semanticResult, _ := FetchSemanticMemory(ctx, FetchSemanticMemoryInput{
			Query:     in.Query,
			SessionID: in.SessionID,
			TenantID:  in.TenantID,
			Threshold: in.Threshold,
			TopK:      in.SemanticTopK,
		})
		for _, item := range result.Items {
			key := ""
			// Prefer point ID for deduplication
			if pid, ok := item["_point_id"].(string); ok && pid != "" {
				key = pid
			} else if id, ok := item["id"].(string); ok && id != "" {
				key = id
			} else {
				// Fallback to composite key (query + first 100 chars of answer)
				if q, ok := item["query"].(string); ok {
					key = q
				}
				if a, ok := item["answer"].(string); ok && len(a) > 100 {
					key += "_" + a[:100]
				} else if a, ok := item["answer"].(string); ok {
					key += "_" + a
				}
			}
			if key != "" {
				seen[key] = true
			}
		}

		// Add non-duplicate semantic items
		for _, item := range semanticResult.Items {
			if item != nil {
				key := ""
				// Build dedup key using same logic
				if pid, ok := item["_point_id"].(string); ok && pid != "" {
					key = pid
				} else if id, ok := item["id"].(string); ok && id != "" {
					key = id
				} else {
					if q, ok := item["query"].(string); ok {
						key = q
					}
					if a, ok := item["answer"].(string); ok && len(a) > 100 {
						key += "_" + a[:100]
					} else if a, ok := item["answer"].(string); ok {
						key += "_" + a
					}
				}

				isDuplicate := false
				if key != "" && seen[key] {
					isDuplicate = true
				}

				if !isDuplicate {
					item["_source"] = "semantic"
					result.Items = append(result.Items, item)
					result.Sources["semantic"]++
					if key != "" {
						seen[key] = true
					}
				} else {
					result.Sources["duplicate"]++
				}
			}
		}
	}

	// Fetch summaries if we have a query and vector db is configured
	// Summaries provide compressed historical context
	if in.Query != "" && in.SemanticTopK > 0 {
		vdb := vectordb.Get()
		embSvc := embeddings.Get()
		if vdb != nil && embSvc != nil && vdb.GetConfig().Summaries != "" {
			// Generate embedding for query
			vec, err := embSvc.GenerateEmbedding(ctx, in.Query, "")
			if err == nil {
				// Use configured summary limit or default to 3
				summaryLimit := in.SummaryTopK
				if summaryLimit <= 0 {
					summaryLimit = 3
				}
				summaries, _ := vdb.SearchSummaries(ctx, vec, in.SessionID, in.TenantID, summaryLimit, in.Threshold)

				// Add summaries with source marker and dedup
				for _, summary := range summaries {
					if summary.Payload != nil {
						// Build dedup key
						key := ""
						if id, ok := summary.Payload["summary_id"].(string); ok && id != "" {
							key = "summary_" + id
						} else if content, ok := summary.Payload["content"].(string); ok && len(content) > 50 {
							key = "summary_" + content[:50]
						}

						// Skip if already seen
						if key != "" && seen[key] {
							continue
						}

						summary.Payload["_source"] = "summary"
						result.Items = append(result.Items, summary.Payload)
						result.Sources["summary"]++

						if key != "" {
							seen[key] = true
						}
					}
				}
			}
		}
	}

	// Limit total items to prevent context explosion
	maxTotal := 10
	if len(result.Items) > maxTotal {
		result.Items = result.Items[:maxTotal]
	}

	return result, nil
}
