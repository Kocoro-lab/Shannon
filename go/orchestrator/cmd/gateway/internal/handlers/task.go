package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	commonpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

// TaskHandler handles task-related HTTP requests
type TaskHandler struct {
	orchClient orchpb.OrchestratorServiceClient
	db         *sqlx.DB
	redis      *redis.Client
	logger     *zap.Logger
}

// ResearchStrategiesConfig represents research strategy presets loaded from YAML
type ResearchStrategiesConfig struct {
	Strategies map[string]struct {
		MaxIterations            int  `yaml:"max_iterations"` // deprecated
		VerificationEnabled      bool `yaml:"verification_enabled"`
		MaxConcurrentAgents      int  `yaml:"max_concurrent_agents"`
		ReactMaxIterations       int  `yaml:"react_max_iterations"`
		GapFillingEnabled        bool `yaml:"gap_filling_enabled"`
		GapFillingMaxGaps        int  `yaml:"gap_filling_max_gaps"`
		GapFillingMaxIterations  int  `yaml:"gap_filling_max_iterations"`
		GapFillingCheckCitations bool `yaml:"gap_filling_check_citations"`
	} `yaml:"strategies"`
}

// Cached research strategies configuration
var (
	researchStrategiesOnce   sync.Once
	researchStrategiesCached *ResearchStrategiesConfig
	researchStrategiesErr    error
)

// loadResearchStrategies loads presets from standard locations
func loadResearchStrategies() (*ResearchStrategiesConfig, error) {
	researchStrategiesOnce.Do(func() {
		candidates := []string{"config/research_strategies.yaml", "/app/config/research_strategies.yaml"}
		for _, p := range candidates {
			if _, statErr := os.Stat(p); statErr == nil {
				data, rerr := os.ReadFile(p)
				if rerr != nil {
					researchStrategiesErr = rerr
					return
				}
				var tmp ResearchStrategiesConfig
				if yerr := yaml.Unmarshal(data, &tmp); yerr != nil {
					researchStrategiesErr = yerr
					return
				}
				researchStrategiesCached = &tmp
				researchStrategiesErr = nil
				return
			}
		}
		researchStrategiesErr = fmt.Errorf("research_strategies.yaml not found")
	})
	return researchStrategiesCached, researchStrategiesErr
}

// applyStrategyPreset seeds ctxMap with preset defaults when absent
func applyStrategyPreset(ctxMap map[string]interface{}, strategy string) {
	s := strings.ToLower(strings.TrimSpace(strategy))
	if s == "" {
		return
	}
	cfg, err := loadResearchStrategies()
	if err != nil || cfg == nil || cfg.Strategies == nil {
		return
	}
	preset, ok := cfg.Strategies[s]
	if !ok {
		return
	}
	// Seed react_max_iterations (independent of deprecated max_iterations)
	if _, ok := ctxMap["react_max_iterations"]; !ok && preset.ReactMaxIterations >= 1 && preset.ReactMaxIterations <= 10 {
		ctxMap["react_max_iterations"] = preset.ReactMaxIterations
	}
	// Seed max_concurrent_agents
	if _, ok := ctxMap["max_concurrent_agents"]; !ok && preset.MaxConcurrentAgents >= 1 && preset.MaxConcurrentAgents <= 20 {
		ctxMap["max_concurrent_agents"] = preset.MaxConcurrentAgents
	}
	// Seed enable_verification
	if _, ok := ctxMap["enable_verification"]; !ok {
		ctxMap["enable_verification"] = preset.VerificationEnabled
	}
	// Seed gap filling settings (always apply, not gated by max_iterations)
	if _, ok := ctxMap["gap_filling_enabled"]; !ok {
		ctxMap["gap_filling_enabled"] = preset.GapFillingEnabled
	}
	if _, ok := ctxMap["gap_filling_max_gaps"]; !ok && preset.GapFillingMaxGaps > 0 {
		ctxMap["gap_filling_max_gaps"] = preset.GapFillingMaxGaps
	}
	if _, ok := ctxMap["gap_filling_max_iterations"]; !ok && preset.GapFillingMaxIterations > 0 {
		ctxMap["gap_filling_max_iterations"] = preset.GapFillingMaxIterations
	}
	if _, ok := ctxMap["gap_filling_check_citations"]; !ok {
		ctxMap["gap_filling_check_citations"] = preset.GapFillingCheckCitations
	}
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
	// Optional execution mode hint (e.g., "supervisor").
	// Routed via metadata labels to orchestrator.
	Mode string `json:"mode,omitempty"`
	// Optional model tier hint; if provided, inject into context
	// so downstream services can honor it (small|medium|large).
	ModelTier string `json:"model_tier,omitempty"`
	// Optional specific model override; if provided, inject into context
	// (e.g., "gpt-5-2025-08-07", "gpt-5-pro-2025-10-06", "claude-sonnet-4-5-20250929").
	ModelOverride    string `json:"model_override,omitempty"`
	ProviderOverride string `json:"provider_override,omitempty"`
	// Phase 6: Strategy presets (mapped into context)
	ResearchStrategy    string `json:"research_strategy,omitempty"`     // quick|standard|deep|academic
	MaxIterations       *int   `json:"max_iterations,omitempty"`        // Optional override
	MaxConcurrentAgents *int   `json:"max_concurrent_agents,omitempty"` // Optional override
	EnableVerification  *bool  `json:"enable_verification,omitempty"`   // Optional flag
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
	TaskID     string                 `json:"task_id"`
	WorkflowID string                 `json:"workflow_id,omitempty"` // Same as task_id, for clarity
	Status     string                 `json:"status"`
	Result     string                 `json:"result,omitempty"`   // Raw result from LLM (plain text or JSON)
	Response   map[string]interface{} `json:"response,omitempty"` // Parsed JSON (backward compatibility)
	Error      string                 `json:"error,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
	// Extra metadata to enable "reply" UX
	Query     string                 `json:"query,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
	Mode      string                 `json:"mode,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"` // Task context (force_research, research_strategy, etc.)
	// Usage metadata
	ModelUsed string                 `json:"model_used,omitempty"`
	Provider  string                 `json:"provider,omitempty"`
	Usage     map[string]interface{} `json:"usage,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // Task metadata (citations, etc.)
}

// ListTasksResponse represents the list tasks response
type ListTasksResponse struct {
	Tasks      []TaskSummary `json:"tasks"`
	TotalCount int32         `json:"total_count"`
}

// TaskSummary represents a single task in listing
type TaskSummary struct {
	TaskID          string                 `json:"task_id"`
	Query           string                 `json:"query,omitempty"`
	Status          string                 `json:"status"`
	Mode            string                 `json:"mode,omitempty"`
	CreatedAt       *time.Time             `json:"created_at,omitempty"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	TotalTokenUsage map[string]interface{} `json:"total_token_usage,omitempty"`
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
			Labels:    map[string]string{},
		},
		Query: req.Query,
	}

	// Ensure context map exists so we can inject optional fields safely
	ctxMap := map[string]interface{}{}
	if len(req.Context) > 0 {
		for k, v := range req.Context {
			ctxMap[k] = v
		}
	}
	// Normalize alias: context.template_name -> context.template (if not already set)
	if _, ok := ctxMap["template"]; !ok {
		if v, ok2 := ctxMap["template_name"].(string); ok2 {
			if tv := strings.TrimSpace(v); tv != "" {
				ctxMap["template"] = tv
			}
		}
	}
	// Validate and inject model_tier from top-level (top-level wins)
	if mt := strings.TrimSpace(strings.ToLower(req.ModelTier)); mt != "" {
		switch mt {
		case "small", "medium", "large":
			// Top-level overrides any value in context
			ctxMap["model_tier"] = mt
			h.logger.Debug("Applied top-level model_tier override", zap.String("model_tier", mt))
		default:
			h.sendError(w, "Invalid model_tier (allowed: small, medium, large)", http.StatusBadRequest)
			return
		}
	}
	// Inject top-level model_override when provided
	if mo := strings.TrimSpace(req.ModelOverride); mo != "" {
		ctxMap["model_override"] = mo
		h.logger.Debug("Applied top-level model_override", zap.String("model_override", mo))
	}
	// Inject top-level provider_override when provided
	if po := strings.TrimSpace(strings.ToLower(req.ProviderOverride)); po != "" {
		// Validate provider exists
		validProviders := []string{"openai", "anthropic", "google", "groq", "xai", "deepseek", "qwen", "zai", "ollama", "mistral", "cohere"}
		isValid := false
		for _, valid := range validProviders {
			if po == valid {
				isValid = true
				break
			}
		}
		if !isValid {
			h.sendError(w, fmt.Sprintf("Invalid provider_override: %s (allowed: %s)", po, strings.Join(validProviders, ", ")), http.StatusBadRequest)
			return
		}
		ctxMap["provider_override"] = po
		h.logger.Debug("Applied top-level provider_override", zap.String("provider_override", po))
	}

	// Map research strategy controls into context (streaming endpoint parity)
	if rs := strings.TrimSpace(strings.ToLower(req.ResearchStrategy)); rs != "" {
		switch rs {
		case "quick", "standard", "deep", "academic":
			ctxMap["research_strategy"] = rs
		default:
			h.sendError(w, "Invalid research_strategy (allowed: quick, standard, deep, academic)", http.StatusBadRequest)
			return
		}
	}
	if req.MaxIterations != nil {
		if *req.MaxIterations <= 0 || *req.MaxIterations > 50 {
			h.sendError(w, "max_iterations out of range (1..50)", http.StatusBadRequest)
			return
		}
		ctxMap["max_iterations"] = *req.MaxIterations
	}
	if req.MaxConcurrentAgents != nil {
		if *req.MaxConcurrentAgents <= 0 || *req.MaxConcurrentAgents > 20 {
			h.sendError(w, "max_concurrent_agents out of range (1..20)", http.StatusBadRequest)
			return
		}
		ctxMap["max_concurrent_agents"] = *req.MaxConcurrentAgents
	}
	if req.EnableVerification != nil {
		ctxMap["enable_verification"] = *req.EnableVerification
	}

	// Apply research strategy presets (seed defaults only when absent)
	if rs, ok := ctxMap["research_strategy"].(string); ok && strings.TrimSpace(rs) != "" {
		applyStrategyPreset(ctxMap, rs)
	}

	// Conflict validation: disable_ai=true cannot be combined with model controls
	var disableAI bool
	if v, exists := ctxMap["disable_ai"]; exists {
		switch t := v.(type) {
		case bool:
			disableAI = t
		case string:
			s := strings.TrimSpace(strings.ToLower(t))
			disableAI = s == "true" || s == "1" || s == "yes" || s == "y"
		case float64:
			disableAI = t != 0
		case int:
			disableAI = t != 0
		}
	}
	if disableAI {
		// top-level conflicts
		if req.ModelTier != "" || req.ModelOverride != "" || req.ProviderOverride != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
		// context conflicts
		if vt, ok := ctxMap["model_tier"].(string); ok && strings.TrimSpace(vt) != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
		if vo, ok := ctxMap["model_override"].(string); ok && strings.TrimSpace(vo) != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
		if vp, ok := ctxMap["provider_override"].(string); ok && strings.TrimSpace(vp) != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
	}
	// Add context if present
	if len(ctxMap) > 0 {
		st, err := structpb.NewStruct(ctxMap)
		if err != nil {
			h.logger.Warn("Failed to convert context to struct", zap.Error(err))
		} else {
			grpcReq.Context = st
		}
	}

	// Propagate optional mode via labels for routing (e.g., supervisor)
	if m := strings.TrimSpace(strings.ToLower(req.Mode)); m != "" {
		// Validate allowed modes to avoid silent drift
		switch m {
		case "simple", "standard", "complex", "supervisor":
			if grpcReq.Metadata.Labels == nil {
				grpcReq.Metadata.Labels = map[string]string{}
			}
			grpcReq.Metadata.Labels["mode"] = m
		default:
			h.sendError(w, "Invalid mode (allowed: simple, standard, complex, supervisor)", http.StatusBadRequest)
			return
		}
	}

	// Propagate auth/tracing headers to gRPC metadata
	ctx = withGRPCMetadata(ctx, r)

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

// SubmitTaskAndGetStreamURL handles POST /api/v1/tasks/stream
// Submits a task and returns a stream URL for SSE consumption.
func (h *TaskHandler) SubmitTaskAndGetStreamURL(w http.ResponseWriter, r *http.Request) {
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

	// Build gRPC request (reuse same shape as SubmitTask)
	grpcReq := &orchpb.SubmitTaskRequest{
		Metadata: &commonpb.TaskMetadata{
			UserId:    userCtx.UserID.String(),
			TenantId:  userCtx.TenantID.String(),
			SessionId: req.SessionID,
			Labels:    map[string]string{},
		},
		Query: req.Query,
	}

	// Ensure context map exists so we can inject optional fields safely
	ctxMap := map[string]interface{}{}
	if len(req.Context) > 0 {
		for k, v := range req.Context {
			ctxMap[k] = v
		}
	}
	// Normalize alias: context.template_name -> context.template (if not already set)
	if _, ok := ctxMap["template"]; !ok {
		if v, ok2 := ctxMap["template_name"].(string); ok2 {
			if tv := strings.TrimSpace(v); tv != "" {
				ctxMap["template"] = tv
			}
		}
	}
	// Validate and inject model_tier from top-level (top-level wins)
	if mt := strings.TrimSpace(strings.ToLower(req.ModelTier)); mt != "" {
		switch mt {
		case "small", "medium", "large":
			ctxMap["model_tier"] = mt
			h.logger.Debug("Applied top-level model_tier override", zap.String("model_tier", mt))
		default:
			h.sendError(w, "Invalid model_tier (allowed: small, medium, large)", http.StatusBadRequest)
			return
		}
	}
	// Inject top-level model_override when provided
	if mo := strings.TrimSpace(req.ModelOverride); mo != "" {
		ctxMap["model_override"] = mo
		h.logger.Debug("Applied top-level model_override", zap.String("model_override", mo))
	}
	// Inject top-level provider_override when provided
	if po := strings.TrimSpace(strings.ToLower(req.ProviderOverride)); po != "" {
		// Validate provider exists
		validProviders := []string{"openai", "anthropic", "google", "groq", "xai", "deepseek", "qwen", "zai", "ollama", "mistral", "cohere"}
		isValid := false
		for _, valid := range validProviders {
			if po == valid {
				isValid = true
				break
			}
		}
		if !isValid {
			h.sendError(w, fmt.Sprintf("Invalid provider_override: %s (allowed: %s)", po, strings.Join(validProviders, ", ")), http.StatusBadRequest)
			return
		}
		ctxMap["provider_override"] = po
		h.logger.Debug("Applied top-level provider_override", zap.String("provider_override", po))
	}

	// Map research strategy controls into context (streaming endpoint parity)
	if rs := strings.TrimSpace(strings.ToLower(req.ResearchStrategy)); rs != "" {
		switch rs {
		case "quick", "standard", "deep", "academic":
			ctxMap["research_strategy"] = rs
		default:
			h.sendError(w, "Invalid research_strategy (allowed: quick, standard, deep, academic)", http.StatusBadRequest)
			return
		}
	}
	if req.MaxIterations != nil {
		if *req.MaxIterations <= 0 || *req.MaxIterations > 50 {
			h.sendError(w, "max_iterations out of range (1..50)", http.StatusBadRequest)
			return
		}
		ctxMap["max_iterations"] = *req.MaxIterations
	}
	if req.MaxConcurrentAgents != nil {
		if *req.MaxConcurrentAgents <= 0 || *req.MaxConcurrentAgents > 20 {
			h.sendError(w, "max_concurrent_agents out of range (1..20)", http.StatusBadRequest)
			return
		}
		ctxMap["max_concurrent_agents"] = *req.MaxConcurrentAgents
	}
	if req.EnableVerification != nil {
		ctxMap["enable_verification"] = *req.EnableVerification
	}

	// Apply research strategy presets (seed defaults only when absent)
	if rs, ok := ctxMap["research_strategy"].(string); ok && strings.TrimSpace(rs) != "" {
		applyStrategyPreset(ctxMap, rs)
	}

	// Conflict validation: disable_ai=true cannot be combined with model controls
	var disableAI bool
	if v, exists := ctxMap["disable_ai"]; exists {
		switch t := v.(type) {
		case bool:
			disableAI = t
		case string:
			s := strings.TrimSpace(strings.ToLower(t))
			disableAI = s == "true" || s == "1" || s == "yes" || s == "y"
		case float64:
			disableAI = t != 0
		case int:
			disableAI = t != 0
		}
	}
	if disableAI {
		if req.ModelTier != "" || req.ModelOverride != "" || req.ProviderOverride != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
		if vt, ok := ctxMap["model_tier"].(string); ok && strings.TrimSpace(vt) != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
		if vo, ok := ctxMap["model_override"].(string); ok && strings.TrimSpace(vo) != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
		if vp, ok := ctxMap["provider_override"].(string); ok && strings.TrimSpace(vp) != "" {
			h.sendError(w, "disable_ai=true conflicts with model_tier/model_override", http.StatusBadRequest)
			return
		}
	}
	// Add context if present
	if len(ctxMap) > 0 {
		st, err := structpb.NewStruct(ctxMap)
		if err != nil {
			h.logger.Warn("Failed to convert context to struct", zap.Error(err))
		} else {
			grpcReq.Context = st
		}
	}

	// Propagate optional mode via labels for routing (e.g., supervisor)
	if m := strings.TrimSpace(strings.ToLower(req.Mode)); m != "" {
		switch m {
		case "simple", "standard", "complex", "supervisor":
			if grpcReq.Metadata.Labels == nil {
				grpcReq.Metadata.Labels = map[string]string{}
			}
			grpcReq.Metadata.Labels["mode"] = m
		default:
			h.sendError(w, "Invalid mode (allowed: simple, standard, complex, supervisor)", http.StatusBadRequest)
			return
		}
	}

	// Propagate auth/tracing headers to gRPC metadata
	ctx = withGRPCMetadata(ctx, r)

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
	h.logger.Info("Task submitted with stream URL",
		zap.String("task_id", resp.TaskId),
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("session_id", req.SessionID),
	)

	// Prepare stream URL (clients will use EventSource on this URL)
	streamURL := fmt.Sprintf("/api/v1/stream/sse?workflow_id=%s", resp.WorkflowId)

	// Headers for discoverability
	w.Header().Set("X-Workflow-ID", resp.WorkflowId)
	w.Header().Set("X-Session-ID", req.SessionID)
	w.Header().Set("Link", fmt.Sprintf("<%s>; rel=stream", streamURL))
	w.Header().Set("Content-Type", "application/json")

	// Body with stream URL
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"workflow_id": resp.WorkflowId,
		"task_id":     resp.TaskId,
		"stream_url":  streamURL,
	})
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

	// Propagate auth/tracing headers to gRPC metadata
	ctx = withGRPCMetadata(ctx, r)

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

	// Prepare response with raw result
	statusResp := TaskStatusResponse{
		TaskID:     resp.TaskId,
		WorkflowID: resp.TaskId, // Same as task_id
		Status:     resp.Status.String(),
		Result:     resp.Result, // Always include raw result (plain text or JSON string)
		Error:      resp.ErrorMessage,
	}

	// Optionally parse as JSON for backward compatibility (response field)
	// If result is valid JSON, populate response field; otherwise leave it nil
	if resp.Result != "" {
		var responseData map[string]interface{}
		if err := json.Unmarshal([]byte(resp.Result), &responseData); err == nil {
			statusResp.Response = responseData
		}
		// If unmarshal fails, it's plain text - only result field will be populated
	}

	// Enrich with metadata from database (query, session_id, mode, model, provider, tokens, cost, metadata)
	var (
		q                sql.NullString
		sid              sql.NullString
		mode             sql.NullString
		modelUsed        sql.NullString
		provider         sql.NullString
		totalTokens      sql.NullInt32
		promptTokens     sql.NullInt32
		completionTokens sql.NullInt32
		totalCost        sql.NullFloat64
		metadataJSON     []byte
	)
	row := h.db.QueryRowxContext(ctx, `
		SELECT
			query,
			COALESCE(session_id,''),
			COALESCE(mode,''),
			COALESCE(model_used,''),
			COALESCE(provider,''),
			total_tokens,
			prompt_tokens,
			completion_tokens,
			total_cost_usd,
			metadata
		FROM task_executions
		WHERE workflow_id = $1
		LIMIT 1`, taskID)
	if err := row.Scan(&q, &sid, &mode, &modelUsed, &provider, &totalTokens, &promptTokens, &completionTokens, &totalCost, &metadataJSON); err != nil {
		h.logger.Warn("Failed to scan task metadata", zap.Error(err), zap.String("workflow_id", taskID))
	}
	statusResp.Query = q.String
	statusResp.SessionID = sid.String
	statusResp.Mode = mode.String

	// Populate model and provider if available
	if modelUsed.Valid && modelUsed.String != "" {
		statusResp.ModelUsed = modelUsed.String
	}
	if provider.Valid && provider.String != "" {
		statusResp.Provider = provider.String
	}

	// Populate usage metadata if available
	if totalTokens.Valid || totalCost.Valid {
		statusResp.Usage = map[string]interface{}{}
		if totalTokens.Valid && totalTokens.Int32 > 0 {
			statusResp.Usage["total_tokens"] = totalTokens.Int32
		}
		if promptTokens.Valid && promptTokens.Int32 > 0 {
			statusResp.Usage["input_tokens"] = promptTokens.Int32
		}
		if completionTokens.Valid && completionTokens.Int32 > 0 {
			statusResp.Usage["output_tokens"] = completionTokens.Int32
		}
		if totalCost.Valid && totalCost.Float64 > 0 {
			statusResp.Usage["estimated_cost"] = totalCost.Float64
		}
	}

	// Parse and populate metadata (citations, etc.) if available
	if len(metadataJSON) > 0 {
		var metadata map[string]interface{}
		if err := json.Unmarshal(metadataJSON, &metadata); err == nil {
			statusResp.Metadata = metadata

			// Extract context from metadata if available (stored as metadata.task_context)
			if taskContext, ok := metadata["task_context"].(map[string]interface{}); ok {
				statusResp.Context = taskContext
			}
		}
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

// ListTasks handles GET /api/v1/tasks
func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userCtx, ok := ctx.Value("user").(*auth.UserContext)
	if !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query params
	q := r.URL.Query()
	limit := parseIntDefault(q.Get("limit"), 20)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := parseIntDefault(q.Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	sessionID := q.Get("session_id")
	statusStr := q.Get("status")

	// Map status to proto
	var statusFilter orchpb.TaskStatus
	switch strings.ToUpper(statusStr) {
	case "QUEUED":
		statusFilter = orchpb.TaskStatus_TASK_STATUS_QUEUED
	case "RUNNING":
		statusFilter = orchpb.TaskStatus_TASK_STATUS_RUNNING
	case "COMPLETED":
		statusFilter = orchpb.TaskStatus_TASK_STATUS_COMPLETED
	case "FAILED":
		statusFilter = orchpb.TaskStatus_TASK_STATUS_FAILED
	case "CANCELLED", "CANCELED":
		statusFilter = orchpb.TaskStatus_TASK_STATUS_CANCELLED
	case "TIMEOUT":
		statusFilter = orchpb.TaskStatus_TASK_STATUS_TIMEOUT
	default:
		statusFilter = orchpb.TaskStatus_TASK_STATUS_UNSPECIFIED
	}

	req := &orchpb.ListTasksRequest{
		UserId:       userCtx.UserID.String(),
		SessionId:    sessionID,
		Limit:        int32(limit),
		Offset:       int32(offset),
		FilterStatus: statusFilter,
	}

	// Propagate auth/tracing headers to gRPC metadata
	ctx = withGRPCMetadata(ctx, r)

	resp, err := h.orchClient.ListTasks(ctx, req)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			h.sendError(w, st.Message(), http.StatusInternalServerError)
		} else {
			h.sendError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Map to HTTP response shape
	out := ListTasksResponse{Tasks: make([]TaskSummary, 0, len(resp.Tasks)), TotalCount: resp.TotalCount}
	for _, t := range resp.Tasks {
		var createdAt, completedAt *time.Time
		if t.CreatedAt != nil {
			ct := t.CreatedAt.AsTime()
			createdAt = &ct
		}
		if t.CompletedAt != nil {
			cp := t.CompletedAt.AsTime()
			completedAt = &cp
		}
		var usage map[string]interface{}
		if t.TotalTokenUsage != nil {
			usage = map[string]interface{}{
				"total_tokens":      t.TotalTokenUsage.TotalTokens,
				"cost_usd":          t.TotalTokenUsage.CostUsd,
				"prompt_tokens":     t.TotalTokenUsage.PromptTokens,
				"completion_tokens": t.TotalTokenUsage.CompletionTokens,
			}
		}
		out.Tasks = append(out.Tasks, TaskSummary{
			TaskID:          t.TaskId,
			Query:           t.Query,
			Status:          t.Status.String(),
			Mode:            t.Mode.String(),
			CreatedAt:       createdAt,
			CompletedAt:     completedAt,
			TotalTokenUsage: usage,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(out)
}

// GetTaskEvents handles GET /api/v1/tasks/{id}/events
func (h *TaskHandler) GetTaskEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if _, ok := ctx.Value("user").(*auth.UserContext); !ok {
		h.sendError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	taskID := r.PathValue("id")
	if taskID == "" {
		h.sendError(w, "Task ID is required", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	limit := parseIntDefault(q.Get("limit"), 50)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := parseIntDefault(q.Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}

	rows, err := h.db.QueryxContext(ctx, `
        SELECT workflow_id, type, COALESCE(agent_id,''), COALESCE(message,''), timestamp, COALESCE(seq,0), COALESCE(stream_id,'')
        FROM event_logs
        WHERE workflow_id = $1
        ORDER BY timestamp ASC
        LIMIT $2 OFFSET $3
    `, taskID, limit, offset)
	if err != nil {
		h.sendError(w, fmt.Sprintf("Failed to load events: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Event struct {
		WorkflowID string    `json:"workflow_id"`
		Type       string    `json:"type"`
		AgentID    string    `json:"agent_id,omitempty"`
		Message    string    `json:"message,omitempty"`
		Timestamp  time.Time `json:"timestamp"`
		Seq        uint64    `json:"seq"`
		StreamID   string    `json:"stream_id,omitempty"`
	}
	events := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.WorkflowID, &e.Type, &e.AgentID, &e.Message, &e.Timestamp, &e.Seq, &e.StreamID); err != nil {
			h.sendError(w, fmt.Sprintf("Failed to scan event: %v", err), http.StatusInternalServerError)
			return
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		h.sendError(w, fmt.Sprintf("Failed to read events: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"events": events, "count": len(events)})
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
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

// CancelTask handles POST /api/v1/tasks/{id}/cancel
func (h *TaskHandler) CancelTask(w http.ResponseWriter, r *http.Request) {
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

	// Parse optional request body for reason
	type cancelRequest struct {
		Reason string `json:"reason,omitempty"`
	}
	var req cancelRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// Propagate auth/tracing headers to gRPC metadata
	ctx = withGRPCMetadata(ctx, r)

	// Call CancelTask gRPC
	// Service layer enforces ownership and handles all auth/authorization
	cancelReq := &orchpb.CancelTaskRequest{
		TaskId: taskID,
		Reason: req.Reason,
	}
	cancelResp, err := h.orchClient.CancelTask(ctx, cancelReq)
	if err != nil {
		if st, ok := status.FromError(err); ok {
			switch st.Code() {
			case codes.Unauthenticated:
				h.sendError(w, "Unauthorized", http.StatusUnauthorized)
			case codes.PermissionDenied:
				h.sendError(w, "Forbidden", http.StatusForbidden)
			case codes.NotFound:
				h.sendError(w, "Task not found", http.StatusNotFound)
			default:
				h.sendError(w, fmt.Sprintf("Failed to cancel task: %v", st.Message()), http.StatusInternalServerError)
			}
		} else {
			h.sendError(w, fmt.Sprintf("Failed to cancel task: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Log cancellation
	h.logger.Info("Task cancelled",
		zap.String("task_id", taskID),
		zap.String("user_id", userCtx.UserID.String()),
		zap.String("reason", req.Reason),
	)

	// Return 202 Accepted
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": cancelResp.Success,
		"message": cancelResp.Message,
	})
}

// sendError sends an error response
func (h *TaskHandler) sendError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
