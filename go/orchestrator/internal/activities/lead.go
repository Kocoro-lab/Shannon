package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	"go.temporal.io/sdk/activity"
)

// ── Lead Decision Types ─────────────────────────────────────────────────────

// LeadEvent describes what triggered the Lead to wake up.
type LeadEvent struct {
	Type             string                 `json:"type"` // "agent_completed", "agent_idle", "help_request", "checkpoint", "human_input"
	AgentID          string                 `json:"agent_id"`
	ResultSummary    string                 `json:"result_summary"`
	HumanMessage     string                 `json:"human_message,omitempty"`
	CompletionReport map[string]interface{} `json:"completion_report,omitempty"`
	Error            string                 `json:"error,omitempty"`
	Success          bool                   `json:"success,omitempty"`
	FileContents     []FileReadResult       `json:"file_contents,omitempty"` // Lead file_read results
	ToolResults      []ToolResultEntry      `json:"tool_results,omitempty"`  // Lead tool_call results
}

// FileReadResult holds the content of a file read by Lead.
type FileReadResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
}

// ToolResultEntry holds the result of a tool executed by Lead.
type ToolResultEntry struct {
	Tool   string `json:"tool"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// LeadAgentState tracks an agent's current state for Lead decisions.
type LeadAgentState struct {
	AgentID        string   `json:"agent_id"`
	Status         string   `json:"status"`          // "running", "idle", "completed"
	CurrentTask    string   `json:"current_task"`
	IterationsUsed int      `json:"iterations_used"`
	ElapsedSeconds int      `json:"elapsed_seconds,omitempty"` // Wall-clock time since agent started current task
	Role           string   `json:"role,omitempty"`             // Agent's assigned role (researcher, analyst, synthesis_writer, etc.)
	FilesWritten   []string `json:"files_written,omitempty"`    // Files written by agent (for Lead to pass to synthesis_writer)
}

// LeadBudget tracks global budget for the swarm.
type LeadBudget struct {
	TotalLLMCalls      int `json:"total_llm_calls"`
	RemainingLLMCalls  int `json:"remaining_llm_calls"`
	TotalTokens        int `json:"total_tokens"`
	RemainingTokens    int `json:"remaining_tokens"`
	ElapsedSeconds     int `json:"elapsed_seconds"`
	MaxWallClockSeconds int `json:"max_wall_clock_seconds"`
}

// LeadDecisionInput is the input to the LeadDecision activity.
type LeadDecisionInput struct {
	WorkflowID    string                   `json:"workflow_id"`
	Event         LeadEvent                `json:"event"`
	TaskList      []SwarmTask              `json:"task_list"`
	AgentStates   []LeadAgentState         `json:"agent_states"`
	Budget        LeadBudget               `json:"budget"`
	History       []map[string]interface{} `json:"history"`                    // Recent Lead decisions
	Messages            []map[string]interface{} `json:"messages,omitempty"`              // Agent→Lead mailbox messages
	OriginalQuery       string                   `json:"original_query,omitempty"`        // User's original query (for language context)
	ConversationHistory []map[string]interface{} `json:"conversation_history,omitempty"`   // Session history for multi-turn context
	WorkspaceFiles      []string                 `json:"workspace_files,omitempty"`        // File paths in workspace (for planning context)
	HitlMessages        []string                 `json:"hitl_messages,omitempty"`           // All HITL messages received during this execution
}

// LeadAction is a single action the Lead wants to take.
type LeadAction struct {
	Type            string                   `json:"type"` // assign_task, spawn_agent, send_message, broadcast, revise_plan, file_read, done
	TaskID          string                   `json:"task_id,omitempty"`
	AgentID         string                   `json:"agent_id,omitempty"`
	Role            string                   `json:"role,omitempty"`
	TaskDescription string                   `json:"task_description,omitempty"`
	To              string                   `json:"to,omitempty"`
	Content         string                   `json:"content,omitempty"`
	ModelTier       string                   `json:"model_tier,omitempty"` // small, medium, large
	Create          []map[string]interface{} `json:"create,omitempty"`
	Cancel          []string                 `json:"cancel,omitempty"`
	Update          []map[string]interface{} `json:"update,omitempty"` // revise_plan: update existing task descriptions
	Path            string                   `json:"path,omitempty"`            // file_read target path
	Tool            string                   `json:"tool,omitempty"`            // tool_call: web_search, web_fetch, calculator
	ToolParams      map[string]interface{}   `json:"tool_params,omitempty"`     // tool_call: tool arguments
}

// LeadDecisionResult is the output from the LeadDecision activity.
type LeadDecisionResult struct {
	DecisionSummary     string       `json:"decision_summary"`
	Actions             []LeadAction `json:"actions"`
	TokensUsed          int          `json:"tokens_used"`
	InputTokens         int          `json:"input_tokens"`
	OutputTokens        int          `json:"output_tokens"`
	CacheReadTokens     int          `json:"cache_read_tokens"`
	CacheCreationTokens int          `json:"cache_creation_tokens"`
	ModelUsed           string       `json:"model_used"`
	Provider            string       `json:"provider"`
}

// LeadDecision calls the Python LLM service's /lead/decide endpoint (D2: replay-safe).
func LeadDecision(ctx context.Context, in LeadDecisionInput) (LeadDecisionResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("LeadDecision called", "workflow_id", in.WorkflowID, "event", in.Event.Type)

	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/lead/decide", base)

	body, err := json.Marshal(in)
	if err != nil {
		return LeadDecisionResult{}, fmt.Errorf("failed to marshal lead decision input: %w", err)
	}

	// Must be under the 150s Temporal StartToCloseTimeout to let Temporal
	// detect the timeout and retry, rather than the HTTP client swallowing it.
	timeoutSec := 120
	if v := os.Getenv("LEAD_DECISION_TIMEOUT_SECONDS"); v != "" {
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
		return LeadDecisionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return LeadDecisionResult{}, fmt.Errorf("lead decision HTTP call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Error("LeadDecision HTTP error", "status", resp.StatusCode, "body", string(bodyBytes))
		return LeadDecisionResult{}, fmt.Errorf("lead decision returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var out LeadDecisionResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return LeadDecisionResult{}, fmt.Errorf("failed to decode lead decision response: %w", err)
	}

	logger.Info("LeadDecision completed",
		"workflow_id", in.WorkflowID,
		"decision", out.DecisionSummary,
		"actions", len(out.Actions),
		"tokens", out.TokensUsed,
	)
	return out, nil
}

// ListWorkspaceFilesInput is the input for the ListWorkspaceFiles activity.
type ListWorkspaceFilesInput struct {
	SessionID string `json:"session_id"`
}

// ListWorkspaceFilesResult is the output of the ListWorkspaceFiles activity.
type ListWorkspaceFilesResult struct {
	Files []WorkspaceMaterial `json:"files"`
}

// ListWorkspaceFiles reads workspace file metadata for closing_checkpoint.
func ListWorkspaceFiles(ctx context.Context, in ListWorkspaceFilesInput) (ListWorkspaceFilesResult, error) {
	if in.SessionID == "" {
		return ListWorkspaceFilesResult{}, nil
	}
	if err := validateSessionID(in.SessionID); err != nil {
		return ListWorkspaceFilesResult{}, fmt.Errorf("invalid session_id: %w", err)
	}
	baseDir := os.Getenv("SHANNON_SESSION_WORKSPACES_DIR")
	if baseDir == "" {
		baseDir = "/tmp/shannon-sessions"
	}
	sessionDir := filepath.Join(baseDir, in.SessionID)
	files := readWorkspaceFiles(sessionDir, 10000, 2000)
	return ListWorkspaceFilesResult{Files: files}, nil
}

// ── Lead Tool Execution ───────────────────────────────────────────────────

// LeadToolInput is the input for direct tool execution by Lead.
type LeadToolInput struct {
	Tool       string                 `json:"tool"`
	ToolParams map[string]interface{} `json:"tool_params"`
	SessionID  string                 `json:"session_id,omitempty"`
}

// LeadToolResult is the output of direct tool execution.
type LeadToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"` // formatted text
	Error   string `json:"error,omitempty"`
}

// LeadExecuteTool calls Python LLM-service POST /tools/execute directly.
// Zero LLM cost — the tool registry executes without involving an LLM.
func LeadExecuteTool(ctx context.Context, in LeadToolInput) (LeadToolResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("LeadExecuteTool called", "tool", in.Tool)

	base := os.Getenv("LLM_SERVICE_URL")
	if base == "" {
		base = "http://llm-service:8000"
	}
	url := fmt.Sprintf("%s/tools/execute", base)

	// Build request body matching Python ToolExecuteRequest field names
	reqBody := map[string]interface{}{
		"tool_name":  in.Tool,
		"parameters": in.ToolParams,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return LeadToolResult{}, fmt.Errorf("failed to marshal tool input: %w", err)
	}

	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: interceptors.NewWorkflowHTTPRoundTripper(nil),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return LeadToolResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return LeadToolResult{Error: err.Error()}, nil // Return error in result, don't fail activity
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return LeadToolResult{Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))}, nil
	}

	var pyResp struct {
		Success bool        `json:"success"`
		Output  interface{} `json:"output"`
		Text    string      `json:"text"`
		Error   string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pyResp); err != nil {
		return LeadToolResult{Error: fmt.Sprintf("decode error: %v", err)}, nil
	}

	// Prefer formatted text; fall back to raw output
	output := pyResp.Text
	if output == "" {
		if pyResp.Output != nil {
			if s, ok := pyResp.Output.(string); ok {
				output = s
			} else {
				b, _ := json.Marshal(pyResp.Output)
				output = string(b)
			}
		}
	}

	logger.Info("LeadExecuteTool completed", "tool", in.Tool, "success", pyResp.Success, "output_len", len(output))
	return LeadToolResult{
		Success: pyResp.Success,
		Output:  output,
		Error:   pyResp.Error,
	}, nil
}
