package workflows

import (
	"context"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/budget"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// Stub for GetWorkflowConfig
func getWorkflowConfigStub(ctx context.Context) (activities.WorkflowConfig, error) {
	return activities.WorkflowConfig{}, nil
}

// Stub for budget check
func checkTokenBudgetStub(ctx context.Context, in activities.BudgetCheckInput) (*budget.BackpressureResult, error) {
	_ = ctx
	_ = in
	return &budget.BackpressureResult{
		BudgetCheckResult: &budget.BudgetCheckResult{
			CanProceed: true,
		},
	}, nil
}

// Stub for decomposition to force cognitive strategy
func decomposeReactStub(ctx context.Context, in activities.DecompositionInput) (activities.DecompositionResult, error) {
	_ = ctx
	_ = in
	return activities.DecompositionResult{ComplexityScore: 0.5, Mode: "standard", CognitiveStrategy: "react"}, nil
}

func TestOrchestratorRoutesToReactWhenStrategySpecified(t *testing.T) {
    suite := &testsuite.WorkflowTestSuite{}
    env := suite.NewTestWorkflowEnvironment()

    // Register activities used by the router
	env.RegisterActivityWithOptions(getWorkflowConfigStub, activity.RegisterOptions{Name: "GetWorkflowConfig"})
	env.RegisterActivityWithOptions(checkTokenBudgetStub, activity.RegisterOptions{Name: "CheckTokenBudgetWithBackpressure"})
	env.RegisterActivityWithOptions(decomposeReactStub, activity.RegisterOptions{Name: "DecomposeTask"})

	// Register the real child workflow; we'll stub its activities
	env.RegisterWorkflow(strategies.ReactWorkflow)

	// Stubs required by ReactWorkflow
	env.RegisterActivityWithOptions(func(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
		return activities.AgentExecutionResult{Response: "react-ok", Success: true, TokensUsed: 1}, nil
	}, activity.RegisterOptions{Name: "ExecuteAgent"})
	env.RegisterActivityWithOptions(func(ctx context.Context, in activities.SessionUpdateInput) (activities.SessionUpdateResult, error) {
		return activities.SessionUpdateResult{Success: true}, nil
	}, activity.RegisterOptions{Name: "UpdateSessionResult"})
	env.RegisterActivityWithOptions(func(ctx context.Context, in activities.RecordQueryInput) (activities.RecordQueryResult, error) {
		return activities.RecordQueryResult{}, nil
	}, activity.RegisterOptions{Name: "RecordQuery"})
    env.RegisterActivityWithOptions(func(ctx context.Context, in activities.PatternMetricsInput) error { return nil }, activity.RegisterOptions{Name: "RecordPatternMetrics"})

    // Register stub for streaming events to avoid ActivityNotRegisteredError
    env.RegisterActivityWithOptions(func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil }, activity.RegisterOptions{Name: "EmitTaskUpdate"})

	input := TaskInput{Query: "q", UserID: "u", SessionID: "s"}
	env.ExecuteWorkflow(OrchestratorWorkflow, input)

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("router workflow failed: %v", env.GetWorkflowError())
	}
	var out TaskResult
	_ = env.GetWorkflowResult(&out)
	if !out.Success || out.Result != "react-ok" {
		t.Fatalf("unexpected router result: %+v", out)
	}
}
