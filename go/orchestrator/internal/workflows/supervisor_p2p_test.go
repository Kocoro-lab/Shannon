package workflows

import (
	"context"
	"testing"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// TestSupervisorWorkflowP2PDisabled tests that P2P coordination is skipped when disabled
func TestSupervisorWorkflowP2PDisabled(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	// Mock activities
	env.OnActivity("EmitTaskUpdate", mock.Anything, mock.Anything).Return(nil)
	env.OnActivity("FetchSupervisorMemory", mock.Anything, mock.Anything).Return(
		nil, // Return nil memory to skip enhanced features
		nil,
	)

	// Mock GetWorkflowConfig to disable P2P
	env.OnActivity(activities.GetWorkflowConfig, mock.Anything).Return(
		&activities.WorkflowConfig{
			P2PCoordinationEnabled: false, // P2P disabled
			P2PTimeoutSeconds:      360,
		},
		nil,
	)

	// Mock decomposition with P2P dependencies
	env.OnActivity(constants.DecomposeTaskActivity, mock.Anything, mock.Anything).Return(
		activities.DecompositionResult{
			Mode:             "complex",
			ComplexityScore:  0.6,
			ExecutionStrategy: "sequential",
			Subtasks: []activities.Subtask{
				{
					ID:          "task1",
					Description: "First task",
					Produces:    []string{"data1"},
				},
				{
					ID:          "task2",
					Description: "Second task that needs data1",
					Consumes:    []string{"data1"}, // Has dependency but P2P is disabled
					Dependencies: []string{"task1"},
				},
			},
			AgentTypes: []string{"analyst", "processor"},
		},
		nil,
	)

	// Mock agent executions - these should be called WITHOUT waiting for P2P
	callCount := 0
	env.OnActivity(activities.ExecuteAgent, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, input activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			callCount++
			return activities.AgentExecutionResult{
				AgentID:    input.AgentID,
				Response:   "Task completed",
				Success:    true,
				TokensUsed: 100,
			}, nil
		},
	)

	// Mock synthesis
	env.OnActivity(activities.SynthesizeResultsLLM, mock.Anything, mock.Anything).Return(
		activities.SynthesisResult{
			FinalResult: "All tasks completed successfully",
			TokensUsed:  50,
		},
		nil,
	)

	// Mock session update
	env.OnActivity(constants.UpdateSessionResultActivity, mock.Anything, mock.Anything).Return(
		activities.SessionUpdateResult{},
		nil,
	)

	// Mock persistence
	env.OnActivity(activities.PersistAgentExecutionStandalone, mock.Anything, mock.Anything).Return(nil)

	// Execute workflow
	env.ExecuteWorkflow(SupervisorWorkflow, TaskInput{
		Query:     "Complex task with dependencies",
		UserID:    "test-user",
		SessionID: "test-session",
		Mode:      "complex",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Success)
	require.Equal(t, 2, callCount, "Both agents should execute without P2P waiting")

	// Verify workflow completed successfully
	// Note: TestWorkflowEnvironment doesn't provide timing info, but in real runs
	// this would complete quickly without P2P waits
}

// TestSupervisorWorkflowP2PEnabled tests P2P coordination when enabled
func TestSupervisorWorkflowP2PEnabled(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	// Mock activities
	env.OnActivity("EmitTaskUpdate", mock.Anything, mock.Anything).Return(nil)
	env.OnActivity("FetchSupervisorMemory", mock.Anything, mock.Anything).Return(nil, nil)

	// Mock GetWorkflowConfig to ENABLE P2P
	env.OnActivity(activities.GetWorkflowConfig, mock.Anything).Return(
		&activities.WorkflowConfig{
			P2PCoordinationEnabled: true, // P2P enabled
			P2PTimeoutSeconds:      10,   // Short timeout for testing
		},
		nil,
	)

	// Mock decomposition with P2P dependencies
	env.OnActivity(constants.DecomposeTaskActivity, mock.Anything, mock.Anything).Return(
		activities.DecompositionResult{
			Mode:             "complex",
			ComplexityScore:  0.7,
			ExecutionStrategy: "sequential",
			Subtasks: []activities.Subtask{
				{
					ID:          "producer",
					Description: "Produce data",
					Produces:    []string{"shared_data"},
				},
				{
					ID:          "consumer",
					Description: "Consume data",
					Consumes:    []string{"shared_data"},
				},
			},
			AgentTypes: []string{"producer", "consumer"},
		},
		nil,
	)

	// Track workspace operations
	workspaceAppended := false
	workspaceChecked := false

	// Mock workspace list (checking for dependencies)
	env.OnActivity(constants.WorkspaceListActivity, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, input activities.WorkspaceListInput) ([]activities.WorkspaceEntry, error) {
			workspaceChecked = true
			if input.Topic == "shared_data" && workspaceAppended {
				// Data is available after first task
				return []activities.WorkspaceEntry{
					{Topic: "shared_data", Entry: map[string]interface{}{"value": "test"}},
				}, nil
			}
			// No data yet
			return []activities.WorkspaceEntry{}, nil
		},
	)

	// Mock workspace append (producing data)
	env.OnActivity(constants.WorkspaceAppendActivity, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, input activities.WorkspaceAppendInput) (activities.WorkspaceAppendResult, error) {
			if input.Topic == "shared_data" {
				workspaceAppended = true
			}
			return activities.WorkspaceAppendResult{Seq: 1}, nil
		},
	)

	// Mock agent executions
	env.OnActivity(activities.ExecuteAgent, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, input activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			return activities.AgentExecutionResult{
				AgentID:    input.AgentID,
				Response:   "Task completed",
				Success:    true,
				TokensUsed: 100,
			}, nil
		},
	)

	// Mock synthesis
	env.OnActivity(activities.SynthesizeResultsLLM, mock.Anything, mock.Anything).Return(
		activities.SynthesisResult{
			FinalResult: "P2P coordination successful",
			TokensUsed:  50,
		},
		nil,
	)

	// Mock session update
	env.OnActivity(constants.UpdateSessionResultActivity, mock.Anything, mock.Anything).Return(
		activities.SessionUpdateResult{},
		nil,
	)

	// Mock persistence
	env.OnActivity(activities.PersistAgentExecutionStandalone, mock.Anything, mock.Anything).Return(nil)

	// Execute workflow
	env.ExecuteWorkflow(SupervisorWorkflow, TaskInput{
		Query:     "Task requiring P2P coordination",
		UserID:    "test-user",
		SessionID: "test-session",
		Mode:      "complex",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result TaskResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Success)

	// Verify P2P coordination occurred
	require.True(t, workspaceChecked, "Workspace should be checked for dependencies")
	require.True(t, workspaceAppended, "Producer should append to workspace")
}

// TestSupervisorWorkflowP2PVersionGates tests version gate handling
func TestSupervisorWorkflowP2PVersionGates(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	// Set version for deterministic replay
	env.OnGetVersion("p2p_sync_v1", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	env.OnGetVersion("team_workspace_v1", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	// Mock activities
	env.OnActivity("EmitTaskUpdate", mock.Anything, mock.Anything).Return(nil)
	env.OnActivity("FetchSupervisorMemory", mock.Anything, mock.Anything).Return(nil, nil)

	// P2P enabled but version gates prevent execution
	env.OnActivity(activities.GetWorkflowConfig, mock.Anything).Return(
		&activities.WorkflowConfig{
			P2PCoordinationEnabled: true,
			P2PTimeoutSeconds:      360,
		},
		nil,
	)

	// Mock decomposition with dependencies
	env.OnActivity(constants.DecomposeTaskActivity, mock.Anything, mock.Anything).Return(
		activities.DecompositionResult{
			Mode:            "complex",
			ComplexityScore: 0.6,
			Subtasks: []activities.Subtask{
				{
					ID:          "task1",
					Description: "Task with dependency",
					Consumes:    []string{"data"},
				},
			},
		},
		nil,
	)

	// This should NOT be called due to version gates
	workspaceListCalled := false
	env.OnActivity(constants.WorkspaceListActivity, mock.Anything, mock.Anything).Return(
		func(ctx context.Context, input activities.WorkspaceListInput) ([]activities.WorkspaceEntry, error) {
			workspaceListCalled = true
			return []activities.WorkspaceEntry{}, nil
		},
	)

	// Mock agent execution
	env.OnActivity(activities.ExecuteAgent, mock.Anything, mock.Anything).Return(
		activities.AgentExecutionResult{
			Response:   "Completed",
			Success:    true,
			TokensUsed: 100,
		},
		nil,
	)

	// Mock other required activities
	env.OnActivity(activities.SynthesizeResultsLLM, mock.Anything, mock.Anything).Return(
		activities.SynthesisResult{FinalResult: "Done", TokensUsed: 50},
		nil,
	)
	env.OnActivity(constants.UpdateSessionResultActivity, mock.Anything, mock.Anything).Return(
		activities.SessionUpdateResult{},
		nil,
	)
	env.OnActivity(activities.PersistAgentExecutionStandalone, mock.Anything, mock.Anything).Return(nil)

	// Execute workflow
	env.ExecuteWorkflow(SupervisorWorkflow, TaskInput{
		Query:     "Test with version gates",
		UserID:    "test-user",
		SessionID: "test-session",
		Mode:      "complex",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// P2P should be skipped due to version gates
	require.False(t, workspaceListCalled, "Workspace operations should be skipped with DefaultVersion")
}