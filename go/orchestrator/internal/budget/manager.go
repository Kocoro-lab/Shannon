package budget

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pricing "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// TokenBudget represents budget constraints at different levels
type TokenBudget struct {
	// Task-level budgets
	TaskBudget     int `json:"task_budget"`
	TaskTokensUsed int `json:"task_tokens_used"`

	// Session-level budgets
	SessionBudget     int `json:"session_budget"`
	SessionTokensUsed int `json:"session_tokens_used"`

	// User-level budgets (daily/monthly)
	// Daily/Monthly budgets removed - task and session budgets are sufficient

	// Cost tracking
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	ActualCostUSD    float64 `json:"actual_cost_usd"`

	// Enforcement settings
	HardLimit        bool    `json:"hard_limit"`        // Stop execution if exceeded
	WarningThreshold float64 `json:"warning_threshold"` // Warn at X% of budget (0.8 = 80%)
	RequireApproval  bool    `json:"require_approval"`  // Require approval if exceeded
}

// BudgetTokenUsage tracks token consumption for budget management (renamed to avoid conflict with models.TokenUsage)
type BudgetTokenUsage struct {
	ID             string                 `json:"id"`
	UserID         string                 `json:"user_id"`
	SessionID      string                 `json:"session_id"`
	TaskID         string                 `json:"task_id"`
	AgentID        string                 `json:"agent_id"`
	Model          string                 `json:"model"`
	Provider       string                 `json:"provider"`
	InputTokens    int                    `json:"input_tokens"`
	OutputTokens   int                    `json:"output_tokens"`
	TotalTokens    int                    `json:"total_tokens"`
	CostUSD        float64                `json:"cost_usd"`
	Timestamp      time.Time              `json:"timestamp"`
	Metadata       map[string]interface{} `json:"metadata"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"` // Optional key for retry safety
}

// BudgetManager manages token budgets and usage tracking
//
// Mutex Lock Ordering (IMPORTANT - to prevent deadlocks):
// When acquiring multiple locks, always follow this order:
//  1. mu (main budget mutex) - protects sessionBudgets, userBudgets
//  2. rateLimitsmu - protects rateLimiters map
//  3. cbMu - protects circuitBreakers map
//  4. priorityMu - protects priorityTiers map
//  5. allocationMu - protects sessionAllocations map
//  6. idempotencyMu - protects processedUsage map
//
// Never acquire a lower-numbered lock while holding a higher-numbered lock.
// Each mutex protects independent data structures, minimizing lock contention.
type BudgetManager struct {
	db     *sql.DB
	logger *zap.Logger

	// In-memory cache for active sessions
	sessionBudgets map[string]*TokenBudget
	userBudgets    map[string]*TokenBudget
	mu             sync.RWMutex // Lock order: 1

	// Budget policies
	defaultTaskBudget    int
	defaultSessionBudget int
	// Daily/monthly budgets removed

	// Enhanced features - Backpressure control
	backpressureThreshold float64 // Activate backpressure at X% of budget (default 0.8)
	maxBackpressureDelay  int     // Maximum delay in milliseconds

	// Rate limiting
	rateLimiters map[string]*rate.Limiter
	rateLimitsmu sync.RWMutex // Lock order: 2

	// Circuit breaker per user
	circuitBreakers map[string]*CircuitBreaker
	cbMu            sync.RWMutex // Lock order: 3

	// Priority tiers for allocation
	priorityTiers map[string]PriorityTier
	priorityMu    sync.RWMutex // Lock order: 4

	// Session allocations for dynamic reallocation
	sessionAllocations map[string]int
	allocationMu       sync.RWMutex // Lock order: 5

	// Idempotency tracking for retry safety
	processedUsage map[string]bool // Maps idempotency key to processed status
	idempotencyMu  sync.RWMutex    // Lock order: 6 - Separate mutex for idempotency tracking
	idempotencyTTL time.Duration   // How long to keep idempotency records (default 1 hour)
}

// ErrTokenOverflow indicates a token counter would overflow the int range.
var ErrTokenOverflow = fmt.Errorf("token count would overflow")

// NewBudgetManager creates a new budget manager
func NewBudgetManager(db *sql.DB, logger *zap.Logger) *BudgetManager {
	bm := &BudgetManager{
		db:             db,
		logger:         logger,
		sessionBudgets: make(map[string]*TokenBudget),
		userBudgets:    make(map[string]*TokenBudget),

		// Default budgets (configurable via shannon.yaml session.token_budget_per_task)
		defaultTaskBudget:    10000, // 10K tokens per task (fallback if not configured)
		defaultSessionBudget: 50000, // 50K tokens per session (fallback)

		// Enhanced features initialization
		backpressureThreshold: 0.8,
		maxBackpressureDelay:  5000,
		rateLimiters:          make(map[string]*rate.Limiter),
		circuitBreakers:       make(map[string]*CircuitBreaker),
		priorityTiers:         make(map[string]PriorityTier),
		sessionAllocations:    make(map[string]int),

		// Idempotency tracking
		processedUsage: make(map[string]bool),
		idempotencyTTL: 1 * time.Hour, // Keep idempotency records for 1 hour
	}

	// Initialize database tables if needed
	if db != nil {
		bm.initializeTables()
	}

	return bm
}

// Options allow configuring budget manager behavior from config/env
type Options struct {
	BackpressureThreshold  float64
	MaxBackpressureDelayMs int
	// Default task/session budgets (tokens); 0 = use built-in defaults
	DefaultTaskBudget    int
	DefaultSessionBudget int
}

// NewBudgetManagerWithOptions creates a budget manager and applies options
func NewBudgetManagerWithOptions(db *sql.DB, logger *zap.Logger, opts Options) *BudgetManager {
	bm := NewBudgetManager(db, logger)
	if opts.BackpressureThreshold > 0 {
		bm.backpressureThreshold = opts.BackpressureThreshold
	}
	if opts.MaxBackpressureDelayMs > 0 {
		bm.maxBackpressureDelay = opts.MaxBackpressureDelayMs
	}
	if opts.DefaultTaskBudget > 0 {
		bm.defaultTaskBudget = opts.DefaultTaskBudget
	}
	if opts.DefaultSessionBudget > 0 {
		bm.defaultSessionBudget = opts.DefaultSessionBudget
	}
	return bm
}

// CheckBudget verifies if an operation can proceed within budget constraints
func (bm *BudgetManager) CheckBudget(ctx context.Context, userID, sessionID, taskID string, estimatedTokens int) (*BudgetCheckResult, error) {
	// Phase 1: Try read lock first for existing budgets (optimization for common case)
	bm.mu.RLock()
	userBudget, userExists := bm.userBudgets[userID]
	sessionBudget, sessionExists := bm.sessionBudgets[sessionID]
	bm.mu.RUnlock()

	// Phase 2: If budgets don't exist, acquire write lock to create them
	if !userExists || !sessionExists {
		bm.mu.Lock()
		// Double-check pattern: budgets might have been created by another goroutine
		if !userExists {
			if ub, exists := bm.userBudgets[userID]; exists {
				userBudget = ub
			} else {
				userBudget = &TokenBudget{
					HardLimit:        true,
					WarningThreshold: 0.8,
				}
				bm.userBudgets[userID] = userBudget
			}
		}
		if !sessionExists {
			if sb, exists := bm.sessionBudgets[sessionID]; exists {
				sessionBudget = sb
			} else {
				sessionBudget = &TokenBudget{
					TaskBudget:       bm.defaultTaskBudget,
					SessionBudget:    bm.defaultSessionBudget,
					HardLimit:        false,
					WarningThreshold: 0.8,
					RequireApproval:  false,
				}
				bm.sessionBudgets[sessionID] = sessionBudget
			}
		}
		bm.mu.Unlock()
	}

	result := &BudgetCheckResult{
		CanProceed:      true,
		RequireApproval: false,
		Warnings:        []string{},
	}

	// Acquire read lock to safely read budget values
	bm.mu.RLock()
	// Create local copies of the values we need to check
	taskTokensUsed := sessionBudget.TaskTokensUsed
	taskBudget := sessionBudget.TaskBudget
	sessionTokensUsed := sessionBudget.SessionTokensUsed
	sessionBudgetLimit := sessionBudget.SessionBudget
	// Daily budget tracking removed - using task/session budgets only
	hardLimit := sessionBudget.HardLimit
	requireApproval := sessionBudget.RequireApproval
	warningThreshold := sessionBudget.WarningThreshold
	bm.mu.RUnlock()

	// Check task-level budget
	if taskTokensUsed+estimatedTokens > taskBudget {
		if hardLimit {
			result.CanProceed = false
			result.Reason = fmt.Sprintf("Task budget exceeded: %d/%d tokens",
				taskTokensUsed+estimatedTokens, taskBudget)
		} else {
			result.RequireApproval = requireApproval
			result.Warnings = append(result.Warnings, "Task budget will be exceeded")
		}
	}

	// Check session-level budget
	if sessionTokensUsed+estimatedTokens > sessionBudgetLimit {
		if hardLimit {
			result.CanProceed = false
			result.Reason = fmt.Sprintf("Session budget exceeded: %d/%d tokens",
				sessionTokensUsed+estimatedTokens, sessionBudgetLimit)
		} else {
			result.Warnings = append(result.Warnings, "Session budget will be exceeded")
		}
	}

	// Daily budget check removed - task and session budgets provide sufficient control

	// Check warning threshold and emit streaming event
	taskUsagePercent := float64(taskTokensUsed) / float64(taskBudget)
	if taskUsagePercent > warningThreshold {
		warningMsg := fmt.Sprintf("Task budget at %.1f%% (threshold: %.1f%%)",
			taskUsagePercent*100, warningThreshold*100)
		result.Warnings = append(result.Warnings, warningMsg)

		// Emit BUDGET_THRESHOLD event for streaming observability
		bm.emitBudgetThresholdEvent(taskID, sessionID, warningMsg, map[string]interface{}{
			"usage_percent":     taskUsagePercent * 100,
			"threshold_percent": warningThreshold * 100,
			"tokens_used":       taskTokensUsed,
			"tokens_budget":     taskBudget,
			"level":             "warning",
			"budget_type":       "task",
		})
	}

	// Estimate cost using priority-1 small tier model from config
	defaultModel := pricing.GetPriorityOneModel("small")
	if defaultModel == "" {
		defaultModel = "gpt-5-nano-2025-08-07" // Fallback if config unavailable
	}
	result.EstimatedCost = bm.estimateCost(estimatedTokens, defaultModel)
	result.RemainingTaskBudget = taskBudget - taskTokensUsed
	result.RemainingSessionBudget = sessionBudgetLimit - sessionTokensUsed
	// RemainingDailyBudget removed

	return result, nil
}

// RecordUsage records actual token usage after an operation
func (bm *BudgetManager) RecordUsage(ctx context.Context, usage *BudgetTokenUsage) error {
	// Generate ID if not provided
	if usage.ID == "" {
		usage.ID = uuid.New().String()
	}

	// Check idempotency if key is provided
	if usage.IdempotencyKey != "" {
		bm.idempotencyMu.RLock()
		if bm.processedUsage[usage.IdempotencyKey] {
			bm.idempotencyMu.RUnlock()
			bm.logger.Debug("Skipping duplicate usage record",
				zap.String("idempotency_key", usage.IdempotencyKey),
				zap.String("usage_id", usage.ID))
			return nil // Already processed, skip to prevent double-counting
		}
		bm.idempotencyMu.RUnlock()
	}

	usage.Timestamp = time.Now()
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens

	// Calculate cost using centralized pricing (config/models.yaml)
	usage.CostUSD = pricing.CostForSplit(usage.Model, usage.InputTokens, usage.OutputTokens)

	// Update in-memory budgets with overflow checks
	const maxInt = int(^uint(0) >> 1)
	bm.mu.Lock()
	if sessionBudget, ok := bm.sessionBudgets[usage.SessionID]; ok {
		if sessionBudget.TaskTokensUsed > maxInt-usage.TotalTokens ||
			sessionBudget.SessionTokensUsed > maxInt-usage.TotalTokens {
			bm.mu.Unlock()
			return ErrTokenOverflow
		}
		sessionBudget.TaskTokensUsed += usage.TotalTokens
		sessionBudget.SessionTokensUsed += usage.TotalTokens
		sessionBudget.ActualCostUSD += usage.CostUSD
	}
	// Daily/monthly user budget tracking removed
	bm.mu.Unlock()

	// Store in database
	err := bm.storeUsage(ctx, usage)
	if err != nil {
		return err
	}

	// Mark as processed for idempotency (only after successful storage)
	if usage.IdempotencyKey != "" {
		bm.idempotencyMu.Lock()
		bm.processedUsage[usage.IdempotencyKey] = true
		bm.idempotencyMu.Unlock()

		// TODO: Add periodic cleanup of old idempotency keys based on TTL
		// This could be done in a background goroutine or during each call
	}

	return nil
}

// GetUsageReport generates a usage report for a user/session/task
func (bm *BudgetManager) GetUsageReport(ctx context.Context, filters UsageFilters) (*UsageReport, error) {
	report := &UsageReport{
		StartTime: filters.StartTime,
		EndTime:   filters.EndTime,
	}

	// Query database for usage records using migration schema
	// Join with task_executions to support filtering by workflow_id (common case) or task_id
	rows, err := bm.db.QueryContext(ctx, `
		SELECT tu.user_id, tu.task_id, tu.model, tu.provider,
		       SUM(tu.prompt_tokens) as input_total,
		       SUM(tu.completion_tokens) as output_total,
		       SUM(tu.total_tokens) as total_tokens,
		       SUM(tu.cost_usd) as total_cost,
		       COUNT(*) as request_count
		FROM token_usage tu
		LEFT JOIN task_executions te ON tu.task_id = te.id
		WHERE tu.created_at BETWEEN $1 AND $2
		  AND ($3 = '' OR tu.user_id::text = $3)
		  AND ($4 = '' OR tu.task_id::text = $4 OR te.workflow_id = $4)
		GROUP BY tu.user_id, tu.task_id, tu.model, tu.provider
		ORDER BY total_tokens DESC
	`, filters.StartTime, filters.EndTime, filters.UserID, filters.TaskID)

	if err != nil {
		return nil, fmt.Errorf("failed to query usage: %w", err)
	}
	defer rows.Close()

	var totalTokens int
	var totalCost float64

	for rows.Next() {
		var detail UsageDetail
		err := rows.Scan(
			&detail.UserID, &detail.TaskID,
			&detail.Model, &detail.Provider,
			&detail.InputTokens, &detail.OutputTokens, &detail.TotalTokens,
			&detail.CostUSD, &detail.RequestCount,
		)
		if err != nil {
			continue
		}

		report.Details = append(report.Details, detail)
		totalTokens += detail.TotalTokens
		totalCost += detail.CostUSD

		// Update model breakdown
		if report.ModelBreakdown == nil {
			report.ModelBreakdown = make(map[string]ModelUsage)
		}
		modelKey := fmt.Sprintf("%s:%s", detail.Provider, detail.Model)
		mb := report.ModelBreakdown[modelKey]
		mb.Tokens += detail.TotalTokens
		mb.Cost += detail.CostUSD
		mb.Requests += detail.RequestCount
		report.ModelBreakdown[modelKey] = mb
	}

	report.TotalTokens = totalTokens
	report.TotalCostUSD = totalCost

	return report, nil
}

// Helper methods

func (bm *BudgetManager) getUserBudget(userID string) *TokenBudget {
	if budget, ok := bm.userBudgets[userID]; ok {
		return budget
	}
	// Return a transient default without mutating shared maps
	return &TokenBudget{
		HardLimit:        true,
		WarningThreshold: 0.8,
	}
}

func (bm *BudgetManager) getSessionBudget(sessionID string) *TokenBudget {
	if budget, ok := bm.sessionBudgets[sessionID]; ok {
		return budget
	}
	// Return a transient default without mutating shared maps
	return &TokenBudget{
		TaskBudget:       bm.defaultTaskBudget,
		SessionBudget:    bm.defaultSessionBudget,
		HardLimit:        false,
		WarningThreshold: 0.8,
		RequireApproval:  false,
	}
}

func (bm *BudgetManager) estimateCost(tokens int, model string) float64 {
	// Use centralized pricing for estimation; if model unknown, defaults are applied
	return pricing.CostForTokens(model, tokens)
}

func (bm *BudgetManager) storeUsage(ctx context.Context, usage *BudgetTokenUsage) error {
	// Skip database operations if no database is configured (e.g., in tests)
	if bm.db == nil {
		return nil
	}

	// Handle user_id - convert to UUID or lookup/create user
	var userUUID *uuid.UUID
	if usage.UserID != "" {
		parsed, err := uuid.Parse(usage.UserID)
		if err == nil {
			// Valid UUID
			userUUID = &parsed
		} else {
			// Not a UUID, lookup or create user by external_id
			var uid uuid.UUID
			err := bm.db.QueryRowContext(ctx,
				"SELECT id FROM users WHERE external_id = $1",
				usage.UserID,
			).Scan(&uid)

			if err != nil {
				// User doesn't exist, create it
				uid = uuid.New()
				// Use QueryRowContext with RETURNING to properly get the id on conflict
				err = bm.db.QueryRowContext(ctx,
					"INSERT INTO users (id, external_id, created_at, updated_at) VALUES ($1, $2, NOW(), NOW()) ON CONFLICT (external_id) DO UPDATE SET updated_at = NOW() RETURNING id",
					uid, usage.UserID,
				).Scan(&uid)
				if err != nil {
					bm.logger.Error("Failed to create or retrieve user",
						zap.String("user_id", usage.UserID),
						zap.Error(err))
					// Return error instead of silently continuing
					return fmt.Errorf("failed to resolve user_id %s: %w", usage.UserID, err)
				}
				userUUID = &uid
			} else {
				userUUID = &uid
			}
		}
	}

	// Handle task_id - convert to UUID, verify existence, or resolve by workflow_id; otherwise store NULL
	var taskUUID *uuid.UUID
	if usage.TaskID != "" {
		if parsed, err := uuid.Parse(usage.TaskID); err == nil {
			// Parsed as UUID; verify it exists in task_executions
			exists := false
			if bm.db != nil {
				if err := bm.db.QueryRowContext(ctx,
					"SELECT EXISTS(SELECT 1 FROM task_executions WHERE id = $1)",
					parsed,
				).Scan(&exists); err != nil {
					bm.logger.Warn("Failed to verify task_id UUID existence",
						zap.String("task_id", usage.TaskID),
						zap.Error(err))
				}
			}
			if exists {
				taskUUID = &parsed
				bm.logger.Debug("Successfully verified task_id UUID",
					zap.String("task_id", parsed.String()))
			} else if bm.db != nil {
				// Might actually be a workflow_id that looks like a UUID; try resolving by workflow_id
				bm.logger.Debug("task_id UUID not found in task_executions; attempting workflow_id resolution",
					zap.String("task_id", usage.TaskID))
				var resolved uuid.UUID
				if err2 := bm.db.QueryRowContext(ctx,
					"SELECT id FROM task_executions WHERE workflow_id = $1 LIMIT 1",
					usage.TaskID,
				).Scan(&resolved); err2 == nil {
					taskUUID = &resolved
					bm.logger.Info("Resolved task_id by workflow_id after UUID check failed",
						zap.String("workflow_id", usage.TaskID),
						zap.String("resolved_task_id", resolved.String()))
				} else {
					bm.logger.Warn("task_id UUID not found and no matching workflow_id; storing NULL",
						zap.String("task_id", usage.TaskID),
						zap.String("user_id", usage.UserID),
						zap.String("model", usage.Model),
						zap.Error(err2))
				}
			} else {
				bm.logger.Warn("task_id UUID provided but DB unavailable; storing NULL",
					zap.String("task_id", usage.TaskID))
			}
		} else if bm.db != nil {
			// Not a UUID: attempt to resolve by workflow_id -> task_executions.id
			bm.logger.Debug("task_id is not a UUID; attempting workflow_id resolution",
				zap.String("task_id", usage.TaskID),
				zap.Error(err))
			var resolved uuid.UUID
			if err2 := bm.db.QueryRowContext(ctx,
				"SELECT id FROM task_executions WHERE workflow_id = $1 LIMIT 1",
				usage.TaskID,
			).Scan(&resolved); err2 == nil {
				taskUUID = &resolved
				bm.logger.Info("Successfully resolved task_id by workflow_id",
					zap.String("workflow_id", usage.TaskID),
					zap.String("resolved_task_id", resolved.String()))
			} else {
				bm.logger.Warn("Invalid task_id format and no matching workflow_id; storing NULL",
					zap.String("task_id", usage.TaskID),
					zap.String("user_id", usage.UserID),
					zap.String("model", usage.Model),
					zap.String("parse_error", err.Error()),
					zap.Error(err2))
			}
		} else {
			bm.logger.Warn("Invalid task_id format and DB unavailable; storing NULL",
				zap.String("task_id", usage.TaskID),
				zap.Error(err))
		}
	}

	// Store using schema that matches migration: prompt_tokens, completion_tokens, created_at
	_, err := bm.db.ExecContext(ctx, `
		INSERT INTO token_usage (
			user_id, task_id, agent_id, provider, model,
			prompt_tokens, completion_tokens, total_tokens, cost_usd
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, userUUID, taskUUID, usage.AgentID, usage.Provider, usage.Model,
		usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.CostUSD)

	if err != nil {
		bm.logger.Error("Failed to store token usage", zap.Error(err))
		return fmt.Errorf("failed to store usage: %w", err)
	}

	return nil
}

func (bm *BudgetManager) initializeTables() {
	// Note: Tables are now created via migrations, specifically:
	// - token_usage table in 001_initial_schema.sql
	// - budget_policies table can be added in a future migration
	// This method is kept for backward compatibility but does minimal work

	bm.logger.Info("Budget manager initialized - using migration-managed schema")
}

// Types for results and filters

type BudgetCheckResult struct {
	CanProceed             bool     `json:"can_proceed"`
	RequireApproval        bool     `json:"require_approval"`
	Reason                 string   `json:"reason"`
	Warnings               []string `json:"warnings"`
	EstimatedCost          float64  `json:"estimated_cost"`
	RemainingTaskBudget    int      `json:"remaining_task_budget"`
	RemainingSessionBudget int      `json:"remaining_session_budget"`
	// RemainingDailyBudget removed
}

type UsageFilters struct {
	UserID    string
	SessionID string
	TaskID    string
	StartTime time.Time
	EndTime   time.Time
}

type UsageReport struct {
	StartTime      time.Time             `json:"start_time"`
	EndTime        time.Time             `json:"end_time"`
	TotalTokens    int                   `json:"total_tokens"`
	TotalCostUSD   float64               `json:"total_cost_usd"`
	Details        []UsageDetail         `json:"details"`
	ModelBreakdown map[string]ModelUsage `json:"model_breakdown"`
}

type UsageDetail struct {
	UserID       string  `json:"user_id"`
	SessionID    string  `json:"session_id"`
	TaskID       string  `json:"task_id"`
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	RequestCount int     `json:"request_count"`
}

type ModelUsage struct {
	Tokens   int     `json:"tokens"`
	Cost     float64 `json:"cost"`
	Requests int     `json:"requests"`
}

// Enhanced Budget Manager Features - Backpressure and Circuit Breaker

// CircuitBreaker tracks failure patterns
type CircuitBreaker struct {
	failureCount    int32
	lastFailureTime time.Time
	state           string // "closed", "open", "half-open"
	config          CircuitBreakerConfig
	successCount    int32
	mu              sync.RWMutex
}

// CircuitBreakerConfig defines circuit breaker parameters
type CircuitBreakerConfig struct {
	FailureThreshold int
	ResetTimeout     time.Duration
	HalfOpenRequests int
}

// PriorityTier defines budget allocation priorities
type PriorityTier struct {
	Priority         int
	BudgetMultiplier float64
}

// BackpressureResult extends BudgetCheckResult with backpressure info
type BackpressureResult struct {
	*BudgetCheckResult
	BackpressureActive bool   `json:"backpressure_active"`
	BackpressureDelay  int    `json:"backpressure_delay_ms"`
	CircuitBreakerOpen bool   `json:"circuit_breaker_open"`
	BudgetPressure     string `json:"budget_pressure"` // low, medium, high, critical
}

// Enhanced Budget Manager Methods

// CheckBudgetWithBackpressure checks budget and applies backpressure if needed
func (bm *BudgetManager) CheckBudgetWithBackpressure(
	ctx context.Context, userID, sessionID, taskID string, estimatedTokens int,
) (*BackpressureResult, error) {

	// Regular budget check
	baseResult, err := bm.CheckBudget(ctx, userID, sessionID, taskID, estimatedTokens)
	if err != nil {
		return nil, err
	}

	result := &BackpressureResult{
		BudgetCheckResult: baseResult,
	}

	// Calculate usage percentage INCLUDING the new tokens (ensure budget exists)
	// Use session budget for backpressure calculation (daily budget removed)
	// Copy required fields under lock to avoid data races on concurrent updates
	var (
		sbExists      bool
		sbTokensUsed  int
		sbBudgetLimit int
	)
	bm.mu.RLock()
	if sb, ok := bm.sessionBudgets[sessionID]; ok && sb != nil {
		sbExists = true
		sbTokensUsed = sb.SessionTokensUsed
		sbBudgetLimit = sb.SessionBudget
	}
	bm.mu.RUnlock()

	var usagePercent float64
	if sbExists && sbBudgetLimit > 0 {
		projectedUsage := sbTokensUsed + estimatedTokens
		usagePercent = float64(projectedUsage) / float64(sbBudgetLimit)
	} else {
		usagePercent = 0 // No budget defined, no backpressure
	}

	// Apply backpressure if threshold exceeded
	if usagePercent >= bm.backpressureThreshold {
		result.BackpressureActive = true
		result.BackpressureDelay = bm.calculateBackpressureDelay(usagePercent)
	}

	// Determine budget pressure level
	result.BudgetPressure = bm.calculatePressureLevel(usagePercent)

	return result, nil
}

// calculateBackpressureDelay calculates delay based on usage
func (bm *BudgetManager) calculateBackpressureDelay(usagePercent float64) int {
	if usagePercent < bm.backpressureThreshold {
		return 0
	}

	// Map usage percentage to delay ranges
	if usagePercent >= 1.0 {
		return bm.maxBackpressureDelay // At or over limit
	} else if usagePercent >= 0.95 {
		return 1500 // 95-100%: 1000-2000ms range (returning midpoint)
	} else if usagePercent >= 0.9 {
		return 750 // 90-95%: 500-1000ms range (returning midpoint)
	} else if usagePercent >= 0.85 {
		return 300 // 85-90%: medium delay
	} else if usagePercent >= 0.8 {
		return 50 // 80-85%: 10-100ms range (returning midpoint)
	}

	return 0
}

// calculatePressureLevel determines the budget pressure level
func (bm *BudgetManager) calculatePressureLevel(usagePercent float64) string {
	switch {
	case usagePercent < 0.5:
		return "low"
	case usagePercent < 0.75:
		return "medium"
	case usagePercent < 0.9:
		return "high"
	default:
		return "critical"
	}
}

// SetUserBudget sets budget for a user
func (bm *BudgetManager) SetUserBudget(userID string, budget *TokenBudget) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.userBudgets[userID] = budget
}

// SetSessionBudget sets budget for a session
func (bm *BudgetManager) SetSessionBudget(sessionID string, budget *TokenBudget) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.sessionBudgets[sessionID] = budget
}

// SetRateLimit sets rate limit for a user
func (bm *BudgetManager) SetRateLimit(userID string, requestsPerInterval int, interval time.Duration) {
	bm.rateLimitsmu.Lock()
	defer bm.rateLimitsmu.Unlock()

	// Calculate rate per second
	ratePerSecond := float64(requestsPerInterval) / interval.Seconds()
	bm.rateLimiters[userID] = rate.NewLimiter(rate.Limit(ratePerSecond), requestsPerInterval)
}

// CheckRateLimit checks if request is allowed under rate limit
func (bm *BudgetManager) CheckRateLimit(userID string) bool {
	bm.rateLimitsmu.RLock()
	limiter, exists := bm.rateLimiters[userID]
	bm.rateLimitsmu.RUnlock()

	if !exists {
		return true // No rate limit configured
	}

	return limiter.Allow()
}

// GetBudgetPressure returns the current budget pressure level
// NOTE: Daily budget removed - pressure tracking now done per-session via CheckTokenBudgetWithBackpressure
func (bm *BudgetManager) GetBudgetPressure(userID string) string {
	// Daily budget tracking removed, always return low pressure
	// Use CheckTokenBudgetWithBackpressure for per-session pressure monitoring
	return "low"
}

// ResetUserUsage resets usage counters for a user
func (bm *BudgetManager) ResetUserUsage(userID string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if budget, ok := bm.userBudgets[userID]; ok {
		// Daily/monthly fields removed
		budget.TaskTokensUsed = 0
		budget.SessionTokensUsed = 0
	}
}

// Circuit Breaker Methods

// ConfigureCircuitBreaker sets up circuit breaker for a user
func (bm *BudgetManager) ConfigureCircuitBreaker(userID string, config CircuitBreakerConfig) {
	bm.cbMu.Lock()
	defer bm.cbMu.Unlock()

	bm.circuitBreakers[userID] = &CircuitBreaker{
		state:  "closed",
		config: config,
	}
}

// RecordFailure records a failure for circuit breaker
func (bm *BudgetManager) RecordFailure(userID string) {
	bm.cbMu.RLock()
	cb, exists := bm.circuitBreakers[userID]
	bm.cbMu.RUnlock()

	if !exists {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	atomic.AddInt32(&cb.failureCount, 1)
	cb.lastFailureTime = time.Now()

	if int(cb.failureCount) >= cb.config.FailureThreshold {
		cb.state = "open"
	}
}

// RecordSuccess records a success for circuit breaker
func (bm *BudgetManager) RecordSuccess(userID string) {
	bm.cbMu.RLock()
	cb, exists := bm.circuitBreakers[userID]
	bm.cbMu.RUnlock()

	if !exists {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == "half-open" {
		atomic.AddInt32(&cb.successCount, 1)
		if int(cb.successCount) >= cb.config.HalfOpenRequests {
			cb.state = "closed"
			atomic.StoreInt32(&cb.failureCount, 0)
			atomic.StoreInt32(&cb.successCount, 0)
		}
	}
}

// GetCircuitState returns current circuit breaker state
func (bm *BudgetManager) GetCircuitState(userID string) string {
	bm.cbMu.RLock()
	cb, exists := bm.circuitBreakers[userID]
	bm.cbMu.RUnlock()

	if !exists {
		return "closed"
	}
	// Lock for possible state transition without mixed unlock/lock
	cb.mu.Lock()
	if cb.state == "open" && time.Since(cb.lastFailureTime) > cb.config.ResetTimeout {
		cb.state = "half-open"
		atomic.StoreInt32(&cb.successCount, 0)
	}
	state := cb.state
	cb.mu.Unlock()
	return state
}

// CheckBudgetWithCircuitBreaker includes circuit breaker check
func (bm *BudgetManager) CheckBudgetWithCircuitBreaker(
	ctx context.Context, userID, sessionID, taskID string, estimatedTokens int,
) (*BackpressureResult, error) {

	// Check circuit breaker first
	state := bm.GetCircuitState(userID)
	if state == "open" {
		return &BackpressureResult{
			BudgetCheckResult: &BudgetCheckResult{
				CanProceed: false,
				Reason:     "Circuit breaker is open due to repeated failures",
			},
			CircuitBreakerOpen: true,
		}, nil
	}

	// Allow limited requests in half-open state
	if state == "half-open" {
		bm.cbMu.RLock()
		cb := bm.circuitBreakers[userID]
		bm.cbMu.RUnlock()

		if cb != nil && int(atomic.LoadInt32(&cb.successCount)) >= cb.config.HalfOpenRequests {
			return &BackpressureResult{
				BudgetCheckResult: &BudgetCheckResult{
					CanProceed: false,
					Reason:     "Circuit breaker in half-open state, test quota exceeded",
				},
				CircuitBreakerOpen: true,
			}, nil
		}
	}

	return bm.CheckBudgetWithBackpressure(ctx, userID, sessionID, taskID, estimatedTokens)
}

// Priority-based allocation methods

// SetPriorityTiers configures priority tiers
func (bm *BudgetManager) SetPriorityTiers(tiers map[string]PriorityTier) {
	bm.priorityMu.Lock()
	defer bm.priorityMu.Unlock()
	bm.priorityTiers = tiers
}

// AllocateBudgetByPriority allocates budget based on priority
func (bm *BudgetManager) AllocateBudgetByPriority(ctx context.Context, baseBudget int, priority string) int {
	bm.priorityMu.RLock()
	defer bm.priorityMu.RUnlock()

	if tier, ok := bm.priorityTiers[priority]; ok {
		return int(float64(baseBudget) * tier.BudgetMultiplier)
	}

	return baseBudget
}

// AllocateBudgetAcrossSessions distributes budget across sessions
func (bm *BudgetManager) AllocateBudgetAcrossSessions(ctx context.Context, sessions []string, totalBudget int) {
	bm.allocationMu.Lock()
	defer bm.allocationMu.Unlock()

	if len(sessions) == 0 {
		return
	}

	perSession := totalBudget / len(sessions)
	for _, session := range sessions {
		bm.sessionAllocations[session] = perSession
	}
}

// GetSessionAllocation returns allocated budget for a session
func (bm *BudgetManager) GetSessionAllocation(sessionID string) int {
	bm.allocationMu.RLock()
	defer bm.allocationMu.RUnlock()
	return bm.sessionAllocations[sessionID]
}

// ReallocateBudgetsByUsage redistributes budget based on usage patterns
func (bm *BudgetManager) ReallocateBudgetsByUsage(ctx context.Context, sessions []string) {
	// Gather usage under read lock
	type sessionUsage struct {
		id    string
		usage int
	}
	usages := make([]sessionUsage, 0, len(sessions))
	totalUsage := 0
	bm.mu.RLock()
	for _, session := range sessions {
		if budget, ok := bm.sessionBudgets[session]; ok {
			u := sessionUsage{id: session, usage: budget.SessionTokensUsed}
			usages = append(usages, u)
			totalUsage += u.usage
		}
	}
	bm.mu.RUnlock()

	if totalUsage == 0 {
		return
	}

	// Update allocations under allocation lock only
	bm.allocationMu.Lock()
	defer bm.allocationMu.Unlock()

	totalBudget := 0
	for _, session := range sessions {
		if allocation, ok := bm.sessionAllocations[session]; ok {
			totalBudget += allocation
		}
	}
	for _, u := range usages {
		proportion := float64(u.usage) / float64(totalUsage)
		smoothed := proportion*0.7 + 0.3/float64(len(sessions))
		bm.sessionAllocations[u.id] = int(float64(totalBudget) * smoothed)
	}
}

// NewEnhancedBudgetManager creates an enhanced budget manager (compatibility wrapper)
func NewEnhancedBudgetManager(db *sql.DB, logger *zap.Logger) *BudgetManager {
	return NewBudgetManager(db, logger)
}

// emitBudgetThresholdEvent emits a streaming event when budget thresholds are crossed
func (bm *BudgetManager) emitBudgetThresholdEvent(taskID, sessionID, message string, payload map[string]interface{}) {
	// This is best-effort, non-blocking - we don't want budget monitoring to fail the task
	// The streaming manager will publish to Redis for SSE/WebSocket delivery
	if taskID == "" {
		return // No workflow ID to emit event to
	}

	event := streaming.Event{
		WorkflowID: taskID,
		Type:       "BUDGET_THRESHOLD",
		AgentID:    "budget-manager",
		Message:    message,
		Payload:    payload,
		Timestamp:  time.Now(),
	}

	streaming.Get().Publish(taskID, event)
	bm.logger.Debug("Emitted BUDGET_THRESHOLD event",
		zap.String("task_id", taskID),
		zap.String("session_id", sessionID),
		zap.String("message", message),
	)
}
