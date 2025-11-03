package workflows

import (
    "strings"
    "time"

    "go.temporal.io/sdk/workflow"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/budget"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
    ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/ratecontrol"
)

// BudgetPreflight performs a token budget check with optional backpressure delay.
// It returns the full BackpressureResult so callers can decide how to proceed.
func BudgetPreflight(ctx workflow.Context, input TaskInput, estimatedTokens int) (*budget.BackpressureResult, error) {
	logger := workflow.GetLogger(ctx)

	// Use short activity timeout; this should be fast
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	})

	var res budget.BackpressureResult
	err := workflow.ExecuteActivity(actx, constants.CheckTokenBudgetWithBackpressureActivity, activities.BudgetCheckInput{
		UserID:          input.UserID,
		SessionID:       input.SessionID,
		TaskID:          workflow.GetInfo(ctx).WorkflowExecution.ID,
		EstimatedTokens: estimatedTokens,
	}).Get(ctx, &res)
	if err != nil {
		return nil, err
	}

	if res.BackpressureActive && res.BackpressureDelay > 0 {
		logger.Info("Applying budget backpressure delay",
			"delay_ms", res.BackpressureDelay,
			"pressure_level", res.BudgetPressure,
		)
		_ = workflow.Sleep(ctx, time.Duration(res.BackpressureDelay)*time.Millisecond)
	}

    rateControlVersion := workflow.GetVersion(ctx, "provider_rate_control_v1", workflow.DefaultVersion, 1)
    if rateControlVersion >= 1 {
        // Resolve runtime setting from features.yaml + env override
        var f *config.Features
        if feats, err := config.Load(); err == nil {
            f = feats
        }
        er := config.ResolveEnforcementRuntime(f)
        if !er.ProviderRateControlEnabled {
            logger.Info("Provider rate control disabled by config/env")
        } else {
            tier := deriveModelTier(input.Context)
            provider := resolveProviderFromContext(input.Context)

            // If provider is unknown, infer from tier's primary provider (models.yaml priority 1)
            if provider == "unknown" {
                provider = inferProviderFromTier(tier)
            }

            delay := ratecontrol.DelayForRequest(provider, tier, estimatedTokens)
            if delay > 0 {
                logger.Info("Applying provider rate control delay",
                    "provider", provider,
                    "tier", tier,
                    "delay_ms", delay.Milliseconds(),
                )
                // Record metric for rate limit delay
                ometrics.RateLimitDelay.WithLabelValues(provider, tier).Observe(delay.Seconds())
                _ = workflow.Sleep(ctx, delay)
            }
        }
    }
    return &res, nil
}

// WithAgentBudget returns a child context annotated with a per-agent budget.
// Strategies can pass this context to budgeted activities.
func WithAgentBudget(ctx workflow.Context, maxTokens int) workflow.Context {
	return workflow.WithValue(ctx, "agent_budget", maxTokens)
}

// EstimateTokens provides a coarse estimate of tokens needed for executing the plan.
// It mirrors the logic used by the previous budgeted workflow and keeps it central.
func EstimateTokens(decomp activities.DecompositionResult) int {
	return EstimateTokensWithConfig(decomp, nil)
}

// EstimateTokensWithConfig provides a coarse estimate with configurable thresholds
func EstimateTokensWithConfig(decomp activities.DecompositionResult, cfg *activities.WorkflowConfig) int {
	base := 2000
	mul := 1.0

	// Use configurable thresholds with defaults
	mediumThreshold := 0.5
	if cfg != nil && cfg.ComplexityMediumThreshold > 0 {
		mediumThreshold = cfg.ComplexityMediumThreshold
	}

	if decomp.ComplexityScore > mediumThreshold {
		mul = 2.5
	} else if decomp.ComplexityScore > 0.4 {
		mul = 1.5
	}
	n := len(decomp.Subtasks)
	if n == 0 {
		n = 1
	}
	return int(float64(base*n) * mul)
}

func resolveProviderFromContext(ctx map[string]interface{}) string {
	if ctx == nil {
		return "unknown"
	}
	if v, ok := ctx["provider"].(string); ok {
		if provider := strings.ToLower(strings.TrimSpace(v)); provider != "" {
			return provider
		}
	}
	if v, ok := ctx["llm_provider"].(string); ok {
		if provider := strings.ToLower(strings.TrimSpace(v)); provider != "" {
			return provider
		}
	}
	if v, ok := ctx["model"].(string); ok {
		if model := strings.TrimSpace(v); model != "" {
			return strings.ToLower(strings.TrimSpace(detectProviderFromModel(model)))
		}
	}
	return "unknown"
}

// inferProviderFromTier returns the most likely provider for a given tier.
// Reads priority-1 provider from config/models.yaml to avoid config drift.
// Falls back to hardcoded defaults if config unavailable.
func inferProviderFromTier(tier string) string {
	// Try to get from config first (data-driven)
	if provider := pricing.GetPriorityOneProvider(tier); provider != "" {
		return provider
	}

	// Fallback to hardcoded defaults if config not available
	switch tier {
	case "small":
		return "openai" // Default: gpt-5-nano-2025-08-07 is priority 1
	case "medium":
		return "openai" // Default: gpt-5-2025-08-07 is priority 1
	case "large":
		return "openai" // Default: gpt-4.1-2025-04-14 is priority 1
	default:
		return "unknown"
	}
}
