package schedules

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Typed errors for proper gRPC code mapping
var (
	ErrInvalidCronExpression = errors.New("invalid cron expression")
	ErrIntervalTooShort      = errors.New("cron interval too short")
	ErrScheduleLimitReached  = errors.New("schedule limit reached")
	ErrBudgetExceeded        = errors.New("budget exceeds limit")
	ErrInvalidTimezone       = errors.New("invalid timezone")
	ErrScheduleNotFound      = errors.New("schedule not found")
)

// Config holds resource limit configuration
type Config struct {
	MaxPerUser          int     // Max schedules per user (default: 50)
	MinCronIntervalMins int     // Min interval between runs in minutes (default: 60)
	MaxBudgetPerRunUSD  float64 // Max budget per execution (default: 10.0)
}

// Manager handles schedule CRUD operations
type Manager struct {
	temporalClient client.Client
	dbOps          *DBOperations
	config         *Config
	logger         *zap.Logger
	cronParser     cron.Parser
}

// NewManager creates a new schedule manager
func NewManager(tc client.Client, db *sql.DB, cfg *Config, logger *zap.Logger) *Manager {
	return &Manager{
		temporalClient: tc,
		dbOps:          NewDBOperations(db),
		config:         cfg,
		logger:         logger,
		cronParser:     cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// CreateSchedule creates a new scheduled task
func (m *Manager) CreateSchedule(ctx context.Context, req *CreateScheduleInput) (*Schedule, error) {
	// 1. Validate cron expression
	schedule, err := m.cronParser.Parse(req.CronExpression)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCronExpression, err)
	}

	// 2. Enforce minimum interval
	if !m.validateMinInterval(req.CronExpression) {
		return nil, fmt.Errorf("%w: must be at least %d minutes", ErrIntervalTooShort, m.config.MinCronIntervalMins)
	}

	// 3. Check per-user limit
	count, err := m.dbOps.CountSchedulesByUser(ctx, req.UserID, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to check schedule limit: %w", err)
	}
	if count >= m.config.MaxPerUser {
		return nil, fmt.Errorf("%w: %d/%d schedules", ErrScheduleLimitReached, count, m.config.MaxPerUser)
	}

	// 4. Validate and enforce budget limit
	if req.MaxBudgetPerRunUSD < 0 {
		return nil, fmt.Errorf("budget cannot be negative: $%.2f", req.MaxBudgetPerRunUSD)
	}
	if req.MaxBudgetPerRunUSD > m.config.MaxBudgetPerRunUSD {
		return nil, fmt.Errorf("%w: $%.2f > $%.2f", ErrBudgetExceeded,
			req.MaxBudgetPerRunUSD, m.config.MaxBudgetPerRunUSD)
	}

	// 5. Validate timezone
	timezone := req.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	tz, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidTimezone, timezone)
	}

	// 6. Generate IDs
	scheduleID := uuid.New()
	temporalScheduleID := fmt.Sprintf("schedule-%s", scheduleID.String())

	_, err = m.temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: temporalScheduleID,
		Spec: client.ScheduleSpec{
			CronExpressions: []string{req.CronExpression},
			TimeZoneName:    timezone,
		},
		Action: &client.ScheduleWorkflowAction{
			// ID intentionally omitted - Temporal auto-generates unique ID per run
			// Format: <schedule-id>-<timestamp>, enabling true per-execution history
			Workflow:           "ScheduledTaskWorkflow",
			TaskQueue:          "shannon-tasks",
			WorkflowRunTimeout: time.Duration(req.TimeoutSeconds) * time.Second,
			Args: []interface{}{
				ScheduledTaskInput{
					ScheduleID:         scheduleID.String(),
					TaskQuery:          req.TaskQuery,
					TaskContext:        req.TaskContext,
					MaxBudgetPerRunUSD: req.MaxBudgetPerRunUSD,
					UserID:             req.UserID.String(),
					TenantID:           req.TenantID.String(),
				},
			},
			Memo: map[string]interface{}{
				"schedule_id": scheduleID.String(),
				"user_id":     req.UserID.String(),
				"tenant_id":   req.TenantID.String(),
			},
		},
		Paused: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Temporal schedule: %w", err)
	}

	// 7. Calculate next run time
	nextRun := schedule.Next(time.Now().In(tz))

	// 8. Persist to database
	dbSchedule := &Schedule{
		ID:                 scheduleID,
		UserID:             req.UserID,
		TenantID:           req.TenantID,
		Name:               req.Name,
		Description:        req.Description,
		CronExpression:     req.CronExpression,
		Timezone:           timezone,
		TaskQuery:          req.TaskQuery,
		TaskContext:        req.TaskContext,
		MaxBudgetPerRunUSD: req.MaxBudgetPerRunUSD,
		TimeoutSeconds:     req.TimeoutSeconds,
		TemporalScheduleID: temporalScheduleID,
		Status:             ScheduleStatusActive,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		NextRunAt:          &nextRun,
	}

	if err := m.dbOps.CreateSchedule(ctx, dbSchedule); err != nil {
		// Rollback: Delete Temporal schedule
		_ = m.temporalClient.ScheduleClient().GetHandle(ctx, temporalScheduleID).Delete(ctx)
		return nil, fmt.Errorf("failed to persist schedule: %w", err)
	}

	m.logger.Info("Schedule created",
		zap.String("schedule_id", scheduleID.String()),
		zap.String("user_id", req.UserID.String()),
		zap.String("cron", req.CronExpression),
	)

	return dbSchedule, nil
}

// PauseSchedule pauses a schedule (prevents future runs)
func (m *Manager) PauseSchedule(ctx context.Context, scheduleID uuid.UUID, reason string) error {
	// 1. Get schedule from DB
	dbSchedule, err := m.dbOps.GetSchedule(ctx, scheduleID)
	if err != nil {
		return fmt.Errorf("schedule not found: %w", err)
	}

	if dbSchedule.Status == ScheduleStatusPaused {
		return nil // Already paused, idempotent
	}

	// 2. Pause in Temporal
	handle := m.temporalClient.ScheduleClient().GetHandle(ctx, dbSchedule.TemporalScheduleID)
	if err := handle.Pause(ctx, client.SchedulePauseOptions{
		Note: reason,
	}); err != nil {
		return fmt.Errorf("failed to pause Temporal schedule: %w", err)
	}

	// 3. Update DB status
	if err := m.dbOps.UpdateScheduleStatus(ctx, scheduleID, ScheduleStatusPaused); err != nil {
		return fmt.Errorf("failed to update schedule status: %w", err)
	}

	m.logger.Info("Schedule paused",
		zap.String("schedule_id", scheduleID.String()),
		zap.String("reason", reason),
	)

	return nil
}

// ResumeSchedule resumes a paused schedule
func (m *Manager) ResumeSchedule(ctx context.Context, scheduleID uuid.UUID, reason string) (*time.Time, error) {
	// 1. Get schedule from DB
	dbSchedule, err := m.dbOps.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("schedule not found: %w", err)
	}

	if dbSchedule.Status == ScheduleStatusActive {
		return dbSchedule.NextRunAt, nil // Already active, return next run
	}

	// 2. Unpause in Temporal
	handle := m.temporalClient.ScheduleClient().GetHandle(ctx, dbSchedule.TemporalScheduleID)
	if err := handle.Unpause(ctx, client.ScheduleUnpauseOptions{
		Note: reason,
	}); err != nil {
		return nil, fmt.Errorf("failed to unpause Temporal schedule: %w", err)
	}

	// 3. Calculate next run
	schedule, _ := m.cronParser.Parse(dbSchedule.CronExpression)
	tz, _ := time.LoadLocation(dbSchedule.Timezone)
	if tz == nil {
		tz = time.UTC
	}
	nextRun := schedule.Next(time.Now().In(tz))

	// 4. Update DB status and next run
	if err := m.dbOps.UpdateScheduleStatus(ctx, scheduleID, ScheduleStatusActive); err != nil {
		return nil, fmt.Errorf("failed to update schedule status: %w", err)
	}
	if err := m.dbOps.UpdateScheduleNextRun(ctx, scheduleID, nextRun); err != nil {
		return nil, fmt.Errorf("failed to update next run time: %w", err)
	}

	m.logger.Info("Schedule resumed",
		zap.String("schedule_id", scheduleID.String()),
		zap.String("reason", reason),
		zap.Time("next_run", nextRun),
	)

	return &nextRun, nil
}

// DeleteSchedule soft-deletes a schedule
func (m *Manager) DeleteSchedule(ctx context.Context, scheduleID uuid.UUID) error {
	// 1. Get schedule from DB
	dbSchedule, err := m.dbOps.GetSchedule(ctx, scheduleID)
	if err != nil {
		return fmt.Errorf("schedule not found: %w", err)
	}

	// 2. Delete from Temporal
	handle := m.temporalClient.ScheduleClient().GetHandle(ctx, dbSchedule.TemporalScheduleID)
	if err := handle.Delete(ctx); err != nil {
		m.logger.Warn("Failed to delete Temporal schedule (may already be deleted)",
			zap.String("schedule_id", scheduleID.String()),
			zap.Error(err),
		)
		// Continue with DB deletion even if Temporal delete fails
	}

	// 3. Soft delete in DB
	if err := m.dbOps.UpdateScheduleStatus(ctx, scheduleID, ScheduleStatusDeleted); err != nil {
		return fmt.Errorf("failed to delete schedule: %w", err)
	}

	m.logger.Info("Schedule deleted",
		zap.String("schedule_id", scheduleID.String()),
	)

	return nil
}

// UpdateSchedule updates schedule configuration
func (m *Manager) UpdateSchedule(ctx context.Context, req *UpdateScheduleInput) (*time.Time, error) {
	// 1. Get existing schedule
	dbSchedule, err := m.dbOps.GetSchedule(ctx, req.ScheduleID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrScheduleNotFound
		}
		return nil, fmt.Errorf("failed to get schedule: %w", err)
	}

	// 2. Validate new cron expression if provided
	if req.CronExpression != nil && *req.CronExpression != "" {
		if _, err := m.cronParser.Parse(*req.CronExpression); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCronExpression, err)
		}
		if !m.validateMinInterval(*req.CronExpression) {
			return nil, fmt.Errorf("%w: must be at least %d minutes", ErrIntervalTooShort, m.config.MinCronIntervalMins)
		}
	}

	// 2.5. Validate budget if provided
	if req.MaxBudgetPerRunUSD != nil && *req.MaxBudgetPerRunUSD > m.config.MaxBudgetPerRunUSD {
		return nil, fmt.Errorf("%w: $%.2f > $%.2f", ErrBudgetExceeded,
			*req.MaxBudgetPerRunUSD, m.config.MaxBudgetPerRunUSD)
	}

	// 2.6. Validate timezone if provided (reject empty string - use existing or explicit value)
	if req.Timezone != nil {
		if *req.Timezone == "" {
			return nil, fmt.Errorf("%w: empty timezone not allowed, omit field to keep existing", ErrInvalidTimezone)
		}
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidTimezone, *req.Timezone)
		}
	}

	// 3. Update Temporal Schedule
	handle := m.temporalClient.ScheduleClient().GetHandle(ctx, dbSchedule.TemporalScheduleID)

	// Check if any Temporal-affecting fields changed
	specChanged := req.CronExpression != nil || req.Timezone != nil
	actionChanged := req.TaskQuery != nil || req.TaskContext != nil ||
		req.MaxBudgetPerRunUSD != nil || req.TimeoutSeconds != nil

	if specChanged || actionChanged {
		// Build effective values for update
		cronExpr := dbSchedule.CronExpression
		if req.CronExpression != nil {
			cronExpr = *req.CronExpression
		}
		tz := dbSchedule.Timezone
		if req.Timezone != nil {
			tz = *req.Timezone
		}
		taskQuery := dbSchedule.TaskQuery
		if req.TaskQuery != nil {
			taskQuery = *req.TaskQuery
		}
		taskContext := dbSchedule.TaskContext
		if req.TaskContext != nil {
			taskContext = req.TaskContext
		}
		budget := dbSchedule.MaxBudgetPerRunUSD
		if req.MaxBudgetPerRunUSD != nil {
			budget = *req.MaxBudgetPerRunUSD
		}
		timeout := dbSchedule.TimeoutSeconds
		if req.TimeoutSeconds != nil {
			timeout = *req.TimeoutSeconds
		}

		if err := handle.Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				// Update spec (cron/timezone)
				spec := input.Description.Schedule.Spec
				if spec == nil {
					spec = &client.ScheduleSpec{}
				}
				// Temporal Describe returns compiled calendar specs; clear them to avoid merging old times
				spec.Calendars = nil
				spec.Intervals = nil
				spec.CronExpressions = []string{cronExpr}
				spec.TimeZoneName = tz
				input.Description.Schedule.Spec = spec

				// Update action args (query/context/budget/timeout)
				if action, ok := input.Description.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
					action.Args = []interface{}{
						ScheduledTaskInput{
							ScheduleID:         dbSchedule.ID.String(),
							TaskQuery:          taskQuery,
							TaskContext:        taskContext,
							MaxBudgetPerRunUSD: budget,
							UserID:             dbSchedule.UserID.String(),
							TenantID:           dbSchedule.TenantID.String(),
						},
					}
					action.WorkflowRunTimeout = time.Duration(timeout) * time.Second
				}

				return &client.ScheduleUpdate{
					Schedule: &input.Description.Schedule,
				}, nil
			},
		}); err != nil {
			return nil, fmt.Errorf("failed to update Temporal schedule: %w", err)
		}
	}

	// 4. Update database
	if err := m.dbOps.UpdateSchedule(ctx, req); err != nil {
		return nil, fmt.Errorf("failed to update schedule: %w", err)
	}

	// 5. Get authoritative next run time from Temporal (not local calculation)
	desc, err := handle.Describe(ctx)
	if err != nil {
		m.logger.Warn("Failed to describe Temporal schedule, falling back to local calculation",
			zap.String("schedule_id", req.ScheduleID.String()),
			zap.Error(err))
		// Fallback to local calculation if Temporal describe fails
		cronExpr := dbSchedule.CronExpression
		if req.CronExpression != nil {
			cronExpr = *req.CronExpression
		}
		tz := dbSchedule.Timezone
		if req.Timezone != nil {
			tz = *req.Timezone
		}
		schedule, _ := m.cronParser.Parse(cronExpr)
		location, _ := time.LoadLocation(tz)
		if location == nil {
			location = time.UTC
		}
		nextRun := schedule.Next(time.Now().In(location))
		if err := m.dbOps.UpdateScheduleNextRun(ctx, req.ScheduleID, nextRun); err != nil {
			return nil, fmt.Errorf("failed to update next run time: %w", err)
		}
		return &nextRun, nil
	}

	// Use Temporal's authoritative next action time
	var nextRun time.Time
	if len(desc.Info.NextActionTimes) > 0 {
		nextRun = desc.Info.NextActionTimes[0]
	} else {
		// No next action scheduled (paused or no future runs)
		nextRun = time.Time{}
	}

	if err := m.dbOps.UpdateScheduleNextRun(ctx, req.ScheduleID, nextRun); err != nil {
		return nil, fmt.Errorf("failed to update next run time: %w", err)
	}

	m.logger.Info("Schedule updated",
		zap.String("schedule_id", req.ScheduleID.String()),
	)

	return &nextRun, nil
}

// GetSchedule retrieves a single schedule
func (m *Manager) GetSchedule(ctx context.Context, scheduleID uuid.UUID) (*Schedule, error) {
	return m.dbOps.GetSchedule(ctx, scheduleID)
}

// ListSchedules retrieves schedules for a user
func (m *Manager) ListSchedules(ctx context.Context, userID, tenantID uuid.UUID, page, pageSize int, statusFilter string) ([]*Schedule, int, error) {
	return m.dbOps.ListSchedules(ctx, userID, tenantID, page, pageSize, statusFilter)
}

// VerifyScheduleExists checks if a schedule exists in Temporal and marks it as deleted if orphaned
func (m *Manager) VerifyScheduleExists(ctx context.Context, schedule *Schedule) (bool, error) {
	if schedule.Status != ScheduleStatusActive && schedule.Status != ScheduleStatusPaused {
		return true, nil // Only verify active/paused schedules
	}

	handle := m.temporalClient.ScheduleClient().GetHandle(ctx, schedule.TemporalScheduleID)
	_, err := handle.Describe(ctx)
	if err != nil {
		// Check if it's a "not found" error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NotFound") {
			m.logger.Warn("Detected orphaned schedule - Temporal schedule not found",
				zap.String("schedule_id", schedule.ID.String()),
				zap.String("temporal_id", schedule.TemporalScheduleID),
			)
			// Mark as deleted in DB
			if err := m.dbOps.UpdateScheduleStatus(ctx, schedule.ID, ScheduleStatusDeleted); err != nil {
				m.logger.Error("Failed to mark orphaned schedule as deleted", zap.Error(err))
				return false, err
			}
			return false, nil
		}
		// Other errors - don't mark as deleted, just report
		m.logger.Warn("Failed to verify Temporal schedule", zap.Error(err))
		return true, nil // Assume exists if we can't verify
	}
	return true, nil
}

// DetectAndCleanOrphanedSchedules checks all active schedules and marks orphaned ones as deleted
func (m *Manager) DetectAndCleanOrphanedSchedules(ctx context.Context) ([]uuid.UUID, error) {
	// Get all active/paused schedules
	schedules, err := m.dbOps.GetAllActiveSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get active schedules: %w", err)
	}

	var orphanedIDs []uuid.UUID
	for _, schedule := range schedules {
		exists, err := m.VerifyScheduleExists(ctx, schedule)
		if err != nil {
			m.logger.Warn("Error verifying schedule",
				zap.String("schedule_id", schedule.ID.String()),
				zap.Error(err))
			continue
		}
		if !exists {
			orphanedIDs = append(orphanedIDs, schedule.ID)
		}
	}

	if len(orphanedIDs) > 0 {
		m.logger.Info("Cleaned up orphaned schedules",
			zap.Int("count", len(orphanedIDs)),
		)
	}

	return orphanedIDs, nil
}

// validateMinInterval checks if a cron expression meets the minimum interval requirement
func (m *Manager) validateMinInterval(cronExpression string) bool {
	if m.config.MinCronIntervalMins <= 0 {
		return true // No minimum enforced
	}

	schedule, err := m.cronParser.Parse(cronExpression)
	if err != nil {
		return false
	}

	// Calculate next two execution times
	tz := time.UTC
	now := time.Now().In(tz)
	next1 := schedule.Next(now)
	next2 := schedule.Next(next1)

	// Check if interval is at least the minimum
	intervalMinutes := next2.Sub(next1).Minutes()
	return intervalMinutes >= float64(m.config.MinCronIntervalMins)
}
