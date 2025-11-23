"use client";

import { useSearchParams, useRouter } from "next/navigation";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RunTimeline } from "@/components/run-timeline";
import { RunConversation } from "@/components/run-conversation";
import { ChatInput } from "@/components/chat-input";
import { ArrowLeft, Download, Share, Loader2 } from "lucide-react";
import Link from "next/link";
import { Suspense, useEffect, useState, useRef, useCallback } from "react";
import { useRunStream } from "@/lib/shannon/stream";
import { useSelector, useDispatch } from "react-redux";
import { RootState } from "@/lib/store";
import { getSessionEvents, getTask, Turn, Event } from "@/lib/shannon/api";
import { resetRun, addMessage, addEvent } from "@/lib/features/runSlice";

function RunDetailContent() {
    const searchParams = useSearchParams();
    const sessionId = searchParams.get("session_id");
    const taskIdParam = searchParams.get("id");
    const isNewSession = (sessionId === "new" || !sessionId) && !taskIdParam;

    const [isLoading, setIsLoading] = useState(!isNewSession);
    const [error, setError] = useState<string | null>(null);
    const [sessionData, setSessionData] = useState<{ turns: Turn[], events: Event[] } | null>(null);
    const [currentTaskId, setCurrentTaskId] = useState<string | null>(null);
    const dispatch = useDispatch();
    const router = useRouter();
    
    // Refs for auto-scrolling
    const timelineScrollRef = useRef<HTMLDivElement>(null);
    const conversationScrollRef = useRef<HTMLDivElement>(null);

    // Connect to SSE stream for the current task if one is running
    useRunStream(currentTaskId);

    // Get data from Redux (streaming state)
    const runEvents = useSelector((state: RootState) => state.run.events);
    const runMessages = useSelector((state: RootState) => state.run.messages);
    const runStatus = useSelector((state: RootState) => state.run.status);

    // Reset Redux state only when switching sessions or starting fresh
    const prevSessionIdRef = useRef<string | null>(null);
    const hasInitializedRef = useRef(false);
    
    useEffect(() => {
        // Reset on initial mount or when session changes
        const sessionChanged = sessionId !== prevSessionIdRef.current;
        const shouldReset = !hasInitializedRef.current || (sessionChanged && prevSessionIdRef.current !== null);
        
        if (shouldReset) {
            dispatch(resetRun());
            prevSessionIdRef.current = sessionId;
            hasInitializedRef.current = true;
            // Reset the loaded messages flag when session changes
            hasLoadedMessagesRef.current = false;
            hasInitializedTaskRef.current = null;
            setCurrentTaskId(null);
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
            // Fetch events to build the timeline
            // Backend validation limits to 100 turns per request
            const eventsData = await getSessionEvents(sessionId, 100, 0);

            // The API response might not have a top-level 'events' array if it's just returning turns with embedded events.
            // Let's collect all events from all turns if top-level events is missing.
            const allEvents: Event[] = (eventsData as any).events || eventsData.turns.flatMap(t => t.events || []);

            setSessionData({ turns: eventsData.turns, events: allEvents });

            // Only populate Redux messages if we haven't loaded them yet
            // This prevents duplicates when navigating with an active task
            if (!hasLoadedMessagesRef.current || forceReload) {
                if (forceReload) {
                    // Clear current state first on forced reload
                    dispatch(resetRun());
                }

                // Add historical events to Redux
                allEvents.forEach((event: Event) => {
                    dispatch(addEvent(event as any));
                });

                // Add historical messages (turns) to Redux
                // We need to reconstruct messages from turns
                eventsData.turns.forEach(turn => {
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
                        dispatch(addMessage({
                            id: `assistant-${turn.task_id}`,
                            role: "assistant",
                            content: turn.final_output,
                            timestamp: new Date(turn.timestamp).toLocaleTimeString(), // Approximate
                            metadata: turn.metadata,
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
    }, [sessionId]);
    
    // Refetch session history when a task completes to update the summary
    // But don't refetch immediately - wait for user to switch tabs or explicitly refresh
    useEffect(() => {
        if (runStatus === "completed" && sessionId && sessionId !== "new") {
            // Just update the sessionData for the summary, don't reset messages
            const timer = setTimeout(async () => {
                try {
                    const eventsData = await getSessionEvents(sessionId, 100, 0);
                    const allEvents: Event[] = (eventsData as any).events || eventsData.turns.flatMap(t => t.events || []);
                    setSessionData({ turns: eventsData.turns, events: allEvents });
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

        // If this was a new session, we need to update the URL
        if (isNewSession) {
            // We don't have the new session ID immediately available from submitTask response in all cases
            // But usually the backend creates a session. 
            // For now, let's assume we stay on "new" until we get a session ID, 
            // OR we should have gotten the session ID from the task creation if possible.
            // Since the current API doesn't easily give us the session ID on creation without fetching the task,
            // we might need to fetch the task details.

            // However, for the streaming to work, we just need the task ID.
            // The session ID update can happen later or we can redirect.
        }

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
    };

    const messageMatchesTask = (message: any, taskId: string | null) => {
        if (!taskId) return false;
        if (message.taskId && message.taskId === taskId) return true;
        return typeof message.id === "string" && message.id.includes(taskId);
    };

    // Watch for task completion and fetch final output if needed
    useEffect(() => {
        const fetchTaskResult = async () => {
            console.log("[RunDetail] Fallback fetch check - status:", runStatus, "taskId:", currentTaskId, "messages:", runMessages.length);
            
            if (runStatus === "completed" && currentTaskId) {
                console.log("[RunDetail] Task completed, checking if we need to fetch final output");
                
                // Check if we have an assistant message for this task
                const hasAssistantResponse = runMessages.some(m => 
                    m.role === "assistant" && !m.isStreaming && !m.isGenerating && messageMatchesTask(m, currentTaskId)
                );
                
                console.log("[RunDetail] Has assistant response:", hasAssistantResponse);
                console.log("[RunDetail] Messages:", runMessages.map(m => ({ role: m.role, isStreaming: m.isStreaming, contentPreview: m.content?.substring(0, 50) })));

                if (!hasAssistantResponse) {
                    console.log("[RunDetail] No assistant response found, fetching task result from API");
                    try {
                        const task = await getTask(currentTaskId);
                        console.log("[RunDetail] Task fetched:", task);
                        if (task.result) {
                            dispatch(addMessage({
                                id: `assistant-${currentTaskId}`,
                                role: "assistant",
                                content: task.result,
                                timestamp: new Date().toLocaleTimeString(),
                                metadata: task.metadata,
                                taskId: currentTaskId,
                            }));
                            console.log("[RunDetail] Added final output from task API:", task.result.substring(0, 100));
                        } else {
                            console.warn("[RunDetail] Task has no result field:", task);
                        }
                    } catch (err) {
                        console.error("[RunDetail] Failed to fetch task result:", err);
                    }
                } else {
                    console.log("[RunDetail] Assistant response already exists, skipping fallback fetch");
                }
            }
        };

        fetchTaskResult();
    }, [runStatus, currentTaskId, runMessages, dispatch]);

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
        "LLM_PROMPT",
    ]);

    const timelineEvents = runEvents
        .filter((event: any) => !excludedEventTypes.has(event.type) && event.type)
        .map((event: any, index) => ({
            id: event.stream_id || `event-${index}`,
            type: categorizeEvent(event.type),
            status: getEventStatus(event.type),
            title: getFriendlyTitle(event),
            timestamp: event.timestamp ? new Date(event.timestamp).toLocaleTimeString() : "",
            details: event.payload ? JSON.stringify(event.payload, null, 2) : undefined
        }));

    // Combine historical messages with streaming messages
    // Redux `runMessages` should now contain both if we populated it correctly
    const messages = runMessages;

    // Auto-scroll timeline to bottom when new events arrive
    useEffect(() => {
        if (timelineScrollRef.current) {
            const scrollContainer = timelineScrollRef.current.querySelector('[data-radix-scroll-area-viewport]');
            if (scrollContainer) {
                // Use requestAnimationFrame to avoid blocking render
                requestAnimationFrame(() => {
                    scrollContainer.scrollTop = scrollContainer.scrollHeight;
                });
            }
        }
    }, [timelineEvents.length]);

    // Auto-scroll conversation to bottom when new messages arrive
    useEffect(() => {
        if (conversationScrollRef.current) {
            const scrollContainer = conversationScrollRef.current.querySelector('[data-radix-scroll-area-viewport]');
            if (scrollContainer) {
                // Use requestAnimationFrame to avoid blocking render
                requestAnimationFrame(() => {
                    scrollContainer.scrollTop = scrollContainer.scrollHeight;
                });
            }
        }
    }, [messages.length]);

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
        <div className="flex h-full flex-col">
            {/* Header */}
            <header className="flex h-14 items-center justify-between border-b px-6 bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
                <div className="flex items-center gap-4">
                    <Button variant="ghost" size="icon" asChild>
                        <Link href="/runs">
                            <ArrowLeft className="h-4 w-4" />
                        </Link>
                    </Button>
                    <div className="flex items-center gap-2">
                        <h1 className="text-lg font-semibold">
                            {isNewSession ? "New Session" : `Session ${sessionId?.slice(0, 8)}...`}
                        </h1>
                        <Badge variant="outline" className="bg-blue-50 text-blue-700 border-blue-200">
                            {isNewSession ? "New" : "Active"}
                        </Badge>
                    </div>
                </div>
                <div className="flex items-center gap-2">
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

            {/* Main Content - Split View */}
            <div className="flex flex-1 overflow-hidden">
                {/* Left Column: Timeline */}
                <div className="w-1/3 border-r bg-muted/10">
                    <div className="p-4 font-medium text-sm text-muted-foreground uppercase tracking-wider">
                        Execution Timeline
                    </div>
                    <ScrollArea className="h-[calc(100vh-8rem)]" ref={timelineScrollRef}>
                        {timelineEvents.length > 0 ? (
                            <RunTimeline events={timelineEvents as any} />
                        ) : (
                            <div className="p-4 text-sm text-muted-foreground text-center">
                                No events yet.
                            </div>
                        )}
                    </ScrollArea>
                </div>

                {/* Right Column: Tabs */}
                <div className="flex-1 bg-background">
                    <Tabs defaultValue="conversation" className="h-full flex flex-col">
                        <div className="border-b px-4">
                            <TabsList className="h-12 w-full justify-start bg-transparent p-0">
                                <TabsTrigger value="conversation" className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:border-b-2 data-[state=active]:border-primary rounded-none h-full px-4">
                                    Conversation
                                </TabsTrigger>
                                <TabsTrigger value="summary" className="data-[state=active]:bg-transparent data-[state=active]:shadow-none data-[state=active]:border-b-2 data-[state=active]:border-primary rounded-none h-full px-4">
                                    Summary
                                </TabsTrigger>
                            </TabsList>
                        </div>

                        <TabsContent value="conversation" className="flex-1 p-0 m-0 data-[state=active]:flex flex-col overflow-hidden">
                            <ScrollArea className="h-[calc(100vh-16rem)]" ref={conversationScrollRef}>
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

                            {/* Chat Input Box */}
                            <div className="border-t bg-background p-4">
                                <ChatInput
                                    sessionId={isNewSession ? undefined : sessionId}
                                    disabled={runStatus === "running"}
                                    isTaskComplete={runStatus !== "running"}
                                    onTaskCreated={handleTaskCreated}
                                />
                            </div>
                        </TabsContent>

                        <TabsContent value="summary" className="flex-1 p-6 m-0 overflow-auto">
                            <div className="max-w-4xl mx-auto space-y-4">
                                <div>
                                    <h2 className="text-xl font-bold">Session Summary</h2>
                                    <p className="text-sm text-muted-foreground">Overview of your conversation and resource usage</p>
                                </div>

                                {/* Key Metrics */}
                                <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Turns</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">{sessionData?.turns.length || 0}</div>
                                    </Card>
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Events</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">{runEvents.length || 0}</div>
                                    </Card>
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Tokens</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">
                                            {(sessionData?.turns.reduce((sum, turn) => sum + (turn.metadata?.tokens_used || 0), 0) || 0).toLocaleString()}
                                        </div>
                                    </Card>
                                    <Card className="p-3">
                                        <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Total Time</div>
                                        <div className="text-xl sm:text-2xl font-bold mt-1">
                                            {((sessionData?.turns.reduce((sum, turn) => sum + (turn.metadata?.execution_time_ms || 0), 0) || 0) / 1000).toFixed(1)}s
                                        </div>
                                    </Card>
                                </div>

                                {/* Token Usage Details */}
                                {sessionData?.turns && sessionData.turns.length > 0 && (
                                    <Card className="p-4">
                                        <h3 className="text-base font-semibold mb-3">Token Usage by Turn</h3>
                                        <div className="space-y-2">
                                            {sessionData.turns.map((turn, index) => (
                                                <div key={turn.task_id} className="flex items-center justify-between py-2 border-b last:border-b-0">
                                                    <div className="flex-1 min-w-0">
                                                        <div className="text-xs font-medium truncate">Turn {index + 1}</div>
                                                        <div className="text-xs text-muted-foreground truncate">{turn.user_query}</div>
                                                    </div>
                                                    <div className="flex items-center gap-3 sm:gap-4 ml-4">
                                                        <div className="text-right">
                                                            <div className="text-sm font-medium">{(turn.metadata?.tokens_used || 0).toLocaleString()}</div>
                                                            <div className="text-xs text-muted-foreground">tokens</div>
                                                        </div>
                                                        <div className="text-right">
                                                            <div className="text-sm font-medium">{((turn.metadata?.execution_time_ms || 0) / 1000).toFixed(1)}s</div>
                                                            <div className="text-xs text-muted-foreground">time</div>
                                                        </div>
                                                    </div>
                                                </div>
                                            ))}
                                        </div>
                                    </Card>
                                )}

                                {/* Agents Involved */}
                                {sessionData?.turns && sessionData.turns.length > 0 && (() => {
                                    const allAgents = new Set<string>();
                                    sessionData.turns.forEach(turn => {
                                        turn.metadata?.agents_involved?.forEach((agent: string) => allAgents.add(agent));
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
                                {sessionData?.turns && sessionData.turns.length > 0 && (
                                    <Card className="p-4">
                                        <h3 className="text-base font-semibold mb-3">Average Metrics</h3>
                                        <div className="grid grid-cols-2 gap-4">
                                            <div>
                                                <div className="text-xs text-muted-foreground">Avg. Tokens per Turn</div>
                                                <div className="text-lg font-bold mt-1">
                                                    {Math.round(
                                                        sessionData.turns.reduce((sum, turn) => sum + (turn.metadata?.tokens_used || 0), 0) / sessionData.turns.length
                                                    ).toLocaleString()}
                                                </div>
                                            </div>
                                            <div>
                                                <div className="text-xs text-muted-foreground">Avg. Time per Turn</div>
                                                <div className="text-lg font-bold mt-1">
                                                    {(
                                                        sessionData.turns.reduce((sum, turn) => sum + (turn.metadata?.execution_time_ms || 0), 0) / 
                                                        sessionData.turns.length / 1000
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
