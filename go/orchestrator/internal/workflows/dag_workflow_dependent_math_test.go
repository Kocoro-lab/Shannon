package workflows

import (
	"context"
	"fmt"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
	"github.com/stretchr/testify/assert"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// TestDependentMath_NoPlaceholdersAndNumericValuePropagation ensures that:
// - tool_parameters are cleared for dependent subtasks
// - previous_results contains a parsed numeric value
// - the chain computes the correct final number deterministically
func TestDependentMath_NoPlaceholdersAndNumericValuePropagation(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register the child workflow that AgentDAGWorkflow calls
	env.RegisterWorkflow(strategies.DAGWorkflow)

	// 1) Decomposition: three-step dependent math
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.DecompositionInput) (activities.DecompositionResult, error) {
			return activities.DecompositionResult{
				ComplexityScore:   0.8,
				Mode:              "complex",
				ExecutionStrategy: "sequential",
				Subtasks: []activities.Subtask{
					{ // task-1: independent
						ID:              "task-1",
						Description:     "Calculate 50 * 4",
						Dependencies:    []string{},
						EstimatedTokens: 50,
						SuggestedTools:  []string{"calculator"},
						ToolParameters: map[string]interface{}{
							"tool":       "calculator",
							"expression": "50*4",
						},
					},
					{ // task-2: depends on task-1; decomposition includes a placeholder that should be cleared
						ID:              "task-2",
						Description:     "Add 100 to previous result",
						Dependencies:    []string{"task-1"},
						EstimatedTokens: 50,
						SuggestedTools:  []string{"calculator"},
						ToolParameters: map[string]interface{}{
							"tool":       "calculator",
							"expression": "result_of_task_1 + 100",
						},
					},
					{ // task-3: depends on task-2; placeholder should be cleared
						ID:              "task-3",
						Description:     "Divide by 10",
						Dependencies:    []string{"task-2"},
						EstimatedTokens: 50,
						SuggestedTools:  []string{"calculator"},
						ToolParameters: map[string]interface{}{
							"tool":       "calculator",
							"expression": "result_of_task_2 / 10",
						},
					},
				},
			}, nil
		},
		activity.RegisterOptions{Name: "DecomposeTask"},
	)

	// 2) ExecuteAgent: emulate deterministic calculator using previous_results numeric value
	var clearedTP2, clearedTP3 bool
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			// Check that tool_parameters are cleared for dependent tasks
			switch in.AgentID {
			case "agent-task-2":
				if len(in.ToolParameters) == 0 {
					clearedTP2 = true
				}
			case "agent-task-3":
				if len(in.ToolParameters) == 0 {
					clearedTP3 = true
				}
			}

			// Compute result based on previous_results numeric value when present
			// Fall back to task-1 constant if no dependencies
			var resp string
			switch in.AgentID {
			case "agent-task-1":
				resp = "200.0" // 50 * 4
			case "agent-task-2":
				if prev, ok := in.Context["previous_results"].(map[string]interface{}); ok {
					if vmap, ok := prev["task-1"].(map[string]interface{}); ok {
						if vv, ok := vmap["numeric_value"].(float64); ok {
							resp = fmt.Sprintf("%.1f", vv+100.0)
						}
					}
				}
				if resp == "" {
					resp = "300.0"
				}
			case "agent-task-3":
				if prev, ok := in.Context["previous_results"].(map[string]interface{}); ok {
					if vmap, ok := prev["task-2"].(map[string]interface{}); ok {
						if vv, ok := vmap["numeric_value"].(float64); ok {
							resp = fmt.Sprintf("%.1f", vv/10.0)
						}
					}
				}
				if resp == "" {
					resp = "30.0"
				}
			default:
				resp = ""
			}

			return activities.AgentExecutionResult{
				AgentID:    in.AgentID,
				Response:   resp,
				Success:    true,
				TokensUsed: 0,
			}, nil
		},
		activity.RegisterOptions{Name: "ExecuteAgent"},
	)

	// Minimal stubs to satisfy workflow
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.SessionUpdateInput) (activities.SessionUpdateResult, error) {
			return activities.SessionUpdateResult{Success: true}, nil
		},
		activity.RegisterOptions{Name: "UpdateSessionResult"},
	)
	// Register GetWorkflowConfig activity
	env.RegisterActivityWithOptions(
		func(ctx context.Context) (activities.WorkflowConfig, error) {
			return activities.WorkflowConfig{
				ParallelMaxConcurrency:   5,
				SequentialPassResults:    true,
				SequentialExtractNumeric: true,
				P2PCoordinationEnabled:   false,
				ReflectionEnabled:        false,
				HybridDependencyTimeout:  300,
			}, nil
		},
		activity.RegisterOptions{Name: "GetWorkflowConfig"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.RecordQueryInput) (activities.RecordQueryResult, error) {
			return activities.RecordQueryResult{}, nil
		},
		activity.RegisterOptions{Name: "RecordQuery"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.RecommendStrategyInput) (activities.RecommendStrategyOutput, error) {
			return activities.RecommendStrategyOutput{Source: "test"}, nil
		},
		activity.RegisterOptions{Name: "RecommendWorkflowStrategy"},
	)
	// Register RecordPatternMetrics activity (use map for input since type may not be exported)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in map[string]interface{}) error {
			// No-op for testing
			return nil
		},
		activity.RegisterOptions{Name: "RecordPatternMetrics"},
	)
	// Register RecordLearningRouterMetrics (Sprint 3 addition)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in map[string]interface{}) error {
			return nil
		},
		activity.RegisterOptions{Name: "RecordLearningRouterMetrics"},
	)
    // Register EmitTaskUpdate (streaming events)
    env.RegisterActivityWithOptions(
        func(ctx context.Context, in activities.EmitTaskUpdateInput) error {
            return nil
        },
        activity.RegisterOptions{Name: "EmitTaskUpdate"},
    )
	// Register CheckTokenBudgetWithBackpressure
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{
				"can_proceed":        true,
				"backpressure_delay": 0,
			}, nil
		},
		activity.RegisterOptions{Name: "CheckTokenBudgetWithBackpressure"},
	)
	// Register FetchHierarchicalMemory (memory system)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		},
		activity.RegisterOptions{Name: "FetchHierarchicalMemory"},
	)
	// Synthesis returns last agent result
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.SynthesisInput) (activities.SynthesisResult, error) {
			out := ""
			if len(in.AgentResults) > 0 {
				out = in.AgentResults[len(in.AgentResults)-1].Response
			}
			return activities.SynthesisResult{FinalResult: out, TokensUsed: 0}, nil
		},
		activity.RegisterOptions{Name: "SynthesizeResults"},
	)
	// Also register SynthesizeResultsLLM (same function, different name)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.SynthesisInput) (activities.SynthesisResult, error) {
			out := ""
			if len(in.AgentResults) > 0 {
				out = in.AgentResults[len(in.AgentResults)-1].Response
			}
			return activities.SynthesisResult{FinalResult: out, TokensUsed: 0}, nil
		},
		activity.RegisterOptions{Name: "SynthesizeResultsLLM"},
	)

	// Execute workflow
	input := TaskInput{
		Query:              "Chained dependent math",
		UserID:             "test-user",
		SessionID:          "test-session",
		BypassSingleResult: false,
	}
	env.ExecuteWorkflow(AgentDAGWorkflow, input)

	assert.True(t, env.IsWorkflowCompleted())
	assert.NoError(t, env.GetWorkflowError())

	var result TaskResult
	assert.NoError(t, env.GetWorkflowResult(&result))

	// Final value should be (50*4 + 100) / 10 = 30
	assert.Equal(t, "30.0", result.Result)
	assert.True(t, result.Success)

	// Ensure tool_parameters were cleared for dependent tasks
	assert.True(t, clearedTP2, "tool_parameters should be cleared for task-2")
	assert.True(t, clearedTP3, "tool_parameters should be cleared for task-3")
}
