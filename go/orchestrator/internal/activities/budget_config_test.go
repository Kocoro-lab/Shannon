package activities

import (
    "context"
    "os"
    "testing"
    "time"

    "go.uber.org/zap"
)

// Test env overrides are wired into BudgetActivities/NewBudgetManager
func TestBudgetEnvOverrides_WireIntoManager(t *testing.T) {
    // Set env overrides
    os.Setenv("BACKPRESSURE_THRESHOLD", "0.9")
    os.Setenv("MAX_BACKPRESSURE_DELAY_MS", "120")
    defer func() {
        os.Unsetenv("BACKPRESSURE_THRESHOLD")
        os.Unsetenv("MAX_BACKPRESSURE_DELAY_MS")
    }()

    acts := NewBudgetActivities(nil, zap.NewNop())

    // Case 1: 85% projected usage should NOT trigger backpressure with threshold 0.9
    in := BudgetCheckInput{UserID: "u", SessionID: "s", TaskID: "t1", EstimatedTokens: 85000}
    res, err := acts.CheckTokenBudgetWithBackpressure(context.Background(), in)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if res.BackpressureActive {
        t.Fatalf("expected no backpressure at 85%% with threshold 0.9, got %+v", res)
    }

    // Case 2: 100% projected usage should return max delay from env (120ms)
    in2 := BudgetCheckInput{UserID: "u2", SessionID: "s2", TaskID: "t2", EstimatedTokens: 100000}
    start := time.Now()
    res2, err := acts.CheckTokenBudgetWithBackpressure(context.Background(), in2)
    elapsed := time.Since(start)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !res2.BackpressureActive || res2.BackpressureDelay != 120 {
        t.Fatalf("expected backpressure with 120ms delay, got %+v", res2)
    }
    // Activity should NOT block - it returns immediately with delay value for workflow to handle
    // This ensures Temporal workers are not blocked
    if elapsed > 50*time.Millisecond {
        t.Fatalf("activity should return immediately without blocking, but took %v", elapsed)
    }
}
