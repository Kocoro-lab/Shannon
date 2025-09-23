import { createStore } from 'zustand/vanilla';
import type { Agent, AppState, ProjectMetrics, WorkItem } from './types';
import { DEFAULT_SEED, RUNNING_DEFAULT } from './config';

type PartialWithId<T extends { id: string }> = Partial<T> & { id: string };

export interface UIState extends AppState {
  lastTickId: number;
  // Currently selected plan name (for UI display)
  plan_name?: string;
  // UI: whether radar ping sound is enabled
  pingAudioEnabled: boolean;
  // Reducers
  applySnapshot: (snapshot: AppState) => void;
  applyTick: (diff: {
    tick_id: number;
    items?: PartialWithId<WorkItem>[];
    agents?: PartialWithId<Agent>[];
    metrics?: Partial<ProjectMetrics>;
    agents_remove?: string[];
  }) => void;
  setPlanName: (name: string) => void;
  setPingAudioEnabled: (enabled: boolean) => void;
  togglePingAudio: () => void;
}

const emptyMetrics: ProjectMetrics = {
  active_agents: 0,
  total_tokens: 0,
  total_spend_usd: 0,
  live_tps: 0,
  live_spend_per_s: 0,
  completion_rate: 0,
};

export function createAppStore(initial?: Partial<AppState>) {
  return createStore<UIState>()((set, get) => ({
    items: initial?.items ?? {},
    agents: initial?.agents ?? {},
    metrics: initial?.metrics ?? emptyMetrics,
    seed: initial?.seed ?? DEFAULT_SEED,
    running: initial?.running ?? RUNNING_DEFAULT,
    lastTickId: 0,
    plan_name: initial && (initial as UIState).plan_name,
    pingAudioEnabled: false,

    applySnapshot(snapshot) {
      set({
        items: snapshot.items ?? {},
        agents: snapshot.agents ?? {},
        metrics: snapshot.metrics ?? emptyMetrics,
        seed: snapshot.seed,
        running: snapshot.running,
        lastTickId: 0, // reset ordering on fresh snapshot
      });
    },

    applyTick(diff) {
      if (diff.tick_id <= get().lastTickId) return; // ignore out-of-order

      set((state) => {
        // Clone maps shallowly before mutating
        const items = { ...state.items } as Record<string, WorkItem>;
        const agents = { ...state.agents } as Record<string, Agent>;

        if (diff.items) {
          for (const patch of diff.items) {
            const id = patch.id;
            const prev = items[id] ?? ({ id } as WorkItem);
            items[id] = { ...prev, ...patch } as WorkItem;
          }
        }

        if (diff.agents) {
          for (const patch of diff.agents) {
            const id = patch.id;
            const prev = agents[id] ?? ({ id } as Agent);
            agents[id] = { ...prev, ...patch } as Agent;
          }
        }

        if (diff.agents_remove && diff.agents_remove.length) {
          for (const id of diff.agents_remove) {
            if (id in agents) delete agents[id];
          }
        }

        const metrics = diff.metrics ? { ...state.metrics, ...diff.metrics } : state.metrics;

        return { items, agents, metrics, lastTickId: diff.tick_id };
      });
    },

    setPlanName(name: string) {
      set({ plan_name: name });
    },

    setPingAudioEnabled(enabled: boolean) {
      set({ pingAudioEnabled: !!enabled });
    },

    togglePingAudio() {
      const cur = get().pingAudioEnabled;
      set({ pingAudioEnabled: !cur });
    },
  }));
}

// App-level singleton store (UI can import this).
export const appStore = createAppStore();
