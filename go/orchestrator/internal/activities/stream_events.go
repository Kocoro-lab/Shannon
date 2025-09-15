package activities

import (
    "context"
    "time"

    "go.uber.org/zap"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
)

// StreamEventType is a minimal set of event types for streaming_v1
type StreamEventType string

const (
    StreamEventWorkflowStarted StreamEventType = "WORKFLOW_STARTED"
    StreamEventAgentStarted    StreamEventType = "AGENT_STARTED"
    StreamEventAgentCompleted  StreamEventType = "AGENT_COMPLETED"
    StreamEventErrorOccurred   StreamEventType = "ERROR_OCCURRED"
    StreamEventMessageSent     StreamEventType = "MESSAGE_SENT"
    StreamEventMessageReceived StreamEventType = "MESSAGE_RECEIVED"
    StreamEventWorkspaceUpdated StreamEventType = "WORKSPACE_UPDATED"
    // Extended types (emitted when corresponding gates are enabled)
    StreamEventTeamRecruited   StreamEventType = "TEAM_RECRUITED"
    StreamEventTeamRetired     StreamEventType = "TEAM_RETIRED"
    StreamEventRoleAssigned    StreamEventType = "ROLE_ASSIGNED"
    StreamEventDelegation      StreamEventType = "DELEGATION"
    StreamEventDependencySatisfied StreamEventType = "DEPENDENCY_SATISFIED"
)

// EmitTaskUpdateInput carries minimal event data for streaming_v1
type EmitTaskUpdateInput struct {
    WorkflowID string           `json:"workflow_id"`
    EventType  StreamEventType  `json:"event_type"`
    AgentID    string           `json:"agent_id,omitempty"`
    Message    string           `json:"message,omitempty"`
    Timestamp  time.Time        `json:"timestamp"`
}

// EmitTaskUpdate logs a minimal deterministic event. In future it can publish to a stream.
func EmitTaskUpdate(ctx context.Context, in EmitTaskUpdateInput) error {
    logger := zap.L()
    logger.Info("streaming_v1 event",
        zap.String("workflow_id", in.WorkflowID),
        zap.String("type", string(in.EventType)),
        zap.String("agent_id", in.AgentID),
        zap.String("message", in.Message),
        zap.Time("ts", in.Timestamp),
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
