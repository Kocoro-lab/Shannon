/**
 * Workflow API client with support for both embedded and cloud modes.
 *
 * - Embedded mode: Uses Tauri commands for direct backend communication
 * - Cloud mode: Uses HTTP REST API to Shannon cloud service
 *
 * The implementation automatically detects the runtime environment.
 */

import { invoke } from '@tauri-apps/api/core';

// Check if running in Tauri (embedded mode) or browser (cloud mode)
const isTauri = typeof window !== 'undefined' && '__TAURI__' in window;

export interface WorkflowHistoryEntry {
  workflow_id: string;
  pattern_type: string;
  status: string;
  created_at: string;
  completed_at?: string;
}

export interface WorkflowDetails extends WorkflowHistoryEntry {
  input: string;
  output?: string;
  error?: string;
  session_id?: string;
  user_id: string;
}

export interface WorkflowStatusResponse {
  workflow_id: string;
  status: string;
  progress: number;
  output?: string;
  error?: string;
}

export interface SubmitWorkflowRequest {
  pattern_type: string;
  query: string;
  session_id?: string;
  mode?: string;
  model?: string;
}

export interface SubmitWorkflowResponse {
  workflow_id: string;
  status: string;
  submitted_at: string;
}

/**
 * Workflow API client.
 * Automatically uses Tauri commands (embedded) or HTTP (cloud).
 */
export class WorkflowAPI {
  private baseUrl?: string;

  constructor(baseUrl?: string) {
    this.baseUrl = baseUrl;
  }

  /**
   * Get workflow history for a session or user.
   */
  async getHistory(params?: {
    session_id?: string;
    limit?: number;
    status?: string;
  }): Promise<WorkflowHistoryEntry[]> {
    if (isTauri) {
      // Embedded mode: use Tauri command
      return invoke<WorkflowHistoryEntry[]>('get_workflow_history', {
        sessionId: params?.session_id,
        limit: params?.limit || 50,
      });
    } else {
      // Cloud mode: use HTTP API
      const query = new URLSearchParams();
      if (params?.session_id) query.set('session_id', params.session_id);
      if (params?.limit) query.set('limit', params.limit.toString());
      if (params?.status) query.set('status', params.status);

      const url = `${this.baseUrl || ''}/api/v1/tasks?${query}`;
      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to fetch workflow history: ${response.statusText}`);
      }
      return response.json();
    }
  }

  /**
   * Get workflow details by ID.
   */
  async getWorkflow(workflowId: string): Promise<WorkflowDetails> {
    if (isTauri) {
      // Embedded mode: use Tauri command
      return invoke<WorkflowDetails>('get_workflow_status', { workflowId });
    } else {
      // Cloud mode: use HTTP API
      const url = `${this.baseUrl || ''}/api/v1/tasks/${workflowId}`;
      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to fetch workflow: ${response.statusText}`);
      }
      return response.json();
    }
  }

  /**
   * Export workflow to JSON.
   */
  async exportWorkflow(workflowId: string): Promise<string> {
    if (isTauri) {
      // Embedded mode: use Tauri command
      return invoke<string>('export_workflow', { workflowId });
    } else {
      // Cloud mode: use HTTP API
      const url = `${this.baseUrl || ''}/api/v1/tasks/${workflowId}/export`;
      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`Failed to export workflow: ${response.statusText}`);
      }
      return response.text();
    }
  }

  /**
   * Submit a new workflow.
   */
  async submitWorkflow(request: SubmitWorkflowRequest): Promise<SubmitWorkflowResponse> {
    if (isTauri) {
      // Embedded mode: use Tauri command
      return invoke<SubmitWorkflowResponse>('submit_workflow', { request });
    } else {
      // Cloud mode: use HTTP API
      const url = `${this.baseUrl || ''}/api/v1/tasks`;
      const response = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(request),
      });
      if (!response.ok) {
        throw new Error(`Failed to submit workflow: ${response.statusText}`);
      }
      return response.json();
    }
  }

  /**
   * Pause a running workflow.
   */
  async pauseWorkflow(workflowId: string): Promise<void> {
    if (isTauri) {
      // Embedded mode: use Tauri command
      await invoke('pause_workflow', { workflowId });
    } else {
      // Cloud mode: use HTTP API
      const url = `${this.baseUrl || ''}/api/v1/tasks/${workflowId}/pause`;
      const response = await fetch(url, { method: 'POST' });
      if (!response.ok) {
        throw new Error(`Failed to pause workflow: ${response.statusText}`);
      }
    }
  }

  /**
   * Resume a paused workflow.
   */
  async resumeWorkflow(workflowId: string): Promise<void> {
    if (isTauri) {
      // Embedded mode: use Tauri command
      await invoke('resume_workflow', { workflowId });
    } else {
      // Cloud mode: use HTTP API
      const url = `${this.baseUrl || ''}/api/v1/tasks/${workflowId}/resume`;
      const response = await fetch(url, { method: 'POST' });
      if (!response.ok) {
        throw new Error(`Failed to resume workflow: ${response.statusText}`);
      }
    }
  }

  /**
   * Cancel a running workflow.
   */
  async cancelWorkflow(workflowId: string): Promise<void> {
    if (isTauri) {
      // Embedded mode: use Tauri command
      await invoke('cancel_workflow', { workflowId });
    } else {
      // Cloud mode: use HTTP API
      const url = `${this.baseUrl || ''}/api/v1/tasks/${workflowId}/cancel`;
      const response = await fetch(url, { method: 'POST' });
      if (!response.ok) {
        throw new Error(`Failed to cancel workflow: ${response.statusText}`);
      }
    }
  }

  /**
   * Check if running in embedded (Tauri) mode.
   */
  static isEmbedded(): boolean {
    return isTauri;
  }
}

// Default instance
export const workflowAPI = new WorkflowAPI();
