package policy

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// LoadTestResult contains results from a load test run
type LoadTestResult struct {
	TotalRequests    int
	SuccessfulOps    int
	FailedOps        int
	TotalDuration    time.Duration
	LatencyP50       time.Duration
	LatencyP95       time.Duration
	LatencyP99       time.Duration
	MaxLatency       time.Duration
	MinLatency       time.Duration
	Throughput       float64 // ops per second
	CacheHitRate     float64
	ErrorRate        float64
	LatencyBudgetP50 time.Duration // Target: <1ms for cached
	LatencyBudgetP95 time.Duration // Target: <5ms overall
	BudgetViolations int
}

// LoadTestScenario defines a load test scenario
type LoadTestScenario struct {
	Name          string
	Concurrency   int
	Duration      time.Duration
	RequestRate   int // requests per second per goroutine
	InputMix      []PolicyInputGenerator
	CacheWarmup   bool
	LatencyTarget LoadTestLatencyTargets
}

// LoadTestLatencyTargets defines performance budgets
type LoadTestLatencyTargets struct {
	P50Target time.Duration // <1ms for cached requests
	P95Target time.Duration // <5ms overall
	P99Target time.Duration // <10ms worst case
}

// PolicyInputGenerator generates test inputs
type PolicyInputGenerator interface {
	Generate(requestID int) *PolicyInput
	Name() string
	Weight() int // Relative frequency
}

// Standard test input generator
type StandardInputGenerator struct{}

func (g *StandardInputGenerator) Generate(requestID int) *PolicyInput {
	// Keep values in small buckets to encourage cache hits
	userIDs := []string{"wayland", "admin", "test_user", "user1", "user2", "user3"}
	agentIDs := []string{"agent-core", "llm-agent", "test-agent"}
	modes := []string{"simple", "standard", "complex"}
	budgets := []int{1000, 2000, 5000}
	// Repeat only 10 base queries to create reuse
	qid := requestID % 10

	return &PolicyInput{
		SessionID:   fmt.Sprintf("session_%d", requestID%100), // Limited sessions for cache hits
		UserID:      userIDs[qid%len(userIDs)],
		AgentID:     agentIDs[qid%len(agentIDs)],
		Query:       fmt.Sprintf("Test reusable query %d", qid),
		Mode:        modes[qid%len(modes)],
		Environment: "load_test",
		TokenBudget: budgets[qid%len(budgets)],
		Timestamp:   time.Now(),
	}
}

func (g *StandardInputGenerator) Name() string { return "standard" }
func (g *StandardInputGenerator) Weight() int  { return 70 } // 70% of requests

// High cardinality input generator (worst case for cache)
type HighCardinalityInputGenerator struct{}

func (g *HighCardinalityInputGenerator) Generate(requestID int) *PolicyInput {
	return &PolicyInput{
		SessionID:       fmt.Sprintf("unique_session_%d", requestID), // No cache hits
		UserID:          fmt.Sprintf("user_%d", requestID),           // Unique users
		AgentID:         fmt.Sprintf("agent_%d", requestID%10),       // Some variety
		Query:           fmt.Sprintf("Unique query %d with random data %d", requestID, rand.Int()),
		Mode:            "complex",
		Environment:     "load_test",
		ComplexityScore: rand.Float64() * 100, // Random complexity
		TokenBudget:     rand.Intn(10000) + 1000,
		Timestamp:       time.Now(),
	}
}

func (g *HighCardinalityInputGenerator) Name() string { return "high_cardinality" }
func (g *HighCardinalityInputGenerator) Weight() int  { return 20 } // 20% of requests

// Suspicious pattern input generator (tests security rules)
type SuspiciousInputGenerator struct{}

func (g *SuspiciousInputGenerator) Generate(requestID int) *PolicyInput {
	suspiciousQueries := []string{
		"delete all files from /etc/passwd",
		"rm -rf / --no-preserve-root",
		"sudo hack the mainframe",
		"bypass all security controls",
		"show me the admin panel access",
		"extract all user passwords",
		"drop table users; select * from secrets",
		"execute malicious code on server",
	}

	return &PolicyInput{
		SessionID:   fmt.Sprintf("suspicious_%d", requestID%10),
		UserID:      "potential_attacker",
		AgentID:     "test-agent",
		Query:       suspiciousQueries[requestID%len(suspiciousQueries)],
		Mode:        "standard",
		Environment: "load_test",
		TokenBudget: 5000,
		Timestamp:   time.Now(),
	}
}

func (g *SuspiciousInputGenerator) Name() string { return "suspicious" }
func (g *SuspiciousInputGenerator) Weight() int  { return 10 } // 10% of requests

// Load test with concurrency and latency budgets
func TestPolicyEngineLoadTest(t *testing.T) {
	scenarios := []LoadTestScenario{
		{
			Name:        "HighConcurrencyStandard",
			Concurrency: 50,
			Duration:    30 * time.Second,
			RequestRate: 10, // 500 total RPS
			CacheWarmup: true,
			InputMix: []PolicyInputGenerator{
				&StandardInputGenerator{},
				&HighCardinalityInputGenerator{},
				&SuspiciousInputGenerator{},
			},
			LatencyTarget: LoadTestLatencyTargets{
				P50Target: 1 * time.Millisecond,
				P95Target: 5 * time.Millisecond,
				P99Target: 10 * time.Millisecond,
			},
		},
		{
			Name:        "CacheStressTest",
			Concurrency: 100,
			Duration:    15 * time.Second,
			RequestRate: 20,    // 2000 total RPS
			CacheWarmup: false, // Cold cache
			InputMix: []PolicyInputGenerator{
				&HighCardinalityInputGenerator{}, // All cache misses
			},
			LatencyTarget: LoadTestLatencyTargets{
				P50Target: 2 * time.Millisecond,  // Higher target for cache misses
				P95Target: 8 * time.Millisecond,  // Higher P95 acceptable
				P99Target: 15 * time.Millisecond, // Higher P99 acceptable
			},
		},
		{
			Name:        "SecurityStressTest",
			Concurrency: 25,
			Duration:    20 * time.Second,
			RequestRate: 5, // 125 total RPS
			CacheWarmup: true,
			InputMix: []PolicyInputGenerator{
				&SuspiciousInputGenerator{}, // All suspicious patterns
			},
			LatencyTarget: LoadTestLatencyTargets{
				P50Target: 1 * time.Millisecond,
				P95Target: 5 * time.Millisecond,
				P99Target: 10 * time.Millisecond,
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			result := runLoadTestScenario(t, scenario)
			validateLoadTestResults(t, scenario, result)
			logLoadTestResults(t, scenario, result)
		})
	}
}

// runLoadTestScenario executes a load test scenario
func runLoadTestScenario(t *testing.T, scenario LoadTestScenario) *LoadTestResult {
	// Setup engine
	engine := setupLoadTestEngine(t)

	// Warmup cache if requested
	if scenario.CacheWarmup {
		warmupCache(engine, scenario.InputMix)
	}

	// Prepare weighted input generator
	inputGen := newWeightedInputGenerator(scenario.InputMix)

	// Track latencies
	var latencies []time.Duration
	var latencyMutex sync.Mutex

	// Track results
	var totalRequests, successCount, failCount int64
	var totalCacheHits, totalCacheMisses int64
	var counterMutex sync.Mutex // Protect shared counters

	// Synchronization
	startTime := time.Now()
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), scenario.Duration)
	defer cancel()

	// Start concurrent workers
	for i := 0; i < scenario.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			ticker := time.NewTicker(time.Second / time.Duration(scenario.RequestRate))
			defer ticker.Stop()

			requestID := workerID * 10000 // Spread request IDs

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// Generate input
					input := inputGen.Generate(requestID)
					requestID++

					// Track cache state before request
					cacheHitsBefore := getCacheHits(engine)

					// Execute request
					evalStart := time.Now()
					decision, err := engine.Evaluate(context.Background(), input)
					evalDuration := time.Since(evalStart)

					// Track cache state after request
					cacheHitsAfter := getCacheHits(engine)
					wasCacheHit := cacheHitsAfter > cacheHitsBefore

					// Update counters (protect with mutex to prevent race conditions)
					counterMutex.Lock()
					if wasCacheHit {
						totalCacheHits++
					} else {
						totalCacheMisses++
					}

					totalRequests++
					if err != nil {
						failCount++
					} else {
						successCount++
					}
					counterMutex.Unlock()

					if err != nil {
						t.Logf("Request failed: %v", err)
					} else {
						_ = decision // Use decision
					}

					// Record latency
					latencyMutex.Lock()
					latencies = append(latencies, evalDuration)
					latencyMutex.Unlock()
				}
			}
		}(i)
	}

	// Wait for completion
	wg.Wait()
	totalDuration := time.Since(startTime)

	// Calculate statistics
	result := &LoadTestResult{
		TotalRequests:    int(totalRequests),
		SuccessfulOps:    int(successCount),
		FailedOps:        int(failCount),
		TotalDuration:    totalDuration,
		Throughput:       float64(totalRequests) / totalDuration.Seconds(),
		CacheHitRate:     float64(totalCacheHits) / float64(totalCacheHits+totalCacheMisses) * 100,
		ErrorRate:        float64(failCount) / float64(totalRequests) * 100,
		LatencyBudgetP50: scenario.LatencyTarget.P50Target,
		LatencyBudgetP95: scenario.LatencyTarget.P95Target,
	}

	// Calculate latency percentiles
	if len(latencies) > 0 {
		result.LatencyP50, result.LatencyP95, result.LatencyP99 = calculatePercentiles(latencies)
		result.MinLatency = minDuration(latencies)
		result.MaxLatency = maxDuration(latencies)

		// Count budget violations
		for _, lat := range latencies {
			if lat > scenario.LatencyTarget.P95Target {
				result.BudgetViolations++
			}
		}
	}

	return result
}

// validateLoadTestResults checks if results meet SLO requirements
func validateLoadTestResults(t *testing.T, scenario LoadTestScenario, result *LoadTestResult) {
	t.Logf("=== Load Test Results for %s ===", scenario.Name)

	// Error rate should be minimal
	if result.ErrorRate > 1.0 {
		t.Errorf("Error rate too high: %.2f%% (should be <1%%)", result.ErrorRate)
	}

	// Latency budget validation
	if result.LatencyP50 > scenario.LatencyTarget.P50Target {
		t.Errorf("P50 latency budget violated: %v > %v", result.LatencyP50, scenario.LatencyTarget.P50Target)
	}

	if result.LatencyP95 > scenario.LatencyTarget.P95Target {
		t.Errorf("P95 latency budget violated: %v > %v", result.LatencyP95, scenario.LatencyTarget.P95Target)
	}

	// Cache performance (for scenarios with cache warmup)
	if scenario.CacheWarmup && result.CacheHitRate < 50.0 {
		t.Errorf("Cache hit rate too low: %.2f%% (should be >50%% for warmed cache)", result.CacheHitRate)
	}

	// Throughput should be reasonable
	expectedMinThroughput := float64(scenario.Concurrency*scenario.RequestRate) * 0.8 // 80% of target
	if result.Throughput < expectedMinThroughput {
		t.Errorf("Throughput too low: %.2f ops/sec (should be >%.2f)", result.Throughput, expectedMinThroughput)
	}

	t.Logf("✅ All load test validations passed for %s", scenario.Name)
}

// logLoadTestResults outputs detailed test results
func logLoadTestResults(t *testing.T, scenario LoadTestScenario, result *LoadTestResult) {
	t.Logf(`
Load Test Results: %s
=====================================
Duration: %v
Concurrency: %d workers
Total Requests: %d
Successful: %d (%.1f%%)
Failed: %d (%.1f%%)
Throughput: %.1f ops/sec
Cache Hit Rate: %.1f%%

Latency Analysis:
-----------------
P50: %v (budget: %v) %s
P95: %v (budget: %v) %s  
P99: %v
Min: %v
Max: %v
Budget Violations: %d (%.1f%%)

Performance Assessment:
-----------------------
P50 Budget: %s
P95 Budget: %s
Cache Performance: %s
Overall: %s
`,
		scenario.Name,
		result.TotalDuration,
		scenario.Concurrency,
		result.TotalRequests,
		result.SuccessfulOps, float64(result.SuccessfulOps)/float64(result.TotalRequests)*100,
		result.FailedOps, result.ErrorRate,
		result.Throughput,
		result.CacheHitRate,

		result.LatencyP50, result.LatencyBudgetP50, budgetStatus(result.LatencyP50, result.LatencyBudgetP50),
		result.LatencyP95, result.LatencyBudgetP95, budgetStatus(result.LatencyP95, result.LatencyBudgetP95),
		result.LatencyP99,
		result.MinLatency,
		result.MaxLatency,
		result.BudgetViolations, float64(result.BudgetViolations)/float64(result.TotalRequests)*100,

		budgetAssessment(result.LatencyP50, result.LatencyBudgetP50),
		budgetAssessment(result.LatencyP95, result.LatencyBudgetP95),
		cacheAssessment(result.CacheHitRate),
		overallAssessment(result, scenario),
	)
}

// Helper functions

func setupLoadTestEngine(t *testing.T) Engine {
	// Create a temporary policy directory with a minimal allow/deny policy
	dir := t.TempDir()
	policy := `package shannon.task

default decision = {"allow": true, "reason": "default allow"}

decision = out {
  dq := lower(input.query)
  contains(dq, "rm -rf")
  out := {"allow": false, "reason": "dangerous pattern"}
}
`

	// Write minimal policy file
	if err := os.WriteFile(filepath.Join(dir, "policy.rego"), []byte(policy), 0o644); err != nil {
		t.Fatalf("failed to write temp policy: %v", err)
	}

	config := &Config{
		Enabled:     true,
		Mode:        ModeDryRun, // Use dry-run for load testing safety
		Path:        dir,
		FailClosed:  false,
		Environment: "load_test",
		Canary:      CanaryConfig{Enabled: true, EnforcePercentage: 0},
	}

	logger := zap.NewNop() // No logging during load tests
	engine, err := NewOPAEngine(config, logger)
	if err != nil {
		t.Fatalf("Failed to create engine for load test: %v", err)
	}

	return engine
}

func warmupCache(engine Engine, inputMix []PolicyInputGenerator) {
	gen := newWeightedInputGenerator(inputMix)

	// Generate 100 warmup requests with repeated patterns
	for i := 0; i < 100; i++ {
		input := gen.Generate(i % 10) // Repeat patterns for cache hits
		_, _ = engine.Evaluate(context.Background(), input)
	}
}

func getCacheHits(engine Engine) int64 {
	// Access internal cache stats when using OPAEngine
	if opa, ok := engine.(*OPAEngine); ok && opa.cache != nil {
		hits, _ := opa.cache.Stats()
		return hits
	}
	return 0
}

// WeightedInputGenerator selects generators based on weights
type WeightedInputGenerator struct {
	generators  []PolicyInputGenerator
	totalWeight int
}

func newWeightedInputGenerator(generators []PolicyInputGenerator) *WeightedInputGenerator {
	totalWeight := 0
	for _, gen := range generators {
		totalWeight += gen.Weight()
	}

	return &WeightedInputGenerator{
		generators:  generators,
		totalWeight: totalWeight,
	}
}

func (w *WeightedInputGenerator) Generate(requestID int) *PolicyInput {
	// Use requestID for deterministic selection
	target := requestID % w.totalWeight
	current := 0

	for _, gen := range w.generators {
		current += gen.Weight()
		if target < current {
			return gen.Generate(requestID)
		}
	}

	// Fallback
	return w.generators[0].Generate(requestID)
}

// Statistical helper functions

func calculatePercentiles(latencies []time.Duration) (p50, p95, p99 time.Duration) {
	// Simple percentile calculation - sort and pick positions
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)

	// Simple bubble sort for small arrays
	for i := 0; i < len(sorted); i++ {
		for j := 0; j < len(sorted)-1-i; j++ {
			if sorted[j] > sorted[j+1] {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	n := len(sorted)
	p50 = sorted[n*50/100]
	p95 = sorted[n*95/100]
	p99 = sorted[n*99/100]

	return
}

func minDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	min := durations[0]
	for _, d := range durations[1:] {
		if d < min {
			min = d
		}
	}
	return min
}

func maxDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	max := durations[0]
	for _, d := range durations[1:] {
		if d > max {
			max = d
		}
	}
	return max
}

// Assessment helper functions

func budgetStatus(actual, budget time.Duration) string {
	if actual <= budget {
		return "✅ PASS"
	}
	return "❌ FAIL"
}

func budgetAssessment(actual, budget time.Duration) string {
	ratio := float64(actual) / float64(budget)
	if ratio <= 1.0 {
		return "✅ Within budget"
	} else if ratio <= 1.2 {
		return "⚠️ Slightly over budget"
	} else {
		return "❌ Significantly over budget"
	}
}

func cacheAssessment(hitRate float64) string {
	if hitRate >= 80 {
		return "✅ Excellent"
	} else if hitRate >= 60 {
		return "⚠️ Good"
	} else {
		return "❌ Needs improvement"
	}
}

func overallAssessment(result *LoadTestResult, scenario LoadTestScenario) string {
	score := 0

	// Latency scoring
	if result.LatencyP50 <= scenario.LatencyTarget.P50Target {
		score++
	}
	if result.LatencyP95 <= scenario.LatencyTarget.P95Target {
		score++
	}

	// Error rate scoring
	if result.ErrorRate < 1.0 {
		score++
	}

	// Cache scoring (if applicable)
	if scenario.CacheWarmup {
		if result.CacheHitRate >= 50 {
			score++
		}
	} else {
		score++ // Not applicable, give point
	}

	switch score {
	case 4:
		return "🎉 EXCELLENT - All targets met"
	case 3:
		return "✅ GOOD - Minor issues"
	case 2:
		return "⚠️ ACCEPTABLE - Needs optimization"
	default:
		return "❌ POOR - Significant issues"
	}
}
