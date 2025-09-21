import type { TaskSubmitResponse, TaskStatusResponse } from './types';

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
