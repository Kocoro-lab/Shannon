package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ScheduleHandler struct {
	orchClient orchpb.OrchestratorServiceClient
	db         *sqlx.DB
	logger     *zap.Logger
}

func NewScheduleHandler(client orchpb.OrchestratorServiceClient, db *sqlx.DB, logger *zap.Logger) *ScheduleHandler {
	return &ScheduleHandler{
		orchClient: client,
		db:         db,
		logger:     logger,
	}
}

// CreateSchedule handles POST /api/v1/schedules
func (h *ScheduleHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name               string            `json:"name"`
		Description        string            `json:"description"`
		CronExpression     string            `json:"cron_expression"`
		Timezone           string            `json:"timezone"`
		TaskQuery          string            `json:"task_query"`
		TaskContext        map[string]string `json:"task_context"`
		MaxBudgetPerRunUSD float64           `json:"max_budget_per_run_usd"`
		TimeoutSeconds     int32             `json:"timeout_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx = withGRPCMetadata(ctx, r)

	createReq := &orchpb.CreateScheduleRequest{
		Name:               req.Name,
		Description:        req.Description,
		CronExpression:     req.CronExpression,
		Timezone:           req.Timezone,
		TaskQuery:          req.TaskQuery,
		TaskContext:        req.TaskContext,
		MaxBudgetPerRunUsd: req.MaxBudgetPerRunUSD,
		TimeoutSeconds:     req.TimeoutSeconds,
	}

	resp, err := h.orchClient.CreateSchedule(ctx, createReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.InvalidArgument:
				h.sendError(w, st.Message(), http.StatusBadRequest)
			case codes.ResourceExhausted:
				h.sendError(w, st.Message(), http.StatusTooManyRequests)
			default:
				h.sendError(w, st.Message(), http.StatusInternalServerError)
			}
			return
		}
		h.sendError(w, "Failed to create schedule", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Schedule created",
		zap.String("schedule_id", resp.ScheduleId),
		zap.String("user_id", userCtx.UserID.String()),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"schedule_id": resp.ScheduleId,
		"message":     resp.Message,
		"next_run_at": resp.NextRunAt,
	})
}

// GetSchedule handles GET /api/v1/schedules/{id}
func (h *ScheduleHandler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scheduleID := r.PathValue("id")
	if scheduleID == "" {
		h.sendError(w, "Schedule ID required", http.StatusBadRequest)
		return
	}

	ctx = withGRPCMetadata(ctx, r)

	getReq := &orchpb.GetScheduleRequest{ScheduleId: scheduleID}
	resp, err := h.orchClient.GetSchedule(ctx, getReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.NotFound:
				h.sendError(w, "Schedule not found", http.StatusNotFound)
			default:
				h.sendError(w, st.Message(), http.StatusInternalServerError)
			}
			return
		}
		h.sendError(w, "Failed to get schedule", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp.Schedule)
}

// ListSchedules handles GET /api/v1/schedules
func (h *ScheduleHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	statusFilter := r.URL.Query().Get("status")

	ctx = withGRPCMetadata(ctx, r)

	listReq := &orchpb.ListSchedulesRequest{
		Page:     int32(page),
		PageSize: int32(pageSize),
		Status:   statusFilter,
	}

	resp, err := h.orchClient.ListSchedules(ctx, listReq)
	if err != nil {
		h.sendError(w, "Failed to list schedules", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// UpdateSchedule handles PUT /api/v1/schedules/{id}
func (h *ScheduleHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scheduleID := r.PathValue("id")
	if scheduleID == "" {
		h.sendError(w, "Schedule ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Name               *string           `json:"name"`
		Description        *string           `json:"description"`
		CronExpression     *string           `json:"cron_expression"`
		Timezone           *string           `json:"timezone"`
		TaskQuery          *string           `json:"task_query"`
		TaskContext        map[string]string `json:"task_context"`
		MaxBudgetPerRunUSD *float64          `json:"max_budget_per_run_usd"`
		TimeoutSeconds     *int32            `json:"timeout_seconds"`
		ClearTaskContext   bool              `json:"clear_task_context"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx = withGRPCMetadata(ctx, r)

	updateReq := &orchpb.UpdateScheduleRequest{
		ScheduleId:         scheduleID,
		Name:               req.Name,
		Description:        req.Description,
		CronExpression:     req.CronExpression,
		Timezone:           req.Timezone,
		TaskQuery:          req.TaskQuery,
		TaskContext:        req.TaskContext,
		MaxBudgetPerRunUsd: req.MaxBudgetPerRunUSD,
		TimeoutSeconds:     req.TimeoutSeconds,
		ClearTaskContext:   req.ClearTaskContext,
	}

	resp, err := h.orchClient.UpdateSchedule(ctx, updateReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.NotFound:
				h.sendError(w, "Schedule not found", http.StatusNotFound)
			case codes.InvalidArgument:
				h.sendError(w, st.Message(), http.StatusBadRequest)
			default:
				h.sendError(w, st.Message(), http.StatusInternalServerError)
			}
			return
		}
		h.sendError(w, "Failed to update schedule", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Schedule updated",
		zap.String("schedule_id", scheduleID),
		zap.String("user_id", userCtx.UserID.String()),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// PauseSchedule handles POST /api/v1/schedules/{id}/pause
func (h *ScheduleHandler) PauseSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scheduleID := r.PathValue("id")
	if scheduleID == "" {
		h.sendError(w, "Schedule ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx = withGRPCMetadata(ctx, r)

	pauseReq := &orchpb.PauseScheduleRequest{
		ScheduleId: scheduleID,
		Reason:     req.Reason,
	}
	resp, err := h.orchClient.PauseSchedule(ctx, pauseReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.NotFound:
				h.sendError(w, "Schedule not found", http.StatusNotFound)
			default:
				h.sendError(w, st.Message(), http.StatusInternalServerError)
			}
			return
		}
		h.sendError(w, "Failed to pause schedule", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Schedule paused",
		zap.String("schedule_id", scheduleID),
		zap.String("user_id", userCtx.UserID.String()),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ResumeSchedule handles POST /api/v1/schedules/{id}/resume
func (h *ScheduleHandler) ResumeSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scheduleID := r.PathValue("id")
	if scheduleID == "" {
		h.sendError(w, "Schedule ID required", http.StatusBadRequest)
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx = withGRPCMetadata(ctx, r)

	resumeReq := &orchpb.ResumeScheduleRequest{
		ScheduleId: scheduleID,
		Reason:     req.Reason,
	}
	resp, err := h.orchClient.ResumeSchedule(ctx, resumeReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.NotFound:
				h.sendError(w, "Schedule not found", http.StatusNotFound)
			default:
				h.sendError(w, st.Message(), http.StatusInternalServerError)
			}
			return
		}
		h.sendError(w, "Failed to resume schedule", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Schedule resumed",
		zap.String("schedule_id", scheduleID),
		zap.String("user_id", userCtx.UserID.String()),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// DeleteSchedule handles DELETE /api/v1/schedules/{id}
func (h *ScheduleHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scheduleID := r.PathValue("id")
	if scheduleID == "" {
		h.sendError(w, "Schedule ID required", http.StatusBadRequest)
		return
	}

	ctx = withGRPCMetadata(ctx, r)

	deleteReq := &orchpb.DeleteScheduleRequest{ScheduleId: scheduleID}
	resp, err := h.orchClient.DeleteSchedule(ctx, deleteReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.NotFound:
				h.sendError(w, "Schedule not found", http.StatusNotFound)
			default:
				h.sendError(w, st.Message(), http.StatusInternalServerError)
			}
			return
		}
		h.sendError(w, "Failed to delete schedule", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Schedule deleted",
		zap.String("schedule_id", scheduleID),
		zap.String("user_id", userCtx.UserID.String()),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ScheduleRunResponse represents a single schedule run in the API response
type ScheduleRunResponse struct {
	WorkflowID       string     `json:"workflow_id"`
	Query            string     `json:"query"`
	Status           string     `json:"status"`
	Result           *string    `json:"result,omitempty"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
	ModelUsed        *string    `json:"model_used,omitempty"`
	Provider         *string    `json:"provider,omitempty"`
	TotalTokens      int        `json:"total_tokens"`
	TotalCostUSD     float64    `json:"total_cost_usd"`
	DurationMs       *int       `json:"duration_ms,omitempty"`
	TriggeredAt      time.Time  `json:"triggered_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// ListScheduleRunsResponse represents the response for listing schedule runs
type ListScheduleRunsResponse struct {
	Runs       []ScheduleRunResponse `json:"runs"`
	TotalCount int                   `json:"total_count"`
	Page       int                   `json:"page"`
	PageSize   int                   `json:"page_size"`
}

// GetScheduleRuns handles GET /api/v1/schedules/{id}/runs
func (h *ScheduleHandler) GetScheduleRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	scheduleID := r.PathValue("id")
	if scheduleID == "" {
		h.sendError(w, "Schedule ID required", http.StatusBadRequest)
		return
	}

	// Parse pagination params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Verify user owns this schedule
	var ownerUserID string
	err := h.db.GetContext(ctx, &ownerUserID, `
		SELECT user_id::text FROM scheduled_tasks
		WHERE id = $1 AND status != 'DELETED'
	`, scheduleID)
	if err != nil {
		h.sendError(w, "Schedule not found", http.StatusNotFound)
		return
	}
	if ownerUserID != userCtx.UserID.String() {
		h.sendError(w, "Schedule not found", http.StatusNotFound)
		return
	}

	// Get total count
	var totalCount int
	err = h.db.GetContext(ctx, &totalCount, `
		SELECT COUNT(*) FROM v_schedule_execution_history
		WHERE schedule_id = $1
	`, scheduleID)
	if err != nil {
		h.logger.Error("Failed to count schedule runs", zap.Error(err))
		h.sendError(w, "Failed to list schedule runs", http.StatusInternalServerError)
		return
	}

	// Query runs using the view
	type runRow struct {
		WorkflowID   *string    `db:"workflow_id"`
		Query        *string    `db:"query"`
		Status       *string    `db:"status"`
		Result       *string    `db:"result"`
		ErrorMessage *string    `db:"error_message"`
		ModelUsed    *string    `db:"model_used"`
		Provider     *string    `db:"provider"`
		TotalTokens  *int       `db:"total_tokens"`
		TotalCostUSD *float64   `db:"total_cost_usd"`
		DurationMs   *int       `db:"duration_ms"`
		TriggeredAt  time.Time  `db:"triggered_at"`
		StartedAt    *time.Time `db:"started_at"`
		CompletedAt  *time.Time `db:"completed_at"`
	}

	var rows []runRow
	err = h.db.SelectContext(ctx, &rows, `
		SELECT
			workflow_id, query, status, result, error_message,
			model_used, provider, total_tokens, total_cost_usd, duration_ms,
			triggered_at, started_at, completed_at
		FROM v_schedule_execution_history
		WHERE schedule_id = $1
		ORDER BY triggered_at DESC
		LIMIT $2 OFFSET $3
	`, scheduleID, pageSize, offset)
	if err != nil {
		h.logger.Error("Failed to query schedule runs", zap.Error(err))
		h.sendError(w, "Failed to list schedule runs", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	runs := make([]ScheduleRunResponse, 0, len(rows))
	for _, row := range rows {
		run := ScheduleRunResponse{
			TriggeredAt: row.TriggeredAt,
		}
		if row.WorkflowID != nil {
			run.WorkflowID = *row.WorkflowID
		}
		if row.Query != nil {
			run.Query = *row.Query
		}
		if row.Status != nil {
			run.Status = *row.Status
		} else {
			run.Status = "UNKNOWN"
		}
		run.Result = row.Result
		run.ErrorMessage = row.ErrorMessage
		run.ModelUsed = row.ModelUsed
		run.Provider = row.Provider
		if row.TotalTokens != nil {
			run.TotalTokens = *row.TotalTokens
		}
		if row.TotalCostUSD != nil {
			run.TotalCostUSD = *row.TotalCostUSD
		}
		run.DurationMs = row.DurationMs
		run.StartedAt = row.StartedAt
		run.CompletedAt = row.CompletedAt

		runs = append(runs, run)
	}

	resp := ListScheduleRunsResponse{
		Runs:       runs,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *ScheduleHandler) sendError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
