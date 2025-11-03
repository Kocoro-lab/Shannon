package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/util"
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

	// Update token usage and cost (centralized pricing; prefer per-agent, then model, then default)
	costUSD := input.CostUSD
	if costUSD <= 0 {
		if len(input.AgentUsage) > 0 {
			var total float64
			for _, au := range input.AgentUsage {
				if au.Model == "" {
					if au.InputTokens > 0 || au.OutputTokens > 0 {
						total += pricing.CostForSplit("", au.InputTokens, au.OutputTokens)
					} else {
						total += pricing.CostForTokens("", au.Tokens)
					}
					a.logger.Warn("Pricing fallback used (missing model)", zap.Int("tokens", au.Tokens))
					continue
				}
				if _, ok := pricing.PricePerTokenForModel(au.Model); !ok {
					a.logger.Warn("Pricing model not found; using default", zap.String("model", au.Model), zap.Int("tokens", au.Tokens))
				}
				if au.InputTokens > 0 || au.OutputTokens > 0 {
					total += pricing.CostForSplit(au.Model, au.InputTokens, au.OutputTokens)
				} else {
					total += pricing.CostForTokens(au.Model, au.Tokens)
				}
			}
			costUSD = total
		} else if input.ModelUsed != "" {
			if _, ok := pricing.PricePerTokenForModel(input.ModelUsed); !ok {
				a.logger.Warn("Pricing model not found; using default", zap.String("model", input.ModelUsed), zap.Int("tokens", input.TokensUsed))
			}
			costUSD = pricing.CostForTokens(input.ModelUsed, input.TokensUsed)
		} else {
			costUSD = float64(input.TokensUsed) * pricing.DefaultPerToken()
		}
	}
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

	// Maintain conversational context for follow-up tasks
	sess.SetContextValue("last_updated_at", time.Now().UTC().Format(time.RFC3339))
	sess.SetContextValue("total_tokens_used", sess.TotalTokensUsed)
	sess.SetContextValue("total_cost_usd", sess.TotalCostUSD)
	if input.TokensUsed > 0 {
		sess.SetContextValue("last_tokens_used", input.TokensUsed)
	}
	if input.AgentsUsed > 0 {
		sess.SetContextValue("last_agents_used", input.AgentsUsed)
	}
	if input.Result != "" {
		sess.SetContextValue("last_response", util.TruncateString(input.Result, 500, false))
	}

	// Update session metadata
	if sess.Metadata == nil {
		sess.Metadata = make(map[string]interface{})
	}
	sess.Metadata["last_agents_used"] = input.AgentsUsed
	sess.Metadata["last_workflow_result"] = util.TruncateString(input.Result, 200, false)

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
// Truncation unified in util.TruncateString
