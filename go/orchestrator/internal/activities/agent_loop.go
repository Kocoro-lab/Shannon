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

// TeamMemberInfo describes a teammate for prompt injection.
type TeamMemberInfo struct {
	AgentID string `json:"agent_id"`
	Task    string `json:"task"`
}

// AgentLoopStepInput is the input for a single reason-act iteration of an autonomous agent.
type AgentLoopStepInput struct {
	AgentID       string                 `json:"agent_id"`
	WorkflowID    string                 `json:"workflow_id"`
	Task          string                 `json:"task"`
	Iteration     int                    `json:"iteration"`
	MaxIterations int                    `json:"max_iterations,omitempty"` // Total iterations for budget display
	Messages      []AgentMailboxMsg      `json:"messages,omitempty"`       // Inbox messages from other agents
	History       []AgentLoopTurn        `json:"history,omitempty"`        // Previous turns in this agent's loop
	Context       map[string]interface{} `json:"context,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	TeamRoster    []TeamMemberInfo       `json:"team_roster,omitempty"`    // Teammates and their tasks
	WorkspaceData []WorkspaceSnippet     `json:"workspace_data,omitempty"` // Recent KV workspace entries
}

// AgentMailboxMsg is a message received from another agent's mailbox.
type AgentMailboxMsg struct {
	From    string                 `json:"from"`
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// AgentLoopTurn records a previous action in the agent's loop for context.
type AgentLoopTurn struct {
	Iteration int         `json:"iteration"`
	Action    string      `json:"action"`
	Result    interface{} `json:"result,omitempty"`
}

// AgentLoopStepResult is the LLM's decision for one iteration.
type AgentLoopStepResult struct {
	Action string `json:"action"` // "tool_call", "send_message", "publish_data", "request_help", "done"

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

	// done fields
	Response string `json:"response,omitempty"`

	// LLM usage metadata
	TokensUsed   int    `json:"tokens_used,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	ModelUsed    string `json:"model_used,omitempty"`
	Provider     string `json:"provider,omitempty"`
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

	timeoutSec := 60
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
