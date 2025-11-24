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
import { ChatInput, AgentSelection } from "@/components/chat-input";
import { ArrowLeft, Download, Share, Loader2, Eye, EyeOff } from "lucide-react";
import Link from "next/link";
import { Suspense, useEffect, useState, useRef, useCallback, useMemo } from "react";
import { useRunStream } from "@/lib/shannon/stream";
import { useSelector, useDispatch } from "react-redux";
import { RootState } from "@/lib/store";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { getSessionEvents, getSessionHistory, getTask, getSession, Turn, Event } from "@/lib/shannon/api";
import { resetRun, addMessage, addEvent, updateMessageMetadata, setStreamError, setSelectedAgent, setResearchStrategy, setMainWorkflowId } from "@/lib/features/runSlice";

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
    const [streamRestartKey, setStreamRestartKey] = useState(0);
    const [activeTab, setActiveTab] = useState("conversation");
    const [showAgentTrace, setShowAgentTrace] = useState(false);
    const dispatch = useDispatch();
    const router = useRouter();
    
    // Refs for auto-scrolling
    const timelineScrollRef = useRef<HTMLDivElement>(null);
    const conversationScrollRef = useRef<HTMLDivElement>(null);
    
    // Refs for tracking history/message loading state
    const hasLoadedMessagesRef = useRef(false);
    const hasFetchedHistoryRef = useRef(false);
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
        // Reset on initial mount or when session changes
        const sessionChanged = sessionId !== prevSessionIdRef.current;
        // Don't reset when transitioning from "new" to a real session ID (task creation flow)
        const isNewToReal = prevSessionIdRef.current === "new" && sessionId && sessionId !== "new";
        const shouldReset = !hasInitializedRef.current || (sessionChanged && prevSessionIdRef.current !== null && !isNewToReal);
        
        if (shouldReset) {
            dispatch(resetRun());
            prevSessionIdRef.current = sessionId;
            hasInitializedRef.current = true;
            // Reset the loaded messages flag when session changes
            hasLoadedMessagesRef.current = false;
            hasFetchedHistoryRef.current = false;
            hasInitializedTaskRef.current = null;
            setCurrentTaskId(null);
        } else if (sessionChanged) {
            // Update session ref even if not resetting (for "new" -> real ID transition)
            prevSessionIdRef.current = sessionId;
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
                    const isDeepResearch = taskContext.force_research === true;
                    const strategy = taskContext.research_strategy || "quick";
                    
                    console.log("[RunDetail] Task context - Agent type:", isDeepResearch ? "deep_research" : "normal", "Strategy:", strategy);
                    console.log("[RunDetail] Task context details:", taskContext);
                    
                    dispatch(setSelectedAgent(isDeepResearch ? "deep_research" : "normal"));
                    if (isDeepResearch) {
                        dispatch(setResearchStrategy(strategy as "quick" | "standard" | "deep" | "academic"));
                    }
                }

                // Add the user message immediately
                dispatch(addMessage({
                    id: `user-query-${workflowId}`,
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

                // If the task has a session ID, update the URL
                if (task.session_id && (!sessionId || sessionId === "new")) {
                    const newParams = new URLSearchParams(searchParams.toString());
                    newParams.set("session_id", task.session_id);
                    router.replace(`/run-detail?${newParams.toString()}`);
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
        
        // Don't fetch history if we're currently streaming a new task unless forced
        if (!forceReload && runStatus === "running" && currentTaskId) {
            console.log("[RunDetail] Skipping history fetch while task is running");
            return;
        }

        setIsLoading(true);
        setError(null);
        try {
            // Fetch session details to get context (agent type and research strategy)
            let sessionContext: Record<string, any> = {};
            try {
                const sessionDetails = await getSession(sessionId);
                console.log("[RunDetail] Session details:", sessionDetails);
                console.log("[RunDetail] Session context:", sessionDetails.context);
                sessionContext = sessionDetails.context || {};
            } catch (err) {
                console.error("[RunDetail] Failed to fetch session details:", err);
            }
            
            // Extract agent type and research strategy from session context
            let isDeepResearch = sessionContext.force_research === true;
            let strategy = sessionContext.research_strategy || "quick";
            
            console.log("[RunDetail] Session context parsed - force_research:", sessionContext.force_research, "research_strategy:", sessionContext.research_strategy);
            console.log("[RunDetail] Initial detection - Agent type:", isDeepResearch ? "deep_research" : "normal", "Strategy:", strategy);

            // Fetch events to build the timeline and conversation
            // Backend validation limits to 100 turns per request
            const eventsData = await getSessionEvents(sessionId, 100, 0);
            
            // If session context is empty or doesn't have agent info, check the first task's context
            if (!isDeepResearch && eventsData.turns.length > 0) {
                const firstTaskId = eventsData.turns[0].task_id;
                const firstWorkflowId = eventsData.turns[0].events.length > 0 ? eventsData.turns[0].events[0].workflow_id : firstTaskId;
                console.log("[RunDetail] Session context empty, checking first task context. TaskId:", firstTaskId, "WorkflowId:", firstWorkflowId);
                
                try {
                    const firstTask = await getTask(firstWorkflowId);
                    console.log("[RunDetail] First task details:", firstTask);
                    console.log("[RunDetail] First task context:", firstTask.context);
                    console.log("[RunDetail] First task metadata:", firstTask.metadata);
                    
                    // Context may be in task.context or metadata.task_context
                    let taskContext = firstTask.context;
                    if (!taskContext && firstTask.metadata?.task_context) {
                        taskContext = firstTask.metadata.task_context;
                        console.log("[RunDetail] Found context in metadata.task_context:", taskContext);
                    }
                    
                    if (taskContext && taskContext.force_research === true) {
                        isDeepResearch = true;
                        strategy = taskContext.research_strategy || "quick";
                        console.log("[RunDetail] Found deep research in first task - Strategy:", strategy);
                    }
                } catch (err) {
                    console.warn("[RunDetail] Failed to fetch first task details:", err);
                }
            }
            
            // Update Redux state with final agent selection and research strategy
            console.log("[RunDetail] Final detection - Agent type:", isDeepResearch ? "deep_research" : "normal", "Strategy:", strategy);
            dispatch(setSelectedAgent(isDeepResearch ? "deep_research" : "normal"));
            if (isDeepResearch) {
                dispatch(setResearchStrategy(strategy as "quick" | "standard" | "deep" | "academic"));
            }
            console.log("[RunDetail] Redux state updated");

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
                            timestamp: new Date().toISOString()
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
                
                // Add deduplicated events to Redux
                deduplicatedHistoricalEvents.forEach((event: Event) => {
                    dispatch(addEvent(event as any));
                });

                // Check if the last task is running before loading messages
                if (eventsData.turns.length > 0) {
                    const lastTurn = eventsData.turns[eventsData.turns.length - 1];
                    const lastWorkflowId = lastTurn.events.length > 0 ? lastTurn.events[0].workflow_id : lastTurn.task_id;
                    
                    try {
                        const taskStatus = await getTask(lastWorkflowId);
                        if ((taskStatus.status === "TASK_STATUS_RUNNING" || taskStatus.status === "TASK_STATUS_QUEUED") && !taskStatus.result) {
                            lastRunningWorkflowId = lastWorkflowId;
                            console.log("[RunDetail] Last task is running:", lastWorkflowId, "- will show generating indicator");
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
                
                console.log("[RunDetail] Loading", eventsData.turns.length, "turns into messages");
                eventsData.turns.forEach((turn, turnIndex) => {
                    const workflowId = turn.events.length > 0 ? turn.events[0].workflow_id : turn.task_id;
                    console.log(`[RunDetail] Processing turn ${turnIndex + 1}/${eventsData.turns.length}, task_id: ${turn.task_id}, workflow_id: ${workflowId}`);
                    
                    const isCurrentlyRunning = workflowId === lastRunningWorkflowId;
                    
                    // User message
                    dispatch(addMessage({
                        id: `user-${turn.task_id}`,
                        role: "user",
                        content: turn.user_query,
                        timestamp: new Date(turn.timestamp).toLocaleTimeString(),
                        taskId: turn.task_id,
                    }));
                    console.log(`[RunDetail] Added user message for turn ${turnIndex + 1}`);

                    // For running tasks, show generating indicator instead of intermediate messages
                    if (isCurrentlyRunning) {
                        console.log("[RunDetail] Task is running - adding generating placeholder instead of intermediate messages");
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
                    if (turn.final_output) {
                        // Get full task details (includes citations and metadata)
                        const fullTaskDetails = taskDetailsMap.get(workflowId);
                        const metadata = fullTaskDetails?.metadata || turn.metadata;
                        
                        if (metadata?.citations) {
                            console.log(`[RunDetail] Loaded ${metadata.citations.length} citations for turn ${turn.task_id}`);
                        }
                        
                        console.log(`[RunDetail] Adding assistant message from final_output (authoritative result)`);
                        dispatch(addMessage({
                            id: `assistant-${turn.task_id}`,
                            role: "assistant",
                            content: turn.final_output,
                            timestamp: new Date(turn.timestamp).toLocaleTimeString(),
                            metadata: metadata,
                            taskId: turn.task_id,
                        }));
                    } else {
                        console.warn(`[RunDetail] Turn ${turnIndex + 1} has no final_output!`);
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

    // Only fetch session history on initial load, not during active tasks
    useEffect(() => {
        // Only fetch history once when the session first loads
        if (sessionId && sessionId !== "new" && !hasFetchedHistoryRef.current && !currentTaskId) {
            fetchSessionHistory();
            hasFetchedHistoryRef.current = true;
        }
    }, [sessionId, fetchSessionHistory, currentTaskId]);
    
    // Refetch session history when a task completes to update the summary
    // Title updates via streaming title_generator events
    useEffect(() => {
        if (runStatus === "completed" && sessionId && sessionId !== "new") {
            // Update both events (for timeline) and history (for costs in summary)
            // Don't trigger message reload - messages are already in Redux from streaming
            const timer = setTimeout(async () => {
                try {
                    const eventsData = await getSessionEvents(sessionId, 100, 0);
                    const allEvents: Event[] = (eventsData as any).events || eventsData.turns.flatMap(t => t.events || []);
                    setSessionData({ turns: eventsData.turns, events: allEvents });
                    
                    // Fetch history for cost data only (don't reload messages)
                    const historyData = await getSessionHistory(sessionId);
                    setSessionHistory(historyData);
                    
                    // Mark that we've loaded messages to prevent fetchSessionHistory from reloading them
                    if (!hasLoadedMessagesRef.current) {
                        hasLoadedMessagesRef.current = true;
                    }
                } catch (err) {
                    console.error("Failed to refresh session data:", err);
                }
            }, 1000);
            return () => clearTimeout(timer);
        }
    }, [runStatus, sessionId]);

    const handleTaskCreated = async (newTaskId: string, query: string, workflowId?: string) => {
        const activeWorkflowId = workflowId || newTaskId;
        console.log("New task created:", newTaskId, "workflow:", activeWorkflowId);
        setCurrentTaskId(activeWorkflowId);
        
        // Set this as the main workflow ID in Redux
        dispatch(setMainWorkflowId(activeWorkflowId));

        // Add user query to messages immediately
        dispatch(addMessage({
            id: `user-query-${activeWorkflowId}`,
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

        // If this was a new session, fetch the task to obtain the session ID and update the URL
        if (isNewSession) {
            try {
                const taskDetails = await getTask(activeWorkflowId);
                if (taskDetails.session_id) {
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
            
            if (!task.result) {
                console.warn("[RunDetail] ⚠️ Task has no result field - task may still be running:", task.status);
                return;
            }
            
            // Check if we already have the authoritative result (exact match)
            const hasAuthoritativeResult = runMessages.some(m => 
                m.role === "assistant" && 
                m.taskId === currentTaskId && 
                m.content === task.result &&
                !m.isStreaming
            );
            
            if (hasAuthoritativeResult) {
                console.log("[RunDetail] ✓ Authoritative result already present in messages");
                return;
            }
            
            // Add the authoritative result from task.result (canonical value)
            // This ensures the final answer comes from the task record, not intermediate stream messages
            console.log("[RunDetail] ➕ Adding authoritative result to messages (length:", task.result.length, "chars)");
            const messageId = `assistant-final-${currentTaskId}`;
            console.log("[RunDetail] Message ID:", messageId);
            
            dispatch(addMessage({
                id: messageId,
                role: "assistant",
                content: task.result,
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
        if (eventType === "ERROR_OCCURRED" || eventType === "error") return "failed";
        if (eventType.includes("STARTED") || eventType === "AGENT_THINKING" ||
            eventType === "WAITING" || eventType === "APPROVAL_REQUESTED") return "running";
        if (eventType.includes("COMPLETED") || eventType === "thread.message.completed" ||
            eventType.includes("OBSERVATION") || eventType === "APPROVAL_DECISION" ||
            eventType === "DEPENDENCY_SATISFIED" || eventType === "done") return "completed";
        return "completed";
    };

    // Helper to create friendly title from event type
    const getFriendlyTitle = (event: any): string => {
        if (event.message) return event.message;
        const typeMap: Record<string, string> = {
            "WORKFLOW_STARTED": "Workflow Started",
            "WORKFLOW_COMPLETED": "Workflow Completed",
            "AGENT_STARTED": "Agent Started",
            "AGENT_COMPLETED": "Agent Completed",
            "AGENT_THINKING": "Agent Planning",
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
            "thread.message.completed": "LLM Response",
            "done": "Task Done",
            "STREAM_END": "Stream Ended",
        };
        return typeMap[event.type] || event.type;
    };

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

    const timelineEvents = deduplicatedEvents
        .map((event: any, index) => {
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
            
            return {
                id: uniqueId,
                type: categorizeEvent(event.type),
                status: eventStatus,
                title: getFriendlyTitle(event),
                timestamp: event.timestamp ? new Date(event.timestamp).toLocaleTimeString() : "",
                details: event.payload ? JSON.stringify(event.payload, null, 2) : undefined
            };
        });

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

    // Auto-scroll timeline to bottom when events change
    useEffect(() => {
        if (timelineScrollRef.current) {
            const scrollContainer = timelineScrollRef.current.querySelector('[data-slot="scroll-area-viewport"]');
            if (scrollContainer) {
                // Use requestAnimationFrame to avoid blocking render
                requestAnimationFrame(() => {
                    scrollContainer.scrollTop = scrollContainer.scrollHeight;
                });
            }
        }
    }, [timelineEvents]);

    // Auto-scroll conversation to bottom when messages change (including content updates during streaming)
    useEffect(() => {
        if (conversationScrollRef.current && activeTab === "conversation") {
            const scrollContainer = conversationScrollRef.current.querySelector('[data-slot="scroll-area-viewport"]');
            if (scrollContainer) {
                // Use requestAnimationFrame to avoid blocking render
                requestAnimationFrame(() => {
                    scrollContainer.scrollTop = scrollContainer.scrollHeight;
                });
            }
        }
    }, [messages, activeTab]);

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

    const agentLabelMap: Record<AgentSelection, string> = {
        normal: "Everyday Agent",
        deep_research: "Deep Research",
    };

    return (
        <div className="flex h-full flex-col overflow-hidden">
            {/* Header */}
            <header className="flex h-14 items-center justify-between border-b px-6 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60 shrink-0">
                <div className="flex items-center gap-4">
                    <Button variant="ghost" size="icon" asChild>
                        <Link href="/runs">
                            <ArrowLeft className="h-4 w-4" />
                        </Link>
                    </Button>
                    <div className="flex items-center gap-2 min-w-0 flex-1 max-w-2xl">
                        <h1 className="text-lg font-semibold truncate" title={sessionTitle || (isNewSession ? "New Session" : `Session ${sessionId?.slice(0, 8)}...`)}>
                            {sessionTitle || (isNewSession ? "New Session" : `Session ${sessionId?.slice(0, 8)}...`)}
                        </h1>
                        <Badge variant="outline" className="bg-blue-50 text-blue-700 border-blue-200 shrink-0">
                            {agentLabelMap[selectedAgent] || "Normal chat"}
                        </Badge>
                        <Badge variant="secondary" className="text-xs shrink-0">
                            {isNewSession ? "New" : "Active"}
                        </Badge>
                    </div>
                </div>
                <div className="flex items-center gap-3">
                    <div className="flex items-center gap-2">
                        <span className="text-xs text-muted-foreground">Agent</span>
                        <Select
                            value={selectedAgent}
                            onValueChange={(val) => dispatch(setSelectedAgent(val as AgentSelection))}
                        >
                            <SelectTrigger className="h-9 w-44 text-sm">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                <SelectItem value="normal">Everyday Agent</SelectItem>
                                <SelectItem value="deep_research">Deep Research</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                    <Button variant="outline" size="sm">
                        <Share className="mr-2 h-4 w-4" />
                        Share
                    </Button>
                    <Button variant="outline" size="sm">
                        <Download className="mr-2 h-4 w-4" />
                        Export
                    </Button>
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
                {/* Left Column: Timeline */}
                <div className="w-1/3 border-r bg-muted/10 flex flex-col">
                    <div className="p-4 flex items-center justify-between gap-2 shrink-0">
                        <div className="font-medium text-sm text-muted-foreground uppercase tracking-wider">
                            Execution Timeline
                        </div>
                        {isReconnecting && (
                            <span className="text-xs text-muted-foreground">Reconnecting...</span>
                        )}
                    </div>
                    <div className="flex-1 min-h-0">
                        <ScrollArea className="h-full" ref={timelineScrollRef}>
                            {timelineEvents.length > 0 ? (
                                <RunTimeline events={timelineEvents as any} />
                            ) : (
                                <div className="p-4 text-sm text-muted-foreground text-center">
                                    No events yet.
                                </div>
                            )}
                        </ScrollArea>
                    </div>
                </div>

                {/* Right Column: Tabs */}
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
                            {activeTab === "conversation" && (
                                <Button
                                    variant="outline"
                                    size="sm"
                                    onClick={() => setShowAgentTrace(!showAgentTrace)}
                                    className="gap-2"
                                >
                                    {showAgentTrace ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                                    {showAgentTrace ? "Hide Agent Trace" : "Show Agent Trace"}
                                </Button>
                            )}
                        </div>

                        <TabsContent value="conversation" className="flex-1 p-0 m-0 data-[state=active]:flex flex-col overflow-hidden">
                            <div className="flex-1 min-h-0">
                                <ScrollArea className="h-full" ref={conversationScrollRef}>
                                    {messages.length > 0 ? (
                                        <RunConversation messages={messages as any} showAgentTrace={showAgentTrace} />
                                    ) : (
                                        <div className="flex items-center justify-center h-full">
                                            <div className="text-center text-muted-foreground">
                                                <p className="text-lg mb-2">Start a conversation</p>
                                                <p className="text-sm">Messages will appear here</p>
                                            </div>
                                        </div>
                                    )}
                                </ScrollArea>
                            </div>

                            {/* Chat Input Box */}
                            <div className="border-t bg-background p-4 shrink-0">
                                <ChatInput
                                    sessionId={isNewSession ? undefined : sessionId ?? undefined}
                                    disabled={runStatus === "running"}
                                    isTaskComplete={runStatus !== "running"}
                                    selectedAgent={selectedAgent}
                                    initialResearchStrategy={researchStrategy}
                                    onTaskCreated={handleTaskCreated}
                                />
                            </div>
                        </TabsContent>

                        <TabsContent value="summary" className="flex-1 p-6 m-0 overflow-auto min-h-0">
                            <div className="max-w-4xl mx-auto space-y-4">
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
                                    <Card className="p-4">
                                        <h3 className="text-base font-semibold mb-3">Token Usage by Turn</h3>
                                        <div className="space-y-2">
                                            {sessionHistory.tasks.map((task: any, index: number) => (
                                                <div key={task.task_id} className="py-2 border-b last:border-b-0">
                                                    <div className="flex items-center justify-between">
                                                        <div className="flex-1 min-w-0">
                                                            <div className="text-xs font-medium truncate">Turn {index + 1}</div>
                                                            <div className="text-xs text-muted-foreground truncate">{task.query}</div>
                                                            {(task.model_used || task.metadata?.model) && (
                                                                <div className="text-xs text-muted-foreground mt-0.5">
                                                                    {task.model_used || task.metadata?.model}
                                                                    {(task.provider || task.metadata?.provider) && ` (${task.provider || task.metadata?.provider})`}
                                                                </div>
                                                            )}
                                                        </div>
                                                        <div className="flex items-center gap-3 sm:gap-4 ml-4">
                                                            <div className="text-right">
                                                                <div className="text-sm font-medium">{(task.total_tokens || 0).toLocaleString()}</div>
                                                                <div className="text-xs text-muted-foreground">tokens</div>
                                                            </div>
                                                            <div className="text-right">
                                                                <div className="text-sm font-medium">${(task.total_cost_usd || 0).toFixed(4)}</div>
                                                                <div className="text-xs text-muted-foreground">cost</div>
                                                            </div>
                                                            <div className="text-right">
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

                                {/* Models Used */}
                                {sessionHistory?.tasks && sessionHistory.tasks.length > 0 && (() => {
                                    const modelUsage = new Map<string, { count: number; tokens: number; cost: number }>();
                                    sessionHistory.tasks.forEach((task: any) => {
                                        const model = task.model_used || task.metadata?.model;
                                        const provider = task.provider || task.metadata?.provider;
                                        if (model) {
                                            const key = provider ? `${model} (${provider})` : model;
                                            const existing = modelUsage.get(key) || { count: 0, tokens: 0, cost: 0 };
                                            modelUsage.set(key, {
                                                count: existing.count + 1,
                                                tokens: existing.tokens + (task.total_tokens || 0),
                                                cost: existing.cost + (task.total_cost_usd || 0)
                                            });
                                        }
                                    });
                                    return modelUsage.size > 0 ? (
                                        <Card className="p-4">
                                            <h3 className="text-base font-semibold mb-3">Models Used</h3>
                                            <div className="space-y-2">
                                                {Array.from(modelUsage.entries()).map(([model, usage]) => (
                                                    <div key={model} className="flex items-center justify-between py-1.5 text-xs">
                                                        <div className="font-medium flex-1">{model}</div>
                                                        <div className="flex items-center gap-4 text-muted-foreground">
                                                            <span>{usage.count} {usage.count === 1 ? 'call' : 'calls'}</span>
                                                            <span>{usage.tokens.toLocaleString()} tokens</span>
                                                            <span>${usage.cost.toFixed(4)}</span>
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
                                        <Card className="p-4">
                                            <h3 className="text-base font-semibold mb-3">Agents Involved</h3>
                                            <div className="flex flex-wrap gap-2">
                                                {Array.from(allAgents).map(agent => (
                                                    <Badge key={agent} variant="secondary" className="text-xs">
                                                        {agent}
                                                    </Badge>
                                                ))}
                                            </div>
                                        </Card>
                                    ) : null;
                                })()}

                                {/* Average Metrics */}
                                {sessionHistory?.tasks && sessionHistory.tasks.length > 0 && (
                                    <Card className="p-4">
                                        <h3 className="text-base font-semibold mb-3">Average Metrics</h3>
                                        <div className="grid grid-cols-2 gap-4">
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
                                <Card className="p-4">
                                    <h3 className="text-base font-semibold mb-3">Session Information</h3>
                                    <div className="space-y-2 text-xs">
                                        <div className="flex justify-between items-center gap-2">
                                            <span className="text-muted-foreground">Session ID</span>
                                            <span className="font-mono text-xs truncate">{sessionId}</span>
                                        </div>
                                        {currentTaskId && (
                                            <div className="flex justify-between items-center gap-2">
                                                <span className="text-muted-foreground">Current Task ID</span>
                                                <span className="font-mono text-xs truncate">{currentTaskId}</span>
                                            </div>
                                        )}
                                        <div className="flex justify-between items-center">
                                            <span className="text-muted-foreground">Status</span>
                                            <Badge variant={runStatus === "completed" ? "default" : runStatus === "running" ? "secondary" : "outline"} className="text-xs">
                                                {runStatus}
                                            </Badge>
                                        </div>
                                    </div>
                                </Card>
                            </div>
                        </TabsContent>
                    </Tabs>
                </div>
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
