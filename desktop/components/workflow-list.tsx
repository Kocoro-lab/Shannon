'use client';

/**
 * Workflow List Component
 *
 * Displays a filterable and searchable list of workflows.
 * Supports both embedded and cloud modes via abstracted API.
 */

import { useCallback, useEffect, useState } from 'react';
import { WorkflowHistoryEntry, workflowAPI } from '@/lib/shannon/workflows';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Search, Filter, Download } from 'lucide-react';

interface WorkflowListProps {
  sessionId?: string;
  onWorkflowClick?: (workflowId: string) => void;
}

type WorkflowStatus = 'all' | 'running' | 'completed' | 'failed' | 'paused' | 'cancelled';

export function WorkflowList({ sessionId, onWorkflowClick }: WorkflowListProps) {
  const [workflows, setWorkflows] = useState<WorkflowHistoryEntry[]>([]);
  const [filteredWorkflows, setFilteredWorkflows] = useState<WorkflowHistoryEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<WorkflowStatus>('all');

  // Load workflows
  const loadWorkflows = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await workflowAPI.getHistory({
        session_id: sessionId,
        limit: 100,
      });
      setWorkflows(data);
      setFilteredWorkflows(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workflows');
    } finally {
      setLoading(false);
    }
  }, [sessionId]);

  useEffect(() => {
    loadWorkflows();
  }, [loadWorkflows]);

  // Filter workflows based on search and status
  useEffect(() => {
    let filtered = workflows;

    // Status filter
    if (statusFilter !== 'all') {
      filtered = filtered.filter(
        (w) => w.status.toLowerCase() === statusFilter.toLowerCase()
      );
    }

    // Search filter
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      filtered = filtered.filter(
        (w) =>
          w.workflow_id.toLowerCase().includes(query) ||
          w.pattern_type.toLowerCase().includes(query)
      );
    }

    setFilteredWorkflows(filtered);
  }, [workflows, searchQuery, statusFilter]);

  // Export workflow
  const handleExport = async (workflowId: string) => {
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
      console.error('Failed to export workflow:', err);
    }
  };

  // Status badge styling
  const getStatusBadge = (status: string) => {
    const statusLower = status.toLowerCase();
    const variants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
      running: 'default',
      completed: 'secondary',
      failed: 'destructive',
      paused: 'outline',
      cancelled: 'outline',
    };

    return (
      <Badge variant={variants[statusLower] || 'outline'}>
        {status}
      </Badge>
    );
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center p-8">
        <div className="text-muted-foreground">Loading workflows...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 text-destructive">
        <p>Error: {error}</p>
        <Button onClick={loadWorkflows} variant="outline" className="mt-2">
          Retry
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Filter Bar */}
      <div className="flex gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search workflows..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-8"
          />
        </div>

        <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v as WorkflowStatus)}>
          <SelectTrigger className="w-[180px]">
            <Filter className="mr-2 h-4 w-4" />
            <SelectValue placeholder="Filter by status" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Status</SelectItem>
            <SelectItem value="running">Running</SelectItem>
            <SelectItem value="completed">Completed</SelectItem>
            <SelectItem value="failed">Failed</SelectItem>
            <SelectItem value="paused">Paused</SelectItem>
            <SelectItem value="cancelled">Cancelled</SelectItem>
          </SelectContent>
        </Select>

        <Button onClick={loadWorkflows} variant="outline" size="icon">
          <span className="sr-only">Refresh</span>
          ⟳
        </Button>
      </div>

      {/* Workflow List */}
      {filteredWorkflows.length === 0 ? (
        <div className="text-center p-8 text-muted-foreground">
          No workflows found
        </div>
      ) : (
        <div className="space-y-2">
          {filteredWorkflows.map((workflow) => (
            <button
              key={workflow.workflow_id}
              type="button"
              className="w-full border rounded-lg p-4 hover:bg-accent cursor-pointer transition-colors text-left"
              onClick={() => onWorkflowClick?.(workflow.workflow_id)}
            >
              <div className="flex items-start justify-between">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <span className="font-mono text-sm text-muted-foreground">
                      {workflow.workflow_id.substring(0, 8)}
                    </span>
                    {getStatusBadge(workflow.status)}
                  </div>
                  <div className="text-sm text-muted-foreground">
                    Pattern: {workflow.pattern_type}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">
                    Created: {new Date(workflow.created_at).toLocaleString()}
                  </div>
                  {workflow.completed_at && (
                    <div className="text-xs text-muted-foreground">
                      Completed: {new Date(workflow.completed_at).toLocaleString()}
                    </div>
                  )}
                </div>

                <Button
                  variant="ghost"
                  size="sm"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleExport(workflow.workflow_id);
                  }}
                >
                  <Download className="h-4 w-4" />
                </Button>
              </div>
            </button>
          ))}
        </div>
      )}

      {/* Results Count */}
      <div className="text-sm text-muted-foreground text-center">
        Showing {filteredWorkflows.length} of {workflows.length} workflows
      </div>
    </div>
  );
}
