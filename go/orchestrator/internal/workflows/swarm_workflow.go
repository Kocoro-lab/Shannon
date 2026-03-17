package workflows

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/agents"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
)

// isTransientError classifies tool errors as transient (rate limit, timeout) vs permanent.
// Transient errors warrant backoff and retry; permanent errors count toward abort threshold.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	transientPatterns := []string{"rate limit", "429", "timeout", "timed out", "temporary", "unavailable", "503", "502"}
	for _, p := range transientPatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// truncateDecisionSummary caps the decision_summary to 12000 chars.
func truncateDecisionSummary(s string) string {
	if len(s) > 12000 {
		return s[:12000]
	}
	return s
}

// taskHasUnmetDeps checks if a task's depends_on constraints are all satisfied.
// Returns true if ANY dependency is not yet "completed" in the task list.
func taskHasUnmetDeps(taskID string, taskList []activities.SwarmTask) bool {
	for _, t := range taskList {
		if t.ID == taskID && len(t.DependsOn) > 0 {
			for _, dep := range t.DependsOn {
				depComplete := false
				for _, other := range taskList {
					if other.ID == dep && other.Status == "completed" {
						depComplete = true
						break
					}
				}
				if !depComplete {
					return true
				}
			}
		}
	}
	return false
}

// hasAssignablePendingTask returns true if any pending task has all dependencies met.
// Used by checkpoint skip to avoid waking Lead when only blocked tasks remain.
func hasAssignablePendingTask(taskList []activities.SwarmTask) bool {
	for _, t := range taskList {
		if t.Status == "pending" && !taskHasUnmetDeps(t.ID, taskList) {
			return true
		}
	}
	return false
}

// Only filters specific tool output patterns (file_write blobs, raw action JSON),
// not arbitrary JSON which may contain valid structured results.
func looksLikeToolJSON(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) == 0 {
		return false
	}
	if trimmed[0] != '{' {
		return false
	}
	// file_write raw output: {"path": "...", "content": "..."} — large blobs
	if strings.Contains(trimmed, `"path"`) && strings.Contains(trimmed, `"content"`) && len(trimmed) > 500 {
		return true
	}
	// Raw action JSON leaked through: {"action": "tool_call", "tool": "..."}
	if strings.Contains(trimmed, `"action"`) && strings.Contains(trimmed, `"tool"`) && strings.Contains(trimmed, `"tool_params"`) {
		return true
	}
	return false
}

// jaccardSimilarity computes word-level Jaccard similarity between two strings.
// Used by search saturation detection (B3).
func jaccardSimilarity(a, b string) float64 {
	wordsA := toWordSet(strings.ToLower(a))
	wordsB := toWordSet(strings.ToLower(b))
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	intersection := 0
	union := make(map[string]bool)
	for w := range wordsA {
		union[w] = true
		if wordsB[w] {
			intersection++
		}
	}
	for w := range wordsB {
		union[w] = true
	}
	if len(union) == 0 {
		return 0
	}
	return float64(intersection) / float64(len(union))
}

func toWordSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		if len(w) > 1 { // Skip single-char noise
			set[w] = true
		}
	}
	return set
}

// isSearchSaturated checks if the last N queries in a window are too similar.
// Returns true if most pairwise comparisons within the window exceed minSimilarity.
func isSearchSaturated(queries []string, window int, minSimilarity float64) bool {
	if len(queries) < window {
		return false
	}
	recent := queries[len(queries)-window:]
	similarCount := 0
	totalPairs := 0
	for i := 0; i < len(recent); i++ {
		for j := i + 1; j < len(recent); j++ {
			totalPairs++
			if jaccardSimilarity(recent[i], recent[j]) >= minSimilarity {
				similarCount++
			}
		}
	}
	// Need most pairs to be similar (allow 1 outlier)
	return totalPairs > 0 && similarCount >= totalPairs-1
}

// buildToolsUsedSummary creates a compact one-line summary of tool calls from history.
// Example output: "web_search(x3), file_read(x2), file_write(x1)"
// This is included in idle signals so the parent can inject it on reassignment,
// preventing agents from repeating the same searches/reads.
func buildToolsUsedSummary(history []activities.AgentLoopTurn) string {
	counts := make(map[string]int)
	for _, turn := range history {
		action := turn.Action
		if action == "" || action == "idle" || action == "done_converted_to_idle" {
			continue
		}
		counts[action]++
	}
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for action, count := range counts {
		parts = append(parts, fmt.Sprintf("%s(x%d)", action, count))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// toFloat64 safely converts interface{} to float64 (JSON numbers are float64).
func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// convertHistoryForLead converts session conversation history into a concise format for Lead.
// Keeps last few exchanges, truncates long assistant responses to avoid bloating Lead's context.
func convertHistoryForLead(messages []Message) []map[string]interface{} {
	const maxMessages = 6   // Last 3 turns (user + assistant)
	const maxAssistantLen = 800

	start := 0
	if len(messages) > maxMessages {
		start = len(messages) - maxMessages
	}

	result := make([]map[string]interface{}, 0, len(messages)-start)
	for _, msg := range messages[start:] {
		content := msg.Content
		if msg.Role == "assistant" && len(content) > maxAssistantLen {
			content = content[:maxAssistantLen] + "\n... [truncated — use file_read to check workspace for full details]"
		}
		result = append(result, map[string]interface{}{
			"role":    msg.Role,
			"content": content,
		})
	}
	return result
}

// ── fetchTracker ────────────────────────────────────────────────────────────
// Consolidates L1 fetch cache, L2 team knowledge, and L3 search overlap
// into a single structure, eliminating duplicated check/write logic.

const fetchTrackerMaxEntries = 200 // cap to prevent unbounded memory growth

type fetchTracker struct {
	cache      map[string]string               // L1: normalized URL → full response
	discovered map[string]string               // L3: normalized URL → discovering agent
	knowledge  []activities.TeamKnowledgeEntry // L2: metadata for cross-agent sharing
}

func newFetchTracker(seed []activities.TeamKnowledgeEntry) *fetchTracker {
	ft := &fetchTracker{
		cache:      make(map[string]string),
		discovered: make(map[string]string),
		knowledge:  make([]activities.TeamKnowledgeEntry, 0, len(seed)),
	}
	if len(seed) > 0 {
		ft.knowledge = append(ft.knowledge, seed...)
		for _, tk := range seed {
			if norm := normalizeURL(tk.URL); norm != "" {
				ft.discovered[norm] = tk.Agent
			}
		}
	}
	return ft
}

// checkCache returns cached content if ALL requested URLs are cached (L1).
// Returns ("", false) when ft is nil, tool is not a fetch, or any URL is uncached.
func (ft *fetchTracker) checkCache(tool string, params map[string]interface{}) (string, bool) {
	if ft == nil || (tool != "web_fetch" && tool != "web_subpage_fetch") {
		return "", false
	}
	urls := extractURLsFromParams(params)
	if len(urls) == 0 {
		return "", false
	}
	for _, rawURL := range urls {
		if _, ok := ft.cache[normalizeURL(rawURL)]; !ok {
			return "", false
		}
	}
	var b strings.Builder
	for i, rawURL := range urls {
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(ft.cache[normalizeURL(rawURL)])
	}
	return b.String(), true
}

// writeCache stores fetch results and updates knowledge + discovered (L1 write).
// Returns URLs that were newly added (for logging).
func (ft *fetchTracker) writeCache(tool string, params map[string]interface{}, response interface{}, agentID string) []string {
	if ft == nil || (tool != "web_fetch" && tool != "web_subpage_fetch") {
		return nil
	}
	urls := extractURLsFromParams(params)
	respStr := fmt.Sprintf("%v", response)
	var added []string
	for _, rawURL := range urls {
		norm := normalizeURL(rawURL)
		if _, exists := ft.cache[norm]; exists {
			continue
		}
		if len(ft.cache) >= fetchTrackerMaxEntries {
			break
		}
		ft.cache[norm] = respStr
		ft.discovered[norm] = agentID
		ft.knowledge = append(ft.knowledge, activities.TeamKnowledgeEntry{
			URL:       rawURL,
			Agent:     agentID,
			Summary:   truncateToSentence(respStr, 200),
			CharCount: len(respStr),
		})
		added = append(added, rawURL)
	}
	return added
}

// checkSearchOverlap registers new URLs and returns overlap stats (L3).
func (ft *fetchTracker) checkSearchOverlap(result interface{}, agentID string) (overlapPct float64, known, total int) {
	if ft == nil || result == nil {
		return 0, 0, 0
	}
	searchURLs := extractURLsFromSearchResults(fmt.Sprintf("%v", result))
	newCount := 0
	for _, su := range searchURLs {
		norm := normalizeURL(su)
		if _, exists := ft.discovered[norm]; exists {
			known++
		} else {
			newCount++
			ft.discovered[norm] = agentID
		}
	}
	total = known + newCount
	if total > 0 && known > 0 {
		overlapPct = float64(known) / float64(total) * 100
	}
	return overlapPct, known, total
}

// Knowledge returns the accumulated team knowledge entries (nil-safe).
func (ft *fetchTracker) Knowledge() []activities.TeamKnowledgeEntry {
	if ft == nil {
		return nil
	}
	return ft.knowledge
}

// mergeTeamKnowledge deduplicates and appends new entries into the global list.
func mergeTeamKnowledge(global []activities.TeamKnowledgeEntry, incoming []activities.TeamKnowledgeEntry) []activities.TeamKnowledgeEntry {
	seen := make(map[string]bool, len(global))
	for _, e := range global {
		seen[normalizeURL(e.URL)] = true
	}
	for _, e := range incoming {
		norm := normalizeURL(e.URL)
		if norm != "" && !seen[norm] {
			seen[norm] = true
			global = append(global, e)
		}
	}
	return global
}

// normalizeURL strips fragments and trailing slashes, lowercases scheme+host,
// preserves path case and query parameters.
// NOTE: Related but distinct from metadata.NormalizeURL (which also strips utm/www).
func normalizeURL(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return raw
	}
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		path = "/"
	}
	base := strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + path
	if u.RawQuery != "" {
		return base + "?" + u.RawQuery
	}
	return base
}

// truncateToSentence cuts text at sentence boundary or maxLen.
func truncateToSentence(text string, maxLen int) string {
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	cut := string(runes[:maxLen])
	if idx := strings.LastIndex(cut, ". "); idx > maxLen/2 {
		return cut[:idx+1]
	}
	return cut + "..."
}

// extractURLsFromParams extracts URLs from tool_call params (handles both "url" string and "urls" array).
func extractURLsFromParams(params map[string]interface{}) []string {
	var urls []string
	if u, ok := params["url"].(string); ok && u != "" {
		urls = append(urls, u)
	}
	if arr, ok := params["urls"].([]interface{}); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok && s != "" {
				urls = append(urls, s)
			}
		}
	}
	return urls
}

// extractURLsFromSearchResults extracts URLs from search result text using regex.
func extractURLsFromSearchResults(result string) []string {
	re := regexp.MustCompile(`https?://[^\s"'\]>]+`)
	matches := re.FindAllString(result, -1)
	seen := make(map[string]bool)
	var urls []string
	for _, m := range matches {
		m = strings.TrimRight(m, ".,;:!?)")
		if !seen[m] {
			seen[m] = true
			urls = append(urls, m)
		}
	}
	return urls
}

// ── AgentLoop ──────────────────────────────────────────────────────────────────

// TeamMember describes a teammate visible to each agent for collaboration.
type TeamMember struct {
	AgentID string `json:"agent_id"`
	Task    string `json:"task"`
	Role    string `json:"role,omitempty"`
}

// AgentLoopInput is the input for a persistent agent loop.
type AgentLoopInput struct {
	AgentID               string                 `json:"agent_id"`
	WorkflowID            string                 `json:"workflow_id"`
	Task                  string                 `json:"task"`
	MaxIterations         int                    `json:"max_iterations"`
	SessionID             string                 `json:"session_id,omitempty"`
	UserID                string                 `json:"user_id,omitempty"`
	Context               map[string]interface{} `json:"context,omitempty"`
	TeamRoster            []TeamMember           `json:"team_roster,omitempty"`
	WorkspaceMaxEntries   int                    `json:"workspace_max_entries,omitempty"`
	WorkspaceSnippetChars int                    `json:"workspace_snippet_chars,omitempty"`
	Role                  string                 `json:"role,omitempty"` // Persona role: researcher, coder, analyst, generalist
	ReassignCount         int                              `json:"reassign_count,omitempty"` // B1: tracks reassignment count per agent
	OriginalQuery         string                           `json:"original_query,omitempty"` // User's original query for agent context
	TeamKnowledge         []activities.TeamKnowledgeEntry  `json:"team_knowledge,omitempty"` // Cross-agent fetch knowledge (L2 dedup)
}

// AgentLoopResult is the final result from a persistent agent.
type AgentLoopResult struct {
	AgentID             string `json:"agent_id"`
	Role                string `json:"role,omitempty"`
	Response            string `json:"response"`
	Iterations          int    `json:"iterations"`
	TokensUsed          int    `json:"tokens_used"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
	ModelUsed           string `json:"model_used"`
	Provider            string `json:"provider"`
	Success             bool                             `json:"success"`
	Error               string                           `json:"error,omitempty"`
	ToolsUsed           string                           `json:"tools_used,omitempty"` // Compact summary: "web_search(x3), file_read(x2)"
	TeamKnowledge       []activities.TeamKnowledgeEntry  `json:"team_knowledge,omitempty"` // URLs fetched by this agent (for parent merge)
}



// AgentLoop is a persistent agent workflow that runs a reason-act cycle.
// Each iteration: check mailbox → call LLM → execute action → loop.
func AgentLoop(ctx workflow.Context, input AgentLoopInput) (AgentLoopResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("AgentLoop started",
		"agent_id", input.AgentID,
		"task", input.Task,
		"max_iterations", input.MaxIterations,
	)

	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute, // 90s too short for synthesis_writer generating long reports
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	// Short timeout for P2P activities
	p2pCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// Version gate for P2P activities (RegisterFile, SendAgentMessage, WorkspaceAppend).
	// Old workflows in replay must not execute these new activity calls.
	p2pV := workflow.GetVersion(ctx, "agent_loop_p2p_v1", workflow.DefaultVersion, 1)

	// Emit agent started event
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: input.WorkflowID,
		EventType:  activities.StreamEventAgentStarted,
		AgentID:    input.AgentID,
		Message:    activities.MsgAgentStarted(input.AgentID),
		Timestamp:  workflow.Now(ctx),
		Payload:    map[string]interface{}{"role": input.Role},
	}).Get(ctx, nil)

	// Convert team roster to activity-level type
	var teamRoster []activities.TeamMemberInfo
	for _, tm := range input.TeamRoster {
		teamRoster = append(teamRoster, activities.TeamMemberInfo{
			AgentID: tm.AgentID,
			Task:    tm.Task,
			Role:    tm.Role,
		})
	}

	// Apply guardrail defaults if not set
	wsMaxEntries := input.WorkspaceMaxEntries
	if wsMaxEntries <= 0 {
		wsMaxEntries = 5
	}
	wsSnippetChars := input.WorkspaceSnippetChars
	if wsSnippetChars <= 0 {
		wsSnippetChars = 800
	}

	modelTier := deriveModelTier(input.Context)

	var history []activities.AgentLoopTurn
	var totalTokens, totalInput, totalOutput int
	var totalCacheRead, totalCacheCreation int
	var lastModel, lastProvider string
	var savedDoneResponse string // Preserved when "done" is converted to "idle"
	var lastWorkspaceSeq uint64
	var lastMailboxSeq uint64
	var filesWritten []string // Paths written by file_write during current task
	var consecutiveToolErrors int   // Track consecutive tool failures to prevent infinite loops
	var consecutiveNonToolActions int    // Track iterations without tool calls for convergence detection
	var consecutiveMsgWithoutWrite int // Track send_message/publish_data without file_write
	var consecutiveTransientErrors int // Track transient errors for escalating backoff
	var stepResult activities.AgentLoopStepResult // Declared outside loop so shutdown check can reference previous iteration's result
	var lastResponseID string                     // OpenAI Responses API: chain from previous response for cache reuse
	var pendingSystemMessage string               // Urgent directive for next iteration (rendered at prompt end for recency bias)
	var runningNotes string                       // Agent's cumulative notes — survives history truncation
	var recentSearchQueries []string              // B3: Track web_search queries for saturation detection
	var searchesSinceLastFetch int                // B4: Detect consecutive searches without fetch
	var cumulativeToolCalls int                    // Total tool_calls across all tasks — survives reassignment

	// Team knowledge dedup: L1 fetch cache, L2 cross-agent knowledge, L3 search overlap.
	teamKnowledgeVersion := workflow.GetVersion(ctx, "team-knowledge-dedup", workflow.DefaultVersion, 1)
	var ft *fetchTracker
	if teamKnowledgeVersion >= 1 {
		ft = newFetchTracker(input.TeamKnowledge)
	}

	for iteration := 0; iteration < input.MaxIterations; iteration++ {
		// Non-blocking shutdown check: catches signals sent while the agent
		// was blocked in a tool_call activity on the previous iteration.
		shutdownCh := workflow.GetSignalChannel(ctx, fmt.Sprintf("agent:%s:shutdown", input.AgentID))
		shutdownSel := workflow.NewSelector(ctx)
		shouldShutdown := false
		shutdownSel.AddReceive(shutdownCh, func(ch workflow.ReceiveChannel, more bool) {
			var msg string
			ch.Receive(ctx, &msg)
			shouldShutdown = true
		})
		shutdownSel.AddDefault(func() {}) // no signal → continue
		shutdownSel.Select(ctx)
		if shouldShutdown {
			logger.Info("Agent shutdown during iteration", "agent_id", input.AgentID, "iteration", iteration)
			shutdownResponse := savedDoneResponse
			if shutdownResponse == "" && stepResult.DecisionSummary != "" {
				shutdownResponse = stepResult.DecisionSummary
			}
			if shutdownResponse == "" {
				shutdownResponse = fmt.Sprintf("Agent %s shutdown after %d iterations", input.AgentID, iteration)
			}
			return AgentLoopResult{
				AgentID:      input.AgentID,
				Role:         input.Role,
				Response:     shutdownResponse,
				Iterations:   iteration,
				TokensUsed:          totalTokens,
				InputTokens:         totalInput,
				OutputTokens:        totalOutput,
				CacheReadTokens:     totalCacheRead,
				CacheCreationTokens: totalCacheCreation,
				ModelUsed:           lastModel,
				Provider:            lastProvider,
				Success:             true,
				TeamKnowledge:       ft.Knowledge(),
			}, nil
		}

		logger.Info("AgentLoop iteration", "agent_id", input.AgentID, "iteration", iteration)

		// Step 1: Check mailbox for incoming messages
		var mailboxMsgs []activities.AgentMailboxMsg
		var fetchedMsgs []activities.AgentMessage
		fetchErr := workflow.ExecuteActivity(p2pCtx, constants.FetchAgentMessagesActivity, activities.FetchAgentMessagesInput{
			WorkflowID: input.WorkflowID,
			AgentID:    input.AgentID,
			SinceSeq:   lastMailboxSeq,
		}).Get(ctx, &fetchedMsgs)
		if fetchErr != nil {
			logger.Warn("AgentLoop mailbox fetch failed", "agent_id", input.AgentID, "error", fetchErr)
		}
		if fetchErr == nil && len(fetchedMsgs) > 0 {
			for _, m := range fetchedMsgs {
				mailboxMsgs = append(mailboxMsgs, activities.AgentMailboxMsg{
					From:    m.From,
					Type:    string(m.Type),
					Payload: m.Payload,
				})
				if m.Seq > lastMailboxSeq {
					lastMailboxSeq = m.Seq
				}
			}
			logger.Info("AgentLoop received mailbox messages",
				"agent_id", input.AgentID,
				"count", len(mailboxMsgs),
			)
		}

		// Step 1b: Fetch shared workspace entries from ALL topics (findings from other agents)
		var wsSnippets []activities.WorkspaceSnippet
		var wsEntries []activities.WorkspaceEntry
		wsErr := workflow.ExecuteActivity(p2pCtx, constants.WorkspaceListAllActivity, activities.WorkspaceListAllInput{
			WorkflowID: input.WorkflowID,
			SinceSeq:   lastWorkspaceSeq,
			MaxEntries: wsMaxEntries,
		}).Get(ctx, &wsEntries)
		if wsErr != nil {
			logger.Warn("AgentLoop workspace fetch failed", "agent_id", input.AgentID, "error", wsErr)
		}
		if wsErr == nil && len(wsEntries) > 0 {
			for _, e := range wsEntries {
				author, _ := e.Entry["author"].(string)
				data, _ := e.Entry["data"].(string)
				// Truncate to control token usage (rune-safe to avoid splitting UTF-8)
				if runeData := []rune(data); len(runeData) > wsSnippetChars {
					data = string(runeData[:wsSnippetChars]) + "..."
				}
				wsSnippets = append(wsSnippets, activities.WorkspaceSnippet{
					Author: author,
					Data:   data,
					Seq:    e.Seq,
				})
				if e.Seq > lastWorkspaceSeq {
					lastWorkspaceSeq = e.Seq
				}
			}
			logger.Info("AgentLoop fetched workspace entries",
				"agent_id", input.AgentID,
				"count", len(wsSnippets),
			)
		}

		// Step 1c: Fetch TaskList for prompt injection
		var currentTaskList []activities.SwarmTask
		_ = workflow.ExecuteActivity(p2pCtx, constants.GetTaskListActivity, activities.GetTaskListInput{
			WorkflowID: input.WorkflowID,
		}).Get(ctx, &currentTaskList)

		// Step 2: Call LLM to decide next action
		stepResult = activities.AgentLoopStepResult{} // Reset for this iteration
		if err := workflow.ExecuteActivity(ctx, constants.AgentLoopStepActivity, activities.AgentLoopStepInput{
			AgentID:            input.AgentID,
			WorkflowID:         input.WorkflowID,
			Task:               input.Task,
			Iteration:          iteration,
			MaxIterations:      input.MaxIterations,
			Messages:           mailboxMsgs,
			History:            history,
			Context:            input.Context,
			SessionID:          input.SessionID,
			TeamRoster:         teamRoster,
			WorkspaceData:      wsSnippets,
			SuggestedTools:     []string{"web_search", "web_fetch", "web_subpage_fetch", "web_crawl", "python_executor", "calculator", "file_read", "file_write", "file_edit", "file_delete", "file_list", "file_search", "diff_files", "json_query"},
			RoleDescription:    input.Task,
			Role:               input.Role,
			TaskList:           currentTaskList,
			ModelTier:          modelTier,
			PreviousResponseID: lastResponseID,
			SystemMessage:      pendingSystemMessage,
			RunningNotes:       runningNotes,
			IsSwarm:            true,
			CumulativeToolCalls: cumulativeToolCalls,
			TeamKnowledge:      ft.Knowledge(),
			OriginalQuery:      input.OriginalQuery,
		}).Get(ctx, &stepResult); err != nil {
			logger.Error("AgentLoop LLM step failed", "agent_id", input.AgentID, "error", err)
			// Build fallback response from accumulated state
			fallbackResponse := savedDoneResponse
			if fallbackResponse == "" && len(history) > 0 {
				for i := len(history) - 1; i >= 0; i-- {
					if history[i].DecisionSummary != "" {
						fallbackResponse = history[i].DecisionSummary
						break
					}
				}
			}
			return AgentLoopResult{
				AgentID:             input.AgentID,
				Role:                input.Role,
				Response:            fallbackResponse,
				Success:             false,
				Error:               fmt.Sprintf("LLM step failed at iteration %d: %v", iteration, err),
				Iterations:          iteration,
				TokensUsed:          totalTokens,
				InputTokens:         totalInput,
				OutputTokens:        totalOutput,
				CacheReadTokens:     totalCacheRead,
				CacheCreationTokens: totalCacheCreation,
				ModelUsed:           lastModel,
				Provider:            lastProvider,
				TeamKnowledge:       ft.Knowledge(),
			}, nil
		}

		pendingSystemMessage = "" // Clear after consumption

		// Track token usage
		totalTokens += stepResult.TokensUsed
		totalInput += stepResult.InputTokens
		totalOutput += stepResult.OutputTokens
		totalCacheRead += stepResult.CacheReadTokens
		totalCacheCreation += stepResult.CacheCreationTokens
		if stepResult.ModelUsed != "" {
			lastModel = stepResult.ModelUsed
		}
		if stepResult.Provider != "" {
			lastProvider = stepResult.Provider
		}

		// Record per-step LLM token usage (agent thinking)
		if stepResult.InputTokens > 0 || stepResult.OutputTokens > 0 {
			recCtx := opts.WithTokenRecordOptions(ctx)
			provider := stepResult.Provider
			if provider == "" {
				provider = detectProviderFromModel(stepResult.ModelUsed)
			}
			_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
				UserID:              input.UserID,
				SessionID:           input.SessionID,
				TaskID:              input.WorkflowID,
				AgentID:             input.AgentID,
				Model:               stepResult.ModelUsed,
				Provider:            provider,
				InputTokens:         stepResult.InputTokens,
				OutputTokens:        stepResult.OutputTokens,
				CacheReadTokens:     stepResult.CacheReadTokens,
				CacheCreationTokens: stepResult.CacheCreationTokens,
				Metadata:            map[string]interface{}{"workflow": "swarm", "phase": "agent_step", "iteration": iteration},
			}).Get(ctx, nil)
		}
		if stepResult.ResponseID != "" {
			lastResponseID = stepResult.ResponseID
		}
		if stepResult.Notes != "" {
			runningNotes = stepResult.Notes
		}

		// Post-LLM shutdown check: catches signals that arrived during the
		// AgentLoopStep activity (which can block for 30-90s on LLM calls).
		// Only intercept if the LLM chose a non-tool action (idle/done).
		// If the LLM returned a tool_call, let the tool execute first —
		// shutting down mid-tool loses work (e.g., file_write never executes).
		if stepResult.Action == "idle" || stepResult.Action == "done" {
			postLLMShutdownCh := workflow.GetSignalChannel(ctx, fmt.Sprintf("agent:%s:shutdown", input.AgentID))
			postLLMSel := workflow.NewSelector(ctx)
			postLLMShutdown := false
			postLLMSel.AddReceive(postLLMShutdownCh, func(ch workflow.ReceiveChannel, more bool) {
				var msg string
				ch.Receive(ctx, &msg)
				postLLMShutdown = true
			})
			postLLMSel.AddDefault(func() {})
			postLLMSel.Select(ctx)
			if postLLMShutdown {
				logger.Info("Agent shutdown after LLM step", "agent_id", input.AgentID, "iteration", iteration)
				shutdownResponse := savedDoneResponse
				if shutdownResponse == "" && stepResult.DecisionSummary != "" {
					shutdownResponse = stepResult.DecisionSummary
				}
				if shutdownResponse == "" && stepResult.Response != "" {
					shutdownResponse = stepResult.Response
				}
				if shutdownResponse == "" {
					shutdownResponse = fmt.Sprintf("Agent %s shutdown after %d iterations", input.AgentID, iteration)
				}
				return AgentLoopResult{
					AgentID:      input.AgentID,
					Role:         input.Role,
					Response:     shutdownResponse,
					Iterations:   iteration + 1,
					TokensUsed:          totalTokens,
					InputTokens:         totalInput,
					OutputTokens:        totalOutput,
					CacheReadTokens:     totalCacheRead,
					CacheCreationTokens: totalCacheCreation,
					ModelUsed:           lastModel,
					Provider:     lastProvider,
					Success:      true,
					TeamKnowledge: ft.Knowledge(),
				}, nil
			}
		}

		// Force done on last iteration if LLM didn't choose it
		if iteration == input.MaxIterations-1 && stepResult.Action != "done" {
			logger.Warn("Forcing done on last iteration",
				"agent_id", input.AgentID,
				"original_action", stepResult.Action,
			)
			// Build summary from last 3 iterations only (avoid blowing synthesis context)
			var histSummary string
			startIdx := len(history) - 3
			if startIdx < 0 {
				startIdx = 0
			}
			for _, h := range history[startIdx:] {
				s := fmt.Sprintf("%v", h.Result)
				if s == "" || s == "<nil>" {
					continue
				}
				if len(s) > 2000 {
					s = s[:2000] + "..."
				}
				histSummary += fmt.Sprintf("[Iteration %d - %s]: %s\n", h.Iteration, h.Action, s)
			}
			if histSummary == "" {
				histSummary = fmt.Sprintf("Agent %s reached iteration limit. Last action was: %s", input.AgentID, stepResult.Action)
			} else {
				histSummary = fmt.Sprintf("Agent %s reached iteration limit (%d iterations). Partial findings from last 3 iterations:\n%s",
					input.AgentID, input.MaxIterations, histSummary)
			}
			stepResult.Action = "done"
			stepResult.Response = histSummary
		}

		// Enforce Lead-controlled lifecycle: in swarm mode (has team roster),
		// agents cannot self-exit with "done". Convert "done" → "idle".
		// Non-swarm agents (no team roster) can exit normally.
		if stepResult.Action == "done" && len(input.TeamRoster) > 0 {
			logger.Info("Agent tried done — converting to idle (Lead controls lifecycle)",
				"agent_id", input.AgentID,
				"iteration", iteration,
			)

			// Preserve the agent's response so it's not lost when idle times out.
			// Guard: LLMs sometimes stuff a raw tool_call JSON into the response
			// field (e.g. the entire file_write action). Detect and fall back to
			// DecisionSummary which is always a clean human-readable summary.
			if stepResult.Response != "" {
				resp := strings.TrimSpace(stepResult.Response)
				if strings.HasPrefix(resp, "{") && strings.Contains(resp, `"action"`) && strings.Contains(resp, `"tool"`) {
					if stepResult.DecisionSummary != "" {
						savedDoneResponse = stepResult.DecisionSummary
					}
				} else {
					savedDoneResponse = stepResult.Response
				}
			}
			history = append(history, activities.AgentLoopTurn{
				Iteration:       iteration,
				Action:          "done_converted_to_idle",
				Result:          "Lead controls your lifecycle. Going idle — Lead will assign new work or shut you down.",
				DecisionSummary: truncateDecisionSummary(stepResult.DecisionSummary),
			})
			stepResult.Action = "idle"
		}

		// Step 3: Execute the action
		switch stepResult.Action {
		case "done":
			// This case is now only reachable internally (e.g., max iterations forced done).
			// Normal agent "done" is converted to "idle" above.
			logger.Info("AgentLoop completed (internal)", "agent_id", input.AgentID, "iterations", iteration+1)

			// Emit agent completed event
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: input.WorkflowID,
				EventType:  activities.StreamEventAgentCompleted,
				AgentID:    input.AgentID,
				Message:    activities.MsgAgentCompleted(input.AgentID),
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)

			return AgentLoopResult{
				AgentID:      input.AgentID,
				Role:         input.Role,
				Response:     stepResult.Response,
				Iterations:   iteration + 1,
				TokensUsed:          totalTokens,
				InputTokens:         totalInput,
				OutputTokens:        totalOutput,
				CacheReadTokens:     totalCacheRead,
				CacheCreationTokens: totalCacheCreation,
				ModelUsed:           lastModel,
				Provider:     lastProvider,
				Success:      true,
				TeamKnowledge: ft.Knowledge(),
			}, nil

		case "tool_call":
			consecutiveNonToolActions = 0 // Tool use = progress
			// Reset message spam counter on file_write/file_edit (agent is producing output)
			if stepResult.Tool == "file_write" || stepResult.Tool == "file_edit" {
				consecutiveMsgWithoutWrite = 0
			}

			// Auto-inject extract_prompt for web_fetch/web_crawl so extraction
			// LLM focuses on data the agent actually needs (not generic extraction).
			if (stepResult.Tool == "web_fetch" || stepResult.Tool == "web_crawl") && stepResult.ToolParams != nil {
				if _, hasPrompt := stepResult.ToolParams["extract_prompt"]; !hasPrompt {
					taskSnippet := input.Task
					if len(taskSnippet) > 200 {
						taskSnippet = taskSnippet[:200]
					}
					stepResult.ToolParams["extract_prompt"] = fmt.Sprintf("Extract data relevant to: %s", taskSnippet)
				}
			}

			// L1 fetch cache check
			turnResult := interface{}(nil)
			if cached, hit := ft.checkCache(stepResult.Tool, stepResult.ToolParams); hit {
				logger.Info("L1 fetch cache hit — skipping ExecuteAgent",
					"agent_id", input.AgentID,
					"tool", stepResult.Tool,
					"urls", len(extractURLsFromParams(stepResult.ToolParams)),
				)
				turnResult = cached
				consecutiveToolErrors = 0
				consecutiveTransientErrors = 0
				cumulativeToolCalls++
			}

			if turnResult == nil {
			// Execute tool via standard agent execution (one-shot)
			var toolRes activities.AgentExecutionResult
			toolErr := workflow.ExecuteActivity(ctx, activities.ExecuteAgent, activities.AgentExecutionInput{
				Query:            fmt.Sprintf("Execute tool %s with params: %v", stepResult.Tool, stepResult.ToolParams),
				AgentID:          input.AgentID,
				Context:          map[string]interface{}{"force_swarm": true},
				SuggestedTools:   []string{stepResult.Tool},
				ToolParameters:   stepResult.ToolParams,
				SessionID:        input.SessionID,
				UserID:           input.UserID,
				ParentWorkflowID: input.WorkflowID,
			}).Get(ctx, &toolRes)

			if toolErr == nil {
				turnResult = toolRes.Response
				consecutiveToolErrors = 0
				consecutiveTransientErrors = 0
				cumulativeToolCalls++ // Survives history truncation and reassignment

				// Record tool execution LLM tokens (typically Haiku)
				if toolRes.InputTokens > 0 || toolRes.OutputTokens > 0 {
					recCtx := opts.WithTokenRecordOptions(ctx)
					provider := toolRes.Provider
					if provider == "" {
						provider = detectProviderFromModel(toolRes.ModelUsed)
					}
					_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
						UserID:              input.UserID,
						SessionID:           input.SessionID,
						TaskID:              input.WorkflowID,
						AgentID:             input.AgentID,
						Model:               toolRes.ModelUsed,
						Provider:            provider,
						InputTokens:         toolRes.InputTokens,
						OutputTokens:        toolRes.OutputTokens,
						CacheReadTokens:     toolRes.CacheReadTokens,
						CacheCreationTokens: toolRes.CacheCreationTokens,
						Metadata:            map[string]interface{}{"workflow": "swarm", "phase": "tool_exec", "tool": stepResult.Tool},
					}).Get(ctx, nil)
				}

				// Record tool cost entries (e.g., web_search API costs)
				opts.RecordToolCostEntries(ctx, toolRes, input.UserID, input.SessionID, input.WorkflowID)

				// Auto-register files written by agents
				if stepResult.Tool == "file_write" {
					if path, ok := stepResult.ToolParams["path"].(string); ok && path != "" {
						filesWritten = append(filesWritten, path)
						if p2pV >= 1 {
							_ = workflow.ExecuteActivity(p2pCtx, constants.RegisterFileActivity, activities.RegisterFileInput{
								WorkflowID: input.WorkflowID,
								Path:       path,
								Author:     input.AgentID,
								Size:       len(fmt.Sprintf("%v", stepResult.ToolParams["content"])),
								Summary:    truncateDecisionSummary(stepResult.DecisionSummary),
							}).Get(ctx, nil)
						}

						// Prevent double-write: inject system message reminding agent
						// it already wrote its deliverable. This survives history truncation
						// (unlike notes which may be too short after large file_write).
						pendingSystemMessage = fmt.Sprintf(
							"FILE ALREADY WRITTEN: You saved your deliverable to '%s' in the previous iteration. "+
								"Do NOT write another file. Proceed to publish_data and then idle.",
							path,
						)
					}
				}

				// L1 fetch cache write
				if added := ft.writeCache(stepResult.Tool, stepResult.ToolParams, toolRes.Response, input.AgentID); len(added) > 0 {
					for _, u := range added {
						logger.Info("L1 fetch cache write", "agent_id", input.AgentID, "url", u, "chars", len(fmt.Sprintf("%v", toolRes.Response)))
					}
				}

				// Accumulate tool execution tokens into local totals (Bug #1 fix)
				totalTokens += toolRes.TokensUsed
				totalInput += toolRes.InputTokens
				totalOutput += toolRes.OutputTokens
				totalCacheRead += toolRes.CacheReadTokens
				totalCacheCreation += toolRes.CacheCreationTokens
			} else {
				if isTransientError(toolErr) {
					consecutiveTransientErrors++
					backoff := time.Duration(consecutiveTransientErrors) * 5 * time.Second
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					logger.Warn("Transient tool error, backing off",
						"agent_id", input.AgentID,
						"tool", stepResult.Tool,
						"backoff", backoff,
						"attempt", consecutiveTransientErrors,
						"error", toolErr,
					)
					if err := workflow.Sleep(ctx, backoff); err != nil {
						return AgentLoopResult{
							AgentID:             input.AgentID,
							Role:                input.Role,
							Response:            savedDoneResponse,
							Success:             false,
							Error:               fmt.Sprintf("cancelled during backoff: %v", err),
							TokensUsed:          totalTokens,
							InputTokens:         totalInput,
							OutputTokens:        totalOutput,
							CacheReadTokens:     totalCacheRead,
							CacheCreationTokens: totalCacheCreation,
							ModelUsed:           lastModel,
							Provider:            lastProvider,
							TeamKnowledge:       ft.Knowledge(),
						}, nil
					}
					turnResult = fmt.Sprintf("transient error (will retry): %v", toolErr)
				} else {
					turnResult = fmt.Sprintf("tool error: %v", toolErr)
					consecutiveToolErrors++
				}
			}
			} // end if turnResult == nil (cache miss → execute tool)
			history = append(history, activities.AgentLoopTurn{
				Iteration:       iteration,
				Action:          fmt.Sprintf("tool_call:%s", stepResult.Tool),
				Result:          turnResult,
				DecisionSummary: truncateDecisionSummary(stepResult.DecisionSummary),
			})

			// B4: Reset search-without-fetch counter on any fetch/crawl
			if stepResult.Tool == "web_fetch" || stepResult.Tool == "web_subpage_fetch" || stepResult.Tool == "web_crawl" {
				searchesSinceLastFetch = 0
			}

			// B3: Search saturation detection — inject warning if agent repeats similar queries
			if stepResult.Tool == "web_search" {
				if q, ok := stepResult.ToolParams["query"].(string); ok && q != "" {
					recentSearchQueries = append(recentSearchQueries, q)
					// Cap to last 6 queries — saturation only checks window of 3
					if len(recentSearchQueries) > 6 {
						recentSearchQueries = recentSearchQueries[len(recentSearchQueries)-6:]
					}
					if isSearchSaturated(recentSearchQueries, 3, 0.7) {
						if pendingSystemMessage == "" {
							pendingSystemMessage = "SEARCH SATURATED: You have searched very similar queries 3+ times with diminishing returns. STOP searching and synthesize what you have. Go to PHASE 3 immediately."
						}
						logger.Warn("Search saturation detected",
							"agent_id", input.AgentID,
							"total_searches", len(recentSearchQueries),
							"last_query", q,
						)
					}
				}

				// B4: Consecutive search without fetch — enforce search→fetch cycle
				searchesSinceLastFetch++
				if searchesSinceLastFetch >= 2 {
					if pendingSystemMessage == "" {
						pendingSystemMessage = "MANDATORY: You just searched twice without fetching. Your research cycle REQUIRES: search → FETCH → think. " +
							"Pick 3-8 URLs from your search results and call web_fetch(urls=[...]) NOW. Do NOT search again until you have fetched."
					}
					logger.Warn("Consecutive search without fetch",
						"agent_id", input.AgentID,
						"searches_since_fetch", searchesSinceLastFetch,
					)
				}

				// L3: Search overlap detection
				if overlapPct, known, total := ft.checkSearchOverlap(turnResult, input.AgentID); total > 0 && known > 0 {
					logger.Info("L3 search overlap",
						"agent_id", input.AgentID,
						"total_urls", total,
						"known", known,
						"new", total-known,
						"overlap_pct", overlapPct,
					)
					if overlapPct >= 70 && pendingSystemMessage == "" {
						pendingSystemMessage = fmt.Sprintf(
							"SEARCH OVERLAP: %d/%d URLs from your search are already known to the team. "+
								"Focus on NEW angles or UNIQUE URLs that teammates haven't explored. "+
								"Check ## Team Knowledge for data the team already has.",
							known, total,
						)
					}
				}
			}

		case "send_message":
			// Collaborative action — but spam detection: 3+ messages without file_write is non-progress.
			consecutiveMsgWithoutWrite++
			if consecutiveMsgWithoutWrite >= 3 {
				consecutiveNonToolActions++
				logger.Warn("Agent send_message spam detected: 3+ messages without file_write",
					"agent_id", input.AgentID, "count", consecutiveMsgWithoutWrite)
			} else {
				consecutiveNonToolActions = 0
			}
			// Send message to another agent via P2P mailbox
			if p2pV >= 1 {
				_ = workflow.ExecuteActivity(p2pCtx, constants.SendAgentMessageActivity, activities.SendAgentMessageInput{
					WorkflowID: input.WorkflowID,
					From:       input.AgentID,
					To:         stepResult.To,
					Type:       activities.MessageType(stepResult.MessageType),
					Payload:    stepResult.Payload,
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
			}

			// Notify parent so it can forward a wake-up signal to idle recipients.
			// Parent holds agentFutures mapping; we don't know target's child workflow ID.
			if parentExec := workflow.GetInfo(ctx).ParentWorkflowExecution; parentExec != nil && stepResult.To != "" {
				// Include message preview so Temporal UI shows content (not just from/to)
				preview := ""
				if msg, ok := stepResult.Payload["message"]; ok {
					preview = fmt.Sprintf("%v", msg)
					if len(preview) > 200 {
						preview = preview[:200] + "..."
					}
				}
				_ = workflow.SignalExternalWorkflow(ctx, parentExec.ID, "",
					"agent-message-sent",
					map[string]string{"from": input.AgentID, "to": stepResult.To, "preview": preview},
				).Get(ctx, nil)
			}

			history = append(history, activities.AgentLoopTurn{
				Iteration:       iteration,
				Action:          fmt.Sprintf("send_message:%s", stepResult.To),
				Result:          "sent",
				DecisionSummary: truncateDecisionSummary(stepResult.DecisionSummary),
			})

		case "publish_data":
			// Collaborative action — but spam detection: 3+ messages without file_write is non-progress.
			consecutiveMsgWithoutWrite++
			if consecutiveMsgWithoutWrite >= 3 {
				consecutiveNonToolActions++
				logger.Warn("Agent publish_data spam detected: 3+ without file_write",
					"agent_id", input.AgentID, "count", consecutiveMsgWithoutWrite)
			} else {
				consecutiveNonToolActions = 0
			}
			// Publish to workspace topic
			if p2pV >= 1 {
				_ = workflow.ExecuteActivity(p2pCtx, constants.WorkspaceAppendActivity, activities.WorkspaceAppendInput{
					WorkflowID: input.WorkflowID,
					Topic:      stepResult.Topic,
					Entry: map[string]interface{}{
						"author": input.AgentID,
						"data":   stepResult.Data,
					},
					Timestamp: workflow.Now(ctx),
				}).Get(ctx, nil)
			}

			history = append(history, activities.AgentLoopTurn{
				Iteration:       iteration,
				Action:          fmt.Sprintf("publish_data:%s", stepResult.Topic),
				Result:          "published",
				DecisionSummary: truncateDecisionSummary(stepResult.DecisionSummary),
			})

		case "idle":
			// Agent goes idle — notify parent and wait for new task (Temporal Signal)
			consecutiveNonToolActions = 0
			consecutiveMsgWithoutWrite = 0
			logger.Info("Agent going idle", "agent_id", input.AgentID)

			// Build result summary for Lead: prefer savedDoneResponse (from done→idle),
			// then stepResult.Response (from idle with response), then DecisionSummary as fallback.
			idleSummary := savedDoneResponse
			if idleSummary == "" && stepResult.Response != "" {
				idleSummary = stepResult.Response
			}
			if idleSummary == "" && stepResult.DecisionSummary != "" {
				idleSummary = stepResult.DecisionSummary
			}

			history = append(history, activities.AgentLoopTurn{
				Iteration:       iteration,
				Action:          "idle",
				Result:          "waiting for new task",
				DecisionSummary: truncateDecisionSummary(stepResult.DecisionSummary),
			})

			// Notify Lead that this agent is idle and available for reassignment.
			// Signal the parent SwarmWorkflow so Lead wakes up immediately.
			// Carry result_summary so Lead can evaluate what the agent produced.
			var parentWfID string
			if parentExec := workflow.GetInfo(ctx).ParentWorkflowExecution; parentExec != nil {
				parentWfID = parentExec.ID
			}
			if parentWfID != "" {
				// Build compact tools-used summary so Lead can inject it on reassignment
				toolsUsed := buildToolsUsedSummary(history)

				// Build enriched completion report for Lead
				completionReport := stepResult.CompletionReport
				if completionReport == nil {
					completionReport = map[string]interface{}{}
				}
				if len(filesWritten) > 0 {
					completionReport["files_written"] = filesWritten
				}
				if _, ok := completionReport["summary"]; !ok {
					completionReport["summary"] = idleSummary
				}

				idlePayload := map[string]interface{}{
					"agent_id":          input.AgentID,
					"result_summary":    idleSummary,
					"iterations":        iteration + 1,
					"tools_used":        toolsUsed,
					"completion_report": completionReport,
					"team_knowledge":    ft.Knowledge(),
				}
				_ = workflow.SignalExternalWorkflow(ctx, parentWfID, "", "agent-idle", idlePayload).Get(ctx, nil)
				logger.Info("Agent signaled parent: idle", "agent_id", input.AgentID, "parent", parentWfID)
			}

			// Wait for new task, peer message, or shutdown via Temporal Signals (zero-cost idle)
			taskCh := workflow.GetSignalChannel(ctx, fmt.Sprintf("agent:%s:new-task", input.AgentID))
			shutdownCh := workflow.GetSignalChannel(ctx, fmt.Sprintf("agent:%s:shutdown", input.AgentID))
			peerMsgCh := workflow.GetSignalChannel(ctx, fmt.Sprintf("agent:%s:peer-message", input.AgentID))

			idleSel := workflow.NewSelector(ctx)
			var newTaskDesc string
			var newModelTier string
			gotShutdown := false
			gotPeerMessage := false

			idleSel.AddReceive(taskCh, func(ch workflow.ReceiveChannel, more bool) {
				var assignment map[string]string
				ch.Receive(ctx, &assignment)
				newTaskDesc = assignment["description"]
				newModelTier = assignment["model_tier"]
				logger.Info("Agent received new task", "agent_id", input.AgentID, "task", newTaskDesc, "model_tier", newModelTier)
			})
			idleSel.AddReceive(shutdownCh, func(ch workflow.ReceiveChannel, more bool) {
				var msg string
				ch.Receive(ctx, &msg)
				gotShutdown = true
				logger.Info("Agent received shutdown", "agent_id", input.AgentID)
			})
			// P2P wake-up: peer message signal from parent (forwarded from another agent).
			// Agent resumes its current task — mailbox is polled at next iteration start.
			idleSel.AddReceive(peerMsgCh, func(ch workflow.ReceiveChannel, more bool) {
				var info map[string]string
				ch.Receive(ctx, &info)
				gotPeerMessage = true
				logger.Info("Agent woken by peer message", "agent_id", input.AgentID, "from", info["from"])
			})
			// Timeout: if idle for 10 minutes, auto-exit (increased from 5min)
			idleSel.AddFuture(workflow.NewTimer(ctx, 10*time.Minute), func(f workflow.Future) {
				_ = f.Get(ctx, nil)
				gotShutdown = true
				logger.Info("Agent idle timeout", "agent_id", input.AgentID)
			})
			idleSel.Select(ctx)

			if gotShutdown {
				// Reuse idleSummary (computed above with fallback chain:
				// savedDoneResponse → stepResult.Response → DecisionSummary)
				idleResponse := idleSummary
				if idleResponse == "" {
					idleResponse = "Agent shutdown after idle"
				}
				return AgentLoopResult{
					AgentID:      input.AgentID,
					Role:         input.Role,
					Response:     idleResponse,
					Iterations:   iteration + 1,
					TokensUsed:          totalTokens,
					InputTokens:         totalInput,
					OutputTokens:        totalOutput,
					CacheReadTokens:     totalCacheRead,
					CacheCreationTokens: totalCacheCreation,
					ModelUsed:           lastModel,
					Provider:     lastProvider,
					Success:      true,
					TeamKnowledge: ft.Knowledge(),
				}, nil
			}

			if gotPeerMessage {
				// Woken by peer message — resume current task with unchanged state.
				// Next iteration will poll mailbox and see the incoming message in ## Inbox Messages.
				// No task/history/iteration changes needed; budget constraints still apply.
				continue
			}

			// Got new task — keep recent history so agent remembers what it searched/read.
			// Python-side truncation (agent.py:3042) already limits older turns to 500 chars,
			// so retaining 5 turns adds ~2-4K tokens — far less than repeating tool calls.
			input.Task = newTaskDesc
			// Update model tier if Lead specified one for this reassignment
			if newModelTier != "" {
				modelTier = newModelTier
			}
			const keepLastNOnReassign = 5
			if len(history) > keepLastNOnReassign {
				history = history[len(history)-keepLastNOnReassign:]
			}
			filesWritten = nil // Reset files written for new task

			// B1: Per-agent reassignment limit to prevent runaway iteration expansion.
			// Each agent can be reassigned at most 2 times (3 total tasks including initial).
			const maxReassignments = 2
			const iterationsPerReassign = 15
			const maxTotalIterations = 80 // Lowered from 150

			input.ReassignCount++
			if input.ReassignCount > maxReassignments {
				logger.Warn("Agent reached max reassignments, rejecting new task",
					"agent_id", input.AgentID,
					"reassign_count", input.ReassignCount,
					"max", maxReassignments,
				)
				// Don't accept the new task — agent stays idle and will be shut down
				savedDoneResponse = ""
				continue
			}

			newMax := input.MaxIterations + iterationsPerReassign
			if newMax > maxTotalIterations {
				newMax = maxTotalIterations
			}
			if newMax > input.MaxIterations {
				logger.Info("Extending iteration budget on reassignment",
					"agent_id", input.AgentID,
					"old_max", input.MaxIterations,
					"new_max", newMax,
					"current_iteration", iteration,
				)
				input.MaxIterations = newMax
			}

			savedDoneResponse = ""     // Clear stale response so shutdown returns fresh result
			continue

		default:
			// Treat unrecognized actions (file_write, file_read, web_search, etc.)
			// as tool_call where the action name is the tool name. LLMs sometimes
			// return the tool name directly instead of wrapping in tool_call.
			toolName := stepResult.Action
			if toolName == "" {
				consecutiveNonToolActions++
				logger.Warn("Empty action from LLM", "agent_id", input.AgentID)
				history = append(history, activities.AgentLoopTurn{
					Iteration:       iteration,
					Action:          "unknown:empty",
					Result:          "skipped",
					DecisionSummary: truncateDecisionSummary(stepResult.DecisionSummary),
				})
			} else {
				consecutiveNonToolActions = 0 // Implicit tool use = progress
				logger.Info("Treating action as tool_call", "action", toolName, "agent_id", input.AgentID)
				params := stepResult.ToolParams
				if len(params) == 0 {
					// Some actions put params in payload or data fields
					params = make(map[string]interface{})
					if stepResult.Data != "" {
						params["content"] = stepResult.Data
					}
					if stepResult.Topic != "" {
						params["path"] = stepResult.Topic
					}
				}
				// L1 fetch cache check (default case)
				turnResult := interface{}(nil)
				if cached, hit := ft.checkCache(toolName, params); hit {
					logger.Info("L1 fetch cache hit — skipping ExecuteAgent",
						"agent_id", input.AgentID,
						"tool", toolName,
						"urls", len(extractURLsFromParams(params)),
					)
					turnResult = cached
					consecutiveToolErrors = 0
					consecutiveTransientErrors = 0
					cumulativeToolCalls++
				}

				if turnResult == nil {
				var toolRes activities.AgentExecutionResult
				toolErr := workflow.ExecuteActivity(ctx, activities.ExecuteAgent, activities.AgentExecutionInput{
					Query:            fmt.Sprintf("Execute tool %s with params: %v", toolName, params),
					AgentID:          input.AgentID,
					Context:          map[string]interface{}{"force_swarm": true},
					SuggestedTools:   []string{toolName},
					ToolParameters:   params,
					SessionID:        input.SessionID,
					UserID:           input.UserID,
					ParentWorkflowID: input.WorkflowID,
				}).Get(ctx, &toolRes)

				if toolErr == nil {
					turnResult = toolRes.Response
					consecutiveToolErrors = 0
					consecutiveTransientErrors = 0
					cumulativeToolCalls++ // Survives history truncation and reassignment

					// Record tool execution LLM tokens (typically Haiku)
					if toolRes.InputTokens > 0 || toolRes.OutputTokens > 0 {
						recCtx := opts.WithTokenRecordOptions(ctx)
						provider := toolRes.Provider
						if provider == "" {
							provider = detectProviderFromModel(toolRes.ModelUsed)
						}
						_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
							UserID:              input.UserID,
							SessionID:           input.SessionID,
							TaskID:              input.WorkflowID,
							AgentID:             input.AgentID,
							Model:               toolRes.ModelUsed,
							Provider:            provider,
							InputTokens:         toolRes.InputTokens,
							OutputTokens:        toolRes.OutputTokens,
							CacheReadTokens:     toolRes.CacheReadTokens,
							CacheCreationTokens: toolRes.CacheCreationTokens,
							Metadata:            map[string]interface{}{"workflow": "swarm", "phase": "tool_exec", "tool": toolName},
						}).Get(ctx, nil)
					}

					// Record tool cost entries (e.g., web_search API costs)
					opts.RecordToolCostEntries(ctx, toolRes, input.UserID, input.SessionID, input.WorkflowID)

					// Auto-register files written by agents
					if toolName == "file_write" {
						if path, ok := params["path"].(string); ok && path != "" {
							if p2pV >= 1 {
								_ = workflow.ExecuteActivity(p2pCtx, constants.RegisterFileActivity, activities.RegisterFileInput{
									WorkflowID: input.WorkflowID,
									Path:       path,
									Author:     input.AgentID,
									Size:       len(fmt.Sprintf("%v", params["content"])),
									Summary:    truncateDecisionSummary(stepResult.DecisionSummary),
								}).Get(ctx, nil)
							}

							// Track for completion report (same as tool_call case)
							filesWritten = append(filesWritten, path)
							pendingSystemMessage = fmt.Sprintf(
								"SUCCESS: File written to '%s'. Do NOT write to this file again. "+
									"If you have more findings, go idle with key_findings.", path)
						}
					}

					// L1 fetch cache write
					if added := ft.writeCache(toolName, params, toolRes.Response, input.AgentID); len(added) > 0 {
						for _, u := range added {
							logger.Info("L1 fetch cache write", "agent_id", input.AgentID, "url", u, "chars", len(fmt.Sprintf("%v", toolRes.Response)))
						}
					}

					// Accumulate tool execution tokens into local totals (Bug #1 fix)
					totalTokens += toolRes.TokensUsed
					totalInput += toolRes.InputTokens
					totalOutput += toolRes.OutputTokens
					totalCacheRead += toolRes.CacheReadTokens
					totalCacheCreation += toolRes.CacheCreationTokens
				} else {
					if isTransientError(toolErr) {
						consecutiveTransientErrors++
						backoff := time.Duration(consecutiveTransientErrors) * 5 * time.Second
						if backoff > 30*time.Second {
							backoff = 30 * time.Second
						}
						logger.Warn("Transient tool error, backing off",
							"agent_id", input.AgentID,
							"tool", toolName,
							"backoff", backoff,
							"attempt", consecutiveTransientErrors,
							"error", toolErr,
						)
						if err := workflow.Sleep(ctx, backoff); err != nil {
							return AgentLoopResult{
								AgentID:             input.AgentID,
								Role:                input.Role,
								Response:            savedDoneResponse,
								Success:             false,
								Error:               fmt.Sprintf("cancelled during backoff: %v", err),
								TokensUsed:          totalTokens,
								InputTokens:         totalInput,
								OutputTokens:        totalOutput,
								CacheReadTokens:     totalCacheRead,
								CacheCreationTokens: totalCacheCreation,
								ModelUsed:           lastModel,
								Provider:            lastProvider,
								TeamKnowledge:       ft.Knowledge(),
							}, nil
						}
						turnResult = fmt.Sprintf("transient error (will retry): %v", toolErr)
					} else {
						turnResult = fmt.Sprintf("tool error: %v", toolErr)
						consecutiveToolErrors++
						logger.Warn("Tool execution failed",
							"agent_id", input.AgentID,
							"tool", toolName,
							"consecutive_errors", consecutiveToolErrors,
							"error", toolErr,
						)
					}
				}
				} // end if !defaultCacheHit
				history = append(history, activities.AgentLoopTurn{
					Iteration:       iteration,
					Action:          fmt.Sprintf("tool_call:%s", toolName),
					Result:          turnResult,
					DecisionSummary: truncateDecisionSummary(stepResult.DecisionSummary),
				})

				// Bail out if agent is stuck in a loop of failing tool calls (permanent errors only)
				if consecutiveToolErrors >= 3 {
					logger.Warn("AgentLoop aborting: 3 consecutive permanent tool errors",
						"agent_id", input.AgentID,
						"last_tool", toolName,
					)
					return AgentLoopResult{
						AgentID:      input.AgentID,
						Role:         input.Role,
						Response:     fmt.Sprintf("Agent stopped after %d consecutive tool failures. Last attempted: %s", consecutiveToolErrors, toolName),
						Iterations:   iteration + 1,
						TokensUsed:          totalTokens,
						InputTokens:         totalInput,
						OutputTokens:        totalOutput,
						CacheReadTokens:     totalCacheRead,
						CacheCreationTokens: totalCacheCreation,
						ModelUsed:           lastModel,
						Provider:     lastProvider,
						Success:      false,
						Error:        "consecutive tool errors",
						TeamKnowledge: ft.Knowledge(),
					}, nil
				}
			}
		}

		// History truncation: sliding window to prevent unbounded growth.
		// Keep first 2 turns (initial context) + last 13 (recent context) = max 15.
		const maxHistoryTurns = 15
		if len(history) > maxHistoryTurns {
			history = append(history[:2], history[len(history)-(maxHistoryTurns-2):]...)
		}

		// Convergence detection: if agent hasn't used tools for 3 consecutive iterations,
		// it's likely stuck in a reasoning loop without making progress (Claude Code pattern)
		if consecutiveNonToolActions >= 3 {
			logger.Warn("AgentLoop converged: no tool use for 3 consecutive iterations",
				"agent_id", input.AgentID,
				"iteration", iteration,
			)
			// Build partial findings from history
			var summary string
			startIdx := len(history) - 3
			if startIdx < 0 {
				startIdx = 0
			}
			for _, h := range history[startIdx:] {
				s := fmt.Sprintf("%v", h.Result)
				if s == "" || s == "<nil>" {
					continue
				}
				if len(s) > 2000 {
					s = s[:2000] + "..."
				}
				summary += fmt.Sprintf("[%s]: %s\n", h.Action, s)
			}
			if summary == "" {
				summary = fmt.Sprintf("Agent %s converged after %d iterations with no further tool use.", input.AgentID, iteration+1)
			}
			return AgentLoopResult{
				AgentID:      input.AgentID,
				Role:         input.Role,
				Response:     summary,
				Iterations:   iteration + 1,
				TokensUsed:          totalTokens,
				InputTokens:         totalInput,
				OutputTokens:        totalOutput,
				CacheReadTokens:     totalCacheRead,
				CacheCreationTokens: totalCacheCreation,
				ModelUsed:           lastModel,
				Provider:     lastProvider,
				Success:      true,
				TeamKnowledge: ft.Knowledge(),
			}, nil
		}

		// Emit progress event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: input.WorkflowID,
			EventType:  activities.StreamEventProgress,
			AgentID:    input.AgentID,
			Message:    activities.MsgAgentProgress(input.AgentID, iteration+1, input.MaxIterations, stepResult.Action),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
	}

	// Max iterations reached — return partial success so synthesis can use whatever was collected
	logger.Warn("AgentLoop max iterations reached", "agent_id", input.AgentID)
	var partialResponse string
	for _, h := range history {
		if s, ok := h.Result.(string); ok && s != "" {
			partialResponse += s + "\n"
		}
	}
	if partialResponse == "" {
		partialResponse = "Max iterations reached without completing task"
	}
	return AgentLoopResult{
		AgentID:             input.AgentID,
		Role:                input.Role,
		Response:            partialResponse,
		Iterations:          input.MaxIterations,
		TokensUsed:          totalTokens,
		InputTokens:         totalInput,
		OutputTokens:        totalOutput,
		CacheReadTokens:     totalCacheRead,
		CacheCreationTokens: totalCacheCreation,
		ModelUsed:           lastModel,
		Provider:            lastProvider,
		Success:             true,
		TeamKnowledge:       ft.Knowledge(),
	}, nil
}

// ── SwarmWorkflow ──────────────────────────────────────────────────────────────

// SwarmWorkflow orchestrates persistent AgentLoop child workflows with
// inter-agent messaging and dynamic spawn capability.
func SwarmWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	if input.ParentWorkflowID != "" {
		workflowID = input.ParentWorkflowID
	}

	logger.Info("SwarmWorkflow started", "query", input.Query, "workflow_id", workflowID)

	// Activity options
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	p2pCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// Emit workflow started
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowStarted,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmStarted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Version gate for P2P activities in SwarmWorkflow (send_message, broadcast).
	swarmP2pV := workflow.GetVersion(ctx, "swarm_p2p_v1", workflow.DefaultVersion, 1)

	// Load swarm config
	var cfg activities.WorkflowConfig
	if err := workflow.ExecuteActivity(ctx, activities.GetWorkflowConfig).Get(ctx, &cfg); err != nil {
		logger.Warn("Failed to load config, using defaults", "error", err)
		cfg.SwarmMaxAgents = 10
		cfg.SwarmMaxIterationsPerAgent = 25
		cfg.SwarmAgentTimeoutSeconds = 1800
		cfg.SwarmMaxTotalLLMCalls = 200
		cfg.SwarmMaxTotalTokens = 1000000
		cfg.SwarmMaxWallClockMinutes = 30
	}

	// Setup workspace directories
	_ = workflow.ExecuteActivity(p2pCtx, constants.SetupWorkspaceDirsActivity, activities.SetupWorkspaceDirsInput{
		WorkflowID: workflowID,
		SessionID:  input.SessionID,
	}).Get(ctx, nil)

	// Replay safety: keep legacy behavior for old runs, disable injected memory prompt
	// for current/new runs. The injected context key is not consumed by /agent/loop.
	memoryPromptVersion := workflow.GetVersion(ctx, "swarm_user_memory_prompt_v1", workflow.DefaultVersion, 1)
	if memoryPromptVersion == workflow.DefaultVersion && input.UserID != "" {
		if input.Context == nil {
			input.Context = make(map[string]interface{})
		}
		input.Context["user_memory_prompt"] = "You have access to a persistent memory directory at /memory/. " +
			"This contains knowledge from past sessions with this user.\n\n" +
			"At the start, read /memory/MEMORY.md to see what memories exist. " +
			"Read specific files for full details as needed."
	}

	// Lead activity context — longer timeout for LLM call
	leadCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 150 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
	})

	// Track spawns, budget, and agent states
	spawnCount := 0
	var budgetTotalLLMCalls, budgetTotalTokens int
	var historyTruncated bool
	var lastLeadMailboxSeq uint64 // Track last-read seq from Lead's mailbox
	maxLLMCalls := cfg.SwarmMaxTotalLLMCalls
	maxTokens := cfg.SwarmMaxTotalTokens
	maxWallClockSeconds := cfg.SwarmMaxWallClockMinutes * 60
	swarmStartTime := workflow.Now(ctx)
	var elapsed int // declared here so it can be updated per-iteration and in file_read inner loop

	type agentFuture struct {
		ID         string
		Future     workflow.ChildWorkflowFuture
		WorkflowID string // Child workflow execution ID for signaling
	}
	var agentFutures []agentFuture
	var roster []TeamMember
	results := make(map[string]AgentLoopResult)
	idleSnapshots := make(map[string]AgentLoopResult)       // Best-effort snapshots from idle notifications
	swarmTeamKnowledge := make([]activities.TeamKnowledgeEntry, 0) // Cross-agent knowledge accumulator (L2)
	completionCh := workflow.NewChannel(ctx)
	agentStates := make(map[string]activities.LeadAgentState)
	agentTaskStartTimes := make(map[string]time.Time) // agentID → when current task started
	agentTaskMap := make(map[string]string)            // agentID → current taskID
	leadHistory := make([]map[string]interface{}, 0) // Must be non-nil for JSON → Pydantic List

	// Version gate for Lead tool_call support (Temporal replay safety)
	leadToolCallV := workflow.GetVersion(ctx, "lead-tool-call-v1", workflow.DefaultVersion, 1)

	allowedLeadTools := map[string]bool{
		"web_search": true, "web_fetch": true, "calculator": true,
		"file_list": true, "file_read": true,
	}

	const maxToolCallRounds = 5

	// Dedicated activity context for Lead tool execution (web_search/web_fetch need 30-60s).
	// p2pCtx has 10s timeout which is too short — tool calls would always timeout.
	toolCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 90 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// ── Phase 1: Lead Initial Planning ──────────────────────────────────────
	// Lead Agent decomposes the query into tasks and decides which agents to spawn.
	// This replaces the old DecomposeTask → static roster approach.
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventProgress,
		AgentID:    "swarm-lead",
		Message:    activities.MsgSwarmPlanning(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Convert session history for Lead multi-turn context
	var conversationHistory []map[string]interface{}
	if len(input.History) > 0 {
		conversationHistory = convertHistoryForLead(input.History)
	}

	var planDecision activities.LeadDecisionResult
	planEvent := activities.LeadEvent{
		Type:          "initial_plan",
		ResultSummary: input.Query,
	}
	// Phase 1 inner loop: handles tool_call actions (same pattern as Phase 2).
	// Lead may return tool_call to search/fetch before deciding on plan.
	for phase1ToolRound := 0; phase1ToolRound <= maxToolCallRounds; phase1ToolRound++ {
		elapsed = int(workflow.Now(ctx).Sub(swarmStartTime).Seconds())
		planErr := workflow.ExecuteActivity(leadCtx, constants.LeadDecisionActivity, activities.LeadDecisionInput{
			WorkflowID: workflowID,
			Event:      planEvent,
			TaskList:   make([]activities.SwarmTask, 0),
			AgentStates: make([]activities.LeadAgentState, 0),
			Budget: activities.LeadBudget{
				TotalLLMCalls:       budgetTotalLLMCalls,
				RemainingLLMCalls:   maxLLMCalls - budgetTotalLLMCalls,
				TotalTokens:         budgetTotalTokens,
				RemainingTokens:     maxTokens - budgetTotalTokens,
				ElapsedSeconds:      elapsed,
				MaxWallClockSeconds: maxWallClockSeconds,
			},
			History:             leadHistory,
			OriginalQuery:       input.Query,
			ConversationHistory: conversationHistory,
		}).Get(ctx, &planDecision)

		if planErr != nil {
			logger.Error("Lead initial planning failed", "error", planErr)
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Lead planning failed: %v", planErr)}, planErr
		}

		budgetTotalLLMCalls++
		budgetTotalTokens += planDecision.TokensUsed

		// Record Lead token usage
		{
			recCtx := opts.WithTokenRecordOptions(ctx)
			provider := planDecision.Provider
			if provider == "" {
				provider = detectProviderFromModel(planDecision.ModelUsed)
			}
			_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
				UserID:              input.UserID,
				SessionID:           input.SessionID,
				TaskID:              workflowID,
				AgentID:             "swarm-lead",
				Model:               planDecision.ModelUsed,
				Provider:            provider,
				InputTokens:         planDecision.InputTokens,
				OutputTokens:        planDecision.OutputTokens,
				CacheReadTokens:     planDecision.CacheReadTokens,
				CacheCreationTokens: planDecision.CacheCreationTokens,
				Metadata:            map[string]interface{}{"workflow": "swarm", "phase": "initial_plan"},
			}).Get(ctx, nil)
		}

		leadHistory = append(leadHistory, map[string]interface{}{
			"decision_summary": planDecision.DecisionSummary,
			"event":            "initial_plan",
			"actions":          len(planDecision.Actions),
		})

		// Separate actions into file I/O, tool_call, and others
		var fileIOActions []activities.LeadAction
		var toolCallActions []activities.LeadAction
		var otherActions []activities.LeadAction
		for _, action := range planDecision.Actions {
			if action.Type == "file_read" || action.Type == "file_list" {
				fileIOActions = append(fileIOActions, action)
			} else if leadToolCallV >= 1 && action.Type == "tool_call" {
				toolCallActions = append(toolCallActions, action)
			} else {
				otherActions = append(otherActions, action)
			}
		}

		// Handle interim_reply before I/O — only emit on first round to avoid repetitive messages
		if phase1ToolRound == 0 {
			for _, action := range otherActions {
				if action.Type == "interim_reply" && action.Content != "" {
					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventLLMOutput,
						AgentID:    "swarm-lead",
						Message:    action.Content,
						Payload: map[string]interface{}{
							"role":    "lead",
							"interim": true,
						},
						Timestamp: workflow.Now(ctx),
					}).Get(ctx, nil)
				}
			}
		}

		// Execute file I/O AND tool_call in the same round (no mutual exclusion)
		ranFileIO := false
		ranToolCalls := false

		// File I/O: file_read + file_list (zero LLM cost)
		if len(fileIOActions) > 0 && phase1ToolRound < maxToolCallRounds {
			ranFileIO = true
			var fileContents []activities.FileReadResult
			for _, fr := range fileIOActions {
				if fr.Type == "file_list" {
					var listResult activities.ListWorkspaceFilesResult
					listErr := workflow.ExecuteActivity(p2pCtx, constants.ListWorkspaceFilesActivity,
						activities.ListWorkspaceFilesInput{SessionID: input.SessionID}).Get(ctx, &listResult)
					if listErr != nil {
						fileContents = append(fileContents, activities.FileReadResult{Path: ".", Error: listErr.Error()})
					} else {
						var listing string
						for _, f := range listResult.Files {
							listing += f.Path + "\n"
						}
						if listing == "" {
							listing = "(empty workspace)"
						}
						fileContents = append(fileContents, activities.FileReadResult{Path: ".", Content: listing})
					}
				} else if fr.Path != "" {
					var readResult activities.ReadWorkspaceFileResult
					readErr := workflow.ExecuteActivity(p2pCtx, constants.ReadWorkspaceFileActivity,
						activities.ReadWorkspaceFileInput{
							SessionID: input.SessionID, Path: fr.Path, MaxChars: 4000,
						}).Get(ctx, &readResult)
					if readErr != nil {
						fileContents = append(fileContents, activities.FileReadResult{Path: fr.Path, Error: readErr.Error()})
					} else {
						fileContents = append(fileContents, activities.FileReadResult{
							Path: readResult.Path, Content: readResult.Content,
							Truncated: readResult.Truncated, Error: readResult.Error,
						})
					}
				}
			}
			planEvent.FileContents = fileContents
		}

		// Tool calls
		if len(toolCallActions) > 0 && phase1ToolRound < maxToolCallRounds {
			ranToolCalls = true
			var toolResults []activities.ToolResultEntry
			for _, tc := range toolCallActions {
				if !allowedLeadTools[tc.Tool] {
					toolResults = append(toolResults, activities.ToolResultEntry{
						Tool: tc.Tool, Error: fmt.Sprintf("tool %q not in allowlist", tc.Tool),
					})
					continue
				}
				_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
					WorkflowID: workflowID,
					EventType:  activities.StreamEventLeadToolCall,
					AgentID:    "swarm-lead",
					Message:    fmt.Sprintf("Lead executing %s", tc.Tool),
					Payload: map[string]interface{}{"tool": tc.Tool, "phase": "initial_plan"},
					Timestamp: workflow.Now(ctx),
				}).Get(ctx, nil)
				var toolResult activities.LeadToolResult
				toolErr := workflow.ExecuteActivity(toolCtx, constants.LeadExecuteToolActivity,
					activities.LeadToolInput{
						Tool: tc.Tool, ToolParams: tc.ToolParams, SessionID: input.SessionID,
					}).Get(ctx, &toolResult)
				if toolErr != nil {
					toolResults = append(toolResults, activities.ToolResultEntry{Tool: tc.Tool, Error: toolErr.Error()})
				} else {
					output := toolResult.Output
					maxChars := 4000
					if tc.Tool == "calculator" {
						maxChars = 500
					}
					if len(output) > maxChars {
						output = output[:maxChars] + "\n... [truncated]"
					}
					toolResults = append(toolResults, activities.ToolResultEntry{
						Tool: tc.Tool, Output: output, Error: toolResult.Error,
					})
				}
				logger.Info("Lead Phase 1 tool_call executed", "tool", tc.Tool, "round", phase1ToolRound)
			}
			planEvent.ToolResults = toolResults
		}

		// Clear stale fields (M2)
		if ranFileIO && !ranToolCalls {
			planEvent.ToolResults = nil
		}
		if ranToolCalls && !ranFileIO {
			planEvent.FileContents = nil
		}

		// If any I/O was executed, loop back for another LeadDecision
		if !ranFileIO && !ranToolCalls {
			break // No more I/O — proceed to action processing
		}
		logger.Info("Lead Phase 1 I/O round complete, calling LeadDecision again",
			"round", phase1ToolRound, "ranFileIO", ranFileIO, "ranToolCalls", ranToolCalls)
		continue
	}

	logger.Info("Lead initial plan",
		"summary", planDecision.DecisionSummary,
		"actions", len(planDecision.Actions),
	)

	// Check if Lead wants to reply directly (no agents needed)
	// This happens when Lead uses tool_call to gather data and then replies.
	// Version-gated: old workflows in replay won't have this early return path.
	directReplyV := workflow.GetVersion(ctx, "swarm_direct_reply_v1", workflow.DefaultVersion, 1)
	if directReplyV >= 1 {
		var directReply string
		for _, action := range planDecision.Actions {
			if action.Type == "reply" || action.Type == "done" {
				directReply = action.Content
				break
			}
		}
		if directReply != "" {
			logger.Info("Lead answering directly without agents", "reply_len", len(directReply))
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventLLMOutput,
				AgentID:    "swarm-lead",
				Message:    directReply,
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
			return TaskResult{
				Success: true,
				Result:  directReply,
			}, nil
		}
	}

	// Emit LEAD_DECISION so the frontend can show Lead's pulse in Radar
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventLeadDecision,
		AgentID:    "swarm-lead",
		Message:    planDecision.DecisionSummary,
		Payload: map[string]interface{}{
			"event_type": "initial_plan",
			"actions":    len(planDecision.Actions),
		},
	}).Get(ctx, nil)

	// Execute Lead's initial planning actions (revise_plan + spawn_agent)
	// Helper to spawn an agent from Lead action
	spawnAgent := func(taskDesc string, role string, modelTier string, taskID string) {
		if spawnCount >= cfg.SwarmMaxAgents {
			logger.Warn("Lead spawn rejected: at capacity")
			return
		}
		agentName := agents.GetAgentName(workflowID, spawnCount)
		spawnCount++

		roster = append(roster, TeamMember{AgentID: agentName, Task: taskDesc, Role: role})
		agentStates[agentName] = activities.LeadAgentState{
			AgentID:     agentName,
			Status:      "running",
			CurrentTask: taskDesc,
			Role:        role,
		}
		agentTaskStartTimes[agentName] = workflow.Now(ctx)

		// Build per-agent context with model_tier.
		// Swarm agents default to "medium" when Lead omits model_tier,
		// preventing fallthrough to budget_agent_max which incorrectly maps to "small".
		agentCtx := make(map[string]interface{}, len(input.Context)+1)
		for k, v := range input.Context {
			agentCtx[k] = v
		}
		if modelTier == "" {
			modelTier = "medium"
		}
		agentCtx["model_tier"] = modelTier

		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowExecutionTimeout: time.Duration(cfg.SwarmAgentTimeoutSeconds) * time.Second,
			ParentClosePolicy:        enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		future := workflow.ExecuteChildWorkflow(childCtx, AgentLoop, AgentLoopInput{
			AgentID:               agentName,
			WorkflowID:            workflowID,
			Task:                  taskDesc,
			MaxIterations:         cfg.SwarmMaxIterationsPerAgent,
			SessionID:             input.SessionID,
			UserID:                input.UserID,
			Context:               agentCtx,
			TeamRoster:            roster,
			WorkspaceMaxEntries:   cfg.SwarmWorkspaceMaxEntries,
			WorkspaceSnippetChars: cfg.SwarmWorkspaceSnippetChars,
			Role:                  role,
			OriginalQuery:         input.Query,
			TeamKnowledge:         swarmTeamKnowledge,
		})
		af := agentFuture{ID: agentName, Future: future}
		// Resolve child workflow execution ID for signaling
		var childExec workflow.Execution
		if err := future.GetChildWorkflowExecution().Get(ctx, &childExec); err == nil {
			af.WorkflowID = childExec.ID
		}
		agentFutures = append(agentFutures, af)

		workflow.Go(ctx, func(gCtx workflow.Context) {
			var result AgentLoopResult
			if err := future.Get(gCtx, &result); err != nil {
				result = AgentLoopResult{AgentID: agentName, Success: false, Error: err.Error()}
			}
			// If child workflow timed out / was cancelled (Success=false, empty Response),
			// check if we already captured an idle snapshot with useful data.
			if !result.Success {
				if snapshot, ok := idleSnapshots[agentName]; ok && snapshot.Response != "" {
					if len(snapshot.Response) > len(result.Response) {
						logger.Info("Agent child workflow failed, using idle snapshot as fallback",
							"agent_id", agentName,
							"error", result.Error,
							"snapshot_len", len(snapshot.Response),
						)
						snapshot.Error = result.Error
						snapshot.Iterations = result.Iterations
						result = snapshot
					}
				}
			}
			completionCh.Send(gCtx, result)
		})

		// Link agent to TaskList entry if specified
		if taskID != "" {
			_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
				WorkflowID: workflowID,
				TaskID:     taskID,
				Status:     "in_progress",
				AgentID:    agentName,
			}).Get(ctx, nil)
			agentTaskMap[agentName] = taskID
		}

		logger.Info("Lead spawned agent", "agent_id", agentName, "task", taskDesc, "task_id", taskID)
	}

	var planTasks []activities.SwarmTask // tracks tasks from revise_plan for dependency checking

	// Phase 1: Process revise_plan first to ensure tasks exist in Redis before spawn_agent references them.
	// Without this ordering, spawn_agent may call UpdateTaskStatus(T1, in_progress) before T1 is created.
	for _, action := range planDecision.Actions {
		if action.Type == "revise_plan" {
			for _, newTask := range action.Create {
				task := activities.SwarmTask{
					ID:          fmt.Sprintf("%v", newTask["id"]),
					Description: fmt.Sprintf("%v", newTask["description"]),
					CreatedBy:   "lead",
				}
				// Parse depends_on for task dependency enforcement
				if deps, ok := newTask["depends_on"].([]interface{}); ok {
					for _, d := range deps {
						task.DependsOn = append(task.DependsOn, fmt.Sprintf("%v", d))
					}
				}
				planTasks = append(planTasks, task)
			}
		}
	}
	if len(planTasks) > 0 {
		_ = workflow.ExecuteActivity(p2pCtx, constants.InitTaskListActivity, activities.InitTaskListInput{
			WorkflowID: workflowID,
			Tasks:      planTasks,
		}).Get(ctx, nil)
	}

	// Phase 1b: Process task description updates from revise_plan
	// Version-gated: this activity call didn't exist in older workflows.
	phase1DescV := workflow.GetVersion(ctx, "swarm_phase1_task_desc_update_v1", workflow.DefaultVersion, 1)
	if phase1DescV >= 1 {
		for _, action := range planDecision.Actions {
			if action.Type == "revise_plan" {
				for _, upd := range action.Update {
					taskID := fmt.Sprintf("%v", upd["id"])
					desc, _ := upd["description"].(string)
					if taskID != "" && desc != "" {
						_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskDescriptionActivity, activities.UpdateTaskDescriptionInput{
							WorkflowID:  workflowID,
							TaskID:      taskID,
							Description: desc,
						}).Get(ctx, nil)
					}
				}
			}
		}
	}

	// Phase 2: Process remaining actions (interim_reply, spawn_agent, etc.)
	for _, action := range planDecision.Actions {
		switch action.Type {
		case "revise_plan":
			continue // already handled in Phase 1
		case "interim_reply":
			if action.Content != "" {
				_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
					WorkflowID: workflowID,
					EventType:  activities.StreamEventLLMOutput,
					AgentID:    "swarm-lead",
					Message:    action.Content,
					Payload: map[string]interface{}{
						"role":    "lead",
						"interim": true,
					},
					Timestamp: workflow.Now(ctx),
				}).Get(ctx, nil)
			}
		case "spawn_agent":
			// Dependency guard: reject spawn for tasks with unmet depends_on
			if action.TaskID != "" && taskHasUnmetDeps(action.TaskID, planTasks) {
				logger.Warn("Rejecting spawn_agent: task has unmet dependencies — will be assigned when deps complete",
					"task_id", action.TaskID,
					"role", action.Role,
				)
			} else {
				spawnAgent(action.TaskDescription, action.Role, action.ModelTier, action.TaskID)
			}
		}
	}

	// Auto-link orphaned tasks to unlinked agents (defensive fix for Lead omitting task_id in spawn_agent).
	// Without this, tasks stay "pending" forever and block all downstream depends_on chains.
	autoLinkV := workflow.GetVersion(ctx, "swarm_auto_link_orphans_v1", workflow.DefaultVersion, 1)
	if autoLinkV == 1 {
		var unlinked []string
		for _, af := range agentFutures {
			if _, ok := agentTaskMap[af.ID]; !ok {
				unlinked = append(unlinked, af.ID)
			}
		}
		if len(unlinked) > 0 {
			// Find orphaned: pending tasks not claimed by any agent, no unmet deps
			claimed := make(map[string]bool)
			for _, tid := range agentTaskMap {
				claimed[tid] = true
			}
			var orphaned []string
			for _, t := range planTasks {
				if (t.Status == "" || t.Status == "pending") && !claimed[t.ID] && !taskHasUnmetDeps(t.ID, planTasks) {
					orphaned = append(orphaned, t.ID)
				}
			}
			if len(unlinked) == len(orphaned) && len(unlinked) > 0 {
				for i, agentID := range unlinked {
					taskID := orphaned[i]
					agentTaskMap[agentID] = taskID
					_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity,
						activities.UpdateTaskStatusInput{WorkflowID: workflowID, TaskID: taskID, Status: "in_progress", AgentID: agentID}).Get(ctx, nil)
					logger.Info("Auto-linked orphaned task to agent", "agent_id", agentID, "task_id", taskID)
				}
			} else if len(unlinked) > 0 {
				logger.Warn("Unlinked agents after initial plan, cannot auto-link (count mismatch)",
					"unlinked", len(unlinked), "orphaned", len(orphaned))
			}
		}
	}

	if len(agentFutures) == 0 {
		return TaskResult{Success: false, ErrorMessage: "Lead planning produced no agents to spawn"}, nil
	}

	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventProgress,
		AgentID:    "swarm-lead",
		Message:    activities.MsgSwarmSpawning(len(agentFutures)),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// ── Phase 2: Lead Event-Driven Monitoring ───────────────────────────────
	completedCount := 0
	totalExpected := len(agentFutures)
	checkpointInterval := 2 * time.Minute // Fix 2: reduced from 60s to 120s

	// Signal channel for idle agents — wakes Lead immediately when an agent goes idle
	agentIdleCh := workflow.GetSignalChannel(ctx, "agent-idle")

	// Signal channel for P2P message forwarding — agents notify parent when they send
	// a message, so parent can wake idle recipients via Temporal Signal (zero LLM cost).
	messageSentCh := workflow.GetSignalChannel(ctx, "agent-message-sent")

	// Signal channel for HITL (Human-in-the-Loop) — user can send messages to Lead mid-execution
	humanInputCh := workflow.GetSignalChannel(ctx, "human-input")

	// Throttle interim_reply emissions — suppress if <60s since last emit
	const interimThrottleSeconds = 60
	var lastInterimEmitTime time.Time

	// Track whether a HITL message has been received but not yet acted on
	// (i.e., Lead hasn't created tasks or revised plan in response).
	// Prevents FORCED CLOSE from skipping deferred user requests.
	// After 2 checkpoint rounds without action, give up to prevent infinite loop.
	hitlPendingAction := false
	hitlReminderCount := 0
	var hitlMessages []string // Accumulate all HITL messages for Lead context

	// Event-driven Lead loop (D1, D5: event coalescing)
	for completedCount < totalExpected {
		var event activities.LeadEvent
		gotCompletion := false

		sel := workflow.NewSelector(ctx)

		// Create cancellable timer context before handlers so closures can capture timerCancel
		timerCtx, timerCancel := workflow.WithCancel(ctx)

		// Agent idle notification — agent finished task and is waiting for reassignment
		sel.AddReceive(agentIdleCh, func(ch workflow.ReceiveChannel, more bool) {
			timerCancel() // cancel the checkpoint timer
			var idleInfo map[string]interface{}
			ch.Receive(ctx, &idleInfo)
			agentID, _ := idleInfo["agent_id"].(string)
			resultSummary, _ := idleInfo["result_summary"].(string)
			iterations, _ := idleInfo["iterations"].(float64) // JSON numbers → float64
			toolsUsed, _ := idleInfo["tools_used"].(string)
			completionReport, _ := idleInfo["completion_report"].(map[string]interface{})

			// Filter raw tool JSON from result_summary (P4 fix)
			if resultSummary != "" && looksLikeToolJSON(resultSummary) {
				logger.Warn("Filtering tool JSON from idle result_summary — agent likely output file content as text instead of calling file_write",
					"agent_id", agentID,
					"summary_length", len(resultSummary),
				)
				resultSummary = ""
			}

			logger.Info("Lead notified: agent went idle",
				"agent_id", agentID,
				"has_result", resultSummary != "",
				"iterations", int(iterations),
				"tools_used", toolsUsed,
			)

			// Update agent state to idle — preserve CurrentTask so Lead knows what was completed
			if s, ok := agentStates[agentID]; ok {
				s.Status = "idle"
				s.IterationsUsed = int(iterations)
				// Populate FilesWritten from completion_report so Lead can pass paths to synthesis_writer
				if filesRaw, ok := completionReport["files_written"]; ok {
					if files, ok := filesRaw.([]string); ok {
						s.FilesWritten = files
					} else if filesArr, ok := filesRaw.([]interface{}); ok {
						for _, f := range filesArr {
							if fs, ok := f.(string); ok {
								s.FilesWritten = append(s.FilesWritten, fs)
							}
						}
					}
				}
				agentStates[agentID] = s
			}

			// Mark the agent's current task as completed in TaskList.
			// This is critical for depends_on enforcement — downstream tasks check
			// dep status in TaskList, not agentStates.
			if taskID, ok := agentTaskMap[agentID]; ok && taskID != "" {
				idleTaskErr := workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
					WorkflowID: workflowID,
					TaskID:     taskID,
					Status:     "completed",
					AgentID:    agentID,
				}).Get(ctx, nil)
				if idleTaskErr != nil {
					logger.Error("Failed to mark task completed on agent idle",
						"agent_id", agentID, "task_id", taskID, "error", idleTaskErr)
				} else {
					logger.Info("Marked task completed on agent idle",
						"agent_id", agentID, "task_id", taskID)
				}
				delete(agentTaskMap, agentID)
			} else {
				logger.Warn("Agent idle but no task mapping found",
					"agent_id", agentID, "agentTaskMap_size", len(agentTaskMap))
			}

			// Save idle snapshot — if child workflow later times out, we still have this result
			if resultSummary != "" {
				idleSnapshots[agentID] = AgentLoopResult{
					AgentID:    agentID,
					Response:   resultSummary,
					Iterations: int(iterations),
					Success:    true,
					ToolsUsed:  toolsUsed,
				}
			}

			// Merge agent's team knowledge into global accumulator (L2 cross-agent dedup)
			if tkRaw, ok := idleInfo["team_knowledge"]; ok {
				if tkSlice, ok := tkRaw.([]interface{}); ok {
					var agentTK []activities.TeamKnowledgeEntry
					for _, item := range tkSlice {
						if m, ok := item.(map[string]interface{}); ok {
							agentTK = append(agentTK, activities.TeamKnowledgeEntry{
								URL:       fmt.Sprintf("%v", m["url"]),
								Agent:     fmt.Sprintf("%v", m["agent"]),
								Summary:   fmt.Sprintf("%v", m["summary"]),
								CharCount: int(toFloat64(m["char_count"])),
							})
						}
					}
					if len(agentTK) > 0 {
						swarmTeamKnowledge = mergeTeamKnowledge(swarmTeamKnowledge, agentTK)
						logger.Info("Merged team knowledge from idle agent",
							"agent_id", agentID,
							"new_entries", len(agentTK),
							"total_entries", len(swarmTeamKnowledge),
						)
					}
				}
			}

			event = activities.LeadEvent{
				Type:             "agent_idle",
				AgentID:          agentID,
				ResultSummary:    truncateDecisionSummary(resultSummary),
				CompletionReport: completionReport,
			}
		})

		// Agent completion events
		sel.AddReceive(completionCh, func(ch workflow.ReceiveChannel, more bool) {
			timerCancel() // cancel the checkpoint timer
			var result AgentLoopResult
			ch.Receive(ctx, &result)
			results[result.AgentID] = result
			completedCount++
			// Each AgentLoopStep iteration = exactly 1 LLM call.
			// Tool execution tokens (web_search results, etc.) are NOT counted here
			// because they're processed by agent-core, not the LLM service.
			budgetTotalLLMCalls += result.Iterations
			budgetTotalTokens += result.TokensUsed
			// Note: per-step LLM tokens and per-tool Haiku tokens are already recorded
			// inside AgentLoop (agent_step + tool_exec phases). No aggregate recording
			// here to avoid double-counting.

			// Update agent state
			agentStates[result.AgentID] = activities.LeadAgentState{
				AgentID:        result.AgentID,
				Status:         "completed",
				CurrentTask:    "",
				IterationsUsed: result.Iterations,
				Role:           result.Role,
			}

			// Merge completed agent's team knowledge into global accumulator
			if len(result.TeamKnowledge) > 0 {
				swarmTeamKnowledge = mergeTeamKnowledge(swarmTeamKnowledge, result.TeamKnowledge)
				logger.Info("Merged team knowledge from completed agent",
					"agent_id", result.AgentID,
					"new_entries", len(result.TeamKnowledge),
					"total_entries", len(swarmTeamKnowledge),
				)
			}

			resultSummary := truncateDecisionSummary(result.Response)
			if resultSummary == "" && result.Error != "" {
				resultSummary = fmt.Sprintf("Agent failed: %s", truncateDecisionSummary(result.Error))
			}
			event = activities.LeadEvent{
				Type:          "agent_completed",
				AgentID:       result.AgentID,
				ResultSummary: resultSummary,
				Error:         result.Error,
				Success:       result.Success,
			}
			gotCompletion = true

			// Sync TaskList: mark the agent's current task as completed
			if taskID, ok := agentTaskMap[result.AgentID]; ok && taskID != "" {
				_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
					WorkflowID: workflowID,
					TaskID:     taskID,
					Status:     "completed",
					AgentID:    result.AgentID,
				}).Get(ctx, nil)
				delete(agentTaskMap, result.AgentID)
			}

			// Clear child workflow ID to prevent signaling a dead workflow
			for i := range agentFutures {
				if agentFutures[i].ID == result.AgentID {
					agentFutures[i].WorkflowID = ""
					break
				}
			}

			logger.Info("Agent completed",
				"agent_id", result.AgentID,
				"success", result.Success,
				"completed", completedCount,
				"total", totalExpected,
			)
		})

		// Checkpoint timer (wake Lead periodically even if no events)
		// Uses cancellable timerCtx declared above to prevent leaked timer futures
		timerFuture := workflow.NewTimer(timerCtx, checkpointInterval)
		sel.AddFuture(timerFuture, func(f workflow.Future) {
			_ = f.Get(timerCtx, nil)
			if !gotCompletion {
				event = activities.LeadEvent{Type: "checkpoint"}
			}
		})

		// HITL: human user sends a message to Lead mid-execution
		sel.AddReceive(humanInputCh, func(ch workflow.ReceiveChannel, more bool) {
			timerCancel() // cancel the checkpoint timer
			var humanMsg map[string]string
			ch.Receive(ctx, &humanMsg)
			message := humanMsg["message"]
			logger.Info("Lead received human input", "message", message)
			hitlPendingAction = true
			hitlMessages = append(hitlMessages, message)

			event = activities.LeadEvent{
				Type:         "human_input",
				HumanMessage: message,
			}
		})

		sel.Select(ctx)

		// P2P message forwarding: drain buffered "agent-message-sent" signals and
		// forward wake-up signals to idle recipients. No Lead LLM call — pure Go routing.
		for {
			var msgInfo map[string]string
			if !messageSentCh.ReceiveAsync(&msgInfo) {
				break
			}
			targetID := msgInfo["to"]
			fromID := msgInfo["from"]
			if s, ok := agentStates[targetID]; ok && s.Status == "idle" {
				for _, af := range agentFutures {
					if af.ID == targetID && af.WorkflowID != "" {
						_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "",
							fmt.Sprintf("agent:%s:peer-message", targetID),
							map[string]string{"from": fromID},
						).Get(ctx, nil)
						logger.Info("Forwarded peer message to idle agent",
							"from", fromID, "to", targetID)
						break
					}
				}
			}
		}

		// Signal coalescing (P3 fix): drain any additional idle signals that arrived
		// simultaneously. ReceiveAsync is deterministic — only drains buffered signals.
		if event.Type == "agent_idle" {
			for {
				var extraIdle map[string]interface{}
				if !agentIdleCh.ReceiveAsync(&extraIdle) {
					break
				}
				extraAgentID, _ := extraIdle["agent_id"].(string)
				extraSummary, _ := extraIdle["result_summary"].(string)
				extraIter, _ := extraIdle["iterations"].(float64)
				extraToolsUsed, _ := extraIdle["tools_used"].(string)
				extraReport, _ := extraIdle["completion_report"].(map[string]interface{})

				if extraSummary != "" && looksLikeToolJSON(extraSummary) {
					logger.Warn("Filtering tool JSON from coalesced idle result_summary",
						"agent_id", extraAgentID,
						"summary_length", len(extraSummary),
					)
					extraSummary = ""
				}

				// Update agentStates for each drained signal (Codex finding #3)
				if s, ok := agentStates[extraAgentID]; ok {
					s.Status = "idle"
					s.IterationsUsed = int(extraIter)
					// Populate FilesWritten from completion_report (mirrors primary idle handler)
					if extraReport != nil {
						if filesRaw, ok := extraReport["files_written"]; ok {
							if files, ok := filesRaw.([]string); ok {
								s.FilesWritten = files
							} else if filesArr, ok := filesRaw.([]interface{}); ok {
								for _, f := range filesArr {
									if fs, ok := f.(string); ok {
										s.FilesWritten = append(s.FilesWritten, fs)
									}
								}
							}
						}
					}
					agentStates[extraAgentID] = s
				}
				// Merge coalesced agent's team knowledge into global accumulator (L2 dedup)
				if tkRaw, ok := extraIdle["team_knowledge"]; ok {
					if tkSlice, ok := tkRaw.([]interface{}); ok {
						var agentTK []activities.TeamKnowledgeEntry
						for _, item := range tkSlice {
							if m, ok := item.(map[string]interface{}); ok {
								agentTK = append(agentTK, activities.TeamKnowledgeEntry{
									URL:       fmt.Sprintf("%v", m["url"]),
									Agent:     fmt.Sprintf("%v", m["agent"]),
									Summary:   fmt.Sprintf("%v", m["summary"]),
									CharCount: int(toFloat64(m["char_count"])),
								})
							}
						}
						if len(agentTK) > 0 {
							swarmTeamKnowledge = mergeTeamKnowledge(swarmTeamKnowledge, agentTK)
							logger.Info("Merged team knowledge from coalesced idle agent",
								"agent_id", extraAgentID,
								"new_entries", len(agentTK),
								"total_entries", len(swarmTeamKnowledge),
							)
						}
					}
				}
				// Mark coalesced agent's task as completed in TaskList (mirrors primary idle handler)
				if taskID, ok := agentTaskMap[extraAgentID]; ok && taskID != "" {
					_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
						WorkflowID: workflowID,
						TaskID:     taskID,
						Status:     "completed",
						AgentID:    extraAgentID,
					}).Get(ctx, nil)
					delete(agentTaskMap, extraAgentID)
				}
				if extraSummary != "" {
					idleSnapshots[extraAgentID] = AgentLoopResult{
						AgentID:    extraAgentID,
						Response:   extraSummary,
						Iterations: int(extraIter),
						Success:    true,
						ToolsUsed:  extraToolsUsed,
					}
				}

				// Use first agent's completion report if primary doesn't have one
				if event.CompletionReport == nil && extraReport != nil {
					event.CompletionReport = extraReport
				}

				// Append per-agent detail to event summary (Codex finding #7)
				event.ResultSummary = fmt.Sprintf("%s\n---\n%s: %s",
					event.ResultSummary, extraAgentID, truncateDecisionSummary(extraSummary))
				event.AgentID = fmt.Sprintf("%s+%s", event.AgentID, extraAgentID)

				logger.Info("Coalesced idle signal", "agent_id", extraAgentID)
			}
		}

		// Budget check (D9) — force synthesis if exhausted
		elapsed = int(workflow.Now(ctx).Sub(swarmStartTime).Seconds())
		if budgetTotalLLMCalls >= maxLLMCalls || budgetTotalTokens >= maxTokens || elapsed >= maxWallClockSeconds {
			logger.Warn("Budget exhausted, forcing synthesis",
				"llm_calls", budgetTotalLLMCalls,
				"tokens", budgetTotalTokens,
				"elapsed_seconds", elapsed,
			)
			break
		}

		// Skip Lead LLM call if all agents are already done
		if completedCount >= totalExpected {
			break
		}

		// Skip Lead LLM call on checkpoint when there's nothing actionable
		if event.Type == "checkpoint" {
			hasIdleAgent := false
			hasRunningAgent := false
			for _, s := range agentStates {
				switch s.Status {
				case "idle":
					hasIdleAgent = true
				case "running":
					hasRunningAgent = true
				}
			}
			// Quick check TaskList for pending tasks (without LLM call)
			var checkTaskList []activities.SwarmTask
			_ = workflow.ExecuteActivity(p2pCtx, constants.GetTaskListActivity, activities.GetTaskListInput{
				WorkflowID: workflowID,
			}).Get(ctx, &checkTaskList)
			// B2: Only count unblocked pending tasks as actionable (replay-safe)
			checkpointDepsV := workflow.GetVersion(ctx, "checkpoint-deps-filter", workflow.DefaultVersion, 1)
			hasPendingTask := false
			if checkpointDepsV >= 1 {
				hasPendingTask = hasAssignablePendingTask(checkTaskList)
			} else {
				for _, t := range checkTaskList {
					if t.Status == "pending" {
						hasPendingTask = true
					}
				}
			}
			// Skip 1: all agents running, no idle to process, no pending tasks
			if !hasIdleAgent && !hasPendingTask {
				logger.Info("Checkpoint: no idle agents, no pending tasks — skipping Lead LLM call")
				continue
			}
			// Terminal state: all agents idle, none running, no pending tasks.
			// Lead already had its chance during agent_idle events — force wrap-up.
			// EXCEPTION: if there's an unresolved HITL message, give Lead another chance (max 2 rounds).
			if hasIdleAgent && !hasRunningAgent && !hasPendingTask {
				if hitlPendingAction && hitlReminderCount < 2 {
					hitlReminderCount++
					logger.Info("Terminal state but HITL pending — giving Lead another chance",
						"reminder", hitlReminderCount)
					// Fall through to normal LeadDecision below
				} else {
					logger.Info("Terminal state: all agents idle with no pending tasks — auto-completing")
					// Shutdown all non-completed agents
					for _, af := range agentFutures {
						if af.WorkflowID != "" {
							if s, ok := agentStates[af.ID]; ok && s.Status == "completed" {
								continue
							}
							_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "",
								fmt.Sprintf("agent:%s:shutdown", af.ID), "Auto-complete",
							).Get(ctx, nil)
							// Emit AGENT_COMPLETED so frontend can remove radar flight
							_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
								WorkflowID: workflowID,
								EventType:  activities.StreamEventAgentCompleted,
								AgentID:    af.ID,
								Message:    activities.MsgAgentCompleted(af.ID),
								Timestamp:  workflow.Now(ctx),
							}).Get(ctx, nil)
						}
					}
					// Populate results from idle snapshots (agents already did the work)
					for agentID, snap := range idleSnapshots {
						if _, exists := results[agentID]; !exists {
							results[agentID] = snap
							logger.Info("Using idle snapshot for agent result", "agent_id", agentID)
						}
					}
					goto synthesis
				}
			}
		}

		// Fetch current TaskList
		var currentTaskList []activities.SwarmTask
		_ = workflow.ExecuteActivity(p2pCtx, constants.GetTaskListActivity, activities.GetTaskListInput{
			WorkflowID: workflowID,
		}).Get(ctx, &currentTaskList)

		// Collect agent states — inject elapsed time for running agents
		now := workflow.Now(ctx)
		var statesList []activities.LeadAgentState
		for _, s := range agentStates {
			if s.Status == "running" {
				if startTime, ok := agentTaskStartTimes[s.AgentID]; ok {
					s.ElapsedSeconds = int(now.Sub(startTime).Seconds())
				}
			}
			statesList = append(statesList, s)
		}

		// Read Lead's mailbox — agents can send_message(to="lead") to escalate issues
		var leadMessages []activities.AgentMessage
		leadMailboxErr := workflow.ExecuteActivity(p2pCtx, constants.FetchAgentMessagesActivity,
			activities.FetchAgentMessagesInput{
				WorkflowID: workflowID,
				AgentID:    "lead",
				SinceSeq:   lastLeadMailboxSeq,
			}).Get(ctx, &leadMessages)

		var leadMsgMaps []map[string]interface{}
		if leadMailboxErr == nil {
			for _, m := range leadMessages {
				leadMsgMaps = append(leadMsgMaps, map[string]interface{}{
					"from":    m.From,
					"type":    string(m.Type),
					"payload": m.Payload,
				})
				if m.Seq > lastLeadMailboxSeq {
					lastLeadMailboxSeq = m.Seq
				}
			}
		}

		// Collect workspace file paths for Lead context — scan DISK so multi-turn
		// conversations see files from previous swarm runs, not just current run.
		var wsFileList []string
		{
			var diskFiles activities.ListWorkspaceFilesResult
			_ = workflow.ExecuteActivity(p2pCtx, constants.ListWorkspaceFilesActivity,
				activities.ListWorkspaceFilesInput{SessionID: input.SessionID},
			).Get(ctx, &diskFiles)
			seen := make(map[string]bool)
			for _, f := range diskFiles.Files {
				if !seen[f.Path] {
					wsFileList = append(wsFileList, f.Path)
					seen[f.Path] = true
				}
			}
			// Merge agent-reported files (in case disk scan missed in-flight writes)
			for _, s := range agentStates {
				for _, f := range s.FilesWritten {
					if !seen[f] {
						wsFileList = append(wsFileList, f)
						seen[f] = true
					}
				}
			}
		}

		// Lead decision with optional file_read / tool_call inner loop.
		// When Lead returns file_read or tool_call actions, Go executes them (zero LLM cost
		// for file_read; direct tool invocation for tool_call) and calls LeadDecision again
		// with results injected into the event.
		const maxFileReadRounds = 3
		var decision activities.LeadDecisionResult
		var leadDecisionOK bool
		fileReadRound := 0
		toolCallRound := 0
		for fileReadRound <= maxFileReadRounds || toolCallRound < maxToolCallRounds {
			// Refresh elapsed time each round so Lead gets accurate budget info
			elapsed = int(workflow.Now(ctx).Sub(swarmStartTime).Seconds())
			// Call Lead LLM for decision (D2: Temporal Activity ensures replay determinism)
			leadErr := workflow.ExecuteActivity(leadCtx, constants.LeadDecisionActivity, activities.LeadDecisionInput{
				WorkflowID:  workflowID,
				Event:       event,
				TaskList:    currentTaskList,
				AgentStates: statesList,
				Budget: activities.LeadBudget{
					TotalLLMCalls:       budgetTotalLLMCalls,
					RemainingLLMCalls:   maxLLMCalls - budgetTotalLLMCalls,
					TotalTokens:         budgetTotalTokens,
					RemainingTokens:     maxTokens - budgetTotalTokens,
					ElapsedSeconds:      elapsed,
					MaxWallClockSeconds: maxWallClockSeconds,
				},
				History:             leadHistory,
				Messages:            leadMsgMaps,
				OriginalQuery:       input.Query,
				ConversationHistory: conversationHistory,
				WorkspaceFiles:      wsFileList,
				HitlMessages:        hitlMessages,
			}).Get(ctx, &decision)

			if leadErr != nil {
				logger.Warn("Lead decision failed, continuing with checkpoint", "error", leadErr)
				break
			}

			// Track Lead decision in history
			budgetTotalLLMCalls++
			budgetTotalTokens += decision.TokensUsed

			// Record Lead decision token usage
			{
				recCtx := opts.WithTokenRecordOptions(ctx)
				provider := decision.Provider
				if provider == "" {
					provider = detectProviderFromModel(decision.ModelUsed)
				}
				_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
					UserID:              input.UserID,
					SessionID:           input.SessionID,
					TaskID:              workflowID,
					AgentID:             "swarm-lead",
					Model:               decision.ModelUsed,
					Provider:            provider,
					InputTokens:         decision.InputTokens,
					OutputTokens:        decision.OutputTokens,
					CacheReadTokens:     decision.CacheReadTokens,
					CacheCreationTokens: decision.CacheCreationTokens,
					Metadata:            map[string]interface{}{"workflow": "swarm", "phase": "lead_decision"},
				}).Get(ctx, nil)
			}

			leadHistory = append(leadHistory, map[string]interface{}{
				"decision_summary": decision.DecisionSummary,
				"event":            event.Type,
				"actions":          len(decision.Actions),
			})
			// Truncate leadHistory to prevent unbounded growth in long-running swarms.
			// Keep last 20 entries (up from 5) for better Lead decision context.
			const maxLeadHistory = 20
			if len(leadHistory) > maxLeadHistory {
				leadHistory = leadHistory[len(leadHistory)-maxLeadHistory:]
			}

			// Classify actions into file I/O, tool_call, and others (M1: include file_list)
			var fileIOActions []activities.LeadAction
			var toolCallActions []activities.LeadAction
			var otherActions []activities.LeadAction
			for _, action := range decision.Actions {
				if action.Type == "file_read" || action.Type == "file_list" {
					fileIOActions = append(fileIOActions, action)
				} else if leadToolCallV >= 1 && action.Type == "tool_call" {
					toolCallActions = append(toolCallActions, action)
				} else {
					otherActions = append(otherActions, action)
				}
			}

			// Execute file I/O AND tool_call in the same round (M3: no mutual exclusion)
			ranFileIO := false
			ranToolCalls := false

			// File I/O: file_read + file_list (zero LLM cost)
			if len(fileIOActions) > 0 && fileReadRound < maxFileReadRounds {
				fileReadRound++
				ranFileIO = true
				var fileContents []activities.FileReadResult
				for _, fr := range fileIOActions {
					if fr.Type == "file_list" {
						var listResult activities.ListWorkspaceFilesResult
						listErr := workflow.ExecuteActivity(p2pCtx, constants.ListWorkspaceFilesActivity,
							activities.ListWorkspaceFilesInput{SessionID: input.SessionID}).Get(ctx, &listResult)
						if listErr != nil {
							fileContents = append(fileContents, activities.FileReadResult{Path: ".", Error: listErr.Error()})
						} else {
							var listing string
							for _, f := range listResult.Files {
								listing += f.Path + "\n"
							}
							if listing == "" {
								listing = "(empty workspace)"
							}
							fileContents = append(fileContents, activities.FileReadResult{Path: ".", Content: listing})
						}
					} else if fr.Path != "" {
						var readResult activities.ReadWorkspaceFileResult
						readErr := workflow.ExecuteActivity(p2pCtx, constants.ReadWorkspaceFileActivity,
							activities.ReadWorkspaceFileInput{
								SessionID: input.SessionID, Path: fr.Path, MaxChars: 4000,
							}).Get(ctx, &readResult)
						if readErr != nil {
							fileContents = append(fileContents, activities.FileReadResult{Path: fr.Path, Error: readErr.Error()})
						} else {
							fileContents = append(fileContents, activities.FileReadResult{
								Path: readResult.Path, Content: readResult.Content,
								Truncated: readResult.Truncated, Error: readResult.Error,
							})
						}
					}
				}
				event.FileContents = fileContents
			}

			// Tool calls (zero LLM cost — direct tool invocation)
			if len(toolCallActions) > 0 && toolCallRound < maxToolCallRounds {
				toolCallRound++
				ranToolCalls = true
				var toolResults []activities.ToolResultEntry
				for _, tc := range toolCallActions {
					if !allowedLeadTools[tc.Tool] {
						logger.Warn("Lead requested disallowed tool, skipping", "tool", tc.Tool)
						toolResults = append(toolResults, activities.ToolResultEntry{
							Tool: tc.Tool, Error: fmt.Sprintf("tool %q not in allowlist", tc.Tool),
						})
						continue
					}
					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventLeadToolCall,
						AgentID:    "swarm-lead",
						Message:    fmt.Sprintf("Lead executing %s", tc.Tool),
						Payload: map[string]interface{}{
							"tool": tc.Tool, "phase": "lead_tool_call",
						},
						Timestamp: workflow.Now(ctx),
					}).Get(ctx, nil)
					var toolResult activities.LeadToolResult
					toolErr := workflow.ExecuteActivity(toolCtx, constants.LeadExecuteToolActivity,
						activities.LeadToolInput{
							Tool: tc.Tool, ToolParams: tc.ToolParams, SessionID: input.SessionID,
						}).Get(ctx, &toolResult)
					if toolErr != nil {
						toolResults = append(toolResults, activities.ToolResultEntry{Tool: tc.Tool, Error: toolErr.Error()})
					} else {
						output := toolResult.Output
						maxChars := 4000
						if tc.Tool == "calculator" {
							maxChars = 500
						}
						if len(output) > maxChars {
							output = output[:maxChars] + "\n... [truncated]"
						}
						toolResults = append(toolResults, activities.ToolResultEntry{
							Tool: tc.Tool, Output: output, Error: toolResult.Error,
						})
					}
					logger.Info("Lead tool_call executed", "tool", tc.Tool, "round", toolCallRound)
				}
				event.ToolResults = toolResults
			}

			// M2: clear stale fields — if only one type ran, nil out the other
			if ranFileIO && !ranToolCalls {
				event.ToolResults = nil
			}
			if ranToolCalls && !ranFileIO {
				event.FileContents = nil
			}

			// If any I/O was executed, loop back for another LeadDecision
			if ranFileIO || ranToolCalls {
				logger.Info("Lead I/O round complete, calling LeadDecision again",
					"fileReadRound", fileReadRound, "toolCallRound", toolCallRound)
				continue
			}

			// No file_read/file_list/tool_call actions (or max rounds reached) — replace decision.Actions
			// with otherActions (file_reads/tool_calls already handled) if we had any
			if len(fileIOActions) > 0 || len(toolCallActions) > 0 {
				decision.Actions = otherActions
			}
			leadDecisionOK = true
			break // Exit file_read loop, proceed to action processing
		}

		if !leadDecisionOK {
			continue // Lead decision failed or only had file_reads — wait for next event
		}

		logger.Info("Lead decision",
			"summary", decision.DecisionSummary,
			"actions", len(decision.Actions),
		)

		// Emit LEAD_DECISION for Radar visualization (gold pulse)
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventLeadDecision,
			AgentID:    "swarm-lead",
			Message:    decision.DecisionSummary,
			Payload: map[string]interface{}{
				"event_type": event.Type,
				"actions":    len(decision.Actions),
			},
		}).Get(ctx, nil)

		// Emit HITL_RESPONSE when Lead processes a human_input event
		if event.Type == "human_input" {
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventHITLResponse,
				AgentID:    "swarm-lead",
				Message:    decision.DecisionSummary,
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)
		}

		// Execute Lead actions
		for _, action := range decision.Actions {
			switch action.Type {
			case "spawn_agent":
				hitlPendingAction = false // Lead acted on user request
				if strings.TrimSpace(action.TaskDescription) == "" {
					logger.Warn("Lead spawn ignored: empty task description", "role", action.Role)
					continue
				}
				if spawnCount >= cfg.SwarmMaxAgents {
					logger.Warn("Lead spawn rejected: at capacity", "requested", action.TaskDescription)
					continue
				}
				// Dependency guard
				if action.TaskID != "" && taskHasUnmetDeps(action.TaskID, currentTaskList) {
					// Debug: log all task statuses for dependency diagnosis
					var depDebug []string
					for _, t := range currentTaskList {
						depDebug = append(depDebug, fmt.Sprintf("%s=%s", t.ID, t.Status))
					}
					logger.Warn("Lead spawn rejected: task has unmet dependencies",
						"task_id", action.TaskID, "role", action.Role,
						"task_statuses", strings.Join(depDebug, ", "))
					continue
				}
				prevCount := spawnCount
				spawnAgent(action.TaskDescription, action.Role, action.ModelTier, action.TaskID)
				if spawnCount > prevCount {
					totalExpected++
					// Auto-link: if Lead omitted task_id, try to find the unique orphaned task
					if autoLinkV == 1 && action.TaskID == "" {
						newAgent := agentFutures[len(agentFutures)-1].ID
						if _, ok := agentTaskMap[newAgent]; !ok {
							claimed := make(map[string]bool)
							for _, tid := range agentTaskMap {
								claimed[tid] = true
							}
							var candidates []string
							for _, t := range currentTaskList {
								if t.Status == "pending" && t.Owner == "" && !claimed[t.ID] && !taskHasUnmetDeps(t.ID, currentTaskList) {
									candidates = append(candidates, t.ID)
								}
							}
							if len(candidates) == 1 {
								agentTaskMap[newAgent] = candidates[0]
								_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity,
									activities.UpdateTaskStatusInput{WorkflowID: workflowID, TaskID: candidates[0], Status: "in_progress", AgentID: newAgent}).Get(ctx, nil)
								logger.Info("Auto-linked spawned agent (main loop)", "agent_id", newAgent, "task_id", candidates[0])
							} else {
								logger.Warn("Spawned agent without task_id, cannot auto-link",
									"agent_id", newAgent, "candidates", len(candidates))
							}
						}
					}
				}

			case "assign_task":
				hitlPendingAction = false // Lead acted on user request
				if action.AgentID != "" {
					// Dependency guard
					if action.TaskID != "" && taskHasUnmetDeps(action.TaskID, currentTaskList) {
						logger.Warn("Lead assign_task rejected: task has unmet dependencies",
							"task_id", action.TaskID, "agent_id", action.AgentID)
						break
					}
					taskDesc := action.TaskDescription
					if taskDesc == "" {
						for _, t := range currentTaskList {
							if t.ID == action.TaskID {
								taskDesc = t.Description
								break
							}
						}
					}

					// P1 fix: inject previous work context + tools-used summary
					// so agent skips re-orientation and doesn't repeat tool calls
					if snap, ok := idleSnapshots[action.AgentID]; ok && snap.Response != "" {
						prevContext := snap.Response
						if len(prevContext) > 1500 {
							prevContext = prevContext[:1500] + "..."
						}
						if snap.ToolsUsed != "" {
							taskDesc = fmt.Sprintf("PREVIOUS WORK CONTEXT:\n%s\n\nTOOLS ALREADY USED (do NOT repeat these):\n%s\n\nNEW INSTRUCTIONS:\n%s",
								prevContext, snap.ToolsUsed, taskDesc)
						} else {
							taskDesc = fmt.Sprintf("PREVIOUS WORK CONTEXT:\n%s\n\nNEW INSTRUCTIONS:\n%s",
								prevContext, taskDesc)
						}
					}

					// Inject workspace file registry so reassigned agent knows what files exist
					var fileEntries []activities.FileRegistryEntry
					fileRegErr := workflow.ExecuteActivity(p2pCtx, constants.GetFileRegistryActivity,
						activities.GetFileRegistryInput{WorkflowID: workflowID}).Get(ctx, &fileEntries)
					if fileRegErr == nil && len(fileEntries) > 0 {
						var fileList strings.Builder
						fileList.WriteString("\n\nWORKSPACE FILES (already created by team):\n")
						for _, f := range fileEntries {
							fmt.Fprintf(&fileList, "  %s (%d bytes, by %s)\n", f.Path, f.Size, f.Author)
						}
						taskDesc += fileList.String()
					}

					if action.TaskID != "" {
						_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
							WorkflowID: workflowID,
							TaskID:     action.TaskID,
							Status:     "in_progress",
							AgentID:    action.AgentID,
						}).Get(ctx, nil)
						agentTaskMap[action.AgentID] = action.TaskID
					}

					assignPayload := map[string]string{
						"description": taskDesc,
					}
					if action.ModelTier != "" {
						assignPayload["model_tier"] = action.ModelTier
					}
					for _, af := range agentFutures {
						if af.ID == action.AgentID && af.WorkflowID != "" {
							_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "", fmt.Sprintf("agent:%s:new-task", action.AgentID), assignPayload).Get(ctx, nil)
							break
						}
					}

					// Notify frontend that idle agent is active again
					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventAgentStarted,
						AgentID:    action.AgentID,
						Message:    fmt.Sprintf("Re-assigned: %s", truncateDecisionSummary(action.TaskDescription)),
						Payload: map[string]interface{}{
							"role": action.Role,
						},
					}).Get(ctx, nil)

					// Preserve IterationsUsed from prior work (read-modify-write)
					if s, ok := agentStates[action.AgentID]; ok {
						s.Status = "running"
						s.CurrentTask = taskDesc
						agentStates[action.AgentID] = s
					} else {
						agentStates[action.AgentID] = activities.LeadAgentState{
							AgentID:     action.AgentID,
							Status:      "running",
							CurrentTask: taskDesc,
						}
					}
					agentTaskStartTimes[action.AgentID] = workflow.Now(ctx)
					logger.Info("Lead assigned task to idle agent",
						"agent_id", action.AgentID,
						"task_id", action.TaskID,
						"task", taskDesc,
					)
				}

			case "send_message":
				if swarmP2pV >= 1 {
					_ = workflow.ExecuteActivity(p2pCtx, constants.SendAgentMessageActivity, activities.SendAgentMessageInput{
						WorkflowID: workflowID,
						From:       "lead",
						To:         action.To,
						Type:       activities.MessageTypeInfo,
						Payload:    map[string]interface{}{"message": action.Content},
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
				}
				// Wake idle recipient so it sees the Lead message immediately
				if s, ok := agentStates[action.To]; ok && s.Status == "idle" {
					for _, af := range agentFutures {
						if af.ID == action.To && af.WorkflowID != "" {
							_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "",
								fmt.Sprintf("agent:%s:peer-message", action.To),
								map[string]string{"from": "lead"},
							).Get(ctx, nil)
							break
						}
					}
				}

			case "broadcast":
				broadcastIDs := make([]string, 0, len(agentStates))
				for id := range agentStates {
					broadcastIDs = append(broadcastIDs, id)
				}
				sort.Strings(broadcastIDs)
				for _, id := range broadcastIDs {
					s := agentStates[id]
					if s.Status == "running" || s.Status == "idle" {
						if swarmP2pV >= 1 {
							_ = workflow.ExecuteActivity(p2pCtx, constants.SendAgentMessageActivity, activities.SendAgentMessageInput{
								WorkflowID: workflowID,
								From:       "lead",
								To:         s.AgentID,
								Type:       activities.MessageTypeInfo,
								Payload:    map[string]interface{}{"message": action.Content},
								Timestamp:  workflow.Now(ctx),
							}).Get(ctx, nil)
						}
						// Wake idle agents so they see the broadcast immediately
						if s.Status == "idle" {
							for _, af := range agentFutures {
								if af.ID == id && af.WorkflowID != "" {
									_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "",
										fmt.Sprintf("agent:%s:peer-message", id),
										map[string]string{"from": "lead"},
									).Get(ctx, nil)
									break
								}
							}
						}
					}
				}

			case "revise_plan":
				hitlPendingAction = false // Lead acted on user request
				for _, newTask := range action.Create {
					task := activities.SwarmTask{
						ID:          fmt.Sprintf("%v", newTask["id"]),
						Description: fmt.Sprintf("%v", newTask["description"]),
						CreatedBy:   "lead",
					}
					// Parse depends_on for task dependency enforcement (matches initial planning)
					if deps, ok := newTask["depends_on"].([]interface{}); ok {
						for _, d := range deps {
							task.DependsOn = append(task.DependsOn, fmt.Sprintf("%v", d))
						}
					}
					_ = workflow.ExecuteActivity(p2pCtx, constants.CreateTaskActivity, activities.CreateTaskInput{
						WorkflowID: workflowID,
						Task:       task,
					}).Get(ctx, nil)
				}
				for _, cancelID := range action.Cancel {
					_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
						WorkflowID: workflowID,
						TaskID:     cancelID,
						Status:     "completed",
						AgentID:    "lead",
					}).Get(ctx, nil)
				}
				// Update existing task descriptions
				// Version-gated: this activity call didn't exist in older workflows.
				phase2DescV := workflow.GetVersion(ctx, "swarm_phase2_task_desc_update_v1", workflow.DefaultVersion, 1)
				for _, upd := range action.Update {
					taskID := fmt.Sprintf("%v", upd["id"])
					desc, _ := upd["description"].(string)
					if taskID != "" && desc != "" {
						if phase2DescV >= 1 {
							_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskDescriptionActivity, activities.UpdateTaskDescriptionInput{
								WorkflowID:  workflowID,
								TaskID:      taskID,
								Description: desc,
							}).Get(ctx, nil)
						}
						// Sync in-memory currentTaskList (always, regardless of version)
						for i := range currentTaskList {
							if currentTaskList[i].ID == taskID {
								currentTaskList[i].Description = desc
								break
							}
						}
					}
				}

			case "shutdown_agent":
				if action.AgentID != "" {
					for _, af := range agentFutures {
						if af.ID == action.AgentID && af.WorkflowID != "" {
							_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "",
								fmt.Sprintf("agent:%s:shutdown", action.AgentID),
								"Lead shutdown",
							).Get(ctx, nil)
							break
						}
					}
					if s, ok := agentStates[action.AgentID]; ok {
						s.Status = "completed"
						agentStates[action.AgentID] = s
					}
					if taskID, ok := agentTaskMap[action.AgentID]; ok && taskID != "" {
						_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity,
							activities.UpdateTaskStatusInput{
								WorkflowID: workflowID,
								TaskID:     taskID,
								Status:     "completed",
								AgentID:    action.AgentID,
							}).Get(ctx, nil)
						delete(agentTaskMap, action.AgentID)
						// Sync in-memory currentTaskList so subsequent spawn_agent
						// dependency checks see the updated status (fixes stale snapshot race)
						for i := range currentTaskList {
							if currentTaskList[i].ID == taskID {
								currentTaskList[i].Status = "completed"
								break
							}
						}
					}
					// Emit AGENT_COMPLETED so frontend can remove radar flight
					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventAgentCompleted,
						AgentID:    action.AgentID,
						Message:    activities.MsgAgentCompleted(action.AgentID),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
					logger.Info("Lead shutdown individual agent", "agent_id", action.AgentID)
				}

			case "interim_reply":
				if action.Content != "" {
					now := workflow.Now(ctx)
					if lastInterimEmitTime.IsZero() || now.Sub(lastInterimEmitTime).Seconds() >= float64(interimThrottleSeconds) {
						lastInterimEmitTime = now
						_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
							WorkflowID: workflowID,
							EventType:  activities.StreamEventLLMOutput,
							AgentID:    "swarm-lead",
							Message:    action.Content,
							Payload: map[string]interface{}{
								"role":    "lead",
								"interim": true,
							},
							Timestamp: now,
						}).Get(ctx, nil)
					} else {
						logger.Info("Throttled interim_reply (too frequent)",
							"seconds_since_last", int(now.Sub(lastInterimEmitTime).Seconds()))
					}
				}

			case "noop":
				// Lead explicitly chose to wait — no action needed this round.
				logger.Info("Lead chose noop — waiting for running agents")

			case "done", "reply":
				// Guard: reject premature "done"/"reply" if any agent is still running.
				// LLM sometimes returns done/reply action while its decision_summary says "wait".
				hasRunning := false
				for _, s := range agentStates {
					if s.Status == "running" {
						hasRunning = true
						break
					}
				}
				if hasRunning {
					logger.Warn("Lead said done but agents still running — ignoring premature done",
						"running_agents", func() []string {
							var ids []string
							for _, s := range agentStates {
								if s.Status == "running" {
									ids = append(ids, s.AgentID)
								}
							}
							return ids
						}(),
					)
					continue // Skip done, wait for agents to finish
				}

				logger.Info("Lead decided: all tasks complete, shutting down agents")
				// Gracefully shutdown remaining agents (skip already-completed ones)
				for _, af := range agentFutures {
					if af.WorkflowID != "" {
						if s, ok := agentStates[af.ID]; ok && s.Status == "completed" {
							continue // Already shut down via shutdown_agent
						}
						_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "",
							fmt.Sprintf("agent:%s:shutdown", af.ID), "Lead completed",
						).Get(ctx, nil)
						// Emit AGENT_COMPLETED so frontend can remove radar flight
						_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
							WorkflowID: workflowID,
							EventType:  activities.StreamEventAgentCompleted,
							AgentID:    af.ID,
							Message:    activities.MsgAgentCompleted(af.ID),
							Timestamp:  workflow.Now(ctx),
						}).Get(ctx, nil)
					}
				}
				// Drain remaining results with timeout
				drainTimer := workflow.NewTimer(ctx, 60*time.Second)
				for completedCount < totalExpected {
					drainSel := workflow.NewSelector(ctx)
					drained := false
					drainSel.AddReceive(completionCh, func(ch workflow.ReceiveChannel, more bool) {
						var r AgentLoopResult
						ch.Receive(ctx, &r)
						results[r.AgentID] = r
						completedCount++
						// Sync TaskList (mirror of main loop logic)
						if taskID, ok := agentTaskMap[r.AgentID]; ok && taskID != "" {
							_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
								WorkflowID: workflowID,
								TaskID:     taskID,
								Status:     "completed",
								AgentID:    r.AgentID,
							}).Get(ctx, nil)
							delete(agentTaskMap, r.AgentID)
						}
					})
					drainSel.AddReceive(agentIdleCh, func(ch workflow.ReceiveChannel, more bool) {
						var m map[string]interface{}
						ch.Receive(ctx, &m) // discard
					})
					drainSel.AddFuture(drainTimer, func(f workflow.Future) {
						_ = f.Get(ctx, nil)
						drained = true
					})
					drainSel.Select(ctx)
					if drained {
						break
					}
				}
				goto synthesis
			}
		}

		// ContinueAsNew check (D6): prevent Temporal event limit
		if workflow.GetInfo(ctx).GetCurrentHistoryLength() > 8000 {
			logger.Warn("ContinueAsNew triggered: event count high",
				"events", workflow.GetInfo(ctx).GetCurrentHistoryLength(),
			)
			// For now, break and synthesize — full ContinueAsNew with snapshot in Phase 3.4
			historyTruncated = true
			break
		}
	}

	// C1 fix: drain running agents when exiting the outer loop via break
	// (budget exhaustion or ContinueAsNew). Without this, child workflows are
	// orphaned and their results are lost from synthesis.
	// Wrapped in {} to scope drainV — goto synthesis must not jump over declarations.
	{
		drainV := workflow.GetVersion(ctx, "budget-drain-v1", workflow.DefaultVersion, 1)
		if drainV >= 1 {
			// Shutdown all non-completed agents
			for _, af := range agentFutures {
				if af.WorkflowID != "" {
					if s, ok := agentStates[af.ID]; ok && s.Status == "completed" {
						continue
					}
					_ = workflow.SignalExternalWorkflow(ctx, af.WorkflowID, "",
						fmt.Sprintf("agent:%s:shutdown", af.ID), "Budget/history drain",
					).Get(ctx, nil)
					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventAgentCompleted,
						AgentID:    af.ID,
						Message:    activities.MsgAgentCompleted(af.ID),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
				}
			}
			// Drain results with timeout
			drainTimer := workflow.NewTimer(ctx, 30*time.Second)
			for completedCount < totalExpected {
				drainSel := workflow.NewSelector(ctx)
				drained := false
				drainSel.AddReceive(completionCh, func(ch workflow.ReceiveChannel, more bool) {
					var r AgentLoopResult
					ch.Receive(ctx, &r)
					results[r.AgentID] = r
					completedCount++
					if taskID, ok := agentTaskMap[r.AgentID]; ok && taskID != "" {
						_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
							WorkflowID: workflowID,
							TaskID:     taskID,
							Status:     "completed",
							AgentID:    r.AgentID,
						}).Get(ctx, nil)
						delete(agentTaskMap, r.AgentID)
					}
				})
				drainSel.AddReceive(agentIdleCh, func(ch workflow.ReceiveChannel, more bool) {
					var m map[string]interface{}
					ch.Receive(ctx, &m) // discard
				})
				drainSel.AddFuture(drainTimer, func(f workflow.Future) {
					_ = f.Get(ctx, nil)
					drained = true
				})
				drainSel.Select(ctx)
				if drained {
					break
				}
			}
			// Fill gaps from idle snapshots
			for agentID, snap := range idleSnapshots {
				if _, exists := results[agentID]; !exists {
					results[agentID] = snap
					logger.Info("Using idle snapshot for drain result", "agent_id", agentID)
				}
			}
		}
	}

synthesis:

	// Cleanup: mark any remaining pending/in_progress tasks as completed
	// so the TaskList doesn't have stale entries in Redis.
	{
		var remainingTasks []activities.SwarmTask
		_ = workflow.ExecuteActivity(p2pCtx, constants.GetTaskListActivity, activities.GetTaskListInput{
			WorkflowID: workflowID,
		}).Get(ctx, &remainingTasks)
		for _, t := range remainingTasks {
			if t.Status == "pending" || t.Status == "in_progress" {
				_ = workflow.ExecuteActivity(p2pCtx, constants.UpdateTaskStatusActivity, activities.UpdateTaskStatusInput{
					WorkflowID: workflowID,
					TaskID:     t.ID,
					Status:     "completed",
					AgentID:    "lead",
				}).Get(ctx, nil)
			}
		}
	}

	// Phase 4: Synthesize results
	logger.Info("SwarmWorkflow all agents completed", "result_count", len(results))

	// Log cache stats across all agents
	{
		var swarmCacheRead, swarmCacheCreation, swarmTotalInput int
		for _, r := range results {
			swarmCacheRead += r.CacheReadTokens
			swarmCacheCreation += r.CacheCreationTokens
			swarmTotalInput += r.InputTokens
		}
		if swarmCacheRead > 0 || swarmCacheCreation > 0 {
			logger.Info("Prompt cache stats",
				"workflow_id", workflowID,
				"cache_read_tokens", swarmCacheRead,
				"cache_creation_tokens", swarmCacheCreation,
				"total_input_tokens", swarmTotalInput,
			)
		}
	}

	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventProgress,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmSynthesizing(len(results)),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// Convert AgentLoopResults to AgentExecutionResults for synthesis
	var agentResults []activities.AgentExecutionResult
	var totalTokensUsed int
	synthIDs := make([]string, 0, len(results))
	for id := range results {
		synthIDs = append(synthIDs, id)
	}
	sort.Strings(synthIDs)
	for _, id := range synthIDs {
		r := results[id]
		agentResults = append(agentResults, activities.AgentExecutionResult{
			AgentID:      r.AgentID,
			Role:         r.Role,
			Response:     r.Response,
			TokensUsed:   r.TokensUsed,
			InputTokens:  r.InputTokens,
			OutputTokens: r.OutputTokens,
			ModelUsed:    r.ModelUsed,
			Provider:     r.Provider,
			Success:      r.Success,
			Error:        r.Error,
		})
		totalTokensUsed += r.TokensUsed
	}

	// Pre-synthesis guard: check if any agents produced usable results.
	// Agents that timed out or were cancelled may still have useful output.
	successCount := 0
	for i, r := range agentResults {
		if r.Success {
			successCount++
		} else if r.Response != "" {
			// Promote usable-but-failed agents for synthesis
			agentResults[i].Success = true
			successCount++
		}
	}
	if successCount == 0 {
		logger.Error("SwarmWorkflow all agents failed", "total_agents", len(agentResults))
		meta := buildSwarmMetadata(results)
		if historyTruncated {
			meta["truncated"] = true
			meta["truncation_reason"] = "temporal_history_limit"
		}
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("All %d agents failed — no results to synthesize", len(agentResults)),
			TokensUsed:   totalTokensUsed,
			Metadata:     meta,
		}, nil
	}

	// Note: single-agent results also go through closing_checkpoint below.
	// Agent idle summaries are internal status reports, not user-facing answers.

	// --- Lead Closing Checkpoint (with file I/O + tool_call loop) ---
	// Read workspace files for the closing summary
	var wsResult activities.ListWorkspaceFilesResult
	_ = workflow.ExecuteActivity(ctx, constants.ListWorkspaceFilesActivity, activities.ListWorkspaceFilesInput{
		SessionID: input.SessionID,
	}).Get(ctx, &wsResult)

	// Collect files written by agents in THIS run (to prioritize in closing summary)
	currentRunFiles := make(map[string]bool)
	for _, s := range agentStates {
		for _, f := range s.FilesWritten {
			currentRunFiles[f] = true
		}
	}

	closingSummary := buildClosingSummary(results, wsResult.Files, currentRunFiles)

	closingCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 150 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	})

	const maxClosingRounds = 5
	var closingDecision activities.LeadDecisionResult
	var closingErr error
	closingEvent := activities.LeadEvent{
		Type:          "closing_checkpoint",
		ResultSummary: closingSummary,
	}

	for closingRound := 0; closingRound <= maxClosingRounds; closingRound++ {
		closingElapsed := int(workflow.Now(ctx).Sub(swarmStartTime).Seconds())

		closingErr = workflow.ExecuteActivity(closingCtx, constants.LeadDecisionActivity, activities.LeadDecisionInput{
			WorkflowID: workflowID,
			Event:      closingEvent,
			TaskList:   make([]activities.SwarmTask, 0),
			AgentStates: make([]activities.LeadAgentState, 0),
			Budget: activities.LeadBudget{
				TotalLLMCalls:       budgetTotalLLMCalls,
				RemainingLLMCalls:   maxLLMCalls - budgetTotalLLMCalls,
				TotalTokens:         budgetTotalTokens,
				RemainingTokens:     maxTokens - budgetTotalTokens,
				ElapsedSeconds:      closingElapsed,
				MaxWallClockSeconds: maxWallClockSeconds,
			},
			History:             leadHistory,
			OriginalQuery:       input.Query,
			ConversationHistory: conversationHistory,
			HitlMessages:        hitlMessages,
		}).Get(ctx, &closingDecision)

		if closingErr != nil {
			logger.Warn("Lead closing_checkpoint failed, falling back to synthesis", "error", closingErr, "round", closingRound)
			break
		}

		budgetTotalLLMCalls++
		budgetTotalTokens += closingDecision.TokensUsed

		// Record Lead closing decision token usage
		{
			recCtx := opts.WithTokenRecordOptions(ctx)
			provider := closingDecision.Provider
			if provider == "" {
				provider = detectProviderFromModel(closingDecision.ModelUsed)
			}
			_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
				UserID:              input.UserID,
				SessionID:           input.SessionID,
				TaskID:              workflowID,
				AgentID:             "swarm-lead",
				Model:               closingDecision.ModelUsed,
				Provider:            provider,
				InputTokens:         closingDecision.InputTokens,
				OutputTokens:        closingDecision.OutputTokens,
				CacheReadTokens:     closingDecision.CacheReadTokens,
				CacheCreationTokens: closingDecision.CacheCreationTokens,
				Metadata:            map[string]interface{}{"workflow": "swarm", "phase": "closing_decision"},
			}).Get(ctx, nil)
		}

		leadHistory = append(leadHistory, map[string]interface{}{
			"decision_summary": closingDecision.DecisionSummary,
			"event":            "closing_checkpoint",
			"actions":          len(closingDecision.Actions),
		})

		// Separate actions into file I/O, tool_call, and terminal actions
		var fileIOActions []activities.LeadAction
		var toolCallActions []activities.LeadAction
		var terminalActions []activities.LeadAction
		for _, action := range closingDecision.Actions {
			switch action.Type {
			case "file_read", "file_list":
				fileIOActions = append(fileIOActions, action)
			case "tool_call":
				if leadToolCallV >= 1 {
					toolCallActions = append(toolCallActions, action)
				}
			case "reply", "synthesize", "done":
				terminalActions = append(terminalActions, action)
			}
		}

		// Execute file I/O actions — zero LLM cost
		if len(fileIOActions) > 0 && closingRound < maxClosingRounds {
			var fileContents []activities.FileReadResult
			for _, fr := range fileIOActions {
				if fr.Type == "file_list" {
					var listResult activities.ListWorkspaceFilesResult
					listErr := workflow.ExecuteActivity(p2pCtx, constants.ListWorkspaceFilesActivity,
						activities.ListWorkspaceFilesInput{SessionID: input.SessionID}).Get(ctx, &listResult)
					if listErr != nil {
						fileContents = append(fileContents, activities.FileReadResult{Path: ".", Error: listErr.Error()})
					} else {
						var listing string
						for _, f := range listResult.Files {
							listing += f.Path + "\n"
						}
						if listing == "" {
							listing = "(empty workspace)"
						}
						fileContents = append(fileContents, activities.FileReadResult{Path: ".", Content: listing})
					}
				} else if fr.Path != "" {
					var readResult activities.ReadWorkspaceFileResult
					readErr := workflow.ExecuteActivity(p2pCtx, constants.ReadWorkspaceFileActivity,
						activities.ReadWorkspaceFileInput{
							SessionID: input.SessionID, Path: fr.Path, MaxChars: 8000,
						}).Get(ctx, &readResult)
					if readErr != nil {
						fileContents = append(fileContents, activities.FileReadResult{Path: fr.Path, Error: readErr.Error()})
					} else {
						fileContents = append(fileContents, activities.FileReadResult{
							Path: readResult.Path, Content: readResult.Content,
							Truncated: readResult.Truncated, Error: readResult.Error,
						})
					}
				}
			}
			closingEvent.FileContents = fileContents
			closingEvent.ToolResults = nil
			logger.Info("Lead closing file I/O complete, calling LeadDecision again",
				"round", closingRound, "files", len(fileContents))
			continue
		}

		// Execute tool_call actions
		if len(toolCallActions) > 0 && closingRound < maxClosingRounds {
			var toolResults []activities.ToolResultEntry
			for _, tc := range toolCallActions {
				if !allowedLeadTools[tc.Tool] {
					toolResults = append(toolResults, activities.ToolResultEntry{
						Tool: tc.Tool, Error: fmt.Sprintf("tool %q not in allowlist", tc.Tool),
					})
					continue
				}
				var toolResult activities.LeadToolResult
				toolErr := workflow.ExecuteActivity(toolCtx, constants.LeadExecuteToolActivity,
					activities.LeadToolInput{
						Tool: tc.Tool, ToolParams: tc.ToolParams, SessionID: input.SessionID,
					}).Get(ctx, &toolResult)
				if toolErr != nil {
					toolResults = append(toolResults, activities.ToolResultEntry{Tool: tc.Tool, Error: toolErr.Error()})
				} else {
					output := toolResult.Output
					if len(output) > 4000 {
						output = output[:4000] + "\n... [truncated]"
					}
					toolResults = append(toolResults, activities.ToolResultEntry{
						Tool: tc.Tool, Output: output, Error: toolResult.Error,
					})
				}
				logger.Info("Lead closing tool_call executed", "tool", tc.Tool, "round", closingRound)
			}
			closingEvent.ToolResults = toolResults
			closingEvent.FileContents = nil
			logger.Info("Lead closing tool_call round complete",
				"round", closingRound, "tools_executed", len(toolResults))
			continue
		}

		// Handle terminal actions (reply / synthesize / done)
		for _, action := range terminalActions {
			switch action.Type {
			case "reply":
				replyContent := action.Content
				if isLeadReplyValid(replyContent, wsResult.Files) {
					logger.Info("SwarmWorkflow using Lead reply",
						"reply_len", len(replyContent),
						"tokens", closingDecision.TokensUsed,
					)

					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventLLMOutput,
						AgentID:    "swarm-supervisor",
						Message:    replyContent,
						Timestamp:  workflow.Now(ctx),
						Payload: map[string]interface{}{
							"tokens_used": closingDecision.TokensUsed,
							"model_used":  closingDecision.ModelUsed,
							"provider":    closingDecision.Provider,
						},
					}).Get(ctx, nil)

					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventDataProcessing,
						AgentID:    "swarm-supervisor",
						Message:    fmt.Sprintf("Used %s (~%d tokens)", closingDecision.ModelUsed, closingDecision.TokensUsed),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)

					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventDataProcessing,
						AgentID:    "swarm-supervisor",
						Message:    "Final answer ready",
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)

					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventWorkflowCompleted,
						AgentID:    "swarm-supervisor",
						Message:    activities.MsgSwarmCompleted(),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)

					totalTokensUsed += closingDecision.TokensUsed

					// User-level memory extraction (best-effort, awaited to prevent child workflow cancellation)
					if input.UserID != "" && len(replyContent) >= 500 {
						memCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
							StartToCloseTimeout: 60 * time.Second,
							RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
						})
						_ = workflow.ExecuteActivity(memCtx, activities.ExtractMemoryActivity, activities.MemoryExtractInput{
							UserID:           input.UserID,
							TenantID:         input.TenantID,
							SessionID:        input.SessionID,
							Query:            input.Query,
							Result:           replyContent,
							ParentWorkflowID: workflowID,
						}).Get(ctx, nil)
					}

					replyMeta := buildSwarmMetadata(results)
					if historyTruncated {
						replyMeta["truncated"] = true
						replyMeta["truncation_reason"] = "temporal_history_limit"
					}
					return TaskResult{
						Result:     replyContent,
						Success:    true,
						TokensUsed: totalTokensUsed,
						Metadata:   replyMeta,
					}, nil
				}
				logger.Warn("Lead reply failed validation, falling back to synthesis",
					"reply_len", len(replyContent),
					"files", len(wsResult.Files),
				)

			case "synthesize":
				logger.Info("Lead requested synthesis")

			case "done":
				logger.Info("Lead returned done (backward compat), using synthesis")
			}
		}
		break // terminal action processed (or no actions left), fall through to synthesis
	}

	// LLM synthesis — use medium model tier for swarm (agents already did heavy lifting)
	synthContext := make(map[string]interface{}, len(input.Context)+1)
	for k, v := range input.Context {
		synthContext[k] = v
	}
	if _, exists := synthContext["synthesis_model_tier"]; !exists {
		synthContext["synthesis_model_tier"] = "medium"
	}

	var synth activities.SynthesisResult
	if err := workflow.ExecuteActivity(ctx, activities.SynthesizeResultsLLM, activities.SynthesisInput{
		Query:            input.Query,
		AgentResults:     agentResults,
		Context:          synthContext,
		ParentWorkflowID: workflowID,
		SessionID:        input.SessionID,
	}).Get(ctx, &synth); err != nil {
		meta := buildSwarmMetadata(results)
		if historyTruncated {
			meta["truncated"] = true
			meta["truncation_reason"] = "temporal_history_limit"
		}
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Synthesis failed: %v", err),
			TokensUsed:   totalTokensUsed,
			Metadata:     meta,
		}, err
	}

	totalTokensUsed += synth.TokensUsed

	// Record synthesis token usage
	{
		recCtx := opts.WithTokenRecordOptions(ctx)
		provider := synth.Provider
		if provider == "" {
			provider = detectProviderFromModel(synth.ModelUsed)
		}
		_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
			UserID:       input.UserID,
			SessionID:    input.SessionID,
			TaskID:       workflowID,
			AgentID:      "swarm-synthesis",
			Model:        synth.ModelUsed,
			Provider:     provider,
			InputTokens:  synth.InputTokens,
			OutputTokens: synth.CompletionTokens,
			Metadata:     map[string]interface{}{"workflow": "swarm", "phase": "synthesis"},
		}).Get(ctx, nil)
	}

	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "swarm-supervisor",
		Message:    activities.MsgSwarmCompleted(),
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	// User-level memory extraction (best-effort, awaited to prevent child workflow cancellation)
	if input.UserID != "" && len(synth.FinalResult) >= 500 {
		memCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 60 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		_ = workflow.ExecuteActivity(memCtx, activities.ExtractMemoryActivity, activities.MemoryExtractInput{
			UserID:           input.UserID,
			TenantID:         input.TenantID,
			SessionID:        input.SessionID,
			Query:            input.Query,
			Result:           synth.FinalResult,
			ParentWorkflowID: workflowID,
		}).Get(ctx, nil)
	}

	meta := buildSwarmMetadata(results)
	if historyTruncated {
		meta["truncated"] = true
		meta["truncation_reason"] = "temporal_history_limit"
	}
	return TaskResult{
		Result:     synth.FinalResult,
		Success:    true,
		TokensUsed: totalTokensUsed,
		Metadata:   meta,
	}, nil
}

// buildSwarmMetadata builds metadata from swarm agent results.
func buildSwarmMetadata(results map[string]AgentLoopResult) map[string]interface{} {
	agentSummaries := make([]map[string]interface{}, 0, len(results))
	metaIDs := make([]string, 0, len(results))
	for id := range results {
		metaIDs = append(metaIDs, id)
	}
	sort.Strings(metaIDs)
	var totalInput, totalOutput, totalCacheRead, totalCacheCreation int
	var totalCost float64
	for _, id := range metaIDs {
		r := results[id]
		agentSummaries = append(agentSummaries, map[string]interface{}{
			"agent_id":   r.AgentID,
			"iterations": r.Iterations,
			"tokens":     r.TokensUsed,
			"success":    r.Success,
			"model":      r.ModelUsed,
		})
		totalInput += r.InputTokens
		totalOutput += r.OutputTokens
		totalCacheRead += r.CacheReadTokens
		totalCacheCreation += r.CacheCreationTokens
		totalCost += pricing.CostForSplitWithCache(
			r.ModelUsed, r.InputTokens, r.OutputTokens,
			r.CacheReadTokens, r.CacheCreationTokens, r.Provider,
		)
	}

	// Use flat keys only — nested structures (like agentSummaries) cause
	// Temporal JSON codec to fail deserializing map[string]interface{},
	// resulting in empty metadata and zero cost/tokens in persistence.
	meta := map[string]interface{}{
		"workflow_type":          "swarm",
		"total_agents":          len(results),
		"num_agents":            len(results),
		"input_tokens":          totalInput,
		"output_tokens":         totalOutput,
		"total_tokens":          totalInput + totalOutput,
		"cache_read_tokens":     totalCacheRead,
		"cache_creation_tokens": totalCacheCreation,
		"cost_usd":              totalCost,
	}
	return meta
}
