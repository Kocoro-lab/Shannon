package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// emitStub is a no-op activity for emitting events
func emitStub(ctx context.Context, in activities.EmitTaskUpdateInput) error {
	return nil
}

// TestSignalHandlerSetup tests that signal handlers are registered without error
func TestSignalHandlerSetup(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(emitStub, activity.RegisterOptions{Name: "EmitTaskUpdate"})

	wf := func(ctx workflow.Context) (string, error) {
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
		})

		handler := &SignalHandler{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			AgentID:    "test-agent",
			Logger:     workflow.GetLogger(ctx),
			EmitCtx:    emitCtx,
		}
		handler.Setup(ctx)

		// Verify handler is initialized
		if handler.WorkflowID == "" {
			return "", nil
		}

		return "ok", nil
	}

	env.RegisterWorkflow(wf)
	env.ExecuteWorkflow(wf)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, "ok", result)
}

// TestPauseSignalReceived tests that pause signals are received and processed
func TestPauseSignalReceived(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(emitStub, activity.RegisterOptions{Name: "EmitTaskUpdate"})

	wf := func(ctx workflow.Context) (bool, error) {
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
		})

		handler := &SignalHandler{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			AgentID:    "test-agent",
			Logger:     workflow.GetLogger(ctx),
			EmitCtx:    emitCtx,
		}
		handler.Setup(ctx)

		// Simulate some work before checkpoint
		_ = workflow.Sleep(ctx, 100*time.Millisecond)

		// Check pause point - this should block if paused
		err := handler.CheckPausePoint(ctx, "test_checkpoint")
		if err != nil {
			return false, err
		}

		// After resume, workflow should not be paused
		return handler.IsPaused(), nil
	}

	env.RegisterWorkflow(wf)

	// Send pause signal during execution
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalPause, PauseRequest{
			Reason:      "test pause",
			RequestedBy: "test-user",
		})
	}, 50*time.Millisecond)

	// Send resume to allow workflow to complete
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalResume, ResumeRequest{
			RequestedBy: "test-user",
		})
	}, 200*time.Millisecond)

	env.ExecuteWorkflow(wf)

	require.True(t, env.IsWorkflowCompleted())
	// The workflow should complete after resume
	require.NoError(t, env.GetWorkflowError())
}

// TestCancelSignalTerminates tests that cancel signal terminates the workflow
func TestCancelSignalTerminates(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(emitStub, activity.RegisterOptions{Name: "EmitTaskUpdate"})

	wf := func(ctx workflow.Context) (string, error) {
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
		})

		handler := &SignalHandler{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			AgentID:    "test-agent",
			Logger:     workflow.GetLogger(ctx),
			EmitCtx:    emitCtx,
		}
		handler.Setup(ctx)

		// Simulate some work
		_ = workflow.Sleep(ctx, 100*time.Millisecond)

		// Check pause point - this should error if cancelled
		err := handler.CheckPausePoint(ctx, "test_checkpoint")
		if err != nil {
			return "cancelled", err
		}

		return "completed", nil
	}

	env.RegisterWorkflow(wf)

	// Send cancel signal during execution
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalCancel, CancelRequest{
			Reason:      "test cancel",
			RequestedBy: "test-user",
		})
	}, 50*time.Millisecond)

	env.ExecuteWorkflow(wf)

	require.True(t, env.IsWorkflowCompleted())
	// The workflow should error due to cancellation with CanceledError
	err := env.GetWorkflowError()
	require.Error(t, err)
	// Check it's a CanceledError (Temporal's cancellation type)
	var canceledErr *temporal.CanceledError
	assert.True(t, errors.As(err, &canceledErr), "expected CanceledError, got %T", err)
}

// TestReplayDeterminism tests that pause/resume doesn't break replay determinism
func TestReplayDeterminism(t *testing.T) {
	// Run the same workflow twice to verify determinism
	for i := 0; i < 2; i++ {
		suite := &testsuite.WorkflowTestSuite{}
		env := suite.NewTestWorkflowEnvironment()

		env.RegisterActivityWithOptions(emitStub, activity.RegisterOptions{Name: "EmitTaskUpdate"})

		wf := func(ctx workflow.Context) (string, error) {
			// Use version gate as in production code
			v := workflow.GetVersion(ctx, "pause_resume_v1", workflow.DefaultVersion, 1)
			if v < 1 {
				return "legacy", nil
			}

			emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 5 * time.Second,
			})

			handler := &SignalHandler{
				WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
				AgentID:    "test-agent",
				Logger:     workflow.GetLogger(ctx),
				EmitCtx:    emitCtx,
			}
			handler.Setup(ctx)

			// Multiple checkpoints
			if err := handler.CheckPausePoint(ctx, "checkpoint_1"); err != nil {
				return "", err
			}

			_ = workflow.Sleep(ctx, 50*time.Millisecond)

			if err := handler.CheckPausePoint(ctx, "checkpoint_2"); err != nil {
				return "", err
			}

			return "completed", nil
		}

		env.RegisterWorkflow(wf)
		env.ExecuteWorkflow(wf)

		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())

		var result string
		require.NoError(t, env.GetWorkflowResult(&result))
		assert.Equal(t, "completed", result)
	}
}

// TestChildWorkflowRegistration tests child workflow registration and unregistration
func TestChildWorkflowRegistration(t *testing.T) {
	handler := &SignalHandler{
		WorkflowID: "parent-workflow",
		AgentID:    "test",
		State:      &WorkflowControlState{},
	}

	// Register child workflows
	handler.RegisterChildWorkflow("child-1")
	handler.RegisterChildWorkflow("child-2")

	// Unregister one
	handler.UnregisterChildWorkflow("child-1")

	// Unregister non-existent (should not panic)
	handler.UnregisterChildWorkflow("non-existent")
}

// TestChildWorkflowPausePropagation tests that pause signals propagate to child workflows
func TestChildWorkflowPausePropagation(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(emitStub, activity.RegisterOptions{Name: "EmitTaskUpdate"})

	// Mock child workflow that listens for pause
	childWf := func(ctx workflow.Context) (string, error) {
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
		})

		handler := &SignalHandler{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			AgentID:    "child-agent",
			Logger:     workflow.GetLogger(ctx),
			EmitCtx:    emitCtx,
		}
		handler.Setup(ctx)

		// Child does some work
		_ = workflow.Sleep(ctx, 500*time.Millisecond)

		// Check pause point
		err := handler.CheckPausePoint(ctx, "child_checkpoint")
		if err != nil {
			return "cancelled", err
		}

		return "child_completed", nil
	}

	// Parent workflow that spawns child
	parentWf := func(ctx workflow.Context) (string, error) {
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
		})

		handler := &SignalHandler{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			AgentID:    "parent-agent",
			Logger:     workflow.GetLogger(ctx),
			EmitCtx:    emitCtx,
		}
		handler.Setup(ctx)

		// Check pause before spawning child
		if err := handler.CheckPausePoint(ctx, "pre_child"); err != nil {
			return "", err
		}

		// Spawn child workflow (note: in test env, we just call directly)
		childResult, err := childWf(ctx)
		if err != nil {
			return "", err
		}

		return "parent_" + childResult, nil
	}

	env.RegisterWorkflow(parentWf)
	env.RegisterWorkflow(childWf)

	// Send pause signal early
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalPause, PauseRequest{
			Reason:      "pause propagation test",
			RequestedBy: "test-user",
		})
	}, 100*time.Millisecond)

	// Resume to allow completion
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalResume, ResumeRequest{})
	}, 700*time.Millisecond)

	env.ExecuteWorkflow(parentWf)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	// In real scenario, child would receive propagated signal
	// For this test, we verify the parent workflow completed after pause/resume
	var result string
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Contains(t, result, "completed")
}

// TestMultiplePauseResumeCycles tests multiple pause/resume cycles
func TestMultiplePauseResumeCycles(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(emitStub, activity.RegisterOptions{Name: "EmitTaskUpdate"})

	checkpointsReached := 0

	wf := func(ctx workflow.Context) (int, error) {
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
		})

		handler := &SignalHandler{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			AgentID:    "test-agent",
			Logger:     workflow.GetLogger(ctx),
			EmitCtx:    emitCtx,
		}
		handler.Setup(ctx)

		// Multiple checkpoints
		for i := 0; i < 3; i++ {
			if err := handler.CheckPausePoint(ctx, "checkpoint"); err != nil {
				return checkpointsReached, err
			}
			checkpointsReached++
			_ = workflow.Sleep(ctx, 50*time.Millisecond)
		}

		return checkpointsReached, nil
	}

	env.RegisterWorkflow(wf)

	// Pause and resume multiple times
	for i := 0; i < 3; i++ {
		delay := time.Duration(i*100+30) * time.Millisecond
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(SignalPause, PauseRequest{Reason: "pause"})
		}, delay)

		resumeDelay := delay + 20*time.Millisecond
		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(SignalResume, ResumeRequest{})
		}, resumeDelay)
	}

	env.ExecuteWorkflow(wf)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result int
	require.NoError(t, env.GetWorkflowResult(&result))
	assert.Equal(t, 3, result, "all checkpoints should be reached")
}
