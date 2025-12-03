/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

export type EventType =
    | "thread.message.delta"
    | "thread.message.completed"
    | "error"
    | "done"
    | "STREAM_END"
    | "ROLE_ASSIGNED"
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
    | "PROGRESS";

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
    type: "ROLE_ASSIGNED";
    message?: string;
}

export interface TeamRetiredEvent extends BaseEvent {
    type: "ROLE_ASSIGNED";
    message?: string;
}

export interface TeamStatusEvent extends BaseEvent {
    type: "ROLE_ASSIGNED";
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
    type: "PROGRESS";
    message?: string;
}

export interface ErrorRecoveryEvent extends BaseEvent {
    type: "error";
    message: string;
}

export interface ErrorOccurredEvent extends BaseEvent {
    type: "error";
    message: string;
}

export interface BudgetThresholdEvent extends BaseEvent {
    type: "BUDGET_THRESHOLD";
    message?: string;
}

export interface DependencySatisfiedEvent extends BaseEvent {
    type: "PROGRESS";
    message?: string;
}

export interface ApprovalRequestedEvent extends BaseEvent {
    type: "PROGRESS";
    message?: string;
}

export interface ApprovalDecisionEvent extends BaseEvent {
    type: "PROGRESS";
    message?: string;
}

export interface MessageSentEvent extends BaseEvent {
    type: "PROGRESS";
    message?: string;
}

export interface MessageReceivedEvent extends BaseEvent {
    type: "PROGRESS";
    message?: string;
}

export interface WorkspaceUpdatedEvent extends BaseEvent {
    type: "PROGRESS";
    message?: string;
}

export interface StreamEndEvent extends BaseEvent {
    type: "done" | "STREAM_END";
}

export type ShannonEvent =
    | WorkflowStartedEvent
    | WorkflowCompletedEvent
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
    | StreamEndEvent
    | LlmOutputEvent;
