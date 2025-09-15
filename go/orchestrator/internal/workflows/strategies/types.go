package strategies

import "time"

// TaskInput represents the input to a workflow
type TaskInput struct {
    Query      string
    UserID     string
    TenantID   string
    SessionID  string
    Context    map[string]interface{}
    Mode       string

    // Session context for multi-turn conversations
    History    []Message               // Recent conversation history
    SessionCtx map[string]interface{} // Persistent session context

    // Human intervention settings
    RequireApproval bool // Whether to require human approval for this task
    ApprovalTimeout int  // Timeout in seconds for human approval (0 = use default)

    // Workflow behavior flags (deterministic per-run)
    BypassSingleResult  bool // If true, return single successful result directly
}

// Message represents a conversation message
type Message struct {
    Role      string
    Content   string
    Timestamp time.Time
}

// TaskResult represents the result of a workflow execution
type TaskResult struct {
    Result       string
    Success      bool
    TokensUsed   int
    ErrorMessage string
    Metadata     map[string]interface{}
}