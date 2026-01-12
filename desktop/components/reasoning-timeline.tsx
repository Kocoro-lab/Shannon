'use client';

/**
 * Reasoning Timeline Component
 *
 * Displays step-by-step reasoning for Chain of Thought and Tree of Thoughts patterns.
 * Shows each reasoning step with timestamp and confidence score.
 */

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { CheckCircle2, Circle } from 'lucide-react';

export interface ReasoningStep {
  step: number;
  step_type: string;
  content: string;
  timestamp: string;
  confidence?: number;
}

interface ReasoningTimelineProps {
  steps: ReasoningStep[];
  currentStep?: number;
}

export function ReasoningTimeline({ steps, currentStep }: ReasoningTimelineProps) {
  if (steps.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Reasoning Steps</CardTitle>
          <CardDescription>No reasoning steps yet</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Reasoning Timeline</CardTitle>
        <CardDescription>
          {steps.length} step{steps.length !== 1 ? 's' : ''} completed
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {steps.map((step, index) => {
            const isComplete = !currentStep || step.step <= currentStep;
            const isCurrent = currentStep && step.step === currentStep;

            return (
              <div key={step.step} className="flex gap-4">
                {/* Timeline Marker */}
                <div className="flex flex-col items-center">
                  {isComplete ? (
                    <CheckCircle2 className={`h-6 w-6 ${isCurrent ? 'text-primary' : 'text-muted-foreground'}`} />
                  ) : (
                    <Circle className="h-6 w-6 text-muted-foreground" />
                  )}
                  {index < steps.length - 1 && (
                    <div className="w-0.5 h-full bg-border mt-2" />
                  )}
                </div>

                {/* Step Content */}
                <div className="flex-1 pb-4">
                  <div className="flex items-start justify-between mb-2">
                    <div>
                      <div className="font-semibold">
                        Step {step.step}: {step.step_type}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {new Date(step.timestamp).toLocaleTimeString()}
                      </div>
                    </div>
                    {step.confidence !== undefined && (
                      <Badge variant="outline">
                        {(step.confidence * 100).toFixed(0)}% confidence
                      </Badge>
                    )}
                  </div>
                  <div className="text-sm text-muted-foreground whitespace-pre-wrap">
                    {step.content}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </CardContent>
    </Card>
  );
}
