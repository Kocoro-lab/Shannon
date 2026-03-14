/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { createSlice, PayloadAction } from "@reduxjs/toolkit";
import { ShannonEvent } from "../shannon/types";
import { isStatusMessage, isDiagnosticMessage, capProcessedDeltas } from "../utils/message-filter";

// Message type for conversation display
export interface Message {
    id: string;
    role: "user" | "assistant" | "system" | "status";
    content: string;
    timestamp: string;
    taskId?: string;
    sender?: string;
    metadata?: Record<string, unknown>;
    // Streaming state
    isStreaming?: boolean;
    isGenerating?: boolean;
    // Final output markers
    isFinalOutput?: boolean;
    // Screenshot message
    isScreenshot?: boolean;
    // Error states
    isError?: boolean;
    isBrowserError?: boolean;
    isCancelled?: boolean;
    // Status message type
    eventType?: string;
    // Internal: track processed delta sequences for SSE replay dedup
    _processedDeltas?: number[];
    // HITL research plan review
    isResearchPlan?: boolean;
    planRound?: number;
}

// Browser tool execution history entry
interface BrowserToolExecution {
    tool: string;
    status: "running" | "completed" | "failed";
    message?: string;
    timestamp: string;
    // Screenshot data (base64) if tool was browser_screenshot
    screenshot?: string;
    // Error details for failed tools
    error?: string;
    retryAfterSeconds?: number;
}

interface SwarmAgentInfo {
    colorIndex: number;
    role: string;
    status: "running" | "idle" | "completed";
}

interface SwarmState {
    tasks: import("@/lib/shannon/types").SwarmTask[];
    agentRegistry: Record<string, SwarmAgentInfo>;
    nextColorIndex: number;
    leadStatus: string;
}

interface RunState {
    events: ShannonEvent[];
    messages: Message[];
    status: "idle" | "running" | "completed" | "failed";
    connectionState: "idle" | "connecting" | "connected" | "reconnecting" | "error";
    streamError: string | null;
    sessionTitle: string | null;
    selectedAgent: "normal" | "deep_research" | "browser_use";
    researchStrategy: "quick" | "standard" | "deep" | "academic";
    mainWorkflowId: string | null; // Track the main workflow to distinguish from sub-workflows
    // Pause/Resume/Cancel control state
    isPaused: boolean;
    pauseCheckpoint: string | null;
    pauseReason: string | null;
    isCancelling: boolean;
    isCancelled: boolean;
    // Browser automation state (browser_use role)
    browserMode: boolean;             // Is browser_use role active
    browserAutoDetected: boolean;     // Was role auto-detected by backend
    currentIteration: number;         // Current React loop iteration (for multi-step tasks)
    totalIterations: number | null;   // Estimated total iterations (may be null if unknown)
    currentTool: string | null;       // Currently executing browser tool
    toolHistory: BrowserToolExecution[]; // History of tool executions for this task
    // HITL (Human-in-the-Loop) review state
    autoApprove: "on" | "off";        // Review mode toggle for deep_research
    reviewStatus: "none" | "reviewing" | "approved"; // Current review state
    reviewWorkflowId: string | null;  // Workflow being reviewed
    reviewVersion: number;            // Version for optimistic concurrency control
    reviewIntent: "feedback" | "ready" | "execute" | null; // LLM-detected intent
    // Swarm mode
    swarmMode: boolean;               // Multi-agent persistent loop toggle
    swarm: SwarmState | null;          // Swarm task board + agent registry (null = non-swarm)
    // Skills
    selectedSkill: string | null;     // Skill to invoke with next task
}

const initialState: RunState = {
    events: [],
    messages: [],
    status: "idle",
    connectionState: "idle",
    streamError: null,
    sessionTitle: null,
    selectedAgent: "normal",
    researchStrategy: "quick",
    mainWorkflowId: null,
    // Pause/Resume/Cancel control state
    isPaused: false,
    pauseCheckpoint: null,
    pauseReason: null,
    isCancelling: false,
    isCancelled: false,
    // Browser automation state
    browserMode: false,
    browserAutoDetected: false,
    currentIteration: 0,
    totalIterations: null,
    currentTool: null,
    toolHistory: [],
    // HITL (Human-in-the-Loop) review state
    autoApprove: "on",
    reviewStatus: "none",
    reviewWorkflowId: null,
    reviewVersion: 0,
    reviewIntent: null,
    // Swarm mode
    swarmMode: false,
    swarm: null,
    // Skills
    selectedSkill: null,
};

// Helper to create inline status messages from events
// These are SHORT human-readable status messages that appear as pills in conversation
// Per backend guidance: LLM content (LLM_OUTPUT, AGENT_CHUNK, thread.message.*) goes to Agent Trace, not pills
const STATUS_EVENT_TYPES = new Set([
    "WORKFLOW_STARTED",   // "Starting task"
    "PROGRESS",           // "Understanding your request", "Created a plan with N steps", "Reasoning step X of Y"
    "AGENT_STARTED",      // "Analyzing the problem", "Taking action"
    "AGENT_COMPLETED",    // "Decided on next step", "Action completed"
    "DELEGATION",         // Multi-agent coordination
    "DATA_PROCESSING",    // "Answer ready"
    "TOOL_INVOKED",       // "Looking this up: '...'"
    "TOOL_OBSERVATION",   // "Fetch: Wantedly Blog...", "Search: Found 5 results..."
    "TOOL_STARTED",       // Browser automation: "Navigating to...", "Clicking..."
    "TOOL_COMPLETED",     // Browser automation: "Navigation complete", "Extracted content"
    "AGENT_THINKING",     // Short status only (filtered below for long LLM content)
    "APPROVAL_REQUESTED", // Waiting for human approval
    "APPROVAL_DECISION",  // Approval granted/denied
    "WAITING",            // Waiting for dependency or resource
    "DEPENDENCY_SATISFIED", // Dependency is now available
    "STATUS_UPDATE",      // General status update
]);

// Helper to get human-readable tool names for browser automation (9 core Playwright tools)
const BROWSER_TOOL_DISPLAY_NAMES: Record<string, string> = {
    // Navigation
    browser_navigate: "Navigating...",
    navigate: "Navigating...",
    // Interactions
    browser_click: "Clicking element...",
    click: "Clicking element...",
    browser_type: "Typing...",
    type: "Typing...",
    browser_scroll: "Scrolling page...",
    scroll: "Scrolling page...",
    // Data extraction
    browser_extract: "Extracting content...",
    extract: "Extracting content...",
    browser_screenshot: "Taking screenshot...",
    screenshot: "Taking screenshot...",
    // Waiting & evaluation
    browser_wait: "Waiting for element...",
    wait: "Waiting for element...",
    browser_evaluate: "Running JavaScript...",
    evaluate: "Running JavaScript...",
    // Session management
    browser_close: "Closing session...",
    close: "Closing session...",
};

function getBrowserToolDisplayName(tool: string | null | undefined): string {
    if (!tool) return "Processing...";
    const lower = tool.toLowerCase();
    return BROWSER_TOOL_DISPLAY_NAMES[lower] || `${tool}...`;
}

// Check if an AGENT_THINKING message is short status vs long LLM content
const isShortStatusMessage = (message: string): boolean => {
    if (!message) return false;
    // Long messages with LLM reasoning content
    if (message.length > 100) return false;
    // Messages starting with "Thinking:" followed by reasoning are LLM content
    if (message.startsWith("Thinking:") && message.length > 50) return false;
    // Messages with markdown formatting are likely LLM content
    if (message.includes("**") || message.includes("REASON:") || message.includes("ACT:")) return false;
    return true;
};

// Events that should clear all status pills (only when workflow ends)
const PROGRESS_CLEARING_EVENTS = new Set([
    "WORKFLOW_COMPLETED",
    "WORKFLOW_FAILED",
]);

const runSlice = createSlice({
    name: "run",
    initialState,
    reducers: {
        addEvent: (state, action: PayloadAction<ShannonEvent>) => {
            const event = action.payload;
            const isHistorical = (event as any).isHistorical === true;

            // Deduplicate control events in the events array (for timeline display)
            // But still process state changes for all control events
            const controlEventTypes = ["workflow.pausing", "workflow.paused", "workflow.resumed", "workflow.cancelling", "workflow.cancelled"];
            const isControlEvent = controlEventTypes.includes(event.type);
            let skipEventPush = false;

            if (isControlEvent) {
                const isDuplicate = state.events.some((e: ShannonEvent) =>
                    e.type === event.type &&
                    e.workflow_id === event.workflow_id
                );
                if (isDuplicate) {
                    skipEventPush = true; // Don't add to timeline, but continue processing
                }
            }

            if (!skipEventPush) {
                state.events.push(event);

                // Dev-only: Limit events array to prevent HMR WebSocket payload overflow
                // This doesn't affect production (no HMR) but prevents dev server crashes
                if (process.env.NODE_ENV === "development" && state.events.length > 200) {
                    state.events = state.events.slice(-150); // Keep last 150 events
                }
            }

            // Historical events are loaded from the session events API on page refresh.
            // We keep them in `state.events` for the timeline, but avoid generating conversation messages
            // (those are reconstructed from turns in the page).
            if (isHistorical) {
                // Preserve session title from historical title_generator events.
                if (event.type === "thread.message.completed" && event.agent_id === "title_generator") {
                    const completedEvent = event as any;
                    const title = completedEvent.response || completedEvent.content;
                    if (title && !state.sessionTitle) {
                        state.sessionTitle = title;
                    }
                }

                // Preserve browser mode indicator on page reloads by processing ROLE_ASSIGNED.
                if (event.type === "ROLE_ASSIGNED") {
                    const roleEvent = event as any;
                    const role = roleEvent.payload?.role;
                    if (role === "browser_use") {
                        state.browserMode = true;
                        state.browserAutoDetected = roleEvent.payload?.auto_detected ?? false;
                    }
                }
                return;
            }

            // Helper to add/update status message in conversation
            const addStatusMessage = (message: string, eventType: string) => {
                if (!message) return;

                // Always remove existing status message first (we'll re-add at bottom)
                state.messages = state.messages.filter((m: any) =>
                    !(m.role === "status" && m.taskId === event.workflow_id)
                );

                const statusMsg = {
                    id: `status-${event.workflow_id}`,
                    role: "status" as const,
                    content: message,
                    eventType: eventType,
                    timestamp: new Date().toLocaleTimeString(),
                    taskId: event.workflow_id,
                };

                // Always add at the very end of messages
                state.messages.push(statusMsg);
            };

            // Remove status message when real content arrives
            const clearStatusMessage = () => {
                state.messages = state.messages.filter((m: any) =>
                    !(m.role === "status" && m.taskId === event.workflow_id)
                );
            };

            // Clear status when actual content events arrive
            if (PROGRESS_CLEARING_EVENTS.has(event.type)) {
                clearStatusMessage();
            }

            // Add inline status messages for informative events
            // Skip for historical events (loaded from API on page reload) - status pills are only for live streaming
            if (STATUS_EVENT_TYPES.has(event.type) && !isHistorical) {
                const eventWithMessage = event as { message?: string };
                const msg = eventWithMessage.message?.trim();
                if (msg && msg.length > 0) {
                    // Skip "All done" since WORKFLOW_COMPLETED handles completion
                    if (event.type === "WORKFLOW_COMPLETED" || msg === "All done") {
                        // Don't add status for completion
                    }
                    // For AGENT_THINKING, only show short status messages (not LLM reasoning content)
                    // Skip any message that's too long for a status pill (max 150 chars)
                    else if ((event.type === "AGENT_THINKING" && !isShortStatusMessage(msg)) || msg.length > 150) {
                        // Skip long messages
                    }
                    else {
                        addStatusMessage(msg, event.type);
                    }
                }
            }

            // Update status based on event type
            // Priority: STREAM_END/done > WORKFLOW_COMPLETED (main workflow only)
            if (event.type === "done" || event.type === "STREAM_END") {
                // STREAM_END or done is the authoritative "stream finished" marker
                if (state.status !== "failed") {
                    state.status = "completed";
                    // Clear any timeout/error banner since stream completed successfully
                    state.streamError = null;
                }

                // Check if we have any assistant messages for THIS task
                // (must scope by workflow_id — prior turns always have assistant messages in multi-turn)
                const hasAssistantMessage = state.messages.some(m =>
                    m.role === "assistant" && !m.isStreaming && m.taskId === event.workflow_id
                );

                if (hasAssistantMessage) {
                    // We already have the final output from streaming, remove placeholders immediately
                    state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");
                } else {
                    // No assistant message yet - replace generating placeholder with "Finalizing..."
                    // to show that we're fetching the final result from the API
                    const generatingMsgIndex = state.messages.findIndex((m: any) =>
                        m.role === "assistant" && m.isGenerating && m.taskId === event.workflow_id
                    );

                    if (generatingMsgIndex !== -1) {
                        state.messages[generatingMsgIndex] = {
                            ...state.messages[generatingMsgIndex],
                            content: "Finalizing response...",
                            isGenerating: true, // Keep as generating so fetchFinalOutput can replace it
                        };
                    }

                    // Remove status messages
                    state.messages = state.messages.filter((m: any) => m.role !== "status");
                }
            } else if (event.type === "WORKFLOW_COMPLETED") {
                // WORKFLOW_COMPLETED is treated as completion for the main workflow
                // Sub-workflows also emit WORKFLOW_COMPLETED but are ignored via workflow_id check
                const isMainWorkflow = event.workflow_id === state.mainWorkflowId;

                // For historical data (mainWorkflowId is null), only accept WORKFLOW_COMPLETED
                // if the message indicates it's the main workflow completion ("All done")
                // This prevents sub-agent completions from incorrectly marking the task as complete
                const workflowEvent = event as { message?: string };
                const isMainWorkflowMessage = workflowEvent.message === "All done" ||
                    workflowEvent.message?.includes("workflow completed");
                const isHistoricalData = state.mainWorkflowId === null &&
                    state.status !== "completed" &&
                    isMainWorkflowMessage;

                if (isMainWorkflow || isHistoricalData) {
                    // Mark as completed. If STREAM_END arrives later, it will simply confirm completion.
                    state.status = "completed";
                    // Clear any timeout/error banner since workflow completed successfully
                    state.streamError = null;

                    // Check if we have any assistant messages for this workflow
                    // Exclude interim progress messages (swarm lead) — they'll be cleaned up separately
                    const hasAssistantMessage = state.messages.some(m =>
                        m.role === "assistant" && !m.isStreaming && !(m.metadata as any)?.interim && m.taskId === event.workflow_id
                    );

                    if (hasAssistantMessage) {
                        // We already have the final output, remove placeholders immediately
                        state.messages = state.messages.filter((m: any) =>
                            !((m.isGenerating || m.role === "status") && m.taskId === event.workflow_id)
                        );
                    } else {
                        // No assistant message yet - replace generating placeholder with "Finalizing..."
                        const generatingMsgIndex = state.messages.findIndex((m: any) =>
                            m.role === "assistant" && m.isGenerating && m.taskId === event.workflow_id
                        );

                        if (generatingMsgIndex !== -1) {
                            state.messages[generatingMsgIndex] = {
                                ...state.messages[generatingMsgIndex],
                                content: "Finalizing response...",
                                isGenerating: true, // Keep as generating so fetchFinalOutput can replace it
                            };
                        }

                        // Remove status messages for this workflow
                        state.messages = state.messages.filter((m: any) =>
                            !(m.role === "status" && m.taskId === event.workflow_id)
                        );
                    }
                }
            } else if (event.type === "WORKFLOW_FAILED") {
                state.status = "failed";
                // Remove generating placeholders and status messages on failure
                state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");
            } else if (event.type === "workflow.pausing") {
                // Pause request received, workflow will pause at next checkpoint
                // Skip if already paused (control-state already set the correct status)
                if (state.isPaused) return;
                addStatusMessage((event as any).message || "Pausing at next checkpoint...", "workflow.pausing");
            } else if (event.type === "workflow.paused") {
                // Workflow is now paused
                // Skip status update if already paused (control-state already set it)
                const wasAlreadyPaused = state.isPaused;
                state.isPaused = true;
                state.pauseCheckpoint = (event as any).checkpoint || null;
                state.pauseReason = (event as any).message || null;
                // Only update status if we weren't already paused
                if (!wasAlreadyPaused) {
                    addStatusMessage("Workflow paused", "workflow.paused");
                }
            } else if (event.type === "workflow.resumed") {
                // Workflow resumed, clear pause state
                state.isPaused = false;
                state.pauseCheckpoint = null;
                state.pauseReason = null;
                // Clear status message or update to show resumed
                clearStatusMessage();
            } else if (event.type === "workflow.cancelling") {
                // Cancel request received, workflow will cancel
                // Skip if already cancelling (control-state already set it)
                if (state.isCancelling) return;
                state.isCancelling = true;
                addStatusMessage((event as any).message || "Cancelling...", "workflow.cancelling");
            } else if (event.type === "workflow.cancelled") {
                // Workflow is now cancelled
                state.status = "failed"; // Treat cancelled as a terminal state
                state.isCancelling = false;
                state.isCancelled = true;
                state.isPaused = false;
                state.pauseCheckpoint = null;

                // Find the generating placeholder to get taskId
                const generatingMsg = state.messages.find((m: any) => m.isGenerating);
                const taskId = generatingMsg?.taskId || event.workflow_id;

                // Remove generating placeholders and old status messages
                state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");

                // Add a proper system message (same style as history loading)
                state.messages.push({
                    id: `system-cancelled-${Date.now()}`,
                    role: "system" as const,
                    content: "This task was cancelled before it could complete.",
                    timestamp: new Date().toLocaleTimeString(),
                    taskId: taskId,
                    isCancelled: true,
                });
            } else if (event.type === "error") {
                state.status = "failed";
                // Remove generating placeholders and status messages on error
                state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");
            } else if (state.status === "idle") {
                state.status = "running";
            }

            // =============================================================================
            // Browser Automation Event Handlers (browser_use role)
            // =============================================================================

            // Handle ROLE_ASSIGNED - detect browser_use mode
            if (event.type === "ROLE_ASSIGNED") {
                const roleEvent = event as any;
                const role = roleEvent.payload?.role;
                if (role === "browser_use") {
                    state.browserMode = true;
                    state.browserAutoDetected = roleEvent.payload?.auto_detected ?? false;
                    state.currentIteration = 0;
                    state.toolHistory = [];
                }
            }

            // Handle TOOL_INVOKED - browser tool execution started
            // This is the actual event from backend (not TOOL_STARTED)
            if (event.type === "TOOL_INVOKED") {
                const toolEvent = event as any;
                // Extract tool name - backend sends flat or nested under payload
                const toolName = toolEvent.payload?.tool || toolEvent.tool ||
                    (toolEvent.message?.match(/^(\w+):/)?.[1]) ||
                    "browser_action";
                
                // Only track browser tools
                if (state.browserMode || toolName.startsWith("browser_")) {
                    state.currentTool = toolName;
                    
                    // Add to tool history
                    state.toolHistory.push({
                        tool: toolName,
                        status: "running",
                        message: toolEvent.message,
                        timestamp: new Date().toISOString(),
                    });
                    
                    // Show status message with tool-specific display name
                    const displayMessage = toolEvent.message || getBrowserToolDisplayName(toolName);
                    addStatusMessage(displayMessage, "TOOL_INVOKED");
                }
            }

            // Handle TOOL_OBSERVATION - browser tool result received
            // This contains the actual result (screenshot, extracted data, errors)
            if (event.type === "TOOL_OBSERVATION") {
                const toolEvent = event as any;
                // Backend sends data flat (tool, success, output) or nested under payload
                const obs = toolEvent.payload || toolEvent;
                const toolName = obs.tool || state.currentTool || "browser_action";

                // Parse the result to determine success/failure
                let success = obs.success !== false;
                let errorMessage: string | undefined = obs.error;
                let screenshotData: string | undefined;
                let pageUrl: string | undefined;
                let pageTitle: string | undefined;
                let retryAfterSeconds: number | undefined = obs.metadata?.retry_after_seconds;

                // Extract screenshot and page info from output
                const output = obs.output;
                if (output) {
                    screenshotData = output.screenshot;
                    pageUrl = output.url;
                    pageTitle = output.title;
                }
                
                // Update the last matching tool in history
                for (let i = state.toolHistory.length - 1; i >= 0; i--) {
                    if (state.toolHistory[i].status === "running") {
                        state.toolHistory[i].status = success ? "completed" : "failed";
                        if (errorMessage) {
                            state.toolHistory[i].error = errorMessage;
                        }
                        if (screenshotData) {
                            state.toolHistory[i].screenshot = screenshotData;
                        }
                        if (retryAfterSeconds) {
                            state.toolHistory[i].retryAfterSeconds = retryAfterSeconds;
                        }
                        break;
                    }
                }
                
                state.currentTool = null;
                
                // If there's a screenshot, add it as a message in the conversation
                if (screenshotData && success) {
                    const screenshotMsgId = `screenshot-${event.workflow_id}-${Date.now()}`;
                    state.messages.push({
                        id: screenshotMsgId,
                        role: "assistant" as const,
                        content: "", // Screenshot is in metadata
                        timestamp: new Date().toLocaleTimeString(),
                        taskId: event.workflow_id,
                        isScreenshot: true,
                        metadata: {
                            screenshot: screenshotData,
                            pageUrl,
                            pageTitle,
                        },
                    });
                }
                
                // If there's an error, add error message to conversation
                if (!success && errorMessage) {
                    const errorMsgId = `browser-error-${event.workflow_id}-${Date.now()}`;
                    state.messages.push({
                        id: errorMsgId,
                        role: "system" as const,
                        content: errorMessage,
                        timestamp: new Date().toLocaleTimeString(),
                        taskId: event.workflow_id,
                        isError: true,
                        isBrowserError: true,
                        metadata: {
                            tool: toolName,
                            retryAfterSeconds,
                        },
                    });
                    // Also show in status
                    addStatusMessage(`❌ ${errorMessage}`, "TOOL_OBSERVATION");
                }
            }

            // ── SCREENSHOT_SAVED: backend persisted a browser screenshot to workspace ──
            if (event.type === "SCREENSHOT_SAVED") {
                const payload = (event as any).payload || event;
                const screenshotPath = payload.screenshot_path || payload.path;
                const sessionId = payload.session_id;
                if (screenshotPath) {
                    const screenshotMsgId = `screenshot-saved-${event.workflow_id}-${screenshotPath}`;
                    state.messages.push({
                        id: screenshotMsgId,
                        role: "assistant" as const,
                        content: "",
                        timestamp: new Date().toLocaleTimeString(),
                        taskId: event.workflow_id,
                        isScreenshot: true,
                        metadata: {
                            screenshotPath,
                            sessionId,
                        },
                    });
                }
            }

            // Track iterations in browser mode (each AGENT_STARTED is a new iteration)
            if (event.type === "AGENT_STARTED" && state.browserMode) {
                state.currentIteration++;
            }

            // ── Swarm state tracking ──────────────────────────────────────
            // Auto-initialize swarm state when swarm events arrive (fixes race
            // condition where TASKLIST_UPDATED replays before setSwarmMode)
            if (!state.swarm && (event.type === "TASKLIST_UPDATED" || event.type === "TEAM_RECRUITED")) {
                state.swarm = { tasks: [], agentRegistry: {}, nextColorIndex: 0, leadStatus: "" };
                state.swarmMode = true;
            }
            if (state.swarm) {
                // Task list updates
                if (event.type === "TASKLIST_UPDATED" && (event as any).payload?.tasks) {
                    state.swarm.tasks = (event as any).payload.tasks;
                }

                // Agent registration
                if (event.type === "AGENT_STARTED" && event.agent_id) {
                    const agentId = event.agent_id;
                    const systemAgents = new Set(["swarm-lead", "swarm-supervisor", "orchestrator",
                        "planner", "title_generator", "router", "decomposer", "synthesizer"]);
                    if (!systemAgents.has(agentId)) {
                        if (!state.swarm.agentRegistry[agentId]) {
                            state.swarm.agentRegistry[agentId] = {
                                colorIndex: state.swarm.nextColorIndex++,
                                role: (event as any).payload?.role || "generalist",
                                status: "running",
                            };
                        } else {
                            state.swarm.agentRegistry[agentId].status = "running";
                        }
                    }
                }

                // Agent completion
                if (event.type === "AGENT_COMPLETED" && event.agent_id) {
                    const info = state.swarm.agentRegistry[event.agent_id];
                    if (info) {
                        info.status = "completed";
                    }
                }

                // Lead status tracking
                if (event.type === "PROGRESS" &&
                    (event.agent_id === "swarm-lead" || event.agent_id === "swarm-supervisor")) {
                    state.swarm.leadStatus = (event as any).message || "";
                }
            }

            // Note: We intentionally do NOT auto-update selectedAgent from WORKFLOW_STARTED events.
            // The user's agent selection (via dropdown) is authoritative. Session loading already
            // restores historical agent selection when loading a session. Auto-updating here would
            // override the user's explicit choice when they switch modes for follow-up messages.

            // Add timeline metadata for better display
            if (event.type === "WORKFLOW_STARTED" ||
                event.type === "AGENT_STARTED" ||
                event.type === "AGENT_THINKING" ||
                event.type === "LLM_PROMPT" ||
                event.type === "DATA_PROCESSING" ||
                event.type === "PROGRESS" ||
                event.type === "DELEGATION") {
                // These are already in events array, just need to ensure they have display data
                // The timeline component will read from events array
            }

            // Helper to identify intermediate sub-agent outputs that should only appear in timeline/agent trace
            // Per backend guidance: Don't show synthesis messages during streaming - wait for WORKFLOW_COMPLETED
            // The authoritative final answer is fetched via API after completion (fetchFinalOutput)
            const isIntermediateSubAgent = (agentId: string | undefined): boolean => {
                // Empty agent_id is treated as final output (simple responses from non-research tasks)
                if (!agentId) return false;

                // Title generator is handled separately (not shown in conversation)
                if (agentId === "title_generator") return true;

                // WHITELIST: Only simple-agent shows directly (for non-research simple tasks)
                // synthesis outputs are intermediate during streaming - final answer comes from API fetch
                const directOutputAgents = [
                    "simple-agent",        // Simple task agent (non-research)
                    "final_output",        // Canonical final output (streamed)
                    "swarm-lead",          // Lead interim progress messages
                ];

                // If it's a direct output agent, don't skip it
                if (directOutputAgents.includes(agentId)) return false;

                // Everything else is intermediate including synthesis (final answer via fetchFinalOutput)
                return true;
            };

            // HITL_RESPONSE: Lead's response to human input — timeline only (already in events array)
            if (event.type === "HITL_RESPONSE") {
                return;
            }

            // Skip title generation deltas (they're not messages)
            if (event.type === "thread.message.delta" && event.agent_id === "title_generator") {
                return;
            }

            // Skip intermediate sub-agent outputs (timeline only, not conversation)
            // Exception: title_generator completed events need to pass through to set sessionTitle
            if ((event.type === "thread.message.delta" || event.type === "thread.message.completed" || event.type === "LLM_OUTPUT")
                && isIntermediateSubAgent(event.agent_id)
                && !(event.type === "thread.message.completed" && event.agent_id === "title_generator")) {
                return;
            }

            // Handle streaming message deltas (agent trace messages)
            if (event.type === "thread.message.delta") {
                // Accumulate streaming text deltas
                const deltaEvent = event as any;

                const rawParts = Array.isArray(deltaEvent._coalescedParts)
                    ? deltaEvent._coalescedParts
                    : [{ delta: typeof deltaEvent.delta === "string" ? deltaEvent.delta : "", seq: deltaEvent.seq }];

                // Filter out diagnostic/system message parts (should only appear in timeline)
                const filteredParts = rawParts.filter((part: any) => {
                    const text = part.delta;
                    if (text && typeof text === "string") {
                        if (text.startsWith("[Incomplete response:") || text.includes("Task budget at")) {
                            return false;
                        }
                    }
                    return true;
                });

                if (filteredParts.length === 0) {
                    return;
                }

                // Find the last streaming assistant message (NOT the generating placeholder - keep that visible)
                // We search from the end because it should be recent
                let streamingMsgIndex = -1;

                for (let i = state.messages.length - 1; i >= 0; i--) {
                    if (state.messages[i].role === "assistant" && state.messages[i].isStreaming && state.messages[i].taskId === event.workflow_id) {
                        streamingMsgIndex = i;
                        break;
                    }
                }

                // stream_id identifies the message/stream (constant across all deltas in a message)
                // seq identifies the specific SSE event (unique per delta)
                // For message ID: use stream_id to identify which message to append to
                // For delta dedup: use seq (CRITICAL: stream_id is per-message, not per-delta!)
                const streamId = deltaEvent.stream_id || `msg-${Date.now()}`;
                const uniqueId = `stream-${event.workflow_id}-${streamId}`;

                // Delta dedup: only check the target streaming message's processed list (O(m) not O(n*m))
                // _processedDeltas only exists on the streaming message, not all messages
                const processedSet = streamingMsgIndex !== -1
                    ? new Set(state.messages[streamingMsgIndex]._processedDeltas || [])
                    : new Set<number>();

                const newParts = filteredParts.filter((part: any) =>
                    part.seq === undefined || !processedSet.has(part.seq)
                );
                if (newParts.length === 0) return;

                const combinedDelta = newParts.map((part: any) => part.delta || "").join("");
                const newSeqs = newParts
                    .map((part: any) => part.seq)
                    .filter((seq: any): seq is number => seq !== undefined);

                if (streamingMsgIndex !== -1) {
                    // Replace entire message object to ensure React detects changes
                    const existingMsg = state.messages[streamingMsgIndex];
                    const processedDeltas = existingMsg._processedDeltas || [];
                    let updatedProcessed = processedDeltas;
                    for (const seq of newSeqs) {
                        updatedProcessed = capProcessedDeltas(updatedProcessed, seq);
                    }
                    state.messages[streamingMsgIndex] = {
                        ...existingMsg,
                        content: existingMsg.content + combinedDelta,
                        taskId: existingMsg.taskId || event.workflow_id,
                        // Merge metadata if provided (e.g. citations)
                        metadata: deltaEvent.metadata
                            ? { ...existingMsg.metadata, ...deltaEvent.metadata }
                            : existingMsg.metadata,
                        // Track processed delta seqs to prevent duplicate append on replay
                        // Only track if we have a seq (CRITICAL: use seq, not stream_id!)
                        // Cap to last 100 entries to prevent unbounded memory growth
                        _processedDeltas: updatedProcessed,
                    };
                } else {
                    // Check if this specific uniqueId already exists (SSE replay)
                    const exists = state.messages.some((m: any) => m.id === uniqueId);
                    if (exists) return;

                    const newMessage = {
                        id: uniqueId,
                        role: "assistant" as const,
                        sender: event.agent_id, // Set sender for agent trace filtering
                        content: combinedDelta,
                        timestamp: new Date().toLocaleTimeString(),
                        isStreaming: true,
                        taskId: event.workflow_id,
                        metadata: deltaEvent.metadata, // Store metadata if provided
                        // Track processed delta seqs for replay dedup (only if seq available)
                        _processedDeltas: newSeqs,
                    };

                    // Find generating placeholder to insert before it
                    const generatingIndex = state.messages.findIndex((m: any) =>
                        m.role === "assistant" && m.isGenerating && m.taskId === event.workflow_id
                    );

                    if (generatingIndex !== -1) {
                        // Insert before generating placeholder
                        state.messages.splice(generatingIndex, 0, newMessage);
                    } else {
                        // No placeholder, append normally
                        state.messages.push(newMessage);
                    }
                }
            } else if (event.type === "thread.message.completed") {
                const completedEvent = event as any;

                // Handle title generation messages - just store the title (first-title-wins)
                if (event.agent_id === "title_generator") {
                    const title = completedEvent.response || completedEvent.content;
                    if (title && !state.sessionTitle) {
                        // Only set title once (first message wins, aligned with backend)
                        state.sessionTitle = title;
                    }
                    return;
                }

                // Handle final_output explicitly to avoid duplicates between stream/completed paths
                if (event.agent_id === "final_output") {
                    let content = completedEvent.response || completedEvent.content || "";

                    // Ensure content is a string - if it's an object, try to extract meaningful text
                    if (content && typeof content === 'object') {
                        content = (content as any).text || (content as any).message || (content as any).response ||
                            (content as any).content || (content as any).result || JSON.stringify(content);
                    }

                    // Filter out diagnostic/system/status messages
                    if (isDiagnosticMessage(content) || isStatusMessage(content)) {
                        return;
                    }

                    if (!content) return;

                    const finalId = `final-${event.workflow_id}`;

                    // Remove generating placeholder and any streaming final_output message
                    state.messages = state.messages.filter((m: any) =>
                        !(m.isGenerating && m.taskId === event.workflow_id)
                    );
                    state.messages = state.messages.filter((m: any) =>
                        !(m.taskId === event.workflow_id && m.sender === "final_output" && m.isStreaming)
                    );

                    const existingIndex = state.messages.findIndex((m: any) => m.id === finalId);
                    if (existingIndex !== -1) {
                        state.messages[existingIndex] = {
                            ...state.messages[existingIndex],
                            role: "assistant",
                            sender: "final_output",
                            content: content,
                            timestamp: new Date().toLocaleTimeString(),
                            metadata: completedEvent.metadata || state.messages[existingIndex].metadata,
                            taskId: event.workflow_id,
                            isFinalOutput: true,
                            isStreaming: false,
                        };
                    } else {
                        state.messages.push({
                            id: finalId,
                            role: "assistant" as const,
                            sender: "final_output",
                            content: content,
                            timestamp: new Date().toLocaleTimeString(),
                            metadata: completedEvent.metadata,
                            taskId: event.workflow_id,
                            isFinalOutput: true,
                        });
                    }
                    return;
                }

                // For non-title messages: handle completions (agent trace messages)
                // Find the last streaming assistant message (NOT the generating placeholder - keep that visible)
                let streamingMsgIndex = -1;

                for (let i = state.messages.length - 1; i >= 0; i--) {
                    if (state.messages[i].role === "assistant" && state.messages[i].isStreaming && state.messages[i].taskId === event.workflow_id) {
                        streamingMsgIndex = i;
                        break;
                    }
                }

                if (streamingMsgIndex !== -1) {
                    // Check if this is a diagnostic/system message before updating (should only appear in timeline)
                    const responseContent = completedEvent.response || "";
                    if (responseContent && typeof responseContent === 'string') {
                        if (responseContent.startsWith('[Incomplete response:') || responseContent.includes('Task budget at')) {
                            // Remove the streaming message instead of updating it with the error
                            state.messages.splice(streamingMsgIndex, 1);
                            return;
                        }
                    }

                    // Update existing streaming message
                    const msg = state.messages[streamingMsgIndex];
                    msg.isStreaming = false;
                    msg.taskId = msg.taskId || event.workflow_id;
                    // If response is provided, use it (it's the complete text); otherwise keep accumulated content
                    if (completedEvent.response) {
                        msg.content = completedEvent.response;
                    }
                    msg.metadata = completedEvent.metadata;
                } else {
                    // No streaming occurred, create new message with response before generating placeholder
                    let content = completedEvent.response || completedEvent.content || "";

                    // Ensure content is a string - if it's an object, try to extract meaningful text
                    if (content && typeof content === 'object') {
                        // Try common patterns for extracting text from response objects
                        content = (content as any).text || (content as any).message || (content as any).response ||
                            (content as any).content || (content as any).result || JSON.stringify(content);
                    }

                    // Filter out diagnostic/system/status messages that should only appear in timeline
                    if (isDiagnosticMessage(content) || isStatusMessage(content)) {
                        return;
                    }

                    if (content) {
                        // Per backend guidance: synthesis outputs during streaming are intermediate
                        // The final answer is fetched via API after WORKFLOW_COMPLETED
                        const isSynthesisAgent = ["synthesis", "streaming_synthesis"].includes(event.agent_id || "");

                        if (isSynthesisAgent) return;

                        // Use stream_id as primary identifier (consistent with delta path)
                        // This ensures proper dedup when same message arrives via both paths
                        const stableId = completedEvent.stream_id || completedEvent.seq || Date.now();
                        const uniqueId = `stream-${event.workflow_id}-${stableId}`;

                        // Check if this specific uniqueId already exists (SSE replay)
                        const exists = state.messages.some((m: any) => m.id === uniqueId);
                        if (exists) return;

                        // Cross-path dedup: LLM_OUTPUT uses different ID format ({agentId}-...)
                        // Check if same content already exists for this task
                        const hasEquivalentFromLLM = state.messages.some((m: any) =>
                            m.taskId === event.workflow_id &&
                            m.role === "assistant" &&
                            !m.isStreaming &&
                            !m.isGenerating &&
                            m.content === content
                        );
                        if (hasEquivalentFromLLM) return;

                        // For simple-agent, remove generating placeholder
                        if (event.agent_id === "simple-agent") {
                            state.messages = state.messages.filter((m: any) =>
                                !(m.isGenerating && m.taskId === event.workflow_id)
                            );
                        }

                        const newMessage = {
                            id: uniqueId,
                            role: "assistant" as const,
                            sender: completedEvent.agent_id,
                            content: content,
                            timestamp: new Date().toLocaleTimeString(),
                            metadata: completedEvent.metadata,
                            taskId: event.workflow_id,
                        };

                        // Find generating placeholder to insert before it (if not already removed)
                        const generatingIndex = state.messages.findIndex((m: any) =>
                            m.role === "assistant" && m.isGenerating && m.taskId === event.workflow_id
                        );

                        if (generatingIndex !== -1) {
                            state.messages.splice(generatingIndex, 0, newMessage);
                        } else {
                            state.messages.push(newMessage);
                        }
                    }
                }
            } else if (event.type === "LLM_OUTPUT") {
                // Handle LLM_OUTPUT event (sometimes used instead of thread.message.*)
                const llmEvent = event as any;

                // Per backend guidance: synthesis outputs during streaming are intermediate
                // The final answer is fetched via API after WORKFLOW_COMPLETED
                // Only show simple-agent outputs directly (non-research tasks)
                const isSynthesisAgent = ["synthesis", "streaming_synthesis"].includes(event.agent_id || "");
                const isFinalOutput = event.agent_id === "final_output";

                if (isSynthesisAgent) return;

                // Handle final_output event - canonical final answer from backend
                // Content is in event.message (not payload.text)
                // Note: payload.tokens_used is NOT stored here to avoid double-counting
                // Full metadata (citations, costs, usage) comes from API call after completion
                if (isFinalOutput) {
                    const finalContent = llmEvent.message || "";
                    if (finalContent) {
                        const messageId = `final-${event.workflow_id}`;
                        // Remove generating placeholder
                        state.messages = state.messages.filter((m: any) =>
                            !(m.isGenerating && m.taskId === event.workflow_id)
                        );
                        // Remove any streaming final_output message to avoid duplicates
                        state.messages = state.messages.filter((m: any) =>
                            !(m.taskId === event.workflow_id && m.sender === "final_output" && m.isStreaming)
                        );
                        const existingIndex = state.messages.findIndex((m: any) => m.id === messageId);
                        if (existingIndex !== -1) {
                            state.messages[existingIndex] = {
                                ...state.messages[existingIndex],
                                role: "assistant",
                                sender: "final_output",
                                content: finalContent,
                                timestamp: new Date().toLocaleTimeString(),
                                taskId: event.workflow_id,
                                isFinalOutput: true,
                                isStreaming: false,
                            };
                        } else {
                            // Check if we already have a final message for this task (avoid duplicates)
                            const hasExistingFinal = state.messages.some((m: any) =>
                                m.taskId === event.workflow_id && m.isFinalOutput
                            );
                            if (!hasExistingFinal) {
                                state.messages.push({
                                    id: messageId,
                                    role: "assistant",
                                    sender: "final_output",
                                    content: finalContent,
                                    timestamp: new Date().toLocaleTimeString(),
                                    taskId: event.workflow_id,
                                    isFinalOutput: true,  // Flag for deduplication with fetchFinalOutput
                                    // Note: metadata intentionally omitted - will be synced from API
                                });
                            }
                        }
                    }
                    return;
                }

                // Handle interim_reply from swarm lead — replace in-place, not accumulate
                if (llmEvent.payload?.interim === true) {
                    const interimContent = llmEvent.message || "";
                    if (interimContent) {
                        const interimId = `interim-lead-${event.workflow_id}`;
                        const existingIdx = state.messages.findIndex((m: any) => m.id === interimId);
                        if (existingIdx !== -1) {
                            // Replace content in-place (same bubble, updated text)
                            state.messages[existingIdx].content = interimContent;
                            state.messages[existingIdx].timestamp = new Date().toLocaleTimeString();
                        } else {
                            state.messages.push({
                                id: interimId,
                                role: "assistant",
                                sender: "swarm-lead",
                                content: interimContent,
                                timestamp: new Date().toLocaleTimeString(),
                                metadata: { interim: true },
                                taskId: event.workflow_id,
                            });
                        }
                    }
                    return;
                }

                const content = llmEvent.payload?.text || llmEvent.message || "";

                // Filter out diagnostic/system messages that should only appear in timeline
                if (isDiagnosticMessage(content)) {
                    return;
                }

                if (content) {
                    // Generate stable ID for deduplication on SSE replay
                    // Use seq if available, otherwise use a composite ID based on content to prevent duplicates
                    // We use agent_id + workflow_id + (seq OR content hash) for a robust unique identifier
                    const contentIdentifier = content.length > 100 
                        ? `${content.slice(0, 50)}${content.slice(-50)}`.replace(/\s/g, '')
                        : content.replace(/\s/g, '');
                    
                    const stableSeq = event.seq ?? `msg-${contentIdentifier}`;
                    const uniqueId = `${event.agent_id || 'assistant'}-${event.workflow_id}-${stableSeq}`;

                    // Check for duplicate before adding (prevents duplicates on SSE reconnect/replay)
                    const isDuplicate = state.messages.some((m: any) => m.id === uniqueId);
                    if (isDuplicate) return;

                    // Cross-path dedup: thread.message.completed uses different ID format (stream-...)
                    // Check if same content already exists for this task to prevent duplicates across paths
                    const hasEquivalentMessage = state.messages.some((m: any) =>
                        m.taskId === event.workflow_id &&
                        m.role === "assistant" &&
                        !m.isStreaming &&
                        !m.isGenerating &&
                        m.content === content
                    );
                    if (hasEquivalentMessage) return;

                    // For simple-agent, remove generating placeholder
                    if (event.agent_id === "simple-agent") {
                        state.messages = state.messages.filter((m: any) =>
                            !(m.isGenerating && m.taskId === event.workflow_id)
                        );
                    }

                    state.messages.push({
                        id: uniqueId,
                        role: "assistant",
                        sender: event.agent_id,
                        content: content,
                        timestamp: new Date().toLocaleTimeString(),
                        metadata: event.metadata,
                        taskId: event.workflow_id,
                    });
                }
            } else if (event.type === "WORKFLOW_COMPLETED") {
                // Clean up interim messages — final answer will come from fetchFinalOutput
                state.messages = state.messages.filter((m: any) =>
                    !(m.metadata?.interim && m.taskId === event.workflow_id)
                );

                // Workflow completed - check if there's a final result to show
                const workflowEvent = event as any;
                const hasAssistantMessage = state.messages.some(m =>
                    m.role === "assistant" && m.taskId === event.workflow_id
                );

                // If no assistant message and the event has a message/result, add it
                // Note: WORKFLOW_COMPLETED usually just has "All done", not the actual result
                // We should ONLY add substantive content, not status messages
                if (!hasAssistantMessage && workflowEvent.message && workflowEvent.message !== "All done") {
                    // Filter out diagnostic/system/status messages that shouldn't appear as conversation content
                    // The actual result will be fetched via fetchFinalOutput from task.result
                    if (isDiagnosticMessage(workflowEvent.message) || isStatusMessage(workflowEvent.message)) {
                        return;
                    }
                    // Additional WORKFLOW_COMPLETED-specific filters
                    const lowerMessage = (workflowEvent.message || '').toLowerCase();
                    if (lowerMessage.includes('research completed') ||
                        lowerMessage.includes('pipeline completed') ||
                        lowerMessage.endsWith(' completed')) {
                        return;
                    }

                    const uniqueId = `workflow-${event.workflow_id}-${Date.now()}`;
                    state.messages.push({
                        id: uniqueId,
                        role: "assistant",
                        content: workflowEvent.message,
                        timestamp: new Date().toLocaleTimeString(),
                        taskId: event.workflow_id,
                    });
                }
            } else if (event.type === "RESEARCH_PLAN_READY") {
                // Research plan generated - enter review mode
                const planEvent = event as any;
                state.reviewStatus = "reviewing";
                state.reviewWorkflowId = event.workflow_id;
                state.reviewVersion = 0;
                // Read intent from SSE payload (e.g. "ready" shows Approve button, "feedback" does not)
                const planIntent = planEvent.payload?.intent || null;
                state.reviewIntent = planIntent;
                console.log("[Redux] Research plan ready - entering review mode for workflow:", event.workflow_id, "intent:", planIntent);

                // For historical events (isHistorical flag from turn loading), skip message
                // to prevent plan appearing before user query
                const isHistorical = (event as any)._isHistorical;
                if (isHistorical) {
                    console.log("[Redux] Historical RESEARCH_PLAN_READY - setting state only, skipping message");
                    return;
                }

                // Add plan as assistant message with special styling flag
                if (planEvent.message) {
                    state.messages.push({
                        id: `research-plan-${event.workflow_id}-r1`,
                        role: "assistant" as const,
                        content: planEvent.message,
                        timestamp: new Date().toLocaleTimeString(),
                        taskId: event.workflow_id,
                        isResearchPlan: true,
                        planRound: 1,
                    });
                }
            } else if (event.type === "RESEARCH_PLAN_APPROVED") {
                // Plan approved - exit review mode
                state.reviewStatus = "approved";
                state.reviewIntent = null;
                console.log("[Redux] Research plan approved for workflow:", event.workflow_id);

                const isHistorical = (event as any)._isHistorical;
                if (isHistorical) {
                    console.log("[Redux] Historical RESEARCH_PLAN_APPROVED - setting state only");
                    return;
                }

                // Add approval confirmation message
                state.messages.push({
                    id: `plan-approved-${event.workflow_id}-${Date.now()}`,
                    role: "system" as const,
                    content: "✓ Research plan approved. Starting execution...",
                    timestamp: new Date().toLocaleTimeString(),
                    taskId: event.workflow_id,
                });

                // Add generating placeholder for the upcoming result
                state.messages.push({
                    id: `post-approval-generating-${event.workflow_id}`,
                    role: "assistant" as const,
                    content: "Executing research plan...",
                    timestamp: new Date().toLocaleTimeString(),
                    isGenerating: true,
                    taskId: event.workflow_id,
                });
            } else if (event.type === "REVIEW_USER_FEEDBACK") {
                // User feedback during HITL review — handled by page.tsx handlers for live,
                // and by turn loading for historical. Skip here to avoid duplicates.
                console.log("[Redux] REVIEW_USER_FEEDBACK - skipping (handled by page.tsx)");
            } else if (event.type === "RESEARCH_PLAN_UPDATED") {
                // Updated research plan from feedback — handled by page.tsx handlers for live,
                // and by turn loading for historical. Skip here to avoid duplicates.
                console.log("[Redux] RESEARCH_PLAN_UPDATED - skipping (handled by page.tsx)");
            }
            // AGENT_COMPLETED, TOOL_INVOKED, TOOL_OBSERVATION are timeline-only events
        },
        resetRun: (state) => {
            state.events = [];
            state.messages = [];
            state.status = "idle";
            state.connectionState = "idle";
            state.streamError = null;
            state.sessionTitle = null;
            state.mainWorkflowId = null;
            // Reset pause/resume/cancel state
            state.isPaused = false;
            state.pauseCheckpoint = null;
            state.pauseReason = null;
            state.isCancelling = false;
            state.isCancelled = false;
            // Reset browser automation state
            state.browserMode = false;
            state.browserAutoDetected = false;
            state.currentIteration = 0;
            state.totalIterations = null;
            state.currentTool = null;
            state.toolHistory = [];
            // Reset HITL review state (keep autoApprove preference)
            state.reviewStatus = "none";
            state.reviewWorkflowId = null;
            state.reviewVersion = 0;
            state.reviewIntent = null;
            // Reset swarm mode (prevent stale task board on session switch)
            state.swarmMode = false;
            state.swarm = null;
            // Keep selectedAgent, autoApprove and other preferences persistent across sessions
        },
        // Full reset including user preferences - use on logout/user switch to prevent data leakage
        resetRunFull: (state) => {
            state.events = [];
            state.messages = [];
            state.status = "idle";
            state.connectionState = "idle";
            state.streamError = null;
            state.sessionTitle = null;
            state.mainWorkflowId = null;
            state.isPaused = false;
            state.pauseCheckpoint = null;
            state.pauseReason = null;
            state.isCancelling = false;
            state.isCancelled = false;
            // Also reset user preferences to defaults
            state.selectedAgent = "normal";
            state.researchStrategy = "quick";
            // Reset browser automation state
            state.browserMode = false;
            state.browserAutoDetected = false;
            state.currentIteration = 0;
            state.totalIterations = null;
            state.currentTool = null;
            state.toolHistory = [];
            // Reset HITL review state including preferences
            state.autoApprove = "on";
            state.reviewStatus = "none";
            state.reviewWorkflowId = null;
            state.reviewVersion = 0;
            state.reviewIntent = null;
            // Reset swarm mode
            state.swarmMode = false;
            state.swarm = null;
            // Reset skills
            state.selectedSkill = null;
        },
        addMessage: (state, action: PayloadAction<any>) => {
            if (!state.messages.some(m => m.id === action.payload.id)) {
                state.messages.push(action.payload);
            }
        },
        removeMessage: (state, action: PayloadAction<string>) => {
            const messageId = action.payload;
            state.messages = state.messages.filter(m => m.id !== messageId);
        },
        updateMessageMetadata: (state, action: PayloadAction<{ taskId: string; metadata: any }>) => {
            const { taskId, metadata } = action.payload;

            // Find the last assistant message for this task
            for (let i = state.messages.length - 1; i >= 0; i--) {
                const msg = state.messages[i];
                if (msg.role === "assistant" && msg.taskId === taskId) {
                    // Create a new object to trigger React re-render
                    state.messages[i] = {
                        ...msg,
                        metadata: { ...msg.metadata, ...metadata }
                    };
                    break;
                }
            }
        },
        setConnectionState: (state, action: PayloadAction<RunState["connectionState"]>) => {
            state.connectionState = action.payload;
            if (action.payload === "connected") {
                state.streamError = null;
            }
        },
        setStreamError: (state, action: PayloadAction<string | null>) => {
            state.streamError = action.payload;
            if (action.payload) {
                state.connectionState = "error";
            }
        },
        setSelectedAgent: (state, action: PayloadAction<RunState["selectedAgent"]>) => {
            state.selectedAgent = action.payload;
        },
        setResearchStrategy: (state, action: PayloadAction<RunState["researchStrategy"]>) => {
            state.researchStrategy = action.payload;
        },
        setMainWorkflowId: (state, action: PayloadAction<string | null>) => {
            state.mainWorkflowId = action.payload;
        },
        setStatus: (state, action: PayloadAction<RunState["status"]>) => {
            state.status = action.payload;
        },
        setPaused: (state, action: PayloadAction<{ paused: boolean; checkpoint?: string; reason?: string }>) => {
            state.isPaused = action.payload.paused;
            state.pauseCheckpoint = action.payload.checkpoint || null;
            state.pauseReason = action.payload.reason || null;

            // Update status message to match pause state
            if (action.payload.paused) {
                // Remove old status and add paused status
                state.messages = state.messages.filter((m: any) => m.role !== "status");
                state.messages.push({
                    id: `status-paused-${Date.now()}`,
                    role: "status" as const,
                    content: "Workflow paused",
                    eventType: "workflow.paused",
                    timestamp: new Date().toLocaleTimeString(),
                });
            } else {
                // Resumed - clear status message
                state.messages = state.messages.filter((m: any) => m.role !== "status");
            }
        },
        setCancelling: (state, action: PayloadAction<boolean>) => {
            state.isCancelling = action.payload;

            // Update status message to match cancelling state
            if (action.payload) {
                // Remove old status and add cancelling status
                state.messages = state.messages.filter((m: any) => m.role !== "status");
                state.messages.push({
                    id: `status-cancelling-${Date.now()}`,
                    role: "status" as const,
                    content: "Cancelling workflow...",
                    eventType: "workflow.cancelling",
                    timestamp: new Date().toLocaleTimeString(),
                });
            } else {
                // Cancelled complete - clear status message
                state.messages = state.messages.filter((m: any) => m.role !== "status");
            }
        },
        setCancelled: (state, action: PayloadAction<boolean>) => {
            state.isCancelled = action.payload;
            state.isCancelling = false;

            if (action.payload) {
                // Task is cancelled - update status and replace generating placeholder with cancelled message
                state.status = "failed";

                // Find and remove the generating placeholder, capturing its taskId for the replacement message
                const generatingMsg = state.messages.find((m: any) => m.isGenerating);
                const taskId = generatingMsg?.taskId;

                // Remove generating placeholders and status messages
                state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");

                // Add a proper system message (same style as history loading) instead of status pill
                state.messages.push({
                    id: `system-cancelled-${Date.now()}`,
                    role: "system" as const,
                    content: "This task was cancelled before it could complete.",
                    timestamp: new Date().toLocaleTimeString(),
                    taskId: taskId,
                    isCancelled: true,
                });
            }
        },
        removeGeneratingPlaceholder: (state, action: PayloadAction<string>) => {
            // Remove the "Finalizing response..." or "Generating..." placeholder for a specific taskId
            const taskId = action.payload;
            state.messages = state.messages.filter((m: any) =>
                !(m.role === "assistant" && m.isGenerating && m.taskId === taskId)
            );
        },
        // Browser automation state setters
        setBrowserMode: (state, action: PayloadAction<{ active: boolean; autoDetected?: boolean }>) => {
            state.browserMode = action.payload.active;
            state.browserAutoDetected = action.payload.autoDetected ?? false;
            if (action.payload.active) {
                // Reset iteration tracking when entering browser mode
                state.currentIteration = 0;
                state.totalIterations = null;
                state.toolHistory = [];
            }
        },
        setCurrentTool: (state, action: PayloadAction<string | null>) => {
            state.currentTool = action.payload;
        },
        setTotalIterations: (state, action: PayloadAction<number | null>) => {
            state.totalIterations = action.payload;
        },
        // HITL (Human-in-the-Loop) review state setters
        setAutoApprove: (state, action: PayloadAction<RunState["autoApprove"]>) => {
            state.autoApprove = action.payload;
        },
        setReviewStatus: (state, action: PayloadAction<RunState["reviewStatus"]>) => {
            state.reviewStatus = action.payload;
        },
        setReviewVersion: (state, action: PayloadAction<number>) => {
            state.reviewVersion = action.payload;
        },
        setReviewIntent: (state, action: PayloadAction<RunState["reviewIntent"]>) => {
            state.reviewIntent = action.payload;
        },
        setSwarmMode: (state, action: PayloadAction<boolean>) => {
            state.swarmMode = action.payload;
            if (action.payload && !state.swarm) {
                state.swarm = {
                    tasks: [],
                    agentRegistry: {},
                    nextColorIndex: 0,
                    leadStatus: "",
                };
            }
            if (!action.payload) {
                state.swarm = null;
            }
        },
        setSelectedSkill: (state, action: PayloadAction<string | null>) => {
            state.selectedSkill = action.payload;
        },
    },
});

export const { addEvent, resetRun, resetRunFull, addMessage, removeMessage, updateMessageMetadata, setConnectionState, setStreamError, setSelectedAgent, setResearchStrategy, setMainWorkflowId, setStatus, setPaused, setCancelling, setCancelled, removeGeneratingPlaceholder, setBrowserMode, setCurrentTool, setTotalIterations, setAutoApprove, setReviewStatus, setReviewVersion, setReviewIntent, setSwarmMode, setSelectedSkill } = runSlice.actions;
export default runSlice.reducer;
