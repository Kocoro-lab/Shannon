// Core TypeScript types for the Calming Control Room

export type Status = 'queued' | 'assigned' | 'in_progress' | 'blocked' | 'done';

export interface WorkItem {
  id: string;            // e.g., "A1", "B3"
  group: string;         // 'A', 'B', ... (for grouping/legend)
  sector: string;        // e.g., 'Planning', 'Build', 'Eval', 'Deploy'
  depends_on: string[];  // list of WorkItem ids
  desc?: string;         // optional human-friendly work order description

  estimate_ms: number;   // target duration for this item (ms)
  started_at?: number;   // epoch ms when entered in_progress
  eta_ms?: number;       // rolling ETA in ms (recomputed)

  tps_min: number;       // tokens/sec lower bound for this item
  tps_max: number;       // tokens/sec upper bound
  tps: number;           // current tokens/sec (dynamic within [min,max])
  tokens_done: number;   // cumulative tokens produced for this item
  est_tokens: number;    // derived from estimate + nominal tps

  status: Status;
  agent_id?: string;     // set when in_progress
}

export interface Agent {
  id: string;            // e.g., 'P1','D2','E3','Q7','X584'
  work_item_id: string;  // current assignment
  // Radar motion state (normalized world coords in [-1,1])
  x: number;
  y: number;             // current position
  v: number;             // scalar speed (units/frame) mapped from tps
  curve_phase: number;   // 0..1 for bezier curvature evolution
}

export interface ProjectMetrics {
  active_agents: number;
  total_tokens: number;    // cumulative across all time
  total_spend_usd: number; // cumulative spend
  live_tps: number;        // sum of in_progress tps
  live_spend_per_s: number;
  completion_rate: number; // done / eligible (0..1)
}

export interface AppState {
  items: Record<string, WorkItem>;
  agents: Record<string, Agent>;
  metrics: ProjectMetrics;
  seed: string;
  running: boolean;
}

// Keep this flag export to satisfy existing stub import tests
export const TYPES_MODULE_LOADED = true;
