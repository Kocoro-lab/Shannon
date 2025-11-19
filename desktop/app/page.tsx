'use client';

import React, { useState, useCallback, useEffect } from 'react';
import { MessageList } from '@/components/chat/MessageList';
import { TaskSubmissionForm } from '@/components/chat/TaskSubmissionForm';
import { useAppDispatch, useAppSelector } from '@/lib/store/hooks';
import {
  setCurrentSession,
  addMessage,
  updateMessage,
  addEvent,
  clearEventsForWorkflow,
} from '@/lib/store/sessionSlice';
import {
  setCurrentWorkflow,
  setWorkflowStatus,
  addToHistory,
} from '@/lib/store/workflowSlice';
import { submitTask, getTask } from '@/lib/shannon/api';
import { useShannonStream, useLLMOutput } from '@/lib/shannon/stream';
import type { TaskSubmission, TaskStatusResponse } from '@/lib/shannon/types';

export default function HomePage() {
  const dispatch = useAppDispatch();
  const { messages, events: storedEvents, currentSessionId } = useAppSelector((state) => state.session);
  const { current } = useAppSelector((state) => state.workflow);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [currentAssistantMessageId, setCurrentAssistantMessageId] =
    useState<string | null>(null);

  // Stream events for current workflow (only when we have one)
  const { events, isConnected } = useShannonStream(current.workflowId, {
    autoConnect: true,
    onEvent: (event) => {
      // Add each event to Redux store as it arrives
      dispatch(addEvent(event));
    },
  });

  // Extract LLM output from events
  const { partialText, finalText, isStreaming } = useLLMOutput(events);

  // Update assistant message with streaming text
  useEffect(() => {
    if (!currentAssistantMessageId) return;
    if (!current.workflowId) return;
    if (!events.length) return;

    // Only update when the latest event belongs to the current workflow.
    // This prevents reusing output from a previous workflow when starting a new one.
    const latestWorkflowId = events[events.length - 1]?.workflow_id;
    if (latestWorkflowId !== current.workflowId) return;

    const displayText = finalText || partialText;
    if (displayText) {
      dispatch(
        updateMessage({
          id: currentAssistantMessageId,
          updates: { content: displayText },
        })
      );
    }
  }, [events, partialText, finalText, currentAssistantMessageId, current.workflowId, dispatch]);

  // Fetch final result when workflow completes
  useEffect(() => {
    if (!current.workflowId || current.status === 'completed') return;

    // Only fetch the final result once the workflow has actually completed.
    const hasWorkflowCompleted = events.some(
      (e) => e.type === 'WORKFLOW_COMPLETED'
    );
    if (!hasWorkflowCompleted) return;

    const fetchFinalResult = async () => {
      try {
        const task: TaskStatusResponse = await getTask(current.workflowId!);

        const isCompleted = task.status === 'COMPLETED';
        const isFailed = task.status === 'FAILED';

        if (isCompleted && task.result) {
          dispatch(setWorkflowStatus('completed'));

          if (currentAssistantMessageId) {
            dispatch(
              updateMessage({
                id: currentAssistantMessageId,
                updates: {
                  content: task.result,
                  metadata: {
                    tokens: task.usage?.total_tokens,
                    cost: task.usage?.estimated_cost || task.metadata?.cost_usd,
                    citations: task.metadata?.citations?.length,
                    mode: task.mode,
                  },
                },
              })
            );
          }

          dispatch(
            addToHistory({
              workflowId: current.workflowId!,
              query: task.query,
              status: task.status,
            })
          );
        } else if (isFailed) {
          dispatch(setWorkflowStatus('error'));
          if (currentAssistantMessageId) {
            dispatch(
              updateMessage({
                id: currentAssistantMessageId,
                updates: {
                  content: task.error || 'Task failed',
                },
              })
            );
          }
        }
      } catch (error) {
        console.error('Failed to fetch final result:', error);
        dispatch(setWorkflowStatus('error'));
      }
    };

    const timer = setTimeout(fetchFinalResult, 1000);
    return () => clearTimeout(timer);
  }, [events, current.workflowId, current.status, currentAssistantMessageId, dispatch]);

  const handleSubmit = useCallback(
    async (submission: TaskSubmission) => {
      setIsSubmitting(true);

      try {
        // Ensure we have a session ID for this window
        let sessionId = currentSessionId;
        if (!sessionId) {
          if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
            sessionId = crypto.randomUUID();
          } else {
            sessionId = `session-${Date.now()}`;
          }
          dispatch(setCurrentSession(sessionId));
        }

        // Add user message
        const userMessageId = `user-${Date.now()}`;
        dispatch(
          addMessage({
            id: userMessageId,
            role: 'user',
            content: submission.query,
            timestamp: new Date().toISOString(),
          })
        );

        // Submit task (using /api/v1/tasks/stream endpoint)
        const task = await submitTask({
          ...submission,
          session_id: sessionId,
        });

        // Clear events from previous workflow
        dispatch(clearEventsForWorkflow());

        // Set workflow (task.workflow_id is now guaranteed to exist)
        dispatch(setCurrentWorkflow(task.workflow_id));

        // Add placeholder assistant message
        const assistantMessageId = `assistant-${Date.now()}`;
        setCurrentAssistantMessageId(assistantMessageId);
        dispatch(
          addMessage({
            id: assistantMessageId,
            role: 'assistant',
            content: 'Processing...',
            timestamp: new Date().toISOString(),
            workflowId: task.workflow_id,
          })
        );
      } catch (error) {
        console.error('Failed to submit task:', error);
        dispatch(
          addMessage({
            id: `error-${Date.now()}`,
            role: 'assistant',
            content: 'Failed to submit task. Please try again.',
            timestamp: new Date().toISOString(),
          })
        );
      } finally {
        setIsSubmitting(false);
      }
    },
    [currentSessionId, dispatch]
  );

  return (
    <div className="flex flex-col h-screen">
      <header className="border-b p-4 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Shannon Desktop</h1>
          <p className="text-sm text-muted-foreground">
            Multi-agent AI orchestration
          </p>
        </div>
        <div className="flex items-center gap-2">
          {(() => {
            if (!current.workflowId) {
              return (
                <span className="text-sm text-muted-foreground">○ Idle</span>
              );
            }

            const hasWorkflowCompleted = events.some(
              (e) => e.type === 'WORKFLOW_COMPLETED'
            );

            if (current.status === 'completed' || hasWorkflowCompleted) {
              return (
                <span className="text-sm text-green-500">✓ Completed</span>
              );
            }

            return (
              <>
                <span
                  className={`text-sm ${
                    isConnected ? 'text-green-500' : 'text-muted-foreground'
                  }`}
                >
                  {isConnected ? '● Connected' : '○ Disconnected'}
                </span>
                {isStreaming && (
                  <span className="text-sm text-blue-500 animate-pulse">
                    ● Streaming...
                  </span>
                )}
              </>
            );
          })()}
        </div>
      </header>

      <main className="flex-1 flex flex-col overflow-hidden">
        <div className="flex-1 min-h-0">
          <MessageList
            messages={messages}
            events={storedEvents}
            currentWorkflowId={current.workflowId}
          />
        </div>

        <div className="p-4 border-t">
          <TaskSubmissionForm
            onSubmit={handleSubmit}
            isSubmitting={isSubmitting}
          />
        </div>
      </main>
    </div>
  );
}
