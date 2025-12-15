package activities

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
)

// ScheduleActivities holds dependencies for schedule-related activities
type ScheduleActivities struct {
	DB     *sql.DB
	Logger *zap.Logger
}

// NewScheduleActivities creates a new ScheduleActivities instance
func NewScheduleActivities(sqlDB *sql.DB, logger *zap.Logger) *ScheduleActivities {
	return &ScheduleActivities{
		DB:     sqlDB,
		Logger: logger,
	}
}

// RecordScheduleExecutionInput is the input for starting execution tracking
type RecordScheduleExecutionInput struct {
	ScheduleID uuid.UUID
	TaskID     string // workflow_id of the child workflow
	Query      string // task query for display
	UserID     string
	TenantID   string
}

// RecordScheduleExecutionStart logs the start of a scheduled execution
// and creates a task_executions record for unified task tracking
func (a *ScheduleActivities) RecordScheduleExecutionStart(ctx context.Context, input RecordScheduleExecutionInput) error {
	a.Logger.Debug("Recording schedule execution start",
		zap.String("schedule_id", input.ScheduleID.String()),
		zap.String("task_id", input.TaskID),
	)

	// 1. Create link record in scheduled_task_executions
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO scheduled_task_executions (schedule_id, task_id, triggered_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (schedule_id, task_id) DO NOTHING
	`, input.ScheduleID, input.TaskID)
	if err != nil {
		a.Logger.Error("Failed to create scheduled_task_executions link", zap.Error(err))
		return err
	}

	// 2. Create task_executions record using shared persistence code
	dbClient := GetGlobalDBClient()
	if dbClient == nil {
		a.Logger.Warn("Global DB client not available, skipping task_executions persistence")
		return nil
	}

	// Parse user/tenant IDs
	var userID *uuid.UUID
	if input.UserID != "" {
		if uid, err := uuid.Parse(input.UserID); err == nil {
			userID = &uid
		}
	}

	// Use synthetic session ID for scheduled runs: "schedule:<scheduleID>:<taskID>"
	// Use full IDs to avoid collisions and panics from short IDs
	sessionID := fmt.Sprintf("schedule:%s:%s", input.ScheduleID.String(), input.TaskID)

	task := &db.TaskExecution{
		WorkflowID:  input.TaskID,
		UserID:      userID,
		SessionID:   sessionID,
		Query:       input.Query,
		Status:      "RUNNING",
		StartedAt:   time.Now(),
		TriggerType: "schedule",
		ScheduleID:  &input.ScheduleID,
	}

	if err := dbClient.SaveTaskExecution(ctx, task); err != nil {
		a.Logger.Error("Failed to create task_executions record for scheduled run",
			zap.String("task_id", input.TaskID),
			zap.Error(err))
		// Don't fail the activity - the link record is created, schedule can proceed
		return nil
	}

	a.Logger.Info("Scheduled execution started and task_executions record created",
		zap.String("schedule_id", input.ScheduleID.String()),
		zap.String("task_id", input.TaskID),
		zap.String("trigger_type", "schedule"),
	)

	return nil
}

// RecordScheduleExecutionCompleteInput is the input for completing execution tracking
type RecordScheduleExecutionCompleteInput struct {
	ScheduleID uuid.UUID
	TaskID     string
	Status     string // COMPLETED, FAILED, CANCELLED
	TotalCost  float64
	ErrorMsg   string
	Result     string // optional result text

	// Metadata from child workflow (for unified task_executions consistency)
	ModelUsed        string
	Provider         string
	TotalTokens      int
	PromptTokens     int
	CompletionTokens int
}

// RecordScheduleExecutionComplete logs the completion of a scheduled execution
// and updates the task_executions record
func (a *ScheduleActivities) RecordScheduleExecutionComplete(ctx context.Context, input RecordScheduleExecutionCompleteInput) error {
	a.Logger.Debug("Recording schedule execution completion",
		zap.String("schedule_id", input.ScheduleID.String()),
		zap.String("task_id", input.TaskID),
		zap.String("status", input.Status),
		zap.Float64("cost", input.TotalCost),
	)

	// 1. Update task_executions via shared persistence code
	dbClient := GetGlobalDBClient()
	if dbClient != nil {
		now := time.Now()
		var result *string
		if input.Result != "" {
			result = &input.Result
		}
		var errorMsg *string
		if input.ErrorMsg != "" {
			errorMsg = &input.ErrorMsg
		}

		// Get existing task to preserve fields, then update
		existingTask, err := dbClient.GetTaskExecution(ctx, input.TaskID)
		if err != nil {
			a.Logger.Warn("Failed to get existing task_executions record",
				zap.String("task_id", input.TaskID),
				zap.Error(err))
		}

		if existingTask != nil {
			// Update existing record with completion info
			existingTask.Status = input.Status
			existingTask.CompletedAt = &now
			existingTask.TotalCostUSD = input.TotalCost
			if result != nil {
				existingTask.Result = result
			}
			if errorMsg != nil {
				existingTask.ErrorMessage = errorMsg
			}

			// Populate metadata from child workflow result (Option A: unified model)
			if input.ModelUsed != "" {
				existingTask.ModelUsed = input.ModelUsed
			}
			if input.Provider != "" {
				existingTask.Provider = input.Provider
			}
			if input.TotalTokens > 0 {
				existingTask.TotalTokens = input.TotalTokens
			}
			if input.PromptTokens > 0 {
				existingTask.PromptTokens = input.PromptTokens
			}
			if input.CompletionTokens > 0 {
				existingTask.CompletionTokens = input.CompletionTokens
			}

			// Calculate duration if we have start time
			if !existingTask.StartedAt.IsZero() {
				durationMs := int(now.Sub(existingTask.StartedAt).Milliseconds())
				existingTask.DurationMs = &durationMs
			}

			if err := dbClient.SaveTaskExecution(ctx, existingTask); err != nil {
				a.Logger.Warn("Failed to update task_executions record",
					zap.String("task_id", input.TaskID),
					zap.Error(err))
			} else {
				a.Logger.Debug("Updated task_executions record with completion status and metadata",
					zap.String("task_id", input.TaskID),
					zap.String("status", input.Status),
					zap.String("model", input.ModelUsed),
					zap.String("provider", input.Provider),
					zap.Int("total_tokens", input.TotalTokens))
			}
		}
	}

	// 2. Update schedule statistics
	if input.Status == "COMPLETED" {
		_, _ = a.DB.ExecContext(ctx, `
			UPDATE scheduled_tasks
			SET total_runs = total_runs + 1,
				successful_runs = successful_runs + 1,
				last_run_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, input.ScheduleID)
	} else if input.Status == "FAILED" {
		_, _ = a.DB.ExecContext(ctx, `
			UPDATE scheduled_tasks
			SET total_runs = total_runs + 1,
				failed_runs = failed_runs + 1,
				last_run_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, input.ScheduleID)
	}

	// 3. Update next_run_at
	a.updateNextRunAt(ctx, input.ScheduleID)

	a.Logger.Info("Scheduled execution completed",
		zap.String("schedule_id", input.ScheduleID.String()),
		zap.String("task_id", input.TaskID),
		zap.String("status", input.Status),
		zap.Float64("cost", input.TotalCost),
	)

	return nil
}

// updateNextRunAt calculates and updates the next run time for a schedule.
// NOTE: This is a best-effort local calculation. The authoritative next_run_at
// is maintained by Temporal. Activities cannot access ScheduleClient.Describe()
// directly. For accurate next_run_at after schedule changes (cron/timezone),
// see manager.go which reads from Temporal after updates.
func (a *ScheduleActivities) updateNextRunAt(ctx context.Context, scheduleID uuid.UUID) {
	// Fetch schedule's cron expression and timezone
	var cronExpr, timezone string
	err := a.DB.QueryRowContext(ctx, `
		SELECT cron_expression, timezone
		FROM scheduled_tasks
		WHERE id = $1 AND status = 'ACTIVE'
	`, scheduleID).Scan(&cronExpr, &timezone)

	if err != nil {
		a.Logger.Warn("Failed to fetch schedule for next_run_at update",
			zap.String("schedule_id", scheduleID.String()),
			zap.Error(err))
		return
	}

	// Parse cron expression
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		a.Logger.Error("Failed to parse cron expression",
			zap.String("schedule_id", scheduleID.String()),
			zap.String("cron", cronExpr),
			zap.Error(err))
		return
	}

	// Load timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		a.Logger.Warn("Invalid timezone, using UTC",
			zap.String("schedule_id", scheduleID.String()),
			zap.String("timezone", timezone))
		loc = time.UTC
	}

	// Calculate next run time
	nextRun := schedule.Next(time.Now().In(loc))

	// Update database
	_, err = a.DB.ExecContext(ctx, `
		UPDATE scheduled_tasks
		SET next_run_at = $1,
			updated_at = NOW()
		WHERE id = $2
	`, nextRun, scheduleID)

	if err != nil {
		a.Logger.Error("Failed to update next_run_at",
			zap.String("schedule_id", scheduleID.String()),
			zap.Error(err))
	} else {
		a.Logger.Debug("Updated next_run_at",
			zap.String("schedule_id", scheduleID.String()),
			zap.Time("next_run", nextRun))
	}
}
