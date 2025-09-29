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

// MsgContextPreparing returns a short status for context preparation.
func MsgContextPreparing(msgs int, estTokens int) string {
    return fmt.Sprintf("Preparing context (%d msgs, ~%s tokens)", msgs, compactTokens(estTokens))
}

// MsgAgentRunning announces an agent role starting work.
func MsgAgentRunning(role string) string {
    if role == "" {
        role = "Agent"
    }
    // Keep role lowercase for simplicity (e.g., "research agent running")
    return fmt.Sprintf("%s agent running", strings.ToLower(role))
}

// MsgProgress reports step progress.
func MsgProgress(step, total int) string {
    if total <= 0 { total = 1 }
    if step < 0 { step = 0 }
    if step > total { step = total }
    return fmt.Sprintf("Step %d/%d completed", step, total)
}

// MsgBudget reports a simple budget usage.
func MsgBudget(used, limit int) string {
    return fmt.Sprintf("Budget used: %s/%s", compactTokens(used), compactTokens(limit))
}

// MsgCompressionApplied notes that trimming was applied.
func MsgCompressionApplied() string { return "Context trimmed to stay within budget" }

// MsgCombiningResults announces synthesis.
func MsgCombiningResults() string { return "Combining results" }

// MsgWaiting reports a generic waiting status.
func MsgWaiting(_ string) string { return "Waiting for previous step" }

// MsgMemoryRecalled reports number of memory items recalled.
func MsgMemoryRecalled(items int) string { return fmt.Sprintf("Memory recalled (%d items)", items) }

// MsgSummaryAdded indicates a previous context summary was injected.
func MsgSummaryAdded() string { return "Previous context summary added" }
