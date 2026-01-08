/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { useEffect, useRef, useCallback } from "react";
import { getStreamUrl, pollTaskProgress, type TaskProgress } from "./api";
import { useDispatch } from "react-redux";
import { setConnectionState, setStreamError } from "../features/runSlice";

const MAX_RECONNECT_DELAY_MS = 10000;
const BASE_RECONNECT_DELAY_MS = 1000;
const POLL_INTERVAL_MS = 5000; // Poll every 5 seconds when SSE fails
const MAX_SSE_FAILURES_BEFORE_POLLING = 3; // Switch to polling after 3 SSE failures

export function useRunStream(workflowId: string | null, restartKey: number = 0) {
    const eventSourceRef = useRef<EventSource | null>(null);
    const reconnectTimeoutRef = useRef<number | null>(null);
    const pollIntervalRef = useRef<number | null>(null);
    const lastEventIdRef = useRef<string | null>(null);
    const shouldReconnectRef = useRef(true);
    const sseFailureCountRef = useRef(0);
    const isPollingRef = useRef(false);
    const lastProgressRef = useRef<TaskProgress | null>(null);
    const dispatch = useDispatch();

    // Convert task progress to events for Redux
    const progressToEvents = useCallback((progress: TaskProgress) => {
        const events: any[] = [];
        
        // Emit progress event
        events.push({
            type: "PROGRESS",
            workflow_id: progress.workflow_id,
            payload: {
                percent: progress.progress_percent,
                current_step: progress.current_step,
                total_steps: progress.total_steps,
                completed_steps: progress.completed_steps,
                elapsed_time_ms: progress.elapsed_time_ms,
                estimated_remaining_ms: progress.estimated_remaining_ms,
            },
            timestamp: progress.last_updated,
        });

        // Emit subtask progress if available
        if (progress.subtasks) {
            for (const subtask of progress.subtasks) {
                if (subtask.status === "running") {
                    events.push({
                        type: "AGENT_STARTED",
                        workflow_id: progress.workflow_id,
                        agent_id: subtask.agent_id || subtask.id,
                        message: subtask.description,
                        timestamp: progress.last_updated,
                    });
                } else if (subtask.status === "completed") {
                    events.push({
                        type: "AGENT_COMPLETED",
                        workflow_id: progress.workflow_id,
                        agent_id: subtask.agent_id || subtask.id,
                        message: subtask.description,
                        timestamp: progress.last_updated,
                    });
                }
            }
        }

        // Check for completion
        if (progress.status === "completed") {
            events.push({
                type: "WORKFLOW_COMPLETED",
                workflow_id: progress.workflow_id,
                timestamp: progress.last_updated,
            });
            events.push({
                type: "done",
                workflow_id: progress.workflow_id,
                timestamp: progress.last_updated,
            });
        } else if (progress.status === "failed") {
            events.push({
                type: "WORKFLOW_FAILED",
                workflow_id: progress.workflow_id,
                timestamp: progress.last_updated,
            });
        }

        return events;
    }, []);

    useEffect(() => {
        if (!workflowId) return;
        
        // restartKey is used to force reconnection when changed by caller
        // Reference it to satisfy exhaustive-deps and document intent
        void restartKey;
        
        shouldReconnectRef.current = true;
        sseFailureCountRef.current = 0;
        isPollingRef.current = false;

        const cleanupTimeout = () => {
            if (reconnectTimeoutRef.current) {
                clearTimeout(reconnectTimeoutRef.current);
                reconnectTimeoutRef.current = null;
            }
        };

        const cleanupPolling = () => {
            if (pollIntervalRef.current) {
                clearInterval(pollIntervalRef.current);
                pollIntervalRef.current = null;
            }
            isPollingRef.current = false;
        };

        // Start polling fallback
        const startPolling = (taskId: string) => {
            if (isPollingRef.current) return;
            isPollingRef.current = true;
            dispatch(setConnectionState("polling"));
            console.log("[Stream] Switching to polling fallback");

            const poll = async () => {
                if (!shouldReconnectRef.current) {
                    cleanupPolling();
                    return;
                }

                try {
                    const progress = await pollTaskProgress(taskId);
                    
                    // Only dispatch if progress changed
                    if (JSON.stringify(progress) !== JSON.stringify(lastProgressRef.current)) {
                        lastProgressRef.current = progress;
                        const events = progressToEvents(progress);
                        for (const event of events) {
                            dispatch({ type: "run/addEvent", payload: event });
                        }
                    }

                    // Stop polling if task is done
                    if (progress.status === "completed" || progress.status === "failed" || progress.status === "cancelled") {
                        cleanupPolling();
                        dispatch(setConnectionState("idle"));
                        shouldReconnectRef.current = false;
                    }
                } catch (error) {
                    console.error("[Stream] Polling error:", error);
                }
            };

            // Initial poll
            poll();
            // Set up interval
            pollIntervalRef.current = window.setInterval(poll, POLL_INTERVAL_MS);
        };

        const connect = (attempt: number = 0) => {
            if (!workflowId || !shouldReconnectRef.current) return;

            cleanupTimeout();
            dispatch(setStreamError(null));
            dispatch(setConnectionState(attempt > 0 ? "reconnecting" : "connecting"));

            const baseUrl = getStreamUrl(workflowId);
            const url = lastEventIdRef.current
                ? `${baseUrl}&last_event_id=${encodeURIComponent(lastEventIdRef.current)}`
                : baseUrl;

            const eventSource = new EventSource(url);
            eventSourceRef.current = eventSource;

            const handleEvent = (event: MessageEvent, eventType?: string) => {
                try {
                    // Skip empty or undefined data
                    if (!event.data || event.data === "undefined") {
                        return;
                    }
                    
                    // Special case: [DONE] is sent as plain text, not JSON
                    if (event.data === "[DONE]") {
                        return;
                    }
                    
                    const data = JSON.parse(event.data);
                    // Set the type from the SSE event name (eventType) if provided, otherwise fall back to data.type
                    const finalType = eventType || data.type;
                    const eventWithTimestamp = {
                        ...data,
                        type: finalType,
                        timestamp: new Date().toISOString(),
                    };

                    // Track last event id for resume support
                    const candidateId = (event as any).lastEventId || data?.seq?.toString() || data?.stream_id || data?.id;
                    if (candidateId) {
                        lastEventIdRef.current = String(candidateId);
                    }

                    // Dispatch to Redux
                    dispatch({ type: "run/addEvent", payload: eventWithTimestamp });
                    console.log(`[Stream] Received ${finalType} event:`, eventWithTimestamp);
                } catch (e) {
                    console.error("Failed to parse SSE event:", event.data, e);
                }
            };

            eventSource.onopen = () => {
                dispatch(setConnectionState("connected"));
            };

            // Listen for all Shannon event types
            const eventTypes = [
                "thread.message.delta",
                "thread.message.completed",
                "WORKFLOW_STARTED",
                "WORKFLOW_COMPLETED",
                "WORKFLOW_FAILED",
                "workflow.pausing",
                "workflow.paused",
                "workflow.resumed",
                "workflow.cancelling",
                "workflow.cancelled",
                "AGENT_THINKING",
                "AGENT_STARTED",
                "AGENT_COMPLETED",
                "LLM_PROMPT",
                "LLM_OUTPUT",
                "LLM_PARTIAL",
                "PROGRESS",
                "DELEGATION",
                "DATA_PROCESSING",
                "TOOL_INVOKED",
                "TOOL_OBSERVATION",
                "SYNTHESIS",
                "REFLECTION",
                "ROLE_ASSIGNED",
                "TEAM_RECRUITED",
                "TEAM_RETIRED",
                "TEAM_STATUS",
                "WAITING",
                "ERROR_RECOVERY",
                "ERROR_OCCURRED",
                "BUDGET_THRESHOLD",
                "DEPENDENCY_SATISFIED",
                "APPROVAL_REQUESTED",
                "APPROVAL_DECISION",
                "MESSAGE_SENT",
                "MESSAGE_RECEIVED",
                "WORKSPACE_UPDATED",
                "STATUS_UPDATE",
                "error",
                "done",
                "STREAM_END"
            ];

            eventTypes.forEach(type => {
                eventSource.addEventListener(type, (event: Event | MessageEvent) => {
                    if (type === "done" || type === "STREAM_END") {
                        // Dispatch a synthetic done event to Redux
                        dispatch({ 
                            type: "run/addEvent", 
                            payload: {
                                type: "done",
                                workflow_id: workflowId,
                                timestamp: new Date().toISOString(),
                            }
                        });
                        console.log("Stream ended, closing connection");
                        dispatch(setConnectionState("idle"));
                        shouldReconnectRef.current = false;
                        // Close the connection
                        eventSource.close();
                    } else {
                        // Pass the event type from the SSE event field
                        handleEvent(event as MessageEvent, type);
                    }
                });
            });

            // Also listen for generic "message" events as fallback
            eventSource.onmessage = handleEvent;

            eventSource.onerror = (err) => {
                console.error("SSE Error:", err);
                sseFailureCountRef.current++;
                
                // Dispatch error event to Redux
                dispatch({ 
                    type: "run/addEvent", 
                    payload: {
                        type: "error",
                        workflow_id: workflowId,
                        message: "Stream connection error",
                        timestamp: new Date().toISOString(),
                    }
                });
                dispatch(setStreamError("Stream connection error"));
                dispatch(setConnectionState("error"));
                eventSource.close();

                if (!shouldReconnectRef.current) {
                    return;
                }

                // Switch to polling after too many SSE failures
                if (sseFailureCountRef.current >= MAX_SSE_FAILURES_BEFORE_POLLING) {
                    console.log("[Stream] Too many SSE failures, switching to polling");
                    startPolling(workflowId);
                    return;
                }

                const delay = Math.min(
                    BASE_RECONNECT_DELAY_MS * Math.pow(2, attempt),
                    MAX_RECONNECT_DELAY_MS
                );
                reconnectTimeoutRef.current = window.setTimeout(() => connect(attempt + 1), delay);
            };
        };

        connect();

        return () => {
            shouldReconnectRef.current = false;
            cleanupTimeout();
            cleanupPolling();
            if (eventSourceRef.current) {
                eventSourceRef.current.close();
                eventSourceRef.current = null;
            }
            dispatch(setConnectionState("idle"));
        };
    }, [workflowId, restartKey, dispatch, progressToEvents]);
}
