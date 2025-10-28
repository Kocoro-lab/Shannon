package server

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strconv"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	auth "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	cfg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/degradation"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	common "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	pb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
)

// OrchestratorService implements the Orchestrator gRPC service
type OrchestratorService struct {
	pb.UnimplementedOrchestratorServiceServer
	temporalClient  client.Client
	sessionManager  *session.Manager
	humanActivities *activities.HumanInterventionActivities
	dbClient        *db.Client
	logger          *zap.Logger
	degradeMgr      *degradation.Manager
	workflowConfig  *activities.WorkflowConfig

	// Optional typed configuration snapshot for defaults
	shCfg *cfg.ShannonConfig

	// Provider for per-request default workflow flags
	getWorkflowDefaults func() (bypassSingle bool)
}

// SessionManager returns the session manager for use by other services
func (s *OrchestratorService) SessionManager() *session.Manager {
	return s.sessionManager
}

// Shutdown gracefully stops all background services
func (s *OrchestratorService) Shutdown() error {
	if s.degradeMgr != nil {
		if err := s.degradeMgr.Stop(); err != nil {
			s.logger.Error("Failed to stop degradation manager", zap.Error(err))
		} else {
			s.logger.Info("Degradation manager stopped")
		}
	}
	return nil
}

// SetShannonConfig provides a snapshot of typed configuration (optional).
func (s *OrchestratorService) SetShannonConfig(c *cfg.ShannonConfig) {
	s.shCfg = c
}

// SetTemporalClient sets or replaces the Temporal client after service construction.
func (s *OrchestratorService) SetTemporalClient(c client.Client) {
	s.temporalClient = c
}

// SetWorkflowDefaultsProvider sets a provider for BypassSingleResult default
func (s *OrchestratorService) SetWorkflowDefaultsProvider(f func() bool) {
	s.getWorkflowDefaults = f
}

// ListTemplates returns summaries of loaded templates from the registry
func (s *OrchestratorService) ListTemplates(ctx context.Context, _ *pb.ListTemplatesRequest) (*pb.ListTemplatesResponse, error) {
	reg := workflows.TemplateRegistry()
	summaries := reg.List()
	out := make([]*pb.TemplateSummary, 0, len(summaries))
	for _, ts := range summaries {
		out = append(out, &pb.TemplateSummary{
			Name:        ts.Name,
			Version:     ts.Version,
			Key:         ts.Key,
			ContentHash: ts.ContentHash,
		})
	}
	return &pb.ListTemplatesResponse{Templates: out}, nil
}

// NewOrchestratorService creates a new orchestrator service
// Pass nil for sessionCfg to use default configuration
func NewOrchestratorService(temporalClient client.Client, dbClient *db.Client, logger *zap.Logger, sessionCfg *session.ManagerConfig) (*OrchestratorService, error) {
	// Initialize session manager with retry (handles startup races)
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	var sessionMgr *session.Manager
	var err error
	for attempt := 1; attempt <= 15; attempt++ {
		sessionMgr, err = session.NewManagerWithConfig(redisAddr, logger, sessionCfg)
		if err == nil {
			break
		}
		// Exponential-ish backoff capped at 5s
		delay := time.Duration(attempt)
		if delay > 5 {
			delay = 5
		}
		logger.Warn("Redis not ready for session manager, retrying",
			zap.Int("attempt", attempt),
			zap.String("redis_addr", redisAddr),
			zap.Duration("sleep", delay*time.Second),
			zap.Error(err),
		)
		time.Sleep(delay * time.Second)
	}
	if err != nil && sessionMgr == nil {
		return nil, fmt.Errorf("failed to initialize session manager after retries: %w", err)
	}

	// Create degradation manager (wire redis/db wrappers)
	var redisWrapper interface{ IsCircuitBreakerOpen() bool }
	if sessionMgr != nil {
		redisWrapper = sessionMgr.RedisWrapper()
	}
	var dbWrapper interface{ IsCircuitBreakerOpen() bool }
	if dbClient != nil {
		dbWrapper = dbClient.Wrapper()
	}

	// Load workflow configuration
	ctx := context.Background()
	workflowCfg, err := activities.GetWorkflowConfig(ctx)
	if err != nil {
		logger.Warn("Failed to load workflow config, using defaults", zap.Error(err))
		// Use default config with standard thresholds
		workflowCfg = &activities.WorkflowConfig{
			ComplexitySimpleThreshold: 0.3,
			ComplexityMediumThreshold: 0.5,
		}
	}

	service := &OrchestratorService{
		temporalClient:  temporalClient,
		sessionManager:  sessionMgr,
		humanActivities: activities.NewHumanInterventionActivities(),
		dbClient:        dbClient,
		logger:          logger,
		degradeMgr:      degradation.NewManager(redisWrapper, dbWrapper, logger),
		workflowConfig:  workflowCfg,
	}

	// Start degradation manager background monitoring
	if service.degradeMgr != nil {
		ctx := context.Background() // Background context for service lifecycle
		if err := service.degradeMgr.Start(ctx); err != nil {
			logger.Warn("Failed to start degradation manager", zap.Error(err))
		} else {
			logger.Info("Degradation manager started successfully")
		}
	}

	return service, nil
}

// SubmitTask submits a new task for orchestration
func (s *OrchestratorService) SubmitTask(ctx context.Context, req *pb.SubmitTaskRequest) (*pb.SubmitTaskResponse, error) {
	if s.temporalClient == nil {
		return nil, status.Error(codes.Unavailable, "Temporal not ready")
	}
	// gRPC metrics timing
	grpcStart := time.Now()
	defer func() {
		ometrics.RecordGRPCMetrics("orchestrator", "SubmitTask", "OK", time.Since(grpcStart).Seconds())
	}()
	s.logger.Info("Received SubmitTask request",
		zap.String("query", req.Query),
		zap.String("user_id", req.Metadata.GetUserId()),
		zap.String("session_id", req.Metadata.GetSessionId()),
	)

	// Prefer authenticated context for identity and tenancy
	var tenantID string
	var userID string
	if userCtx, err := auth.GetUserContext(ctx); err == nil {
		// Check if this is the default dev user (indicates skipAuth mode)
		if userCtx.UserID.String() == "00000000-0000-0000-0000-000000000002" && req.Metadata.GetUserId() != "" {
			// In dev/demo mode with skipAuth, prefer the userId from request metadata
			// This allows testing with different user identities
			userID = req.Metadata.GetUserId()
		} else {
			userID = userCtx.UserID.String()
		}
		tenantID = userCtx.TenantID.String()
	} else {
		// Fallback to request metadata for backward compatibility
		userID = req.Metadata.GetUserId()
	}
	sessionID := req.Metadata.GetSessionId()

	// Get or create session
	var sess *session.Session
	var err error

	if sessionID != "" {
		// Try to retrieve existing session
		sess, err = s.sessionManager.GetSession(ctx, sessionID)
		if err != nil && err != session.ErrSessionNotFound {
			s.logger.Warn("Failed to retrieve session", zap.Error(err))
		}

		// SECURITY: Validate session ownership
		if sess != nil && sess.UserID != userID {
			s.logger.Warn("User attempted to access another user's session",
				zap.String("requesting_user", userID),
				zap.String("session_owner", sess.UserID),
				zap.String("session_id", sessionID),
			)
			// Treat as if session doesn't exist - force new session creation
			sess = nil
			// Note: We don't return an error to avoid leaking session existence
		}
	}

	// Create new session if needed
	if sess == nil {
		var createErr error
		// If a specific session ID was requested, use it; otherwise generate new
		if sessionID != "" {
			// Create session with the requested ID
			sess, createErr = s.sessionManager.CreateSessionWithID(ctx, sessionID, userID, tenantID, map[string]interface{}{
				"created_from": "orchestrator",
			})
		} else {
			// Generate new session ID
			sess, createErr = s.sessionManager.CreateSession(ctx, userID, tenantID, map[string]interface{}{
				"created_from": "orchestrator",
			})
			sessionID = sess.ID
		}
		if createErr != nil {
			return nil, status.Error(codes.Internal, "failed to create session")
		}
		s.logger.Info("Created new session", zap.String("session_id", sessionID))
	}
	// Ensure session exists in PostgreSQL for FK integrity (idempotent)
	if s.dbClient != nil && sessionID != "" {
		// Prefer explicit userID from request; fall back to session's user
		dbUserID := userID
		if dbUserID == "" && sess != nil && sess.UserID != "" {
			dbUserID = sess.UserID
		}
		s.logger.Debug("Ensuring session exists in PostgreSQL",
			zap.String("session_id", sessionID),
			zap.String("user_id", dbUserID))
		if err := s.dbClient.CreateSession(ctx, sessionID, dbUserID, tenantID); err != nil {
			s.logger.Warn("Failed to ensure session in database",
				zap.String("session_id", sessionID),
				zap.Error(err))
			// Continue anyway - Redis session is available
		}
	} else if s.dbClient == nil {
		s.logger.Debug("dbClient is nil; skipping session persistence")
	}

	// Add current query to history
	s.sessionManager.AddMessage(ctx, sessionID, session.Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Role:      "user",
		Content:   req.Query,
		Timestamp: time.Now(),
	})

	// Create workflow ID
	workflowID := fmt.Sprintf("task-%s-%d", userID, time.Now().Unix())

	// Build session context for workflow
	// Determine desired window size with priority:
	// 1) Request override (context.history_window_size)
	// 2) Preset (context.use_case_preset == "debugging")
	// 3) Env var HISTORY_WINDOW_MESSAGES
	// 4) Default (50)

	clamp := func(n, lo, hi int) int {
		if n < lo {
			return lo
		}
		if n > hi {
			return hi
		}
		return n
	}

	parseBoolish := func(v interface{}) bool {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			trimmed := strings.TrimSpace(val)
			if trimmed == "" {
				return false
			}
			if b, err := strconv.ParseBool(trimmed); err == nil {
				return b
			}
			lower := strings.ToLower(trimmed)
			return lower == "1" || lower == "yes" || lower == "y"
		case float64:
			return val != 0
		case int:
			return val != 0
		default:
			return false
		}
	}

	ctxMap := map[string]interface{}{}
	if req.Context != nil {
		ctxMap = req.Context.AsMap()
	}

	templateName := ""
	templateVersion := ""
	disableAI := false

	if req.Metadata != nil {
		if labels := req.Metadata.GetLabels(); labels != nil {
			if v, ok := labels["template"]; ok {
				templateName = strings.TrimSpace(v)
			}
			if v, ok := labels["template_version"]; ok {
				templateVersion = strings.TrimSpace(v)
			}
			if v, ok := labels["disable_ai"]; ok {
				disableAI = parseBoolish(v)
			}
		}
	}

	if templateName == "" {
		if v, ok := ctxMap["template"].(string); ok {
			templateName = strings.TrimSpace(v)
		}
	}
	if templateVersion == "" {
		if v, ok := ctxMap["template_version"].(string); ok {
			templateVersion = strings.TrimSpace(v)
		}
	}
	if !disableAI {
		if v, ok := ctxMap["disable_ai"]; ok {
			disableAI = parseBoolish(v)
		}
	}

	if templateName != "" {
		ctxMap["template"] = templateName
	}
	if templateVersion != "" {
		ctxMap["template_version"] = templateVersion
	}
	if disableAI {
		ctxMap["disable_ai"] = disableAI
	}

	desiredWindow := 0
	if v, ok := ctxMap["history_window_size"]; ok {
		switch t := v.(type) {
		case float64:
			desiredWindow = int(t)
		case int:
			desiredWindow = t
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
				desiredWindow = n
			}
		}
	} else if preset, ok := ctxMap["use_case_preset"].(string); ok && strings.EqualFold(preset, "debugging") {
		// Debugging preset uses a larger default
		if v := os.Getenv("HISTORY_WINDOW_DEBUG_MESSAGES"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				desiredWindow = n
			}
		}
		if desiredWindow == 0 {
			if s.shCfg != nil && s.shCfg.Session.ContextWindowDebugging > 0 {
				desiredWindow = s.shCfg.Session.ContextWindowDebugging
			} else {
				desiredWindow = 75
			}
		}
	}
	if desiredWindow == 0 {
		if v := os.Getenv("HISTORY_WINDOW_MESSAGES"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				desiredWindow = n
			}
		}
	}
	if desiredWindow == 0 {
		if s.shCfg != nil && s.shCfg.Session.ContextWindowDefault > 0 {
			desiredWindow = s.shCfg.Session.ContextWindowDefault
		} else {
			desiredWindow = 50
		}
	}
	historySize := clamp(desiredWindow, 5, 200)

	history := sess.GetRecentHistory(historySize)

	if _, ok := ctxMap["primers_count"]; !ok {
		if s.shCfg != nil && s.shCfg.Session.PrimersCount >= 0 {
			ctxMap["primers_count"] = s.shCfg.Session.PrimersCount
		}
	}
	if _, ok := ctxMap["recents_count"]; !ok {
		if s.shCfg != nil && s.shCfg.Session.RecentsCount >= 0 {
			ctxMap["recents_count"] = s.shCfg.Session.RecentsCount
		}
	}
	if _, ok := ctxMap["compression_trigger_ratio"]; !ok {
		if v := os.Getenv("COMPRESSION_TRIGGER_RATIO"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				ctxMap["compression_trigger_ratio"] = f
			}
		}
	}
	if _, ok := ctxMap["compression_target_ratio"]; !ok {
		if v := os.Getenv("COMPRESSION_TARGET_RATIO"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				ctxMap["compression_target_ratio"] = f
			}
		}
	}

	st, _ := structpb.NewStruct(ctxMap)
	req.Context = st

	// Emit a compact context-prep event (metadata only)
	estTokens := activities.EstimateTokensFromHistory(history)
	msg := activities.MsgContextPreparing(len(history), estTokens)
	streaming.Get().Publish(workflowID, streaming.Event{
		WorkflowID: workflowID,
		Type:       string(activities.StreamEventDataProcessing),
		AgentID:    "",
		Message:    msg,
		Timestamp:  time.Now(),
	})

	// Prepare workflow input with session context
	input := workflows.TaskInput{
		Query:           req.Query,
		UserID:          userID,
		TenantID:        tenantID,
		SessionID:       sessionID,
		Context:         ctxMap,
		Mode:            "",
		TemplateName:    templateName,
		TemplateVersion: templateVersion,
		DisableAI:       disableAI,
		History:         convertHistoryForWorkflow(history),
		SessionCtx:      sess.Context,
		RequireApproval: req.RequireApproval,
		ApprovalTimeout: 1800, // Default 30 minutes
	}

	// Apply deterministic workflow behavior flags from provider/env
	// Defaults: skip evaluation, bypass single result
	input.BypassSingleResult = true
	if s.getWorkflowDefaults != nil {
		input.BypassSingleResult = s.getWorkflowDefaults()
	}

	// Always route through OrchestratorWorkflow which will analyze complexity
	// and handle simple queries efficiently
	var mode common.ExecutionMode
	if req.ManualDecomposition != nil {
		mode = req.ManualDecomposition.Mode
	} else {
		// Let OrchestratorWorkflow determine complexity and route appropriately
		mode = common.ExecutionMode_EXECUTION_MODE_STANDARD
		s.logger.Info("Routing to OrchestratorWorkflow for complexity analysis",
			zap.String("query", req.Query),
		)
	}

	// Start appropriate workflow based on mode
	var workflowExecution client.WorkflowRun
	workflowType := "OrchestratorWorkflow"
	modeStr := "standard"

	// Store metadata in workflow memo for retrieval later
	memo := map[string]interface{}{
		"user_id":    userID,
		"session_id": sessionID,
		"tenant_id":  tenantID,
		"query":      req.Query,
	}
	if templateName != "" {
		memo["template"] = templateName
		if templateVersion != "" {
			memo["template_version"] = templateVersion
		}
	}
	if disableAI {
		memo["disable_ai"] = disableAI
	}

	// Determine priority from metadata labels (optional)
	queue := "shannon-tasks"
	priority := "normal"   // Track priority for logging
	workflowOverride := "" // Optional workflow override via label

	// Check if priority queues are enabled
	priorityQueuesEnabled := strings.EqualFold(os.Getenv("PRIORITY_QUEUES"), "on") ||
		os.Getenv("PRIORITY_QUEUES") == "1" ||
		strings.EqualFold(os.Getenv("PRIORITY_QUEUES"), "true")

	if req.Metadata != nil {
		labels := req.Metadata.GetLabels()
		if labels != nil {
			if p, ok := labels["priority"]; ok {
				priority = p
				priorityLower := strings.ToLower(p)

				// Only route to priority queues if PRIORITY_QUEUES is enabled
				if priorityQueuesEnabled {
					switch priorityLower {
					case "critical":
						queue = "shannon-tasks-critical"
					case "high":
						queue = "shannon-tasks-high"
					case "normal":
						queue = "shannon-tasks" // Explicitly handle normal priority
					case "low":
						queue = "shannon-tasks-low"
					default:
						// Warn about invalid priority value and use default queue
						s.logger.Warn("Invalid priority value provided, using default queue",
							zap.String("priority", p),
							zap.String("valid_values", "critical, high, normal, low"),
							zap.String("workflow_id", workflowID))
						priority = "normal" // Reset to normal for invalid priorities
					}
				} else if priorityLower != "normal" {
					// Priority queues disabled, log override to default queue
					s.logger.Debug("Priority label ignored in single-queue mode",
						zap.String("priority", p),
						zap.String("workflow_id", workflowID),
						zap.String("queue", queue))
				}
			}
			// Optional workflow override: labels["workflow"] = "supervisor" | "dag"
			if wf, ok := labels["workflow"]; ok {
				workflowOverride = strings.ToLower(wf)
			} else if wf2, ok := labels["mode"]; ok {
				// Back-compat: accept mode=supervisor as override
				if strings.EqualFold(wf2, "supervisor") {
					workflowOverride = "supervisor"
				}
			}
		}
	}
	// Log queue selection for debugging
	if queue != "shannon-tasks" {
		s.logger.Info("Task routed to priority queue",
			zap.String("workflow_id", workflowID),
			zap.String("queue", queue),
			zap.String("priority", priority))
	}

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: queue,
		Memo:      memo,
	}

	// Route based on explicit workflow override; otherwise use AgentDAGWorkflow
	switch workflowOverride {
	case "supervisor":
		input.Mode = "supervisor"
		modeStr = "supervisor"
		memo["mode"] = "supervisor"
		workflowType = "SupervisorWorkflow"
		s.logger.Info("Starting SupervisorWorkflow", zap.String("workflow_id", workflowID))
		workflowExecution, err = s.temporalClient.ExecuteWorkflow(
			ctx,
			workflowOptions,
			workflows.SupervisorWorkflow,
			input,
		)
	case "", "dag":
		// Default: route through OrchestratorWorkflow
		if mode == common.ExecutionMode_EXECUTION_MODE_COMPLEX {
			input.Mode = "complex"
			modeStr = "complex"
			memo["mode"] = "complex"
		} else {
			input.Mode = "standard"
			modeStr = "standard"
			memo["mode"] = "standard"
		}
		s.logger.Info("Starting OrchestratorWorkflow (router)",
			zap.String("workflow_id", workflowID),
			zap.String("initial_mode", modeStr))
		workflowExecution, err = s.temporalClient.ExecuteWorkflow(
			ctx,
			workflowOptions,
			workflows.OrchestratorWorkflow,
			input,
		)
	default:
		// Unknown override: fall back to DAG
		s.logger.Warn("Unknown workflow override; falling back to router", zap.String("override", workflowOverride))
		input.Mode = "standard"
		modeStr = "standard"
		memo["mode"] = "standard"
		workflowType = "OrchestratorWorkflow"
		workflowExecution, err = s.temporalClient.ExecuteWorkflow(
			ctx,
			workflowOptions,
			workflows.OrchestratorWorkflow,
			input,
		)
	}

	if err != nil {
		s.logger.Error("Failed to start workflow", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to start workflow")
	}

	// Write-on-submit: persist initial RUNNING record to task_executions table (idempotent by workflow_id)
	// Using synchronous save to ensure task exists before any token usage recording
	if s.dbClient != nil {
		var uidPtr *uuid.UUID
		if userID != "" {
			if u, err := uuid.Parse(userID); err == nil {
				uidPtr = &u
			}
		}
		started := time.Now()

		// Generate task ID to ensure it exists for foreign key references
		taskID := uuid.New()
		initial := &db.TaskExecution{
			ID:         taskID,
			WorkflowID: workflowExecution.GetID(),
			UserID:     uidPtr,
			SessionID:  sessionID,
			Query:      req.Query,
			Mode:       modeStr,
			Status:     "RUNNING",
			StartedAt:  started,
			CreatedAt:  started,
		}

		// Synchronous save to task_executions to ensure it exists before workflow activities execute
		// This prevents foreign key violations when token_usage tries to reference the task
		if err := s.dbClient.SaveTaskExecution(ctx, initial); err != nil {
			// Log the error but don't fail the workflow - task will be saved again on completion
			s.logger.Warn("Initial task persist failed, will retry on completion",
				zap.String("workflow_id", workflowExecution.GetID()),
				zap.String("task_id", taskID.String()),
				zap.Error(err))
		} else {
			s.logger.Debug("Initial task persisted successfully",
				zap.String("workflow_id", workflowExecution.GetID()),
				zap.String("task_id", taskID.String()))
		}

		// Start async finalizer to persist terminal state regardless of status polling
		go s.watchAndPersist(workflowExecution.GetID(), workflowExecution.GetRunID())
	}

	// Create response with session info
	response := &pb.SubmitTaskResponse{
		WorkflowId: workflowID,
		TaskId:     workflowExecution.GetID(),
		Status:     common.StatusCode_STATUS_CODE_OK,
		Message:    fmt.Sprintf("Task submitted successfully. Session: %s", sessionID),
		Decomposition: &pb.TaskDecomposition{
			Mode:            mode,
			ComplexityScore: 0.5, // Default/estimated score - actual will be determined during workflow execution
		},
		// Session ID is tracked internally, not returned in response for now
	}

	s.logger.Info("Task submitted successfully",
		zap.String("workflow_id", workflowID),
		zap.String("run_id", workflowExecution.GetRunID()),
		zap.String("session_id", sessionID),
	)

	// Increment workflows started metric
	ometrics.WorkflowsStarted.WithLabelValues(workflowType, modeStr).Inc()

	return response, nil
}

// GetTaskStatus gets the status of a submitted task
func (s *OrchestratorService) GetTaskStatus(ctx context.Context, req *pb.GetTaskStatusRequest) (*pb.GetTaskStatusResponse, error) {
	grpcStart := time.Now()
	defer func() {
		ometrics.RecordGRPCMetrics("orchestrator", "GetTaskStatus", "OK", time.Since(grpcStart).Seconds())
	}()
	s.logger.Info("Received GetTaskStatus request", zap.String("task_id", req.TaskId))

	// Describe workflow for non-blocking status
	desc, err := s.temporalClient.DescribeWorkflowExecution(ctx, req.TaskId, "")
	if err != nil || desc == nil || desc.WorkflowExecutionInfo == nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("task not found: %v", err))
	}

	// Enforce tenant ownership using memo if available
	if desc.WorkflowExecutionInfo.Memo != nil {
		dataConverter := converter.GetDefaultDataConverter()
		if tenantField, ok := desc.WorkflowExecutionInfo.Memo.Fields["tenant_id"]; ok && tenantField != nil {
			var memoTenant string
			_ = dataConverter.FromPayload(tenantField, &memoTenant)
			if memoTenant != "" {
				if uc, err := auth.GetUserContext(ctx); err == nil && uc != nil {
					if uc.TenantID.String() != memoTenant {
						// Don't leak existence
						return nil, status.Error(codes.NotFound, "task not found")
					}
				}
			}
		}
	}

	// Extract workflow metadata
	workflowStartTime := desc.WorkflowExecutionInfo.StartTime
	workflowID := req.TaskId

	// Map Temporal status to API status
	var statusOut pb.TaskStatus
	var statusStr string
	switch desc.WorkflowExecutionInfo.Status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		statusOut = pb.TaskStatus_TASK_STATUS_COMPLETED
		statusStr = "COMPLETED"
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		statusOut = pb.TaskStatus_TASK_STATUS_RUNNING
		statusStr = "RUNNING"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		statusOut = pb.TaskStatus_TASK_STATUS_TIMEOUT
		statusStr = "TIMEOUT"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		statusOut = pb.TaskStatus_TASK_STATUS_FAILED
		statusStr = "FAILED"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		statusOut = pb.TaskStatus_TASK_STATUS_CANCELLED
		statusStr = "CANCELLED"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		statusOut = pb.TaskStatus_TASK_STATUS_FAILED
		statusStr = "FAILED"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		statusOut = pb.TaskStatus_TASK_STATUS_RUNNING
		statusStr = "RUNNING"
	default:
		statusOut = pb.TaskStatus_TASK_STATUS_RUNNING
		statusStr = "RUNNING"
	}

	// Best-effort to fetch result if completed
	var result workflows.TaskResult
	var resultErr error
	isTerminal := false

	if statusOut == pb.TaskStatus_TASK_STATUS_COMPLETED ||
		statusOut == pb.TaskStatus_TASK_STATUS_FAILED ||
		statusOut == pb.TaskStatus_TASK_STATUS_TIMEOUT ||
		statusOut == pb.TaskStatus_TASK_STATUS_CANCELLED {
		isTerminal = true

		if statusOut == pb.TaskStatus_TASK_STATUS_COMPLETED {
			we := s.temporalClient.GetWorkflow(ctx, req.TaskId, "")
			resultErr = we.Get(ctx, &result)
			if resultErr != nil {
				s.logger.Warn("Failed to get completed workflow result",
					zap.String("task_id", req.TaskId),
					zap.Error(resultErr))
				// Include error in response but don't fail the status request
				result.ErrorMessage = fmt.Sprintf("Result retrieval failed: %v", resultErr)
			}
		}
	}

	// Extract session ID and other data for persistence and unified response
	var sessionID string
	var userID *uuid.UUID
	var query string
	var mode string

	// Extract from workflow memo using data converter
	if desc.WorkflowExecutionInfo != nil && desc.WorkflowExecutionInfo.Memo != nil {
		dataConverter := converter.GetDefaultDataConverter()

		// Extract user_id from memo
		if userField, ok := desc.WorkflowExecutionInfo.Memo.Fields["user_id"]; ok && userField != nil {
			var userIDStr string
			if err := dataConverter.FromPayload(userField, &userIDStr); err == nil && userIDStr != "" {
				if uid, err := uuid.Parse(userIDStr); err == nil {
					userID = &uid
				}
			}
		}

		// Extract session_id from memo
		if sessionField, ok := desc.WorkflowExecutionInfo.Memo.Fields["session_id"]; ok && sessionField != nil {
			_ = dataConverter.FromPayload(sessionField, &sessionID)
		}

		// Extract query from memo
		if queryField, ok := desc.WorkflowExecutionInfo.Memo.Fields["query"]; ok && queryField != nil {
			_ = dataConverter.FromPayload(queryField, &query)
		}

		// Extract mode from memo
		if modeField, ok := desc.WorkflowExecutionInfo.Memo.Fields["mode"]; ok && modeField != nil {
			_ = dataConverter.FromPayload(modeField, &mode)
		}
	}

	// Persist to database if terminal state
	if isTerminal && s.dbClient != nil {

		// Extract from result metadata if not in memo
		if result.Metadata != nil {
			if query == "" {
				if q, ok := result.Metadata["query"].(string); ok {
					query = q
				}
			}
			if mode == "" {
				if m, ok := result.Metadata["mode"].(string); ok {
					mode = m
				}
			}
		}

		taskExecution := &db.TaskExecution{
			WorkflowID:   workflowID,
			UserID:       userID,
			SessionID:    sessionID,
			Query:        query,
			Mode:         mode,
			Status:       statusStr,
			StartedAt:    workflowStartTime.AsTime(),
			TotalTokens:  result.TokensUsed,
			Result:       &result.Result,
			ErrorMessage: &result.ErrorMessage,
		}

		// Set completed time if terminal (prefer Temporal CloseTime)
		completedAt := getWorkflowEndTime(desc)
		taskExecution.CompletedAt = &completedAt

		// Calculate duration
		if !workflowStartTime.AsTime().IsZero() {
			end := completedAt
			durationMs := int(end.Sub(workflowStartTime.AsTime()).Milliseconds())
			taskExecution.DurationMs = &durationMs
		}

		// Extract metadata from result
		if result.Metadata != nil {
			if complexity, ok := result.Metadata["complexity_score"].(float64); ok {
				taskExecution.ComplexityScore = complexity
			}
			if agentsUsed, ok := result.Metadata["num_agents"].(int); ok {
				taskExecution.AgentsUsed = agentsUsed
			}
			taskExecution.Metadata = db.JSONB(result.Metadata)
		}

		// Queue async write to database
		err := s.dbClient.QueueWrite(db.WriteTypeTaskExecution, taskExecution, func(err error) {
			if err != nil {
				s.logger.Error("Failed to persist task execution",
					zap.String("workflow_id", workflowID),
					zap.Error(err))
			} else {
				s.logger.Debug("Task execution persisted",
					zap.String("workflow_id", workflowID),
					zap.String("status", statusStr))
			}
		})

		if err != nil {
			s.logger.Warn("Failed to queue task execution write",
				zap.String("workflow_id", workflowID),
				zap.Error(err))
		}
	}

	// Build metrics if we have a completed result or metadata
	var metrics *common.ExecutionMetrics
	if result.TokensUsed > 0 || result.Metadata != nil {
		metrics = &common.ExecutionMetrics{
			TokenUsage: &common.TokenUsage{
				TotalTokens: int32(result.TokensUsed),
				// Calculate cost based on model (default to GPT-3.5 pricing)
				CostUsd: calculateTokenCost(result.TokensUsed, result.Metadata),
			},
		}

		// Extract metadata values if available
		if result.Metadata != nil {
			// Get execution mode (using configurable thresholds)
			if complexity, ok := result.Metadata["complexity_score"].(float64); ok {
				simpleThreshold := 0.3 // default
				mediumThreshold := 0.5 // default
				if s.workflowConfig != nil {
					if s.workflowConfig.ComplexitySimpleThreshold > 0 {
						simpleThreshold = s.workflowConfig.ComplexitySimpleThreshold
					}
					if s.workflowConfig.ComplexityMediumThreshold > 0 {
						mediumThreshold = s.workflowConfig.ComplexityMediumThreshold
					}
				}

				if complexity < simpleThreshold {
					metrics.Mode = common.ExecutionMode_EXECUTION_MODE_SIMPLE
				} else if complexity < mediumThreshold {
					metrics.Mode = common.ExecutionMode_EXECUTION_MODE_STANDARD
				} else {
					metrics.Mode = common.ExecutionMode_EXECUTION_MODE_COMPLEX
				}
			} else {
				metrics.Mode = common.ExecutionMode_EXECUTION_MODE_STANDARD
			}

			// Get agent count
			if agentsUsed, ok := result.Metadata["num_agents"].(int); ok {
				metrics.AgentsUsed = int32(agentsUsed)
			}

			// Get cache metrics if available
			if cacheHit, ok := result.Metadata["cache_hit"].(bool); ok {
				metrics.CacheHit = cacheHit
			}
			if cacheScore, ok := result.Metadata["cache_score"].(float64); ok {
				metrics.CacheScore = cacheScore
			}
		}
	}

	// Compute duration for metrics and unified response
	durationSeconds := 0.0
	if isTerminal && workflowStartTime != nil {
		endTime := getWorkflowEndTime(desc)
		durationSeconds = endTime.Sub(workflowStartTime.AsTime()).Seconds()
	}

	// Record completed workflow metrics if terminal
	if isTerminal {
		// Derive mode string for labels
		modeStr := "standard"
		if metrics != nil {
			switch metrics.Mode {
			case common.ExecutionMode_EXECUTION_MODE_SIMPLE:
				modeStr = "simple"
			case common.ExecutionMode_EXECUTION_MODE_COMPLEX:
				modeStr = "complex"
			default:
				modeStr = "standard"
			}
		}
		// Cost
		cost := 0.0
		if metrics != nil && metrics.TokenUsage != nil {
			cost = metrics.TokenUsage.CostUsd
		}
		ometrics.RecordWorkflowMetrics("AgentDAGWorkflow", modeStr, statusStr, durationSeconds, result.TokensUsed, cost)
	}

	// Add unified response to metadata if we have a result
	if isTerminal && result.Result != "" {
		// Calculate execution time in ms
		executionTimeMs := int64(durationSeconds * 1000)

		// Transform to unified response format
		unifiedResp := TransformToUnifiedResponse(result, sessionID, executionTimeMs)

		// Store unified response in result metadata for clients that want it
		if result.Metadata == nil {
			result.Metadata = make(map[string]interface{})
		}
		result.Metadata["unified_response"] = unifiedResp
	}

	response := &pb.GetTaskStatusResponse{
		TaskId:   req.TaskId,
		Status:   statusOut,
		Progress: 0,
		Result:   result.Result,
		Metrics:  metrics,
	}
	return response, nil
}

// CancelTask cancels a running task
func (s *OrchestratorService) CancelTask(ctx context.Context, req *pb.CancelTaskRequest) (*pb.CancelTaskResponse, error) {
    s.logger.Info("Received CancelTask request",
        zap.String("task_id", req.TaskId),
        zap.String("reason", req.Reason),
    )

    // Enforce authentication
    uc, err := auth.GetUserContext(ctx)
    if err != nil || uc == nil {
        return nil, status.Error(codes.Unauthenticated, "authentication required")
    }

    // Verify ownership/tenancy via workflow memo (atomic with cancel on server side)
    desc, dErr := s.temporalClient.DescribeWorkflowExecution(ctx, req.TaskId, "")
    if dErr != nil || desc == nil || desc.WorkflowExecutionInfo == nil {
        return nil, status.Error(codes.NotFound, "task not found")
    }
    if desc.WorkflowExecutionInfo.Memo != nil {
        dc := converter.GetDefaultDataConverter()
        // Check tenant first (primary isolation key)
        if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["tenant_id"]; ok && f != nil {
            var memoTenant string
            _ = dc.FromPayload(f, &memoTenant)
            if memoTenant != "" && uc.TenantID.String() != memoTenant {
                // Do not leak existence
                return nil, status.Error(codes.NotFound, "task not found")
            }
        }
        // Optional: check user ownership when available
        if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["user_id"]; ok && f != nil {
            var memoUser string
            _ = dc.FromPayload(f, &memoUser)
            if memoUser != "" && uc.UserID.String() != memoUser {
                return nil, status.Error(codes.NotFound, "task not found")
            }
        }
    }

    // Perform cancellation
    if err := s.temporalClient.CancelWorkflow(ctx, req.TaskId, ""); err != nil {
        s.logger.Error("Failed to cancel workflow", zap.Error(err))
        return &pb.CancelTaskResponse{
            Success: false,
            Message: fmt.Sprintf("Failed to cancel task: %v", err),
        }, nil
    }

    return &pb.CancelTaskResponse{
        Success: true,
        Message: "Task cancelled successfully",
    }, nil
}

// ListTasks lists tasks for a user/session
func (s *OrchestratorService) ListTasks(ctx context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	if s.dbClient == nil {
		return &pb.ListTasksResponse{Tasks: []*pb.TaskSummary{}, TotalCount: 0}, nil
	}

	// Build filters
	where := []string{"1=1"}
	args := []interface{}{}
	ai := 1

	// Filter by user_id if provided
	if req.UserId != "" {
		if uid, err := uuid.Parse(req.UserId); err == nil {
			where = append(where, fmt.Sprintf("(user_id = $%d OR user_id IS NULL)", ai))
			args = append(args, uid)
			ai++
		}
	}
	// Filter by session_id if provided (task_executions.session_id is VARCHAR)
	if req.SessionId != "" {
		where = append(where, fmt.Sprintf("session_id = $%d", ai))
		args = append(args, req.SessionId)
		ai++
	}
	// Filter by status if provided
	if req.FilterStatus != pb.TaskStatus_TASK_STATUS_UNSPECIFIED {
		statusStr := mapProtoStatusToDB(req.FilterStatus)
		if statusStr != "" {
			where = append(where, fmt.Sprintf("UPPER(status) = UPPER($%d)", ai))
			args = append(args, statusStr)
			ai++
		}
	}

	// Pagination
	limit := int(req.Limit)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := int(req.Offset)
	if offset < 0 {
		offset = 0
	}

	// Total count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM task_executions WHERE %s", strings.Join(where, " AND "))
	var total int32
	if err := s.dbClient.Wrapper().QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		s.logger.Warn("ListTasks count failed", zap.Error(err))
		total = 0
	}

	// Data query
	dataQuery := fmt.Sprintf(`
        SELECT workflow_id, query, status, mode,
               started_at, completed_at, created_at,
               total_tokens,
               total_cost_usd
        FROM task_executions
        WHERE %s
        ORDER BY COALESCE(started_at, created_at) DESC
        LIMIT %d OFFSET %d`, strings.Join(where, " AND "), limit, offset)

	rows, err := s.dbClient.Wrapper().QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to list tasks: %v", err))
	}
	defer rows.Close()

	summaries := make([]*pb.TaskSummary, 0, limit)
	for rows.Next() {
		var (
			workflowID string
			queryText  sql.NullString
			statusStr  sql.NullString
			modeStr    sql.NullString
			started    sql.NullTime
			completed  sql.NullTime
			created    sql.NullTime
			tokens     sql.NullInt64
			costUSD    sql.NullFloat64
		)

		if err := rows.Scan(&workflowID, &queryText, &statusStr, &modeStr, &started, &completed, &created, &tokens, &costUSD); err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to scan row: %v", err))
		}

		summary := &pb.TaskSummary{
			TaskId: workflowID,
			Query:  queryText.String,
			Status: mapDBStatusToProto(statusStr.String),
			Mode:   mapDBModeToProto(modeStr.String),
		}
		if started.Valid {
			summary.CreatedAt = timestamppb.New(started.Time)
		} else if created.Valid {
			summary.CreatedAt = timestamppb.New(created.Time)
		}
		if completed.Valid {
			summary.CompletedAt = timestamppb.New(completed.Time)
		}
		if tokens.Valid || costUSD.Valid {
			tokenUsage := &common.TokenUsage{}
			if tokens.Valid {
				tokenUsage.TotalTokens = int32(tokens.Int64)
			}
			if costUSD.Valid {
				tokenUsage.CostUsd = costUSD.Float64
			}
			summary.TotalTokenUsage = tokenUsage
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to iterate rows: %v", err))
	}

	return &pb.ListTasksResponse{
		Tasks:      summaries,
		TotalCount: total,
	}, nil
}

// GetSessionContext gets session context
func (s *OrchestratorService) GetSessionContext(ctx context.Context, req *pb.GetSessionContextRequest) (*pb.GetSessionContextResponse, error) {
	s.logger.Info("GetSessionContext called", zap.String("session_id", req.SessionId))

	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	// Get session from manager
	sess, err := s.sessionManager.GetSession(ctx, req.SessionId)
	if err != nil {
		if err == session.ErrSessionNotFound {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get session: %v", err))
	}

	// Build response with session data
	response := &pb.GetSessionContextResponse{
		SessionId: req.SessionId,
	}

	// Add session token usage
	if sess.TotalTokensUsed > 0 {
		response.SessionTokenUsage = &common.TokenUsage{
			TotalTokens: int32(sess.TotalTokensUsed),
		}
	}

	// Add session context as Struct
	if sess.Context != nil {
		contextStruct, err := structpb.NewStruct(sess.Context)
		if err == nil {
			response.Context = contextStruct
		}
	}

	if s.dbClient != nil {
		tasks, err := s.loadRecentSessionTasks(ctx, req.SessionId, 5)
		if err != nil {
			s.logger.Warn("Failed to load recent session tasks",
				zap.String("session_id", req.SessionId),
				zap.Error(err))
		} else if len(tasks) > 0 {
			response.RecentTasks = tasks
		}
	}

	return response, nil
}

func (s *OrchestratorService) loadRecentSessionTasks(ctx context.Context, sessionID string, limit int) ([]*pb.TaskSummary, error) {
	if sessionID == "" || limit <= 0 || s.dbClient == nil {
		return nil, nil
	}

	query := `
        SELECT workflow_id, query, status, mode,
               started_at, completed_at, created_at,
               total_tokens, total_cost_usd
        FROM task_executions
        WHERE session_id = $1
        ORDER BY COALESCE(started_at, created_at) DESC
        LIMIT $2`

	rows, err := s.dbClient.Wrapper().QueryContext(ctx, query, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]*pb.TaskSummary, 0, limit)

	for rows.Next() {
		var (
			workflowID string
			queryText  sql.NullString
			statusStr  sql.NullString
			modeStr    sql.NullString
			started    sql.NullTime
			completed  sql.NullTime
			created    sql.NullTime
			tokens     sql.NullInt64
			costUSD    sql.NullFloat64
		)

		if err := rows.Scan(
			&workflowID,
			&queryText,
			&statusStr,
			&modeStr,
			&started,
			&completed,
			&created,
			&tokens,
			&costUSD,
		); err != nil {
			return nil, err
		}

		summary := &pb.TaskSummary{
			TaskId: workflowID,
			Query:  queryText.String,
			Status: mapDBStatusToProto(statusStr.String),
			Mode:   mapDBModeToProto(modeStr.String),
		}

		if started.Valid {
			summary.CreatedAt = timestamppb.New(started.Time)
		} else if created.Valid {
			summary.CreatedAt = timestamppb.New(created.Time)
		}

		if completed.Valid {
			summary.CompletedAt = timestamppb.New(completed.Time)
		}

		if tokens.Valid || costUSD.Valid {
			tokenUsage := &common.TokenUsage{}
			if tokens.Valid {
				tokenUsage.TotalTokens = int32(tokens.Int64)
			}
			if costUSD.Valid {
				tokenUsage.CostUsd = costUSD.Float64
			}
			summary.TotalTokenUsage = tokenUsage
		}

		summaries = append(summaries, summary)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return summaries, nil
}

func mapDBStatusToProto(status string) pb.TaskStatus {
	switch strings.ToUpper(status) {
	case "QUEUED", "PENDING":
		return pb.TaskStatus_TASK_STATUS_QUEUED
	case "RUNNING", "IN_PROGRESS":
		return pb.TaskStatus_TASK_STATUS_RUNNING
	case "COMPLETED", "SUCCEEDED":
		return pb.TaskStatus_TASK_STATUS_COMPLETED
	case "FAILED", "ERROR", "TERMINATED":
		return pb.TaskStatus_TASK_STATUS_FAILED
	case "CANCELLED", "CANCELED":
		return pb.TaskStatus_TASK_STATUS_CANCELLED
	case "TIMEOUT", "TIMED_OUT":
		return pb.TaskStatus_TASK_STATUS_TIMEOUT
	default:
		return pb.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

func mapDBModeToProto(mode string) common.ExecutionMode {
	switch strings.ToLower(mode) {
	case "simple":
		return common.ExecutionMode_EXECUTION_MODE_SIMPLE
	case "complex":
		return common.ExecutionMode_EXECUTION_MODE_COMPLEX
	case "standard":
		fallthrough
	case "":
		return common.ExecutionMode_EXECUTION_MODE_STANDARD
	default:
		return common.ExecutionMode_EXECUTION_MODE_STANDARD
	}
}

func mapProtoStatusToDB(st pb.TaskStatus) string {
	switch st {
	case pb.TaskStatus_TASK_STATUS_QUEUED:
		return "QUEUED"
	case pb.TaskStatus_TASK_STATUS_RUNNING:
		return "RUNNING"
	case pb.TaskStatus_TASK_STATUS_COMPLETED:
		return "COMPLETED"
	case pb.TaskStatus_TASK_STATUS_FAILED:
		return "FAILED"
	case pb.TaskStatus_TASK_STATUS_CANCELLED:
		return "CANCELLED"
	case pb.TaskStatus_TASK_STATUS_TIMEOUT:
		return "TIMEOUT"
	default:
		return ""
	}
}

// watchAndPersist waits for workflow completion and persists terminal state to DB.
func (s *OrchestratorService) watchAndPersist(workflowID, runID string) {
	if s.temporalClient == nil || s.dbClient == nil {
		return
	}
	ctx := context.Background()
	// Wait for workflow completion (ignore result content; we'll describe for status/timestamps)
	we := s.temporalClient.GetWorkflow(ctx, workflowID, runID)
	var tmp interface{}
	_ = we.Get(ctx, &tmp)

	// Describe to fetch status and times
	desc, err := s.temporalClient.DescribeWorkflowExecution(ctx, workflowID, runID)
	if err != nil || desc == nil || desc.WorkflowExecutionInfo == nil {
		s.logger.Warn("watchAndPersist: describe failed", zap.String("workflow_id", workflowID), zap.Error(err))
		return
	}

	st := desc.WorkflowExecutionInfo.GetStatus()
	statusStr := "RUNNING"
	switch st {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		statusStr = "COMPLETED"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		statusStr = "FAILED"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		statusStr = "TIMEOUT"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		statusStr = "CANCELLED"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		statusStr = "FAILED"
	default:
		statusStr = "RUNNING"
	}

	start := time.Now()
	if desc.WorkflowExecutionInfo.GetStartTime() != nil {
		start = desc.WorkflowExecutionInfo.GetStartTime().AsTime()
	}
	end := getWorkflowEndTime(desc)

	te := &db.TaskExecution{
		WorkflowID:  workflowID,
		Status:      statusStr,
		StartedAt:   start,
		CompletedAt: &end,
	}
	// Best-effort: copy memo fields (user_id/session_id/query/mode)
	if m := desc.WorkflowExecutionInfo.Memo; m != nil {
		dc := converter.GetDefaultDataConverter()
		if f, ok := m.Fields["user_id"]; ok && f != nil {
			var uidStr string
			if err := dc.FromPayload(f, &uidStr); err == nil {
				if u, err := uuid.Parse(uidStr); err == nil {
					te.UserID = &u
				}
			}
		}
		if f, ok := m.Fields["session_id"]; ok && f != nil {
			_ = dc.FromPayload(f, &te.SessionID)
		}
		if f, ok := m.Fields["query"]; ok && f != nil {
			_ = dc.FromPayload(f, &te.Query)
		}
		if f, ok := m.Fields["mode"]; ok && f != nil {
			_ = dc.FromPayload(f, &te.Mode)
		}
	}

	_ = s.dbClient.QueueWrite(db.WriteTypeTaskExecution, te, func(err error) {
		if err != nil {
			s.logger.Warn("watchAndPersist: final write failed", zap.String("workflow_id", workflowID), zap.Error(err))
		} else {
			s.logger.Debug("watchAndPersist: final write ok", zap.String("workflow_id", workflowID), zap.String("status", statusStr))
		}
	})
}

// getWorkflowEndTime returns the workflow end time, preferring Temporal CloseTime.
// Falls back to time.Now() if CloseTime is unavailable (e.g., race or visibility lag).
func getWorkflowEndTime(desc *workflowservice.DescribeWorkflowExecutionResponse) time.Time {
	if desc != nil && desc.WorkflowExecutionInfo != nil && desc.WorkflowExecutionInfo.CloseTime != nil {
		return desc.WorkflowExecutionInfo.CloseTime.AsTime()
	}
	return time.Now()
}

// RegisterOrchestratorServiceServer registers the service with the gRPC server
func RegisterOrchestratorServiceServer(s *grpc.Server, srv pb.OrchestratorServiceServer) {
	pb.RegisterOrchestratorServiceServer(s, srv)
}

// calculateTokenCost calculates the cost based on token count and model
func calculateTokenCost(tokens int, metadata map[string]interface{}) float64 {
	// Prefer centralized pricing config (model-specific) with sensible fallback.
	var model string
	if metadata != nil {
		if m, ok := metadata["model"].(string); ok && m != "" {
			model = m
		} else if m, ok := metadata["model_used"].(string); ok && m != "" {
			// Fallback to model_used if model is not present
			model = m
		}
	}
	return pricing.CostForTokens(model, tokens)
}

// Helper function to convert session history for workflow
func convertHistoryForWorkflow(messages []session.Message) []workflows.Message {
	result := make([]workflows.Message, len(messages))
	for i, msg := range messages {
		result[i] = workflows.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		}
	}
	return result
}

// ApproveTask handles human approval for a task
func (s *OrchestratorService) ApproveTask(ctx context.Context, req *pb.ApproveTaskRequest) (*pb.ApproveTaskResponse, error) {
    s.logger.Info("Received ApproveTask request",
        zap.String("approval_id", req.ApprovalId),
        zap.String("workflow_id", req.WorkflowId),
        zap.Bool("approved", req.Approved),
    )

    // Validate input
    if req.ApprovalId == "" || req.WorkflowId == "" {
        return &pb.ApproveTaskResponse{
            Success: false,
            Message: "approval_id and workflow_id are required",
        }, nil
    }

    // Enforce authentication and ownership
    uc, err := auth.GetUserContext(ctx)
    if err != nil || uc == nil {
        return nil, status.Error(codes.Unauthenticated, "authentication required")
    }
    desc, dErr := s.temporalClient.DescribeWorkflowExecution(ctx, req.WorkflowId, req.RunId)
    if dErr != nil || desc == nil || desc.WorkflowExecutionInfo == nil {
        return nil, status.Error(codes.NotFound, "workflow not found")
    }
    if desc.WorkflowExecutionInfo.Memo != nil {
        dc := converter.GetDefaultDataConverter()
        if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["tenant_id"]; ok && f != nil {
            var memoTenant string
            _ = dc.FromPayload(f, &memoTenant)
            if memoTenant != "" && uc.TenantID.String() != memoTenant {
                return nil, status.Error(codes.NotFound, "workflow not found")
            }
        }
        if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["user_id"]; ok && f != nil {
            var memoUser string
            _ = dc.FromPayload(f, &memoUser)
            if memoUser != "" && uc.UserID.String() != memoUser {
                return nil, status.Error(codes.NotFound, "workflow not found")
            }
        }
    }

	// Create the approval result
	approvalResult := activities.HumanApprovalResult{
		ApprovalID:     req.ApprovalId,
		Approved:       req.Approved,
		Feedback:       req.Feedback,
		ModifiedAction: req.ModifiedAction,
		ApprovedBy:     req.ApprovedBy,
		Timestamp:      time.Now(),
	}

	// Store the approval in our activities (for tracking/audit)
	if procErr := s.humanActivities.ProcessApprovalResponse(ctx, approvalResult); procErr != nil {
		s.logger.Error("Failed to process approval response", zap.Error(procErr))
	}

	// Send signal to the workflow
	signalName := fmt.Sprintf("human-approval-%s", req.ApprovalId)
	err = s.temporalClient.SignalWorkflow(
		ctx,
		req.WorkflowId,
		req.RunId, // Can be empty to signal the current run
		signalName,
		approvalResult,
	)

	if err != nil {
		s.logger.Error("Failed to signal workflow",
			zap.String("workflow_id", req.WorkflowId),
			zap.String("signal_name", signalName),
			zap.Error(err),
		)
		return &pb.ApproveTaskResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to signal workflow: %v", err),
		}, nil
	}

	s.logger.Info("Successfully signaled workflow with approval",
		zap.String("workflow_id", req.WorkflowId),
		zap.String("approval_id", req.ApprovalId),
		zap.Bool("approved", req.Approved),
	)

	return &pb.ApproveTaskResponse{
		Success: true,
		Message: fmt.Sprintf("Approval %s processed successfully", req.ApprovalId),
	}, nil
}

// GetPendingApprovals gets pending approvals for a user/session
func (s *OrchestratorService) GetPendingApprovals(ctx context.Context, req *pb.GetPendingApprovalsRequest) (*pb.GetPendingApprovalsResponse, error) {
	s.logger.Info("Received GetPendingApprovals request",
		zap.String("user_id", req.UserId),
		zap.String("session_id", req.SessionId),
	)

	// In a production system, this would query a database for pending approvals
	// For now, return an empty list as this is primarily for UI/monitoring
	// The actual approval state is maintained in the workflow and in-memory activities

	return &pb.GetPendingApprovalsResponse{
		Approvals: []*pb.PendingApproval{},
	}, nil
}
