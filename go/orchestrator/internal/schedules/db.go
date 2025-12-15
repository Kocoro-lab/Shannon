package schedules

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DBOperations handles schedule database operations
type DBOperations struct {
	db *sql.DB
}

// NewDBOperations creates a new DBOperations instance
func NewDBOperations(db *sql.DB) *DBOperations {
	return &DBOperations{db: db}
}

// CreateSchedule inserts a new schedule
func (d *DBOperations) CreateSchedule(ctx context.Context, s *Schedule) error {
	contextJSON, _ := json.Marshal(s.TaskContext)

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO scheduled_tasks (
			id, user_id, tenant_id, name, description, cron_expression, timezone,
			task_query, task_context, max_budget_per_run_usd, timeout_seconds,
			temporal_schedule_id, status, created_at, updated_at, next_run_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`,
		s.ID, s.UserID, s.TenantID, s.Name, s.Description, s.CronExpression, s.Timezone,
		s.TaskQuery, contextJSON, s.MaxBudgetPerRunUSD, s.TimeoutSeconds,
		s.TemporalScheduleID, s.Status, s.CreatedAt, s.UpdatedAt, s.NextRunAt,
	)
	return err
}

// GetSchedule retrieves a schedule by ID
func (d *DBOperations) GetSchedule(ctx context.Context, id uuid.UUID) (*Schedule, error) {
	var s Schedule
	var contextJSON []byte
	var lastRunAt, nextRunAt sql.NullTime
	var tenantID sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, user_id, tenant_id, name, description, cron_expression, timezone,
			   task_query, task_context, max_budget_per_run_usd, timeout_seconds,
			   temporal_schedule_id, status, created_at, updated_at,
			   last_run_at, next_run_at, total_runs, successful_runs, failed_runs
		FROM scheduled_tasks
		WHERE id = $1 AND status != 'DELETED'
	`, id).Scan(
		&s.ID, &s.UserID, &tenantID, &s.Name, &s.Description, &s.CronExpression, &s.Timezone,
		&s.TaskQuery, &contextJSON, &s.MaxBudgetPerRunUSD, &s.TimeoutSeconds,
		&s.TemporalScheduleID, &s.Status, &s.CreatedAt, &s.UpdatedAt,
		&lastRunAt, &nextRunAt, &s.TotalRuns, &s.SuccessfulRuns, &s.FailedRuns,
	)
	if err != nil {
		return nil, err
	}

	// Parse tenant ID
	if tenantID.Valid {
		tid, _ := uuid.Parse(tenantID.String)
		s.TenantID = tid
	}

	// Parse task context
	_ = json.Unmarshal(contextJSON, &s.TaskContext)
	if s.TaskContext == nil {
		s.TaskContext = make(map[string]interface{})
	}

	// Parse nullable timestamps
	if lastRunAt.Valid {
		s.LastRunAt = &lastRunAt.Time
	}
	if nextRunAt.Valid {
		s.NextRunAt = &nextRunAt.Time
	}

	return &s, nil
}

// ListSchedules retrieves schedules for a user with pagination
func (d *DBOperations) ListSchedules(ctx context.Context, userID, tenantID uuid.UUID, page, pageSize int, statusFilter string) ([]*Schedule, int, error) {
	offset := (page - 1) * pageSize

	statusClause := "AND status != 'DELETED'"
	if statusFilter == "ACTIVE" {
		statusClause = "AND status = 'ACTIVE'"
	} else if statusFilter == "PAUSED" {
		statusClause = "AND status = 'PAUSED'"
	}

	// Get total count
	var totalCount int
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) FROM scheduled_tasks
		WHERE user_id = $1 AND (tenant_id = $2 OR $2 = '00000000-0000-0000-0000-000000000000'::uuid) %s
	`, statusClause)
	err := d.db.QueryRowContext(ctx, countQuery, userID, tenantID).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get schedules
	query := fmt.Sprintf(`
		SELECT id, user_id, tenant_id, name, description, cron_expression, timezone,
			   task_query, task_context, max_budget_per_run_usd, timeout_seconds,
			   temporal_schedule_id, status, created_at, updated_at,
			   last_run_at, next_run_at, total_runs, successful_runs, failed_runs
		FROM scheduled_tasks
		WHERE user_id = $1 AND (tenant_id = $2 OR $2 = '00000000-0000-0000-0000-000000000000'::uuid) %s
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, statusClause)

	rows, err := d.db.QueryContext(ctx, query, userID, tenantID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var scheduleList []*Schedule
	for rows.Next() {
		var s Schedule
		var contextJSON []byte
		var lastRunAt, nextRunAt sql.NullTime
		var tenantIDVal sql.NullString

		err := rows.Scan(
			&s.ID, &s.UserID, &tenantIDVal, &s.Name, &s.Description, &s.CronExpression, &s.Timezone,
			&s.TaskQuery, &contextJSON, &s.MaxBudgetPerRunUSD, &s.TimeoutSeconds,
			&s.TemporalScheduleID, &s.Status, &s.CreatedAt, &s.UpdatedAt,
			&lastRunAt, &nextRunAt, &s.TotalRuns, &s.SuccessfulRuns, &s.FailedRuns,
		)
		if err != nil {
			return nil, 0, err
		}

		// Parse tenant ID
		if tenantIDVal.Valid {
			tid, _ := uuid.Parse(tenantIDVal.String)
			s.TenantID = tid
		}

		// Parse task context
		_ = json.Unmarshal(contextJSON, &s.TaskContext)
		if s.TaskContext == nil {
			s.TaskContext = make(map[string]interface{})
		}

		// Parse nullable timestamps
		if lastRunAt.Valid {
			s.LastRunAt = &lastRunAt.Time
		}
		if nextRunAt.Valid {
			s.NextRunAt = &nextRunAt.Time
		}

		scheduleList = append(scheduleList, &s)
	}

	return scheduleList, totalCount, nil
}

// UpdateScheduleStatus updates a schedule's status
func (d *DBOperations) UpdateScheduleStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE scheduled_tasks SET status = $1, updated_at = NOW()
		WHERE id = $2
	`, status, id)
	return err
}

// UpdateScheduleNextRun updates the next run timestamp
func (d *DBOperations) UpdateScheduleNextRun(ctx context.Context, id uuid.UUID, nextRun time.Time) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE scheduled_tasks SET next_run_at = $1, updated_at = NOW()
		WHERE id = $2
	`, nextRun, id)
	return err
}

// UpdateSchedule updates schedule fields
func (d *DBOperations) UpdateSchedule(ctx context.Context, req *UpdateScheduleInput) error {
	// Only marshal TaskContext if explicitly provided (non-nil)
	// When nil, pass nil to SQL so COALESCE preserves existing value
	var contextJSON interface{}
	if req.TaskContext != nil {
		contextJSON, _ = json.Marshal(req.TaskContext)
	}

	_, err := d.db.ExecContext(ctx, `
		UPDATE scheduled_tasks
		SET name = COALESCE($2, name),
			description = COALESCE($3, description),
			cron_expression = COALESCE($4, cron_expression),
			timezone = COALESCE($5, timezone),
			task_query = COALESCE($6, task_query),
			task_context = COALESCE($7, task_context),
			max_budget_per_run_usd = COALESCE($8, max_budget_per_run_usd),
			timeout_seconds = COALESCE($9, timeout_seconds),
			updated_at = NOW()
		WHERE id = $1
	`,
		req.ScheduleID, req.Name, req.Description, req.CronExpression, req.Timezone,
		req.TaskQuery, contextJSON, req.MaxBudgetPerRunUSD, req.TimeoutSeconds,
	)
	return err
}

// CountSchedulesByUser counts active schedules for a user
func (d *DBOperations) CountSchedulesByUser(ctx context.Context, userID, tenantID uuid.UUID) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM scheduled_tasks
		WHERE user_id = $1 AND (tenant_id = $2 OR $2 = '00000000-0000-0000-0000-000000000000'::uuid)
		  AND status IN ('ACTIVE', 'PAUSED')
	`, userID, tenantID).Scan(&count)
	return count, err
}

// GetAllActiveSchedules returns all active/paused schedules (for orphan detection)
func (d *DBOperations) GetAllActiveSchedules(ctx context.Context) ([]*Schedule, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, user_id, tenant_id, name, description, cron_expression, timezone,
			   task_query, task_context, max_budget_per_run_usd, timeout_seconds,
			   temporal_schedule_id, status, created_at, updated_at, last_run_at,
			   next_run_at, total_runs, successful_runs, failed_runs
		FROM scheduled_tasks
		WHERE status IN ('ACTIVE', 'PAUSED')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []*Schedule
	for rows.Next() {
		s := &Schedule{}
		var description, contextJSON sql.NullString
		var lastRunAt, nextRunAt sql.NullTime
		err := rows.Scan(
			&s.ID, &s.UserID, &s.TenantID, &s.Name, &description,
			&s.CronExpression, &s.Timezone, &s.TaskQuery, &contextJSON,
			&s.MaxBudgetPerRunUSD, &s.TimeoutSeconds, &s.TemporalScheduleID,
			&s.Status, &s.CreatedAt, &s.UpdatedAt, &lastRunAt, &nextRunAt,
			&s.TotalRuns, &s.SuccessfulRuns, &s.FailedRuns,
		)
		if err != nil {
			return nil, err
		}
		if description.Valid {
			s.Description = description.String
		}
		if contextJSON.Valid && contextJSON.String != "" {
			_ = json.Unmarshal([]byte(contextJSON.String), &s.TaskContext)
		}
		if lastRunAt.Valid {
			s.LastRunAt = &lastRunAt.Time
		}
		if nextRunAt.Valid {
			s.NextRunAt = &nextRunAt.Time
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

// RecordScheduleExecution logs a schedule execution to history
func (d *DBOperations) RecordScheduleExecution(ctx context.Context, scheduleID uuid.UUID, taskID string, status string, cost float64, errorMsg string) error {
	var completedAt *time.Time
	if status == "COMPLETED" || status == "FAILED" || status == "CANCELLED" {
		now := time.Now()
		completedAt = &now
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO scheduled_task_executions (schedule_id, task_id, status, total_cost_usd, error_message, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, scheduleID, taskID, status, cost, errorMsg, completedAt)

	// Update schedule statistics
	if status == "COMPLETED" {
		_, _ = d.db.ExecContext(ctx, `
			UPDATE scheduled_tasks
			SET total_runs = total_runs + 1,
				successful_runs = successful_runs + 1,
				last_run_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, scheduleID)
	} else if status == "FAILED" {
		_, _ = d.db.ExecContext(ctx, `
			UPDATE scheduled_tasks
			SET total_runs = total_runs + 1,
				failed_runs = failed_runs + 1,
				last_run_at = NOW(),
				updated_at = NOW()
			WHERE id = $1
		`, scheduleID)
	}

	return err
}
