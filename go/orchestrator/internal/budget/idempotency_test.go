package budget

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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
		Model:          "gpt-3.5-turbo",
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
	bm.userBudgets["user-123"] = &TokenBudget{
		UserDailyBudget:   100000,
		UserMonthlyBudget: 1000000,
		UserDailyUsed:     0,
		UserMonthlyUsed:   0,
	}

	// Expect the first insert to succeed
	mock.ExpectExec("INSERT INTO token_usage").WithArgs(
		"user-123",       // user_id
		"task-789",       // task_id
		"openai",         // provider
		"gpt-3.5-turbo",  // model
		100,              // prompt_tokens (InputTokens)
		50,               // completion_tokens (OutputTokens)
		150,              // total_tokens
		sqlmock.AnyArg(), // cost_usd
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
		Model:          "gpt-3.5-turbo",
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
	bm.userBudgets["user-123"] = &TokenBudget{
		UserDailyBudget:   100000,
		UserMonthlyBudget: 1000000,
		UserDailyUsed:     0,
		UserMonthlyUsed:   0,
	}

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

	// Expect both inserts to succeed
	mock.ExpectExec("INSERT INTO token_usage").WillReturnResult(sqlmock.NewResult(1, 1))
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
