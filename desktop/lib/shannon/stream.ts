/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { useCallback, useEffect, useRef } from "react";
import { getStreamUrl } from "./api";
import { useDispatch } from "react-redux";
import { setConnectionState, setStreamError } from "../features/runSlice";

const MAX_RECONNECT_DELAY_MS = 10000;
const BASE_RECONNECT_DELAY_MS = 1000;
const MAX_RECONNECT_ATTEMPTS = 10; // Stop reconnecting after 10 failed attempts

type DeltaBufferEntry = {
    parts: Array<{ delta: string; seq?: number }>;
    metadata?: any;
    baseEvent: any;
};

export function useRunStream(workflowId: string | null, restartKey: number = 0) {
    const eventSourceRef = useRef<EventSource | null>(null);
    const reconnectTimeoutRef = useRef<number | null>(null);
    const lastEventIdRef = useRef<string | null>(null);
    const shouldReconnectRef = useRef(true);
    const deltaBufferRef = useRef<Map<string, DeltaBufferEntry>>(new Map());
    const flushTimeoutRef = useRef<number | null>(null);
    const dispatch = useDispatch();

    const flushDeltaBuffer = useCallback(() => {
        deltaBufferRef.current.forEach((buffer) => {
            if (buffer.parts.length === 0) return;

            const combinedDelta = buffer.parts.map((part) => part.delta).join("");
            dispatch({
                type: "run/addEvent",
                payload: {
                    ...buffer.baseEvent,
                    delta: combinedDelta,
                    _coalescedParts: buffer.parts,
                    metadata: buffer.metadata,
                    timestamp: new Date().toISOString(),
                }
            });
        });
        deltaBufferRef.current.clear();
        flushTimeoutRef.current = null;
    }, [dispatch]);

    useEffect(() => {
        if (!workflowId) return;
        shouldReconnectRef.current = true;

        const cleanupTimeout = () => {
            if (reconnectTimeoutRef.current) {
                clearTimeout(reconnectTimeoutRef.current);
                reconnectTimeoutRef.current = null;
            }
        };

        const connect = async (attempt: number = 0) => {
            if (!workflowId || !shouldReconnectRef.current) return;

            cleanupTimeout();
            dispatch(setStreamError(null));
            dispatch(setConnectionState(attempt > 0 ? "reconnecting" : "connecting"));

            const baseUrl = await getStreamUrl(workflowId);
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

                    if (finalType === "thread.message.delta") {
                        const streamId = data.stream_id ??
                            (data.seq !== undefined ? `seq-${data.seq}` : `msg-${Date.now()}`);
                        const buffer = deltaBufferRef.current.get(streamId);
                        const deltaText = typeof data.delta === "string" ? data.delta : "";
                        const part = { delta: deltaText, seq: data.seq };

                        if (buffer) {
                            buffer.parts.push(part);
                            if (data.metadata) {
                                buffer.metadata = { ...buffer.metadata, ...data.metadata };
                            }
                        } else {
                            deltaBufferRef.current.set(streamId, {
                                parts: [part],
                                metadata: data.metadata,
                                baseEvent: { ...data, type: finalType },
                            });
                        }

                        if (!flushTimeoutRef.current) {
                            flushTimeoutRef.current = window.requestAnimationFrame(flushDeltaBuffer);
                        }
                        return;
                    }

                    if (deltaBufferRef.current.size > 0) {
                        flushDeltaBuffer();
                    }

                    // Dispatch to Redux
                    dispatch({ type: "run/addEvent", payload: eventWithTimestamp });
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
                "TOOL_STARTED",      // Browser automation: tool execution started
                "TOOL_COMPLETED",    // Browser automation: tool execution completed
                "SYNTHESIS",
                "REFLECTION",
                "ROLE_ASSIGNED",
                "TEAM_RECRUITED",
                "TEAM_RETIRED",
                "TEAM_STATUS",
                "TASKLIST_UPDATED",
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
                "SCREENSHOT_SAVED",
                "HITL_RESPONSE",
                "LEAD_DECISION",
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
                        if (deltaBufferRef.current.size > 0) {
                            flushDeltaBuffer();
                        }
                        // Dispatch a synthetic done event to Redux
                        dispatch({ 
                            type: "run/addEvent", 
                            payload: {
                                type: "done",
                                workflow_id: workflowId,
                                timestamp: new Date().toISOString(),
                            }
                        });
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

            eventSource.onerror = () => {
                if (deltaBufferRef.current.size > 0) {
                    flushDeltaBuffer();
                }
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

                // Stop reconnecting after max attempts to prevent infinite loop
                if (attempt >= MAX_RECONNECT_ATTEMPTS) {
                    console.warn(`[Stream] Max reconnect attempts (${MAX_RECONNECT_ATTEMPTS}) reached, giving up`);
                    dispatch(setStreamError("Connection failed after multiple retries. Please refresh the page."));
                    shouldReconnectRef.current = false;
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
            if (flushTimeoutRef.current) {
                cancelAnimationFrame(flushTimeoutRef.current);
                flushTimeoutRef.current = null;
            }
            if (deltaBufferRef.current.size > 0) {
                flushDeltaBuffer();
            }
            if (eventSourceRef.current) {
                eventSourceRef.current.close();
                eventSourceRef.current = null;
            }
            dispatch(setConnectionState("idle"));
        };
    }, [workflowId, restartKey, dispatch, flushDeltaBuffer]);
}
