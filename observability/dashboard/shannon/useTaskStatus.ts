'use client';

import { useEffect, useState } from 'react';
import { fetchTaskStatus } from './api';
import type { TaskStatusResponse } from './types';

export function useTaskStatus(taskId: string | null, apiKey?: string) {
  const [data, setData] = useState<TaskStatusResponse | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!taskId) {
      setData(null);
      setError(null);
      setLoading(false);
      return;
    }

    let cancelled = false;
    const fetchStatus = async () => {
      setLoading(true);
      try {
        const response = await fetchTaskStatus(taskId, apiKey);
        if (!cancelled) {
          setData(response);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err : new Error('Failed to load status'));
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };

    fetchStatus();
    const interval = setInterval(fetchStatus, 1000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [taskId, apiKey]);

  return { data, error, loading };
}
