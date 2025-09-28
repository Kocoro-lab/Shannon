package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"github.com/google/uuid"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
)

// CompressContextInput requests summary for long conversation history
type CompressContextInput struct {
    SessionID string `json:"session_id"`
    TenantID  string `json:"tenant_id,omitempty"`
    // History messages as pairs: {role, content}
    History      []map[string]string `json:"history"`
    TargetTokens int                 `json:"target_tokens"`
    // Parent workflow ID for unified event streaming
    ParentWorkflowID string `json:"parent_workflow_id,omitempty"`
}

// CompressContextResult returns the summary and persistence status
type CompressContextResult struct {
	Summary          string `json:"summary"`
	Stored           bool   `json:"stored"`
	Error            string `json:"error,omitempty"`
	OriginalTokens   int    `json:"original_tokens,omitempty"`   // Token count before compression
	CompressedTokens int    `json:"compressed_tokens,omitempty"` // Token count after compression
}

// CompressAndStoreContext summarizes history via llm-service and stores it in Qdrant
func CompressAndStoreContext(ctx context.Context, in CompressContextInput) (CompressContextResult, error) {
	// Use activity logger for Temporal correlation
	activity.GetLogger(ctx).Info("Compressing context", "session_id", in.SessionID, "history_length", len(in.History))
	logger := zap.L()
	if len(in.History) == 0 {
		return CompressContextResult{Summary: "", Stored: false}, nil
	}

	// Estimate original tokens from history
	originalTokens := 0
	for _, msg := range in.History {
		if content, ok := msg["content"]; ok {
			// Conservative estimate: ~4 chars per token
			originalTokens += len(content) / 4
		}
	}
	// Add overhead for formatting
	originalTokens += len(in.History) * 5

	// Call llm-service /context/compress
	base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
	url := base + "/context/compress"
	reqBody := map[string]interface{}{
		"messages":      in.History,
		"target_tokens": in.TargetTokens,
	}
	buf, _ := json.Marshal(reqBody)
	client := &http.Client{Timeout: 8 * time.Second, Transport: interceptors.NewWorkflowHTTPRoundTripper(nil)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return CompressContextResult{Summary: "", Stored: false, Error: err.Error()}, nil
	}
    req.Header.Set("Content-Type", "application/json")
    if in.ParentWorkflowID != "" {
        req.Header.Set("X-Parent-Workflow-ID", in.ParentWorkflowID)
    }
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("Context compress HTTP error", zap.Error(err))
		return CompressContextResult{Summary: "", Stored: false, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Warn("Context compress non-2xx", zap.Int("status", resp.StatusCode))
		return CompressContextResult{Summary: "", Stored: false, Error: fmt.Sprintf("status_%d", resp.StatusCode)}, nil
	}
	var out struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return CompressContextResult{Summary: "", Stored: false, Error: err.Error()}, nil
	}

	summary := out.Summary
	if summary == "" {
		return CompressContextResult{Summary: "", Stored: false, OriginalTokens: originalTokens}, nil
	}

	// Estimate compressed tokens from summary
	compressedTokens := len(summary)/4 + 10 // Conservative estimate + overhead

	// Record compression ratio metric if we achieved compression
	if originalTokens > 0 && compressedTokens > 0 {
		ratio := float64(originalTokens) / float64(compressedTokens)
		metrics.CompressionRatio.Observe(ratio)
		logger.Info("Context compression completed",
			zap.Int("original_tokens", originalTokens),
			zap.Int("compressed_tokens", compressedTokens),
			zap.Float64("compression_ratio", ratio),
		)
	}

	// Generate embedding for summary and upsert to Qdrant
	if svc := embeddings.Get(); svc != nil {
		if vdb := vectordb.Get(); vdb != nil {
			vec, err := svc.GenerateEmbedding(ctx, summary, "")
			if err != nil {
				logger.Warn("Embedding generation failed for summary", zap.Error(err))
				return CompressContextResult{
					Summary:          summary,
					Stored:           false,
					Error:            "embed_failed",
					OriginalTokens:   originalTokens,
					CompressedTokens: compressedTokens,
				}, nil
			}
			// Generate a deterministic summary ID for deduplication
			summaryID := uuid.New().String()
			payload := map[string]interface{}{
				"session_id": in.SessionID,
				"tenant_id":  in.TenantID,  // Add tenant_id for filtering
				"type":       "summary",
				"timestamp":  time.Now().Unix(),
				"content":    summary,       // Changed from "text" to "content" for consistency
				"summary_id": summaryID,     // Add summary_id for dedup
			}
			if _, err := vdb.UpsertSummaryEmbedding(ctx, vec, payload); err != nil {
				logger.Warn("Qdrant upsert failed for summary", zap.Error(err))
				return CompressContextResult{
					Summary:          summary,
					Stored:           false,
					Error:            "upsert_failed",
					OriginalTokens:   originalTokens,
					CompressedTokens: compressedTokens,
				}, nil
			}
			return CompressContextResult{
				Summary:          summary,
				Stored:           true,
				OriginalTokens:   originalTokens,
				CompressedTokens: compressedTokens,
			}, nil
		}
	}

	// If no vectordb/embeddings, return summary without storage
	return CompressContextResult{
		Summary:          summary,
		Stored:           false,
		OriginalTokens:   originalTokens,
		CompressedTokens: compressedTokens,
	}, nil
}
