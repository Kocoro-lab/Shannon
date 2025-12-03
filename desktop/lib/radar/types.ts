// Core types for radar visualization

export type Status = "queued" | "assigned" | "in_progress" | "blocked" | "done";

export interface WorkItem {
  id: string;
  group: string;
  sector: string;
  depends_on: string[];
  desc?: string;

  estimate_ms: number;
  started_at?: number;
  eta_ms?: number;

  tps_min: number;
  tps_max: number;
  tps: number;
  tokens_done: number;
  est_tokens: number;

  status: Status;
  agent_id?: string;
}

export interface Agent {
  id: string;
  work_item_id: string;
  x: number;
  y: number;
  v: number;
  curve_phase: number;
}

export interface ProjectMetrics {
  active_agents: number;
  total_tokens: number;
  total_spend_usd: number;
  live_tps: number;
  live_spend_per_s: number;
  completion_rate: number;
}

export interface AppState {
  items: Record<string, WorkItem>;
  agents: Record<string, Agent>;
  metrics: ProjectMetrics;
  seed: string;
  running: boolean;
}

