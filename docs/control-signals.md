# Workflow Control Signals

This document describes the pause/resume/cancel control signal system for Shannon workflows.

## Overview

Shannon supports three control signals for workflow management:
- **Pause**: Temporarily halt workflow execution at the next checkpoint
- **Resume**: Continue a paused workflow from its checkpoint
- **Cancel**: Permanently stop a workflow with cleanup

## Signal Semantics

### Pause Signal (`pause_v1`)

**When pause takes effect:**
- Pause signals are queued immediately but take effect only at **checkpoints**
- Checkpoints are strategic points in workflow execution where pause is safe
- OrchestratorWorkflow checkpoints: `pre_routing`, `pre_simple_workflow`, `pre_supervisor_workflow`, `pre_dag_workflow`
- Strategy workflow checkpoints: `pre_simple_strategy`, `pre_research_workflow`, etc.
- Child workflows have their own checkpoints: `pre_execution`, `pre_completion`, `post_agent`

**What "checkpoint" means:**
- A checkpoint is a deliberate pause point inserted by the workflow developer
- At a checkpoint, the workflow queries its control state and blocks if paused
- Checkpoints ensure workflows pause at consistent, resumable states
- Work already in progress (LLM calls, activities) completes before pausing
- Checkpoints use `workflow.Await()` for efficient blocking (no timer polling)

**Behavior:**
1. Signal sent to parent workflow
2. Parent propagates signal to all registered child workflows
3. Each workflow blocks at next checkpoint until resume or cancel

### Resume Signal (`resume_v1`)

**When resume takes effect:**
- Immediately unblocks workflows waiting at checkpoints
- Propagates to all child workflows

**Behavior:**
1. Clears pause state
2. Emits `WORKFLOW_RESUMED` SSE event
3. Workflow continues from checkpoint

### Cancel Signal (`cancel_v1`)

**When cancel takes effect:**
- Immediately marks workflow as cancelled
- Next checkpoint returns an error, terminating the workflow

**Behavior:**
1. Sets cancelled state with reason
2. Propagates to all child workflows
3. Emits `WORKFLOW_CANCELLING` then `WORKFLOW_CANCELLED` events
4. Workflow terminates with cancellation error

## API Endpoints

### gRPC

```protobuf
// Pause a running workflow
rpc PauseTask(PauseTaskRequest) returns (PauseTaskResponse);

// Resume a paused workflow
rpc ResumeTask(ResumeTaskRequest) returns (ResumeTaskResponse);

// Cancel a workflow
rpc CancelTask(CancelTaskRequest) returns (CancelTaskResponse);

// Get current control state
rpc GetControlState(GetControlStateRequest) returns (GetControlStateResponse);
```

### Query Handler

Workflows register a query handler for `control_state_v1` that returns:
```go
type WorkflowControlState struct {
    IsPaused      bool
    PausedAt      time.Time
    PauseReason   string
    PausedBy      string
    IsCancelled   bool
    CancelReason  string
    CancelledBy   string
}
```

## SSE Events

Control signals emit SSE events for frontend integration:

| Internal Type | SSE Event Name | Payload |
|--------------|----------------|---------|
| WORKFLOW_PAUSING | workflow.pausing | workflow_id, agent_id, message |
| WORKFLOW_PAUSED | workflow.paused | workflow_id, agent_id, checkpoint, message |
| WORKFLOW_RESUMED | workflow.resumed | workflow_id, agent_id, message |
| WORKFLOW_CANCELLING | workflow.cancelling | workflow_id, agent_id, message |
| WORKFLOW_CANCELLED | workflow.cancelled | workflow_id, agent_id, message |

## Integration Guide

### Adding Checkpoints to a Workflow

```go
func MyWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
    // Initialize control handler
    controlHandler := &control.SignalHandler{
        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
        AgentID:    "my-agent",
        Logger:     workflow.GetLogger(ctx),
        EmitCtx:    emitCtx,
    }
    controlHandler.Setup(ctx)

    // Pre-execution checkpoint
    if err := controlHandler.CheckPausePoint(ctx, "pre_execution"); err != nil {
        return TaskResult{Success: false, ErrorMessage: err.Error()}, err
    }

    // ... do work ...

    // Pre-completion checkpoint
    if err := controlHandler.CheckPausePoint(ctx, "pre_completion"); err != nil {
        return TaskResult{Success: false, ErrorMessage: err.Error()}, err
    }

    return TaskResult{Success: true}, nil
}
```

### Child Workflow Registration

For parent workflows that spawn children:

```go
// Before starting child
childFuture := workflow.ExecuteChildWorkflow(ctx, ChildWorkflow, input)
var childExec workflow.Execution
childFuture.GetChildWorkflowExecution().Get(ctx, &childExec)
controlHandler.RegisterChildWorkflow(childExec.ID)

// After child completes
err := childFuture.Get(ctx, &result)
controlHandler.UnregisterChildWorkflow(childExec.ID)
```

## Determinism & Replay

Control signals use version gates for replay safety:

```go
version := workflow.GetVersion(ctx, "pause_resume_v1", workflow.DefaultVersion, 1)
if version < 1 {
    // Legacy behavior (no control signals)
    return
}
// Control signal handling
```

This ensures:
- Existing workflow replays are unaffected
- New workflows get control signal support
- Smooth migration for running workflows

## Testing

Unit tests for control signals:
```bash
go test -v ./internal/workflows/control/...
```

Tests cover:
- Signal handler setup
- Pause signal reception and blocking
- Resume signal unblocking
- Cancel signal termination
- Replay determinism
- Child workflow registration
- Multiple pause/resume cycles

## Troubleshooting

### Pause signal sent but workflow continues
- **Cause**: Workflow hasn't reached a checkpoint yet
- **Solution**: Add checkpoints at strategic points in the workflow

### Child workflows not pausing
- **Cause**: Parent didn't register child workflow IDs
- **Solution**: Use `RegisterChildWorkflow()` before starting children

### Control state shows paused but status is RUNNING
- **Cause**: DB status update happens at terminal state
- **Solution**: Query `GetControlState` for real-time pause state
