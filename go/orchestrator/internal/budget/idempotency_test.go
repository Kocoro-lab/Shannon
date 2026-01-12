package budget

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestRecordUsage_Idempotency(t *testing.T) {
	// Create a mock database
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}
	defer db.Close()

	logger := zap.NewNop()
	bm := NewBudgetManager(db, logger)

	// Setup the first usage record with an idempotency key
	usage1 := &BudgetTokenUsage{
		UserID:         "user-123",
		SessionID:      "session-456",
		TaskID:         "task-789",
		AgentID:        "agent-001",
		Model:          "gpt-5-nano-2025-08-07",
		Provider:       "openai",
		InputTokens:    100,
		OutputTokens:   50,
		IdempotencyKey: "workflow-123-activity-456-1",
	}

	// Initialize budget for the session manually
	bm.sessionBudgets["session-456"] = &TokenBudget{
		TaskBudget:        10000,
		SessionBudget:     50000,
		TaskTokensUsed:    0,
		SessionTokensUsed: 0,
	}
	// User-level daily/monthly budgets removed; no user budget initialization required

	// Expect user lookup/creation first
	userID := uuid.New()
	mock.ExpectQuery("SELECT id FROM users WHERE external_id").
		WithArgs("user-123").
		WillReturnError(sql.ErrNoRows) // User doesn't exist

	mock.ExpectQuery("INSERT INTO users").
		WithArgs(sqlmock.AnyArg(), "user-123").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))

	// Expect task lookup
	taskID := uuid.New()
	mock.ExpectQuery("SELECT id FROM task_executions WHERE workflow_id").
		WithArgs("task-789").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(taskID))

	// Expect the first insert to succeed
	mock.ExpectExec("INSERT INTO token_usage").WithArgs(
		userID,                  // user_id (UUID)
		taskID,                  // task_id (UUID)
		"agent-001",             // agent_id
		"openai",                // provider
		"gpt-5-nano-2025-08-07", // model
		100,                     // prompt_tokens (InputTokens)
		50,                      // completion_tokens (OutputTokens)
		150,                     // total_tokens
		sqlmock.AnyArg(),        // cost_usd
	).WillReturnResult(sqlmock.NewResult(1, 1))

	// First call should succeed and record usage
	err = bm.RecordUsage(context.Background(), usage1)
	if err != nil {
		t.Fatalf("First RecordUsage failed: %v", err)
	}

	// Check that tokens were recorded
	sessionBudget := bm.sessionBudgets["session-456"]
	if sessionBudget.TaskTokensUsed != 150 {
		t.Errorf("Expected TaskTokensUsed to be 150, got %d", sessionBudget.TaskTokensUsed)
	}

	// Create a duplicate usage record with the same idempotency key
	usage2 := &BudgetTokenUsage{
		UserID:         "user-123",
		SessionID:      "session-456",
		TaskID:         "task-789",
		AgentID:        "agent-001",
		Model:          "gpt-5-nano",
		Provider:       "openai",
		InputTokens:    100,
		OutputTokens:   50,
		IdempotencyKey: "workflow-123-activity-456-1", // Same idempotency key
	}

	// Second call with same idempotency key should be skipped (no database call expected)
	err = bm.RecordUsage(context.Background(), usage2)
	if err != nil {
		t.Fatalf("Second RecordUsage failed: %v", err)
	}

	// Verify tokens were NOT double-counted
	if sessionBudget.TaskTokensUsed != 150 {
		t.Errorf("Expected TaskTokensUsed to remain 150 (not double-counted), got %d", sessionBudget.TaskTokensUsed)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("Unfulfilled expectations: %v", err)
	}
}

func TestRecordUsage_DifferentIdempotencyKeys(t *testing.T) {
	// Create a mock database
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock: %v", err)
	}
	defer db.Close()

	logger := zap.NewNop()
	bm := NewBudgetManager(db, logger)

	// Initialize budget for the session manually
	bm.sessionBudgets["session-456"] = &TokenBudget{
		TaskBudget:        10000,
		SessionBudget:     50000,
		TaskTokensUsed:    0,
		SessionTokensUsed: 0,
	}
	// User-level daily/monthly budgets removed; no user budget initialization required

	// First usage record
	usage1 := &BudgetTokenUsage{
		UserID:         "user-123",
		SessionID:      "session-456",
		TaskID:         "task-789",
		InputTokens:    100,
		OutputTokens:   50,
		IdempotencyKey: "workflow-123-activity-456-1",
	}

	// Second usage record with different idempotency key
	usage2 := &BudgetTokenUsage{
		UserID:         "user-123",
		SessionID:      "session-456",
		TaskID:         "task-789",
		InputTokens:    200,
		OutputTokens:   100,
		IdempotencyKey: "workflow-123-activity-789-1", // Different key
	}

	// For first usage - expect user and task lookups
	userID := uuid.New()
	taskID := uuid.New()

	// First usage - user lookup
	mock.ExpectQuery("SELECT id FROM users WHERE external_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))

	// First usage - task lookup
	mock.ExpectQuery("SELECT id FROM task_executions WHERE workflow_id").
		WithArgs("task-789").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(taskID))

	// First insert
	mock.ExpectExec("INSERT INTO token_usage").WillReturnResult(sqlmock.NewResult(1, 1))

	// Second usage - same user and task lookups
	mock.ExpectQuery("SELECT id FROM users WHERE external_id").
		WithArgs("user-123").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))

	mock.ExpectQuery("SELECT id FROM task_executions WHERE workflow_id").
		WithArgs("task-789").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(taskID))

	// Second insert
	mock.ExpectExec("INSERT INTO token_usage").WillReturnResult(sqlmock.NewResult(2, 1))

	// Both calls should succeed and record usage
	err = bm.RecordUsage(context.Background(), usage1)
	if err != nil {
		t.Fatalf("First RecordUsage failed: %v", err)
	}

	err = bm.RecordUsage(context.Background(), usage2)
	if err != nil {
		t.Fatalf("Second RecordUsage failed: %v", err)
	}

	// Verify both usages were recorded (150 + 300 = 450)
	sessionBudget := bm.sessionBudgets["session-456"]
	if sessionBudget.TaskTokensUsed != 450 {
		t.Errorf("Expected TaskTokensUsed to be 450, got %d", sessionBudget.TaskTokensUsed)
	}
}

func TestIdempotencyKeyTTLCleanup(t *testing.T) {
	logger := zap.NewNop()

	// Create budget manager with very short TTL for testing
	bm := NewBudgetManager(nil, logger)
	bm.idempotencyTTL = 100 * time.Millisecond // Very short TTL for testing

	// Manually add some expired and non-expired keys
	now := time.Now()
	bm.idempotencyMu.Lock()
	bm.processedUsage["expired-key-1"] = now.Add(-200 * time.Millisecond) // Expired
	bm.processedUsage["expired-key-2"] = now.Add(-300 * time.Millisecond) // Expired
	bm.processedUsage["fresh-key-1"] = now.Add(-50 * time.Millisecond)    // Not expired
	bm.processedUsage["fresh-key-2"] = now                                 // Just added
	bm.idempotencyMu.Unlock()

	if bm.GetIdempotencyKeyCount() != 4 {
		t.Errorf("Expected 4 keys before cleanup, got %d", bm.GetIdempotencyKeyCount())
	}

	// Run cleanup
	bm.cleanupExpiredIdempotencyKeys()

	// Verify expired keys were removed
	count := bm.GetIdempotencyKeyCount()
	if count != 2 {
		t.Errorf("Expected 2 keys after cleanup, got %d", count)
	}

	// Verify correct keys remain
	bm.idempotencyMu.RLock()
	_, hasFresh1 := bm.processedUsage["fresh-key-1"]
	_, hasFresh2 := bm.processedUsage["fresh-key-2"]
	_, hasExpired1 := bm.processedUsage["expired-key-1"]
	_, hasExpired2 := bm.processedUsage["expired-key-2"]
	bm.idempotencyMu.RUnlock()

	if !hasFresh1 || !hasFresh2 {
		t.Error("Fresh keys should not be removed")
	}
	if hasExpired1 || hasExpired2 {
		t.Error("Expired keys should be removed")
	}
}

func TestIdempotencyKeyExpiration(t *testing.T) {
	// Create budget manager with very short TTL
	logger := zap.NewNop()
	bm := NewBudgetManager(nil, logger)
	bm.idempotencyTTL = 50 * time.Millisecond

	// Manually add a key that will expire
	bm.idempotencyMu.Lock()
	bm.processedUsage["will-expire"] = time.Now().Add(-100 * time.Millisecond) // Already expired
	bm.idempotencyMu.Unlock()

	// Create a usage record with the "expired" idempotency key
	usage := &BudgetTokenUsage{
		UserID:         "user-123",
		SessionID:      "session-456",
		TaskID:         "task-789",
		InputTokens:    100,
		OutputTokens:   50,
		IdempotencyKey: "will-expire",
	}

	// Initialize session budget
	bm.sessionBudgets["session-456"] = &TokenBudget{
		TaskBudget:        10000,
		SessionBudget:     50000,
		TaskTokensUsed:    0,
		SessionTokensUsed: 0,
	}

	// Record usage - since the key expired, it should be processed again
	err := bm.RecordUsage(context.Background(), usage)
	if err != nil {
		t.Fatalf("RecordUsage failed: %v", err)
	}

	// Verify tokens were recorded (key was expired, so it was re-processed)
	sessionBudget := bm.sessionBudgets["session-456"]
	if sessionBudget.TaskTokensUsed != 150 {
		t.Errorf("Expected TaskTokensUsed to be 150 (expired key re-processed), got %d", sessionBudget.TaskTokensUsed)
	}
}

func TestStopIdempotencyCleanup(t *testing.T) {
	logger := zap.NewNop()
	bm := NewBudgetManager(nil, logger)

	// Start the cleanup goroutine
	bm.startIdempotencyCleanup()

	// Stop should not panic, even when called multiple times
	bm.StopIdempotencyCleanup()
	bm.StopIdempotencyCleanup() // Second call should be safe
}
