package workflows

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// TestCollaborativeActionsDoNotTriggerConvergence verifies that send_message,
// publish_data, and request_help do NOT increment consecutiveNonToolActions.
// Before the fix, 3 consecutive collaborative actions incorrectly triggered
// convergence detection and terminated the agent.
func TestCollaborativeActionsDoNotTriggerConvergence(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	// Iteration counter to drive mock responses
	iteration := 0

	// Mock AgentLoopStep: return 4 send_message actions, then done
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			defer func() { iteration++ }()
			if iteration < 4 {
				return activities.AgentLoopStepResult{
					Action:      "send_message",
					To:          "teammate-1",
					MessageType: "info",
					Payload:     map[string]interface{}{"message": "sharing findings"},
					TokensUsed:  100,
				}, nil
			}
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "completed after collaboration",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	// Stub P2P activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.SendAgentMessageInput) error { return nil },
		activity.RegisterOptions{Name: constants.SendAgentMessageActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "test task",
		MaxIterations: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	assert.Equal(t, "completed after collaboration", result.Response)
	// Agent ran all 5 iterations (4 send_message + 1 done), NOT stopped at iteration 3
	assert.Equal(t, 5, result.Iterations)
}

// TestEmptyActionsDoTriggerConvergence verifies that truly empty/no-op
// actions still correctly trigger convergence after 3 consecutive occurrences.
func TestEmptyActionsDoTriggerConvergence(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	iteration := 0

	// Mock AgentLoopStep: return 3 empty actions (should trigger convergence)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			defer func() { iteration++ }()
			return activities.AgentLoopStepResult{
				Action:    "", // empty action
				TokensUsed: 50,
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	// Stub activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "test task",
		MaxIterations: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	// Should converge at iteration 3 (0-indexed: after iterations 0, 1, 2)
	assert.Equal(t, 3, result.Iterations)
}

// TestPublishDataDoesNotTriggerConvergence verifies publish_data is treated
// as collaborative progress, not idle behavior.
func TestPublishDataDoesNotTriggerConvergence(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	iteration := 0

	// 3x publish_data + 1x done
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			defer func() { iteration++ }()
			if iteration < 3 {
				return activities.AgentLoopStepResult{
					Action:    "publish_data",
					Topic:     "findings",
					Data:      "important discovery",
					TokensUsed: 80,
				}, nil
			}
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "finished publishing",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	// Stub activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.SendAgentMessageInput) error { return nil },
		activity.RegisterOptions{Name: constants.SendAgentMessageActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceAppendInput) error { return nil },
		activity.RegisterOptions{Name: constants.WorkspaceAppendActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "test task",
		MaxIterations: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	assert.Equal(t, "finished publishing", result.Response)
	// 3 publish_data + 1 done = 4 iterations (not stopped at 3)
	assert.Equal(t, 4, result.Iterations)
}

// TestMailboxSinceSeqIncrementsPerIteration verifies that FetchAgentMessages
// is called with increasing SinceSeq values so messages aren't re-read.
func TestMailboxSinceSeqIncrementsPerIteration(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	iteration := 0

	// Track SinceSeq values passed to FetchAgentMessages
	var mu sync.Mutex
	sinceSeqValues := []uint64{}

	// Mock FetchAgentMessages — returns messages with increasing Seq numbers
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			mu.Lock()
			sinceSeqValues = append(sinceSeqValues, in.SinceSeq)
			mu.Unlock()

			// Return a message with Seq = (call_count * 10) so we can track progression
			callNum := len(sinceSeqValues)
			return []activities.AgentMessage{
				{
					Seq:     uint64(callNum * 10),
					From:    "teammate",
					To:      "test-agent",
					Type:    "info",
					Payload: map[string]interface{}{"message": "hello"},
				},
			}, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)

	// Mock AgentLoopStep: 2 tool_calls then done (3 iterations → 3 fetch calls)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			defer func() { iteration++ }()
			if iteration < 2 {
				return activities.AgentLoopStepResult{
					Action:     "tool_call",
					Tool:       "web_search",
					ToolParams: map[string]interface{}{"query": "test"},
					TokensUsed: 100,
				}, nil
			}
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "done",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	// Stub remaining activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			return activities.AgentExecutionResult{Response: "search result"}, nil
		},
		activity.RegisterOptions{Name: "ExecuteAgent"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "test task",
		MaxIterations: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// Verify SinceSeq progression
	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(sinceSeqValues), 3, "should have at least 3 fetch calls")
	// First call should start at 0
	assert.Equal(t, uint64(0), sinceSeqValues[0], "first fetch should have SinceSeq=0")
	// Second call should have SinceSeq=10 (from first returned message)
	assert.Equal(t, uint64(10), sinceSeqValues[1], "second fetch should have SinceSeq=10")
	// Third call should have SinceSeq=20
	assert.Equal(t, uint64(20), sinceSeqValues[2], "third fetch should have SinceSeq=20")
}

// TestIdleSignalCarriesResultSummary verifies that when a swarm agent does
// done→idle, the savedDoneResponse is preserved and returned on shutdown.
// This proves the idle signal pipeline carries result_summary to the parent.
func TestIdleSignalCarriesResultSummary(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	iteration := 0

	// Mock LLM: first iteration returns "done" with a response
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			defer func() { iteration++ }()
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "Found 3 key findings about quantum computing",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	// Stub activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.GetTaskListInput) ([]activities.SwarmTask, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.GetTaskListActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	// After agent goes idle, send shutdown signal
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("agent:test-agent:shutdown", "Lead completed")
	}, 1*time.Minute)

	// Run with team roster (swarm mode) so "done" converts to "idle"
	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "Research quantum computing",
		MaxIterations: 10,
		TeamRoster: []TeamMember{
			{AgentID: "test-agent", Task: "Research quantum computing"},
			{AgentID: "other-agent", Task: "Analyze results"},
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	// The savedDoneResponse should be preserved through idle→shutdown
	assert.Equal(t, "Found 3 key findings about quantum computing", result.Response)
}

// TestSavedDoneResponseClearedOnReassignment verifies that when an agent
// gets a new task after being idle, the old savedDoneResponse is cleared.
// This prevents stale results from being returned on subsequent shutdown.
func TestSavedDoneResponseClearedOnReassignment(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	var mu sync.Mutex
	callCount := 0

	// Mock LLM: track call count across reassignments
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			mu.Lock()
			current := callCount
			callCount++
			mu.Unlock()

			if current == 0 {
				// First task: "done" with first response
				return activities.AgentLoopStepResult{
					Action:   "done",
					Response: "STALE: first task result",
				}, nil
			}
			// Second task: "done" with second response
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "FRESH: second task result",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	// Stub activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.GetTaskListInput) ([]activities.SwarmTask, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.GetTaskListActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	// After first idle: assign new task
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("agent:test-agent:new-task", map[string]string{
			"description": "Analyze the quantum computing findings",
		})
	}, 1*time.Minute)

	// After second idle: shutdown
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("agent:test-agent:shutdown", "Lead completed")
	}, 3*time.Minute)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "Research quantum computing",
		MaxIterations: 10,
		TeamRoster: []TeamMember{
			{AgentID: "test-agent", Task: "Research quantum computing"},
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	// Must have the SECOND response, not the stale first one
	assert.Equal(t, "FRESH: second task result", result.Response)
	assert.NotContains(t, result.Response, "STALE")
}

// TestAgentRolePropagatedToResult verifies that AgentLoopResult carries the
// Role from AgentLoopInput through all return paths. This ensures the role
// flows through to AgentExecutionResult during synthesis conversion.
func TestAgentRolePropagatedToResult(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "analysis complete",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	// Stub activities
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "Analyze dataset",
		MaxIterations: 5,
		Role:          "analyst",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	assert.Equal(t, "analyst", result.Role, "Role should propagate from input to result")
	assert.Equal(t, "analysis complete", result.Response)
}

// TestAgentRolePropagatedOnIdleShutdown verifies Role propagates through the
// idle→shutdown path (swarm mode where "done" converts to "idle").
func TestAgentRolePropagatedOnIdleShutdown(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "research complete",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.GetTaskListInput) ([]activities.SwarmTask, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.GetTaskListActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	// After agent goes idle, send shutdown
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow("agent:test-agent:shutdown", "Lead completed")
	}, 1*time.Minute)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "Research topic",
		MaxIterations: 10,
		Role:          "researcher",
		TeamRoster: []TeamMember{
			{AgentID: "test-agent", Task: "Research topic"},
		},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	assert.Equal(t, "researcher", result.Role, "Role should propagate through idle→shutdown path")
}

func TestAgentLoop_PassesModelTierToStep(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	env.RegisterWorkflow(AgentLoop)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			assert.Equal(t, "small", in.ModelTier)
			return activities.AgentLoopStepResult{
				Action:   "done",
				Response: "ok",
			}, nil
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "test task",
		MaxIterations: 3,
		Context:       map[string]interface{}{"model_tier": "small"},
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.True(t, result.Success)
	assert.Equal(t, "ok", result.Response)
}

func TestAgentLoopReportsIterationsOnFailure(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(AgentLoop)

	iteration := 0
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			defer func() { iteration++ }()
			if iteration < 3 {
				return activities.AgentLoopStepResult{
					Action:     "tool_call",
					Tool:       "web_search",
					ToolParams: map[string]interface{}{"query": "test"},
					TokensUsed: 100,
				}, nil
			}
			return activities.AgentLoopStepResult{}, fmt.Errorf("context deadline exceeded")
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			return activities.AgentExecutionResult{Response: "search results"}, nil
		},
		activity.RegisterOptions{Name: "ExecuteAgent"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.GetTaskListInput) ([]activities.SwarmTask, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.GetTaskListActivity},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "test task",
		MaxIterations: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.False(t, result.Success)
	assert.Equal(t, 3, result.Iterations)
	assert.Contains(t, result.Error, "context deadline exceeded")
}

func TestErrorPropagationInResultSummary(t *testing.T) {
	// Simulate what the completionCh handler does
	buildResultSummary := func(response, errMsg string) string {
		resultSummary := truncateDecisionSummary(response)
		if resultSummary == "" && errMsg != "" {
			resultSummary = fmt.Sprintf("Agent failed: %s", truncateDecisionSummary(errMsg))
		}
		return resultSummary
	}

	// Case 1: normal success — response is used
	assert.Equal(t, "research complete", buildResultSummary("research complete", ""))

	// Case 2: failure with error — error is surfaced
	summary := buildResultSummary("", "LLM step failed at iteration 3: context deadline exceeded")
	assert.Contains(t, summary, "Agent failed:")
	assert.Contains(t, summary, "context deadline exceeded")

	// Case 3: failure with both response and error — response wins
	assert.Equal(t, "partial results", buildResultSummary("partial results", "timeout"))
}

func TestLooksLikeToolJSON(t *testing.T) {
	// looksLikeToolJSON is deliberately narrow — only catches file_write blobs
	// and raw action JSON, not arbitrary JSON which may contain valid structured results.
	longContent := strings.Repeat("x", 501)
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{"empty", "", false},
		{"clean text", "Key findings: AWS t3.xlarge costs $0.1664/hr on-demand", false},
		{"markdown report", "# Analysis\n## Pricing\nAWS is cheaper for compute-optimized workloads.", false},
		// Deliberately allowed — small JSON objects are valid structured results
		{"small JSON object", `{"tool": "file_write", "file_path": "report.md", "content": "..."}`, false},
		{"JSON array", `[{"name": "t3.xlarge", "price": 0.1664}]`, false},
		// Prose with embedded quotes — not raw tool JSON
		{"embedded tool marker", `Agent wrote file: "file_path": "/findings/report.md"`, false},
		{"web search results", `Results from "search_results": [{"title": "AWS Pricing"}]`, false},
		{"tool_call marker", `The agent used "tool_call": "web_search" to find data`, false},
		{"function marker", `Calling "function": "calculate_cost" with params`, false},
		{"result_type marker", `Got "result_type": "search" from tool execution`, false},
		{"small JSON", `  { "key": "value" }  `, false},
		// Actual file_write blob (path + content + >500 chars) — should be caught
		{"file_write blob", `{"path": "report.md", "content": "` + longContent + `"}`, true},
		// Raw action JSON leaked from agent — should be caught
		{"raw action JSON", `{"action": "tool_call", "tool": "web_search", "tool_params": {"query": "test"}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, looksLikeToolJSON(tt.input), "input: %q", tt.input)
		})
	}
}

func TestSpawnAgentLeadActionFieldDefaults(t *testing.T) {
	// Verify LeadAction zero-value defaults for spawn_agent.
	// Regression guard: if fields are renamed or removed, this fails at compile time.
	action := activities.LeadAction{
		Type: "spawn_agent",
	}
	assert.Equal(t, "spawn_agent", action.Type)
	assert.Empty(t, action.TaskID, "TaskID should default to empty")
	assert.Empty(t, action.Role, "Role should default to empty")
	assert.Empty(t, action.TaskDescription, "TaskDescription should default to empty")
	assert.Empty(t, action.ModelTier, "ModelTier should default to empty")
}

func TestAgentLoopReturnsFallbackResponseOnFailure(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(AgentLoop)

	iteration := 0
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentLoopStepInput) (activities.AgentLoopStepResult, error) {
			defer func() { iteration++ }()
			if iteration < 2 {
				return activities.AgentLoopStepResult{
					Action:          "tool_call",
					Tool:            "web_search",
					ToolParams:      map[string]interface{}{"query": "shoes"},
					TokensUsed:      100,
					DecisionSummary: "Searching for Japanese shoe market data",
				}, nil
			}
			return activities.AgentLoopStepResult{}, fmt.Errorf("context deadline exceeded")
		},
		activity.RegisterOptions{Name: constants.AgentLoopStepActivity},
	)

	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.AgentExecutionInput) (activities.AgentExecutionResult, error) {
			return activities.AgentExecutionResult{Response: "results"}, nil
		},
		activity.RegisterOptions{Name: "ExecuteAgent"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.FetchAgentMessagesInput) ([]activities.AgentMessage, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.WorkspaceListAllInput) ([]activities.WorkspaceEntry, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.WorkspaceListAllActivity},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.EmitTaskUpdateInput) error { return nil },
		activity.RegisterOptions{Name: "EmitTaskUpdate"},
	)
	env.RegisterActivityWithOptions(
		func(ctx context.Context, in activities.GetTaskListInput) ([]activities.SwarmTask, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: constants.GetTaskListActivity},
	)

	env.ExecuteWorkflow(AgentLoop, AgentLoopInput{
		AgentID:       "test-agent",
		WorkflowID:    "test-wf",
		Task:          "test task",
		MaxIterations: 10,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result AgentLoopResult
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.False(t, result.Success)
	assert.Equal(t, 2, result.Iterations)
	assert.Contains(t, result.Response, "Searching for Japanese shoe market data")
}

func TestAgentTaskMapTracking(t *testing.T) {
	agentTaskMap := make(map[string]string)

	// Spawn assigns task
	agentTaskMap["Shimokita"] = "T1"
	assert.Equal(t, "T1", agentTaskMap["Shimokita"])

	// Reassign overwrites
	agentTaskMap["Shimokita"] = "T3"
	assert.Equal(t, "T3", agentTaskMap["Shimokita"])

	// Completion cleans up
	delete(agentTaskMap, "Shimokita")
	_, exists := agentTaskMap["Shimokita"]
	assert.False(t, exists)
}

func TestReassignmentMaxIterations(t *testing.T) {
	// B1: With maxReassignments=2 and maxTotalIterations=80,
	// the maximum iterations after 2 reassignments should be 80 (50 + 15 + 15).
	input := AgentLoopInput{
		MaxIterations: 50,
	}

	const maxReassignments = 2
	const iterationsPerReassign = 15
	const maxTotalIterations = 80

	for i := 0; i < maxReassignments; i++ {
		input.ReassignCount++
		newMax := input.MaxIterations + iterationsPerReassign
		if newMax > maxTotalIterations {
			newMax = maxTotalIterations
		}
		input.MaxIterations = newMax
	}

	assert.Equal(t, 80, input.MaxIterations, "After 2 reassignments: 50 + 15 + 15 = 80")
	assert.Equal(t, 2, input.ReassignCount)

	// 3rd reassignment should be rejected
	input.ReassignCount++
	assert.Greater(t, input.ReassignCount, maxReassignments, "3rd reassignment exceeds limit")
}

func TestHasAssignablePendingTask(t *testing.T) {
	// B2: Only unblocked pending tasks should count as "assignable"
	tests := []struct {
		name     string
		tasks    []activities.SwarmTask
		expected bool
	}{
		{
			name: "no pending tasks",
			tasks: []activities.SwarmTask{
				{ID: "T1", Status: "completed"},
				{ID: "T2", Status: "running"},
			},
			expected: false,
		},
		{
			name: "unblocked pending task",
			tasks: []activities.SwarmTask{
				{ID: "T1", Status: "completed"},
				{ID: "T2", Status: "pending"}, // No depends_on — assignable
			},
			expected: true,
		},
		{
			name: "only blocked pending tasks",
			tasks: []activities.SwarmTask{
				{ID: "T1", Status: "running"},
				{ID: "T2", Status: "pending", DependsOn: []string{"T1"}}, // Blocked by T1
			},
			expected: false,
		},
		{
			name: "mix of blocked and unblocked",
			tasks: []activities.SwarmTask{
				{ID: "T1", Status: "completed"},
				{ID: "T2", Status: "pending", DependsOn: []string{"T1"}}, // Unblocked (T1 completed)
				{ID: "T3", Status: "pending", DependsOn: []string{"T4"}}, // Blocked (T4 not complete)
				{ID: "T4", Status: "running"},
			},
			expected: true, // T2 is assignable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAssignablePendingTask(tt.tasks)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		a, b     string
		expected float64
		delta    float64
	}{
		{"hello world", "hello world", 1.0, 0.01},
		{"React performance benchmark", "React benchmark performance", 1.0, 0.01}, // Same words, different order
		{"abc def ghi", "jkl mno pqr", 0.0, 0.01}, // No overlap
		{"React Server Components performance", "Astro Islands architecture", 0.0, 0.15},
		{"Weaviate benchmark QPS latency", "Weaviate benchmark latency QPS results", 0.67, 0.15},
	}

	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			result := jaccardSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, tt.delta)
		})
	}
}

func TestIsSearchSaturated(t *testing.T) {
	tests := []struct {
		name     string
		queries  []string
		expected bool
	}{
		{
			name:     "identical queries",
			queries:  []string{"React SC performance", "React SC performance", "React SC performance"},
			expected: true,
		},
		{
			name:     "near-identical word reorder",
			queries:  []string{"Weaviate benchmark QPS latency", "Weaviate QPS latency benchmark", "benchmark Weaviate QPS latency"},
			expected: true,
		},
		{
			name:     "diverse queries",
			queries:  []string{"React Server Components", "Astro Islands architecture", "Qwik resumability"},
			expected: false,
		},
		{
			name:     "too few queries",
			queries:  []string{"React SC", "React SC"},
			expected: false,
		},
		{
			name:     "saturated at end of longer list",
			queries:  []string{"diverse first", "another topic", "React Server Components performance benchmark", "React Server Components benchmark performance", "React Server Components performance benchmark results"},
			expected: true, // Last 3 share high word overlap (>0.7 Jaccard)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSearchSaturated(tt.queries, 3, 0.7)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertHistoryForLead verifies session history truncation and formatting
// for Lead multi-turn context injection.
func TestConvertHistoryForLead(t *testing.T) {
	tests := []struct {
		name            string
		messages        []Message
		expectedCount   int
		expectTruncated bool // any assistant message truncated
	}{
		{
			name:          "empty history",
			messages:      nil,
			expectedCount: 0,
		},
		{
			name: "within limit",
			messages: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
			expectedCount: 2,
		},
		{
			name: "exceeds maxMessages — keeps last 6",
			messages: []Message{
				{Role: "user", Content: "msg1"},
				{Role: "assistant", Content: "resp1"},
				{Role: "user", Content: "msg2"},
				{Role: "assistant", Content: "resp2"},
				{Role: "user", Content: "msg3"},
				{Role: "assistant", Content: "resp3"},
				{Role: "user", Content: "msg4"},
				{Role: "assistant", Content: "resp4"},
			},
			expectedCount: 6, // last 6 of 8
		},
		{
			name: "long assistant response truncated",
			messages: []Message{
				{Role: "user", Content: "query"},
				{Role: "assistant", Content: strings.Repeat("x", 1000)},
			},
			expectedCount:   2,
			expectTruncated: true,
		},
		{
			name: "user message not truncated even if long",
			messages: []Message{
				{Role: "user", Content: strings.Repeat("y", 1000)},
			},
			expectedCount:   1,
			expectTruncated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertHistoryForLead(tt.messages)
			assert.Len(t, result, tt.expectedCount)

			if tt.expectTruncated {
				found := false
				for _, m := range result {
					if m["role"] == "assistant" {
						content := m["content"].(string)
						if strings.Contains(content, "[truncated") {
							found = true
						}
						assert.LessOrEqual(t, len(content), 900,
							"truncated content should be around 800 + suffix")
					}
				}
				assert.True(t, found, "expected at least one truncated assistant message")
			}
		})
	}
}

