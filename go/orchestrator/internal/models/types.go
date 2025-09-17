package models

import "time"

// Execution modes
const (
	ModeSimple   = "simple"
	ModeStandard = "standard"
	ModeComplex  = "complex"
)

// Task statuses
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Model tiers
const (
	TierSmall  = "small"
	TierMedium = "medium"
	TierLarge  = "large"
)

// TaskRequest represents an incoming task request
type TaskRequest struct {
	TaskID      string                 `json:"task_id"`
	UserID      string                 `json:"user_id"`
	SessionID   string                 `json:"session_id"`
	TenantID    string                 `json:"tenant_id"`
	Query       string                 `json:"query"`
	Context     map[string]interface{} `json:"context"`
	MaxAgents   int                    `json:"max_agents"`
	TokenBudget float64                `json:"token_budget"`
}

// TaskResponse represents the result of task execution
type TaskResponse struct {
	TaskID  string            `json:"task_id"`
	Status  string            `json:"status"`
	Result  string            `json:"result"`
	Error   string            `json:"error,omitempty"`
	Metrics *ExecutionMetrics `json:"metrics,omitempty"`
}

// ComplexityScore represents the analyzed complexity of a task
type ComplexityScore struct {
	Mode             string      `json:"mode"`
	Score            float64     `json:"score"`
	EstimatedAgents  int         `json:"estimated_agents"`
	EstimatedTokens  int         `json:"estimated_tokens"`
	EstimatedCostUSD float64     `json:"estimated_cost_usd"`
	RecommendedTier  string      `json:"recommended_tier"`
	AgentTasks       []AgentTask `json:"agent_tasks,omitempty"`
	Reasoning        string      `json:"reasoning"`
}

// AgentTask represents a task for a single agent
type AgentTask struct {
	AgentID      string   `json:"agent_id"`
	TaskID       string   `json:"task_id"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
	Mode         string   `json:"mode"`
	ModelTier    string   `json:"model_tier"`
}

// AgentResult represents the result from an agent execution
type AgentResult struct {
	AgentID string            `json:"agent_id"`
	TaskID  string            `json:"task_id"`
	Output  string            `json:"output"`
	Status  string            `json:"status"`
	Error   string            `json:"error,omitempty"`
	Metrics *ExecutionMetrics `json:"metrics,omitempty"`
}

// ExecutionMetrics contains metrics about task execution
type ExecutionMetrics struct {
	LatencyMs  int64       `json:"latency_ms"`
	TokenUsage *TokenUsage `json:"token_usage,omitempty"`
	CacheHit   bool        `json:"cache_hit"`
	CacheScore float64     `json:"cache_score"`
	AgentsUsed int32       `json:"agents_used"`
	Mode       string      `json:"mode"`
}

// TokenUsage tracks token consumption
type TokenUsage struct {
	PromptTokens     int32   `json:"prompt_tokens"`
	CompletionTokens int32   `json:"completion_tokens"`
	TotalTokens      int32   `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Model            string  `json:"model"`
	Tier             string  `json:"tier"`
}

// SessionContext maintains context across requests
type SessionContext struct {
	SessionID   string                 `json:"session_id"`
	UserID      string                 `json:"user_id"`
	Context     map[string]interface{} `json:"context"`
	RecentTasks []TaskSummary          `json:"recent_tasks"`
	TokenUsage  *TokenUsage            `json:"token_usage"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// TaskSummary provides a summary of a task
type TaskSummary struct {
	TaskID      string      `json:"task_id"`
	Query       string      `json:"query"`
	Status      string      `json:"status"`
	Mode        string      `json:"mode"`
	CreatedAt   time.Time   `json:"created_at"`
	CompletedAt *time.Time  `json:"completed_at,omitempty"`
	TokenUsage  *TokenUsage `json:"token_usage,omitempty"`
}
