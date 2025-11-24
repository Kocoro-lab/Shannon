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
import { ArrowLeft, Download, Share, Loader2 } from "lucide-react";
import Link from "next/link";
import { Suspense, useEffect, useState, useRef, useCallback } from "react";
import { useRunStream } from "@/lib/shannon/stream";
import { useSelector, useDispatch } from "react-redux";
import { RootState } from "@/lib/store";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { getSessionEvents, getSessionHistory, getTask, Turn, Event } from "@/lib/shannon/api";
import { resetRun, addMessage, addEvent, updateMessageMetadata, setStreamError, setSelectedAgent, setMainWorkflowId } from "@/lib/features/runSlice";

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
    const dispatch = useDispatch();
    const router = useRouter();
    
    // Refs for auto-scrolling
    const timelineScrollRef = useRef<HTMLDivElement>(null);
    const conversationScrollRef = useRef<HTMLDivElement>(null);

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
    const isReconnecting = connectionState === "reconnecting" || connectionState === "connecting";

    const handleRetryStream = () => {
        dispatch(setStreamError(null));
        setStreamRestartKey(key => key + 1);
    };

    // Reset Redux state only when switching sessions or starting fresh
    const prevSessionIdRef = useRef<string | null>(null);
    const hasInitializedRef = useRef(false);
    
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
            hasInitializedTaskRef.current = null;
            setCurrentTaskId(null);
        } else if (sessionChanged) {
            // Update session ref even if not resetting (for "new" -> real ID transition)
            prevSessionIdRef.current = sessionId;
        }
    }, [dispatch, sessionId]);

    // Handle direct task access (e.g. from New Task dialog)
    const hasInitializedTaskRef = useRef<string | null>(null);
    
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

    // Track if we've already loaded messages for this session
    const hasLoadedMessagesRef = useRef(false);
    
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
            // Fetch events to build the timeline and conversation
            // Backend validation limits to 100 turns per request
            const eventsData = await getSessionEvents(sessionId, 100, 0);

            // The API response might not have a top-level 'events' array if it's just returning turns with embedded events.
            // Let's collect all events from all turns if top-level events is missing.
            const allEvents: Event[] = (eventsData as any).events || eventsData.turns.flatMap(t => t.events || []);

            setSessionData({ turns: eventsData.turns, events: allEvents });

            // Fetch history to get cost data for Summary tab
            const historyData = await getSessionHistory(sessionId);
            setSessionHistory(historyData);

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

                // Add historical events to Redux (filter out title_generator and LLM_OUTPUT messages)
                // LLM_OUTPUT creates duplicate messages - we'll use turn.final_output instead
                allEvents
                    .filter((event: Event) => 
                        (event as any).agent_id !== 'title_generator' && 
                        (event as any).type !== 'LLM_OUTPUT'
                    )
                    .forEach((event: Event) => {
                        dispatch(addEvent(event as any));
                    });

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
                
                eventsData.turns.forEach(turn => {
                    const workflowId = turn.events.length > 0 ? turn.events[0].workflow_id : turn.task_id;
                    
                    // User message
                    dispatch(addMessage({
                        id: `user-${turn.task_id}`,
                        role: "user",
                        content: turn.user_query,
                        timestamp: new Date(turn.timestamp).toLocaleTimeString(),
                        taskId: turn.task_id,
                    }));

                    // Assistant message (final output)
                    if (turn.final_output) {
                        // Get full task details (includes citations)
                        const fullTaskDetails = taskDetailsMap.get(workflowId);
                        const metadata = fullTaskDetails?.metadata || turn.metadata;
                        
                        if (metadata?.citations) {
                            console.log(`[RunDetail] Loaded ${metadata.citations.length} citations for turn ${turn.task_id} (workflow: ${workflowId})`);
                        } else {
                            console.log(`[RunDetail] No citations found for turn ${turn.task_id} (workflow: ${workflowId})`);
                        }
                        
                        dispatch(addMessage({
                            id: `assistant-${turn.task_id}`,
                            role: "assistant",
                            content: turn.final_output,
                            timestamp: new Date(turn.timestamp).toLocaleTimeString(), // Approximate
                            metadata: metadata,
                            taskId: turn.task_id,
                        }));
                    }
                });
                
                hasLoadedMessagesRef.current = true;
            } else {
                console.log("[RunDetail] Skipping message population - already loaded messages for this session");
            }

        } catch (err) {
            setError(err instanceof Error ? err.message : "Failed to load session");
        } finally {
            setIsLoading(false);
        }
    }, [isNewSession, sessionId, runStatus, currentTaskId, dispatch]);

    // Only fetch session history on initial load, not during active tasks
    const hasFetchedHistoryRef = useRef(false);
    
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
            const timer = setTimeout(async () => {
                try {
                    const eventsData = await getSessionEvents(sessionId, 100, 0);
                    const allEvents: Event[] = (eventsData as any).events || eventsData.turns.flatMap(t => t.events || []);
                    setSessionData({ turns: eventsData.turns, events: allEvents });
                    
                    // Fetch history for cost data
                    const historyData = await getSessionHistory(sessionId);
                    setSessionHistory(historyData);
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
        if (!currentTaskId) return;
        console.log("[RunDetail] Fetching final output for task:", currentTaskId);
        try {
            const task = await getTask(currentTaskId);
            console.log("[RunDetail] Task metadata:", task.metadata);
            if (task.metadata?.citations) {
                console.log("[RunDetail] Citations found in task metadata:", task.metadata.citations.length);
            }
            
            // Check if we already have a streaming message for this task
            const hasExistingMessage = runMessages.some(m => 
                m.role === "assistant" && m.taskId === currentTaskId
            );
            
            if (hasExistingMessage && task.metadata) {
                // Update existing message with metadata (including citations)
                console.log("[RunDetail] Updating existing message with metadata");
                dispatch(updateMessageMetadata({
                    taskId: currentTaskId,
                    metadata: task.metadata,
                }));
            } else if (task.result) {
                // No existing message, create new one
                console.log("[RunDetail] Creating new message with result");
                dispatch(addMessage({
                    id: `assistant-${currentTaskId}`,
                    role: "assistant",
                    content: task.result,
                    timestamp: new Date().toLocaleTimeString(),
                    metadata: task.metadata,
                    taskId: currentTaskId,
                }));
            } else {
                console.warn("[RunDetail] Task has no result field:", task);
            }
            
            dispatch(setStreamError(null));
        } catch (err) {
            console.error("[RunDetail] Failed to fetch task result:", err);
            dispatch(setStreamError("Failed to fetch final output"));
        }
    }, [currentTaskId, dispatch, runMessages]);

    const handleFetchFinalOutputClick = () => {
        fetchFinalOutput();
    };

    // Watch for task completion and fetch final output if needed
    useEffect(() => {
        const fetchTaskResult = async () => {
            console.log("[RunDetail] Fallback fetch check - status:", runStatus, "taskId:", currentTaskId, "messages:", runMessages.length);
            
            if (runStatus === "completed" && currentTaskId) {
                console.log("[RunDetail] Task completed, checking if we need to fetch final output");
                
                // Check if we have an assistant message for this task (including streaming ones to avoid duplicate fetches)
                const hasAssistantResponse = runMessages.some(m => 
                    m.role === "assistant" && !m.isGenerating && messageMatchesTask(m, currentTaskId)
                );
                
                console.log("[RunDetail] Has assistant response:", hasAssistantResponse);
                console.log("[RunDetail] Messages:", runMessages.map(m => ({ role: m.role, isStreaming: m.isStreaming, contentPreview: m.content?.substring(0, 50) })));

                if (!hasAssistantResponse) {
                    console.log("[RunDetail] No assistant response found, fetching task result from API");
                    await fetchFinalOutput();
                } else {
                    console.log("[RunDetail] Assistant response already exists, skipping fallback fetch");
                }
            }
        };

        fetchTaskResult();
    }, [runStatus, currentTaskId, runMessages, dispatch, fetchFinalOutput]);

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

    // Track which workflows have completed by looking for WORKFLOW_COMPLETED events
    const completedWorkflows = new Set(
        runEvents
            .filter((e: any) => e.type === "WORKFLOW_COMPLETED")
            .map((e: any) => e.workflow_id)
    );

    const timelineEvents = filteredRunEvents
        .map((event: any, index) => {
            // Check if this event's workflow has completed
            const workflowCompleted = completedWorkflows.has(event.workflow_id);
            
            // If this workflow is completed, mark all its events as completed (static)
            // Otherwise, use the event's natural status (may be running/breathing)
            const eventStatus = workflowCompleted 
                ? "completed" 
                : getEventStatus(event.type);
            
            return {
                id: event.stream_id || `event-${index}`,
                type: categorizeEvent(event.type),
                status: eventStatus,
                title: getFriendlyTitle(event),
                timestamp: event.timestamp ? new Date(event.timestamp).toLocaleTimeString() : "",
                details: event.payload ? JSON.stringify(event.payload, null, 2) : undefined
            };
        });

    // Combine historical messages with streaming messages
    // Redux `runMessages` should now contain both if we populated it correctly
    const messages = runMessages;

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
        normal: "Normal task",
        deep_research: "Deep research",
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
                    <div className="flex items-center gap-2">
                        <h1 className="text-lg font-semibold">
                            {sessionTitle || (isNewSession ? "New Session" : `Session ${sessionId?.slice(0, 8)}...`)}
                        </h1>
                        <Badge variant="outline" className="bg-blue-50 text-blue-700 border-blue-200">
                            {isNewSession ? "New" : "Active"}
                        </Badge>
                        <Badge variant="secondary" className="text-xs">
                            {agentLabelMap[selectedAgent] || "Normal chat"}
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
                                <SelectItem value="normal">Normal task</SelectItem>
                                <SelectItem value="deep_research">Deep research</SelectItem>
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
                        <div className="px-4 pt-4 shrink-0">
                            <TabsList>
                                <TabsTrigger value="conversation">
                                    Conversation
                                </TabsTrigger>
                                <TabsTrigger value="summary">
                                    Summary
                                </TabsTrigger>
                            </TabsList>
                        </div>

                        <TabsContent value="conversation" className="flex-1 p-0 m-0 data-[state=active]:flex flex-col overflow-hidden">
                            <div className="flex-1 min-h-0">
                                <ScrollArea className="h-full" ref={conversationScrollRef}>
                                    {messages.length > 0 ? (
                                        <RunConversation messages={messages as any} />
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
                                            {((sessionHistory?.tasks.reduce((sum: number, task: any) => sum + (task.duration_ms || 0), 0) || sessionData?.turns.reduce((sum, turn) => sum + (turn.metadata?.execution_time_ms || 0), 0) || 0) / 1000).toFixed(1)}s
                                        </div>
                                    </Card>
                                </div>

                                {/* Token Usage Details */}
                                {sessionHistory?.tasks && sessionHistory.tasks.length > 0 && (
                                    <Card className="p-4">
                                        <h3 className="text-base font-semibold mb-3">Token Usage by Turn</h3>
                                        <div className="space-y-2">
                                            {sessionHistory.tasks.map((task: any, index: number) => (
                                                <div key={task.task_id} className="flex items-center justify-between py-2 border-b last:border-b-0">
                                                    <div className="flex-1 min-w-0">
                                                        <div className="text-xs font-medium truncate">Turn {index + 1}</div>
                                                        <div className="text-xs text-muted-foreground truncate">{task.query}</div>
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
                                                            <div className="text-sm font-medium">{((task.duration_ms || 0) / 1000).toFixed(1)}s</div>
                                                            <div className="text-xs text-muted-foreground">time</div>
                                                        </div>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    </Card>
                                )}

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
                                                    {(
                                                        sessionHistory.tasks.reduce((sum: number, task: any) => sum + (task.duration_ms || 0), 0) / 
                                                        sessionHistory.tasks.length / 1000
                                                    ).toFixed(1)}s
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
