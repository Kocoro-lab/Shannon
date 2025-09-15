package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

// BenchmarkPolicyEvaluationCold measures performance without cache
func BenchmarkPolicyEvaluationCold(b *testing.B) {
	
	input := &PolicyInput{
		Query:       "What is the weather today?",
		UserID:      "wayland",
		Mode:        "standard",
		TokenBudget: 1000,
		AgentID:     "test-agent",
	}
	
	b.ResetTimer()
	b.StartTimer()
	
	for i := 0; i < b.N; i++ {
		// Create new engine each time to avoid cache
		freshEngine := setupBenchmarkEngine(b)
		_, err := freshEngine.Evaluate(context.Background(), input)
		if err != nil {
			b.Fatalf("Policy evaluation failed: %v", err)
		}
	}
	
	b.StopTimer()
	reportBenchmarkResults(b, "Cold evaluation (no cache)")
}

// BenchmarkPolicyEvaluationWarm measures performance with cache hits
func BenchmarkPolicyEvaluationWarm(b *testing.B) {
	engine := setupBenchmarkEngine(b)
	
	input := &PolicyInput{
		Query:       "What is the weather today?",
		UserID:      "wayland", 
		Mode:        "standard",
		TokenBudget: 1000,
		AgentID:     "test-agent",
	}
	
	// Warm up cache
	_, err := engine.Evaluate(context.Background(), input)
	if err != nil {
		b.Fatalf("Warmup failed: %v", err)
	}
	
	b.ResetTimer()
	b.StartTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(context.Background(), input)
		if err != nil {
			b.Fatalf("Policy evaluation failed: %v", err)
		}
	}
	
	b.StopTimer()
	reportBenchmarkResults(b, "Warm evaluation (cache hit)")
}

// BenchmarkPolicyEvaluationConcurrent measures concurrent performance
func BenchmarkPolicyEvaluationConcurrent(b *testing.B) {
	engine := setupBenchmarkEngine(b)
	
	// Warm up cache with different inputs
	inputs := []*PolicyInput{
		{Query: "What is the weather?", UserID: "user1", Mode: "standard", TokenBudget: 1000, AgentID: "agent1"},
		{Query: "Calculate 2+2", UserID: "user2", Mode: "simple", TokenBudget: 500, AgentID: "agent2"},  
		{Query: "Complex analysis", UserID: "admin", Mode: "complex", TokenBudget: 5000, AgentID: "agent3"},
	}
	
	for _, input := range inputs {
		_, err := engine.Evaluate(context.Background(), input)
		if err != nil {
			b.Fatalf("Warmup failed: %v", err)
		}
	}
	
	b.ResetTimer()
	b.StartTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		inputIndex := 0
		for pb.Next() {
			input := inputs[inputIndex%len(inputs)]
			_, err := engine.Evaluate(context.Background(), input)
			if err != nil {
				b.Fatalf("Concurrent evaluation failed: %v", err)
			}
			inputIndex++
		}
	})
	
	b.StopTimer()
	reportBenchmarkResults(b, "Concurrent evaluation")
}

// BenchmarkPolicyLoadingAndEvaluation measures policy loading + evaluation
func BenchmarkPolicyLoadingAndEvaluation(b *testing.B) {
	config := setupBenchmarkConfig(b)
	logger := zap.NewNop()
	
	input := &PolicyInput{
		Query:       "What is the weather today?",
		UserID:      "wayland",
		Mode:        "standard", 
		TokenBudget: 1000,
		AgentID:     "test-agent",
	}
	
	b.ResetTimer()
	b.StartTimer()
	
	for i := 0; i < b.N; i++ {
		// Create engine and load policies each time
		engine, err := NewOPAEngine(config, logger)
		if err != nil {
			b.Fatalf("Engine creation failed: %v", err)
		}
		
		_, err = engine.Evaluate(context.Background(), input)
		if err != nil {
			b.Fatalf("Policy evaluation failed: %v", err)
		}
	}
	
	b.StopTimer()
	reportBenchmarkResults(b, "Full load + evaluation")
}

// BenchmarkPolicyEvaluationDifferentModes tests performance across modes
func BenchmarkPolicyEvaluationDifferentModes(b *testing.B) {
	modes := []struct {
		name   string
		mode   string
		budget int
	}{
		{"Simple", "simple", 500},
		{"Standard", "standard", 2000},
		{"Complex", "complex", 10000},
	}
	
	for _, mode := range modes {
		b.Run(mode.name, func(b *testing.B) {
			engine := setupBenchmarkEngine(b)
			input := &PolicyInput{
				Query:       fmt.Sprintf("Test query for %s mode", mode.name),
				UserID:      "wayland",
				Mode:        mode.mode,
				TokenBudget: mode.budget,
				AgentID:     "test-agent",
			}
			
			// Warmup
			_, err := engine.Evaluate(context.Background(), input)
			if err != nil {
				b.Fatalf("Warmup failed: %v", err)
			}
			
			b.ResetTimer()
			
			for i := 0; i < b.N; i++ {
				_, err := engine.Evaluate(context.Background(), input)
				if err != nil {
					b.Fatalf("Policy evaluation failed: %v", err)
				}
			}
			
			reportBenchmarkResults(b, fmt.Sprintf("%s mode evaluation", mode.name))
		})
	}
}

// BenchmarkCachePerformance measures cache hit/miss scenarios
func BenchmarkCachePerformance(b *testing.B) {
	engine := setupBenchmarkEngine(b)
	
	// Generate many unique inputs to test cache behavior
	inputs := make([]*PolicyInput, 100)
	for i := 0; i < 100; i++ {
		inputs[i] = &PolicyInput{
			Query:       fmt.Sprintf("Unique query %d", i),
			UserID:      fmt.Sprintf("user%d", i%10), // 10 different users
			Mode:        "standard",
			TokenBudget: 1000 + i, // Vary token budget
			AgentID:     "test-agent",
		}
	}
	
	b.Run("CacheHits", func(b *testing.B) {
		// Warm up cache with first 10 inputs
		for i := 0; i < 10; i++ {
			_, err := engine.Evaluate(context.Background(), inputs[i])
			if err != nil {
				b.Fatalf("Cache warmup failed: %v", err)
			}
		}
		
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			// Only use first 10 inputs (should hit cache)
			input := inputs[i%10]
			_, err := engine.Evaluate(context.Background(), input)
			if err != nil {
				b.Fatalf("Cache hit evaluation failed: %v", err)
			}
		}
		
		reportBenchmarkResults(b, "Cache hits")
	})
	
	b.Run("CacheMisses", func(b *testing.B) {
		b.ResetTimer()
		
		for i := 0; i < b.N; i++ {
			// Use different input each time (cache misses)
			input := inputs[i%100]
			_, err := engine.Evaluate(context.Background(), input)
			if err != nil {
				b.Fatalf("Cache miss evaluation failed: %v", err)
			}
		}
		
		reportBenchmarkResults(b, "Cache misses")
	})
}

// Helper functions

func setupBenchmarkEngine(b *testing.B) Engine {
	config := setupBenchmarkConfig(b)
	logger := zap.NewNop()
	
	engine, err := NewOPAEngine(config, logger)
	if err != nil {
		b.Fatalf("Failed to create benchmark engine: %v", err)
	}
	
	return engine
}

func setupBenchmarkConfig(b *testing.B) *Config {
	// Create temporary directory for benchmark policies
	tempDir := b.TempDir()
	
	// Write benchmark policy
	policyContent := `
package shannon.task

default decision := {
    "allow": false,
    "reason": "default deny - no matching allow rule",
    "require_approval": false
}

# Simple mode operations
decision := {
    "allow": true,
    "reason": "simple mode operation - low risk",
    "require_approval": false
} {
    input.mode == "simple"
    input.token_budget <= 1000
}

# Standard operations for authenticated users  
decision := {
    "allow": true,
    "reason": "standard operation for authorized user",
    "require_approval": false
} {
    input.mode == "standard"
    input.user_id != ""
    input.token_budget <= 5000
}

# Complex operations - require approval in production
decision := {
    "allow": true,
    "reason": "complex operation approved",
    "require_approval": false
} {
    input.mode == "complex"
    input.token_budget <= 15000
}
`
	
	policyPath := filepath.Join(tempDir, "benchmark.rego")
	err := os.WriteFile(policyPath, []byte(policyContent), 0644)
	if err != nil {
		b.Fatalf("Failed to write benchmark policy: %v", err)
	}
	
	return &Config{
		Enabled:     true,
		Mode:        ModeEnforce,
		Path:        tempDir,
		FailClosed:  false,
		Environment: "benchmark",
	}
}

func reportBenchmarkResults(b *testing.B, description string) {
	nsPerOp := b.Elapsed().Nanoseconds() / int64(b.N)
	usPerOp := float64(nsPerOp) / 1000.0
	msPerOp := usPerOp / 1000.0
	
	b.Logf("%s: %d ops, %.2f ms/op, %.2f μs/op, %d ns/op", 
		description, b.N, msPerOp, usPerOp, nsPerOp)
		
	// Validate sub-millisecond claim
	if msPerOp >= 1.0 {
		b.Logf("WARNING: Operation took %.2f ms (>1ms target)", msPerOp)
	} else {
		b.Logf("SUCCESS: Sub-millisecond performance achieved: %.2f ms", msPerOp)
	}
}

// BenchmarkMemoryUsage provides memory allocation insights
func BenchmarkMemoryUsage(b *testing.B) {
	engine := setupBenchmarkEngine(b)
	input := &PolicyInput{
		Query:       "Memory usage test",
		UserID:      "wayland",
		Mode:        "standard",
		TokenBudget: 1000,
		AgentID:     "test-agent",
	}
	
	b.ReportAllocs()
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(context.Background(), input)
		if err != nil {
			b.Fatalf("Memory benchmark failed: %v", err)
		}
	}
}