package control

import (
	"fmt"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)


// SignalHandler manages pause/resume/cancel for any workflow
type SignalHandler struct {
	State      *WorkflowControlState
	WorkflowID string
	AgentID    string
	Logger     log.Logger
	EmitCtx    workflow.Context

	// SkipSSEEmit when true, suppresses SSE event emissions.
	// Used for child workflows where the parent already emits events.
	SkipSSEEmit bool

	// Child workflow management - simple slice is safe because Temporal workflows
	// are cooperatively scheduled (single goroutine, no true concurrency)
	childWorkflowIDs []string
}

// Setup initializes signal channels and query handler
func (h *SignalHandler) Setup(ctx workflow.Context) {
	version := workflow.GetVersion(ctx, "pause_resume_v1", workflow.DefaultVersion, 1)
	if version < 1 {
		return
	}

	h.State = &WorkflowControlState{}
	h.childWorkflowIDs = []string{}

	// Query handler
	_ = workflow.SetQueryHandler(ctx, QueryControlState, func() (WorkflowControlState, error) {
		return *h.State, nil
	})

	pauseCh := workflow.GetSignalChannel(ctx, SignalPause)
	resumeCh := workflow.GetSignalChannel(ctx, SignalResume)
	cancelCh := workflow.GetSignalChannel(ctx, SignalCancel)

	workflow.Go(ctx, func(gCtx workflow.Context) {
		for {
			sel := workflow.NewSelector(gCtx)

			sel.AddReceive(pauseCh, func(c workflow.ReceiveChannel, more bool) {
				var req PauseRequest
				c.Receive(gCtx, &req)
				h.handlePause(gCtx, req)
			})

			sel.AddReceive(resumeCh, func(c workflow.ReceiveChannel, more bool) {
				var req ResumeRequest
				c.Receive(gCtx, &req)
				h.handleResume(gCtx, req)
			})

			sel.AddReceive(cancelCh, func(c workflow.ReceiveChannel, more bool) {
				var req CancelRequest
				c.Receive(gCtx, &req)
				h.handleCancel(gCtx, req)
			})

			sel.Select(gCtx)
		}
	})
}

// RegisterChildWorkflow adds a child workflow ID for signal propagation
func (h *SignalHandler) RegisterChildWorkflow(childID string) {
	h.childWorkflowIDs = append(h.childWorkflowIDs, childID)
}

// UnregisterChildWorkflow removes a completed child workflow
func (h *SignalHandler) UnregisterChildWorkflow(childID string) {
	for i, id := range h.childWorkflowIDs {
		if id == childID {
			h.childWorkflowIDs = append(h.childWorkflowIDs[:i], h.childWorkflowIDs[i+1:]...)
			return
		}
	}
}

func (h *SignalHandler) handlePause(ctx workflow.Context, req PauseRequest) {
	if h.State.IsPaused {
		h.Logger.Debug("Already paused, ignoring")
		return
	}

	h.State.IsPaused = true
	h.State.PausedAt = workflow.Now(ctx)
	h.State.PauseReason = req.Reason
	h.State.PausedBy = req.RequestedBy

	// Only emit SSE event if not suppressed (child workflows skip to avoid duplicates)
	if !h.SkipSSEEmit {
		_ = workflow.ExecuteActivity(h.EmitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: h.WorkflowID,
			EventType:  activities.StreamEventWorkflowPausing,
			AgentID:    h.AgentID,
			Message:    activities.MsgWorkflowPausing(req.Reason),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
	}

	// Propagate pause to all child workflows
	h.propagateSignalToChildren(ctx, SignalPause, req)
}

func (h *SignalHandler) handleResume(ctx workflow.Context, req ResumeRequest) {
	if !h.State.IsPaused {
		h.Logger.Debug("Not paused, ignoring resume")
		return
	}

	h.State.IsPaused = false
	h.State.PausedAt = time.Time{}
	h.State.PauseReason = ""
	h.State.PausedBy = ""

	// Only emit SSE event if not suppressed (child workflows skip to avoid duplicates)
	if !h.SkipSSEEmit {
		_ = workflow.ExecuteActivity(h.EmitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: h.WorkflowID,
			EventType:  activities.StreamEventWorkflowResumed,
			AgentID:    h.AgentID,
			Message:    activities.MsgWorkflowResumed(req.Reason),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
	}

	// Propagate resume to all child workflows
	h.propagateSignalToChildren(ctx, SignalResume, req)
}

func (h *SignalHandler) handleCancel(ctx workflow.Context, req CancelRequest) {
	h.State.IsCancelled = true
	h.State.CancelReason = req.Reason
	h.State.CancelledBy = req.RequestedBy

	// Only emit SSE event if not suppressed (child workflows skip to avoid duplicates)
	if !h.SkipSSEEmit {
		_ = workflow.ExecuteActivity(h.EmitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: h.WorkflowID,
			EventType:  activities.StreamEventWorkflowCancelling,
			AgentID:    h.AgentID,
			Message:    activities.MsgWorkflowCancelling(req.Reason),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
	}

	// Propagate cancel to all child workflows
	h.propagateSignalToChildren(ctx, SignalCancel, req)
}

// propagateSignalToChildren sends signal to all registered child workflows
func (h *SignalHandler) propagateSignalToChildren(ctx workflow.Context, signalName string, payload interface{}) {
	if len(h.childWorkflowIDs) == 0 {
		return
	}

	// Copy slice to avoid issues if modified during iteration
	children := make([]string, len(h.childWorkflowIDs))
	copy(children, h.childWorkflowIDs)

	// Send signals in parallel to avoid sequential blocking
	futures := make([]workflow.Future, 0, len(children))
	for _, childID := range children {
		future := workflow.SignalExternalWorkflow(ctx, childID, "", signalName, payload)
		futures = append(futures, future)
	}

	// Wait for all signals to complete (in parallel, not sequential)
	for _, future := range futures {
		_ = future.Get(ctx, nil) // Ignore errors - child may have completed
	}
}

// CheckPausePoint blocks if paused, returns error if cancelled
func (h *SignalHandler) CheckPausePoint(ctx workflow.Context, checkpoint string) error {
	if h.State == nil {
		return nil
	}

	// Yield to ensure pending signals are processed before checking state
	// This prevents race where signal is received but not yet processed
	_ = workflow.Sleep(ctx, 0)

	if h.State.IsCancelled {
		// Always emit WORKFLOW_CANCELLED - child workflows should emit this because
		// parent can't know when child actually stops at a checkpoint
		_ = workflow.ExecuteActivity(h.EmitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: h.WorkflowID,
			EventType:  activities.StreamEventWorkflowCancelled,
			AgentID:    h.AgentID,
			Message:    activities.MsgWorkflowCancelled(h.State.CancelReason),
			Timestamp:  workflow.Now(ctx),
			Payload:    map[string]interface{}{"checkpoint": checkpoint},
		}).Get(ctx, nil)
		// Return Temporal's CanceledError so workflow status is CANCELLED not FAILED
		return temporal.NewCanceledError(fmt.Sprintf("workflow cancelled: %s", h.State.CancelReason))
	}

	if h.State.IsPaused {
		// Always emit WORKFLOW_PAUSED - child workflows should emit this because
		// parent can't know when child actually blocks at a checkpoint
		_ = workflow.ExecuteActivity(h.EmitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: h.WorkflowID,
			EventType:  activities.StreamEventWorkflowPaused,
			AgentID:    h.AgentID,
			Message:    activities.MsgWorkflowPaused(checkpoint),
			Timestamp:  workflow.Now(ctx),
			Payload:    map[string]interface{}{"checkpoint": checkpoint},
		}).Get(ctx, nil)

		// Block until resumed or cancelled using Await (no polling, single history event)
		_ = workflow.Await(ctx, func() bool {
			return !h.State.IsPaused || h.State.IsCancelled
		})

		if h.State.IsCancelled {
			// Always emit WORKFLOW_CANCELLED when cancelled while paused
			_ = workflow.ExecuteActivity(h.EmitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: h.WorkflowID,
				EventType:  activities.StreamEventWorkflowCancelled,
				AgentID:    h.AgentID,
				Message:    activities.MsgWorkflowCancelled(h.State.CancelReason),
				Timestamp:  workflow.Now(ctx),
				Payload:    map[string]interface{}{"checkpoint": checkpoint, "was_paused": true},
			}).Get(ctx, nil)
			// Return Temporal's CanceledError so workflow status is CANCELLED not FAILED
			return temporal.NewCanceledError(fmt.Sprintf("workflow cancelled while paused: %s", h.State.CancelReason))
		}
	}

	return nil
}

// IsCancelled returns true if the workflow has been cancelled
func (h *SignalHandler) IsCancelled() bool {
	return h.State != nil && h.State.IsCancelled
}

// IsPaused returns true if the workflow is paused
func (h *SignalHandler) IsPaused() bool {
	return h.State != nil && h.State.IsPaused
}

// EmitCancelledIfNeeded emits the workflow.cancelled SSE event if the workflow was cancelled.
// Call this before returning from the workflow when not using CheckPausePoint.
// This ensures the frontend receives the workflow.cancelled event even if no checkpoint was reached.
func (h *SignalHandler) EmitCancelledIfNeeded(ctx workflow.Context, reason string) {
	if h.State == nil || !h.State.IsCancelled || h.SkipSSEEmit {
		return
	}

	cancelReason := h.State.CancelReason
	if cancelReason == "" {
		cancelReason = reason
	}

	_ = workflow.ExecuteActivity(h.EmitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: h.WorkflowID,
		EventType:  activities.StreamEventWorkflowCancelled,
		AgentID:    h.AgentID,
		Message:    activities.MsgWorkflowCancelled(cancelReason),
		Timestamp:  workflow.Now(ctx),
		Payload:    map[string]interface{}{"reason": cancelReason, "at_return": true},
	}).Get(ctx, nil)
}
