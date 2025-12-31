package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Handler handles OpenAI-compatible API requests.
type Handler struct {
	orchClient     orchpb.OrchestratorServiceClient
	db             *sqlx.DB
	redis          *redis.Client
	logger         *zap.Logger
	registry       *Registry
	translator     *Translator
	sessionManager *SessionManager
	rateLimiter    *RateLimiter
	adminURL       string // URL for SSE streaming (e.g., "http://orchestrator:8081")
}

// NewHandler creates a new OpenAI API handler.
func NewHandler(
	orchClient orchpb.OrchestratorServiceClient,
	db *sqlx.DB,
	redisClient *redis.Client,
	logger *zap.Logger,
	adminURL string,
) (*Handler, error) {
	registry, err := GetRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load model registry: %w", err)
	}

	return &Handler{
		orchClient:     orchClient,
		db:             db,
		redis:          redisClient,
		logger:         logger,
		registry:       registry,
		translator:     NewTranslator(registry),
		sessionManager: NewSessionManager(redisClient, logger),
		rateLimiter:    NewRateLimiter(redisClient, registry, logger),
		adminURL:       strings.TrimRight(adminURL, "/"),
	}, nil
}

// ChatCompletions handles POST /v1/chat/completions
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user context from auth middleware
	userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", ErrorTypeAuthentication, ErrorCodeInvalidAPIKey, http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, fmt.Sprintf("Invalid request body: %v", err), ErrorTypeInvalidRequest, ErrorCodeInvalidRequest, http.StatusBadRequest)
		return
	}

	// Validate model
	modelName := req.Model
	if modelName == "" {
		modelName = h.registry.GetDefaultModel()
	}

	// Initialize metrics recorder
	metrics := NewMetricsRecorder(modelName, "chat_completions", req.Stream)

	if !h.registry.IsValidModel(modelName) {
		metrics.RecordError(ErrorTypeInvalidRequest, ErrorCodeModelNotFound)
		h.sendError(w, fmt.Sprintf("Model '%s' not found. Use GET /v1/models to list available models.", req.Model), ErrorTypeInvalidRequest, ErrorCodeModelNotFound, http.StatusNotFound)
		return
	}

	// Get API key ID for rate limiting (use user ID as fallback)
	apiKeyID := userCtx.UserID.String()
	if userCtx.IsAPIKey && userCtx.APIKeyID != uuid.Nil {
		apiKeyID = userCtx.APIKeyID.String()
	}

	// Check rate limits
	rateLimitResult, err := h.rateLimiter.CheckLimit(ctx, apiKeyID, modelName)
	if err != nil {
		h.logger.Error("Rate limit check failed", zap.Error(err))
		// Continue on error (fail open)
	} else {
		h.rateLimiter.SetRateLimitHeaders(w, rateLimitResult)
		if !rateLimitResult.Allowed {
			// Use the actual limit type that was exceeded (requests or tokens)
			limitType := rateLimitResult.LimitType
			if limitType == "" {
				limitType = "requests" // fallback
			}
			metrics.RecordRateLimited(limitType)
			h.sendError(w, "Rate limit exceeded. Please retry after the specified time.", ErrorTypeRateLimit, ErrorCodeRateLimitExceeded, http.StatusTooManyRequests)
			return
		}
	}

	// Resolve session (with collision handling)
	providedSessionID := r.Header.Get(HeaderSessionID)
	sessionResult, err := h.sessionManager.ResolveSession(
		ctx,
		providedSessionID,
		userCtx.UserID.String(),
		userCtx.TenantID.String(),
		&req,
	)
	if err != nil {
		h.logger.Error("Session resolution failed", zap.Error(err))
		// Continue with derived session on error
	}

	// Echo back X-Session-ID if there was a collision or new session
	if sessionResult != nil && (sessionResult.WasCollision || sessionResult.IsNew) {
		w.Header().Set(HeaderSessionID, sessionResult.SessionID)
		// Also return the full Shannon session ID for history/events lookups
		w.Header().Set("X-Shannon-Session-ID", sessionResult.ShannonSession)
		if sessionResult.IsNew {
			RecordSessionCreated()
		}
		if sessionResult.WasCollision {
			RecordSessionCollision()
		}
	}

	// Translate to Shannon request (use resolved session)
	translated, err := h.translator.TranslateWithSession(&req, userCtx.UserID.String(), userCtx.TenantID.String(), sessionResult)
	if err != nil {
		metrics.RecordError(ErrorTypeInvalidRequest, ErrorCodeInvalidRequest)
		h.sendError(w, err.Error(), ErrorTypeInvalidRequest, ErrorCodeInvalidRequest, http.StatusBadRequest)
		return
	}

	// Add gRPC metadata
	ctx = h.withGRPCMetadata(ctx, r)

	// Submit task to orchestrator
	resp, err := h.orchClient.SubmitTask(ctx, translated.GRPCRequest)
	if err != nil {
		metrics.RecordError(ErrorTypeServer, ErrorCodeInternalError)
		h.handleGRPCError(w, err)
		return
	}

	sessionID := ""
	shannonSessionID := ""
	if sessionResult != nil {
		sessionID = sessionResult.SessionID
		shannonSessionID = sessionResult.ShannonSession
	}

	h.logger.Info("OpenAI task submitted",
		zap.String("task_id", resp.TaskId),
		zap.String("workflow_id", resp.WorkflowId),
		zap.String("model", translated.ModelName),
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("session_id", sessionID),
		zap.String("shannon_session_id", shannonSessionID),
		zap.Bool("stream", translated.Stream),
	)

	// Handle streaming vs non-streaming response
	if translated.Stream {
		h.handleStreamingResponse(ctx, w, r, resp.WorkflowId, translated.ModelName, &req, apiKeyID, metrics)
	} else {
		h.handleNonStreamingResponse(ctx, w, resp.TaskId, resp.WorkflowId, translated.ModelName, apiKeyID, metrics)
	}
}

// handleStreamingResponse connects to Shannon SSE and streams OpenAI-format chunks.
func (h *Handler) handleStreamingResponse(ctx context.Context, w http.ResponseWriter, r *http.Request, workflowID, modelName string, req *ChatCompletionRequest, apiKeyID string, metrics *MetricsRecorder) {
	// Build SSE URL - include agent events for rich UI experiences
	// LLM events: LLM_PARTIAL (streaming), LLM_OUTPUT (final)
	// Stream lifecycle: STREAM_END
	// Subscribe to all event types that the streamer forwards:
	// - LLM events: LLM_PARTIAL, LLM_OUTPUT
	// - Stream lifecycle: STREAM_END
	// - Workflow lifecycle: WORKFLOW_STARTED, WORKFLOW_COMPLETED, WORKFLOW_FAILED, WORKFLOW_PAUSING, WORKFLOW_PAUSED, WORKFLOW_RESUMED, WORKFLOW_CANCELLING, WORKFLOW_CANCELLED
	// - Agent lifecycle: AGENT_STARTED, AGENT_COMPLETED, AGENT_THINKING
	// - Tool events: TOOL_INVOKED, TOOL_OBSERVATION
	// - Progress: PROGRESS, DATA_PROCESSING, WAITING, ERROR_RECOVERY
	// - Team/coordination: TEAM_RECRUITED, TEAM_RETIRED, TEAM_STATUS, ROLE_ASSIGNED, DELEGATION, DEPENDENCY_SATISFIED
	// - Budget/approval: BUDGET_THRESHOLD, APPROVAL_REQUESTED, APPROVAL_DECISION
	// - Errors: ERROR_OCCURRED
	sseEventTypes := "LLM_PARTIAL,LLM_OUTPUT,STREAM_END," +
		"WORKFLOW_STARTED,WORKFLOW_COMPLETED,WORKFLOW_FAILED,WORKFLOW_PAUSING,WORKFLOW_PAUSED,WORKFLOW_RESUMED,WORKFLOW_CANCELLING,WORKFLOW_CANCELLED," +
		"AGENT_STARTED,AGENT_COMPLETED,AGENT_THINKING," +
		"TOOL_INVOKED,TOOL_OBSERVATION," +
		"PROGRESS,DATA_PROCESSING,WAITING,ERROR_RECOVERY," +
		"TEAM_RECRUITED,TEAM_RETIRED,TEAM_STATUS,ROLE_ASSIGNED,DELEGATION,DEPENDENCY_SATISFIED," +
		"BUDGET_THRESHOLD,APPROVAL_REQUESTED,APPROVAL_DECISION," +
		"ERROR_OCCURRED"
	sseURL := fmt.Sprintf("%s/stream/sse?workflow_id=%s&types=%s", h.adminURL, workflowID, sseEventTypes)

	// Create SSE request
	sseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
	if err != nil {
		metrics.RecordError(ErrorTypeServer, ErrorCodeInternalError)
		h.sendError(w, "Failed to create stream request", ErrorTypeServer, ErrorCodeInternalError, http.StatusInternalServerError)
		return
	}

	// Copy auth headers
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		sseReq.Header.Set("Authorization", authHeader)
	}
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		sseReq.Header.Set("X-API-Key", apiKey)
	}

	// Make SSE request
	// Use custom transport with connection timeouts but no request timeout.
	// Deep research and other long-running workflows can stream beyond 10 minutes.
	// The client request context controls cancellation when the caller disconnects.
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second, // Connection timeout
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second, // Wait for response headers
			IdleConnTimeout:       90 * time.Second,
		},
	}
	sseResp, err := client.Do(sseReq)
	if err != nil {
		h.logger.Error("Failed to connect to SSE", zap.Error(err), zap.String("url", sseURL))
		metrics.RecordError(ErrorTypeServer, ErrorCodeInternalError)
		metrics.RecordStreamError("connection_failed")
		h.sendError(w, "Failed to connect to stream", ErrorTypeServer, ErrorCodeInternalError, http.StatusBadGateway)
		return
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		h.logger.Error("SSE returned error", zap.Int("status", sseResp.StatusCode))
		metrics.RecordError(ErrorTypeServer, ErrorCodeInternalError)
		metrics.RecordStreamError("bad_status")
		h.sendError(w, "Stream unavailable", ErrorTypeServer, ErrorCodeInternalError, http.StatusBadGateway)
		return
	}

	// Create streamer and transform events
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	streamer := NewStreamerWithMetrics(h.logger, modelName, metrics)

	if err := streamer.StreamResponse(ctx, sseResp.Body, w, includeUsage); err != nil {
		h.logger.Error("Stream error", zap.Error(err))
		metrics.RecordStreamError("stream_interrupted")
		// Error already written to stream
	} else {
		metrics.RecordSuccess()
		metrics.RecordStreamComplete()
	}

	usage := streamer.GetUsage()
	if usage != nil && usage.TotalTokens == 0 {
		usage = h.getUsageFromDB(ctx, workflowID)
	}
	if usage != nil && usage.TotalTokens > 0 {
		metrics.RecordTokens(usage.PromptTokens, usage.CompletionTokens)
		if err := h.rateLimiter.RecordTokens(ctx, apiKeyID, modelName, usage.TotalTokens); err != nil {
			h.logger.Debug("Failed to record token usage", zap.Error(err))
		}
	}
}

// handleNonStreamingResponse waits for completion and returns full response.
func (h *Handler) handleNonStreamingResponse(ctx context.Context, w http.ResponseWriter, taskID, workflowID, modelName, apiKeyID string, metrics *MetricsRecorder) {
	// Poll for completion
	var result string
	var usage *Usage

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// 35-minute timeout to support deep research and other long-running tasks.
	// For very long tasks, consider using streaming or async polling instead.
	timeout := time.After(35 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			metrics.RecordError(ErrorTypeServer, "request_cancelled")
			h.sendError(w, "Request cancelled", ErrorTypeServer, ErrorCodeInternalError, http.StatusGatewayTimeout)
			return
		case <-timeout:
			metrics.RecordError(ErrorTypeServer, "timeout")
			h.sendError(w, "Request timed out", ErrorTypeServer, ErrorCodeInternalError, http.StatusGatewayTimeout)
			return
		case <-ticker.C:
			// Check task status
			statusResp, err := h.orchClient.GetTaskStatus(ctx, &orchpb.GetTaskStatusRequest{TaskId: taskID})
			if err != nil {
				h.logger.Error("Failed to get task status", zap.Error(err))
				continue
			}

			switch statusResp.Status {
			case orchpb.TaskStatus_TASK_STATUS_COMPLETED:
				result = statusResp.Result
				// Try to get usage from database
				usage = h.getUsageFromDB(ctx, workflowID)
				if usage != nil && usage.TotalTokens > 0 {
					metrics.RecordTokens(usage.PromptTokens, usage.CompletionTokens)
					if err := h.rateLimiter.RecordTokens(ctx, apiKeyID, modelName, usage.TotalTokens); err != nil {
						h.logger.Debug("Failed to record token usage", zap.Error(err))
					}
				}
				goto respond
			case orchpb.TaskStatus_TASK_STATUS_FAILED:
				metrics.RecordError(ErrorTypeServer, "task_failed")
				h.sendError(w, statusResp.ErrorMessage, ErrorTypeServer, ErrorCodeInternalError, http.StatusInternalServerError)
				return
			case orchpb.TaskStatus_TASK_STATUS_CANCELLED:
				metrics.RecordError(ErrorTypeServer, "task_cancelled")
				h.sendError(w, "Task was cancelled", ErrorTypeServer, ErrorCodeInternalError, http.StatusInternalServerError)
				return
			}
		}
	}

respond:
	metrics.RecordSuccess()
	// Build response
	response := &ChatCompletionResponse{
		ID:      GenerateCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelName,
		Choices: []Choice{
			{
				Index: 0,
				Message: &ChatMessage{
					Role:    "assistant",
					Content: result,
				},
				FinishReason: "stop",
			},
		},
		Usage: usage,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListModels handles GET /v1/models
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	// Auth check (optional for models endpoint, but good practice)
	if _, ok := r.Context().Value(auth.UserContextKey).(*auth.UserContext); !ok {
		h.sendError(w, "Unauthorized", ErrorTypeAuthentication, ErrorCodeInvalidAPIKey, http.StatusUnauthorized)
		return
	}

	models := h.registry.ListModels()
	response := ModelsResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetModel handles GET /v1/models/{model}
func (h *Handler) GetModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("model")
	if modelID == "" {
		h.sendError(w, "Model ID required", ErrorTypeInvalidRequest, ErrorCodeInvalidRequest, http.StatusBadRequest)
		return
	}

	if !h.registry.IsValidModel(modelID) {
		h.sendError(w, fmt.Sprintf("Model '%s' not found", modelID), ErrorTypeNotFound, ErrorCodeModelNotFound, http.StatusNotFound)
		return
	}

	model, _ := h.registry.GetModel(modelID)
	response := ModelObject{
		ID:      modelID,
		Object:  "model",
		Created: time.Now().Unix(),
		OwnedBy: "shannon",
	}

	// Add description in a non-standard but useful way
	if model.Description != "" {
		w.Header().Set("X-Model-Description", model.Description)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// getUsageFromDB retrieves usage statistics from the database.
func (h *Handler) getUsageFromDB(ctx context.Context, workflowID string) *Usage {
	var totalTokens, promptTokens, completionTokens int
	err := h.db.QueryRowxContext(ctx, `
		SELECT COALESCE(total_tokens, 0), COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0)
		FROM task_executions WHERE workflow_id = $1
	`, workflowID).Scan(&totalTokens, &promptTokens, &completionTokens)

	if err != nil {
		h.logger.Debug("Failed to get usage from DB", zap.Error(err))
		return nil
	}

	if totalTokens == 0 && promptTokens == 0 && completionTokens == 0 {
		return nil
	}

	return &Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}
}

// extractToken extracts the auth token from the request.
func (h *Handler) extractToken(r *http.Request) string {
	// Try Authorization header first
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Try API key
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return apiKey
	}

	return ""
}

// withGRPCMetadata adds authentication metadata to gRPC context.
func (h *Handler) withGRPCMetadata(ctx context.Context, r *http.Request) context.Context {
	md := metadata.New(nil)

	// Forward user ID
	if userCtx, ok := ctx.Value(auth.UserContextKey).(*auth.UserContext); ok {
		md.Set("x-user-id", userCtx.UserID.String())
		md.Set("x-tenant-id", userCtx.TenantID.String())
	}

	// Forward auth headers.
	// IMPORTANT: orchestrator gRPC auth checks Authorization as JWT first and does not fall back to API key,
	// so we must map Bearer API keys to x-api-key metadata (not authorization).
	if apiKey := strings.TrimSpace(r.Header.Get("X-API-Key")); apiKey != "" {
		md.Set("x-api-key", normalizeAPIKey(apiKey))
	} else if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		raw := strings.TrimSpace(authHeader[len("bearer "):])
		if raw != "" {
			if isLikelyJWT(raw) {
				md.Set("authorization", authHeader)
			} else {
				md.Set("x-api-key", normalizeAPIKey(raw))
			}
		}
	}

	// Forward trace headers
	if traceID := r.Header.Get("traceparent"); traceID != "" {
		md.Set("traceparent", traceID)
	}

	return metadata.NewOutgoingContext(ctx, md)
}

func normalizeAPIKey(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "sk-shannon-") {
		token = strings.TrimPrefix(token, "sk-shannon-")
		if !strings.HasPrefix(token, "sk_") {
			token = "sk_" + token
		}
	}
	return token
}

func isLikelyJWT(token string) bool {
	return strings.Count(token, ".") == 2
}

// handleGRPCError converts gRPC errors to OpenAI error responses.
func (h *Handler) handleGRPCError(w http.ResponseWriter, err error) {
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.InvalidArgument:
			h.sendError(w, st.Message(), ErrorTypeInvalidRequest, ErrorCodeInvalidRequest, http.StatusBadRequest)
		case codes.Unauthenticated:
			h.sendError(w, "Invalid API key", ErrorTypeAuthentication, ErrorCodeInvalidAPIKey, http.StatusUnauthorized)
		case codes.PermissionDenied:
			h.sendError(w, "Permission denied", ErrorTypePermission, ErrorCodeInvalidRequest, http.StatusForbidden)
		case codes.NotFound:
			h.sendError(w, st.Message(), ErrorTypeNotFound, ErrorCodeModelNotFound, http.StatusNotFound)
		case codes.ResourceExhausted:
			h.sendError(w, "Rate limit exceeded", ErrorTypeRateLimit, ErrorCodeRateLimitExceeded, http.StatusTooManyRequests)
		default:
			h.sendError(w, st.Message(), ErrorTypeServer, ErrorCodeInternalError, http.StatusInternalServerError)
		}
	} else {
		h.sendError(w, err.Error(), ErrorTypeServer, ErrorCodeInternalError, http.StatusInternalServerError)
	}
}

// sendError sends an OpenAI-compatible error response.
func (h *Handler) sendError(w http.ResponseWriter, message, errType, code string, httpStatus int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	resp := NewErrorResponse(message, errType, code)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("Failed to encode error response", zap.Error(err))
	}
}

// streamContent is a helper to write raw bytes for non-buffered streaming.
func streamContent(w io.Writer, content string) {
	if flusher, ok := w.(http.Flusher); ok {
		w.Write([]byte(content))
		flusher.Flush()
	}
}
