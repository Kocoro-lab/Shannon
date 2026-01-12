'use client';

/**
 * Workflow History Page
 *
 * Browse and manage workflow history with filtering, search, and detail view.
 * Supports both embedded (Tauri) and cloud modes via abstracted API layer.
 */

import { useState } from 'react';
import { WorkflowList } from '@/components/workflow-list';
import { WorkflowDetail } from '@/components/workflow-detail';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export default function WorkflowHistoryPage() {
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(null);

  return (
    <div className="container mx-auto p-6 space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Workflow History</CardTitle>
          <CardDescription>
            Browse and manage your workflow executions
          </CardDescription>
        </CardHeader>
        <CardContent>
          {!selectedWorkflowId ? (
            <WorkflowList onWorkflowClick={setSelectedWorkflowId} />
          ) : (
            <div>
              <button
                type="button"
                onClick={() => setSelectedWorkflowId(null)}
                className="mb-4 text-sm text-muted-foreground hover:text-foreground"
              >
                ← Back to list
              </button>
              <WorkflowDetail
                workflowId={selectedWorkflowId}
                onClose={() => setSelectedWorkflowId(null)}
              />
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
