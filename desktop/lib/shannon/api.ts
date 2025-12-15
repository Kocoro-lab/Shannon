/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

// Auth headers helper - reads from environment or uses test UUID for development
// Default user ID matches migrations/postgres/003_authentication.sql seed data
// Note: Tenant is derived from user record by backend, not sent as header
function getAuthHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};
    
    // User ID - default is the seeded admin user from migrations
    // Backend CORS allows: X-User-Id (case sensitive)
    const userId = process.env.NEXT_PUBLIC_USER_ID || "00000000-0000-0000-0000-000000000002";
    if (userId) {
        headers["X-User-Id"] = userId;
    }
    
    return headers;
}

export interface TaskSubmitRequest {
    query: string;
    session_id?: string;
    context?: Record<string, any>;
    research_strategy?: "quick" | "standard" | "deep" | "academic";
    max_concurrent_agents?: number;
}

export interface TaskSubmitResponse {
    task_id: string;
    workflow_id?: string;
    status: string;
    message?: string;
    created_at: string;
    stream_url?: string;
}

export async function submitTask(request: TaskSubmitRequest): Promise<TaskSubmitResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify(request),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to submit task: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function getTask(taskId: string) {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}`);

    if (!response.ok) {
        throw new Error(`Failed to get task: ${response.statusText}`);
    }

    return response.json();
}

export interface TaskListResponse {
    tasks: Array<{
        task_id: string;
        query: string;
        status: string;
        mode: string;
        created_at: string;
        completed_at?: string;
        total_token_usage: {
            total_tokens: number;
            cost_usd: number;
            prompt_tokens: number;
            completion_tokens: number;
        };
    }>;
    total_count: number;
}

export async function listTasks(limit: number = 50, offset: number = 0): Promise<TaskListResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks?limit=${limit}&offset=${offset}`);

    if (!response.ok) {
        throw new Error(`Failed to list tasks: ${response.statusText}`);
    }

    return response.json();
}

export function getStreamUrl(workflowId: string): string {
    return `${API_BASE_URL}/api/v1/stream/sse?workflow_id=${workflowId}`;
}

// Session Types

export interface Session {
    session_id: string;
    user_id: string;
    title?: string;
    task_count: number;
    tokens_used: number;
    token_budget?: number;
    created_at: string;
    updated_at?: string;
    expires_at?: string;
    context?: Record<string, any>;
    // Activity tracking
    last_activity_at?: string;
    is_active?: boolean;
    // Task success metrics
    successful_tasks?: number;
    failed_tasks?: number;
    success_rate?: number;
    // Cost tracking
    total_cost_usd?: number;
    average_cost_per_task?: number;
    // Budget utilization
    budget_utilization?: number;
    budget_remaining?: number;
    is_near_budget_limit?: boolean;
    // Latest task preview
    latest_task_query?: string;
    latest_task_status?: string;
    // Research detection
    is_research_session?: boolean;
    first_task_mode?: string;
    research_strategy?: string;
}

export interface SessionListResponse {
    sessions: Session[];
    total_count: number;
}

export interface TaskHistory {
    task_id: string;
    workflow_id: string;
    query: string;
    status: string;
    mode?: string;
    result?: string;
    error_message?: string;
    total_tokens: number;
    total_cost_usd: number;
    duration_ms?: number;
    agents_used: number;
    tools_invoked: number;
    started_at: string;
    completed_at?: string;
    metadata?: Record<string, any>;
}

export interface SessionHistoryResponse {
    session_id: string;
    tasks: TaskHistory[];
    total: number;
}

export interface Event {
    workflow_id: string;
    type: string;
    agent_id?: string;
    message?: string;
    timestamp: string;
    seq: number;
    stream_id?: string;
    payload?: string; // JSON string from backend
}

export interface Turn {
    turn: number;
    task_id: string;
    user_query: string;
    final_output: string;
    timestamp: string;
    events: Event[];
    metadata: {
        tokens_used: number;
        execution_time_ms: number;
        agents_involved: string[];
        cost_usd?: number;
    };
}

export interface SessionEventsResponse {
    session_id: string;
    count: number;
    turns: Turn[];
}

// Session API Functions

export async function listSessions(limit: number = 20, offset: number = 0): Promise<SessionListResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/sessions?limit=${limit}&offset=${offset}`);

    if (!response.ok) {
        throw new Error(`Failed to list sessions: ${response.statusText}`);
    }

    return response.json();
}

export async function getSession(sessionId: string): Promise<Session> {
    const response = await fetch(`${API_BASE_URL}/api/v1/sessions/${sessionId}`);

    if (!response.ok) {
        throw new Error(`Failed to get session: ${response.statusText}`);
    }

    return response.json();
}

export async function getSessionHistory(sessionId: string): Promise<SessionHistoryResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/sessions/${sessionId}/history`);

    if (!response.ok) {
        throw new Error(`Failed to get session history: ${response.statusText}`);
    }

    return response.json();
}

export async function getSessionEvents(sessionId: string, limit: number = 10, offset: number = 0, includePayload: boolean = true): Promise<SessionEventsResponse> {
    const params = new URLSearchParams({
        limit: limit.toString(),
        offset: offset.toString(),
    });

    if (includePayload) {
        params.append('include_payload', 'true');
    }

    const response = await fetch(`${API_BASE_URL}/api/v1/sessions/${sessionId}/events?${params.toString()}`);

    if (!response.ok) {
        throw new Error(`Failed to get session events: ${response.statusText}`);
    }

    return response.json();
}

// Task Control Types

export interface TaskControlResponse {
    success: boolean;
    message: string;
    task_id: string;
}

export interface ControlStateResponse {
    is_paused: boolean;
    is_cancelled: boolean;
    paused_at: string;
    pause_reason: string;
    paused_by: string;
    cancel_reason: string;
    cancelled_by: string;
}

// Task Control API Functions

export async function pauseTask(taskId: string, reason?: string): Promise<TaskControlResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}/pause`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify(reason ? { reason } : {}),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to pause task: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function resumeTask(taskId: string, reason?: string): Promise<TaskControlResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}/resume`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify(reason ? { reason } : {}),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to resume task: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function cancelTask(taskId: string, reason?: string): Promise<{ success: boolean }> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}/cancel`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify(reason ? { reason } : {}),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to cancel task: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function getTaskControlState(taskId: string): Promise<ControlStateResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}/control-state`, {
        // Use the same auth mechanism as other API calls (cookies / Authorization headers).
    });

    if (!response.ok) {
        throw new Error(`Failed to get task control state: ${response.statusText}`);
    }

    return response.json();
}

// Schedule Types

export type ScheduleStatus = 'ACTIVE' | 'PAUSED' | 'DELETED';
export type ScheduleRunStatus = 'COMPLETED' | 'FAILED' | 'RUNNING' | 'UNKNOWN';

export interface ScheduleInfo {
    schedule_id: string;
    name: string;
    description?: string;
    cron_expression: string;
    timezone: string;
    task_query: string;
    task_context?: Record<string, any>;
    status: ScheduleStatus;
    next_run_at?: string;
    last_run_at?: string;
    total_runs: number;
    successful_runs: number;
    failed_runs: number;
    max_budget_per_run_usd?: number;
    timeout_seconds?: number;
    created_at: string;
}

export interface ScheduleRun {
    workflow_id: string;
    query: string;
    status: ScheduleRunStatus;
    result?: string;
    error_message?: string;
    model_used?: string;
    provider?: string;
    total_tokens: number;
    total_cost_usd: number;
    duration_ms?: number;
    triggered_at: string;
    started_at?: string;
    completed_at?: string;
}

export interface ScheduleListResponse {
    schedules: ScheduleInfo[];
    total_count: number;
}

export interface ScheduleRunsResponse {
    runs: ScheduleRun[];
    total_count: number;
    page: number;
    page_size: number;
}

export interface CreateScheduleRequest {
    name: string;
    description?: string;
    cron_expression: string;
    timezone?: string;
    task_query: string;
    task_context?: Record<string, string>;  // Backend expects map[string]string
    max_budget_per_run_usd?: number;
    timeout_seconds?: number;
}

export interface UpdateScheduleRequest {
    name?: string;
    description?: string;
    cron_expression?: string;
    timezone?: string;
    task_query?: string;
    task_context?: Record<string, string>;  // Backend expects map[string]string
    clear_task_context?: boolean;
    max_budget_per_run_usd?: number;
    timeout_seconds?: number;
}

// Schedule API Functions

export async function listSchedules(
    pageSize: number = 50,
    page: number = 1,
    status?: ScheduleStatus
): Promise<ScheduleListResponse> {
    const params = new URLSearchParams({
        page: String(page),
        page_size: String(pageSize),
    });
    if (status) {
        params.set('status', status);
    }

    const response = await fetch(`${API_BASE_URL}/api/v1/schedules?${params}`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to list schedules: ${response.statusText}`);
    }

    return response.json();
}

export async function getSchedule(scheduleId: string): Promise<ScheduleInfo> {
    const response = await fetch(`${API_BASE_URL}/api/v1/schedules/${scheduleId}`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to get schedule: ${response.statusText}`);
    }

    return response.json();
}

export async function getScheduleRuns(
    scheduleId: string,
    page: number = 1,
    pageSize: number = 20
): Promise<ScheduleRunsResponse> {
    const params = new URLSearchParams({
        page: String(page),
        page_size: String(pageSize),
    });

    const response = await fetch(`${API_BASE_URL}/api/v1/schedules/${scheduleId}/runs?${params}`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to get schedule runs: ${response.statusText}`);
    }

    return response.json();
}

export async function createSchedule(request: CreateScheduleRequest): Promise<ScheduleInfo> {
    const response = await fetch(`${API_BASE_URL}/api/v1/schedules`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            ...getAuthHeaders(),
        },
        body: JSON.stringify(request),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to create schedule: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function updateSchedule(
    scheduleId: string,
    request: UpdateScheduleRequest
): Promise<ScheduleInfo> {
    const response = await fetch(`${API_BASE_URL}/api/v1/schedules/${scheduleId}`, {
        method: "PUT",
        headers: {
            "Content-Type": "application/json",
            ...getAuthHeaders(),
        },
        body: JSON.stringify(request),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to update schedule: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function pauseSchedule(scheduleId: string, reason?: string): Promise<ScheduleInfo> {
    const response = await fetch(`${API_BASE_URL}/api/v1/schedules/${scheduleId}/pause`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            ...getAuthHeaders(),
        },
        body: JSON.stringify(reason ? { reason } : {}),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to pause schedule: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function resumeSchedule(scheduleId: string, reason?: string): Promise<ScheduleInfo> {
    const response = await fetch(`${API_BASE_URL}/api/v1/schedules/${scheduleId}/resume`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            ...getAuthHeaders(),
        },
        body: JSON.stringify(reason ? { reason } : {}),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to resume schedule: ${response.statusText} - ${errorText}`);
    }

    return response.json();
}

export async function deleteSchedule(scheduleId: string): Promise<void> {
    const response = await fetch(`${API_BASE_URL}/api/v1/schedules/${scheduleId}`, {
        method: "DELETE",
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to delete schedule: ${response.statusText} - ${errorText}`);
    }
}
