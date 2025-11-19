'use client';

import React, { useState, useCallback } from 'react';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import type { TaskSubmission } from '@/lib/shannon/types';

interface TaskSubmissionFormProps {
  onSubmit: (submission: TaskSubmission) => void;
  isSubmitting?: boolean;
}

export function TaskSubmissionForm({
  onSubmit,
  isSubmitting = false,
}: TaskSubmissionFormProps) {
  const [query, setQuery] = useState('');
  const [researchStrategy, setResearchStrategy] = useState<
    'quick' | 'standard' | 'deep' | 'academic'
  >('standard');
  const [modelTier, setModelTier] = useState<'small' | 'medium' | 'large'>(
    'medium'
  );

  const handleSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault();
    if (!query.trim()) return;

    onSubmit({
      query: query.trim(),
      research_strategy: researchStrategy,
      model_tier: modelTier,
    });

    setQuery('');
  }, [query, researchStrategy, modelTier, onSubmit]);

  const handleStrategyChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    setResearchStrategy(e.target.value as 'quick' | 'standard' | 'deep' | 'academic');
  }, []);

  const handleTierChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    setModelTier(e.target.value as 'small' | 'medium' | 'large');
  }, []);

  return (
    <Card className="p-4">
      <form onSubmit={handleSubmit} className="space-y-4">
        <fieldset disabled={isSubmitting} className="space-y-4">
          <div className="flex gap-2">
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Enter your query..."
              className="flex-1"
            />
            <Button type="submit" disabled={isSubmitting || !query.trim()}>
              {isSubmitting ? 'Submitting...' : 'Submit'}
            </Button>
          </div>

          <div className="flex gap-2">
            <div className="flex-1">
              <select
                value={researchStrategy}
                onChange={handleStrategyChange}
                className="flex h-9 w-full items-center justify-between rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm ring-offset-background placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              >
                <option value="quick">Quick</option>
                <option value="standard">Standard</option>
                <option value="deep">Deep</option>
                <option value="academic">Academic</option>
              </select>
            </div>

            <div className="flex-1">
              <select
                value={modelTier}
                onChange={handleTierChange}
                className="flex h-9 w-full items-center justify-between rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm ring-offset-background placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              >
                <option value="small">Small</option>
                <option value="medium">Medium</option>
                <option value="large">Large</option>
              </select>
            </div>
          </div>
        </fieldset>
      </form>
    </Card>
  );
}
