'use client';

import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import { listTasks } from './api';

interface ActiveTask {
  task_id: string;
  query?: string;
  created_at?: Date;
}

interface UsePlatformTasksOptions {
  pollInterval?: number;
  includeStatuses?: string[];
}

/**
 * Hook to continuously poll for active workflows across the platform.
 * Returns list of currently running/active task IDs for multi-stream monitoring.
 */
let renderCount = 0;

export function usePlatformTasks(
  apiKey?: string,
  options?: UsePlatformTasksOptions
) {
  renderCount++;
  console.log(`[usePlatformTasks] RENDER #${renderCount} - apiKey: ${apiKey ? 'present' : 'missing'}`);

  const [activeTasks, setActiveTasks] = useState<ActiveTask[]>([]);
  const [isPolling, setIsPolling] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const backoffDelayRef = useRef(0);
  const isPollingRef = useRef(false);
  const intervalRef = useRef<NodeJS.Timeout | null>(null);
  const pollInterval = options?.pollInterval ?? 15000; // Default: 15 seconds

  // Memoize to prevent new array on every render
  const includeStatuses = useMemo(
    () => options?.includeStatuses ?? ['RUNNING', 'QUEUED'],
    [options?.includeStatuses]
  );

  useEffect(() => {
    console.log(`[usePlatformTasks] Effect running - apiKey: ${apiKey ? 'present' : 'missing'}, interval exists: ${!!intervalRef.current}`);

    const fetchActiveTasks = async () => {
      // Prevent concurrent polls
      if (isPollingRef.current) {
        console.log('[usePlatformTasks] Skipping poll - already in progress');
        return;
      }

      if (!apiKey && process.env.NEXT_PUBLIC_GATEWAY_SKIP_AUTH !== 'true') {
        console.log('[usePlatformTasks] Skipping poll - no API key and auth required');
        return;
      }

      try {
        isPollingRef.current = true;
        setIsPolling(true);
        setError(null);

        // Fetch all tasks for each status
        const allTasks: ActiveTask[] = [];
        const ONE_HOUR_AGO = Date.now() - 60 * 60 * 1000;

        for (const status of includeStatuses) {
          console.log(`[usePlatformTasks] Fetching tasks with status: ${status}`);
          const response = await listTasks(
            { status, limit: 100, offset: 0 },
            apiKey
          );
          console.log(`[usePlatformTasks] Received ${response.tasks?.length || 0} tasks for status ${status}`);
          if (response.tasks) {
            allTasks.push(
              ...response.tasks
                .filter((t) => {
                  // Filter out tasks older than 1 hour to avoid stale RUNNING workflows
                  if (!t.created_at) return false;
                  const taskTime = new Date(t.created_at).getTime();
                  const isRecent = taskTime > ONE_HOUR_AGO;
                  if (!isRecent) {
                    console.log(`[usePlatformTasks] Filtering out stale task: ${t.task_id} (created ${new Date(t.created_at).toISOString()})`);
                  }
                  return isRecent;
                })
                .map((t) => ({
                  task_id: t.task_id,
                  query: t.query,
                  created_at: t.created_at ? new Date(t.created_at) : undefined,
                }))
            );
          }
        }

        // Deduplicate by task_id
        const uniqueTasks = Array.from(
          new Map(allTasks.map((t) => [t.task_id, t])).values()
        );

        console.log(`[usePlatformTasks] Setting ${uniqueTasks.length} unique active tasks`, uniqueTasks.map(t => t.task_id));
        setActiveTasks(uniqueTasks);
        backoffDelayRef.current = 0; // Reset backoff on success
      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : 'Failed to fetch tasks';

        // Handle rate limiting with exponential backoff
        if (errorMsg.includes('429')) {
          const newBackoff = Math.min(backoffDelayRef.current === 0 ? 5000 : backoffDelayRef.current * 2, 60000);
          backoffDelayRef.current = newBackoff;
          console.warn(`[usePlatformTasks] Rate limited (429) - backing off for ${newBackoff}ms`);
          setError(`Rate limited - retrying in ${newBackoff / 1000}s`);
        } else {
          setError(errorMsg);
          console.error('[usePlatformTasks] Poll error:', err);
        }
      } finally {
        isPollingRef.current = false;
        setIsPolling(false);
      }
    };

    // Prevent duplicate intervals
    if (intervalRef.current) {
      console.log('[usePlatformTasks] Interval already exists, skipping setup');
      return;
    }

    console.log(`[usePlatformTasks] Setting up polling with interval: ${pollInterval}ms`);

    // Initial fetch
    fetchActiveTasks();

    // Set up polling interval
    intervalRef.current = setInterval(() => {
      // Apply backoff if needed
      if (backoffDelayRef.current > 0) {
        console.log(`[usePlatformTasks] Backoff active (${backoffDelayRef.current}ms) - skipping this poll`);
        backoffDelayRef.current = Math.max(0, backoffDelayRef.current - pollInterval);
        return;
      }
      fetchActiveTasks();
    }, pollInterval);

    return () => {
      console.log(`[usePlatformTasks] CLEANUP running - clearing interval: ${!!intervalRef.current}`);
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [apiKey, pollInterval]);

  return {
    activeTasks,
    activeWorkflowIds: activeTasks.map((t) => t.task_id),
    isPolling,
    error,
  };
}
