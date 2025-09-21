export type TaskStatus = 'PENDING' | 'RUNNING' | 'COMPLETED' | 'FAILED';

export interface TaskSubmitResponse {
  task_id: string;
  status: TaskStatus;
  workflow_id: string;
}

export interface TaskStatusResponse {
  task_id: string;
  status: TaskStatus;
  error?: string;
  response?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
  // Enriched fields for replay/audit
  query?: string;
  session_id?: string;
  mode?: string;
}

export type EventType =
  | 'WORKFLOW_STARTED'
  | 'WORKFLOW_COMPLETED'
  | 'AGENT_STARTED'
  | 'AGENT_COMPLETED'
  | 'AGENT_THINKING'
  | 'MESSAGE_SENT'
  | 'MESSAGE_RECEIVED'
  | 'WORKSPACE_UPDATED'
  | 'ROLE_ASSIGNED'
  | 'TOOL_INVOKED'
  | 'TOOL_COMPLETED'
  | 'ERROR_OCCURRED'
  | 'DELEGATION'
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
