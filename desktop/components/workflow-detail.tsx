'use client';

/**
 * Workflow Detail Component
 *
 * Displays detailed information about a workflow including:
 * - Timeline of events
 * - Status and progress
 * - Input and output
 * - Error details if failed
 *
 * Works in both embedded and cloud modes.
 */

import { useCallback, useEffect, useState } from 'react';
import { WorkflowDetails, workflowAPI } from '@/lib/shannon/workflows';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Download, RefreshCw, Play, Pause, X } from 'lucide-react';

interface WorkflowDetailProps {
  workflowId: string;
  onClose?: () => void;
}

export function WorkflowDetail({ workflowId, onClose }: WorkflowDetailProps) {
  const [workflow, setWorkflow] = useState<WorkflowDetails | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadWorkflow = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await workflowAPI.getWorkflow(workflowId);
      setWorkflow(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workflow');
    } finally {
      setLoading(false);
    }
  }, [workflowId]);

  useEffect(() => {
    loadWorkflow();
  }, [loadWorkflow]);

  const handleExport = async () => {
    try {
      const json = await workflowAPI.exportWorkflow(workflowId);
      const blob = new Blob([json], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `workflow-${workflowId}.json`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      console.error('Failed to export:', err);
    }
  };

  const handlePause = async () => {
    try {
      await workflowAPI.pauseWorkflow(workflowId);
      await loadWorkflow();
    } catch (err) {
      console.error('Failed to pause:', err);
    }
  };

  const handleResume = async () => {
    try {
      await workflowAPI.resumeWorkflow(workflowId);
      await loadWorkflow();
    } catch (err) {
      console.error('Failed to resume:', err);
    }
  };

  const handleCancel = async () => {
    try {
      await workflowAPI.cancelWorkflow(workflowId);
      await loadWorkflow();
    } catch (err) {
      console.error('Failed to cancel:', err);
    }
  };

  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center p-8">
          <div className="text-muted-foreground">Loading workflow details...</div>
        </CardContent>
      </Card>
    );
  }

  if (error || !workflow) {
    return (
      <Card>
        <CardContent className="p-4">
          <p className="text-destructive">Error: {error || 'Workflow not found'}</p>
          <Button onClick={loadWorkflow} variant="outline" className="mt-2">
            Retry
          </Button>
        </CardContent>
      </Card>
    );
  }

  const statusLower = workflow.status.toLowerCase();
  const isRunning = statusLower === 'running';
  const isPaused = statusLower === 'paused';
  const canControl = isRunning || isPaused;

  return (
    <div className="space-y-4">
      {/* Header */}
      <Card>
        <CardHeader>
          <div className="flex items-start justify-between">
            <div>
              <CardTitle className="font-mono text-lg">
                {workflow.workflow_id}
              </CardTitle>
              <CardDescription>{workflow.pattern_type}</CardDescription>
            </div>
            <div className="flex gap-2">
              {canControl && (
                <>
                  {isRunning && (
                    <Button
                      onClick={handlePause}
                      size="sm"
                      variant="outline"
                    >
                      <Pause className="h-4 w-4 mr-1" />
                      Pause
                    </Button>
                  )}
                  {isPaused && (
                    <Button
                      onClick={handleResume}
                      size="sm"
                      variant="outline"
                    >
                      <Play className="h-4 w-4 mr-1" />
                      Resume
                    </Button>
                  )}
                  <Button
                    onClick={handleCancel}
                    size="sm"
                    variant="destructive"
                  >
                    <X className="h-4 w-4 mr-1" />
                    Cancel
                  </Button>
                </>
              )}
              <Button onClick={handleExport} size="sm" variant="outline">
                <Download className="h-4 w-4 mr-1" />
                Export
              </Button>
              <Button onClick={loadWorkflow} size="sm" variant="ghost">
                <RefreshCw className="h-4 w-4" />
              </Button>
              {onClose && (
                <Button onClick={onClose} size="sm" variant="ghost">
                  <X className="h-4 w-4" />
                </Button>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <div className="text-sm text-muted-foreground">Status</div>
              <Badge variant={statusLower === 'completed' ? 'secondary' :
                            statusLower === 'failed' ? 'destructive' : 'default'}>
                {workflow.status}
              </Badge>
            </div>
            <div>
              <div className="text-sm text-muted-foreground">Created</div>
              <div className="text-sm">
                {new Date(workflow.created_at).toLocaleString()}
              </div>
            </div>
            {workflow.completed_at && (
              <div>
                <div className="text-sm text-muted-foreground">Completed</div>
                <div className="text-sm">
                  {new Date(workflow.completed_at).toLocaleString()}
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Tabs */}
      <Tabs defaultValue="details" className="w-full">
        <TabsList>
          <TabsTrigger value="details">Details</TabsTrigger>
          <TabsTrigger value="input">Input</TabsTrigger>
          <TabsTrigger value="output">Output</TabsTrigger>
          <TabsTrigger value="timeline">Timeline</TabsTrigger>
        </TabsList>

        <TabsContent value="details" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Workflow Information</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              <div>
                <div className="text-sm text-muted-foreground">Pattern Type</div>
                <div>{workflow.pattern_type}</div>
              </div>
              <div>
                <div className="text-sm text-muted-foreground">Workflow ID</div>
                <div className="font-mono text-sm">{workflow.workflow_id}</div>
              </div>
              {workflow.session_id && (
                <div>
                  <div className="text-sm text-muted-foreground">Session ID</div>
                  <div className="font-mono text-sm">{workflow.session_id}</div>
                </div>
              )}
              <div>
                <div className="text-sm text-muted-foreground">User ID</div>
                <div className="font-mono text-sm">{workflow.user_id}</div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="input">
          <Card>
            <CardHeader>
              <CardTitle>Workflow Input</CardTitle>
            </CardHeader>
            <CardContent>
              <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                {workflow.input || 'No input data'}
              </pre>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="output">
          <Card>
            <CardHeader>
              <CardTitle>Workflow Output</CardTitle>
            </CardHeader>
            <CardContent>
              {workflow.output ? (
                <pre className="bg-muted p-4 rounded-lg overflow-x-auto text-sm">
                  {workflow.output}
                </pre>
              ) : workflow.error ? (
                <div className="text-destructive">
                  <div className="font-semibold mb-2">Error:</div>
                  <pre className="bg-destructive/10 p-4 rounded-lg overflow-x-auto text-sm">
                    {workflow.error}
                  </pre>
                </div>
              ) : (
                <div className="text-muted-foreground">
                  No output yet (workflow may still be running)
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="timeline">
          <Card>
            <CardHeader>
              <CardTitle>Event Timeline</CardTitle>
              <CardDescription>
                Detailed execution history (feature coming soon)
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="text-muted-foreground text-sm">
                Event timeline visualization will be available in the next update.
                Use Export to view full event history.
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
