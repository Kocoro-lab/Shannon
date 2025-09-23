// Centralized runtime configuration and tuning knobs

// Bridge batching cadence (ms) and store flush cadence (ms) are centralized in constants.
// See constants for guidance on tuning.
export { BRIDGE_BATCH_MS, STORE_FLUSH_INTERVAL_MS } from './constants';

// Engine tick rate is now centralized in constants for simplicity.
export { ENGINE_TICK_HZ } from './constants';

// App defaults
export const DEFAULT_SEED = (process.env.NEXT_PUBLIC_DEFAULT_SEED as string) || 'auto';
export const RUNNING_DEFAULT = ((process.env.NEXT_PUBLIC_RUNNING_DEFAULT as string) ?? 'true') === 'true';

// Debug logging toggle
export const DEBUG_LOGS = ((process.env.NEXT_PUBLIC_DEBUG_LOGS as string) ?? 'true') === 'true';
