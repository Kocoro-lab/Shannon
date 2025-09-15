package activities

import (
    "context"
    "fmt"
    "time"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
    "go.uber.org/zap"
)

// UpdateSessionResult updates the session with final results from workflow execution
func (a *Activities) UpdateSessionResult(ctx context.Context, input SessionUpdateInput) (SessionUpdateResult, error) {
    a.logger.Info("Updating session with results",
        zap.String("session_id", input.SessionID),
        zap.Int("tokens_used", input.TokensUsed),
        zap.Int("agents_used", input.AgentsUsed),
    )

    // Validate input
    if input.SessionID == "" {
        return SessionUpdateResult{
            Success: false,
            Error:   "session ID is required",
        }, fmt.Errorf("session ID is required")
    }

    // Get session from manager
    sess, err := a.sessionManager.GetSession(ctx, input.SessionID)
    if err != nil {
        a.logger.Error("Failed to get session", zap.Error(err))
        return SessionUpdateResult{
            Success: false,
            Error:   fmt.Sprintf("Failed to get session: %v", err),
        }, err
    }

    // Update token usage and cost
    costUSD := float64(input.TokensUsed) * 0.000002 // Default GPT-3.5 pricing
    sess.UpdateTokenUsage(input.TokensUsed, costUSD)
    
    // Record metrics
    metrics.RecordSessionTokens(input.TokensUsed)
    
    // Add assistant message to history
    if input.Result != "" {
        message := session.Message{
            ID:         fmt.Sprintf("msg-%d", time.Now().UnixNano()),
            Role:       "assistant",
            Content:    input.Result,
            Timestamp:  time.Now(),
            TokensUsed: input.TokensUsed,
            CostUSD:    costUSD,
        }
        if err := a.sessionManager.AddMessage(ctx, input.SessionID, message); err != nil {
            a.logger.Warn("Failed to add message to history", zap.Error(err))
        }
    }
    
    // Update session metadata
    if sess.Metadata == nil {
        sess.Metadata = make(map[string]interface{})
    }
    sess.Metadata["last_agents_used"] = input.AgentsUsed
    sess.Metadata["last_workflow_result"] = truncateString(input.Result, 200)
    
    // Save session back to Redis
    if err := a.sessionManager.UpdateSession(ctx, sess); err != nil {
        a.logger.Error("Failed to update session", zap.Error(err))
        return SessionUpdateResult{
            Success: false,
            Error:   fmt.Sprintf("Failed to update session: %v", err),
        }, err
    }
    
    a.logger.Info("Session updated successfully with token tracking",
        zap.String("session_id", input.SessionID),
        zap.Int("tokens_added", input.TokensUsed),
        zap.Float64("cost_added", costUSD),
        zap.Int("total_tokens", sess.TotalTokensUsed),
        zap.Float64("total_cost", sess.TotalCostUSD),
    )

    return SessionUpdateResult{
        Success: true,
        Error:   "",
    }, nil
}

// Helper function to truncate strings for logging
func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen] + "..."
}