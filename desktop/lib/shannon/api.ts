/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { getAccessToken, getAPIKey } from "@/lib/auth";

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

// =============================================================================
// Auth Headers Helper
// =============================================================================

// Auth headers helper - uses API key if available, falls back to JWT token, then X-User-Id for dev
function getAuthHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};

    // Try API key first (preferred for OSS backend)
    const apiKey = getAPIKey();
    if (apiKey) {
        headers["X-API-Key"] = apiKey;
        return headers;
    }

    // Try JWT token (for authenticated users without API key)
    const token = getAccessToken();
    if (token) {
        headers["Authorization"] = `Bearer ${token}`;
        return headers;
    }

    // Fallback to X-User-Id for local development without auth
    // Default user ID matches migrations/postgres/003_authentication.sql seed data
    const userId = process.env.NEXT_PUBLIC_USER_ID;
    if (userId) {
        headers["X-User-Id"] = userId;
    }

    return headers;
}

// =============================================================================
// Auth Types
// =============================================================================

export interface AuthUserInfo {
    email: string;
    username: string;
    name?: string;
    picture?: string;
}

export interface AuthResponse {
    user_id: string;
    tenant_id: string;
    access_token: string;
    refresh_token: string;
    expires_in: number;
    api_key?: string;
    tier: string;
    is_new_user: boolean;
    quotas: Record<string, any>;
    user: AuthUserInfo;
}

export interface MeResponse {
    user_id: string;
    tenant_id: string;
    email: string;
    username: string;
    name?: string;
    picture?: string;
    tier: string;
    quotas: Record<string, any>;
    rate_limits: Record<string, any>;
}

// =============================================================================
// Auth API Functions
// =============================================================================

export async function register(
    email: string,
    username: string,
    password: string,
    fullName?: string
): Promise<AuthResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/auth/register`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify({
            email,
            username,
            password,
            full_name: fullName,
        }),
    });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `Registration failed: ${response.statusText}`);
    }

    return response.json();
}

export async function login(email: string, password: string): Promise<AuthResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/auth/login`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify({ email, password }),
    });

    if (!response.ok) {
        const errorData = await response.json().catch(() => ({}));
        throw new Error(errorData.message || `Login failed: ${response.statusText}`);
    }

    return response.json();
}

export async function refreshToken(refreshToken: string): Promise<{
    access_token: string;
    refresh_token: string;
    expires_in: number;
}> {
    const response = await fetch(`${API_BASE_URL}/api/v1/auth/refresh`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify({ refresh_token: refreshToken }),
    });

    if (!response.ok) {
        throw new Error("Token refresh failed");
    }

    return response.json();
}

export async function getCurrentUser(): Promise<MeResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/auth/me`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error("Failed to get current user");
    }

    return response.json();
}

// =============================================================================
// Task Types
// =============================================================================

export interface TaskSubmitRequest {
    prompt: string;  // Changed from 'query' to match backend API
    session_id?: string;
    task_type?: string;
    model?: string;
    max_tokens?: number;
    temperature?: number;
    system_prompt?: string;
    tools?: any[];
    metadata?: Record<string, any>;
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
            ...getAuthHeaders(),
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
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}`, {
        headers: getAuthHeaders(),
    });

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
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks?limit=${limit}&offset=${offset}`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to list tasks: ${response.statusText}`);
    }

    return response.json();
}

export function getStreamUrl(workflowId: string): string {
    const baseUrl = `${API_BASE_URL}/api/v1/stream/sse?workflow_id=${workflowId}`;

    // Add API key for SSE auth (EventSource can't use headers)
    const apiKey = getAPIKey();
    if (apiKey) {
        return `${baseUrl}&api_key=${encodeURIComponent(apiKey)}`;
    }

    // Fallback to token for SSE auth
    const token = getAccessToken();
    if (token) {
        return `${baseUrl}&token=${encodeURIComponent(token)}`;
    }

    return baseUrl;
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
    const response = await fetch(`${API_BASE_URL}/api/v1/sessions?limit=${limit}&offset=${offset}`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to list sessions: ${response.statusText}`);
    }

    return response.json();
}

export async function getSession(sessionId: string): Promise<Session> {
    const response = await fetch(`${API_BASE_URL}/api/v1/sessions/${sessionId}`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to get session: ${response.statusText}`);
    }

    return response.json();
}

export async function getSessionHistory(sessionId: string): Promise<SessionHistoryResponse> {
    const response = await fetch(`${API_BASE_URL}/api/v1/sessions/${sessionId}/history`, {
        headers: getAuthHeaders(),
    });

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

    const response = await fetch(`${API_BASE_URL}/api/v1/sessions/${sessionId}/events?${params.toString()}`, {
        headers: getAuthHeaders(),
    });

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
            ...getAuthHeaders(),
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
            ...getAuthHeaders(),
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
            ...getAuthHeaders(),
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
        headers: getAuthHeaders(),
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

// =============================================================================
// Task Progress Types
// =============================================================================

export interface TaskProgress {
    task_id: string;
    workflow_id: string;
    status: "pending" | "running" | "completed" | "failed" | "cancelled";
    progress_percent: number;
    current_step: string;
    total_steps: number;
    completed_steps: number;
    elapsed_time_ms: number;
    estimated_remaining_ms?: number;
    subtasks?: SubtaskProgress[];
    tokens_used: number;
    cost_usd: number;
    current_agent?: string;
    last_updated: string;
}

export interface SubtaskProgress {
    id: string;
    description: string;
    status: "pending" | "running" | "completed" | "failed";
    agent_id?: string;
}

// =============================================================================
// Task Polling API Functions
// =============================================================================

/**
 * Poll task progress - fallback for when SSE connection fails
 */
export async function pollTaskProgress(taskId: string): Promise<TaskProgress> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}/progress`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        // If progress endpoint doesn't exist, fall back to basic task info
        if (response.status === 404) {
            const task = await getTask(taskId);
            return {
                task_id: taskId,
                workflow_id: task.workflow_id || taskId,
                status: task.status as TaskProgress["status"],
                progress_percent: task.status === "completed" ? 100 : task.status === "running" ? 50 : 0,
                current_step: task.status === "running" ? "Processing..." : task.status,
                total_steps: 1,
                completed_steps: task.status === "completed" ? 1 : 0,
                elapsed_time_ms: 0,
                tokens_used: task.total_token_usage?.total_tokens || 0,
                cost_usd: task.total_token_usage?.cost_usd || 0,
                last_updated: new Date().toISOString(),
            };
        }
        throw new Error(`Failed to get task progress: ${response.statusText}`);
    }

    return response.json();
}

/**
 * Get task final output - useful when SSE missed the completion event
 */
export async function getTaskOutput(taskId: string): Promise<{
    task_id: string;
    output: string;
    status: string;
    tokens_used: number;
    cost_usd: number;
    completed_at?: string;
}> {
    const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}/output`, {
        headers: getAuthHeaders(),
    });

    if (!response.ok) {
        throw new Error(`Failed to get task output: ${response.statusText}`);
    }

    return response.json();
}
