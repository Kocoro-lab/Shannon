/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { useSearchParams, useRouter } from "next/navigation";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RunTimeline } from "@/components/run-timeline";
import { RunConversation } from "@/components/run-conversation";
import { ChatInput, AgentSelection, ReviewPlanMode } from "@/components/chat-input";
import { ArrowLeft, Loader2, Sparkles, Microscope, Eye, EyeOff, PanelRight, PanelRightClose } from "lucide-react";
import { RadarCanvas, RadarBridge } from "@/components/radar";
import Link from "next/link";
import { Suspense, useEffect, useState, useRef, useCallback, useMemo } from "react";
import { useRunStream } from "@/lib/shannon/stream";
import { useSelector, useDispatch } from "react-redux";
import { RootState } from "@/lib/store";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { getSessionEvents, getSessionHistory, getTask, getSession, listSessions, Turn, Event, pauseTask, resumeTask, cancelTask, getTaskControlState, approveReviewPlan } from "@/lib/shannon/api";
import { resetRun, addMessage, addEvent, updateMessageMetadata, setStreamError, setSelectedAgent, setResearchStrategy, setMainWorkflowId, setStatus, setPaused, setCancelling, setCancelled, setReviewPlan, setReviewStatus, setReviewVersion, setReviewIntent } from "@/lib/features/runSlice";

function RunDetailContent() {
    const searchParams = useSearchParams();
    const sessionId = searchParams.get("session_id");
    const taskIdParam = searchParams.get("id");
    const isNewSession = (sessionId === "new" || !sessionId) && !taskIdParam;

    const [isLoading, setIsLoading] = useState(!isNewSession);
    const [error, setError] = useState<string | null>(null);
    const [sessionData, setSessionData] = useState<{ turns: Turn[], events: Event[] } | null>(null);
    const [sessionHistory, setSessionHistory] = useState<any>(null);
    const [currentTaskId, setCurrentTaskId] = useState<string | null>(null);
    const [actualSessionId, setActualSessionId] = useState<string | null>(null); // Track real session ID (not "new")
    const [streamRestartKey, setStreamRestartKey] = useState(0);
    const [activeTab, setActiveTab] = useState("conversation");
    const [showTimeline, setShowTimeline] = useState(false); // Hidden by default - status events now appear inline in conversation
    const [isPauseLoading, setIsPauseLoading] = useState(false);
    const [isResumeLoading, setIsResumeLoading] = useState(false);
    const dispatch = useDispatch();
    const router = useRouter();

    // Refs for auto-scrolling
    const timelineScrollRef = useRef<HTMLDivElement>(null);
    const conversationScrollRef = useRef<HTMLDivElement>(null);
    const userHasScrolledRef = useRef(false); // Track if user manually scrolled up

    // Refs for tracking history/message loading state
    const hasLoadedMessagesRef = useRef(false);
    const hasFetchedHistoryRef = useRef(false);
    const hasFetchedAgentTypeRef = useRef<string | null>(null); // Track session ID for which agent type was fetched
    const hasInitializedTaskRef = useRef<string | null>(null);
    const prevSessionIdRef = useRef<string | null>(null);
    const hasInitializedRef = useRef(false);

    // Connect to SSE stream for the current task if one is running
    useRunStream(currentTaskId, streamRestartKey);

    // Get data from Redux (streaming state)
    const runEvents = useSelector((state: RootState) => state.run.events);
    const runMessages = useSelector((state: RootState) => state.run.messages);
    const runStatus = useSelector((state: RootState) => state.run.status);
    const connectionState = useSelector((state: RootState) => state.run.connectionState);
    const streamError = useSelector((state: RootState) => state.run.streamError);
    const sessionTitle = useSelector((state: RootState) => state.run.sessionTitle);
    const selectedAgent = useSelector((state: RootState) => state.run.selectedAgent);
    const researchStrategy = useSelector((state: RootState) => state.run.researchStrategy);
    const isPaused = useSelector((state: RootState) => state.run.isPaused);
    const pauseCheckpoint = useSelector((state: RootState) => state.run.pauseCheckpoint);
    const isCancelling = useSelector((state: RootState) => state.run.isCancelling);
    const isCancelled = useSelector((state: RootState) => state.run.isCancelled);
    const reviewPlan = useSelector((state: RootState) => state.run.reviewPlan);
    const reviewStatus = useSelector((state: RootState) => state.run.reviewStatus);
    const reviewWorkflowId = useSelector((state: RootState) => state.run.reviewWorkflowId);
    const reviewVersion = useSelector((state: RootState) => state.run.reviewVersion);
    const reviewIntent = useSelector((state: RootState) => state.run.reviewIntent);
    const isReconnecting = connectionState === "reconnecting" || connectionState === "connecting";

    const handleRetryStream = () => {
        dispatch(setStreamError(null));
        setStreamRestartKey(key => key + 1);
    };

    // Helper to format duration in a human-readable way
    const formatDuration = (seconds: number): string => {
        if (seconds < 60) {
            return `${seconds.toFixed(1)}s`;
        }
        const minutes = Math.floor(seconds / 60);
        const remainingSeconds = Math.round(seconds % 60);
        return `${minutes}m ${remainingSeconds}s`;
    };

    // Reset Redux state only when switching sessions or starting fresh
    useEffect(() => {
        // Skip if sessionId is not yet available (Suspense loading)
        if (sessionId === null || sessionId === undefined) return;
        
        // Reset on initial mount or when session changes
        const sessionChanged = sessionId !== prevSessionIdRef.current;
        // Don't reset Redux state when transitioning from "new" to a real session ID (task creation flow)
        // This preserves streaming messages
        const isNewToReal = prevSessionIdRef.current === "new" && sessionId && sessionId !== "new";
        const shouldResetRedux = !hasInitializedRef.current || (sessionChanged && !isNewToReal);

        if (shouldResetRedux) {
            dispatch(resetRun());
            userHasScrolledRef.current = false; // Reset scroll tracking on session change
        }
        
        // Reset fetch flags when session changes
        // For new-to-real transitions, preserve message state (we already have streaming data)
        if (sessionChanged || !hasInitializedRef.current) {
            prevSessionIdRef.current = sessionId;
            hasInitializedRef.current = true;
            
            // Only reset message/history flags if NOT transitioning from "new" to real session
            // During new-to-real transition, handleTaskCreated already added the user message
            if (!isNewToReal) {
                hasLoadedMessagesRef.current = false;
                hasFetchedHistoryRef.current = false;
                hasInitializedTaskRef.current = null;
                setCurrentTaskId(null);
            }
            hasFetchedAgentTypeRef.current = null;
        }
    }, [dispatch, sessionId]);

    // Handle direct task access (e.g. from New Task dialog)
    useEffect(() => {
        const initializeFromTask = async () => {
            if (!taskIdParam) return;

            // Only initialize once per task
            if (hasInitializedTaskRef.current === taskIdParam) {
                return;
            }

            hasInitializedTaskRef.current = taskIdParam;

            try {
                setIsLoading(true);
                const task = await getTask(taskIdParam);
                const workflowId = task.workflow_id || taskIdParam;

                // Ensure streaming uses the workflow ID we got back from the API
                setCurrentTaskId(workflowId);

                // Set this as the main workflow ID in Redux
                dispatch(setMainWorkflowId(workflowId));

                // Extract agent type and research strategy from task context
                // Context is stored in metadata.task_context
                let taskContext = task.context;
                if (!taskContext && task.metadata?.task_context) {
                    taskContext = task.metadata.task_context;
                }

                if (taskContext) {
                    // Handle both boolean true and string "true" (proto map<string,string> converts to string)
                    const isDeepResearch = taskContext.force_research === true || taskContext.force_research === "true";
                    const strategy = taskContext.research_strategy || "quick";

                    console.log("[RunDetail] Task context - Agent type:", isDeepResearch ? "deep_research" : "normal", "Strategy:", strategy);
                    console.log("[RunDetail] Task context details:", taskContext);

                    dispatch(setSelectedAgent(isDeepResearch ? "deep_research" : "normal"));
                    if (isDeepResearch) {
                        dispatch(setResearchStrategy(strategy as "quick" | "standard" | "deep" | "academic"));
                    }
                    
                    // Mark agent type as fetched for the task's session to avoid redundant API calls
                    if (task.session_id) {
                        hasFetchedAgentTypeRef.current = task.session_id;
                    }
                }

                // Add the user message immediately
                // Use task_id format (taskIdParam) to match fetchSessionHistory's ID format and prevent duplicates
                dispatch(addMessage({
                    id: `user-${taskIdParam}`,
                    role: "user",
                    content: task.query,
                    timestamp: new Date(task.created_at || Date.now()).toLocaleTimeString(),
                    taskId: workflowId,
                }));

                // Add generating placeholder if task is still running
                if (task.status === "TASK_STATUS_RUNNING" || task.status === "TASK_STATUS_QUEUED") {
                    dispatch(addMessage({
                        id: `generating-${workflowId}`,
                        role: "assistant",
                        content: "Generating...",
                        timestamp: new Date().toLocaleTimeString(),
                        isGenerating: true,
                        taskId: workflowId,
                    }));
                }

                // If the task has a session ID, update the URL and track it
                if (task.session_id) {
                    setActualSessionId(task.session_id);
                    if (!sessionId || sessionId === "new") {
                        const newParams = new URLSearchParams(searchParams.toString());
                        newParams.set("session_id", task.session_id);
                        router.replace(`/run-detail?${newParams.toString()}`);
                    }
                }
            } catch (err) {
                console.error("Failed to fetch task details:", err);
                setError("Failed to load task details");
            } finally {
                setIsLoading(false);
            }
        };

        initializeFromTask();
    }, [taskIdParam, sessionId, router, searchParams, dispatch]);

    // Fetch full session history
    const fetchSessionHistory = useCallback(async (forceReload = false) => {
        if (isNewSession) return;
        if (!sessionId || sessionId === "new") return;

        // Track actual session ID for use in effects
        setActualSessionId(sessionId);

        // Fetch session's is_research_session flag from listSessions API
        if (hasFetchedAgentTypeRef.current !== sessionId) {
            try {
                const sessionsData = await listSessions(50, 0);
                const session = sessionsData.sessions?.find(s => s.session_id === sessionId);
                
                if (session) {
                    const isDeepResearch = session.is_research_session === true;
                    const strategy = session.research_strategy || "standard";
                    
                    dispatch(setSelectedAgent(isDeepResearch ? "deep_research" : "normal"));
                    if (isDeepResearch) {
                        dispatch(setResearchStrategy(strategy as "quick" | "standard" | "deep" | "academic"));
                    }
                }
                hasFetchedAgentTypeRef.current = sessionId;
            } catch (err) {
                console.error("[RunDetail] Failed to fetch session details:", err);
            }
        }

        // Don't fetch history if we're currently streaming a new task unless forced
        // Also skip if we have a currentTaskId but SSE hasn't connected yet (status still idle)
        if (!forceReload && currentTaskId && (runStatus === "running" || runStatus === "idle")) {
            console.log("[RunDetail] Skipping history fetch while task is in progress or starting");
            return;
        }

        setIsLoading(true);
        setError(null);
        try {
            // Fetch events to build the timeline and conversation
            // Backend validation limits to 100 turns per request
            const eventsData = await getSessionEvents(sessionId, 100, 0, true);

            // Debug: Check if payload is being returned
            console.log('[RunDetail] Session events fetched:', eventsData.turns.length, 'turns');
            if (eventsData.turns.length > 0 && eventsData.turns[0].events.length > 0) {
                const firstEvent = eventsData.turns[0].events[0];
                console.log('[RunDetail] First event sample:', {
                    type: firstEvent.type,
                    message: firstEvent.message?.substring(0, 50),
                    hasPayload: !!firstEvent.payload,
                    payloadType: typeof firstEvent.payload,
                    payloadSample: firstEvent.payload ? String(firstEvent.payload).substring(0, 100) : null
                });
            }

            // Continue with events processing...

            // The API response might not have a top-level 'events' array if it's just returning turns with embedded events.
            // Let's collect all events from all turns if top-level events is missing.
            const allEvents: Event[] = (eventsData as any).events || eventsData.turns.flatMap(t => t.events || []);

            setSessionData({ turns: eventsData.turns, events: allEvents });

            // Fetch history to get cost data for Summary tab
            const historyData = await getSessionHistory(sessionId);
            setSessionHistory(historyData);

            // Declare at function scope so it's accessible later for SSE connection
            let lastRunningWorkflowId: string | null = null;

            // Only populate Redux messages if we haven't loaded them yet
            // This prevents duplicates when navigating with an active task
            if (!hasLoadedMessagesRef.current || forceReload) {
                if (forceReload) {
                    // Clear current state first on forced reload
                    dispatch(resetRun());
                }

                // Extract and set session title from title_generator events (before filtering them out)
                const titleEvent = allEvents.find((event: Event) =>
                    (event as any).agent_id === 'title_generator'
                );
                if (titleEvent) {
                    const title = (titleEvent as any).message || (titleEvent as any).response || (titleEvent as any).content;
                    if (title) {
                        dispatch(addEvent({
                            type: 'thread.message.completed',
                            agent_id: 'title_generator',
                            response: title,
                            workflow_id: (titleEvent as any).workflow_id,
                            timestamp: new Date().toISOString(),
                            isHistorical: true
                        } as any));
                        console.log("[RunDetail] Loaded session title from history:", title);
                    }
                }

                // Add historical events to Redux (filter out events that create conversation messages)
                // For history: LLM_OUTPUT, TOOL_INVOKED, TOOL_OBSERVATION create messages - skip them
                // We'll use turn.final_output for the conversation instead
                // Also deduplicate excessive BUDGET_THRESHOLD events here to reduce Redux state size
                const eventsToAdd = allEvents
                    .filter((event: Event) => {
                        const type = (event as any).type;
                        const agentId = (event as any).agent_id;
                        return agentId !== 'title_generator' &&
                            type !== 'LLM_OUTPUT' &&
                            type !== 'TOOL_INVOKED' &&
                            type !== 'TOOL_OBSERVATION';
                    });

                // Deduplicate BUDGET_THRESHOLD events before adding to Redux
                // Only show first warning and then every 100% increase to minimize clutter
                const deduplicatedHistoricalEvents: Event[] = [];
                let lastBudgetPercent = 0;
                let budgetEventCount = 0;
                const MAX_BUDGET_EVENTS = 5; // Limit total budget events shown

                eventsToAdd.forEach((event: Event) => {
                    if ((event as any).type === 'BUDGET_THRESHOLD') {
                        const match = (event as any).message?.match(/Task budget at ([\d.]+)%/);
                        if (match) {
                            const currentPercent = parseFloat(match[1]);
                            // Keep first event, then only 100%+ increases, with a max limit
                            if (budgetEventCount < MAX_BUDGET_EVENTS &&
                                (lastBudgetPercent === 0 || currentPercent - lastBudgetPercent >= 100)) {
                                deduplicatedHistoricalEvents.push(event);
                                lastBudgetPercent = currentPercent;
                                budgetEventCount++;
                            }
                            // Skip other budget events to reduce clutter
                        } else {
                            // Can't parse, keep it
                            deduplicatedHistoricalEvents.push(event);
                        }
                    } else {
                        // Not a budget event, keep it
                        deduplicatedHistoricalEvents.push(event);
                    }
                });

                // Add deduplicated events to Redux (marked as historical to skip status pill creation)
                deduplicatedHistoricalEvents.forEach((event: Event) => {
                    dispatch(addEvent({ ...event, isHistorical: true } as any));
                });

                // Track failed task info for message display
                let failedTaskInfo: { status: string; errorMessage?: string; workflowId: string; taskId: string } | null = null;

                // Check if the last task is running before loading messages
                if (eventsData.turns.length > 0) {
                    const lastTurn = eventsData.turns[eventsData.turns.length - 1];
                    const lastWorkflowId = lastTurn.events.length > 0 ? lastTurn.events[0].workflow_id : lastTurn.task_id;

                    try {
                        const taskStatus = await getTask(lastWorkflowId);
                        if ((taskStatus.status === "TASK_STATUS_RUNNING" || taskStatus.status === "TASK_STATUS_QUEUED") && !taskStatus.result) {
                            lastRunningWorkflowId = lastWorkflowId;
                            console.log("[RunDetail] Last task is running:", lastWorkflowId, "- will show generating indicator");
                        } else if (taskStatus.status === "TASK_STATUS_FAILED" || taskStatus.status === "TASK_STATUS_CANCELLED") {
                            // Task failed or was cancelled - set status appropriately
                            console.log("[RunDetail] Last task failed/cancelled:", lastWorkflowId, "status:", taskStatus.status);
                            dispatch(setStatus("failed"));
                            failedTaskInfo = {
                                status: taskStatus.status,
                                errorMessage: taskStatus.error_message,
                                workflowId: lastWorkflowId,
                                taskId: lastTurn.task_id
                            };
                            // Add timeline event for failed/cancelled task
                            dispatch(addEvent({
                                type: taskStatus.status === "TASK_STATUS_CANCELLED" ? "TASK_CANCELLED" : "TASK_FAILED",
                                workflow_id: lastWorkflowId,
                                message: taskStatus.status === "TASK_STATUS_CANCELLED" 
                                    ? "Task was cancelled" 
                                    : `Task failed${taskStatus.error_message ? `: ${taskStatus.error_message}` : ""}`,
                                timestamp: new Date().toISOString(),
                                isHistorical: true,
                            } as any));
                        } else if (taskStatus.status === "TASK_STATUS_COMPLETED") {
                            dispatch(setStatus("completed"));
                        }
                    } catch (err) {
                        console.warn("[RunDetail] Failed to check last task status:", err);
                    }
                }

                // Add historical messages (turns) to Redux
                // We need to reconstruct messages from turns
                // Fetch full task details in parallel to get citations
                // Use workflow_id from events as the API expects workflow_id, not task_id
                const taskDetailsPromises = eventsData.turns.map(turn => {
                    // Get workflow_id from the first event in the turn
                    const workflowId = turn.events.length > 0 ? turn.events[0].workflow_id : turn.task_id;
                    return getTask(workflowId).catch(err => {
                        console.warn(`[RunDetail] Failed to fetch task details for workflow ${workflowId}:`, err);
                        return null;
                    });
                });

                const taskDetails = await Promise.all(taskDetailsPromises);
                const taskDetailsMap = new Map(
                    taskDetails
                        .filter(t => t !== null)
                        .map((t, index) => {
                            const turn = eventsData.turns[index];
                            const workflowId = turn.events.length > 0 ? turn.events[0].workflow_id : turn.task_id;
                            return [workflowId, t];
                        })
                );

                // Merge model_breakdown from task details into session history
                // This provides complete model usage data from token_usage table
                if (historyData?.tasks) {
                    const enrichedHistory = {
                        ...historyData,
                        tasks: historyData.tasks.map((task: any) => {
                            const taskDetail = taskDetailsMap.get(task.workflow_id);
                            if (taskDetail?.metadata?.model_breakdown) {
                                return {
                                    ...task,
                                    metadata: {
                                        ...task.metadata,
                                        model_breakdown: taskDetail.metadata.model_breakdown
                                    }
                                };
                            }
                            return task;
                        })
                    };
                    setSessionHistory(enrichedHistory);
                }

                console.log("[RunDetail] Loading", eventsData.turns.length, "turns into messages");
                eventsData.turns.forEach((turn, turnIndex) => {
                    const workflowId = turn.events.length > 0 ? turn.events[0].workflow_id : turn.task_id;
                    console.log(`[RunDetail] Processing turn ${turnIndex + 1}/${eventsData.turns.length}, task_id: ${turn.task_id}, workflow_id: ${workflowId}`);

                    const isCurrentlyRunning = workflowId === lastRunningWorkflowId;

                    // Check for HITL review events in this turn
                    const planReadyEvent = turn.events.find((e: any) => e.type === "RESEARCH_PLAN_READY");
                    const planApprovedEvent = turn.events.find((e: any) => e.type === "RESEARCH_PLAN_APPROVED");

                    // User message
                    dispatch(addMessage({
                        id: `user-${turn.task_id}`,
                        role: "user",
                        content: turn.user_query,
                        timestamp: new Date(turn.timestamp).toLocaleTimeString(),
                        taskId: turn.task_id,
                    }));
                    console.log(`[RunDetail] Added user message for turn ${turnIndex + 1}`);

                    // Add research plan message (from RESEARCH_PLAN_READY event) AFTER user message
                    // This ensures correct ordering: user query → research plan
                    if (planReadyEvent) {
                        const planMessage = (planReadyEvent as any).message;
                        if (planMessage) {
                            dispatch(addMessage({
                                id: `research-plan-${workflowId}-hist`,
                                role: "assistant",
                                content: planMessage,
                                timestamp: new Date((planReadyEvent as any).timestamp || turn.timestamp).toLocaleTimeString(),
                                taskId: workflowId,
                                isResearchPlan: true,
                                planRound: 1,
                            }));
                            console.log(`[RunDetail] Added research plan message for turn ${turnIndex + 1}`);
                        }
                    }

                    // Add plan approval message if approved
                    if (planApprovedEvent) {
                        dispatch(addMessage({
                            id: `plan-approved-${workflowId}-hist`,
                            role: "system",
                            content: "Plan approved. Research started.",
                            timestamp: new Date((planApprovedEvent as any).timestamp || turn.timestamp).toLocaleTimeString(),
                            taskId: workflowId,
                        }));
                        console.log(`[RunDetail] Added plan approval message for turn ${turnIndex + 1}`);
                    }

                    // For running tasks, handle review mode vs normal mode
                    if (isCurrentlyRunning) {
                        // If task is in review mode (plan ready but not approved), don't add generating placeholder
                        if (planReadyEvent && !planApprovedEvent) {
                            console.log("[RunDetail] Task is in review mode - plan shown, no generating placeholder");
                            return; // Plan message already added above
                        }
                        // Normal running task or approved plan waiting for execution
                        console.log("[RunDetail] Task is running - adding generating placeholder");
                        dispatch(addMessage({
                            id: `generating-${workflowId}`,
                            role: "assistant",
                            content: "Generating...",
                            timestamp: new Date().toLocaleTimeString(),
                            isGenerating: true,
                            taskId: workflowId,
                        }));
                        return; // Skip loading intermediate/final messages for this turn
                    }

                    // Add intermediate agent trace messages from events (for "Show Agent Trace" feature)
                    // These are LLM_OUTPUT and thread.message.completed events with agent_id
                    const intermediateEvents = turn.events.filter((event: any) =>
                        (event.type === 'LLM_OUTPUT' || event.type === 'thread.message.completed') &&
                        event.agent_id &&
                        event.agent_id !== 'title_generator' &&
                        event.agent_id !== 'synthesis' && // synthesis is the final answer
                        event.agent_id !== 'simple-agent' && // simple-agent is the final answer
                        event.agent_id !== 'assistant' &&
                        (event.message || event.response || event.content)
                    );

                    intermediateEvents.forEach((event: any, eventIndex: number) => {
                        const content = event.message || event.response || event.content || '';
                        if (content) {
                            const uniqueId = `${event.agent_id}-${turn.task_id}-${eventIndex}`;
                            dispatch(addMessage({
                                id: uniqueId,
                                role: "assistant",
                                sender: event.agent_id, // Set sender for agent trace filtering
                                content: content,
                                timestamp: new Date(event.timestamp || turn.timestamp).toLocaleTimeString(),
                                taskId: turn.task_id,
                            }));
                        }
                    });

                    if (intermediateEvents.length > 0) {
                        console.log(`[RunDetail] Added ${intermediateEvents.length} intermediate agent trace messages for turn ${turnIndex + 1}`);
                    }

                    // Assistant message: Use final_output as the authoritative answer
                    // final_output comes from the task's result field (canonical value)
                    // Get full task details (includes citations, metadata, and result as fallback)
                    const fullTaskDetails = taskDetailsMap.get(workflowId);
                    
                    // Check if task was cancelled - don't display partial/irrelevant content
                    const isCancelledTask = fullTaskDetails?.status === "TASK_STATUS_CANCELLED";
                    
                    // Priority: skip for cancelled > turn.final_output > fullTaskDetails.result > empty
                    const assistantContent = isCancelledTask ? "" : (turn.final_output || fullTaskDetails?.result || "");
                    
                    if (assistantContent) {
                        const metadata = fullTaskDetails?.metadata || turn.metadata;

                        if (metadata?.citations) {
                            console.log(`[RunDetail] Loaded ${metadata.citations.length} citations for turn ${turn.task_id}`);
                        }

                        const source = turn.final_output ? "final_output" : "task.result API fallback";
                        console.log(`[RunDetail] Adding assistant message from ${source} (authoritative result)`);
                        dispatch(addMessage({
                            id: `assistant-${turn.task_id}`,
                            role: "assistant",
                            content: assistantContent,
                            timestamp: new Date(turn.timestamp).toLocaleTimeString(),
                            metadata: metadata,
                            taskId: turn.task_id,
                        }));
                    } else {
                        console.warn(`[RunDetail] Turn ${turnIndex + 1} has no final_output and no task.result!`);
                        // If this is the failed task, add a system message
                        if (failedTaskInfo && workflowId === failedTaskInfo.workflowId) {
                            const isCancelled = failedTaskInfo.status === "TASK_STATUS_CANCELLED";
                            dispatch(addMessage({
                                id: `system-${turn.task_id}`,
                                role: "system",
                                content: isCancelled 
                                    ? "This task was cancelled before it could complete."
                                    : `This task failed${failedTaskInfo.errorMessage ? `: ${failedTaskInfo.errorMessage}` : ". Please try again."}`,
                                timestamp: new Date().toLocaleTimeString(),
                                taskId: turn.task_id,
                                isError: !isCancelled,
                                isCancelled: isCancelled,
                            }));
                        } else if (fullTaskDetails?.status === "TASK_STATUS_COMPLETED") {
                            // Task is completed but has no result - this shouldn't happen but handle gracefully
                            console.error(`[RunDetail] Task ${workflowId} is COMPLETED but has no result!`);
                            dispatch(addMessage({
                                id: `system-${turn.task_id}`,
                                role: "system",
                                content: "Task completed but no response was recorded. This may indicate a system error.",
                                timestamp: new Date().toLocaleTimeString(),
                                taskId: turn.task_id,
                                isError: true,
                            }));
                        }
                    }
                });

                hasLoadedMessagesRef.current = true;
            } else {
                console.log("[RunDetail] Skipping message population - already loaded messages for this session");
            }

            // Establish SSE connection if we detected a running task earlier
            if (lastRunningWorkflowId && !currentTaskId) {
                console.log("[RunDetail] Establishing SSE connection for running task:", lastRunningWorkflowId);
                setCurrentTaskId(lastRunningWorkflowId);
                dispatch(setMainWorkflowId(lastRunningWorkflowId));
            }

        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load session");
        } finally {
            setIsLoading(false);
        }
    }, [isNewSession, sessionId, runStatus, currentTaskId, dispatch]);

    // Fetch session data on initial load
    // fetchSessionHistory handles agent type detection first, then history loading (if no active task)
    useEffect(() => {
        if (sessionId && sessionId !== "new" && !hasFetchedHistoryRef.current) {
            fetchSessionHistory();
            // Only mark as fetched if no task is running (full history was loaded)
            // If task is running, agent type was fetched but we'll need to fetch history later
            if (!currentTaskId) {
                hasFetchedHistoryRef.current = true;
            }
        }
    }, [sessionId, fetchSessionHistory, currentTaskId]);

    // Refetch session history when a task completes to update the summary
    // Title updates via streaming title_generator events
    // Use actualSessionId (from task response) as it's more reliable than URL param for new sessions
    useEffect(() => {
        const effectiveSessionId = actualSessionId || (sessionId !== "new" ? sessionId : null);
        
        if (runStatus === "completed" && effectiveSessionId) {
            // Update both events (for timeline) and history (for costs in summary)
            // Don't trigger message reload - messages are already in Redux from streaming
            // Use progressive retry to handle backend persistence delays
            let retryCount = 0;
            const maxRetries = 3;
            const delays = [1500, 3000, 5000]; // 1.5s, 3s, 5s
            
            const fetchWithRetry = async () => {
                try {
                    console.log(`[RunDetail] Fetching session history (attempt ${retryCount + 1}/${maxRetries + 1}) for:`, effectiveSessionId);
                    const eventsData = await getSessionEvents(effectiveSessionId, 100, 0, true);
                    const allEvents: Event[] = (eventsData as any).events || eventsData.turns.flatMap(t => t.events || []);
                    setSessionData({ turns: eventsData.turns, events: allEvents });

                    // Fetch history for cost data only (don't reload messages)
                    const historyData = await getSessionHistory(effectiveSessionId);
                    
                    // Fetch task details to get model_breakdown (complete data from token_usage table)
                    if (historyData?.tasks && eventsData.turns.length > 0) {
                        const taskDetailsPromises = eventsData.turns.map(turn => {
                            const workflowId = turn.events.length > 0 ? turn.events[0].workflow_id : turn.task_id;
                            return getTask(workflowId).catch(() => null);
                        });
                        const taskDetails = await Promise.all(taskDetailsPromises);
                        const taskDetailsMap = new Map(
                            taskDetails
                                .filter(t => t !== null)
                                .map((t, index) => {
                                    const turn = eventsData.turns[index];
                                    const workflowId = turn.events.length > 0 ? turn.events[0].workflow_id : turn.task_id;
                                    return [workflowId, t];
                                })
                        );
                        
                        // Merge model_breakdown into history
                        const enrichedHistory = {
                            ...historyData,
                            tasks: historyData.tasks.map((task: any) => {
                                const taskDetail = taskDetailsMap.get(task.workflow_id);
                                if (taskDetail?.metadata?.model_breakdown) {
                                    return {
                                        ...task,
                                        metadata: {
                                            ...task.metadata,
                                            model_breakdown: taskDetail.metadata.model_breakdown
                                        }
                                    };
                                }
                                return task;
                            })
                        };
                        setSessionHistory(enrichedHistory);
                    } else {
                        setSessionHistory(historyData);
                    }
                    
                    // Check if we got meaningful data - if not, retry
                    const hasMeaningfulData = historyData?.tasks?.some((task: any) => 
                        task.total_tokens > 0 || task.total_cost_usd > 0
                    );
                    
                    console.log("[RunDetail] Session history refreshed:", historyData?.tasks?.length, "tasks, hasMeaningfulData:", hasMeaningfulData);

                    // If no meaningful data yet and we haven't exhausted retries, schedule another fetch
                    if (!hasMeaningfulData && retryCount < maxRetries) {
                        retryCount++;
                        console.log(`[RunDetail] No token/cost data yet, will retry in ${delays[retryCount - 1]}ms`);
                        setTimeout(fetchWithRetry, delays[retryCount - 1]);
                    }

                    // Mark that we've loaded messages to prevent fetchSessionHistory from reloading them
                    if (!hasLoadedMessagesRef.current) {
                        hasLoadedMessagesRef.current = true;
                    }
                } catch (err) {
                    console.error("Failed to refresh session data:", err);
                    // Retry on error too
                    if (retryCount < maxRetries) {
                        retryCount++;
                        setTimeout(fetchWithRetry, delays[retryCount - 1]);
                    }
                }
            };
            
            const timer = setTimeout(fetchWithRetry, delays[0]);
            return () => clearTimeout(timer);
        }
    }, [runStatus, sessionId, actualSessionId]);

    const handleTaskCreated = async (newTaskId: string, query: string, workflowId?: string) => {
        const activeWorkflowId = workflowId || newTaskId;
        console.log("New task created:", newTaskId, "workflow:", activeWorkflowId);
        setCurrentTaskId(activeWorkflowId);

        // Set this as the main workflow ID in Redux
        dispatch(setMainWorkflowId(activeWorkflowId));
        
        // Reset control state for new task (important for follow-up tasks in same session)
        dispatch(setStatus("running"));
        dispatch(setCancelling(false));
        dispatch(setCancelled(false));
        dispatch(setPaused({ paused: false }));

        // Add user query to messages immediately
        // Use task_id format (not workflow_id) to match fetchSessionHistory's ID format and prevent duplicates
        dispatch(addMessage({
            id: `user-${newTaskId}`,
            role: "user",
            content: query,
            timestamp: new Date().toLocaleTimeString(),
            taskId: activeWorkflowId,
        }));

        // Add a "generating..." placeholder message
        dispatch(addMessage({
            id: `generating-${activeWorkflowId}`,
            role: "assistant",
            content: "Generating...",
            timestamp: new Date().toLocaleTimeString(),
            isGenerating: true,
            taskId: activeWorkflowId,
        }));

        // Mark messages as loaded to prevent fetchSessionHistory from re-adding them
        hasLoadedMessagesRef.current = true;

        // If this was a new session, fetch the task to obtain the session ID and update the URL
        if (isNewSession) {
            try {
                const taskDetails = await getTask(activeWorkflowId);
                if (taskDetails.session_id) {
                    // Store actual session ID for use in effects (URL param updates can be slow)
                    setActualSessionId(taskDetails.session_id);
                    const newParams = new URLSearchParams(searchParams.toString());
                    newParams.set("session_id", taskDetails.session_id);
                    router.replace(`/run-detail?${newParams.toString()}`);
                }
            } catch (err) {
                console.warn("Failed to refresh session ID after task creation:", err);
            }
        }
    };

    const messageMatchesTask = (message: any, taskId: string | null) => {
        if (!taskId) return false;
        if (message.taskId && message.taskId === taskId) return true;
        return typeof message.id === "string" && message.id.includes(taskId);
    };

    const fetchFinalOutput = useCallback(async () => {
        if (!currentTaskId) {
            console.warn("[RunDetail] fetchFinalOutput called but no currentTaskId");
            return;
        }
        console.log("[RunDetail] 🔍 Fetching authoritative result from task API for:", currentTaskId);
        try {
            const task = await getTask(currentTaskId);
            console.log("[RunDetail] ✓ Task fetched - status:", task.status);
            console.log("[RunDetail] Task result (first 200 chars):", task.result?.substring(0, 200));
            console.log("[RunDetail] Task metadata:", task.metadata);
            if (task.metadata?.citations) {
                console.log("[RunDetail] ✓ Citations found:", task.metadata.citations.length);
            }

            // Validate task is actually complete before trusting the result
            // This prevents showing partial results if completion was signaled prematurely
            if (task.status !== "TASK_STATUS_COMPLETED") {
                console.warn("[RunDetail] ⚠️ Task status is not COMPLETED:", task.status, "- SSE completion may be premature");
                if (task.status === "TASK_STATUS_RUNNING" || task.status === "TASK_STATUS_QUEUED") {
                    // Task is still running, don't mark as complete yet
                    dispatch(setStatus("running"));
                    return;
                } else if (task.status === "TASK_STATUS_FAILED" || task.status === "TASK_STATUS_CANCELLED") {
                    // Task failed or was cancelled
                    dispatch(setStatus("failed"));
                    if (task.status === "TASK_STATUS_CANCELLED") {
                        // Properly update cancelled state - SSE may have missed the event
                        dispatch(setCancelled(true));
                    } else {
                        dispatch(setStreamError(task.error_message || "Task failed"));
                    }
                    return;
                }
            }

            if (!task.result) {
                console.warn("[RunDetail] ⚠️ Task has no result field - task may still be running:", task.status);
                return;
            }

            // Check if we already have ANY assistant message for this task (from SSE streaming)
            // This prevents duplicates when both SSE and fetchFinalOutput create messages
            const hasExistingAssistantMessage = runMessages.some(m =>
                m.role === "assistant" &&
                m.taskId === currentTaskId &&
                !m.isStreaming &&
                !m.isGenerating &&
                m.content && m.content.length > 0
            );

            if (hasExistingAssistantMessage) {
                console.log("[RunDetail] ✓ Assistant message already present from SSE, skipping fetchFinalOutput");
                // Update citations if the task has them and existing message doesn't
                const existingMsg = runMessages.find(m => 
                    m.role === "assistant" && m.taskId === currentTaskId && !m.isStreaming && !m.isGenerating
                );
                if (task.metadata?.citations && (!existingMsg?.metadata?.citations || existingMsg.metadata.citations.length === 0)) {
                    console.log("[RunDetail] Updating existing message with citations from task");
                    dispatch(updateMessageMetadata({ taskId: currentTaskId, metadata: { citations: task.metadata.citations } }));
                }
                return;
            }

            // Add the authoritative result from task.result (canonical value)
            // This only happens if SSE didn't create a message (fallback)
            
            // Ensure result is a string
            let resultContent = task.result;
            if (typeof resultContent === 'object') {
                console.warn("[RunDetail] task.result is an object, extracting text:", resultContent);
                resultContent = (resultContent as any).text || (resultContent as any).message || 
                               (resultContent as any).response || (resultContent as any).content || 
                               JSON.stringify(resultContent);
            }
            
            // Filter out status messages that shouldn't be displayed as conversation content
            if (typeof resultContent === 'string') {
                const lowerResult = resultContent.toLowerCase();
                if (lowerResult.includes('task completed') ||
                    lowerResult.includes('task done') ||
                    lowerResult === 'done' ||
                    lowerResult === 'completed' ||
                    lowerResult === 'success' ||
                    (lowerResult.includes('successfully') && resultContent.length < 100)) {
                    console.warn("[RunDetail] ⚠️ task.result contains status message, not actual content:", resultContent);
                    console.warn("[RunDetail] This indicates the LLM response was not properly captured");
                    dispatch(setStreamError("Task completed but response not captured. Please try again."));
                    return;
                }
            }
            
            console.log("[RunDetail] ➕ Adding authoritative result to messages (length:", resultContent.length, "chars)");
            const messageId = `assistant-final-${currentTaskId}`;
            console.log("[RunDetail] Message ID:", messageId);

            dispatch(addMessage({
                id: messageId,
                role: "assistant",
                content: resultContent,
                timestamp: new Date().toLocaleTimeString(),
                metadata: task.metadata,
                taskId: currentTaskId,
            }));

            console.log("[RunDetail] ✓ Message dispatched to Redux");
            dispatch(setStreamError(null));
        } catch (err) {
            console.error("[RunDetail] ❌ Failed to fetch task result:", err);
            dispatch(setStreamError("Failed to fetch final output"));
        }
    }, [currentTaskId, dispatch, runMessages]);

    const handleFetchFinalOutputClick = () => {
        fetchFinalOutput();
    };

    // Watch for task completion and fetch authoritative final result
    // Per best practice: when WORKFLOW_COMPLETED or STREAM_END arrives, fetch task.result (authoritative)
    useEffect(() => {
        const fetchTaskResult = async () => {
            console.log("[RunDetail] Completion check - status:", runStatus, "taskId:", currentTaskId, "messages:", runMessages.length);

            if (runStatus === "completed" && currentTaskId) {
                console.log("[RunDetail] ✓ Task completed! Fetching authoritative result from task API");
                // Always fetch the authoritative result when completion is signaled
                // The task.result field is the canonical value, not intermediate stream messages
                await fetchFinalOutput();
            } else if (runStatus !== "completed") {
                console.log("[RunDetail] Waiting for completion signal... current status:", runStatus);
            }
        };

        fetchTaskResult();
    }, [runStatus, currentTaskId, fetchFinalOutput]);

    // Fallback polling: If task is "running" but no events received for 30s, poll API to check status
    // This handles cases where STREAM_END is lost due to network issues
    const lastEventCountRef = useRef(0);
    const pollingAttemptRef = useRef(0);
    useEffect(() => {
        // Only poll when running and we have a task ID
        if (runStatus !== "running" || !currentTaskId) {
            lastEventCountRef.current = runEvents.length;
            pollingAttemptRef.current = 0;
            return;
        }

        const POLL_INTERVAL_MS = 30000; // 30 seconds
        const MAX_POLL_ATTEMPTS = 5; // Give up after 5 attempts (2.5 minutes total)

        const pollTaskStatus = async () => {
            // Check if we received new events since last poll
            if (runEvents.length > lastEventCountRef.current) {
                console.log("[RunDetail] New events received, resetting poll timer");
                lastEventCountRef.current = runEvents.length;
                pollingAttemptRef.current = 0;
                return;
            }

            pollingAttemptRef.current += 1;
            if (pollingAttemptRef.current > MAX_POLL_ATTEMPTS) {
                console.log("[RunDetail] Max poll attempts reached, stopping fallback polling");
                return;
            }

            console.log(`[RunDetail] ⏱️ No events for 30s, polling task API (attempt ${pollingAttemptRef.current}/${MAX_POLL_ATTEMPTS})...`);
            try {
                const taskStatus = await getTask(currentTaskId);
                console.log("[RunDetail] Poll result - status:", taskStatus.status, "hasResult:", !!taskStatus.result);
                
                if (taskStatus.status === "TASK_STATUS_COMPLETED" && taskStatus.result) {
                    console.log("[RunDetail] ✓ Task is completed (detected via polling), triggering completion flow");
                    dispatch(setStatus("completed"));
                    // fetchFinalOutput will be triggered by the status change
                } else if (taskStatus.status === "TASK_STATUS_FAILED" || taskStatus.status === "TASK_STATUS_CANCELLED") {
                    console.log("[RunDetail] Task failed/cancelled (detected via polling)");
                    dispatch(setStatus("failed"));
                    if (taskStatus.status === "TASK_STATUS_CANCELLED") {
                        // Properly update cancelled state - SSE may have missed the event
                        dispatch(setCancelled(true));
                    } else {
                        dispatch(setStreamError(taskStatus.error_message || "Task failed"));
                    }
                }
            } catch (err) {
                console.warn("[RunDetail] Fallback poll failed:", err);
            }
        };

        const pollInterval = setInterval(pollTaskStatus, POLL_INTERVAL_MS);
        
        return () => clearInterval(pollInterval);
    }, [runStatus, currentTaskId, runEvents.length, dispatch]);

    // Fetch control-state on page load when task is running
    useEffect(() => {
        if (currentTaskId && runStatus === "running") {
            getTaskControlState(currentTaskId)
                .then(state => {
                    dispatch(setPaused({
                        paused: state.is_paused,
                        reason: state.pause_reason || undefined,
                    }));
                    if (state.is_cancelled) {
                        dispatch(setCancelled(true));
                    }
                })
                .catch(err => {
                    console.warn("[RunDetail] Failed to fetch control-state:", err);
                });
        }
    }, [currentTaskId, runStatus, dispatch]);

    // Periodic control-state refresh during pause (every 20s) in case SSE was missed
    useEffect(() => {
        if (!isPaused || !currentTaskId) return;

        const REFRESH_INTERVAL_MS = 20000; // 20 seconds

        const refreshControlState = async () => {
            try {
                const state = await getTaskControlState(currentTaskId);
                if (!state.is_paused) {
                    // SSE missed, sync state
                    dispatch(setPaused({ paused: false }));
                }
                if (state.is_cancelled) {
                    dispatch(setCancelled(true));
                }
            } catch (err) {
                console.warn("[RunDetail] Failed to refresh control-state:", err);
            }
        };

        const interval = setInterval(refreshControlState, REFRESH_INTERVAL_MS);
        return () => clearInterval(interval);
    }, [isPaused, currentTaskId, dispatch]);

    // Periodic control-state refresh during cancelling (every 2s) - SSE often misses workflow.cancelled
    useEffect(() => {
        if (!isCancelling || !currentTaskId) return;

        const CANCEL_POLL_INTERVAL_MS = 2000; // 2 seconds - poll aggressively during cancel

        const checkCancelledState = async () => {
            try {
                // First try control-state API
                const state = await getTaskControlState(currentTaskId);
                if (state.is_cancelled) {
                    console.log("[RunDetail] Cancellation confirmed via control-state polling");
                    dispatch(setCancelled(true));
                    return;
                }
                
                // Fallback: check task status directly
                const taskStatus = await getTask(currentTaskId);
                if (taskStatus.status === "TASK_STATUS_CANCELLED") {
                    console.log("[RunDetail] Cancellation confirmed via task status polling");
                    dispatch(setCancelled(true));
                }
            } catch (err) {
                console.warn("[RunDetail] Failed to check cancelled state:", err);
            }
        };

        // Check immediately, then poll
        checkCancelledState();
        const interval = setInterval(checkCancelledState, CANCEL_POLL_INTERVAL_MS);
        return () => clearInterval(interval);
    }, [isCancelling, currentTaskId, dispatch]);

    // Reset button loading states when pause/resume SSE events update Redux state
    useEffect(() => {
        // When paused: reset pause loading (pause completed)
        // When not paused: reset resume loading (resume completed)
        // Always reset both to handle page load scenarios
        if (isPaused) {
            setIsPauseLoading(false);
            // Also reset resume loading in case page loaded mid-transition
            setIsResumeLoading(false);
        } else {
            setIsResumeLoading(false);
            // Also reset pause loading in case page loaded mid-transition
            setIsPauseLoading(false);
        }
    }, [isPaused]);

    // Pause/Resume/Cancel handlers
    const handlePause = async () => {
        if (!currentTaskId) return;
        setIsPauseLoading(true);
        try {
            await pauseTask(currentTaskId);
            // Button stays disabled until WORKFLOW_PAUSED SSE arrives
        } catch (err) {
            console.error("[RunDetail] Failed to pause task:", err);
            setIsPauseLoading(false);
            dispatch(setStreamError(err instanceof Error ? err.message : "Failed to pause task"));
        }
    };

    const handleResume = async () => {
        if (!currentTaskId) return;
        setIsResumeLoading(true);
        try {
            await resumeTask(currentTaskId);
            // Button stays disabled until WORKFLOW_RESUMED SSE arrives
        } catch (err) {
            console.error("[RunDetail] Failed to resume task:", err);
            setIsResumeLoading(false);
            dispatch(setStreamError(err instanceof Error ? err.message : "Failed to resume task"));
        }
    };

    const handleCancel = async () => {
        if (!currentTaskId) return;
        dispatch(setCancelling(true));
        try {
            await cancelTask(currentTaskId);
            // UI updates via SSE (workflow.cancelling, workflow.cancelled)
        } catch (err) {
            console.error("[RunDetail] Failed to cancel task:", err);
            dispatch(setCancelling(false));
            dispatch(setStreamError(err instanceof Error ? err.message : "Failed to cancel task"));
        }
    };

    // Review Plan (HITL) handlers
    const handleReviewPlanChange = (mode: ReviewPlanMode) => {
        dispatch(setReviewPlan(mode));
    };

    const handleReviewFeedback = async (version: number, intent: "feedback" | "approve", planMessage: string, round: number, userMessage: string) => {
        dispatch(setReviewVersion(version));
        dispatch(setReviewIntent(intent));

        // Add user feedback message
        dispatch(addMessage({
            id: `review-feedback-${Date.now()}`,
            role: "user",
            content: userMessage,
            timestamp: new Date().toLocaleTimeString(),
            taskId: reviewWorkflowId,
        }));

        // Add updated plan message
        dispatch(addMessage({
            id: `research-plan-${reviewWorkflowId}-${Date.now()}`,
            role: "assistant",
            content: planMessage,
            timestamp: new Date().toLocaleTimeString(),
            taskId: reviewWorkflowId,
            isResearchPlan: true,
            planRound: round,
        }));

        // Auto-approve when LLM detects approve intent
        if (intent === "approve" && reviewWorkflowId) {
            try {
                await approveReviewPlan(reviewWorkflowId);
                handleReviewApprove();
            } catch (err) {
                console.error("[RunDetail] Auto-approve failed:", err);
                // Keep the Approve & Run button visible as fallback
            }
        }
    };

    const handleReviewApprove = () => {
        dispatch(setReviewStatus("approved"));
        dispatch(setReviewIntent(null));
    };

    // Helper to categorize event type
    const categorizeEvent = (eventType: string): "agent" | "llm" | "tool" | "system" => {
        if (eventType.includes("AGENT") || eventType.includes("DELEGATION") ||
            eventType.includes("TEAM") || eventType.includes("ROLE")) return "agent";
        if (eventType.includes("LLM") || eventType === "thread.message.completed") return "llm";
        if (eventType.includes("TOOL")) return "tool";
        return "system";
    };

    // Helper to determine event status
    const getEventStatus = (eventType: string): "completed" | "running" | "failed" | "pending" => {
        if (eventType === "ERROR_OCCURRED" || eventType === "error" || 
            eventType === "TASK_FAILED" || eventType === "TASK_CANCELLED" ||
            eventType === "WORKFLOW_FAILED") return "failed";
        if (eventType.includes("STARTED") || eventType === "AGENT_THINKING" ||
            eventType === "WAITING" || eventType === "APPROVAL_REQUESTED") return "running";
        if (eventType.includes("COMPLETED") || eventType === "thread.message.completed" ||
            eventType.includes("OBSERVATION") || eventType === "APPROVAL_DECISION" ||
            eventType === "DEPENDENCY_SATISFIED" || eventType === "done") return "completed";
        return "completed";
    };

    // Helper to extract details from event (JSON payload or verbose message content)
    const extractEventDetails = (event: any): { details?: string, detailsType?: "json" | "text" } => {
        // Priority 1: JSON payload
        if (event.payload) {
            // Backend returns payload as JSON string, streaming returns as object
            let payloadStr: string;
            if (typeof event.payload === "string") {
                const raw = event.payload;
                try {
                    const parsed = JSON.parse(raw);
                    payloadStr = JSON.stringify(parsed, null, 2);
                } catch {
                    payloadStr = raw;
                }
            } else {
                payloadStr = JSON.stringify(event.payload, null, 2);
            }

            return {
                details: payloadStr,
                detailsType: "json"
            };
        }

        // Priority 2: Extract verbose content from message
        if (event.message) {
            // For "Thinking: ..." messages, extract the thinking content as details
            if (event.message.startsWith("Thinking:")) {
                return {
                    details: event.message,
                    detailsType: "text"
                };
            }
        }

        return {};
    };

    // Helper to create friendly, normalized title from event
    const getFriendlyTitle = (event: any): string => {
        // Normalize verbose messages into concise titles
        if (event.message) {
            // "Thinking: REASON..." -> "Agent is reasoning"
            if (event.message.startsWith("Thinking: REASON")) {
                return "Agent is reasoning";
            }
            // "Thinking: ACT..." -> "Agent is planning action"
            if (event.message.startsWith("Thinking: ACT")) {
                return "Agent is planning action";
            }
            // "Thinking: ..." (generic) -> "Agent is thinking"
            if (event.message.startsWith("Thinking:")) {
                return "Agent is thinking";
            }
            // "Expanded query into N research areas" -> "Expanded research query"
            if (event.message.includes("Expanded query into")) {
                return "Expanded research query";
            }
            // "Refining research query" -> keep as is (already concise)
            if (event.message.includes("Refining research query")) {
                return "Refining research query";
            }
            // "Analyzing gathered results" -> keep as is
            if (event.message.includes("Analyzing")) {
                return event.message.split("\n")[0]; // Take first line only
            }
            // For other messages with payload, extract first line as title
            if (event.payload) {
                const firstLine = event.message.split("\n")[0];
                return firstLine.length > 50 ? firstLine.substring(0, 50) + "..." : firstLine;
            }
            // Default: return message as-is if it's already concise
            if (event.message.length <= 50) {
                return event.message;
            }
        }

        // Fallback to type-based mapping
        const typeMap: Record<string, string> = {
            "WORKFLOW_STARTED": "Workflow Started",
            "WORKFLOW_COMPLETED": "Workflow Completed",
            "AGENT_STARTED": "Agent Started",
            "AGENT_COMPLETED": "Agent Completed",
            "AGENT_THINKING": "Agent is thinking",
            "TOOL_INVOKED": "Tool Called",
            "TOOL_OBSERVATION": "Tool Result",
            "DELEGATION": "Task Delegated",
            "ROLE_ASSIGNED": "Role Assigned",
            "TEAM_RECRUITED": "Agent Recruited",
            "TEAM_RETIRED": "Agent Retired",
            "TEAM_STATUS": "Team Update",
            "PROGRESS": "Progress Update",
            "DATA_PROCESSING": "Processing Data",
            "WAITING": "Waiting",
            "ERROR_RECOVERY": "Recovering from Error",
            "ERROR_OCCURRED": "Error Occurred",
            "BUDGET_THRESHOLD": "Budget Alert",
            "DEPENDENCY_SATISFIED": "Dependency Ready",
            "APPROVAL_REQUESTED": "Awaiting Approval",
            "APPROVAL_DECISION": "Approval Decision",
            "MESSAGE_SENT": "Message Sent",
            "MESSAGE_RECEIVED": "Message Received",
            "WORKSPACE_UPDATED": "Workspace Updated",
            "STATUS_UPDATE": "Status Update",
            "thread.message.completed": "LLM Response",
            "done": "Task Done",
            "STREAM_END": "Stream Ended",
        };
        return typeMap[event.type] || event.type;
    };

    // Only process timeline events when timeline is visible (performance optimization)
    const timelineEvents = useMemo(() => {
        if (!showTimeline) return [];
        
        const excludedEventTypes = new Set([
            "thread.message.delta",
            "thread.message.completed",
            "LLM_PROMPT",
            "LLM_OUTPUT",
        ]);

        const filteredRunEvents = runEvents
            .filter((event: any) => !excludedEventTypes.has(event.type) && event.type);

        // Deduplicate excessive BUDGET_THRESHOLD events
        // Keep only the first one and then one every 100% increase, with a max limit
        const deduplicatedEvents = filteredRunEvents.reduce((acc: any[], event: any) => {
            if (event.type === "BUDGET_THRESHOLD") {
                // Extract percentage from message like "Task budget at 85.0% (threshold: 80.0%)"
                const match = event.message?.match(/Task budget at ([\d.]+)%/);
                if (match) {
                    const currentPercent = parseFloat(match[1]);

                    // Find all budget events we've kept
                    const budgetEventsKept = acc.filter((e: any) => e.type === "BUDGET_THRESHOLD");
                    const MAX_BUDGET_EVENTS = 5; // Limit total budget events in timeline

                    if (budgetEventsKept.length === 0) {
                        // First budget event, keep it
                        acc.push(event);
                    } else if (budgetEventsKept.length < MAX_BUDGET_EVENTS) {
                        // Check if this is a significant increase (100% or more)
                        const lastBudgetEvent = budgetEventsKept[budgetEventsKept.length - 1];
                        const lastMatch = lastBudgetEvent.message?.match(/Task budget at ([\d.]+)%/);
                        if (lastMatch) {
                            const lastPercent = parseFloat(lastMatch[1]);
                            if (currentPercent - lastPercent >= 100) {
                                // Significant increase, keep this event
                                acc.push(event);
                            }
                            // Otherwise skip this duplicate budget event
                        }
                    }
                    // Skip if we've already kept MAX_BUDGET_EVENTS
                } else {
                    // Can't parse percentage, keep the event anyway
                    acc.push(event);
                }
            } else {
                // Not a budget event, keep it
                acc.push(event);
            }
            return acc;
        }, []);

        // Track which workflows have completed by looking for WORKFLOW_COMPLETED events
        const completedWorkflows = new Set(
            runEvents
                .filter((e: any) => e.type === "WORKFLOW_COMPLETED")
                .map((e: any) => e.workflow_id)
        );

        return deduplicatedEvents.map((event: any, index) => {
            // Check if this event's workflow has completed
            const workflowCompleted = completedWorkflows.has(event.workflow_id);

            // If this workflow is completed, mark all its events as completed (static)
            // Otherwise, use the event's natural status (may be running/breathing)
            const eventStatus = workflowCompleted
                ? "completed"
                : getEventStatus(event.type);

            // Create a unique ID by combining multiple properties
            // Use stream_id if available, otherwise create a composite key
            const uniqueId = event.stream_id ||
                `${event.workflow_id || 'unknown'}-${event.type}-${event.seq || event.timestamp || index}`;

            // Extract details and determine type
            const { details, detailsType } = extractEventDetails(event);

            return {
                id: uniqueId,
                type: categorizeEvent(event.type),
                status: eventStatus,
                title: getFriendlyTitle(event),
                timestamp: event.timestamp ? new Date(event.timestamp).toLocaleTimeString() : "",
                details,
                detailsType
            };
        });
    }, [showTimeline, runEvents]);

    // Combine historical messages with streaming messages
    // Redux `runMessages` should now contain both if we populated it correctly
    // Use a stable reference to messages to prevent unnecessary re-renders
    const messages = useMemo(() => runMessages, [runMessages]);

    // Debug logging to track message disappearance issue
    useEffect(() => {
        const userMessages = messages.filter((m: any) => m.role === "user");
        console.log(`[RunDetail] Messages updated: ${messages.length} total, ${userMessages.length} user messages`);
        if (userMessages.length > 0) {
            console.log(`[RunDetail] User message IDs:`, userMessages.map((m: any) => m.id));
        }
    }, [messages]);

    // Auto-scroll timeline to bottom when events change (only when visible)
    useEffect(() => {
        if (showTimeline && timelineScrollRef.current) {
            const scrollContainer = timelineScrollRef.current.querySelector('[data-slot="scroll-area-viewport"]');
            if (scrollContainer) {
                // Use requestAnimationFrame to avoid blocking render
                requestAnimationFrame(() => {
                    scrollContainer.scrollTop = scrollContainer.scrollHeight;
                });
            }
        }
    }, [showTimeline, timelineEvents]);

    // Track user scroll to avoid fighting with manual scrolling
    useEffect(() => {
        if (!conversationScrollRef.current) return;
        
        const scrollContainer = conversationScrollRef.current.querySelector('[data-slot="scroll-area-viewport"]');
        if (!scrollContainer) return;

        const handleScroll = () => {
            // Check if user is near bottom (within 100px)
            const isNearBottom = scrollContainer.scrollHeight - scrollContainer.scrollTop - scrollContainer.clientHeight < 100;
            
            // If user scrolled up (not near bottom) during a running task, mark it
            if (!isNearBottom && runStatus === "running") {
                userHasScrolledRef.current = true;
            }
            // If user scrolled back to bottom, reset the flag
            if (isNearBottom) {
                userHasScrolledRef.current = false;
            }
        };

        scrollContainer.addEventListener('scroll', handleScroll, { passive: true });
        return () => scrollContainer.removeEventListener('scroll', handleScroll);
    }, [runStatus]);

    // Reset scroll tracking when a new task starts
    useEffect(() => {
        if (runStatus === "running") {
            // Reset only at the start of a new task, not during
            // Check if this is a fresh start (messages just got a user message)
            const hasOnlyUserMessage = messages.length === 1 && messages[0]?.role === "user";
            if (hasOnlyUserMessage) {
                userHasScrolledRef.current = false;
            }
        }
    }, [runStatus, messages.length]);

    // Auto-scroll conversation to bottom when messages change (only while streaming and user hasn't scrolled up)
    useEffect(() => {
        // Only auto-scroll while task is running and user hasn't manually scrolled up
        if (runStatus !== "running" && runStatus !== "idle") return;
        if (userHasScrolledRef.current) return;
        
        if (conversationScrollRef.current && activeTab === "conversation") {
            const scrollContainer = conversationScrollRef.current.querySelector('[data-slot="scroll-area-viewport"]');
            if (scrollContainer) {
                // Use requestAnimationFrame to avoid blocking render
                requestAnimationFrame(() => {
                    scrollContainer.scrollTop = scrollContainer.scrollHeight;
                });
            }
        }
    }, [messages, activeTab, runStatus]);

    if (isLoading) {
        return (
            <div className="flex items-center justify-center h-screen">
                <div className="text-center space-y-4">
                    <Loader2 className="h-8 w-8 animate-spin mx-auto text-primary" />
                    <div>
                        <h2 className="text-xl font-semibold">Loading session...</h2>
                    </div>
                </div>
            </div>
        );
    }

    if (error) {
        return (
            <div className="flex items-center justify-center h-screen">
                <div className="text-center space-y-4">
                    <div className="text-red-500 text-5xl mb-4">⚠️</div>
                    <h2 className="text-2xl font-bold">Failed to load session</h2>
                    <p className="text-muted-foreground">{error}</p>
                    <Button asChild>
                        <Link href="/runs">Go Back</Link>
                    </Button>
                </div>
            </div>
        );
    }

    return (
        <div className="flex h-full flex-col overflow-hidden">
            {/* Header */}
            <header className="flex h-14 items-center justify-between gap-2 border-b px-3 sm:px-6 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60 shrink-0 overflow-hidden">
                <div className="flex items-center gap-2 sm:gap-4 min-w-0 flex-1">
                    {!isNewSession && (
                        <Button variant="ghost" size="icon" asChild className="shrink-0">
                            <Link href="/runs">
                                <ArrowLeft className="h-4 w-4" />
                            </Link>
                        </Button>
                    )}
                    {!isNewSession && (
                        <div className="min-w-0 flex-1 flex items-center gap-2">
                            <h1 className="text-base sm:text-lg font-semibold truncate" title={sessionTitle || `Session ${sessionId?.slice(0, 8)}...`}>
                                {sessionTitle || `Session ${sessionId?.slice(0, 8)}...`}
                            </h1>
                            {isPaused && (
                                <Badge variant="outline" className="bg-amber-50 dark:bg-amber-900/20 text-amber-700 dark:text-amber-300 border-amber-200 dark:border-amber-800 text-xs shrink-0">
                                    Paused
                                </Badge>
                            )}
                            {isCancelling && (
                                <Badge variant="outline" className="bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 border-red-200 dark:border-red-800 text-xs shrink-0">
                                    <Loader2 className="h-3 w-3 animate-spin mr-1" />
                                    Cancelling...
                                </Badge>
                            )}
                            {isCancelled && !isCancelling && (
                                <Badge variant="outline" className="bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 border-red-200 dark:border-red-800 text-xs shrink-0">
                                    Cancelled
                                </Badge>
                            )}
                        </div>
                    )}
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    <Select
                        value={selectedAgent}
                        onValueChange={(val) => dispatch(setSelectedAgent(val as AgentSelection))}
                    >
                        <SelectTrigger className="h-8 sm:h-9 w-32 sm:w-48 text-xs sm:text-sm">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="normal">
                                <div className="flex items-center gap-2">
                                    <Sparkles className="h-4 w-4 text-amber-500" />
                                    Everyday Agent
                                </div>
                            </SelectItem>
                            <SelectItem value="deep_research">
                                <div className="flex items-center gap-2">
                                    <Microscope className="h-4 w-4 text-violet-500" />
                                    Deep Research
                                </div>
                            </SelectItem>
                        </SelectContent>
                    </Select>

{/* Share and Export buttons hidden - features not yet implemented */}
                </div>
            </header>

            {(connectionState === "error" || streamError) && (
                <div className="flex items-center justify-between gap-3 border-b border-red-200 bg-red-50 px-6 py-3 shrink-0">
                    <div className="text-sm text-red-700">
                        {streamError || "Stream connection error"}
                    </div>
                    <div className="flex gap-2">
                        <Button variant="outline" size="sm" onClick={handleRetryStream}>
                            Retry stream
                        </Button>
                        <Button size="sm" onClick={handleFetchFinalOutputClick}>
                            Fetch final output
                        </Button>
                    </div>
                </div>
            )}

            {/* Main Content - Split View */}
            <div className="flex flex-1 overflow-hidden">
                {/* Left Column: Conversation Tabs */}
                <div className="flex-1 bg-background flex flex-col">
                    <Tabs defaultValue="conversation" value={activeTab} onValueChange={setActiveTab} className="h-full flex flex-col">
                        <div className="px-4 pt-4 shrink-0 flex items-center justify-between gap-4">
                            <TabsList>
                                <TabsTrigger value="conversation">
                                    Conversation
                                </TabsTrigger>
                                <TabsTrigger value="summary">
                                    Summary
                                </TabsTrigger>
                            </TabsList>
                            <div className="flex items-center gap-2">
                                {(runEvents.length > 0 || runStatus === "running") && (
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        onClick={() => setShowTimeline(!showTimeline)}
                                        className="gap-2"
                                    >
                                        {showTimeline ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                                        {showTimeline ? "Hide Timeline" : "Show Timeline"}
                                    </Button>
                                )}
                            </div>
                        </div>

                        <TabsContent value="conversation" className="flex-1 p-0 m-0 data-[state=active]:flex flex-col overflow-hidden">
                            {messages.length > 0 ? (
                                <>
                                    {/* Review Plan banner */}
                                    {reviewStatus === "reviewing" && (
                                        <div className="flex items-center gap-2 px-4 py-2 bg-violet-50 dark:bg-violet-950 border-b border-violet-200 dark:border-violet-800 shrink-0">
                                            <Eye className="h-4 w-4 text-violet-600 dark:text-violet-400" />
                                            <span className="text-sm text-violet-700 dark:text-violet-300">
                                                Research plan ready for review
                                            </span>
                                        </div>
                                    )}

                                    <div className="flex-1 min-h-0">
                                        <ScrollArea className="h-full" ref={conversationScrollRef}>
                                            <RunConversation messages={messages as any} agentType={selectedAgent} />
                                        </ScrollArea>
                                    </div>

                                    {/* Chat Input Box - compact for follow-up messages */}
                                    <div className="border-t bg-background p-4 shrink-0">
                                        <ChatInput
                                            sessionId={isNewSession ? undefined : sessionId ?? undefined}
                                            disabled={runStatus === "running" && reviewStatus !== "reviewing"}
                                            isTaskComplete={runStatus !== "running"}
                                            selectedAgent={selectedAgent}
                                            initialResearchStrategy={researchStrategy}
                                            initialReviewPlan={reviewPlan}
                                            onTaskCreated={handleTaskCreated}
                                            isTaskRunning={runStatus === "running" && reviewStatus !== "reviewing"}
                                            isPaused={isPaused}
                                            isPauseLoading={isPauseLoading}
                                            isResumeLoading={isResumeLoading}
                                            isCancelling={isCancelling}
                                            onPause={handlePause}
                                            onResume={handleResume}
                                            onCancel={handleCancel}
                                            reviewStatus={reviewStatus}
                                            reviewWorkflowId={reviewWorkflowId}
                                            reviewVersion={reviewVersion}
                                            reviewIntent={reviewIntent}
                                            onReviewPlanChange={handleReviewPlanChange}
                                            onReviewFeedback={handleReviewFeedback}
                                            onApprove={handleReviewApprove}
                                        />
                                    </div>
                                </>
                            ) : (
                                /* Empty state - centered input for new sessions */
                                <ChatInput
                                    sessionId={isNewSession ? undefined : sessionId ?? undefined}
                                    disabled={runStatus === "running"}
                                    isTaskComplete={runStatus !== "running"}
                                    selectedAgent={selectedAgent}
                                    initialResearchStrategy={researchStrategy}
                                    initialReviewPlan={reviewPlan}
                                    onTaskCreated={handleTaskCreated}
                                    variant="centered"
                                    isTaskRunning={runStatus === "running"}
                                    isPaused={isPaused}
                                    isPauseLoading={isPauseLoading}
                                    isResumeLoading={isResumeLoading}
                                    isCancelling={isCancelling}
                                    onPause={handlePause}
                                    onResume={handleResume}
                                    onCancel={handleCancel}
                                    reviewStatus={reviewStatus}
                                    reviewWorkflowId={reviewWorkflowId}
                                    reviewVersion={reviewVersion}
                                    reviewIntent={reviewIntent}
                                    onReviewPlanChange={handleReviewPlanChange}
                                    onReviewFeedback={handleReviewFeedback}
                                    onApprove={handleReviewApprove}
                                />
                            )}
                        </TabsContent>

                        <TabsContent value="summary" className="flex-1 p-4 sm:p-6 m-0 overflow-auto min-h-0">
                            <div className="max-w-4xl mx-auto space-y-4 overflow-hidden">
                                <div>
                                    <h2 className="text-xl font-bold">Session Summary</h2>
                                    <p className="text-sm text-muted-foreground">Overview of your conversation and resource usage</p>
                                </div>

                                {/* Key Metrics */}
                                <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Turns</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">{sessionHistory?.tasks.length || sessionData?.turns.length || 0}</div>
                                    </Card>
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Costs</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">
                                            ${(sessionHistory?.tasks.reduce((sum: number, task: any) => sum + (task.total_cost_usd || 0), 0) || 0).toFixed(4)}
                                        </div>
                                    </Card>
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Tokens</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">
                                            {(sessionHistory?.tasks.reduce((sum: number, task: any) => sum + (task.total_tokens || 0), 0) || sessionData?.turns.reduce((sum, turn) => sum + (turn.metadata?.tokens_used || 0), 0) || 0).toLocaleString()}
                                        </div>
                                    </Card>
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Time</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">
                                            {formatDuration((sessionHistory?.tasks.reduce((sum: number, task: any) => sum + (task.duration_ms || 0), 0) || sessionData?.turns.reduce((sum, turn) => sum + (turn.metadata?.execution_time_ms || 0), 0) || 0) / 1000)}
                                        </div>
                                    </Card>
                                </div>

                                {/* Token Usage Details */}
                                {sessionHistory?.tasks && sessionHistory.tasks.length > 0 && (
                                    <Card className="p-3 sm:p-4 overflow-hidden">
                                        <h3 className="text-base font-semibold mb-3">Token Usage by Turn</h3>
                                        <div className="space-y-2">
                                            {sessionHistory.tasks.map((task: any, index: number) => (
                                                <div key={task.task_id} className="py-2 border-b last:border-b-0">
                                                    <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
                                                        <div className="flex-1 min-w-0">
                                                            <div className="text-xs font-medium truncate">Turn {index + 1}</div>
                                                            <div className="text-xs text-muted-foreground truncate">{task.query}</div>
                                                            {(task.model_used || task.metadata?.model) && (
                                                                <div className="text-xs text-muted-foreground mt-0.5 truncate">
                                                                    {task.model_used || task.metadata?.model}
                                                                    {(task.provider || task.metadata?.provider) && ` (${task.provider || task.metadata?.provider})`}
                                                                </div>
                                                            )}
                                                        </div>
                                                        <div className="flex items-center gap-3 sm:gap-4 sm:ml-4 flex-shrink-0">
                                                            <div className="text-left sm:text-right">
                                                                <div className="text-sm font-medium">{(task.total_tokens || 0).toLocaleString()}</div>
                                                                <div className="text-xs text-muted-foreground">tokens</div>
                                                            </div>
                                                            <div className="text-left sm:text-right">
                                                                <div className="text-sm font-medium">${(task.total_cost_usd || 0).toFixed(4)}</div>
                                                                <div className="text-xs text-muted-foreground">cost</div>
                                                            </div>
                                                            <div className="text-left sm:text-right">
                                                                <div className="text-sm font-medium">{formatDuration((task.duration_ms || 0) / 1000)}</div>
                                                                <div className="text-xs text-muted-foreground">time</div>
                                                            </div>
                                                        </div>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    </Card>
                                )}

                                {/* Models Used - Enhanced with agent_usages/model_breakdown data */}
                                {sessionHistory?.tasks && sessionHistory.tasks.length > 0 && (() => {
                                    // Aggregate model usage across all tasks for accurate multi-model tracking
                                    // Priority: agent_usages > model_breakdown > fallback to task-level model
                                    const modelUsage = new Map<string, { 
                                        model: string;
                                        provider: string;
                                        executions: number; 
                                        tokens: number; 
                                        cost: number;
                                        inputTokens: number;
                                        outputTokens: number;
                                    }>();
                                    
                                    let totalCost = 0;
                                    let hasDetailedData = false;
                                    
                                    sessionHistory.tasks.forEach((task: any) => {
                                        // First try model_breakdown (complete data from token_usage table)
                                        const modelBreakdown = task.metadata?.model_breakdown;
                                        if (modelBreakdown && Array.isArray(modelBreakdown) && modelBreakdown.length > 0) {
                                            hasDetailedData = true;
                                            task.metadata.model_breakdown.forEach((entry: any) => {
                                                const key = `${entry.model}|${entry.provider || 'unknown'}`;
                                                const existing = modelUsage.get(key);
                                                const cost = entry.cost_usd || 0;
                                                if (existing) {
                                                    existing.executions += entry.executions || 1;
                                                    existing.tokens += entry.tokens || 0;
                                                    existing.cost += cost;
                                                } else {
                                                    modelUsage.set(key, {
                                                        model: entry.model,
                                                        provider: entry.provider || 'unknown',
                                                        executions: entry.executions || 1,
                                                        tokens: entry.tokens || 0,
                                                        cost,
                                                        inputTokens: 0,
                                                        outputTokens: 0,
                                                    });
                                                }
                                                totalCost += cost;
                                            });
                                        } else {
                                            // Fallback for tasks without detailed breakdown
                                            const model = task.model_used || task.metadata?.model;
                                            const provider = task.provider || task.metadata?.provider || 'unknown';
                                            if (model) {
                                                const key = `${model}|${provider}`;
                                                const existing = modelUsage.get(key);
                                                const cost = task.total_cost_usd || 0;
                                                if (existing) {
                                                    existing.executions += 1;
                                                    existing.tokens += task.total_tokens || 0;
                                                    existing.cost += cost;
                                                } else {
                                                    modelUsage.set(key, {
                                                        model,
                                                        provider,
                                                        executions: 1,
                                                        tokens: task.total_tokens || 0,
                                                        cost,
                                                        inputTokens: 0,
                                                        outputTokens: 0,
                                                    });
                                                }
                                                totalCost += cost;
                                            }
                                        }
                                    });
                                    
                                    // Sort by cost descending
                                    const sortedModels = Array.from(modelUsage.values())
                                        .sort((a, b) => b.cost - a.cost);
                                    
                                    // Calculate percentages
                                    const modelsWithPercentage = sortedModels.map(m => ({
                                        ...m,
                                        percentage: totalCost > 0 ? Math.round((m.cost / totalCost) * 100) : 0
                                    }));
                                    
                                    // Color palette for progress bars
                                    const barColors = [
                                        'bg-blue-500',
                                        'bg-emerald-500', 
                                        'bg-amber-500',
                                        'bg-purple-500',
                                        'bg-rose-500',
                                        'bg-cyan-500',
                                    ];
                                    
                                    return modelsWithPercentage.length > 0 ? (
                                        <Card className="p-3 sm:p-4 overflow-hidden">
                                            <div className="flex items-center justify-between mb-3">
                                                <h3 className="text-base font-semibold">Models Used</h3>
                                                {hasDetailedData && (
                                                    <span className="text-[10px] text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
                                                        detailed
                                                    </span>
                                                )}
                                            </div>
                                            <div className="space-y-3">
                                                {modelsWithPercentage.map((usage, index) => (
                                                    <div key={`${usage.model}-${usage.provider}`} className="space-y-1.5">
                                                        <div className="flex items-center justify-between text-xs">
                                                            <div className="flex items-center gap-2 min-w-0">
                                                                <div 
                                                                    className={`w-2 h-2 rounded-full flex-shrink-0 ${barColors[index % barColors.length]}`}
                                                                />
                                                                <span className="font-medium truncate">{usage.model}</span>
                                                                <span className="text-muted-foreground text-[10px] flex-shrink-0">
                                                                    {usage.provider}
                                                                </span>
                                                            </div>
                                                            <span className="text-muted-foreground flex-shrink-0 ml-2">
                                                                {usage.percentage}%
                                                            </span>
                                                        </div>
                                                        {/* Progress bar */}
                                                        <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                                                            <div 
                                                                className={`h-full rounded-full transition-all duration-500 ${barColors[index % barColors.length]}`}
                                                                style={{ width: `${Math.max(usage.percentage, 2)}%` }}
                                                            />
                                                        </div>
                                                        {/* Stats row */}
                                                        <div className="flex items-center gap-3 text-[10px] text-muted-foreground pl-4">
                                                            <span>{usage.executions} {usage.executions === 1 ? 'call' : 'calls'}</span>
                                                            {usage.inputTokens > 0 && usage.outputTokens > 0 ? (
                                                                <span className="flex items-center gap-1">
                                                                    <span className="text-blue-500/70">↓{usage.inputTokens.toLocaleString()}</span>
                                                                    <span>/</span>
                                                                    <span className="text-emerald-500/70">↑{usage.outputTokens.toLocaleString()}</span>
                                                                </span>
                                                            ) : (
                                                                <span>{usage.tokens.toLocaleString()} tokens</span>
                                                            )}
                                                            <span className="font-medium text-foreground">${usage.cost.toFixed(4)}</span>
                                                        </div>
                                                    </div>
                                                ))}
                                            </div>
                                        </Card>
                                    ) : null;
                                })()}

                                {/* Agents Involved */}
                                {sessionHistory?.tasks && sessionHistory.tasks.length > 0 && (() => {
                                    const allAgents = new Set<string>();
                                    sessionHistory.tasks.forEach((task: any) => {
                                        // Extract agents from metadata if available
                                        const agents = task.metadata?.agents_involved || [];
                                        agents.forEach((agent: any) => allAgents.add(agent));
                                    });
                                    return allAgents.size > 0 ? (
                                        <Card className="p-3 sm:p-4">
                                            <h3 className="text-base font-semibold mb-3">Agents Involved</h3>
                                            <div className="flex flex-wrap gap-2">
                                                {Array.from(allAgents).map(agent => (
                                                    <Badge key={agent} variant="secondary" className="text-xs truncate max-w-full">
                                                        {agent}
                                                    </Badge>
                                                ))}
                                            </div>
                                        </Card>
                                    ) : null;
                                })()}

                                {/* Average Metrics */}
                                {sessionHistory?.tasks && sessionHistory.tasks.length > 0 && (
                                    <Card className="p-3 sm:p-4">
                                        <h3 className="text-base font-semibold mb-3">Average Metrics</h3>
                                        <div className="grid grid-cols-2 gap-3 sm:gap-4">
                                            <div>
                                                <div className="text-xs text-muted-foreground">Avg. Tokens per Turn</div>
                                                <div className="text-lg font-bold mt-1">
                                                    {Math.round(
                                                        sessionHistory.tasks.reduce((sum: number, task: any) => sum + (task.total_tokens || 0), 0) / sessionHistory.tasks.length
                                                    ).toLocaleString()}
                                                </div>
                                            </div>
                                            <div>
                                                <div className="text-xs text-muted-foreground">Avg. Time per Turn</div>
                                                <div className="text-lg font-bold mt-1">
                                                    {formatDuration(
                                                        sessionHistory.tasks.reduce((sum: number, task: any) => sum + (task.duration_ms || 0), 0) /
                                                        sessionHistory.tasks.length / 1000
                                                    )}
                                                </div>
                                            </div>
                                        </div>
                                    </Card>
                                )}

                                {/* Session Info */}
                                <Card className="p-3 sm:p-4 overflow-hidden">
                                    <h3 className="text-base font-semibold mb-3">Session Information</h3>
                                    <div className="space-y-2 text-xs">
                                        <div className="flex justify-between items-center gap-2">
                                            <span className="text-muted-foreground shrink-0">Session ID</span>
                                            <span className="font-mono text-xs truncate min-w-0">{sessionId}</span>
                                        </div>
                                        {currentTaskId && (
                                            <div className="flex justify-between items-center gap-2">
                                                <span className="text-muted-foreground shrink-0">Current Task ID</span>
                                                <span className="font-mono text-xs truncate min-w-0">{currentTaskId}</span>
                                            </div>
                                        )}
                                        <div className="flex justify-between items-center">
                                            <span className="text-muted-foreground">Status</span>
                                            <Badge
                                                variant="outline"
                                                className={`text-xs ${
                                                    runStatus === "completed"
                                                        ? "bg-emerald-50 text-emerald-700 border-emerald-200"
                                                        : runStatus === "running"
                                                            ? "bg-blue-50 text-blue-700 border-blue-200"
                                                            : runStatus === "failed"
                                                                ? "bg-red-50 text-red-700 border-red-200"
                                                                : ""
                                                }`}
                                            >
                                                {runStatus}
                                            </Badge>
                                        </div>
                                    </div>
                                </Card>
                            </div>
                        </TabsContent>
                    </Tabs>
                </div>

                {/* Right Column: Timeline - only show when there are events or a task is running, and toggle is on */}
                {showTimeline && (timelineEvents.length > 0 || runStatus === "running") && (
                    <div className="w-1/3 border-l bg-muted/10 flex flex-col hidden md:flex">
                        {/* Bridge keeps radar store in sync with Redux events */}
                        <RadarBridge />
                        
                        <div className="p-4 flex items-center justify-between gap-2 shrink-0">
                            <div className="font-medium text-sm text-muted-foreground uppercase tracking-wider">
                                Execution Timeline
                            </div>
                            {isReconnecting && (
                                <span className="text-xs text-muted-foreground">Reconnecting...</span>
                            )}
                        </div>
                        
                        {/* Radar visualization */}
                        <div className="shrink-0">
                            <div className="w-full h-[160px] overflow-hidden">
                                <RadarCanvas />
                            </div>
                        </div>
                        
                        <div className="flex-1 min-h-0">
                            <ScrollArea className="h-full" ref={timelineScrollRef}>
                                {timelineEvents.length > 0 ? (
                                    <RunTimeline events={timelineEvents as any} />
                                ) : (
                                    <div className="p-4 text-sm text-muted-foreground text-center">
                                        Starting...
                                    </div>
                                )}
                            </ScrollArea>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}

export default function RunDetailPage() {
    return (
        <Suspense fallback={<div>Loading...</div>}>
            <RunDetailContent />
        </Suspense>
    );
}
