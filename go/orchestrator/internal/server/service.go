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
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	auth "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/degradation"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	common "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	pb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
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

// SetTemporalClient sets or replaces the Temporal client after service construction.
func (s *OrchestratorService) SetTemporalClient(c client.Client) {
	s.temporalClient = c
}

// SetWorkflowDefaultsProvider sets a provider for BypassSingleResult default
func (s *OrchestratorService) SetWorkflowDefaultsProvider(f func() bool) {
	s.getWorkflowDefaults = f
}

// NewOrchestratorService creates a new orchestrator service
func NewOrchestratorService(temporalClient client.Client, dbClient *db.Client, logger *zap.Logger) (*OrchestratorService, error) {
	// Initialize session manager with retry (handles startup races)
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "redis:6379"
	}

	var sessionMgr *session.Manager
	var err error
	for attempt := 1; attempt <= 15; attempt++ {
		sessionMgr, err = session.NewManager(redisAddr, logger)
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

	service := &OrchestratorService{
		temporalClient:  temporalClient,
		sessionManager:  sessionMgr,
		humanActivities: activities.NewHumanInterventionActivities(),
		dbClient:        dbClient,
		logger:          logger,
		degradeMgr:      degradation.NewManager(redisWrapper, dbWrapper, logger),
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
		userID = userCtx.UserID.String()
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
		sess, createErr = s.sessionManager.CreateSession(ctx, userID, tenantID, map[string]interface{}{
			"created_from": "orchestrator",
		})
		if createErr != nil {
			return nil, status.Error(codes.Internal, "failed to create session")
		}
		sessionID = sess.ID
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
	history := sess.GetRecentHistory(10) // Last 10 messages

	// Prepare workflow input with session context
	input := workflows.TaskInput{
		Query:           req.Query,
		UserID:          userID,
		TenantID:        tenantID,
		SessionID:       sessionID,
		Context:         req.Context.AsMap(),
		Mode:            "",
		History:         convertHistoryForWorkflow(history),
		SessionCtx:      sess.Context,
		RequireApproval: false, // TODO: Add to proto request
		ApprovalTimeout: 1800,  // Default 30 minutes
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

	// Determine priority from metadata labels (optional)
	queue := "shannon-tasks"
	priority := "normal"   // Track priority for logging
	workflowOverride := "" // Optional workflow override via label
	if req.Metadata != nil {
		labels := req.Metadata.GetLabels()
		if labels != nil {
			if p, ok := labels["priority"]; ok {
				priority = p
				priorityLower := strings.ToLower(p)
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

	// Create response with session info
	response := &pb.SubmitTaskResponse{
		WorkflowId: workflowID,
		TaskId:     workflowExecution.GetID(),
		Status:     common.StatusCode_STATUS_CODE_OK,
		Message:    fmt.Sprintf("Task submitted successfully. Session: %s", sessionID),
		// Decomposition: nil - will be available via GetTaskStatus after workflow completes analysis
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

	// Persist to database if terminal state
	if isTerminal && s.dbClient != nil {
		// Extract user data from memo if available
		var userID *uuid.UUID
		var sessionID string
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
			// Get execution mode
			if complexity, ok := result.Metadata["complexity_score"].(float64); ok {
				if complexity < 0.3 {
					metrics.Mode = common.ExecutionMode_EXECUTION_MODE_SIMPLE
				} else if complexity < 0.7 {
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
		// Compute duration seconds (prefer Temporal CloseTime)
		durationSeconds := 0.0
		if workflowStartTime != nil {
			endTime := getWorkflowEndTime(desc)
			durationSeconds = endTime.Sub(workflowStartTime.AsTime()).Seconds()
		}
		// Cost
		cost := 0.0
		if metrics != nil && metrics.TokenUsage != nil {
			cost = metrics.TokenUsage.CostUsd
		}
		ometrics.RecordWorkflowMetrics("AgentDAGWorkflow", modeStr, statusStr, durationSeconds, result.TokensUsed, cost)
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

	err := s.temporalClient.CancelWorkflow(ctx, req.TaskId, "")
	if err != nil {
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
	// This would query Temporal's visibility API in production
	// For now, return empty list
	return &pb.ListTasksResponse{
		Tasks:      []*pb.TaskSummary{},
		TotalCount: 0,
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

	sessionUUID, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, nil
	}

	query := `
		SELECT workflow_id, query, status, mode,
		       started_at, completed_at, created_at,
		       NULLIF(metrics->>'total_tokens', '')::bigint AS total_tokens,
		       NULLIF(metrics->>'total_cost_usd', '')::double precision AS total_cost_usd
		FROM tasks
		WHERE session_id = $1
		ORDER BY COALESCE(started_at, created_at) DESC
		LIMIT $2`

	rows, err := s.dbClient.Wrapper().QueryContext(ctx, query, sessionUUID, limit)
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
	// Default to GPT-3.5 pricing: $0.002 per 1K tokens
	pricePerToken := 0.000002

	if metadata != nil {
		if model, ok := metadata["model"].(string); ok {
			// Adjust pricing based on model
			switch {
			case strings.Contains(model, "gpt-4"):
				pricePerToken = 0.00003 // $0.03 per 1K tokens
			case strings.Contains(model, "gpt-3.5"):
				pricePerToken = 0.000002 // $0.002 per 1K tokens
			case strings.Contains(model, "claude"):
				pricePerToken = 0.00001 // Rough estimate
			}
		}
	}

	return float64(tokens) * pricePerToken
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
	err := s.humanActivities.ProcessApprovalResponse(ctx, approvalResult)
	if err != nil {
		s.logger.Error("Failed to process approval response", zap.Error(err))
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
