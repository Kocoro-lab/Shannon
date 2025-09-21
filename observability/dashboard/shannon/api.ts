import type { TaskSubmitResponse, TaskStatusResponse, ListTasksResponse } from './types';

const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

function buildHeaders(apiKey?: string): HeadersInit {
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
  };
  if (apiKey && process.env.NEXT_PUBLIC_GATEWAY_SKIP_AUTH !== 'true') {
    headers['X-API-Key'] = apiKey;
  }
  return headers;
}

export async function submitTask(query: string, apiKey?: string): Promise<TaskSubmitResponse> {
  const response = await fetch(`${API_BASE_URL}/api/v1/tasks`, {
    method: 'POST',
    headers: buildHeaders(apiKey),
    body: JSON.stringify({ query }),
  });

  if (!response.ok) {
    const error = await response.text();
    throw new Error(`Task submission failed: ${response.status} ${error}`);
  }

  const workflowHeader = response.headers.get('X-Workflow-ID');
  const data = await response.json();
  const workflow_id = (workflowHeader ?? data.task_id ?? '').toString();
  if (!workflow_id) {
    throw new Error('Workflow identifier missing in response.');
  }
  return {
    ...data,
    workflow_id,
  };
}

export async function fetchTaskStatus(taskId: string, apiKey?: string): Promise<TaskStatusResponse> {
  const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}`, {
    method: 'GET',
    headers: buildHeaders(apiKey),
  });

  if (!response.ok) {
    throw new Error(`Failed to get task status: ${response.status}`);
  }

  return response.json();
}

export async function listTasks(params: {
  limit?: number;
  offset?: number;
  status?: string;
  session_id?: string;
}, apiKey?: string): Promise<ListTasksResponse> {
  const q = new URLSearchParams();
  if (params.limit != null) q.set('limit', String(params.limit));
  if (params.offset != null) q.set('offset', String(params.offset));
  if (params.status) q.set('status', params.status);
  if (params.session_id) q.set('session_id', params.session_id);
  const response = await fetch(`${API_BASE_URL}/api/v1/tasks?${q.toString()}`, {
    method: 'GET',
    headers: buildHeaders(apiKey),
  });
  if (!response.ok) {
    throw new Error(`Failed to list tasks: ${response.status}`);
  }
  return response.json();
}

export async function getTaskEvents(taskId: string, params: { limit?: number; offset?: number }, apiKey?: string) {
  const q = new URLSearchParams();
  if (params.limit != null) q.set('limit', String(params.limit));
  if (params.offset != null) q.set('offset', String(params.offset));
  const response = await fetch(`${API_BASE_URL}/api/v1/tasks/${taskId}/events?${q.toString()}`, {
    method: 'GET',
    headers: buildHeaders(apiKey),
  });
  if (!response.ok) {
    throw new Error(`Failed to get task events: ${response.status}`);
  }
  return response.json();
}

export async function buildTimeline(taskId: string, params: {
  run_id?: string;
  mode?: 'summary' | 'full';
  include_payloads?: boolean;
  persist?: boolean;
}, apiKey?: string): Promise<any> {
  const q = new URLSearchParams();
  if (params.run_id) q.set('run_id', params.run_id);
  if (params.mode) q.set('mode', params.mode);
  if (params.include_payloads != null) q.set('include_payloads', String(params.include_payloads));
  if (params.persist != null) q.set('persist', String(params.persist));
  const url = `${API_BASE_URL}/api/v1/tasks/${taskId}/timeline?${q.toString()}`;
  const response = await fetch(url, { method: 'GET', headers: buildHeaders(apiKey) });
  // 200 returns timeline inline, 202 accepted async persist
  const bodyText = await response.text();
  let body: any = {};
  try { body = bodyText ? JSON.parse(bodyText) : {}; } catch { body = { raw: bodyText }; }
  return { status: response.status, body };
}
