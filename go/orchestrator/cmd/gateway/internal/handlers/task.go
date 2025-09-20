package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	commonpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// TaskHandler handles task-related HTTP requests
type TaskHandler struct {
	orchClient orchpb.OrchestratorServiceClient
	db         *sqlx.DB
	redis      *redis.Client
	logger     *zap.Logger
}

// NewTaskHandler creates a new task handler
func NewTaskHandler(
	orchClient orchpb.OrchestratorServiceClient,
	db *sqlx.DB,
	redis *redis.Client,
	logger *zap.Logger,
) *TaskHandler {
	return &TaskHandler{
		orchClient: orchClient,
		db:         db,
		redis:      redis,
		logger:     logger,
	}
}

// TaskRequest represents a task submission request
type TaskRequest struct {
	Query     string                 `json:"query"`
	SessionID string                 `json:"session_id,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

// TaskResponse represents a task submission response
type TaskResponse struct {
	TaskID    string    `json:"task_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// TaskStatusResponse represents a task status response
type TaskStatusResponse struct {
	TaskID    string                 `json:"task_id"`
	Status    string                 `json:"status"`
	Response  map[string]interface{} `json:"response,omitempty"`
	Error     string                 `json:"error,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// SubmitTask handles POST /api/v1/tasks
func (h *TaskHandler) SubmitTask(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context from auth middleware
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Query == "" {
		h.sendError(w, "Query is required", http.StatusBadRequest)
		return
	}

	// Generate session ID if not provided
	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}

	// Build gRPC request
	grpcReq := &orchpb.SubmitTaskRequest{
		Metadata: &commonpb.TaskMetadata{
			UserId:    userCtx.UserID.String(),
			TenantId:  userCtx.TenantID.String(),
			SessionId: req.SessionID,
		},
		Query: req.Query,
	}

	// Add context if provided
	if len(req.Context) > 0 {
		st, err := structpb.NewStruct(req.Context)
		if err != nil {
			h.logger.Warn("Failed to convert context to struct", zap.Error(err))
		} else {
			grpcReq.Context = st
		}
	}

	// Add tracing headers to gRPC metadata
	if traceParent := r.Header.Get("traceparent"); traceParent != "" {
		// Propagate trace context via gRPC metadata
		md := metadata.Pairs("traceparent", traceParent)
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	// Submit task to orchestrator
	resp, err := h.orchClient.SubmitTask(ctx, grpcReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.InvalidArgument:
				h.sendError(w, st.Message(), http.StatusBadRequest)
			case codes.ResourceExhausted:
				h.sendError(w, "Rate limit exceeded", http.StatusTooManyRequests)
			default:
				h.sendError(w, fmt.Sprintf("Failed to submit task: %v", st.Message()), http.StatusInternalServerError)
			}
		} else {
			h.sendError(w, fmt.Sprintf("Failed to submit task: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Log task submission
	h.logger.Info("Task submitted",
		zap.String("task_id", resp.TaskId),
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("session_id", req.SessionID),
	)

	// Prepare response
	taskResp := TaskResponse{
		TaskID:    resp.TaskId,
		Status:    resp.Status.String(),
		Message:   resp.Message,
		CreatedAt: time.Now(),
	}

	// Add workflow ID header for tracing
	w.Header().Set("X-Workflow-ID", resp.WorkflowId)
	w.Header().Set("X-Session-ID", req.SessionID)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(taskResp)
}

// GetTaskStatus handles GET /api/v1/tasks/{id}
func (h *TaskHandler) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context
	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract task ID from path
	taskID := r.PathValue("id")
	if taskID == "" {
		h.sendError(w, "Task ID is required", http.StatusBadRequest)
		return
	}

	// Get task status from orchestrator
	grpcReq := &orchpb.GetTaskStatusRequest{
		TaskId: taskID,
	}

	resp, err := h.orchClient.GetTaskStatus(ctx, grpcReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			if st.Code() == codes.NotFound {
				h.sendError(w, "Task not found", http.StatusNotFound)
			} else {
				h.sendError(w, fmt.Sprintf("Failed to get task status: %v", st.Message()), http.StatusInternalServerError)
			}
		} else {
			h.sendError(w, fmt.Sprintf("Failed to get task status: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Parse response JSON if present
	var responseData map[string]interface{}
	if resp.Result != "" {
		json.Unmarshal([]byte(resp.Result), &responseData)
	}

	// Prepare response
	statusResp := TaskStatusResponse{
		TaskID:   resp.TaskId,
		Status:   resp.Status.String(),
		Response: responseData,
		Error:    resp.ErrorMessage,
	}

	// Set timestamps to current time since they're not in the proto
	statusResp.CreatedAt = time.Now()
	statusResp.UpdatedAt = time.Now()

	h.logger.Debug("Task status retrieved",
		zap.String("task_id", taskID),
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("status", resp.Status.String()),
	)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Workflow-ID", taskID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(statusResp)
}

// StreamTask handles GET /api/v1/tasks/{id}/stream
func (h *TaskHandler) StreamTask(w http.ResponseWriter, r *http.Request) {
	// Extract task ID from path
	taskID := r.PathValue("id")
	if taskID == "" {
		h.sendError(w, "Task ID is required", http.StatusBadRequest)
		return
	}

	// Rewrite the request to proxy to admin server
	// This will be handled by the streaming proxy
	// For now, we'll redirect to the SSE endpoint with workflow_id
	redirectURL := fmt.Sprintf("/api/v1/stream/sse?workflow_id=%s", taskID)

	// Copy any additional query parameters
	if types := r.URL.Query().Get("types"); types != "" {
		redirectURL += "&types=" + types
	}
	if lastEventID := r.URL.Query().Get("last_event_id"); lastEventID != "" {
		redirectURL += "&last_event_id=" + lastEventID
	}

	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

// sendError sends an error response
func (h *TaskHandler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
