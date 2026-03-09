package budget

import (
	"context"
	"database/sql"
	"regexp"
	"sync"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

func TestCheckBudget_DefaultsAllowSmallEstimate(t *testing.T) {
	bm := NewBudgetManager(&sql.DB{}, zap.NewNop())
	ctx := context.Background()
	res, err := bm.CheckBudget(ctx, "u1", "s1", "t1", 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.CanProceed {
		t.Fatalf("expected CanProceed=true, got false: %+v", res)
	}
	if res.RemainingTaskBudget <= 0 || res.RemainingSessionBudget <= 0 {
		t.Fatalf("expected positive remaining budgets, got %+v", res)
	}
}

func TestEstimateCost_ModelPricing(t *testing.T) {
	bm := NewBudgetManager(&sql.DB{}, zap.NewNop())
	cost := bm.estimateCost(1000, "gpt-5-nano-2025-08-07")
	if cost <= 0 {
		t.Fatalf("expected positive cost for 1k tokens, got %f", cost)
	}
}

func TestRecordUsage_ExecInsertsTokenUsage(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	bm := NewBudgetManager(db, zap.NewNop())
	usage := &BudgetTokenUsage{
		UserID: "u1", SessionID: "s1", TaskID: "t1", AgentID: "a1",
		Model: "gpt-5-nano-2025-08-07", Provider: "openai", InputTokens: 10, OutputTokens: 20,
	}

	// Expect user lookup
	mock.ExpectQuery("SELECT id FROM users WHERE external_id").
		WithArgs("u1").
		WillReturnError(sql.ErrNoRows)

	// Expect user creation
	userID := "12345678-1234-5678-1234-567812345678"
	mock.ExpectQuery("INSERT INTO users").
		WithArgs(sqlmock.AnyArg(), "u1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))

	// Expect task lookup
	taskID := "87654321-4321-8765-4321-876543218765"
	mock.ExpectQuery("SELECT id FROM task_executions WHERE workflow_id").
		WithArgs("t1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(taskID))

	mock.ExpectExec(regexp.QuoteMeta(
		"INSERT INTO token_usage",
	)).WithArgs(
		sqlmock.AnyArg(), sqlmock.AnyArg(), usage.AgentID, usage.Provider, usage.Model,
		sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		sqlmock.AnyArg(), sqlmock.AnyArg(), // cache_read_tokens, cache_creation_tokens
	).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := bm.RecordUsage(context.Background(), usage); err != nil {
		t.Fatalf("RecordUsage error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetUsageReport_AggregatesRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	bm := NewBudgetManager(db, zap.NewNop())

	rows := sqlmock.NewRows([]string{"user_id", "task_id", "model", "provider", "input_total", "output_total", "total_tokens", "total_cost", "request_count"}).
		AddRow("u1", "t1", "gpt-5-nano-2025-08-07", "openai", 30, 60, 90, 0.1, 2)

	mock.ExpectQuery(`SELECT\s+tu\.user_id,.*FROM\s+token_usage`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(rows)

	from := time.Now().Add(-time.Hour)
	to := time.Now()
	rep, err := bm.GetUsageReport(context.Background(), UsageFilters{StartTime: from, EndTime: to})
	if err != nil {
		t.Fatalf("GetUsageReport error: %v", err)
	}
	if rep.TotalTokens != 90 || rep.TotalCostUSD <= 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestRecordUsage_CostOverrideSkipsPricing(t *testing.T) {
	// When CostOverride > 0, RecordUsage should use it instead of pricing calculation
	bm := NewBudgetManager(nil, zap.NewNop()) // nil db = skip persistence

	// Set up a session budget so RecordUsage updates in-memory state
	bm.SetSessionBudget("s1", &TokenBudget{
		TaskBudget:    100000,
		SessionBudget: 100000,
	})

	overrideCost := 0.0099 // Python-reported real cost
	usage := &BudgetTokenUsage{
		UserID:       "u1",
		SessionID:    "s1",
		TaskID:       "t1",
		AgentID:      "tool_web_fetch",
		Model:        "shannon_web_fetch",
		Provider:     "shannon-scraper",
		InputTokens:  0,
		OutputTokens: 27000, // synthetic tokens that would price high without override
		CostOverride: overrideCost,
	}

	if err := bm.RecordUsage(context.Background(), usage); err != nil {
		t.Fatalf("RecordUsage error: %v", err)
	}

	// CostUSD should be the override value, not the pricing calculation
	if usage.CostUSD != overrideCost {
		t.Fatalf("expected CostUSD=%f (from CostOverride), got %f", overrideCost, usage.CostUSD)
	}
}

func TestRecordUsage_NoCostOverrideFallsToPricing(t *testing.T) {
	// When CostOverride is 0, RecordUsage should use pricing calculation as before
	bm := NewBudgetManager(nil, zap.NewNop())

	bm.SetSessionBudget("s1", &TokenBudget{
		TaskBudget:    100000,
		SessionBudget: 100000,
	})

	usage := &BudgetTokenUsage{
		UserID:       "u1",
		SessionID:    "s1",
		TaskID:       "t1",
		AgentID:      "tool_web_search",
		Model:        "shannon_web_search",
		Provider:     "shannon-scraper",
		InputTokens:  0,
		OutputTokens: 7500,
		CostOverride: 0, // No override — should use pricing
	}

	if err := bm.RecordUsage(context.Background(), usage); err != nil {
		t.Fatalf("RecordUsage error: %v", err)
	}

	// CostUSD should be calculated via pricing, not zero
	if usage.CostUSD <= 0 {
		t.Fatalf("expected CostUSD > 0 from pricing calculation, got %f", usage.CostUSD)
	}
}

func TestCircuitBreaker_ConcurrentRecordAndRead(t *testing.T) {
	bm := NewBudgetManager(nil, zap.NewNop())

	userID := "race-test-user"
	threshold := 10
	bm.ConfigureCircuitBreaker(userID, CircuitBreakerConfig{
		FailureThreshold: threshold,
		ResetTimeout:     5 * time.Second,
		HalfOpenRequests: 3,
	})

	var wg sync.WaitGroup

	// Spawn goroutines that record failures concurrently
	for i := 0; i < threshold+5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bm.RecordFailure(userID)
		}()
	}

	// Spawn goroutines that read circuit state concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bm.GetCircuitState(userID)
		}()
	}

	wg.Wait()

	// After threshold+5 failures, circuit must be open
	state := bm.GetCircuitState(userID)
	if state != "open" {
		t.Fatalf("expected circuit state 'open' after %d failures, got %q", threshold+5, state)
	}
}

func TestCircuitBreaker_ConcurrentHalfOpenTransition(t *testing.T) {
	bm := NewBudgetManager(nil, zap.NewNop())

	// Set up session budget so CheckBudgetWithCircuitBreaker works
	bm.SetSessionBudget("s1", &TokenBudget{
		TaskBudget:    100000,
		SessionBudget: 100000,
	})

	userID := "halfopen-race-user"
	bm.ConfigureCircuitBreaker(userID, CircuitBreakerConfig{
		FailureThreshold: 2,
		ResetTimeout:     1 * time.Millisecond, // very short so it transitions to half-open quickly
		HalfOpenRequests: 3,
	})

	// Trip the circuit breaker
	bm.RecordFailure(userID)
	bm.RecordFailure(userID)

	// Wait for reset timeout so GetCircuitState will transition to half-open
	time.Sleep(5 * time.Millisecond)

	// Trigger the transition to half-open
	state := bm.GetCircuitState(userID)
	if state != "half-open" {
		t.Fatalf("expected circuit state 'half-open' after reset timeout, got %q", state)
	}

	var wg sync.WaitGroup

	// Concurrently record successes and check budget with circuit breaker.
	// This exercises CheckBudgetWithCircuitBreaker reading successCount
	// while RecordSuccess writes it — the exact race being fixed.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bm.RecordSuccess(userID)
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = bm.CheckBudgetWithCircuitBreaker(
				context.Background(), userID, "s1", "t1", 100,
			)
		}()
	}

	wg.Wait()

	// After enough successes in half-open (20 > HalfOpenRequests=3), circuit should be closed
	state = bm.GetCircuitState(userID)
	if state != "closed" {
		t.Fatalf("expected circuit state 'closed' after successes in half-open, got %q", state)
	}
}
