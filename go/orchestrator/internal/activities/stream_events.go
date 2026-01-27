package activities

import (
	"context"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"go.temporal.io/sdk/activity"
)

// StreamEventType is a minimal set of event types for streaming_v1
type StreamEventType string

const (
	StreamEventWorkflowStarted   StreamEventType = "WORKFLOW_STARTED"
	StreamEventWorkflowCompleted StreamEventType = "WORKFLOW_COMPLETED"
	StreamEventAgentStarted      StreamEventType = "AGENT_STARTED"
	StreamEventAgentCompleted    StreamEventType = "AGENT_COMPLETED"
	StreamEventErrorOccurred     StreamEventType = "ERROR_OCCURRED"
	StreamEventMessageSent       StreamEventType = "MESSAGE_SENT"
	StreamEventMessageReceived   StreamEventType = "MESSAGE_RECEIVED"
	StreamEventWorkspaceUpdated  StreamEventType = "WORKSPACE_UPDATED"
	// Extended types (emitted when corresponding gates are enabled)
	StreamEventTeamRecruited       StreamEventType = "TEAM_RECRUITED"
	StreamEventTeamRetired         StreamEventType = "TEAM_RETIRED"
	StreamEventRoleAssigned        StreamEventType = "ROLE_ASSIGNED"
	StreamEventDelegation          StreamEventType = "DELEGATION"
	StreamEventDependencySatisfied StreamEventType = "DEPENDENCY_SATISFIED"
	StreamEventBudgetThreshold     StreamEventType = "BUDGET_THRESHOLD"

	// Human-readable UX events
	StreamEventToolInvoked    StreamEventType = "TOOL_INVOKED"    // Tool usage with details in message
	StreamEventAgentThinking  StreamEventType = "AGENT_THINKING"  // Planning/reasoning phases
	StreamEventTeamStatus     StreamEventType = "TEAM_STATUS"     // Multi-agent coordination updates
	StreamEventProgress       StreamEventType = "PROGRESS"        // Step completion updates
	StreamEventDataProcessing StreamEventType = "DATA_PROCESSING" // Processing/analyzing data
	StreamEventWaiting        StreamEventType = "WAITING"         // Waiting for resources/responses
	StreamEventErrorRecovery  StreamEventType = "ERROR_RECOVERY"  // Handling and recovering from errors
	StreamEventWarning        StreamEventType = "WARNING"         // Non-fatal warnings that user should be aware of

	// LLM events (uniform across workflows)
	StreamEventLLMPrompt  StreamEventType = "LLM_PROMPT"  // Sanitized prompt
	StreamEventLLMPartial StreamEventType = "LLM_PARTIAL" // Incremental output chunk
	StreamEventLLMOutput  StreamEventType = "LLM_OUTPUT"  // Final output for a step
	StreamEventToolObs    StreamEventType = "TOOL_OBSERVATION"

	// Stream lifecycle
	StreamEventStreamEnd StreamEventType = "STREAM_END" // Explicit end-of-stream signal

	// Human approval
	StreamEventApprovalRequested StreamEventType = "APPROVAL_REQUESTED"
	StreamEventApprovalDecision  StreamEventType = "APPROVAL_DECISION"

	// HITL Research Review
	StreamEventResearchPlanReady    StreamEventType = "RESEARCH_PLAN_READY"
	StreamEventResearchPlanUpdated  StreamEventType = "RESEARCH_PLAN_UPDATED"
	StreamEventResearchPlanApproved StreamEventType = "RESEARCH_PLAN_APPROVED"

	// Workflow control (pause/resume/cancel)
	StreamEventWorkflowPausing    StreamEventType = "WORKFLOW_PAUSING"    // Pause signal received
	StreamEventWorkflowPaused     StreamEventType = "WORKFLOW_PAUSED"     // Actually blocked at checkpoint
	StreamEventWorkflowResumed    StreamEventType = "WORKFLOW_RESUMED"    // Unblocked after pause
	StreamEventWorkflowCancelling StreamEventType = "WORKFLOW_CANCELLING" // Cancel signal received
	StreamEventWorkflowCancelled  StreamEventType = "WORKFLOW_CANCELLED"  // Cancelled at checkpoint
)

// EmitTaskUpdateInput carries minimal event data for streaming_v1
type EmitTaskUpdateInput struct {
	WorkflowID string                 `json:"workflow_id"`
	EventType  StreamEventType        `json:"event_type"`
	AgentID    string                 `json:"agent_id,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
}

// EmitTaskUpdate logs a minimal deterministic event. In future it can publish to a stream.
func EmitTaskUpdate(ctx context.Context, in EmitTaskUpdateInput) error {
	logger := activity.GetLogger(ctx)
	logger.Info("streaming_v1 event",
		"workflow_id", in.WorkflowID,
		"type", string(in.EventType),
		"agent_id", in.AgentID,
		"message", in.Message,
		"ts", in.Timestamp,
	)
	// Publish to in-process stream manager (best-effort)
	streaming.Get().Publish(in.WorkflowID, streaming.Event{
		WorkflowID: in.WorkflowID,
		Type:       string(in.EventType),
		AgentID:    in.AgentID,
		Message:    in.Message,
		Payload:    in.Payload,
		Timestamp:  in.Timestamp,
	})
	return nil
}
