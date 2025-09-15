package budget

import (
    "context"
    "database/sql"
    "fmt"
    "sync"
    "sync/atomic"
    "testing"
    "time"
    
    _ "github.com/mattn/go-sqlite3"
    "go.uber.org/zap"
)

// TestEnhancedBudgetManager tests the enhanced budget manager with backpressure
func TestEnhancedBudgetManager(t *testing.T) {
    // Create in-memory database for testing
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("Failed to create test database: %v", err)
    }
    defer db.Close()
    
    // Create test schema
    createTestSchema(t, db)
    
    logger := zap.NewNop()
    manager := NewBudgetManager(db, logger)
    
    t.Run("BackpressureActivation", func(t *testing.T) {
        ctx := context.Background()
        userID := "test-user-1"
        sessionID := "test-session-1"
        
        // Configure aggressive limits for testing
        manager.SetUserBudget(userID, &TokenBudget{
            UserDailyBudget: 1000,
            UserDailyUsed:   0,  // Initialize usage
            HardLimit:       true,
        })
        
        // First request at 70% - should not trigger backpressure
        result, err := manager.CheckBudgetWithBackpressure(ctx, userID, sessionID, "task-1", 700)
        if err != nil {
            t.Fatalf("CheckBudget failed: %v", err)
        }
        if !result.CanProceed {
            t.Error("Should allow request at 70% budget")
        }
        if result.BackpressureActive {
            t.Error("Backpressure should not be active at 70%")
        }
        
        // Record the usage to update the budget
        manager.RecordUsage(ctx, &BudgetTokenUsage{
            UserID:       userID,
            SessionID:    sessionID,
            TaskID:       "task-1",
            InputTokens:  700,
            OutputTokens: 0,
        })
        
        // Next request at 80% (700 used + 100 new = 800/1000) should trigger backpressure
        result, err = manager.CheckBudgetWithBackpressure(ctx, userID, sessionID, "task-2", 100)
        if err != nil {
            t.Fatalf("CheckBudget failed: %v", err)
        }
        
        // At exactly 80%, backpressure should be active
        if !result.BackpressureActive {
            // Get current usage for debugging
            manager.mu.RLock()
            userBudget := manager.getUserBudget(userID)
            t.Errorf("Backpressure should be active at 80%% budget. Current usage: %d/%d",
                userBudget.UserDailyUsed, userBudget.UserDailyBudget)
            manager.mu.RUnlock()
        }
        if result.BackpressureDelay <= 0 {
            t.Error("Should have backpressure delay")
        }
        
        // Record this usage too
        manager.RecordUsage(ctx, &BudgetTokenUsage{
            UserID:       userID,
            SessionID:    sessionID,
            TaskID:       "task-2",
            InputTokens:  100,
            OutputTokens: 0,
        })
        
        // Request that would exceed budget (800 used + 300 new = 1100/1000) should be blocked
        result, err = manager.CheckBudgetWithBackpressure(ctx, userID, sessionID, "task-3", 300)
        if err != nil {
            t.Fatalf("CheckBudget failed: %v", err)
        }
        if result.CanProceed {
            t.Error("Should block request exceeding budget with hard limit")
        }
    })
    
    t.Run("ConcurrentRequestHandling", func(t *testing.T) {
        ctx := context.Background()
        userID := "test-user-2"
        sessionID := "test-session-2"
        
        // Set both user and session budgets
        manager.SetUserBudget(userID, &TokenBudget{
            UserDailyBudget: 10000,
            UserDailyUsed:   0,
            HardLimit:       false,
        })
        
        manager.SetSessionBudget(sessionID, &TokenBudget{
            SessionBudget:     10000,
            SessionTokensUsed: 0,
            HardLimit:         false,
        })
        
        // Pre-seed usage close to backpressure threshold so some requests trigger delay
        manager.RecordUsage(ctx, &BudgetTokenUsage{
            UserID:       userID,
            SessionID:    sessionID,
            TaskID:       "seed",
            InputTokens:  8200,
            OutputTokens: 0,
        })

        // Test concurrent requests
        var wg sync.WaitGroup
        var successCount int32
        var backpressureCount int32
        var mu sync.Mutex
        
        for i := 0; i < 20; i++ {
            wg.Add(1)
            go func(taskNum int) {
                defer wg.Done()
                
                result, err := manager.CheckBudgetWithBackpressure(
                    ctx, userID, sessionID, 
                    fmt.Sprintf("task-%d", taskNum), 600)
                
                if err != nil {
                    return
                }
                
                if result.CanProceed {
                    atomic.AddInt32(&successCount, 1)
                    
                    // Simulate recording usage for successful requests
                    mu.Lock()
                    manager.RecordUsage(ctx, &BudgetTokenUsage{
                        UserID:       userID,
                        SessionID:    sessionID,
                        TaskID:       fmt.Sprintf("task-%d", taskNum),
                        InputTokens:  600,
                        OutputTokens: 0,
                    })
                    mu.Unlock()
                }
                if result.BackpressureActive {
                    atomic.AddInt32(&backpressureCount, 1)
                    // Simulate backpressure delay
                    time.Sleep(time.Duration(result.BackpressureDelay) * time.Millisecond)
                }
            }(i)
        }
        
        wg.Wait()
        
        // Should have some successful requests
        if successCount == 0 {
            t.Error("No successful requests in concurrent test")
        }
        
        // Should have activated backpressure for some requests (20 * 600 = 12000 > 10000 * 0.8)
        if backpressureCount == 0 {
            t.Error("Backpressure was not activated in concurrent scenario")
        }
        
        t.Logf("Concurrent test: %d success, %d with backpressure", 
               successCount, backpressureCount)
    })
    
    t.Run("AdaptiveRateLimiting", func(t *testing.T) {
        userID := "test-user-3"
        
        // Set rate limit
        manager.SetRateLimit(userID, 10, time.Second) // 10 requests per second
        
        startTime := time.Now()
        requestCount := 0
        
        // Attempt 20 requests rapidly
        for i := 0; i < 20; i++ {
            allowed := manager.CheckRateLimit(userID)
            if allowed {
                requestCount++
            }
        }
        
        elapsed := time.Since(startTime)
        
        // Should have rate limited some requests
        if requestCount >= 20 {
            t.Error("Rate limiting not working - all requests allowed")
        }
        
        if requestCount > 12 { // Allow some tolerance
            t.Errorf("Too many requests allowed: %d in %v", requestCount, elapsed)
        }
        
        // Wait for rate limit window to reset
        time.Sleep(time.Second)
        
        // Should allow requests again
        allowed := manager.CheckRateLimit(userID)
        if !allowed {
            t.Error("Rate limit should reset after window")
        }
    })
    
    t.Run("BudgetPressureGradient", func(t *testing.T) {
        userID := "test-user-4"
        
        // Initial setup with zero usage
        manager.SetUserBudget(userID, &TokenBudget{
            UserDailyBudget: 10000,
            UserDailyUsed:   0,
        })
        
        testCases := []struct {
            used     int
            expected string
        }{
            {0, "low"},         // 0% usage
            {2500, "low"},      // 25% usage (< 50%)
            {5000, "medium"},   // 50% usage (50% <= x < 75%)
            {7500, "high"},     // 75% usage (75% <= x < 90%)
            {9000, "critical"}, // 90% usage (>= 90%)
        }
        
        for _, tc := range testCases {
            // Set the specific usage level
            manager.SetUserBudget(userID, &TokenBudget{
                UserDailyBudget: 10000,
                UserDailyUsed:   tc.used,
            })
            
            pressure := manager.GetBudgetPressure(userID)
            if pressure != tc.expected {
                // Calculate actual percentage for debugging
                percent := float64(tc.used) / float64(10000) * 100
                t.Errorf("At %d tokens used (%.1f%%), expected pressure %s, got %s",
                        tc.used, percent, tc.expected, pressure)
            }
        }
    })
    
    t.Run("BackpressureDelayCalculation", func(t *testing.T) {
        manager := NewBudgetManager(nil, zap.NewNop())
        
        testCases := []struct {
            usagePercent float64
            minDelay     int
            maxDelay     int
        }{
            {0.5, 0, 0},       // No delay below threshold
            {0.8, 10, 100},    // Small delay at threshold
            {0.85, 100, 500},  // Medium delay
            {0.9, 500, 1000},  // High delay
            {0.95, 1000, 2000}, // Very high delay
            {1.0, 2000, 5000},  // Maximum delay at limit
        }
        
        for _, tc := range testCases {
            delay := manager.calculateBackpressureDelay(tc.usagePercent)
            if delay < tc.minDelay || delay > tc.maxDelay {
                t.Errorf("For %.1f%% usage, expected delay %d-%dms, got %dms",
                        tc.usagePercent*100, tc.minDelay, tc.maxDelay, delay)
            }
        }
    })
}

// TestBudgetManagerCircuitBreaker tests circuit breaker functionality
func TestBudgetManagerCircuitBreaker(t *testing.T) {
    db, _ := sql.Open("sqlite3", ":memory:")
    defer db.Close()
    createTestSchema(t, db)
    
    logger := zap.NewNop()
    manager := NewBudgetManager(db, logger)
    
    ctx := context.Background()
    userID := "breaker-user"
    
    // Configure circuit breaker
    manager.ConfigureCircuitBreaker(userID, CircuitBreakerConfig{
        FailureThreshold: 3,
        ResetTimeout:     time.Second,
        HalfOpenRequests: 1,
    })
    
    // Simulate failures to trip the breaker
    for i := 0; i < 3; i++ {
        manager.RecordFailure(userID)
    }
    
    // Circuit should be open
    state := manager.GetCircuitState(userID)
    if state != "open" {
        t.Errorf("Circuit should be open after failures, got %s", state)
    }
    
    // Requests should be rejected
    result, _ := manager.CheckBudgetWithCircuitBreaker(ctx, userID, "session", "task", 100)
    if result.CanProceed {
        t.Error("Circuit breaker should block requests when open")
    }
    if result.CircuitBreakerOpen != true {
        t.Error("Should indicate circuit breaker is open")
    }
    
    // Wait for reset timeout
    time.Sleep(time.Second + 100*time.Millisecond)
    
    // Circuit should be half-open
    state = manager.GetCircuitState(userID)
    if state != "half-open" {
        t.Errorf("Circuit should be half-open after timeout, got %s", state)
    }
    
    // One request should be allowed (half-open test)
    result, _ = manager.CheckBudgetWithCircuitBreaker(ctx, userID, "session", "task", 100)
    if !result.CanProceed {
        t.Error("Should allow one request in half-open state")
    }
    
    // Record success to close circuit
    manager.RecordSuccess(userID)
    
    state = manager.GetCircuitState(userID)
    if state != "closed" {
        t.Errorf("Circuit should be closed after success, got %s", state)
    }
}

// TestBudgetAllocationStrategies tests different budget allocation strategies
func TestBudgetAllocationStrategies(t *testing.T) {
    manager := NewBudgetManager(nil, zap.NewNop())
    
    t.Run("PriorityBasedAllocation", func(t *testing.T) {
        ctx := context.Background()
        
        // Set up priority tiers
        manager.SetPriorityTiers(map[string]PriorityTier{
            "critical": {Priority: 1, BudgetMultiplier: 2.0},
            "high":     {Priority: 2, BudgetMultiplier: 1.5},
            "normal":   {Priority: 3, BudgetMultiplier: 1.0},
            "low":      {Priority: 4, BudgetMultiplier: 0.5},
        })
        
        baseBudget := 1000
        
        testCases := []struct {
            priority string
            expected int
        }{
            {"critical", 2000},
            {"high", 1500},
            {"normal", 1000},
            {"low", 500},
        }
        
        for _, tc := range testCases {
            allocated := manager.AllocateBudgetByPriority(ctx, baseBudget, tc.priority)
            if allocated != tc.expected {
                t.Errorf("Priority %s: expected %d, got %d", 
                        tc.priority, tc.expected, allocated)
            }
        }
    })
    
    t.Run("DynamicReallocation", func(t *testing.T) {
        ctx := context.Background()
        
        // Track multiple sessions
        sessions := []string{"session-1", "session-2", "session-3"}
        totalBudget := 30000
        
        // Initial equal allocation
        manager.AllocateBudgetAcrossSessions(ctx, sessions, totalBudget)

        // Ensure session budgets exist so usage is tracked
        for _, s := range sessions {
            manager.SetSessionBudget(s, &TokenBudget{SessionBudget: 20000})
        }

        for _, session := range sessions {
            budget := manager.GetSessionAllocation(session)
            if budget != 10000 {
                t.Errorf("Session %s: expected 10000, got %d", session, budget)
            }
        }
        
        // Simulate usage patterns
        manager.RecordUsage(ctx, &BudgetTokenUsage{
            SessionID:    "session-1",
            InputTokens:  9000, // Heavy usage
            OutputTokens: 0,
        })
        manager.RecordUsage(ctx, &BudgetTokenUsage{
            SessionID:    "session-2",
            InputTokens:  2000, // Light usage
            OutputTokens: 0,
        })
        
        // Trigger reallocation based on usage patterns
        manager.ReallocateBudgetsByUsage(ctx, sessions)
        
        // Session 1 should get more budget due to high usage
        budget1 := manager.GetSessionAllocation("session-1")
        budget2 := manager.GetSessionAllocation("session-2")
        
        if budget1 <= budget2 {
            t.Error("High-usage session should get more budget after reallocation")
        }
    })
}

// Helper function to create test schema
func createTestSchema(t *testing.T, db *sql.DB) {
    schema := `
    CREATE TABLE IF NOT EXISTS token_usage (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id TEXT NOT NULL,
        task_id TEXT,
        session_id TEXT,
        provider TEXT,
        model TEXT,
        prompt_tokens INTEGER DEFAULT 0,
        completion_tokens INTEGER DEFAULT 0,
        total_tokens INTEGER DEFAULT 0,
        cost_usd REAL DEFAULT 0,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    
    CREATE TABLE IF NOT EXISTS budget_policies (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id TEXT UNIQUE,
        daily_limit INTEGER,
        monthly_limit INTEGER,
        hard_limit BOOLEAN DEFAULT 1,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    `
    
    if _, err := db.Exec(schema); err != nil {
        t.Fatalf("Failed to create test schema: %v", err)
    }
}
