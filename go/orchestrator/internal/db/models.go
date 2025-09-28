package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// JSONB represents a PostgreSQL jsonb column
type JSONB map[string]interface{}

// Value implements the driver.Valuer interface
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface
func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("cannot scan %T into JSONB", value)
	}

	return json.Unmarshal(bytes, j)
}

// TaskExecution represents a workflow/task execution record
type TaskExecution struct {
	ID          uuid.UUID  `db:"id"`
	WorkflowID  string     `db:"workflow_id"`
	UserID      *uuid.UUID `db:"user_id"`
	SessionID   string     `db:"session_id"`
	Query       string     `db:"query"`
	Mode        string     `db:"mode"`
	Status      string     `db:"status"`
	StartedAt   time.Time  `db:"started_at"`
	CompletedAt *time.Time `db:"completed_at"`

	// Results
	Result       *string `db:"result"`
	ErrorMessage *string `db:"error_message"`

	// Token metrics
	TotalTokens      int     `db:"total_tokens"`
	PromptTokens     int     `db:"prompt_tokens"`
	CompletionTokens int     `db:"completion_tokens"`
	TotalCostUSD     float64 `db:"total_cost_usd"`

	// Performance metrics
	DurationMs      *int    `db:"duration_ms"`
	AgentsUsed      int     `db:"agents_used"`
	ToolsInvoked    int     `db:"tools_invoked"`
	CacheHits       int     `db:"cache_hits"`
	ComplexityScore float64 `db:"complexity_score"`

	// Metadata
	Metadata  JSONB     `db:"metadata"`
	CreatedAt time.Time `db:"created_at"`
}

// AgentExecution represents an individual agent execution
type AgentExecution struct {
	ID         string    `db:"id"`
	WorkflowID string    `db:"workflow_id"`  // References task_executions.workflow_id
	TaskID     string    `db:"task_id"`      // Optional reference to task_executions.id
	AgentID    string    `db:"agent_id"`

	// Execution details
	Input        string  `db:"input"`
	Output       string  `db:"output"`
	State        string  `db:"state"`
	ErrorMessage string  `db:"error_message"`

	// Token usage
	TokensUsed int    `db:"tokens_used"`
	ModelUsed  string `db:"model_used"`

	// Performance
	DurationMs int64 `db:"duration_ms"`

	// Metadata
	Metadata  JSONB     `db:"metadata"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// ToolExecution represents a tool execution record
type ToolExecution struct {
	ID               string     `db:"id"`
	WorkflowID       string     `db:"workflow_id"`       // References task_executions.workflow_id
	AgentID          string     `db:"agent_id"`          // Agent that executed the tool
	AgentExecutionID *string    `db:"agent_execution_id"` // Optional reference to agent_executions.id

	ToolName string `db:"tool_name"`

	// Execution details
	InputParams    JSONB  `db:"input_params"`
	Output         string `db:"output"`
	Success        bool   `db:"success"`
	Error          string `db:"error"`

	// Performance
	DurationMs     int64 `db:"duration_ms"`
	TokensConsumed int   `db:"tokens_consumed"`

	// Metadata
	Metadata  JSONB     `db:"metadata"`
	CreatedAt time.Time `db:"created_at"`
}

// SessionArchive represents a snapshot of a Redis session
type SessionArchive struct {
	ID        uuid.UUID  `db:"id"`
	SessionID string     `db:"session_id"`
	UserID    *uuid.UUID `db:"user_id"`

	// Snapshot data
	SnapshotData JSONB   `db:"snapshot_data"`
	MessageCount int     `db:"message_count"`
	TotalTokens  int     `db:"total_tokens"`
	TotalCostUSD float64 `db:"total_cost_usd"`

	// Timing
	SessionStartedAt time.Time  `db:"session_started_at"`
	SnapshotTakenAt  time.Time  `db:"snapshot_taken_at"`
	TTLExpiresAt     *time.Time `db:"ttl_expires_at"`
}

// UsageDailyAggregate represents daily usage statistics
type UsageDailyAggregate struct {
	ID     uuid.UUID  `db:"id"`
	UserID *uuid.UUID `db:"user_id"`
	Date   time.Time  `db:"date"`

	// Aggregated metrics
	TotalTasks      int `db:"total_tasks"`
	SuccessfulTasks int `db:"successful_tasks"`
	FailedTasks     int `db:"failed_tasks"`

	// Token usage
	TotalTokens  int     `db:"total_tokens"`
	TotalCostUSD float64 `db:"total_cost_usd"`

	// Model distribution
	ModelUsage JSONB `db:"model_usage"`

	// Tool usage
	ToolsInvoked     int   `db:"tools_invoked"`
	ToolDistribution JSONB `db:"tool_distribution"`

	// Performance
	AvgDurationMs int     `db:"avg_duration_ms"`
	CacheHitRate  float64 `db:"cache_hit_rate"`

	CreatedAt time.Time `db:"created_at"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID         uuid.UUID  `db:"id"`
	UserID     *uuid.UUID `db:"user_id"`
	Action     string     `db:"action"`
	EntityType string     `db:"entity_type"`
	EntityID   string     `db:"entity_id"`

	// Audit details
	IPAddress string `db:"ip_address"`
	UserAgent string `db:"user_agent"`
	RequestID string `db:"request_id"`

	// Changes
	OldValue JSONB `db:"old_value"`
	NewValue JSONB `db:"new_value"`

	CreatedAt time.Time `db:"created_at"`
}

// TaskExecutionFilter provides filtering options for task queries
type TaskExecutionFilter struct {
	UserID    *uuid.UUID
	SessionID *string
	Status    *string
	Mode      *string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// AggregateStats represents aggregated statistics
type AggregateStats struct {
	Period       string  `db:"period"`
	TotalTasks   int     `db:"total_tasks"`
	TotalTokens  int     `db:"total_tokens"`
	TotalCost    float64 `db:"total_cost"`
	AvgDuration  int     `db:"avg_duration"`
	SuccessRate  float64 `db:"success_rate"`
	CacheHitRate float64 `db:"cache_hit_rate"`
	TopModels    JSONB   `db:"top_models"`
	TopTools     JSONB   `db:"top_tools"`
}
