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

	// Human-readable UX events
	StreamEventToolInvoked    StreamEventType = "TOOL_INVOKED"    // Tool usage with details in message
	StreamEventAgentThinking  StreamEventType = "AGENT_THINKING"  // Planning/reasoning phases
	StreamEventTeamStatus     StreamEventType = "TEAM_STATUS"     // Multi-agent coordination updates
	StreamEventProgress       StreamEventType = "PROGRESS"        // Step completion updates
	StreamEventDataProcessing StreamEventType = "DATA_PROCESSING" // Processing/analyzing data
	StreamEventWaiting        StreamEventType = "WAITING"         // Waiting for resources/responses
	StreamEventErrorRecovery  StreamEventType = "ERROR_RECOVERY"  // Handling and recovering from errors

	// LLM events (uniform across workflows)
	StreamEventLLMPrompt  StreamEventType = "LLM_PROMPT"  // Sanitized prompt
	StreamEventLLMPartial StreamEventType = "LLM_PARTIAL" // Incremental output chunk
	StreamEventLLMOutput  StreamEventType = "LLM_OUTPUT"  // Final output for a step
	StreamEventToolObs    StreamEventType = "TOOL_OBSERVATION"

	// Human approval
	StreamEventApprovalRequested StreamEventType = "APPROVAL_REQUESTED"
	StreamEventApprovalDecision  StreamEventType = "APPROVAL_DECISION"
)

// EmitTaskUpdateInput carries minimal event data for streaming_v1
type EmitTaskUpdateInput struct {
	WorkflowID string          `json:"workflow_id"`
	EventType  StreamEventType `json:"event_type"`
	AgentID    string          `json:"agent_id,omitempty"`
	Message    string          `json:"message,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
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
		Timestamp:  in.Timestamp,
	})
	return nil
}
