package activities

import (
	"fmt"
	"strings"
)

// compactTokens formats a token count into a compact human form, e.g. 14800 -> "14.8k".
func compactTokens(tokens int) string {
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	// one decimal precision for thousands
	k := float64(tokens) / 1000.0
	// Trim trailing .0
	s := fmt.Sprintf("%.1fk", k)
	s = strings.ReplaceAll(s, ".0k", "k")
	return s
}

// msgTruncate shortens a string for display, adding ellipsis if needed.
func msgTruncate(s string, maxLen int) string {
	return truncateQuery(s, maxLen)
}

// -----------------------------------------------------------------------------
// Workflow Lifecycle Messages
// -----------------------------------------------------------------------------

// MsgWorkflowStarted announces workflow initialization.
func MsgWorkflowStarted() string { return "Starting task" }

// MsgWorkflowRouting indicates routing to a specific workflow type.
func MsgWorkflowRouting(workflowType, mode string) string {
	if mode != "" {
		return fmt.Sprintf("Using %s mode", mode)
	}
	return fmt.Sprintf("Using %s workflow", workflowType)
}

// MsgWorkflowCompleted announces successful workflow completion.
func MsgWorkflowCompleted() string { return "All done" }

// MsgStreamEnd marks the end of streaming.
func MsgStreamEnd() string { return "Stream complete" }

// -----------------------------------------------------------------------------
// Task Processing Messages
// -----------------------------------------------------------------------------

// MsgThinking shows the system is analyzing the query.
func MsgThinking(query string) string {
	truncated := msgTruncate(query, 50)
	return fmt.Sprintf("Thinking: %s", truncated)
}

// MsgProcessing indicates active processing.
func MsgProcessing() string { return "Processing query" }

// MsgTaskDone indicates task completion.
func MsgTaskDone() string { return "Task complete" }

// MsgTaskFailed reports task failure with reason.
func MsgTaskFailed(reason string) string {
	if reason == "" {
		return "Task failed"
	}
	// Truncate long error messages
	if len(reason) > 100 {
		reason = reason[:100] + "..."
	}
	return fmt.Sprintf("Task failed: %s", reason)
}

// -----------------------------------------------------------------------------
// Planning & Decomposition Messages
// -----------------------------------------------------------------------------

// MsgPlanCreated announces plan creation with step count.
func MsgPlanCreated(steps int) string {
	if steps == 1 {
		return "Created a plan with 1 step"
	}
	return fmt.Sprintf("Created a plan with %d steps", steps)
}

// MsgUnderstandingRequest indicates decomposition is in progress.
func MsgUnderstandingRequest() string { return "Understanding your request" }

// MsgDecompositionFailed indicates fallback to simple execution.
func MsgDecompositionFailed() string { return "Planning skipped, using direct execution" }

// MsgRoleAssigned announces role assignment with available tools.
func MsgRoleAssigned(role string, toolCount int) string {
	if toolCount > 0 {
		return fmt.Sprintf("Assigned %s role with %d tools", role, toolCount)
	}
	return fmt.Sprintf("Assigned %s role", role)
}

// -----------------------------------------------------------------------------
// Agent Messages
// -----------------------------------------------------------------------------

// MsgContextPreparing returns a short status for context preparation.
func MsgContextPreparing(_ int, _ int) string { return "Preparing context" }

// MsgAgentRunning announces an agent role starting work.
func MsgAgentRunning(role string) string {
	if role == "" {
		role = "Agent"
	}
	return fmt.Sprintf("%s agent running", strings.ToLower(role))
}

// MsgAgentThinking indicates agent is analyzing.
func MsgAgentThinking() string { return "Analyzing your question" }

// MsgAgentWorking indicates agent is actively working.
func MsgAgentWorking(agentName string) string {
	if agentName == "" {
		return "Agent working"
	}
	return fmt.Sprintf("%s working", agentName)
}

// -----------------------------------------------------------------------------
// Progress Messages
// -----------------------------------------------------------------------------

// MsgProgress reports step progress.
func MsgProgress(step, total int) string {
	if total <= 0 {
		total = 1
	}
	if step < 0 {
		step = 0
	}
	if step > total {
		step = total
	}
	return fmt.Sprintf("Step %d/%d completed", step, total)
}

// MsgBudget reports a simple budget usage.
func MsgBudget(used, limit int) string {
	return fmt.Sprintf("Budget used: %s/%s", compactTokens(used), compactTokens(limit))
}

// MsgTokensUsed reports token consumption.
func MsgTokensUsed(tokens int) string {
	return fmt.Sprintf("Used %s tokens", compactTokens(tokens))
}

// -----------------------------------------------------------------------------
// Handoff Messages
// -----------------------------------------------------------------------------

// MsgHandoffTemplate announces handoff to a template workflow.
func MsgHandoffTemplate(templateName string) string {
	return fmt.Sprintf("Using template: %s", templateName)
}

// MsgHandoffSimple announces handoff to simple task workflow.
func MsgHandoffSimple() string { return "Processing as simple task" }

// MsgHandoffSupervisor announces handoff to supervisor workflow.
func MsgHandoffSupervisor() string { return "Coordinating multiple agents" }

// MsgHandoffTeamPlan announces handoff to team plan workflow.
func MsgHandoffTeamPlan() string { return "Executing team plan" }

// -----------------------------------------------------------------------------
// Synthesis Messages
// -----------------------------------------------------------------------------

// MsgCompressionApplied notes that trimming was applied.
func MsgCompressionApplied() string { return "Shortened context to fit budget" }

// MsgCombiningResults announces synthesis.
func MsgCombiningResults() string { return "Analyzing gathered results" }

// MsgSynthesizing indicates synthesis in progress.
func MsgSynthesizing() string { return "Combining results" }

// MsgFinalAnswer announces the final answer is ready.
func MsgFinalAnswer() string { return "Answer ready" }

// MsgSynthesisSummary reports synthesis completion with token count.
func MsgSynthesisSummary(tokens int) string {
	return fmt.Sprintf("Synthesized using %s tokens", compactTokens(tokens))
}

// -----------------------------------------------------------------------------
// Memory Messages
// -----------------------------------------------------------------------------

// MsgWaiting reports a generic waiting status.
func MsgWaiting(_ string) string { return "Waiting for previous step" }

// MsgMemoryRecalled reports number of memory items recalled.
func MsgMemoryRecalled(items int) string { return fmt.Sprintf("Found %d past notes", items) }

// MsgSummaryAdded indicates a previous context summary was injected.
func MsgSummaryAdded() string { return "Previous context summary added" }

// -----------------------------------------------------------------------------
// Approval Messages
// -----------------------------------------------------------------------------

// MsgApprovalRequested indicates approval is needed.
func MsgApprovalRequested(reason, approvalID string) string {
	if reason != "" {
		return fmt.Sprintf("Approval needed: %s", reason)
	}
	return "Approval needed"
}

// MsgApprovalProcessed reports approval decision.
func MsgApprovalProcessed(decision string) string {
	switch strings.ToLower(decision) {
	case "approved":
		return "Approval granted, continuing"
	case "denied", "rejected":
		return "Approval denied"
	default:
		return fmt.Sprintf("Approval %s", decision)
	}
}

// -----------------------------------------------------------------------------
// Supervisor Messages
// -----------------------------------------------------------------------------

// MsgSupervisorStarted announces supervisor workflow start.
func MsgSupervisorStarted() string { return "Supervisor coordinating agents" }

// MsgSubtaskProgress reports subtask completion progress.
func MsgSubtaskProgress(completed, total int) string {
	return fmt.Sprintf("Completed %d of %d subtasks", completed, total)
}

// MsgSubtasksFailed reports too many subtask failures.
func MsgSubtasksFailed(failed, total int) string {
	return fmt.Sprintf("Too many subtasks failed (%d/%d)", failed, total)
}

// -----------------------------------------------------------------------------
// React Pattern Messages
// -----------------------------------------------------------------------------

// MsgReactIteration reports React loop progress.
func MsgReactIteration(current, max int) string {
	return fmt.Sprintf("Reasoning step %d of %d", current, max)
}

// MsgReactReasoning indicates reasoning phase started.
func MsgReactReasoning() string { return "Analyzing the progress" }

// MsgReactReasoningDone indicates reasoning phase completed.
func MsgReactReasoningDone() string { return "Decided on next step" }

// MsgReactActing indicates action phase started.
func MsgReactActing() string { return "Taking action" }

// MsgReactActingDone indicates action phase completed.
func MsgReactActingDone() string { return "Action completed" }

// MsgReactLoopDone indicates the React loop finished.
func MsgReactLoopDone() string { return "Finished reasoning loop" }

// -----------------------------------------------------------------------------
// Tool Observation Messages
// -----------------------------------------------------------------------------

// MsgToolCompleted generates a human-readable message for tool completion.
func MsgToolCompleted(toolName string, response string) string {
	if response == "" {
		return fmt.Sprintf("%s completed", humanizeToolName(toolName))
	}

	// Extract first meaningful line or sentence
	summary := extractSummary(response, 80)
	if summary == "" {
		return fmt.Sprintf("%s completed", humanizeToolName(toolName))
	}

	return fmt.Sprintf("%s: %s", humanizeToolName(toolName), summary)
}

// MsgToolFailed generates a human-readable message for tool failure.
func MsgToolFailed(toolName string) string {
	return fmt.Sprintf("%s failed", humanizeToolName(toolName))
}

// humanizeToolName converts tool_name to "Tool Name" format.
func humanizeToolName(toolName string) string {
	switch toolName {
	case "web_search":
		return "Search"
	case "web_fetch":
		return "Fetch"
	case "calculator":
		return "Calculator"
	case "python_code", "python_executor", "code_executor":
		return "Code"
	case "read_file", "file_reader":
		return "File read"
	case "code_reader":
		return "Code review"
	default:
		return strings.Title(strings.ReplaceAll(toolName, "_", " "))
	}
}

// extractSummary extracts a brief summary from response text.
func extractSummary(text string, maxLen int) string {
	// Clean up the text
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Try to get first line that's not empty or a header
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines, headers, and JSON-like content
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "{") {
			continue
		}
		// Found a good line
		return truncateQuery(line, maxLen)
	}

	// Fallback: truncate the whole text
	return truncateQuery(text, maxLen)
}
