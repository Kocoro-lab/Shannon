package schedules

import (
	"time"

	"github.com/google/uuid"
)

// Schedule status constants
const (
	ScheduleStatusActive  = "ACTIVE"
	ScheduleStatusPaused  = "PAUSED"
	ScheduleStatusDeleted = "DELETED"
)

// Schedule represents a scheduled task
type Schedule struct {
	ID                  uuid.UUID              `json:"id"`
	UserID              uuid.UUID              `json:"user_id"`
	TenantID            uuid.UUID              `json:"tenant_id"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	CronExpression      string                 `json:"cron_expression"`
	Timezone            string                 `json:"timezone"`
	TaskQuery           string                 `json:"task_query"`
	TaskContext         map[string]interface{} `json:"task_context"`
	MaxBudgetPerRunUSD  float64                `json:"max_budget_per_run_usd"`
	TimeoutSeconds      int                    `json:"timeout_seconds"`
	TemporalScheduleID  string                 `json:"temporal_schedule_id"`
	Status              string                 `json:"status"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	LastRunAt           *time.Time             `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time             `json:"next_run_at,omitempty"`
	TotalRuns           int                    `json:"total_runs"`
	SuccessfulRuns      int                    `json:"successful_runs"`
	FailedRuns          int                    `json:"failed_runs"`
}

// CreateScheduleInput is the input for creating a schedule
type CreateScheduleInput struct {
	UserID             uuid.UUID
	TenantID           uuid.UUID
	Name               string
	Description        string
	CronExpression     string
	Timezone           string
	TaskQuery          string
	TaskContext        map[string]interface{}
	MaxBudgetPerRunUSD float64
	TimeoutSeconds     int
}

// UpdateScheduleInput is the input for updating a schedule
type UpdateScheduleInput struct {
	ScheduleID         uuid.UUID
	Name               *string
	Description        *string
	CronExpression     *string
	Timezone           *string
	TaskQuery          *string
	TaskContext        map[string]interface{}
	MaxBudgetPerRunUSD *float64
	TimeoutSeconds     *int
}

// ScheduledTaskInput is passed to ScheduledTaskWorkflow
type ScheduledTaskInput struct {
	ScheduleID         string                 `json:"schedule_id"`
	TaskQuery          string                 `json:"task_query"`
	TaskContext        map[string]interface{} `json:"task_context"`
	MaxBudgetPerRunUSD float64                `json:"max_budget_per_run_usd"`
	UserID             string                 `json:"user_id"`
	TenantID           string                 `json:"tenant_id"`
}
