/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { createSlice, PayloadAction } from "@reduxjs/toolkit";
import { ShannonEvent } from "../shannon/types";

interface RunState {
    events: ShannonEvent[];
    messages: any[]; // We'll transform events into messages
    status: "idle" | "running" | "completed" | "failed";
    connectionState: "idle" | "connecting" | "connected" | "reconnecting" | "error";
    streamError: string | null;
    sessionTitle: string | null;
    selectedAgent: "normal" | "deep_research";
    researchStrategy: "quick" | "standard" | "deep" | "academic";
    mainWorkflowId: string | null; // Track the main workflow to distinguish from sub-workflows
    // Pause/Resume/Cancel control state
    isPaused: boolean;
    pauseCheckpoint: string | null;
    pauseReason: string | null;
    isCancelling: boolean;
    isCancelled: boolean;
    // Review Plan (HITL) state
    reviewPlan: "auto" | "review";
    reviewStatus: "none" | "reviewing" | "approved";
    reviewWorkflowId: string | null;
    reviewVersion: number;
    reviewIntent: "feedback" | "ready" | "execute" | null;
}

const initialState: RunState = {
    events: [],
    messages: [],
    status: "idle",
    connectionState: "idle",
    streamError: null,
    sessionTitle: null,
    selectedAgent: "normal",
    researchStrategy: "standard",
    mainWorkflowId: null,
    // Pause/Resume/Cancel control state
    isPaused: false,
    pauseCheckpoint: null,
    pauseReason: null,
    isCancelling: false,
    isCancelled: false,
    // Review Plan (HITL) state
    reviewPlan: "auto",
    reviewStatus: "none",
    reviewWorkflowId: null,
    reviewVersion: 0,
    reviewIntent: null,
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
    "AGENT_THINKING",     // Short status only (filtered below for long LLM content)
    "APPROVAL_REQUESTED", // Waiting for human approval
    "APPROVAL_DECISION",  // Approval granted/denied
    "WAITING",            // Waiting for dependency or resource
    "DEPENDENCY_SATISFIED", // Dependency is now available
    "STATUS_UPDATE",      // General status update
]);

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
                    console.log("[Redux] Duplicate control event (will still process state):", event.type);
                    skipEventPush = true; // Don't add to timeline, but continue processing
                }
            }
            
            if (!skipEventPush) {
                state.events.push(event);
            }

            console.log("[Redux] Received event:", event.type, event);

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
            const isHistorical = (event as any).isHistorical === true;
            if (STATUS_EVENT_TYPES.has(event.type) && !isHistorical) {
                const eventWithMessage = event as { message?: string };
                const msg = eventWithMessage.message?.trim();
                if (msg && msg.length > 0) {
                    // Skip "All done" since WORKFLOW_COMPLETED handles completion
                    if (event.type === "WORKFLOW_COMPLETED" || msg === "All done") {
                        // Don't add status for completion
                    } 
                    // For AGENT_THINKING, only show short status messages (not LLM reasoning content)
                    else if (event.type === "AGENT_THINKING" && !isShortStatusMessage(msg)) {
                        console.log("[Redux] Skipping long AGENT_THINKING (LLM content):", msg.substring(0, 50));
                    }
                    // Skip any message that's too long for a status pill (max 150 chars)
                    else if (msg.length > 150) {
                        console.log("[Redux] Skipping long status message:", msg.substring(0, 50) + "...");
                    }
                    else {
                        addStatusMessage(msg, event.type);
                        console.log("[Redux] Added status message:", msg);
                    }
                }
            }

            // Update status based on event type
            // Priority: STREAM_END/done > WORKFLOW_COMPLETED (main workflow only)
            if (event.type === "done" || event.type === "STREAM_END") {
                // STREAM_END or done is the authoritative "stream finished" marker
                if (state.status !== "failed") {
                    state.status = "completed";
                }
                console.log("[Redux] Stream ended (event type:", event.type, ")");

                // Remove generating placeholder and status messages when stream ends
                state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");
                console.log("[Redux] Removed generating placeholders and status messages on stream end");

                // Check if we have any assistant messages
                const hasAssistantMessage = state.messages.some(m => m.role === "assistant" && !m.isStreaming);
                if (!hasAssistantMessage) {
                    console.warn("[Redux] Stream completion event received but no assistant message found - fetchFinalOutput should trigger");
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
                    console.log(`[Redux] Main workflow completed - marking as completed ${isHistoricalData ? "(historical)" : "(live)"}`, 
                        "message:", workflowEvent.message);
                    // Mark as completed. If STREAM_END arrives later, it will simply confirm completion.
                    // This handles workflows that don't emit STREAM_END (edge cases).
                    state.status = "completed";
                    
                    // Remove generating placeholder and status messages for the completed workflow
                    state.messages = state.messages.filter((m: any) => 
                        !((m.isGenerating || m.role === "status") && m.taskId === event.workflow_id)
                    );
                    console.log("[Redux] Removed generating placeholder and status messages for completed workflow");
                } else if (state.mainWorkflowId === null) {
                    // Log sub-workflow completion but don't mark as complete
                    console.log("[Redux] Sub-workflow completed (not main workflow):", event.workflow_id, 
                        "message:", workflowEvent.message);
                }
            } else if (event.type === "WORKFLOW_FAILED") {
                state.status = "failed";
                // Remove generating placeholders and status messages on failure
                state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");
                const failedMessage = (event as any).message;
                if (failedMessage) {
                    console.log("[Redux] Workflow failed:", failedMessage);
                } else {
                    console.log("[Redux] Workflow failed (no message)");
                }
            } else if (event.type === "workflow.pausing") {
                // Pause request received, workflow will pause at next checkpoint
                // Skip if already paused (control-state already set the correct status)
                if (state.isPaused) {
                    console.log("[Redux] Skipping workflow.pausing - already paused");
                    return;
                }
                console.log("[Redux] Workflow pausing:", (event as any).message);
                addStatusMessage((event as any).message || "Pausing at next checkpoint...", "workflow.pausing");
            } else if (event.type === "workflow.paused") {
                // Workflow is now paused
                // Skip status update if already paused (control-state already set it)
                const wasAlreadyPaused = state.isPaused;
                state.isPaused = true;
                state.pauseCheckpoint = (event as any).checkpoint || null;
                state.pauseReason = (event as any).message || null;
                console.log("[Redux] Workflow paused at checkpoint:", state.pauseCheckpoint);
                // Only update status if we weren't already paused
                if (!wasAlreadyPaused) {
                    addStatusMessage("Workflow paused", "workflow.paused");
                }
            } else if (event.type === "workflow.resumed") {
                // Workflow resumed, clear pause state
                state.isPaused = false;
                state.pauseCheckpoint = null;
                state.pauseReason = null;
                console.log("[Redux] Workflow resumed:", (event as any).message);
                // Clear status message or update to show resumed
                clearStatusMessage();
            } else if (event.type === "workflow.cancelling") {
                // Cancel request received, workflow will cancel
                // Skip if already cancelling (control-state already set it)
                if (state.isCancelling) {
                    console.log("[Redux] Skipping workflow.cancelling - already cancelling");
                    return;
                }
                state.isCancelling = true;
                console.log("[Redux] Workflow cancelling:", (event as any).message);
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
                console.log("[Redux] Workflow cancelled:", (event as any).message);
            } else if (event.type === "error") {
                state.status = "failed";
                // Remove generating placeholders and status messages on error
                state.messages = state.messages.filter((m: any) => !m.isGenerating && m.role !== "status");
                console.log("[Redux] Removed generating placeholders and status messages on error");
            } else if (state.status === "idle") {
                state.status = "running";
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
                ];
                
                // If it's a direct output agent, don't skip it
                if (directOutputAgents.includes(agentId)) return false;
                
                // Everything else is intermediate including synthesis (final answer via fetchFinalOutput)
                return true;
            };

            // Skip title generation deltas (they're not messages)
            if (event.type === "thread.message.delta" && event.agent_id === "title_generator") {
                return;
            }

            // Skip intermediate sub-agent outputs (timeline only, not conversation)
            // Exception: title_generator completed events need to pass through to set sessionTitle
            if ((event.type === "thread.message.delta" || event.type === "thread.message.completed" || event.type === "LLM_OUTPUT") 
                && isIntermediateSubAgent(event.agent_id)
                && !(event.type === "thread.message.completed" && event.agent_id === "title_generator")) {
                console.log("[Redux] Skipping intermediate sub-agent message (timeline only):", event.agent_id);
                return;
            }
            
            // Handle streaming message deltas (agent trace messages)
            if (event.type === "thread.message.delta") {
                // Accumulate streaming text deltas
                const deltaEvent = event as any;
                
                // Filter out deltas that are diagnostic/system messages (should only appear in timeline)
                const deltaContent = deltaEvent.delta || "";
                if (deltaContent && typeof deltaContent === 'string') {
                    if (deltaContent.startsWith('[Incomplete response:') || deltaContent.includes('Task budget at')) {
                        console.log("[Redux] Skipping delta with diagnostic/system message (timeline only)");
                        return;
                    }
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

                console.log("[Redux] Delta received:", deltaEvent.delta, "Streaming msg index:", streamingMsgIndex);

                if (streamingMsgIndex !== -1) {
                    // Append delta to existing streaming message
                    state.messages[streamingMsgIndex].content += deltaEvent.delta || "";
                    state.messages[streamingMsgIndex].taskId = state.messages[streamingMsgIndex].taskId || event.workflow_id;

                    // Update metadata if provided in delta (e.g. citations)
                    if (deltaEvent.metadata) {
                        state.messages[streamingMsgIndex].metadata = {
                            ...state.messages[streamingMsgIndex].metadata,
                            ...deltaEvent.metadata
                        };
                    }
                    console.log("[Redux] Appended to existing message");
                } else {
                    // Create new streaming message with unique ID
                    // Insert it BEFORE the generating placeholder if one exists
                    const uniqueId = `${event.agent_id || 'assistant'}-${event.workflow_id}-${Date.now()}`;
                    const newMessage = {
                        id: uniqueId,
                        role: "assistant" as const,
                        sender: event.agent_id, // Set sender for agent trace filtering
                        content: deltaEvent.delta || "",
                        timestamp: new Date().toLocaleTimeString(),
                        isStreaming: true,
                        taskId: event.workflow_id,
                        metadata: deltaEvent.metadata, // Store metadata if provided
                    };
                    
                    // Find generating placeholder to insert before it
                    const generatingIndex = state.messages.findIndex((m: any) => 
                        m.role === "assistant" && m.isGenerating && m.taskId === event.workflow_id
                    );
                    
                    if (generatingIndex !== -1) {
                        // Insert before generating placeholder
                        state.messages.splice(generatingIndex, 0, newMessage);
                        console.log("[Redux] Created new streaming message before generating placeholder");
                    } else {
                        // No placeholder, append normally
                        state.messages.push(newMessage);
                        console.log("[Redux] Created new streaming message");
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
                        console.log("[Redux] Session title set:", title);
                    } else if (title && state.sessionTitle) {
                        console.log("[Redux] Session title already set, ignoring:", title);
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

                console.log("[Redux] thread.message.completed event:", completedEvent);
                console.log("[Redux] Message completed, response:", completedEvent.response?.substring(0, 100));
                console.log("[Redux] Metadata:", completedEvent.metadata);
                if (completedEvent.metadata?.citations) {
                    console.log("[Redux] Citations found:", completedEvent.metadata.citations.length);
                }

                if (streamingMsgIndex !== -1) {
                    // Check if this is a diagnostic/system message before updating (should only appear in timeline)
                    const responseContent = completedEvent.response || "";
                    if (responseContent && typeof responseContent === 'string') {
                        if (responseContent.startsWith('[Incomplete response:') || responseContent.includes('Task budget at')) {
                            console.log("[Redux] Removing streaming message with diagnostic/system content (timeline only)");
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
                        console.log("[Redux] Updated streaming message with complete response");
                    }
                    msg.metadata = completedEvent.metadata;
                } else {
                    // No streaming occurred, create new message with response before generating placeholder
                    let content = completedEvent.response || completedEvent.content || "";
                    
                    // Ensure content is a string - if it's an object, try to extract meaningful text
                    if (content && typeof content === 'object') {
                        console.warn("[Redux] thread.message.completed content is an object:", content);
                        // Try common patterns for extracting text from response objects
                        content = (content as any).text || (content as any).message || (content as any).response || 
                                  (content as any).content || (content as any).result || JSON.stringify(content);
                    }
                    
                    // Filter out diagnostic/system/status messages that should only appear in timeline
                    if (content && typeof content === 'string') {
                        const lowerContent = content.toLowerCase();
                        if (content.startsWith('[Incomplete response:') || 
                            content.includes('Task budget at') ||
                            lowerContent.includes('task completed') ||
                            lowerContent.includes('task done') ||
                            lowerContent === 'done' ||
                            lowerContent === 'completed' ||
                            lowerContent === 'success' ||
                            (lowerContent.includes('successfully') && content.length < 100)) {
                            console.log("[Redux] Skipping status/system message in thread.message.completed (timeline only):", content);
                            return;
                        }
                    }
                    
                    if (content) {
                        // Per backend guidance: synthesis outputs during streaming are intermediate
                        // The final answer is fetched via API after WORKFLOW_COMPLETED
                        const isSynthesisAgent = ["synthesis", "streaming_synthesis"].includes(event.agent_id || "");
                        
                        if (isSynthesisAgent) {
                            console.log("[Redux] Skipping synthesis thread.message.completed - final answer will come from API fetch");
                            return;
                        }
                        
                        const uniqueId = `${event.agent_id || 'assistant'}-${event.workflow_id}-${event.seq || Date.now()}`;
                        
                        // For simple-agent, remove generating placeholder
                        if (event.agent_id === "simple-agent") {
                            console.log("[Redux] 🎯 Simple-agent from thread.message.completed");
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
                            // Insert before generating placeholder
                            state.messages.splice(generatingIndex, 0, newMessage);
                            console.log("[Redux] Created new message from completion event before generating placeholder");
                        } else {
                            // No placeholder, append normally
                            state.messages.push(newMessage);
                            console.log("[Redux] Created new message from completion event");
                        }
                    } else {
                        console.warn("[Redux] thread.message.completed has no content! Event:", completedEvent);
                    }
                }
            } else if (event.type === "LLM_OUTPUT") {
                // Handle LLM_OUTPUT event (sometimes used instead of thread.message.*)
                const llmEvent = event as any;
                console.log("[Redux] LLM_OUTPUT event:", llmEvent);

                const content = llmEvent.payload?.text || llmEvent.message || "";
                console.log("[Redux] LLM_OUTPUT content:", content?.substring(0, 100));

                // Filter out diagnostic/system messages that should only appear in timeline
                // These are system notifications from the backend
                if (content && typeof content === 'string') {
                    if (content.startsWith('[Incomplete response:') || content.includes('Task budget at')) {
                        console.log("[Redux] Skipping diagnostic/system message (timeline only)");
                        return;
                    }
                }

                // Per backend guidance: synthesis outputs during streaming are intermediate
                // The final answer is fetched via API after WORKFLOW_COMPLETED
                // Only show simple-agent outputs directly (non-research tasks)
                const isSynthesisAgent = ["synthesis", "streaming_synthesis"].includes(event.agent_id || "");
                
                if (isSynthesisAgent) {
                    console.log("[Redux] Skipping synthesis LLM_OUTPUT - final answer will come from API fetch");
                    return;
                }
                
                if (content) {
                    const uniqueId = `${event.agent_id || 'assistant'}-${event.workflow_id}-${event.seq || Date.now()}`;
                    
                    // For simple-agent, remove generating placeholder
                    if (event.agent_id === "simple-agent") {
                        console.log("[Redux] 🎯 Simple-agent output received");
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
                    console.log("[Redux] Created new message from LLM_OUTPUT, total messages:", state.messages.length);
                } else {
                    console.warn("[Redux] LLM_OUTPUT event has no content");
                }
            } else if (event.type === "WORKFLOW_COMPLETED") {
                // Workflow completed - check if there's a final result to show
                const workflowEvent = event as any;
                console.log("[Redux] WORKFLOW_COMPLETED event:", workflowEvent);

                // Check if we already have an assistant message
                const hasAssistantMessage = state.messages.some(m => m.role === "assistant");
                console.log("[Redux] Has assistant message:", hasAssistantMessage);
                console.log("[Redux] Current messages:", state.messages.length);
                console.log("[Redux] Workflow event message:", workflowEvent.message);

                // If no assistant message and the event has a message/result, add it
                // Note: WORKFLOW_COMPLETED usually just has "All done", not the actual result
                // We should ONLY add substantive content, not status messages
                if (!hasAssistantMessage && workflowEvent.message && workflowEvent.message !== "All done") {
                    // Filter out diagnostic/system/status messages that shouldn't appear as conversation content
                    const message = workflowEvent.message;
                    if (message && typeof message === 'string') {
                        const lowerMessage = message.toLowerCase();
                        // Skip status-like messages (these should only appear in timeline, not conversation)
                        if (message.startsWith('[Incomplete response:') || 
                            message.includes('Task budget at') ||
                            lowerMessage.includes('task completed') ||
                            lowerMessage.includes('task done') ||
                            lowerMessage === 'done' ||
                            lowerMessage === 'completed' ||
                            lowerMessage === 'success' ||
                            lowerMessage.includes('successfully') && message.length < 100) { // Short success messages are status, not content
                            console.log("[Redux] Skipping status/system message in WORKFLOW_COMPLETED (timeline only):", message);
                            return;
                        }
                    }
                    
                    const uniqueId = `workflow-${event.workflow_id}-${Date.now()}`;
                    state.messages.push({
                        id: uniqueId,
                        role: "assistant",
                        content: workflowEvent.message,
                        timestamp: new Date().toLocaleTimeString(),
                        taskId: event.workflow_id,
                    });
                    console.log("[Redux] Added final result from WORKFLOW_COMPLETED");
                } else {
                    console.log("[Redux] Not adding message from WORKFLOW_COMPLETED - will rely on fallback fetch");
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

                // For historical events, only set state — messages are loaded from turn data in page.tsx
                // This prevents plan appearing before user query (events dispatch before turns load)
                if (isHistorical) {
                    console.log("[Redux] Historical RESEARCH_PLAN_READY - setting state only, skipping message");
                    return;
                }

                // Remove generating placeholder
                state.messages = state.messages.filter((m: any) =>
                    !(m.isGenerating && m.taskId === event.workflow_id)
                );

                // Add the research plan as a special assistant message
                if (planEvent.message) {
                    state.messages.push({
                        id: `research-plan-${event.workflow_id}-${Date.now()}`,
                        role: "assistant",
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

                // For historical events, only set state — messages are loaded from turn data in page.tsx
                if (isHistorical) {
                    console.log("[Redux] Historical RESEARCH_PLAN_APPROVED - setting state only");
                    return;
                }

                // Add approval confirmation message
                state.messages.push({
                    id: `plan-approved-${event.workflow_id}-${Date.now()}`,
                    role: "system",
                    content: "Plan approved. Research started.",
                    timestamp: new Date().toLocaleTimeString(),
                    taskId: event.workflow_id,
                });

                // Add generating placeholder for the actual research execution
                state.messages.push({
                    id: `generating-${event.workflow_id}`,
                    role: "assistant",
                    content: "Generating...",
                    timestamp: new Date().toLocaleTimeString(),
                    isGenerating: true,
                    taskId: event.workflow_id,
                });
            } else if (event.type === "REVIEW_USER_FEEDBACK") {
                // User feedback during HITL review — treat as a regular user chat message
                console.log("[Redux] Review user feedback for workflow:", event.workflow_id);
                if (isHistorical) {
                    console.log("[Redux] Historical REVIEW_USER_FEEDBACK - skipping (handled in page.tsx turn loading)");
                    return;
                }
                // Live: message is already added by handleReviewFeedback via addMessage.
                // The SSE event arrives after the HTTP response, so the message already exists.
                // addMessage deduplicates by ID, but the live path uses a different ID.
                // We skip here to avoid duplicates — live messages come from handleReviewFeedback.
                console.log("[Redux] Live REVIEW_USER_FEEDBACK - skipping (already added by handleReviewFeedback)");
            } else if (event.type === "RESEARCH_PLAN_UPDATED") {
                // Updated research plan from feedback — treat as assistant chat message
                console.log("[Redux] Research plan updated for workflow:", event.workflow_id);
                if (isHistorical) {
                    console.log("[Redux] Historical RESEARCH_PLAN_UPDATED - skipping (handled in page.tsx turn loading)");
                    return;
                }
                // Live: message is already added by handleReviewFeedback.
                console.log("[Redux] Live RESEARCH_PLAN_UPDATED - skipping (already added by handleReviewFeedback)");
            } else if (event.type === "AGENT_COMPLETED") {
                // AGENT_COMPLETED is just a status event, not a message
                // The actual response comes from thread.message.completed
                // Don't add this to messages - it's just "Task done" status
                console.log("[Redux] Agent completed, skipping message creation");
            } else if (event.type === "TOOL_INVOKED") {
                // Tool invocations are only shown in the timeline (via state.events), not in the conversation
                console.log("[Redux] Tool invoked, skipping message creation (timeline only)");
            } else if (event.type === "TOOL_OBSERVATION") {
                // Tool observations are only shown in the timeline (via state.events), not in the conversation
                console.log("[Redux] Tool observation, skipping message creation (timeline only)");
            }
        },
        resetRun: (state) => {
            // Preserve user preferences across session changes
            const preservedAgent = state.selectedAgent;
            const preservedStrategy = state.researchStrategy;
            const preservedReviewPlan = state.reviewPlan;

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
            // Review state reset
            state.reviewStatus = "none";
            state.reviewWorkflowId = null;
            state.reviewVersion = 0;
            state.reviewIntent = null;
            // Restore user preferences
            state.selectedAgent = preservedAgent;
            state.researchStrategy = preservedStrategy;
            state.reviewPlan = preservedReviewPlan;
        },
        addMessage: (state, action: PayloadAction<any>) => {
            console.log("[Redux] addMessage called:", action.payload);
            if (!state.messages.some(m => m.id === action.payload.id)) {
                state.messages.push(action.payload);
                console.log("[Redux] Message added to state");
            } else {
                console.warn("[Redux] Message with ID already exists:", action.payload.id);
            }
        },
        removeMessage: (state, action: PayloadAction<string>) => {
            state.messages = state.messages.filter(m => m.id !== action.payload);
        },
        updateMessageMetadata: (state, action: PayloadAction<{ taskId: string; metadata: any }>) => {
            const { taskId, metadata } = action.payload;
            console.log("[Redux] updateMessageMetadata called for task:", taskId);

            // Find the last assistant message for this task
            for (let i = state.messages.length - 1; i >= 0; i--) {
                const msg = state.messages[i];
                if (msg.role === "assistant" && msg.taskId === taskId) {
                    console.log("[Redux] Updating metadata for message:", msg.id, "Citations:", metadata?.citations?.length);
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
            console.log("[Redux] Main workflow ID set to:", action.payload);
        },
        setStatus: (state, action: PayloadAction<RunState["status"]>) => {
            state.status = action.payload;
            console.log("[Redux] Status manually set to:", action.payload);
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
            console.log("[Redux] Pause state set:", action.payload);
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
            console.log("[Redux] Cancelling state set to:", action.payload);
        },
        setReviewPlan: (state, action: PayloadAction<RunState["reviewPlan"]>) => {
            state.reviewPlan = action.payload;
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
            console.log("[Redux] Cancelled state set to:", action.payload);
        },
    },
});

export const { addEvent, resetRun, addMessage, removeMessage, updateMessageMetadata, setConnectionState, setStreamError, setSelectedAgent, setResearchStrategy, setMainWorkflowId, setStatus, setPaused, setCancelling, setCancelled, setReviewPlan, setReviewStatus, setReviewVersion, setReviewIntent } = runSlice.actions;
export default runSlice.reducer;
