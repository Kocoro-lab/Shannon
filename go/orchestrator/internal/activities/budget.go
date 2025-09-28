package activities

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/budget"
	cfg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
)

// BudgetActivities handles token budget operations
type BudgetActivities struct {
	budgetManager *budget.BudgetManager
	logger        *zap.Logger
}

// NewBudgetActivities creates a new budget activities handler
func NewBudgetActivities(db *sql.DB, logger *zap.Logger) *BudgetActivities {
	var features *cfg.Features
	if f, err := cfg.Load(); err == nil {
		features = f
	}
	bcfg := cfg.BudgetFromEnvOrDefaults(features)
	opts := budget.Options{
		BackpressureThreshold:  bcfg.Backpressure.Threshold,
		MaxBackpressureDelayMs: bcfg.Backpressure.MaxDelayMs,
		// Circuit breaker and rate limit are configured per-user at runtime
	}
	return &BudgetActivities{
		budgetManager: budget.NewBudgetManagerWithOptions(db, logger, opts),
		logger:        logger,
	}
}

// NewBudgetActivitiesWithManager allows injecting a custom BudgetManager (useful for tests)
func NewBudgetActivitiesWithManager(mgr *budget.BudgetManager, logger *zap.Logger) *BudgetActivities {
	return &BudgetActivities{
		budgetManager: mgr,
		logger:        logger,
	}
}

// BudgetCheckInput represents input for budget checking
type BudgetCheckInput struct {
	UserID          string `json:"user_id"`
	SessionID       string `json:"session_id"`
	TaskID          string `json:"task_id"`
	EstimatedTokens int    `json:"estimated_tokens"`
}

// CheckTokenBudget checks if an operation can proceed within budget
func (b *BudgetActivities) CheckTokenBudget(ctx context.Context, input BudgetCheckInput) (*budget.BudgetCheckResult, error) {
	b.logger.Info("Checking token budget",
		zap.String("user_id", input.UserID),
		zap.String("session_id", input.SessionID),
		zap.Int("estimated_tokens", input.EstimatedTokens),
	)

	result, err := b.budgetManager.CheckBudget(
		ctx,
		input.UserID,
		input.SessionID,
		input.TaskID,
		input.EstimatedTokens,
	)

	if err != nil {
		b.logger.Error("Budget check failed", zap.Error(err))
		return nil, fmt.Errorf("budget check failed: %w", err)
	}

	if !result.CanProceed {
		b.logger.Warn("Budget constraint exceeded",
			zap.String("reason", result.Reason),
			zap.Int("remaining", result.RemainingTaskBudget),
		)
	}

	return result, nil
}

// CheckTokenBudgetWithBackpressure checks budget and applies backpressure if needed
func (b *BudgetActivities) CheckTokenBudgetWithBackpressure(ctx context.Context, input BudgetCheckInput) (*budget.BackpressureResult, error) {
	b.logger.Info("Checking token budget with backpressure",
		zap.String("user_id", input.UserID),
		zap.String("session_id", input.SessionID),
		zap.Int("estimated_tokens", input.EstimatedTokens),
	)

	result, err := b.budgetManager.CheckBudgetWithBackpressure(
		ctx,
		input.UserID,
		input.SessionID,
		input.TaskID,
		input.EstimatedTokens,
	)

	if err != nil {
		b.logger.Error("Budget check with backpressure failed", zap.Error(err))
		return nil, fmt.Errorf("budget check failed: %w", err)
	}

	if result.BackpressureActive {
		b.logger.Warn("Backpressure activated",
			zap.String("pressure_level", result.BudgetPressure),
			zap.Int("delay_ms", result.BackpressureDelay),
		)

		// Don't apply delay here - let workflow handle it with workflow.Sleep()
		// This prevents blocking Temporal workers
	}

	if !result.CanProceed {
		b.logger.Warn("Budget constraint exceeded with backpressure",
			zap.String("reason", result.Reason),
			zap.Int("remaining", result.RemainingTaskBudget),
		)
	}

	return result, nil
}

// CheckTokenBudgetWithCircuitBreaker checks budget with circuit breaker behavior
func (b *BudgetActivities) CheckTokenBudgetWithCircuitBreaker(ctx context.Context, input BudgetCheckInput) (*budget.BackpressureResult, error) {
	b.logger.Info("Checking token budget with circuit breaker",
		zap.String("user_id", input.UserID),
		zap.String("session_id", input.SessionID),
		zap.Int("estimated_tokens", input.EstimatedTokens),
	)

	result, err := b.budgetManager.CheckBudgetWithCircuitBreaker(
		ctx,
		input.UserID,
		input.SessionID,
		input.TaskID,
		input.EstimatedTokens,
	)
	if err != nil {
		b.logger.Error("Budget check with circuit breaker failed", zap.Error(err))
		return nil, fmt.Errorf("budget check failed: %w", err)
	}

	// If circuit is open, return immediately with reason
	if result.CircuitBreakerOpen || !result.CanProceed {
		b.logger.Warn("Request blocked by circuit breaker or budget",
			zap.Bool("breaker_open", result.CircuitBreakerOpen),
			zap.String("reason", result.Reason),
		)
		return result, nil
	}

	// Log backpressure but don't apply delay - let workflow handle it
	if result.BackpressureActive && result.BackpressureDelay > 0 {
		b.logger.Warn("Backpressure activated (circuit check)",
			zap.String("pressure_level", result.BudgetPressure),
			zap.Int("delay_ms", result.BackpressureDelay),
		)
		// Don't apply delay here - let workflow handle it with workflow.Sleep()
		// This prevents blocking Temporal workers
	}

	return result, nil
}

// TokenUsageInput represents token usage to record
type TokenUsageInput struct {
	UserID       string                 `json:"user_id"`
	SessionID    string                 `json:"session_id"`
	TaskID       string                 `json:"task_id"`
	AgentID      string                 `json:"agent_id"`
	Model        string                 `json:"model"`
	Provider     string                 `json:"provider"`
	InputTokens  int                    `json:"input_tokens"`
	OutputTokens int                    `json:"output_tokens"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// RecordTokenUsage records actual token usage
func (b *BudgetActivities) RecordTokenUsage(ctx context.Context, input TokenUsageInput) error {
	// Get activity info for idempotency key
	info := activity.GetInfo(ctx)

	b.logger.Info("Recording token usage",
		zap.String("user_id", input.UserID),
		zap.String("agent_id", input.AgentID),
		zap.Int("total_tokens", input.InputTokens+input.OutputTokens),
		zap.String("activity_id", info.ActivityID),
		zap.Int32("attempt", info.Attempt),
	)

	// Generate idempotency key using workflow ID, activity ID, and attempt number
	// This ensures retries of the same activity won't double-count tokens
	idempotencyKey := fmt.Sprintf("%s-%s-%d", info.WorkflowExecution.ID, info.ActivityID, info.Attempt)

	usage := &budget.BudgetTokenUsage{
		UserID:         input.UserID,
		SessionID:      input.SessionID,
		TaskID:         input.TaskID,
		AgentID:        input.AgentID,
		Model:          input.Model,
		Provider:       input.Provider,
		InputTokens:    input.InputTokens,
		OutputTokens:   input.OutputTokens,
		Metadata:       input.Metadata,
		IdempotencyKey: idempotencyKey,
	}

	err := b.budgetManager.RecordUsage(ctx, usage)
	if err != nil {
		b.logger.Error("Failed to record token usage", zap.Error(err))
		return fmt.Errorf("failed to record usage: %w", err)
	}

	return nil
}

// BudgetedAgentInput combines agent input with budget constraints
type BudgetedAgentInput struct {
	AgentInput AgentExecutionInput `json:"agent_input"`
	MaxTokens  int                 `json:"max_tokens"`
	UserID     string              `json:"user_id"`
	TaskID     string              `json:"task_id"`
	ModelTier  string              `json:"model_tier"` // small/medium/large
}

// detectProviderFromModel determines the provider based on the model name
func detectProviderFromModel(model string) string {
	modelLower := strings.ToLower(model)

	// OpenAI models
	if strings.Contains(modelLower, "gpt-4") || strings.Contains(modelLower, "gpt-3") ||
		strings.Contains(modelLower, "davinci") || strings.Contains(modelLower, "turbo") ||
		strings.Contains(modelLower, "o1") {
		return "openai"
	}

	// Anthropic models
	if strings.Contains(modelLower, "claude") || strings.Contains(modelLower, "opus") ||
		strings.Contains(modelLower, "sonnet") || strings.Contains(modelLower, "haiku") {
		return "anthropic"
	}

	// Google models
	if strings.Contains(modelLower, "gemini") || strings.Contains(modelLower, "palm") ||
		strings.Contains(modelLower, "bard") {
		return "google"
	}

	// Llama/Meta models
	if strings.Contains(modelLower, "llama") || strings.Contains(modelLower, "codellama") {
		return "meta"
	}

	// Mistral models
	if strings.Contains(modelLower, "mistral") || strings.Contains(modelLower, "mixtral") {
		return "mistral"
	}

	// Cohere models
	if strings.Contains(modelLower, "command") || strings.Contains(modelLower, "cohere") {
		return "cohere"
	}

	// Default to unknown
	return "unknown"
}

// ExecuteAgentWithBudget executes an agent with token budget constraints
func (b *BudgetActivities) ExecuteAgentWithBudget(ctx context.Context, input BudgetedAgentInput) (*AgentExecutionResult, error) {
	b.logger.Info("Executing agent with budget constraints",
		zap.String("agent_id", input.AgentInput.AgentID),
		zap.Int("max_tokens", input.MaxTokens),
		zap.String("model_tier", input.ModelTier),
	)

	// Check budget before execution
	budgetCheck, err := b.budgetManager.CheckBudget(
		ctx,
		input.UserID,
		input.AgentInput.SessionID,
		input.TaskID,
		input.MaxTokens,
	)

	if err != nil {
		return nil, fmt.Errorf("budget check failed: %w", err)
	}

	if !budgetCheck.CanProceed {
		return &AgentExecutionResult{
			AgentID: input.AgentInput.AgentID,
			Success: false,
			Error:   fmt.Sprintf("Budget exceeded: %s", budgetCheck.Reason),
		}, nil
	}

	// Select model based on tier and budget
	model, provider := selectModelForTier(input.ModelTier, input.MaxTokens)

	// Add budget constraints to context
	input.AgentInput.Context["max_tokens"] = input.MaxTokens
	input.AgentInput.Context["model"] = model
	input.AgentInput.Context["provider"] = provider
	input.AgentInput.Context["model_tier"] = input.ModelTier

	// Execute the actual agent using shared helper (not calling activity directly)
	activity.GetLogger(ctx).Info("Executing agent with budget",
		"agent_id", input.AgentInput.AgentID,
		"max_tokens", input.MaxTokens,
	)
	logger := zap.L()
	result, err := executeAgentCore(ctx, input.AgentInput, logger)
	if err != nil {
		return nil, fmt.Errorf("agent execution failed: %w", err)
	}

	// Don't override model - let it report what was actually used
	// TODO: Pass specific model through to Rust/Python to enforce budget constraints

	// Ensure tokens don't exceed budget
	if result.TokensUsed > input.MaxTokens {
		b.logger.Warn("Agent used more tokens than budgeted",
			zap.Int("used", result.TokensUsed),
			zap.Int("max", input.MaxTokens),
		)
		result.TokensUsed = input.MaxTokens // Cap at max budget
	}

	// Record the actual usage with the model that was actually used
	// Use the model from result if available, otherwise fall back to what we selected
	actualModel := result.ModelUsed
	if actualModel == "" {
		actualModel = model // Fallback to our selection if agent didn't report
	}
	// Determine actual provider from the model that was actually used
	actualProvider := detectProviderFromModel(actualModel)

	// Get activity info for idempotency key
	info := activity.GetInfo(ctx)
	idempotencyKey := fmt.Sprintf("%s-%s-%d", info.WorkflowExecution.ID, info.ActivityID, info.Attempt)

	// Estimate split for input/output tokens if not provided
	inputTokens := result.TokensUsed * 6 / 10
	outputTokens := result.TokensUsed * 4 / 10
	err = b.budgetManager.RecordUsage(ctx, &budget.BudgetTokenUsage{
		UserID:         input.UserID,
		SessionID:      input.AgentInput.SessionID,
		TaskID:         input.TaskID,
		AgentID:        input.AgentInput.AgentID,
		Model:          actualModel,
		Provider:       actualProvider,
		InputTokens:    inputTokens,
		OutputTokens:   outputTokens,
		IdempotencyKey: idempotencyKey,
	})

	if err != nil {
		b.logger.Error("Failed to record usage after agent execution", zap.Error(err))
	}

	return &result, nil
}

// UsageReportInput represents input for generating usage reports
type UsageReportInput struct {
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	TaskID    string    `json:"task_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// GenerateUsageReport generates a token usage report
func (b *BudgetActivities) GenerateUsageReport(ctx context.Context, input UsageReportInput) (*budget.UsageReport, error) {
	b.logger.Info("Generating usage report",
		zap.String("user_id", input.UserID),
		zap.String("session_id", input.SessionID),
	)

	// Set default time range if not provided
	if input.EndTime.IsZero() {
		input.EndTime = time.Now()
	}
	if input.StartTime.IsZero() {
		input.StartTime = input.EndTime.Add(-24 * time.Hour)
	}

	report, err := b.budgetManager.GetUsageReport(ctx, budget.UsageFilters{
		UserID:    input.UserID,
		SessionID: input.SessionID,
		TaskID:    input.TaskID,
		StartTime: input.StartTime,
		EndTime:   input.EndTime,
	})

	if err != nil {
		b.logger.Error("Failed to generate usage report", zap.Error(err))
		return nil, fmt.Errorf("failed to generate report: %w", err)
	}

	return report, nil
}

// UpdateBudgetInput represents input for updating budget policies
type UpdateBudgetInput struct {
	UserID           string   `json:"user_id"`
	SessionID        string   `json:"session_id"`
	TaskBudget       *int     `json:"task_budget,omitempty"`
	SessionBudget    *int     `json:"session_budget,omitempty"`
	DailyBudget      *int     `json:"daily_budget,omitempty"`
	MonthlyBudget    *int     `json:"monthly_budget,omitempty"`
	HardLimit        *bool    `json:"hard_limit,omitempty"`
	WarningThreshold *float64 `json:"warning_threshold,omitempty"`
	RequireApproval  *bool    `json:"require_approval,omitempty"`
}

// UpdateBudgetPolicy updates budget policies for a user/session
func (b *BudgetActivities) UpdateBudgetPolicy(ctx context.Context, input UpdateBudgetInput) error {
	b.logger.Info("Updating budget policy",
		zap.String("user_id", input.UserID),
		zap.String("session_id", input.SessionID),
	)

	// This would update the budget policies in the database
	// For now, we'll just log the update

	return nil
}

// selectModelForTier selects a model based on tier and token budget.
// NOTE: Tier is determined by task complexity or explicit caller request,
// NOT by quota-based allocation. The 50/40/10 distribution in configs
// is a target guideline, not an enforced ratio.
func selectModelForTier(tier string, maxTokens int) (string, string) {
	// Model selection based on tier and token constraints
	switch tier {
	case "small":
		if maxTokens > 8000 {
			return "gpt-3.5-turbo-16k", "openai"
		}
		return "gpt-3.5-turbo", "openai"

	case "medium":
		if maxTokens > 100000 {
			return "claude-3-sonnet", "anthropic"
		}
		return "gpt-4", "openai"

	case "large":
		if maxTokens > 100000 {
			return "claude-3-opus", "anthropic"
		}
		return "gpt-4-turbo", "openai"

	default:
		return "gpt-3.5-turbo", "openai"
	}
}

// SessionUpdateInput is defined in types.go
