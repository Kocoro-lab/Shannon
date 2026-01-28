/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { useEffect, useRef } from "react";
import { getStreamUrl } from "./api";
import { useDispatch } from "react-redux";
import { setConnectionState, setStreamError } from "../features/runSlice";

const MAX_RECONNECT_DELAY_MS = 10000;
const BASE_RECONNECT_DELAY_MS = 1000;

export function useRunStream(workflowId: string | null, restartKey: number = 0) {
    const eventSourceRef = useRef<EventSource | null>(null);
    const reconnectTimeoutRef = useRef<number | null>(null);
    const lastEventIdRef = useRef<string | null>(null);
    const shouldReconnectRef = useRef(true);
    const dispatch = useDispatch();

    useEffect(() => {
        if (!workflowId) return;
        shouldReconnectRef.current = true;

        const cleanupTimeout = () => {
            if (reconnectTimeoutRef.current) {
                clearTimeout(reconnectTimeoutRef.current);
                reconnectTimeoutRef.current = null;
            }
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
                "RESEARCH_PLAN_READY",
                "RESEARCH_PLAN_UPDATED",
                "RESEARCH_PLAN_APPROVED",
                "REVIEW_USER_FEEDBACK",
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
            if (eventSourceRef.current) {
                eventSourceRef.current.close();
                eventSourceRef.current = null;
            }
            dispatch(setConnectionState("idle"));
        };
    }, [workflowId, restartKey, dispatch]);
}
