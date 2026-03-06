package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"go.temporal.io/sdk/activity"
)

// WorkspaceSnippet is a truncated KV workspace entry for prompt injection.
type WorkspaceSnippet struct {
	Author string `json:"author"`
	Data   string `json:"data"`
	Seq    uint64 `json:"seq"`
}

// TeamKnowledgeEntry records a URL fetched by any swarm agent for cross-agent dedup.
type TeamKnowledgeEntry struct {
	URL       string `json:"url"`
	Agent     string `json:"agent"`
	Summary   string `json:"summary"`
	CharCount int    `json:"char_count"`
}

// TeamMemberInfo describes a teammate for prompt injection.
type TeamMemberInfo struct {
	AgentID string `json:"agent_id"`
	Task    string `json:"task"`
	Role    string `json:"role,omitempty"`
}

// AgentLoopStepInput is the input for a single reason-act iteration of an autonomous agent.
type AgentLoopStepInput struct {
	AgentID         string                 `json:"agent_id"`
	WorkflowID      string                 `json:"workflow_id"`
	Task            string                 `json:"task"`
	Iteration       int                    `json:"iteration"`
	MaxIterations   int                    `json:"max_iterations,omitempty"` // Total iterations for budget display
	Messages        []AgentMailboxMsg      `json:"messages,omitempty"`       // Inbox messages from other agents
	History         []AgentLoopTurn        `json:"history,omitempty"`        // Previous turns in this agent's loop
	Context         map[string]interface{} `json:"context,omitempty"`
	SessionID       string                 `json:"session_id,omitempty"`
	TeamRoster      []TeamMemberInfo       `json:"team_roster,omitempty"`    // Teammates and their tasks
	WorkspaceData   []WorkspaceSnippet     `json:"workspace_data,omitempty"` // Recent KV workspace entries
	SuggestedTools  []string               `json:"suggested_tools,omitempty"`  // Tools available to this agent
	RoleDescription string                 `json:"role_description,omitempty"` // e.g. "financial research specialist"
	Role            string                 `json:"role,omitempty"`             // Persona role: researcher, coder, analyst, generalist
	TaskList        []SwarmTask            `json:"task_list,omitempty"`        // Current TaskList state for prompt injection
	ModelTier          string                 `json:"model_tier,omitempty"`          // "small", "medium", "large" (empty = default)
	PreviousResponseID string                 `json:"previous_response_id,omitempty"` // OpenAI Responses API: chain from previous response
	SystemMessage      string                 `json:"system_message,omitempty"`       // Urgent directive appended to user prompt end (recency bias)
	RunningNotes       string                 `json:"running_notes,omitempty"`        // Agent's cumulative notes — survives history truncation
	IsSwarm            bool                   `json:"is_swarm,omitempty"`             // True when running inside SwarmWorkflow (enables done→idle mapping)
	CumulativeToolCalls int                   `json:"cumulative_tool_calls,omitempty"` // Total tool_calls across all tasks (survives reassignment)
	TeamKnowledge      []TeamKnowledgeEntry   `json:"team_knowledge,omitempty"`       // URLs already fetched by other agents (L1 dedup)
	OriginalQuery      string                 `json:"original_query,omitempty"`       // User's original question for agent context
}

// AgentMailboxMsg is a message received from another agent's mailbox.
type AgentMailboxMsg struct {
	From    string                 `json:"from"`
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// AgentLoopTurn records a previous action in the agent's loop for context.
type AgentLoopTurn struct {
	Iteration       int         `json:"iteration"`
	Action          string      `json:"action"`
	Result          interface{} `json:"result,omitempty"`
	DecisionSummary string      `json:"decision_summary,omitempty"`
}

// AgentLoopStepResult is the LLM's decision for one iteration.
type AgentLoopStepResult struct {
	Action          string `json:"action"`                     // "tool_call", "send_message", "publish_data", "request_help", "done"
	DecisionSummary string `json:"decision_summary,omitempty"` // 1-3 sentence reasoning (D7: controlled, not unlimited chain-of-thought)

	// tool_call fields
	Tool       string                 `json:"tool,omitempty"`
	ToolParams map[string]interface{} `json:"tool_params,omitempty"`
	ToolResult interface{}            `json:"tool_result,omitempty"` // Populated after execution

	// send_message fields
	To          string                 `json:"to,omitempty"`
	MessageType string                 `json:"message_type,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`

	// publish_data fields
	Topic string `json:"topic,omitempty"`
	Data  string `json:"data,omitempty"`

	// request_help fields
	HelpDescription string   `json:"help_description,omitempty"`
	HelpSkills      []string `json:"help_skills,omitempty"`

	// complete_task / claim_task fields
	TaskID string `json:"task_id,omitempty"` // TaskList entry to mark completed or claimed

	// create_task fields
	TaskDescription string `json:"task_description,omitempty"`

	// done fields
	Response string `json:"response,omitempty"`

	// LLM usage metadata
	TokensUsed          int    `json:"tokens_used,omitempty"`
	InputTokens         int    `json:"input_tokens,omitempty"`
	OutputTokens        int    `json:"output_tokens,omitempty"`
	CacheReadTokens     int    `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int    `json:"cache_creation_tokens,omitempty"`
	ModelUsed           string `json:"model_used,omitempty"`
	Provider            string `json:"provider,omitempty"`
	ResponseID          string                 `json:"response_id,omitempty"` // OpenAI Responses API ID for chaining
	CompletionReport    map[string]interface{} `json:"completion_report,omitempty"`
	Notes               string                 `json:"notes,omitempty"` // Agent's updated running notes for next iteration
}

// AgentLoopStep calls the Python LLM service's /agent/loop endpoint
// to get the next action for a persistent agent.
func AgentLoopStep(ctx context.Context, in AgentLoopStepInput) (AgentLoopStepResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("AgentLoopStep called", "agent_id", in.AgentID, "iteration", in.Iteration, "task_len", len(in.Task))

	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/agent/loop", base)

	body, err := json.Marshal(in)
	if err != nil {
		return AgentLoopStepResult{}, fmt.Errorf("failed to marshal agent loop input: %w", err)
	}

	timeoutSec := 180 // /agent/loop includes LLM call + tool execution + interpretation pass
	if v := os.Getenv("AGENT_LOOP_STEP_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSec = n
		}
	}

	client := &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return AgentLoopStepResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return AgentLoopStepResult{}, fmt.Errorf("agent loop step HTTP call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Error("AgentLoopStep HTTP error", "agent_id", in.AgentID, "status", resp.StatusCode, "body", string(bodyBytes))
		return AgentLoopStepResult{}, fmt.Errorf("agent loop step returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var out AgentLoopStepResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return AgentLoopStepResult{}, fmt.Errorf("failed to decode agent loop step response: %w", err)
	}

	logger.Info("AgentLoopStep completed", "agent_id", in.AgentID, "iteration", in.Iteration, "action", out.Action, "tokens", out.TokensUsed)
	return out, nil
}
