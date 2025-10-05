package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"go.uber.org/zap"
)

// CheckCompressionNeededInput checks if compression should be triggered
type CheckCompressionNeededInput struct {
	SessionID       string `json:"session_id"`
	MessageCount    int    `json:"message_count"`
	EstimatedTokens int    `json:"estimated_tokens"`
	ModelTier       string `json:"model_tier"`
}

// CheckCompressionNeededResult indicates if compression should proceed
type CheckCompressionNeededResult struct {
	ShouldCompress bool   `json:"should_compress"`
	Reason         string `json:"reason"`
}

// CheckCompressionNeeded determines if context compression should be triggered
// This activity checks token thresholds and rate limits via session state
func (a *Activities) CheckCompressionNeeded(ctx context.Context, in CheckCompressionNeededInput) (CheckCompressionNeededResult, error) {
	logger := zap.L()

	// Get model window and threshold
	modelWindow := GetModelWindowSize(in.ModelTier)
	threshold := int(float64(modelWindow) * 0.75)

	// Check token threshold first
	if in.EstimatedTokens < threshold {
		metrics.CompressionEvents.WithLabelValues("skipped").Inc()
		return CheckCompressionNeededResult{
			ShouldCompress: false,
			Reason:         "Below token threshold",
		}, nil
	}

	// Check rate limiting via session state
	if a.sessionManager != nil && in.SessionID != "" {
		sessData, err := a.sessionManager.GetSession(ctx, in.SessionID)
		if err == nil && sessData != nil && sessData.Metadata != nil {
			// Check compression state for rate limiting
			if compState, ok := sessData.Metadata["compression_state"].(map[string]interface{}); ok {
				// Check message count since last compression
				if lastCount, ok := compState["last_message_count"].(int); ok {
					messagesSince := in.MessageCount - lastCount
					if messagesSince < 20 { // Minimum 20 new messages
						metrics.CompressionEvents.WithLabelValues("skipped").Inc()
						return CheckCompressionNeededResult{
							ShouldCompress: false,
							Reason:         "Insufficient new messages since last compression",
						}, nil
					}
				}

				// Check time since last compression
				if lastTime, ok := compState["last_compressed_at"].(int64); ok {
					timeSince := time.Since(time.Unix(lastTime, 0))
					if timeSince < 30*time.Minute { // Minimum 30 minutes
						metrics.CompressionEvents.WithLabelValues("skipped").Inc()
						return CheckCompressionNeededResult{
							ShouldCompress: false,
							Reason:         "Too soon since last compression",
						}, nil
					}
				}
			}
		}
	}

	logger.Info("Compression check passed",
		zap.String("session_id", in.SessionID),
		zap.Int("tokens", in.EstimatedTokens),
		zap.Int("threshold", threshold),
		zap.Int("messages", in.MessageCount),
	)

	metrics.CompressionEvents.WithLabelValues("triggered").Inc()
	return CheckCompressionNeededResult{
		ShouldCompress: true,
		Reason:         "Token threshold exceeded and rate limits passed",
	}, nil
}

// UpdateCompressionStateInput updates session after compression
type UpdateCompressionStateInput struct {
	SessionID    string `json:"session_id"`
	MessageCount int    `json:"message_count"`
}

// UpdateCompressionStateResult indicates update success
type UpdateCompressionStateResult struct {
	Updated bool   `json:"updated"`
	Error   string `json:"error,omitempty"`
}

// UpdateCompressionStateActivity updates the compression state in session after successful compression
func (a *Activities) UpdateCompressionStateActivity(ctx context.Context, in UpdateCompressionStateInput) (UpdateCompressionStateResult, error) {
	if a.sessionManager == nil || in.SessionID == "" {
		return UpdateCompressionStateResult{Updated: false}, nil
	}

	// Get current session
	sessData, err := a.sessionManager.GetSession(ctx, in.SessionID)
	if err != nil || sessData == nil {
		return UpdateCompressionStateResult{
			Updated: false,
			Error:   "Session not found",
		}, nil
	}

	// Get or create compression state
	totalCompressions := 0
	if sessData.Metadata == nil {
		sessData.Metadata = make(map[string]interface{})
	}

	if compState, ok := sessData.Metadata["compression_state"].(map[string]interface{}); ok {
		if total, ok := compState["total_compressions"].(int); ok {
			totalCompressions = total
		}
	}
	totalCompressions++

	// Update compression state
	sessData.Metadata["compression_state"] = map[string]interface{}{
		"last_compressed_at": time.Now().Unix(),
		"last_message_count": in.MessageCount,
		"total_compressions": totalCompressions,
	}

	// Persist the updated session
	if err := a.sessionManager.UpdateSession(ctx, sessData); err != nil {
		zap.L().Error("Failed to persist compression state",
			zap.String("session_id", in.SessionID),
			zap.Error(err),
		)
		return UpdateCompressionStateResult{
			Updated: false,
			Error:   fmt.Sprintf("Failed to persist: %v", err),
		}, nil
	}

	zap.L().Info("Updated and persisted compression state",
		zap.String("session_id", in.SessionID),
		zap.Int("message_count", in.MessageCount),
		zap.Int("total_compressions", totalCompressions),
	)

	return UpdateCompressionStateResult{Updated: true}, nil
}
