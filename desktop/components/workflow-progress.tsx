'use client';

/**
 * Workflow Progress Component
 *
 * Real-time progress visualization for running workflows.
 * Shows progress bar, current step, and control buttons.
 * Works in both embedded and cloud modes.
 */

import { useCallback, useEffect, useState } from 'react';
import { listen } from '@tauri-apps/api/event';
import { WorkflowStatusResponse, workflowAPI } from '@/lib/shannon/workflows';
import { Progress } from '@/components/ui/progress';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Pause, Play, X, AlertCircle } from 'lucide-react';

interface WorkflowProgressProps {
  workflowId: string;
  onComplete?: () => void;
  onError?: (error: string) => void;
}

export function WorkflowProgress({
  workflowId,
  onComplete,
  onError,
}: WorkflowProgressProps) {
  const [progress, setProgress] = useState(0);
  const [currentStep, setCurrentStep] = useState<string>('Initializing...');
  const [status, setStatus] = useState<string>('running');
  const [isPaused, setIsPaused] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Poll for status updates (fallback for cloud mode)
  const pollStatus = useCallback(async () => {
    try {
      const response = await workflowAPI.getWorkflow(workflowId);
      setProgress(response.progress || 0);
      setStatus(response.status);
      setIsPaused(response.status.toLowerCase() === 'paused');

      if (response.error) {
        setError(response.error);
        onError?.(response.error);
      }

      if (response.status.toLowerCase() === 'completed') {
        onComplete?.();
      }
    } catch (err) {
      console.error('Failed to poll status:', err);
    }
  }, [workflowId, onComplete, onError]);

  // Subscribe to real-time events (embedded mode)
  useEffect(() => {
    const isTauri = workflowAPI.isEmbedded();

    if (isTauri) {
      // Embedded mode: listen to Tauri events
      let unlisten: (() => void) | null = null;

      const setupListener = async () => {
        try {
          const eventName = `workflow-event-${workflowId}`;
          unlisten = await listen(eventName, (event) => {
            const payload = event.payload as any;

            // Update progress based on event type
            if (payload.Progress) {
              setProgress(payload.Progress.percent);
              setCurrentStep(payload.Progress.message || 'Processing...');
            } else if (payload.WorkflowCompleted) {
              setProgress(100);
              setStatus('completed');
              onComplete?.();
            } else if (payload.WorkflowFailed) {
              setStatus('failed');
              setError(payload.WorkflowFailed.error);
              onError?.(payload.WorkflowFailed.error);
            } else if (payload.WorkflowPaused) {
              setIsPaused(true);
              setStatus('paused');
            } else if (payload.WorkflowResumed) {
              setIsPaused(false);
              setStatus('running');
            }
          });
        } catch (err) {
          console.error('Failed to setup event listener:', err);
        }
      };

      setupListener();

      return () => {
        unlisten?.();
      };
    } else {
      // Cloud mode: poll for updates
      const interval = setInterval(pollStatus, 1000);
      return () => clearInterval(interval);
    }
  }, [workflowId, pollStatus, onComplete, onError]);

  const handlePause = async () => {
    try {
      await workflowAPI.pauseWorkflow(workflowId);
      setIsPaused(true);
      setStatus('paused');
    } catch (err) {
      console.error('Failed to pause:', err);
    }
  };

  const handleResume = async () => {
    try {
      await workflowAPI.resumeWorkflow(workflowId);
      setIsPaused(false);
      setStatus('running');
    } catch (err) {
      console.error('Failed to resume:', err);
    }
  };

  const handleCancel = async () => {
    try {
      await workflowAPI.cancelWorkflow(workflowId);
      setStatus('cancelled');
    } catch (err) {
      console.error('Failed to cancel:', err);
    }
  };

  const isRunning = status.toLowerCase() === 'running';
  const isCompleted = status.toLowerCase() === 'completed';
  const isFailed = status.toLowerCase() === 'failed';

  return (
    <Card>
      <CardHeader>
        <CardTitle>Workflow Progress</CardTitle>
        <CardDescription>
          {workflowId.substring(0, 16)}...
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Progress Bar */}
        <div className="space-y-2">
          <div className="flex justify-between text-sm">
            <span className="text-muted-foreground">{currentStep}</span>
            <span className="font-medium">{progress}%</span>
          </div>
          <Progress value={progress} className="h-2" />
        </div>

        {/* Error Display */}
        {error && (
          <div className="flex items-start gap-2 p-3 bg-destructive/10 border border-destructive/20 rounded-lg">
            <AlertCircle className="h-4 w-4 text-destructive mt-0.5" />
            <div className="flex-1">
              <div className="font-semibold text-destructive text-sm">Error</div>
              <div className="text-sm text-destructive/80">{error}</div>
            </div>
          </div>
        )}

        {/* Control Buttons */}
        <div className="flex gap-2">
          {isRunning && !isPaused && (
            <Button onClick={handlePause} variant="outline" size="sm">
              <Pause className="h-4 w-4 mr-1" />
              Pause
            </Button>
          )}
          {isPaused && (
            <Button onClick={handleResume} variant="outline" size="sm">
              <Play className="h-4 w-4 mr-1" />
              Resume
            </Button>
          )}
          {(isRunning || isPaused) && (
            <Button onClick={handleCancel} variant="destructive" size="sm">
              <X className="h-4 w-4 mr-1" />
              Cancel
            </Button>
          )}
          {isCompleted && (
            <div className="text-sm text-green-600 font-medium">
              ✓ Completed
            </div>
          )}
          {isFailed && (
            <div className="text-sm text-destructive font-medium">
              ✗ Failed
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
