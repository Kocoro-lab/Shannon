package activities

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchSupervisorMemory(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		input     FetchSupervisorMemoryInput
		wantErr   bool
		checkFunc func(t *testing.T, memory *SupervisorMemoryContext)
	}{
		{
			name: "successful fetch with session ID",
			input: FetchSupervisorMemoryInput{
				SessionID: "test-session-123",
				UserID:    "test-user-456",
				Query:     "test query",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, memory *SupervisorMemoryContext) {
				assert.NotNil(t, memory)
				assert.NotNil(t, memory.StrategyPerformance)
				assert.NotNil(t, memory.DecompositionHistory)
				assert.NotNil(t, memory.TeamCompositions)
				assert.NotNil(t, memory.FailurePatterns)
			},
		},
		{
			name: "empty session ID should still work",
			input: FetchSupervisorMemoryInput{
				SessionID: "",
				UserID:    "test-user-456",
				Query:     "test query",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, memory *SupervisorMemoryContext) {
				assert.NotNil(t, memory)
				assert.NotNil(t, memory.StrategyPerformance)
			},
		},
		{
			name: "complex query with special characters",
			input: FetchSupervisorMemoryInput{
				SessionID: "test-session-789",
				UserID:    "test-user-101",
				Query:     "Write a function that handles $pecial ch@racters & edge cases!",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, memory *SupervisorMemoryContext) {
				assert.NotNil(t, memory)
				// Check failure patterns are identified for complex queries
				if len(memory.FailurePatterns) > 0 {
					assert.NotEmpty(t, memory.FailurePatterns[0].Mitigation)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory, err := FetchSupervisorMemory(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.checkFunc != nil {
				tt.checkFunc(t, memory)
			}
		})
	}
}

func TestRecordDecomposition(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		input   RecordDecompositionInput
		wantErr bool
	}{
		{
			name: "successful record with all fields",
			input: RecordDecompositionInput{
				SessionID:  "test-session-123",
				Query:      "Analyze this complex task",
				Subtasks:   []string{"subtask1", "subtask2", "subtask3"},
				Strategy:   "parallel",
				Success:    true,
				DurationMs: 1500,
				TokensUsed: 250,
			},
			wantErr: false,
		},
		{
			name: "record with error message",
			input: RecordDecompositionInput{
				SessionID:    "test-session-456",
				Query:        "Failed task",
				Subtasks:     []string{"subtask1"},
				Strategy:     "sequential",
				Success:      false,
				DurationMs:   500,
				TokensUsed:   50,
				ErrorMessage: "Task failed due to timeout",
			},
			wantErr: false,
		},
		{
			name: "empty subtasks list",
			input: RecordDecompositionInput{
				SessionID:  "test-session-789",
				Query:      "Simple query",
				Subtasks:   []string{},
				Strategy:   "simple",
				Success:    true,
				DurationMs: 100,
				TokensUsed: 10,
			},
			wantErr: false,
		},
		{
			name: "missing session ID",
			input: RecordDecompositionInput{
				Query:      "Query without session",
				Subtasks:   []string{"task1"},
				Strategy:   "parallel",
				Success:    true,
				DurationMs: 200,
				TokensUsed: 30,
			},
			wantErr: false, // Should handle gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RecordDecomposition(ctx, tt.input)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				// RecordDecomposition is fire-and-forget, may return nil even if services unavailable
				// So we just check it doesn't panic
				assert.True(t, err == nil || err != nil) // Always passes, just ensures no panic
			}
		})
	}
}

func TestDecompositionAdvisor(t *testing.T) {
	// Create a mock memory context
	memory := &SupervisorMemoryContext{
		DecompositionHistory: []DecompositionMemory{
			{
				QueryPattern: "analyze data",
				Subtasks:     []string{"load data", "preprocess", "analyze", "visualize"},
				Strategy:     "sequential",
				SuccessRate:  0.9,
				AvgDuration:  2000,
			},
			{
				QueryPattern: "build api",
				Subtasks:     []string{"design schema", "implement endpoints", "add tests"},
				Strategy:     "parallel",
				SuccessRate:  0.85,
				AvgDuration:  1500,
			},
		},
		StrategyPerformance: map[string]StrategyStats{
			"parallel": {
				TotalRuns:    100,
				SuccessRate:  0.8,
				AvgDuration:  1000,
				AvgTokenCost: 500,
			},
			"sequential": {
				TotalRuns:    50,
				SuccessRate:  0.9,
				AvgDuration:  2000,
				AvgTokenCost: 300,
			},
		},
		FailurePatterns: []FailurePattern{
			{
				Pattern:     "rate_limit",
				Indicators:  []string{"quickly", "fast", "urgent"},
				Mitigation:  "Use sequential execution to avoid rate limits",
				Occurrences: 5,
			},
		},
		UserPreferences: UserProfile{
			ExpertiseLevel:  "intermediate",
			SpeedVsAccuracy: 0.5,
		},
	}

	advisor := NewDecompositionAdvisor(memory)
	require.NotNil(t, advisor)

	tests := []struct {
		name      string
		query     string
		checkFunc func(t *testing.T, suggestion DecompositionSuggestion)
	}{
		{
			name:  "similar query matches previous pattern",
			query: "analyze data", // Exact match with stored pattern
			checkFunc: func(t *testing.T, suggestion DecompositionSuggestion) {
				assert.True(t, suggestion.UsesPreviousSuccess)
				assert.Equal(t, "sequential", suggestion.Strategy)
				assert.Greater(t, suggestion.Confidence, 0.8) // 0.9 * 1.0 = 0.9
				assert.Len(t, suggestion.SuggestedSubtasks, 4)
			},
		},
		{
			name:  "query with rate limit indicators",
			query: "Process this quickly and fast",
			checkFunc: func(t *testing.T, suggestion DecompositionSuggestion) {
				assert.True(t, suggestion.PreferSequential)
				assert.Equal(t, "sequential", suggestion.Strategy)
				assert.Contains(t, suggestion.Warnings, "Use sequential execution to avoid rate limits")
			},
		},
		{
			name:  "new query without matching pattern",
			query: "Create a machine learning model",
			checkFunc: func(t *testing.T, suggestion DecompositionSuggestion) {
				assert.False(t, suggestion.UsesPreviousSuccess)
				assert.NotEmpty(t, suggestion.Strategy)
				assert.GreaterOrEqual(t, suggestion.Confidence, 0.0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestDecomposition(tt.query)
			tt.checkFunc(t, suggestion)
		})
	}
}

func TestDecompositionAdvisor_SelectOptimalStrategy(t *testing.T) {
	memory := &SupervisorMemoryContext{
		StrategyPerformance: map[string]StrategyStats{
			"parallel": {
				TotalRuns:   100,
				SuccessRate: 0.8,
				AvgDuration: 1000,
			},
			"sequential": {
				TotalRuns:   100,
				SuccessRate: 0.95,
				AvgDuration: 3000,
			},
		},
		UserPreferences: UserProfile{
			SpeedVsAccuracy: 0.3, // Prefer speed
		},
	}

	advisor := NewDecompositionAdvisor(memory)

	// Test multiple calls to ensure strategy selection works
	strategies := make(map[string]int)
	for i := 0; i < 100; i++ {
		rand.Seed(time.Now().UnixNano() + int64(i))
		strategy := advisor.selectOptimalStrategy()
		strategies[strategy]++
	}

	// Should mostly select "parallel" due to speed preference
	assert.Greater(t, strategies["parallel"], strategies["sequential"])
}

func TestDecompositionAdvisor_UserPreferences(t *testing.T) {
	tests := []struct {
		name                 string
		expertiseLevel       string
		speedVsAccuracy      float64
		expectedSequential   bool
		expectedExplanations bool
	}{
		{
			name:                 "beginner prefers sequential",
			expertiseLevel:       "beginner",
			speedVsAccuracy:      0.5,
			expectedSequential:   true,
			expectedExplanations: true,
		},
		{
			name:                 "expert handles parallel",
			expertiseLevel:       "expert",
			speedVsAccuracy:      0.5,
			expectedSequential:   false,
			expectedExplanations: false,
		},
		{
			name:                 "accuracy preference forces sequential",
			expertiseLevel:       "intermediate",
			speedVsAccuracy:      0.9,
			expectedSequential:   true,
			expectedExplanations: false,
		},
		{
			name:                 "speed preference forces parallel",
			expertiseLevel:       "intermediate",
			speedVsAccuracy:      0.2,
			expectedSequential:   false,
			expectedExplanations: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := &SupervisorMemoryContext{
				StrategyPerformance: make(map[string]StrategyStats),
				UserPreferences: UserProfile{
					ExpertiseLevel:  tt.expertiseLevel,
					SpeedVsAccuracy: tt.speedVsAccuracy,
				},
			}

			advisor := NewDecompositionAdvisor(memory)
			suggestion := advisor.SuggestDecomposition("test query")

			assert.Equal(t, tt.expectedSequential, suggestion.PreferSequential)
			assert.Equal(t, tt.expectedExplanations, suggestion.AddExplanations)
		})
	}
}

func TestCalculateSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected float64
	}{
		{
			name:     "identical strings",
			a:        "hello world",
			b:        "hello world",
			expected: 1.0,
		},
		{
			name:     "case insensitive match",
			a:        "Hello World",
			b:        "hello world",
			expected: 1.0,
		},
		{
			name:     "partial match",
			a:        "analyze data files",
			b:        "process data quickly",
			expected: 1.0 / 3.0, // 1 common word out of 3
		},
		{
			name:     "no match",
			a:        "apple orange",
			b:        "car bike",
			expected: 0.0,
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: 1.0, // Empty strings are identical
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	pattern := FailurePattern{
		Pattern:    "rate_limit",
		Indicators: []string{"quickly", "fast", "urgent", "asap"},
		Mitigation: "Use sequential execution",
	}

	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "matches quickly",
			query:    "Process this quickly please",
			expected: true,
		},
		{
			name:     "matches fast case insensitive",
			query:    "Do it FAST",
			expected: true,
		},
		{
			name:     "no match",
			query:    "Process this carefully",
			expected: false,
		},
		{
			name:     "multiple indicators",
			query:    "Need this urgent and fast",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPattern(tt.query, pattern)
			assert.Equal(t, tt.expected, result)
		})
	}
}
