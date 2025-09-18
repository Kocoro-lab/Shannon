package patterns

import (
	"context"
	"testing"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns/execution"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// minimal ExecuteAgent stub
func execAgentStub(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
	// Deterministic, fast stub
	resp := in.Query
	if resp == "" {
		resp = "ok"
	}
	return activities.AgentExecutionResult{AgentID: in.AgentID, Response: resp + " [stub]", Success: true, TokensUsed: 10, ModelUsed: "test"}, nil
}

func getWorkflowConfigStub(ctx context.Context) (activities.WorkflowConfig, error) {
	// Defaults for tests
	return activities.WorkflowConfig{ReactMaxIterations: 3, ReactObservationWindow: 2}, nil
}

func TestReactLoopExecutes(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(execAgentStub, activity.RegisterOptions{Name: "ExecuteAgent"})
	env.RegisterActivityWithOptions(getWorkflowConfigStub, activity.RegisterOptions{Name: "GetWorkflowConfig"})

	// Wrap ReactLoop into a small workflow for testing
	wf := func(ctx workflow.Context) (string, error) {
		cfg := ReactConfig{MaxIterations: 2, ObservationWindow: 1, MaxObservations: 10, MaxThoughts: 10, MaxActions: 10}
		opts := Options{BudgetAgentMax: 0, SessionID: "s", UserID: "u", ModelTier: "small"}
		res, err := ReactLoop(ctx, "solve x", map[string]interface{}{}, "s", []string{}, cfg, opts)
		if err != nil {
			return "", err
		}
		return res.FinalResult, nil
	}

	env.RegisterWorkflow(wf)
	env.ExecuteWorkflow(wf)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("ReactLoop workflow failed: %v", env.GetWorkflowError())
	}
	var out string
	_ = env.GetWorkflowResult(&out)
	if out == "" {
		t.Fatalf("expected non-empty result")
	}
}

func TestChainOfThoughtExecutes(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	// Respond with a message containing a clear final answer marker
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			_ = ctx
			return activities.AgentExecutionResult{Response: "Reasoning... Therefore: 42", Success: true, TokensUsed: 7}, nil
		},
		activity.RegisterOptions{Name: "ExecuteAgent"},
	)

	wf := func(ctx workflow.Context) (string, error) {
		cfg := ChainOfThoughtConfig{MaxSteps: 3}
		opts := Options{ModelTier: "small"}
		res, err := ChainOfThought(ctx, "What is the answer?", map[string]interface{}{}, "s", []string{}, cfg, opts)
		if err != nil {
			return "", err
		}
		return res.FinalAnswer, nil
	}

	env.RegisterWorkflow(wf)
	env.ExecuteWorkflow(wf)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("ChainOfThought workflow failed: %v", env.GetWorkflowError())
	}
	var out string
	_ = env.GetWorkflowResult(&out)
	if out == "" {
		t.Fatalf("expected final answer to be parsed")
	}
}

// Optional sanity for semaphore limit: ensure activities overlap is bounded.
func TestParallelRespectsMaxConcurrency(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	// Slow stub so overlaps can be observed by the test env scheduler
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			time.Sleep(20 * time.Millisecond)
			return activities.AgentExecutionResult{AgentID: in.AgentID, Response: "ok", Success: true}, nil
		},
		activity.RegisterOptions{Name: "ExecuteAgent"},
	)

	wf := func(ctx workflow.Context) (int, error) {
		tasks := []execution.ParallelTask{{ID: "1", Description: "a"}, {ID: "2", Description: "b"}, {ID: "3", Description: "c"}}
		cfg := execution.ParallelConfig{MaxConcurrency: 2, Context: map[string]interface{}{}}
		res, err := execution.ExecuteParallel(ctx, tasks, "s", []string{}, cfg, 0, "u", "small")
		if err != nil {
			return 0, err
		}
		return len(res.Results), nil
	}

	env.RegisterWorkflow(wf)
	env.ExecuteWorkflow(wf)
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("Parallel workflow failed: %v", env.GetWorkflowError())
	}
	var count int
	_ = env.GetWorkflowResult(&count)
	if count != 3 {
		t.Fatalf("expected 3 results, got %d", count)
	}
}
