/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

export type EventType =
    | "thread.message.delta"
    | "thread.message.completed"
    | "error"
    | "done"
    | "STREAM_END"
    | "WORKFLOW_FAILED"
    | "workflow.pausing"
    | "workflow.paused"
    | "workflow.resumed"
    | "workflow.cancelling"
    | "workflow.cancelled"
    | "ROLE_ASSIGNED"
    | "TEAM_RECRUITED"
    | "TEAM_RETIRED"
    | "TEAM_STATUS"
    | "DELEGATION"
    | "BUDGET_THRESHOLD"
    | "TOOL_INVOKED"
    | "TOOL_OBSERVATION"
    | "WORKFLOW_STARTED"
    | "WORKFLOW_COMPLETED"
    | "AGENT_STARTED"
    | "AGENT_COMPLETED"
    | "AGENT_THINKING"
    | "LLM_PROMPT"
    | "LLM_OUTPUT"
    | "DATA_PROCESSING"
    | "PROGRESS"
    | "WAITING"
    | "APPROVAL_REQUESTED"
    | "APPROVAL_DECISION"
    | "DEPENDENCY_SATISFIED"
    | "ERROR_OCCURRED"
    | "ERROR_RECOVERY"
    | "MESSAGE_SENT"
    | "MESSAGE_RECEIVED"
    | "WORKSPACE_UPDATED"
    | "STATUS_UPDATE"
    | "RESEARCH_PLAN_READY"
    | "RESEARCH_PLAN_UPDATED"
    | "RESEARCH_PLAN_APPROVED"
    | "REVIEW_USER_FEEDBACK";

export interface BaseEvent {
    type: EventType;
    workflow_id: string;
    agent_id?: string;
    seq?: number;
    stream_id?: string;
    timestamp?: string; // Client-side timestamp
}

export interface ThreadMessageDeltaEvent extends BaseEvent {
    type: "thread.message.delta";
    delta: string;
}

export interface ThreadMessageCompletedEvent extends BaseEvent {
    type: "thread.message.completed";
    response: string;
    metadata?: {
        usage?: {
            total_tokens: number;
            input_tokens: number;
            output_tokens: number;
        };
        model?: string;
        provider?: string;
    };
}

export interface ErrorEvent extends BaseEvent {
    type: "error";
    message: string;
    code?: string;
}

export interface DoneEvent extends BaseEvent {
    type: "done";
}

export interface LlmOutputEvent extends BaseEvent {
    type: "LLM_OUTPUT";
    payload: {
        text: string;
    };
    message?: string;
    metadata?: any;
}

export interface ToolInvokedEvent extends BaseEvent {
    type: "TOOL_INVOKED";
    message: string; // Human-readable description
    payload?: {
        tool: string;
        params: Record<string, any>;
    };
}

export interface ToolObservationEvent extends BaseEvent {
    type: "TOOL_OBSERVATION";
    message: string; // Tool output (truncated to 2000 chars)
    payload?: {
        tool: string;
        success: boolean;
    };
}

export interface WorkflowStartedEvent extends BaseEvent {
    type: "WORKFLOW_STARTED";
    message?: string;
}

export interface WorkflowCompletedEvent extends BaseEvent {
    type: "WORKFLOW_COMPLETED";
    message?: string;
}

export interface WorkflowFailedEvent extends BaseEvent {
    type: "WORKFLOW_FAILED";
    message?: string;
    error_code?: string;
}

export interface AgentStartedEvent extends BaseEvent {
    type: "AGENT_STARTED";
    message?: string;
}

export interface AgentCompletedEvent extends BaseEvent {
    type: "AGENT_COMPLETED";
    message?: string;
}

export interface AgentThinkingEvent extends BaseEvent {
    type: "AGENT_THINKING";
    message?: string;
}

export interface LlmPromptEvent extends BaseEvent {
    type: "LLM_PROMPT";
    message?: string;
}

export interface DelegationEvent extends BaseEvent {
    type: "DELEGATION";
    message?: string;
}

export interface RoleAssignedEvent extends BaseEvent {
    type: "ROLE_ASSIGNED";
    message?: string;
}

export interface TeamRecruitedEvent extends BaseEvent {
    type: "TEAM_RECRUITED";
    message?: string;
}

export interface TeamRetiredEvent extends BaseEvent {
    type: "TEAM_RETIRED";
    message?: string;
}

export interface TeamStatusEvent extends BaseEvent {
    type: "TEAM_STATUS";
    message?: string;
}

export interface ProgressEvent extends BaseEvent {
    type: "PROGRESS";
    message?: string;
}

export interface DataProcessingEvent extends BaseEvent {
    type: "DATA_PROCESSING";
    message?: string;
}

export interface WaitingEvent extends BaseEvent {
    type: "WAITING";
    message?: string;
}

export interface ErrorRecoveryEvent extends BaseEvent {
    type: "ERROR_RECOVERY";
    message: string;
}

export interface ErrorOccurredEvent extends BaseEvent {
    type: "ERROR_OCCURRED";
    message: string;
}

export interface BudgetThresholdEvent extends BaseEvent {
    type: "BUDGET_THRESHOLD";
    message?: string;
}

export interface DependencySatisfiedEvent extends BaseEvent {
    type: "DEPENDENCY_SATISFIED";
    message?: string;
}

export interface ApprovalRequestedEvent extends BaseEvent {
    type: "APPROVAL_REQUESTED";
    message?: string;
}

export interface ApprovalDecisionEvent extends BaseEvent {
    type: "APPROVAL_DECISION";
    message?: string;
}

export interface MessageSentEvent extends BaseEvent {
    type: "MESSAGE_SENT";
    message?: string;
}

export interface MessageReceivedEvent extends BaseEvent {
    type: "MESSAGE_RECEIVED";
    message?: string;
}

export interface WorkspaceUpdatedEvent extends BaseEvent {
    type: "WORKSPACE_UPDATED";
    message?: string;
}

export interface StatusUpdateEvent extends BaseEvent {
    type: "STATUS_UPDATE";
    message?: string;
}

export interface StreamEndEvent extends BaseEvent {
    type: "done" | "STREAM_END";
}

export interface WorkflowPausingEvent extends BaseEvent {
    type: "workflow.pausing";
    message?: string;
}

export interface WorkflowPausedEvent extends BaseEvent {
    type: "workflow.paused";
    checkpoint?: string;
    message?: string;
}

export interface WorkflowResumedEvent extends BaseEvent {
    type: "workflow.resumed";
    message?: string;
}

export interface WorkflowCancellingEvent extends BaseEvent {
    type: "workflow.cancelling";
    message?: string;
}

export interface WorkflowCancelledEvent extends BaseEvent {
    type: "workflow.cancelled";
    message?: string;
}

export interface ResearchPlanReadyEvent extends BaseEvent {
    type: "RESEARCH_PLAN_READY";
    message?: string;
}

export interface ResearchPlanUpdatedEvent extends BaseEvent {
    type: "RESEARCH_PLAN_UPDATED";
    message?: string;
}

export interface ResearchPlanApprovedEvent extends BaseEvent {
    type: "RESEARCH_PLAN_APPROVED";
    message?: string;
}

export interface ReviewUserFeedbackEvent extends BaseEvent {
    type: "REVIEW_USER_FEEDBACK";
    message?: string;
}

export type ShannonEvent =
    | WorkflowStartedEvent
    | WorkflowCompletedEvent
    | WorkflowFailedEvent
    | WorkflowPausingEvent
    | WorkflowPausedEvent
    | WorkflowResumedEvent
    | WorkflowCancellingEvent
    | WorkflowCancelledEvent
    | AgentStartedEvent
    | AgentCompletedEvent
    | AgentThinkingEvent
    | ToolInvokedEvent
    | ToolObservationEvent
    | LlmPromptEvent
    | ThreadMessageDeltaEvent
    | ThreadMessageCompletedEvent
    | ErrorEvent
    | DoneEvent
    | DelegationEvent
    | RoleAssignedEvent
    | TeamRecruitedEvent
    | TeamRetiredEvent
    | TeamStatusEvent
    | ProgressEvent
    | DataProcessingEvent
    | WaitingEvent
    | ErrorRecoveryEvent
    | ErrorOccurredEvent
    | BudgetThresholdEvent
    | DependencySatisfiedEvent
    | ApprovalRequestedEvent
    | ApprovalDecisionEvent
    | MessageSentEvent
    | MessageReceivedEvent
    | WorkspaceUpdatedEvent
    | StatusUpdateEvent
    | StreamEndEvent
    | LlmOutputEvent
    | ResearchPlanReadyEvent
    | ResearchPlanUpdatedEvent
    | ResearchPlanApprovedEvent
    | ReviewUserFeedbackEvent;
