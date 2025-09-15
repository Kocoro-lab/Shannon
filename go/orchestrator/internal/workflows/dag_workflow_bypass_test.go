package workflows

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/stretchr/testify/assert"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/activity"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
)

// TestBypassSingleResult verifies that single successful results bypass synthesis
func TestBypassSingleResult(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register the child workflow that AgentDAGWorkflow calls
	env.RegisterWorkflow(strategies.DAGWorkflow)

	// Mock DecomposeTask - returns simple task with single subtask
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.DecompositionInput) (activities.DecompositionResult, error) {
			return activities.DecompositionResult{
				ComplexityScore: 0.2,
				Mode:           "simple",
				Subtasks: []activities.Subtask{
					{ID: "1", Description: "Calculate 2+2"},
				},
			}, nil
		},
		activity.RegisterOptions{Name: "DecomposeTask"},
	)

	// Mock ExecuteSimpleTask - returns single successful result
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.ExecuteSimpleTaskInput) (activities.ExecuteSimpleTaskResult, error) {
			return activities.ExecuteSimpleTaskResult{
				Response:   "4",
				Success:    true,
				TokensUsed: 10,
			}, nil
		},
		activity.RegisterOptions{Name: "ExecuteSimpleTask"},
	)

	// Mock UpdateSessionResult
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.SessionUpdateInput) (activities.SessionUpdateResult, error) {
			return activities.SessionUpdateResult{Success: true}, nil
		},
		activity.RegisterOptions{Name: "UpdateSessionResult"},
	)

	// Mock RecordQuery
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.RecordQueryInput) (activities.RecordQueryResult, error) {
			return activities.RecordQueryResult{}, nil
		},
		activity.RegisterOptions{Name: "RecordQuery"},
	)

	// THIS SHOULD NOT BE CALLED when bypass is enabled
	synthesisCallCount := 0
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.SynthesisInput) (activities.SynthesisResult, error) {
			synthesisCallCount++
			return activities.SynthesisResult{
				FinalResult: "Synthesized: 4",
				TokensUsed:  20,
			}, nil
		},
		activity.RegisterOptions{Name: "SynthesizeResultsLLM"},
	)

	// Test with bypass enabled
	input := TaskInput{
		Query:              "What is 2+2?",
		UserID:            "test-user",
		SessionID:         "test-session",
		BypassSingleResult: true, // Enable bypass
	}

	env.ExecuteWorkflow(AgentDAGWorkflow, input)

	assert.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())

	var result TaskResult
	assert.NoError(t, env.GetWorkflowResult(&result))

	// Verify result is from agent, not synthesis
	assert.Equal(t, "4", result.Result)
	assert.True(t, result.Success)
	assert.Equal(t, 10, result.TokensUsed) // Only agent tokens, no synthesis

	// Verify synthesis was NOT called
	assert.Equal(t, 0, synthesisCallCount, "Synthesis should not be called when bypass is enabled")
}

// TestNoBypassMultipleResults verifies synthesis is called for multiple results
func TestNoBypassMultipleResults(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register the child workflow that AgentDAGWorkflow calls
	env.RegisterWorkflow(strategies.DAGWorkflow)

	// Mock DecomposeTask - returns complex task with multiple subtasks
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.DecompositionInput) (activities.DecompositionResult, error) {
			return activities.DecompositionResult{
				ComplexityScore: 0.7,
				Mode:           "complex",
				Subtasks: []activities.Subtask{
					{ID: "1", Description: "First part"},
					{ID: "2", Description: "Second part"},
				},
			}, nil
		},
		activity.RegisterOptions{Name: "DecomposeTask"},
	)

	// Mock ExecuteAgent for multiple agents
	var agentCallCount int32
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			atomic.AddInt32(&agentCallCount, 1)
			return activities.AgentExecutionResult{
				AgentID:   input.AgentID,
				Response:  "Partial result " + input.AgentID,
				Success:   true,
				TokensUsed: 15,
			}, nil
		},
		activity.RegisterOptions{Name: "ExecuteAgent"},
	)

	// Mock synthesis - SHOULD be called for multiple results
	synthesisCallCount := 0
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.SynthesisInput) (activities.SynthesisResult, error) {
			synthesisCallCount++
			return activities.SynthesisResult{
				FinalResult: "Combined result",
				TokensUsed:  30,
			}, nil
		},
		activity.RegisterOptions{Name: "SynthesizeResultsLLM"},
	)

	// Mock fallback synthesis
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.SynthesisInput) (activities.SynthesisResult, error) {
			return activities.SynthesisResult{
				FinalResult: "Fallback combined",
				TokensUsed:  5,
			}, nil
		},
		activity.RegisterOptions{Name: "SynthesizeResults"},
	)

	// Mock UpdateSessionResult
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.SessionUpdateInput) (activities.SessionUpdateResult, error) {
			return activities.SessionUpdateResult{Success: true}, nil
		},
		activity.RegisterOptions{Name: "UpdateSessionResult"},
	)

	// Mock RecordQuery
	env.RegisterActivityWithOptions(
		func(ctx context.Context, input activities.RecordQueryInput) (activities.RecordQueryResult, error) {
			return activities.RecordQueryResult{}, nil
		},
		activity.RegisterOptions{Name: "RecordQuery"},
	)

	input := TaskInput{
		Query:              "Complex query",
		UserID:            "test-user",
		SessionID:         "test-session",
		BypassSingleResult: true, // Bypass enabled, but should not apply to multiple results
	}

	env.ExecuteWorkflow(AgentDAGWorkflow, input)

	assert.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())

	var result TaskResult
	assert.NoError(t, env.GetWorkflowResult(&result))

	// Verify synthesis was called
	assert.Equal(t, 1, synthesisCallCount, "Synthesis should be called for multiple results")
	assert.Equal(t, "Combined result", result.Result)
	assert.True(t, result.Success)
	// Total tokens: 2 agents * 15 + synthesis 30 = 60
	assert.Equal(t, 60, result.TokensUsed)
}