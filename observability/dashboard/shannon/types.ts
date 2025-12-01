export type TaskStatus = 'PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED';

export interface TaskSubmitResponse {
  task_id: string;
  status: TaskStatus;
  workflow_id: string;
}

export interface TaskStatusResponse {
  task_id: string;
  status: TaskStatus;
  result?: string;  // Raw LLM output (plain text or JSON)
  response?: Record<string, unknown>;  // Parsed JSON (backward compat)
  error?: string;
  created_at?: string;
  updated_at?: string;
  // Enriched fields for replay/audit
  query?: string;
  session_id?: string;
  mode?: string;
  // Usage metadata (NEW)
  model_used?: string;
  provider?: string;
  usage?: {
    total_tokens?: number;
    input_tokens?: number;
    output_tokens?: number;
    estimated_cost?: number;
  };
  metadata?: {
    cost_usd?: number;
    num_agents?: number;
    citations?: Array<{
      url: string;
      title?: string;
      source?: string;
      source_type?: string;
      retrieved_at?: string;
      published_date?: string;
      credibility_score?: number;
    }>;
    // Deprecated: prefer model_breakdown for usage/cost analysis.
    agent_usages?: Array<{
      agent_id: string;
      tokens?: number;
      cost_usd?: number;
      model?: string;
    }>;
    model_breakdown?: Array<{
      model: string;
      provider?: string;
      executions?: number;
      tokens: number;
      cost_usd?: number;
      percentage?: number;
    }>;
    [key: string]: unknown;
  };
}

export type EventType =
  // Core workflow events
  | 'WORKFLOW_STARTED'
  | 'WORKFLOW_COMPLETED'
  // Agent lifecycle
  | 'AGENT_STARTED'
  | 'AGENT_COMPLETED'
  | 'AGENT_THINKING'
  // Communication (P2P v1)
  | 'MESSAGE_SENT'
  | 'MESSAGE_RECEIVED'
  | 'WORKSPACE_UPDATED'
  // Team coordination
  | 'TEAM_RECRUITED'
  | 'TEAM_RETIRED'
  | 'ROLE_ASSIGNED'
  | 'TEAM_STATUS'
  | 'DELEGATION'
  | 'DEPENDENCY_SATISFIED'
  // Tool usage
  | 'TOOL_INVOKED'
  | 'TOOL_OBSERVATION'
  // LLM streaming
  | 'LLM_PROMPT'
  | 'LLM_PARTIAL'
  | 'LLM_OUTPUT'
  // Progress & status
  | 'PROGRESS'
  | 'DATA_PROCESSING'
  | 'WAITING'
  | 'ERROR_RECOVERY'
  | 'ERROR_OCCURRED'
  // Stream lifecycle
  | 'STREAM_END'
  // Human approval
  | 'APPROVAL_REQUESTED'
  | 'APPROVAL_DECISION'
  | string;

export interface TaskEvent {
  workflow_id: string;
  type: EventType;
  agent_id: string;
  message: string;
  timestamp: Date;
  seq: number;
  stream_id?: string;
  formatted?: string;
}

export interface TaskSummary {
  task_id: string;
  query?: string;
  status: string;
  mode?: string;
  created_at?: string;
  completed_at?: string;
  total_token_usage?: Record<string, unknown>;
}

export interface ListTasksResponse {
  tasks: TaskSummary[];
  total_count: number;
}
