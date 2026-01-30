package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
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

	"github.com/robfig/cron/v3"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	auth "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	cfg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/degradation"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
	common "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	pb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/schedules"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
)

// validateCronSchedule validates a cron expression.
// Supports standard 5-field format and Temporal-compatible descriptors (@every, @hourly, etc).
// Returns an error if the expression is invalid.
func validateCronSchedule(cronExpr string) error {
	cronExpr = strings.TrimSpace(cronExpr)
	if cronExpr == "" {
		return fmt.Errorf("empty cron expression")
	}

	// Support Temporal-compatible descriptors (e.g., @every 1h, @hourly, @daily)
	if strings.HasPrefix(cronExpr, "@") {
		// Use parser with descriptor support
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		_, err := parser.Parse(cronExpr)
		return err
	}

	// Standard 5-field cron (minute, hour, day-of-month, month, day-of-week)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(cronExpr)
	return err
}

// jsonEncodedPrefix marks values that were JSON-encoded during transport.
// This prefix prevents backwards-incompatible coercion of string values like "true" or "123".
const jsonEncodedPrefix = "\x00json:"

// decodeTaskContext converts proto map[string]string to map[string]interface{},
// decoding only values with the JSON prefix marker back to their original types.
// Plain string values (including "true", "123") are preserved as strings.
func decodeTaskContext(ctx map[string]string) map[string]interface{} {
	if ctx == nil {
		return nil
	}
	result := make(map[string]interface{}, len(ctx))
	for k, v := range ctx {
		if strings.HasPrefix(v, jsonEncodedPrefix) {
			// JSON-encoded value: decode it
			jsonStr := strings.TrimPrefix(v, jsonEncodedPrefix)
			var decoded interface{}
			if err := json.Unmarshal([]byte(jsonStr), &decoded); err == nil {
				result[k] = decoded
			} else {
				result[k] = v // fallback to original if decode fails
			}
		} else {
			// Plain string: preserve as-is
			result[k] = v
		}
	}
	return result
}

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
	scheduleManager *schedules.Manager

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

// SetScheduleManager sets the schedule manager after service construction.
func (s *OrchestratorService) SetScheduleManager(m *schedules.Manager) {
	s.scheduleManager = m
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
	dbSessionID := sessionID      // Use the requested ID for DB persistence/metrics
	runtimeSessionID := sessionID // Session ID used for Redis/session manager/history

	// Get or create session
	var sess *session.Session
	var err error

	if sessionID != "" {
		// Try to retrieve existing session
		sess, err = s.sessionManager.GetSession(ctx, sessionID)
		if err != nil && err != session.ErrSessionNotFound {
			s.logger.Warn("Failed to retrieve session", zap.Error(err))
		}

		// If session not found and we have a DB client, check for external_id alias
		// This handles the case where the client sends a UUID but the Redis session
		// was created with an external_id (or vice versa)
		if sess == nil && s.dbClient != nil {
			var externalID sql.NullString
			row := s.dbClient.Wrapper().QueryRowContext(ctx, `
				SELECT context->>'external_id'
				FROM sessions
				WHERE id::text = $1 AND deleted_at IS NULL`, sessionID)
			if err := row.Scan(&externalID); err == nil && externalID.Valid && externalID.String != "" {
				// Try to get session using the external_id
				aliasedSession, aliasErr := s.sessionManager.GetSession(ctx, externalID.String)
				if aliasErr == nil && aliasedSession != nil {
					sess = aliasedSession
					runtimeSessionID = aliasedSession.ID
					dbSessionID = runtimeSessionID
					s.logger.Debug("Resolved session via external_id alias",
						zap.String("requested_id", sessionID),
						zap.String("resolved_id", externalID.String))
				}
			}
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

	// Prepare session context for PostgreSQL persistence
	var sessionContext map[string]interface{}

	// Create new session if needed
	if sess == nil {
		var createErr error

		// Build initial session context with task context for first task
		sessionContext = map[string]interface{}{
			"created_from": "orchestrator",
		}

		// Preserve first task's context (including role) in session context
		if req.Context != nil {
			taskCtx := req.Context.AsMap()

			// Store entire task context as first_task_context for reference
			sessionContext["first_task_context"] = taskCtx

			// Explicitly preserve role field for quick access
			if role, ok := taskCtx["role"].(string); ok && role != "" {
				sessionContext["role"] = role
				sessionContext["first_task_mode"] = role // Use role as first_task_mode
			}

			// Preserve research mode flags
			if forceResearch, ok := taskCtx["force_research"].(bool); ok {
				sessionContext["force_research"] = forceResearch
			}
			if researchStrategy, ok := taskCtx["research_strategy"].(string); ok && researchStrategy != "" {
				sessionContext["research_strategy"] = researchStrategy
			}
		}

		// If a specific session ID was requested, use it; otherwise generate new
		if sessionID != "" {
			// Create session with the requested ID
			sess, createErr = s.sessionManager.CreateSessionWithID(ctx, sessionID, userID, tenantID, sessionContext)
		} else {
			// Generate new session ID
			sess, createErr = s.sessionManager.CreateSession(ctx, userID, tenantID, sessionContext)
			sessionID = sess.ID
		}
		runtimeSessionID = sess.ID
		dbSessionID = runtimeSessionID
		if createErr != nil {
			return nil, status.Error(codes.Internal, "failed to create session")
		}
		s.logger.Info("Created new session",
			zap.String("session_id", sessionID),
			zap.Any("context", sessionContext))
	} else {
		// For existing sessions, use metadata from Redis session for PostgreSQL upsert
		if sess.Metadata != nil {
			sessionContext = sess.Metadata
		}
	}
	// Ensure session exists in PostgreSQL for FK integrity (idempotent)
	if s.dbClient != nil && dbSessionID != "" {
		// Prefer explicit userID from request; fall back to session's user
		dbUserID := userID
		if dbUserID == "" && sess != nil && sess.UserID != "" {
			dbUserID = sess.UserID
		}

		// Use the sessionContext we built earlier (don't read from sess.Context which may be empty)
		dbSessionContext := sessionContext

		s.logger.Debug("Ensuring session exists in PostgreSQL",
			zap.String("session_id", dbSessionID),
			zap.String("user_id", dbUserID),
			zap.Any("context", dbSessionContext))
		if err := s.dbClient.CreateSession(ctx, dbSessionID, dbUserID, tenantID, dbSessionContext); err != nil {
			s.logger.Warn("Failed to ensure session in database",
				zap.String("session_id", dbSessionID),
				zap.Error(err))
			// Continue anyway - Redis session is available
		}
	} else if s.dbClient == nil {
		s.logger.Debug("dbClient is nil; skipping session persistence")
	}

	// Add current query to history
	if err := s.sessionManager.AddMessage(ctx, runtimeSessionID, session.Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		Role:      "user",
		Content:   req.Query,
		Timestamp: time.Now(),
	}); err != nil {
		s.logger.Warn("Failed to append message to session history",
			zap.String("session_id", runtimeSessionID),
			zap.Error(err))
	}

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
		AgentID:    "orchestrator",
		Message:    msg,
		Timestamp:  time.Now(),
	})

	// Prepare workflow input with session context
	input := workflows.TaskInput{
		Query:           req.Query,
		UserID:          userID,
		TenantID:        tenantID,
		SessionID:       runtimeSessionID,
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
		"session_id": runtimeSessionID,
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
	forcedModeLabel := ""  // Optional mode override for router (standard|complex)

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
				ml := strings.ToLower(strings.TrimSpace(wf2))
				switch ml {
				case "supervisor":
					workflowOverride = "supervisor"
				case "simple":
					workflowOverride = "simple"
				case "complex", "standard":
					forcedModeLabel = ml
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

	// Check for cron schedule in metadata labels (e.g., labels["cron_schedule"] = "0 9 * * 1-5")
	if req.Metadata != nil {
		if labels := req.Metadata.GetLabels(); labels != nil {
			if cronSchedule, ok := labels["cron_schedule"]; ok {
				cronSchedule = strings.TrimSpace(cronSchedule)
				if cronSchedule != "" {
					// Validate cron expression before setting
					if err := validateCronSchedule(cronSchedule); err != nil {
						s.logger.Error("Invalid cron schedule",
							zap.String("workflow_id", workflowID),
							zap.String("cron_schedule", cronSchedule),
							zap.Error(err))
						return nil, status.Errorf(codes.InvalidArgument, "invalid cron_schedule: %v", err)
					}
					workflowOptions.CronSchedule = cronSchedule
					s.logger.Info("Workflow configured with cron schedule",
						zap.String("workflow_id", workflowID),
						zap.String("cron_schedule", cronSchedule))
				}
			}
		}
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
	case "simple":
		input.Mode = "simple"
		modeStr = "simple"
		memo["mode"] = "simple"
		workflowType = "SimpleTaskWorkflow"
		s.logger.Info("Starting SimpleTaskWorkflow", zap.String("workflow_id", workflowID))
		workflowExecution, err = s.temporalClient.ExecuteWorkflow(
			ctx,
			workflowOptions,
			workflows.SimpleTaskWorkflow,
			input,
		)
	case "", "dag":
		// Default: route through OrchestratorWorkflow
		if forcedModeLabel == "complex" {
			input.Mode = "complex"
			modeStr = "complex"
			memo["mode"] = "complex"
		} else if forcedModeLabel == "standard" {
			input.Mode = "standard"
			modeStr = "standard"
			memo["mode"] = "standard"
		} else {
			if mode == common.ExecutionMode_EXECUTION_MODE_COMPLEX {
				input.Mode = "complex"
				modeStr = "complex"
				memo["mode"] = "complex"
			} else {
				input.Mode = "standard"
				modeStr = "standard"
				memo["mode"] = "standard"
			}
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
		var tenantPtr *uuid.UUID
		if tenantID != "" {
			if t, err := uuid.Parse(tenantID); err == nil {
				tenantPtr = &t
			}
		}
		started := time.Now()

		// Generate task ID to ensure it exists for foreign key references
		taskID := uuid.New()
		initial := &db.TaskExecution{
			ID:         taskID,
			WorkflowID: workflowExecution.GetID(),
			UserID:     uidPtr,
			SessionID:  dbSessionID,
			TenantID:   tenantPtr,
			Query:      req.Query,
			Mode:       modeStr,
			Status:     "RUNNING",
			StartedAt:  started,
			CreatedAt:  started,
		}

		// Persist submission context early so GET status can surface it while running
		if len(ctxMap) > 0 {
			taskCtx := make(map[string]interface{}, len(ctxMap))
			for k, v := range ctxMap {
				taskCtx[k] = v
			}
			initial.Metadata = db.JSONB{
				"task_context": taskCtx,
			}
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
		// Only fallback to DB for NotFound errors (Temporal retention expired)
		// Other errors (outages, permission issues) should propagate as Internal
		var notFoundErr *serviceerror.NotFound
		if errors.As(err, &notFoundErr) && s.dbClient != nil {
			return s.getTaskStatusFromDB(ctx, req.TaskId)
		}
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to describe workflow: %v", err))
		}
		return nil, status.Error(codes.NotFound, "task not found")
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
		// Default to RUNNING, but check control state for PAUSED
		statusOut = pb.TaskStatus_TASK_STATUS_RUNNING
		statusStr = "RUNNING"
		// Best-effort query control state to detect paused workflows
		if ctrlResp, qErr := s.temporalClient.QueryWorkflow(ctx, req.TaskId, "", workflows.QueryControlState); qErr == nil {
			var ctrlState workflows.WorkflowControlState
			if ctrlResp.Get(&ctrlState) == nil && ctrlState.IsPaused {
				statusOut = pb.TaskStatus_TASK_STATUS_PAUSED
				statusStr = "PAUSED"
			}
		}

		// Race condition mitigation: the stream may emit WORKFLOW_COMPLETED slightly
		// before Temporal's visibility APIs show the workflow as closed.
		// Treat the stream signal as a hint and retry Describe briefly; only return
		// a terminal status once Temporal confirms it.
		if statusOut == pb.TaskStatus_TASK_STATUS_RUNNING {
			if streaming.Get().HasEmittedCompletion(ctx, req.TaskId) {
				s.logger.Debug("WORKFLOW_COMPLETED seen in stream; retrying Temporal Describe",
					zap.String("task_id", req.TaskId))

				retryCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
				defer cancel()

			retryLoop:
				for attempt := 0; attempt < 3; attempt++ {
					if attempt > 0 {
						delay := time.Duration(attempt) * 50 * time.Millisecond
						timer := time.NewTimer(delay)
						select {
						case <-retryCtx.Done():
							timer.Stop()
							break retryLoop
						case <-timer.C:
						}
					}

					descAfterWait, descErr := s.temporalClient.DescribeWorkflowExecution(retryCtx, req.TaskId, "")
					if descErr != nil || descAfterWait == nil || descAfterWait.WorkflowExecutionInfo == nil {
						if retryCtx.Err() != nil {
							break
						}
						continue
					}

					// Update status only when Temporal confirms a non-running state.
					if descAfterWait.WorkflowExecutionInfo.Status == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
						continue
					}

					desc = descAfterWait
					switch descAfterWait.WorkflowExecutionInfo.Status {
					case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
						statusOut = pb.TaskStatus_TASK_STATUS_COMPLETED
						statusStr = "COMPLETED"
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
					default:
						statusOut = pb.TaskStatus_TASK_STATUS_RUNNING
						statusStr = "RUNNING"
					}
					break retryLoop
				}
			}
		}
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
	var tenantUUID *uuid.UUID
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

		// Extract tenant_id from memo
		if tenantField, ok := desc.WorkflowExecutionInfo.Memo.Fields["tenant_id"]; ok && tenantField != nil {
			var tenantIDStr string
			if err := dataConverter.FromPayload(tenantField, &tenantIDStr); err == nil && tenantIDStr != "" {
				if tid, err := uuid.Parse(tenantIDStr); err == nil {
					tenantUUID = &tid
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

	// Variables to store DB-aggregated metrics for terminal workflows
	var dbTotalTokens int
	var dbTotalCost float64
	var dbPromptTokens int
	var dbCompletionTokens int
	var hasDBMetrics bool

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
			TenantID:     tenantUUID,
			Query:        query,
			Mode:         mode,
			Status:       statusStr,
			StartedAt:    workflowStartTime.AsTime(),
			TotalTokens:  result.TokensUsed, // Will be overridden by metadata if available
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

			// Extract model and provider
			if model, ok := result.Metadata["model"].(string); ok && model != "" {
				taskExecution.ModelUsed = model
			} else if model, ok := result.Metadata["model_used"].(string); ok && model != "" {
				taskExecution.ModelUsed = model
			}

			if provider, ok := result.Metadata["provider"].(string); ok && provider != "" {
				taskExecution.Provider = provider
			} else if taskExecution.ModelUsed != "" {
				// Fallback: detect provider from model name
				taskExecution.Provider = detectProviderFromModel(taskExecution.ModelUsed)
			}

			// Extract token breakdown (prompt vs completion)
			if inputTokens, ok := result.Metadata["input_tokens"].(float64); ok {
				taskExecution.PromptTokens = int(inputTokens)
			} else if inputTokens, ok := result.Metadata["input_tokens"].(int); ok {
				taskExecution.PromptTokens = inputTokens
			}

			if outputTokens, ok := result.Metadata["output_tokens"].(float64); ok {
				taskExecution.CompletionTokens = int(outputTokens)
			} else if outputTokens, ok := result.Metadata["output_tokens"].(int); ok {
				taskExecution.CompletionTokens = outputTokens
			}

			// Check if metadata has the correct total_tokens value (preferred over TokensUsed)
			if totalTokens, ok := result.Metadata["total_tokens"].(float64); ok && totalTokens > 0 {
				taskExecution.TotalTokens = int(totalTokens)
			} else if totalTokens, ok := result.Metadata["total_tokens"].(int); ok && totalTokens > 0 {
				taskExecution.TotalTokens = totalTokens
			}

			// If breakdown not available but we have total, estimate split (60% prompt, 40% completion)
			if taskExecution.PromptTokens == 0 && taskExecution.CompletionTokens == 0 && taskExecution.TotalTokens > 0 {
				taskExecution.PromptTokens = taskExecution.TotalTokens * 6 / 10
				taskExecution.CompletionTokens = taskExecution.TotalTokens - taskExecution.PromptTokens
			}

			// Extract cost
			if cost, ok := result.Metadata["cost_usd"].(float64); ok {
				taskExecution.TotalCostUSD = cost
			} else if cost, ok := result.Metadata["total_cost"].(float64); ok {
				taskExecution.TotalCostUSD = cost
			} else if result.TokensUsed > 0 {
				// Fallback: calculate cost from tokens
				taskExecution.TotalCostUSD = calculateTokenCost(result.TokensUsed, result.Metadata)
			}

			// Preserve original request-level task_context fields (e.g., force_research)
			// The initial task creation stores these at task submission, but they get lost
			// when result.Metadata overwrites them. Fetch and merge them back.
			if s.dbClient != nil {
				var origMetadataJSON sql.NullString
				row := s.dbClient.Wrapper().QueryRowContext(ctx, `
					SELECT metadata::text FROM task_executions WHERE workflow_id = $1`, workflowID)
				if err := row.Scan(&origMetadataJSON); err == nil && origMetadataJSON.Valid && origMetadataJSON.String != "" {
					var origMetadata map[string]interface{}
					if jsonErr := json.Unmarshal([]byte(origMetadataJSON.String), &origMetadata); jsonErr == nil {
						if origTaskCtx, ok := origMetadata["task_context"].(map[string]interface{}); ok {
							// Ensure task_context exists in result metadata
							resultTaskCtx, hasResultCtx := result.Metadata["task_context"].(map[string]interface{})
							if !hasResultCtx {
								resultTaskCtx = make(map[string]interface{})
							}
							// Preserve request-level fields that shouldn't be overwritten by runtime fields
							requestFields := []string{"force_research", "synthesis_template", "synthesis_template_override"}
							for _, field := range requestFields {
								if val, exists := origTaskCtx[field]; exists {
									if _, alreadySet := resultTaskCtx[field]; !alreadySet {
										resultTaskCtx[field] = val
									}
								}
							}
							result.Metadata["task_context"] = resultTaskCtx
						}
					}
				}
			}

			taskExecution.Metadata = db.JSONB(result.Metadata)
		}

		// If model/provider missing, derive dominant from token_usage
		if s.dbClient != nil {
			if taskExecution.ModelUsed == "" || taskExecution.Provider == "" || strings.EqualFold(taskExecution.Provider, "unknown") {
				var topModel sql.NullString
				var topProvider sql.NullString
				row := s.dbClient.Wrapper().QueryRowContext(ctx, `
                        SELECT COALESCE(model, '') AS model, COALESCE(provider, '') AS provider
                        FROM (
                            SELECT tu.model, tu.provider, SUM(tu.total_tokens) AS tt
                            FROM token_usage tu
                            JOIN task_executions te ON tu.task_id = te.id
                            WHERE te.workflow_id = $1
                            GROUP BY tu.model, tu.provider
                            ORDER BY tt DESC
                            LIMIT 1
                        ) t`, workflowID)
				if err := row.Scan(&topModel, &topProvider); err == nil {
					if taskExecution.ModelUsed == "" && topModel.Valid && topModel.String != "" {
						taskExecution.ModelUsed = topModel.String
					}
					if (taskExecution.Provider == "" || strings.EqualFold(taskExecution.Provider, "unknown")) && topProvider.Valid && topProvider.String != "" {
						taskExecution.Provider = topProvider.String
					}
					// Fallback: detect provider from model when still empty
					if taskExecution.Provider == "" && taskExecution.ModelUsed != "" {
						taskExecution.Provider = detectProviderFromModel(taskExecution.ModelUsed)
					}
				}
			}

			// Enrich agent_usages in metadata from token_usage if missing or zero
			// Build per-agent summary (agent_id, model, provider, input/output/total, cost)
			type agentUsageRow struct {
				AgentID      sql.NullString
				Model        sql.NullString
				Provider     sql.NullString
				InputTokens  sql.NullInt64
				OutputTokens sql.NullInt64
				TotalTokens  sql.NullInt64
				CostUSD      sql.NullFloat64
			}

			needAgentUsages := true
			if result.Metadata != nil {
				if au, ok := result.Metadata["agent_usages"]; ok {
					// If existing usages contain any non-zero totals, keep them
					if list, ok2 := au.([]map[string]interface{}); ok2 {
						for _, m := range list {
							if v, ok := m["total_tokens"]; ok {
								switch t := v.(type) {
								case int:
									if t > 0 {
										needAgentUsages = false
									}
								case float64:
									if int(t) > 0 {
										needAgentUsages = false
									}
								}
							}
							if !needAgentUsages {
								break
							}
						}
					}
				}
			}

			if needAgentUsages {
				rows, err := s.dbClient.Wrapper().QueryContext(ctx, `
                        SELECT COALESCE(agent_id, '') AS agent_id,
                               COALESCE(model, '')     AS model,
                               COALESCE(provider, '')  AS provider,
                               COALESCE(SUM(prompt_tokens), 0)     AS input_tokens,
                               COALESCE(SUM(completion_tokens), 0) AS output_tokens,
                               COALESCE(SUM(total_tokens), 0)      AS total_tokens,
                               COALESCE(SUM(cost_usd), 0)          AS cost_usd
                        FROM token_usage tu
                        JOIN task_executions te ON tu.task_id = te.id
                        WHERE te.workflow_id = $1
                        GROUP BY agent_id, model, provider
                        ORDER BY total_tokens DESC`, workflowID)
				if err == nil {
					defer rows.Close()
					var usages []map[string]interface{}
					for rows.Next() {
						var r agentUsageRow
						if scanErr := rows.Scan(&r.AgentID, &r.Model, &r.Provider, &r.InputTokens, &r.OutputTokens, &r.TotalTokens, &r.CostUSD); scanErr != nil {
							continue
						}
						usage := map[string]interface{}{}
						if r.AgentID.Valid {
							usage["agent_id"] = r.AgentID.String
						}
						if r.Model.Valid && r.Model.String != "" {
							usage["model"] = r.Model.String
						}
						if r.InputTokens.Valid {
							usage["input_tokens"] = int(r.InputTokens.Int64)
						}
						if r.OutputTokens.Valid {
							usage["output_tokens"] = int(r.OutputTokens.Int64)
						}
						if r.TotalTokens.Valid {
							usage["total_tokens"] = int(r.TotalTokens.Int64)
						}
						if r.CostUSD.Valid {
							usage["cost_usd"] = r.CostUSD.Float64
						}
						usages = append(usages, usage)
					}
					if len(usages) > 0 {
						if result.Metadata == nil {
							result.Metadata = make(map[string]interface{})
						}
						result.Metadata["agent_usages"] = usages
						taskExecution.Metadata = db.JSONB(result.Metadata)
					}
				} else {
					s.logger.Debug("agent_usages enrichment query failed", zap.Error(err))
				}
			}
		}

		// Always aggregate from token_usage as the source of truth for all workflow phases
		if s.dbClient != nil {
			var aggCost sql.NullFloat64
			var aggTotalTokens sql.NullInt64
			var aggPromptTokens sql.NullInt64
			var aggCompletionTokens sql.NullInt64
			row := s.dbClient.Wrapper().QueryRowContext(ctx, `
                SELECT
                    COALESCE(SUM(tu.cost_usd), 0),
                    COALESCE(SUM(tu.total_tokens), 0),
                    COALESCE(SUM(tu.prompt_tokens), 0),
                    COALESCE(SUM(tu.completion_tokens), 0)
                FROM token_usage tu
                JOIN task_executions te ON tu.task_id = te.id
                WHERE te.workflow_id = $1`, workflowID)
			if err := row.Scan(&aggCost, &aggTotalTokens, &aggPromptTokens, &aggCompletionTokens); err == nil {
				// Only overwrite with DB aggregates when non-zero (preserves workflow metadata when token_usage rows don't exist yet)
				s.logger.Info("Token usage aggregation succeeded",
					zap.String("workflow_id", workflowID),
					zap.Float64("cost", aggCost.Float64),
					zap.Int64("total_tokens", aggTotalTokens.Int64),
					zap.Int64("prompt_tokens", aggPromptTokens.Int64),
					zap.Int64("completion_tokens", aggCompletionTokens.Int64))
				if aggCost.Valid && aggCost.Float64 > 0 {
					taskExecution.TotalCostUSD = aggCost.Float64
					dbTotalCost = aggCost.Float64
				}
				if aggTotalTokens.Valid && aggTotalTokens.Int64 > 0 {
					taskExecution.TotalTokens = int(aggTotalTokens.Int64)
					dbTotalTokens = int(aggTotalTokens.Int64)
				}
				if aggPromptTokens.Valid && aggPromptTokens.Int64 > 0 {
					taskExecution.PromptTokens = int(aggPromptTokens.Int64)
					dbPromptTokens = int(aggPromptTokens.Int64)
				}
				if aggCompletionTokens.Valid && aggCompletionTokens.Int64 > 0 {
					taskExecution.CompletionTokens = int(aggCompletionTokens.Int64)
					dbCompletionTokens = int(aggCompletionTokens.Int64)
				}
				// Mark that we have DB metrics available
				if dbTotalTokens > 0 {
					hasDBMetrics = true
				}
			} else {
				s.logger.Warn("Token usage aggregation failed",
					zap.String("workflow_id", workflowID),
					zap.Error(err))
			}

			// Secondary fallback: derive from agent_executions only if token_usage aggregation returned zero
			if taskExecution.TotalTokens == 0 {
				var aeTokens sql.NullInt64
				row2 := s.dbClient.Wrapper().QueryRowContext(ctx, `
                    SELECT COALESCE(SUM(ae.tokens_used), 0)
                    FROM agent_executions ae
                    WHERE ae.workflow_id = $1`, workflowID)
				if err2 := row2.Scan(&aeTokens); err2 == nil {
					if aeTokens.Valid && aeTokens.Int64 > 0 {
						taskExecution.TotalTokens = int(aeTokens.Int64)
						// Compute cost using model from metadata when available
						taskExecution.TotalCostUSD = calculateTokenCost(taskExecution.TotalTokens, result.Metadata)
					}
				} else {
					s.logger.Warn("Agent execution aggregation failed",
						zap.String("workflow_id", workflowID),
						zap.Error(err2))
				}
			}
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
	// For terminal workflows, use DB-aggregated totals; for running workflows, use result metadata
	if hasDBMetrics || result.TokensUsed > 0 || result.Metadata != nil {
		totalTokens := result.TokensUsed
		totalCost := calculateTokenCost(result.TokensUsed, result.Metadata)

		// Override with DB aggregates for terminal workflows
		if hasDBMetrics {
			totalTokens = dbTotalTokens
			totalCost = dbTotalCost
		}

		metrics = &common.ExecutionMetrics{
			TokenUsage: &common.TokenUsage{
				TotalTokens: int32(totalTokens),
				CostUsd:     totalCost,
			},
		}

		// Populate prompt/completion breakdown for terminal workflows from DB
		if hasDBMetrics {
			if dbPromptTokens > 0 {
				metrics.TokenUsage.PromptTokens = int32(dbPromptTokens)
			}
			if dbCompletionTokens > 0 {
				metrics.TokenUsage.CompletionTokens = int32(dbCompletionTokens)
			}
		}

		// Extract metadata values if available
		if result.Metadata != nil {
			// Populate model/provider into metrics when available
			if m, ok := result.Metadata["model"].(string); ok && m != "" {
				metrics.TokenUsage.Model = m
			} else if mu, ok := result.Metadata["model_used"].(string); ok && mu != "" {
				metrics.TokenUsage.Model = mu
			}
			if p, ok := result.Metadata["provider"].(string); ok && p != "" {
				metrics.TokenUsage.Provider = p
			}
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

		// Update result.Metadata with aggregated DB values before creating unified_response
		// This ensures unified_response contains accurate cost/token data from token_usage aggregation
		if hasDBMetrics {
			if result.Metadata == nil {
				result.Metadata = make(map[string]interface{})
			}
			if dbTotalTokens > 0 {
				result.Metadata["total_tokens"] = dbTotalTokens
			}
			if dbPromptTokens > 0 {
				result.Metadata["input_tokens"] = dbPromptTokens
			}
			if dbCompletionTokens > 0 {
				result.Metadata["output_tokens"] = dbCompletionTokens
			}
			if dbTotalCost > 0 {
				result.Metadata["cost_usd"] = dbTotalCost
			}
			// Also update result.TokensUsed for consistency
			result.TokensUsed = dbTotalTokens
		}

		// Transform to unified response format (now with accurate aggregated values)
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

	// Send graceful cancel signal first (allows checkpoint cleanup and SSE emission)
	cancelReq := workflows.CancelRequest{
		Reason:      req.Reason,
		RequestedBy: uc.UserID.String(),
	}
	// Fire-and-forget signal - don't fail cancel if signal fails
	_ = s.temporalClient.SignalWorkflow(ctx, req.TaskId, "", workflows.SignalCancel, cancelReq)

	// Brief delay to allow signal processing before Temporal cancellation
	// This ensures the workflow's signal handler can update state and emit SSE events
	time.Sleep(100 * time.Millisecond)

	// Then request Temporal cancellation (fallback for stuck activities)
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

// PauseTask pauses a running workflow (with ownership check)
func (s *OrchestratorService) PauseTask(ctx context.Context, req *pb.PauseTaskRequest) (*pb.PauseTaskResponse, error) {
	s.logger.Info("Received PauseTask request",
		zap.String("task_id", req.TaskId),
		zap.String("reason", req.Reason),
	)

	// Enforce authentication
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	// Verify ownership/tenancy via workflow memo (existing pattern from CancelTask)
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
				return nil, status.Error(codes.NotFound, "task not found")
			}
		}
		// Check user ownership
		if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["user_id"]; ok && f != nil {
			var memoUser string
			_ = dc.FromPayload(f, &memoUser)
			if memoUser != "" && uc.UserID.String() != memoUser {
				return nil, status.Error(codes.NotFound, "task not found")
			}
		}
	}

	// Validate workflow state - cannot pause completed/failed/cancelled workflows
	switch desc.WorkflowExecutionInfo.Status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return nil, status.Error(codes.FailedPrecondition, "cannot pause completed task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return nil, status.Error(codes.FailedPrecondition, "cannot pause failed task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return nil, status.Error(codes.FailedPrecondition, "cannot pause cancelled task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return nil, status.Error(codes.FailedPrecondition, "cannot pause timed out task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		// Check if already paused
		if ctrlResp, qErr := s.temporalClient.QueryWorkflow(ctx, req.TaskId, "", workflows.QueryControlState); qErr == nil {
			var ctrlState workflows.WorkflowControlState
			if ctrlResp.Get(&ctrlState) == nil && ctrlState.IsPaused {
				return nil, status.Error(codes.FailedPrecondition, "task is already paused")
			}
		}
	}

	// Send pause signal to Temporal
	pauseReq := workflows.PauseRequest{
		Reason:      req.Reason,
		RequestedBy: uc.UserID.String(),
	}
	if err := s.temporalClient.SignalWorkflow(ctx, req.TaskId, "", workflows.SignalPause, pauseReq); err != nil {
		s.logger.Error("Failed to send pause signal", zap.Error(err))
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to pause: %v", err))
	}

	// Update task status in DB
	if s.dbClient != nil {
		_ = s.dbClient.UpdateTaskStatus(ctx, req.TaskId, "PAUSED")
	}

	s.logger.Info("Task paused",
		zap.String("task_id", req.TaskId),
		zap.String("user_id", uc.UserID.String()),
	)

	return &pb.PauseTaskResponse{
		Success: true,
		Message: "Pause signal sent",
	}, nil
}

// ResumeTask resumes a paused workflow (with ownership check)
func (s *OrchestratorService) ResumeTask(ctx context.Context, req *pb.ResumeTaskRequest) (*pb.ResumeTaskResponse, error) {
	s.logger.Info("Received ResumeTask request",
		zap.String("task_id", req.TaskId),
		zap.String("reason", req.Reason),
	)

	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	// Verify ownership/tenancy via workflow memo
	desc, dErr := s.temporalClient.DescribeWorkflowExecution(ctx, req.TaskId, "")
	if dErr != nil || desc == nil || desc.WorkflowExecutionInfo == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	if desc.WorkflowExecutionInfo.Memo != nil {
		dc := converter.GetDefaultDataConverter()
		if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["tenant_id"]; ok && f != nil {
			var memoTenant string
			_ = dc.FromPayload(f, &memoTenant)
			if memoTenant != "" && uc.TenantID.String() != memoTenant {
				return nil, status.Error(codes.NotFound, "task not found")
			}
		}
		if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["user_id"]; ok && f != nil {
			var memoUser string
			_ = dc.FromPayload(f, &memoUser)
			if memoUser != "" && uc.UserID.String() != memoUser {
				return nil, status.Error(codes.NotFound, "task not found")
			}
		}
	}

	// Validate workflow state - cannot resume completed/failed/cancelled workflows
	switch desc.WorkflowExecutionInfo.Status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return nil, status.Error(codes.FailedPrecondition, "cannot resume completed task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return nil, status.Error(codes.FailedPrecondition, "cannot resume failed task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return nil, status.Error(codes.FailedPrecondition, "cannot resume cancelled task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return nil, status.Error(codes.FailedPrecondition, "cannot resume timed out task")
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		// Check if task is actually paused
		isPaused := false
		if ctrlResp, qErr := s.temporalClient.QueryWorkflow(ctx, req.TaskId, "", workflows.QueryControlState); qErr == nil {
			var ctrlState workflows.WorkflowControlState
			if ctrlResp.Get(&ctrlState) == nil {
				isPaused = ctrlState.IsPaused
			}
		}
		if !isPaused {
			return nil, status.Error(codes.FailedPrecondition, "task is not paused")
		}
	}

	resumeReq := workflows.ResumeRequest{
		Reason:      req.Reason,
		RequestedBy: uc.UserID.String(),
	}
	if err := s.temporalClient.SignalWorkflow(ctx, req.TaskId, "", workflows.SignalResume, resumeReq); err != nil {
		s.logger.Error("Failed to send resume signal", zap.Error(err))
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to resume: %v", err))
	}

	// Update task status back to RUNNING
	if s.dbClient != nil {
		_ = s.dbClient.UpdateTaskStatus(ctx, req.TaskId, "RUNNING")
	}

	s.logger.Info("Task resumed",
		zap.String("task_id", req.TaskId),
		zap.String("user_id", uc.UserID.String()),
	)

	return &pb.ResumeTaskResponse{
		Success: true,
		Message: "Resume signal sent",
	}, nil
}

// GetControlState queries workflow control state (with ownership check)
func (s *OrchestratorService) GetControlState(ctx context.Context, req *pb.GetControlStateRequest) (*pb.GetControlStateResponse, error) {
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	// Verify ownership/tenancy via workflow memo
	desc, dErr := s.temporalClient.DescribeWorkflowExecution(ctx, req.TaskId, "")
	if dErr != nil || desc == nil || desc.WorkflowExecutionInfo == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	if desc.WorkflowExecutionInfo.Memo != nil {
		dc := converter.GetDefaultDataConverter()
		if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["tenant_id"]; ok && f != nil {
			var memoTenant string
			_ = dc.FromPayload(f, &memoTenant)
			if memoTenant != "" && uc.TenantID.String() != memoTenant {
				return nil, status.Error(codes.NotFound, "task not found")
			}
		}
		if f, ok := desc.WorkflowExecutionInfo.Memo.Fields["user_id"]; ok && f != nil {
			var memoUser string
			_ = dc.FromPayload(f, &memoUser)
			if memoUser != "" && uc.UserID.String() != memoUser {
				return nil, status.Error(codes.NotFound, "task not found")
			}
		}
	}

	// Query Temporal workflow for control state
	resp, err := s.temporalClient.QueryWorkflow(ctx, req.TaskId, "", workflows.QueryControlState)
	if err != nil {
		// Workflow may have completed - return default state
		return &pb.GetControlStateResponse{
			IsPaused:    false,
			IsCancelled: false,
			PausedAt:    "", // Empty string for never-paused
		}, nil
	}

	var state workflows.WorkflowControlState
	if err := resp.Get(&state); err != nil {
		return nil, status.Error(codes.Internal, "failed to decode state")
	}

	// Handle zero time gracefully - return empty string instead of epoch
	pausedAtStr := ""
	if !state.PausedAt.IsZero() {
		pausedAtStr = state.PausedAt.Format(time.RFC3339)
	}

	return &pb.GetControlStateResponse{
		IsPaused:     state.IsPaused,
		IsCancelled:  state.IsCancelled,
		PausedAt:     pausedAtStr,
		PauseReason:  state.PauseReason,
		PausedBy:     state.PausedBy,
		CancelReason: state.CancelReason,
		CancelledBy:  state.CancelledBy,
	}, nil
}

// ListTasks lists tasks for a user/session
func (s *OrchestratorService) ListTasks(ctx context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	if s.dbClient == nil {
		return &pb.ListTasksResponse{Tasks: []*pb.TaskSummary{}, TotalCount: 0}, nil
	}

	// Enforce tenant scoping when available
	var tenantFilter *uuid.UUID
	if uc, err := auth.GetUserContext(ctx); err == nil && uc != nil {
		t := uc.TenantID
		tenantFilter = &t
	}

	// Build filters
	where := []string{"1=1"}
	args := []interface{}{}
	ai := 1

	if tenantFilter != nil {
		where = append(where, fmt.Sprintf("tenant_id = $%d", ai))
		args = append(args, *tenantFilter)
		ai++
	}

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

	var tenantFilter *uuid.UUID
	if uc, err := auth.GetUserContext(ctx); err == nil && uc != nil {
		t := uc.TenantID
		tenantFilter = &t
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
		// Resolve both canonical UUID and external_id for dual-format session IDs
		sessionIDs := []string{}
		var dbID string
		var extID sql.NullString
		row := s.dbClient.Wrapper().QueryRowContext(ctx, `
            SELECT id::text, context->>'external_id'
            FROM sessions
            WHERE (id::text = $1 OR context->>'external_id' = $1) AND deleted_at IS NULL
        `, req.SessionId)
		if err := row.Scan(&dbID, &extID); err == nil {
			sessionIDs = append(sessionIDs, dbID)
			if extID.Valid && extID.String != "" {
				sessionIDs = append(sessionIDs, extID.String)
			}
			// Best-effort tenant hint from persisted session
			if tenantFilter == nil {
				var sidTenant sql.NullString
				if err := s.dbClient.Wrapper().QueryRowContext(ctx,
					`SELECT tenant_id::text FROM sessions WHERE id::text = $1 OR context->>'external_id' = $1 LIMIT 1`,
					req.SessionId,
				).Scan(&sidTenant); err == nil && sidTenant.Valid {
					if tid, err := uuid.Parse(sidTenant.String); err == nil {
						tenantFilter = &tid
					}
				}
			}
		} else {
			// Fallback to the provided session ID if not resolvable in DB
			sessionIDs = append(sessionIDs, req.SessionId)
		}

		tasks, err := s.loadRecentSessionTasksByIDs(ctx, sessionIDs, 5, tenantFilter)
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

func (s *OrchestratorService) loadRecentSessionTasks(ctx context.Context, sessionID string, limit int, tenantID *uuid.UUID) ([]*pb.TaskSummary, error) {
	if sessionID == "" || limit <= 0 || s.dbClient == nil {
		return nil, nil
	}

	where := []string{"session_id = $1"}
	args := []interface{}{sessionID, limit}
	if tenantID != nil {
		where = append(where, "tenant_id = $3")
		args = append(args, *tenantID)
	}

	query := fmt.Sprintf(`
        SELECT workflow_id, query, status, mode,
               started_at, completed_at, created_at,
               total_tokens, total_cost_usd
        FROM task_executions
        WHERE %s
        ORDER BY COALESCE(started_at, created_at) DESC
        LIMIT $2`, strings.Join(where, " AND "))

	rows, err := s.dbClient.Wrapper().QueryContext(ctx, query, args...)
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

// loadRecentSessionTasksByIDs loads recent tasks for one or two possible session IDs
// to support dual-format session identifiers (UUID and external string ID).
func (s *OrchestratorService) loadRecentSessionTasksByIDs(ctx context.Context, sessionIDs []string, limit int, tenantID *uuid.UUID) ([]*pb.TaskSummary, error) {
	// Normalize inputs
	ids := make([]string, 0, 2)
	for _, id := range sessionIDs {
		if id != "" {
			ids = append(ids, id)
			if len(ids) == 2 {
				break
			}
		}
	}
	if len(ids) == 0 || limit <= 0 || s.dbClient == nil {
		return nil, nil
	}
	if len(ids) == 1 {
		return s.loadRecentSessionTasks(ctx, ids[0], limit, tenantID)
	}

	where := []string{"(session_id = $1 OR session_id = $2)"}
	args := []interface{}{ids[0], ids[1], limit}
	if tenantID != nil {
		where = append(where, "tenant_id = $4")
		args = append(args, *tenantID)
	}

	// Build query for two IDs
	query := fmt.Sprintf(`
        SELECT workflow_id, query, status, mode,
               started_at, completed_at, created_at,
               total_tokens, total_cost_usd
        FROM task_executions
        WHERE %s
        ORDER BY COALESCE(started_at, created_at) DESC
        LIMIT $3`, strings.Join(where, " AND "))

	rows, err := s.dbClient.Wrapper().QueryContext(ctx, query, args...)
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
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to iterate rows: %v", err))
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
	case "PAUSED":
		return pb.TaskStatus_TASK_STATUS_PAUSED
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
	case pb.TaskStatus_TASK_STATUS_PAUSED:
		return "PAUSED"
	default:
		return ""
	}
}

// watchAndPersist waits for workflow completion and persists terminal state to DB.
// It loops until the workflow reaches a terminal state (no hard timeout) to support
// long-running workflows like SupervisorWorkflow that can run for hours or days.
func (s *OrchestratorService) watchAndPersist(workflowID, runID string) {
	if s.temporalClient == nil || s.dbClient == nil {
		return
	}

	// Loop until workflow reaches terminal state. Use per-iteration timeouts
	// but no overall deadline - workflows can legitimately run for hours/days.
	const pollInterval = 5 * time.Minute
	const maxConsecutiveErrors = 12 // Give up after ~1 hour of consecutive errors

	consecutiveErrors := 0
	for {
		// Per-iteration timeout for the wait call
		waitCtx, waitCancel := context.WithTimeout(context.Background(), pollInterval)

		// Try to wait for workflow completion
		// Pass empty runID to track the latest run (handles CONTINUED_AS_NEW correctly)
		we := s.temporalClient.GetWorkflow(waitCtx, workflowID, "")
		var tmp interface{}
		waitErr := we.Get(waitCtx, &tmp)
		waitCancel()

		// Check workflow status
		// Pass empty runID to get the latest run - handles CONTINUED_AS_NEW correctly
		descCtx, descCancel := context.WithTimeout(context.Background(), 30*time.Second)
		desc, err := s.temporalClient.DescribeWorkflowExecution(descCtx, workflowID, "")
		descCancel()

		if err != nil || desc == nil || desc.WorkflowExecutionInfo == nil {
			consecutiveErrors++
			s.logger.Warn("watchAndPersist: describe failed",
				zap.String("workflow_id", workflowID),
				zap.Int("consecutive_errors", consecutiveErrors),
				zap.Error(err))
			if consecutiveErrors >= maxConsecutiveErrors {
				s.logger.Error("watchAndPersist: giving up after too many consecutive errors",
					zap.String("workflow_id", workflowID))
				// Mark task as FAILED in DB to avoid leaving it stuck in RUNNING state
				failCtx, failCancel := context.WithTimeout(context.Background(), 10*time.Second)
				if _, err2 := s.dbClient.Wrapper().ExecContext(
					failCtx,
					`UPDATE task_executions
					 SET status = 'FAILED',
					     error_message = COALESCE(error_message, 'Workflow status unknown: persistence monitoring failed after consecutive errors'),
					     completed_at = NOW()
					 WHERE workflow_id = $1
					   AND status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED')`,
					workflowID,
				); err2 != nil {
					s.logger.Error("watchAndPersist: failed to mark task as FAILED",
						zap.String("workflow_id", workflowID),
						zap.Error(err2))
				} else {
					s.logger.Info("watchAndPersist: marked task as FAILED due to monitoring failure",
						zap.String("workflow_id", workflowID))
				}
				failCancel()
				return
			}
			time.Sleep(30 * time.Second) // Back off before retry
			continue
		}
		consecutiveErrors = 0 // Reset on successful describe

		st := desc.WorkflowExecutionInfo.GetStatus()
		isTerminal := st == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED ||
			st == enumspb.WORKFLOW_EXECUTION_STATUS_FAILED ||
			st == enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT ||
			st == enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED ||
			st == enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED

		if !isTerminal {
			// Workflow still running (or CONTINUED_AS_NEW) - continue waiting
			// Since we pass empty runID, CONTINUED_AS_NEW is handled correctly
			// by tracking the new run automatically
			if waitErr != nil {
				s.logger.Debug("watchAndPersist: workflow still running, will retry",
					zap.String("workflow_id", workflowID),
					zap.String("status", st.String()))
			}
			continue
		}

		// Workflow reached terminal state - persist it
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
		}

		s.logger.Info("watchAndPersist: workflow reached terminal state",
			zap.String("workflow_id", workflowID),
			zap.String("status", statusStr))

		start := time.Now()
		if desc.WorkflowExecutionInfo.GetStartTime() != nil {
			start = desc.WorkflowExecutionInfo.GetStartTime().AsTime()
		}
		end := getWorkflowEndTime(desc)
		durationMs := int(end.Sub(start).Milliseconds())

		// Persist terminal state
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 30*time.Second)

		// Update terminal fields only for terminal workflows
		if _, err2 := s.dbClient.Wrapper().ExecContext(
			persistCtx,
			`UPDATE task_executions
             SET status = $2,
                 completed_at = $3,
                 duration_ms = COALESCE(duration_ms, $4)
             WHERE workflow_id = $1
               AND status NOT IN ('COMPLETED', 'FAILED')`,
			workflowID, statusStr, end, durationMs,
		); err2 != nil {
			s.logger.Warn("watchAndPersist: final status update failed",
				zap.String("workflow_id", workflowID),
				zap.Error(err2))
		} else {
			s.logger.Debug("watchAndPersist: final status updated",
				zap.String("workflow_id", workflowID),
				zap.String("status", statusStr))
		}

		// Trigger rich persistence (result + token/cost aggregation)
		if _, err := s.GetTaskStatus(persistCtx, &pb.GetTaskStatusRequest{TaskId: workflowID}); err != nil {
			s.logger.Warn("watchAndPersist: GetTaskStatus persistence failed",
				zap.String("workflow_id", workflowID),
				zap.Error(err))
		} else {
			s.logger.Debug("watchAndPersist: terminal metrics persisted",
				zap.String("workflow_id", workflowID))
		}

		// Best-effort polling to tolerate visibility lag
		const maxAttempts = 6
		for attempt := 0; attempt < maxAttempts; attempt++ {
			var tokens sql.NullInt64
			var res sql.NullString
			row := s.dbClient.Wrapper().QueryRowContext(persistCtx,
				`SELECT total_tokens, result FROM task_executions WHERE workflow_id = $1`, workflowID,
			)
			if err := row.Scan(&tokens, &res); err == nil {
				hasTokens := tokens.Valid && tokens.Int64 > 0
				hasResult := res.Valid && res.String != ""
				if hasTokens || hasResult {
					s.logger.Debug("watchAndPersist: persistence confirmed",
						zap.String("workflow_id", workflowID),
						zap.Bool("has_tokens", hasTokens),
						zap.Bool("has_result", hasResult))
					break
				}
			}
			if _, err := s.GetTaskStatus(persistCtx, &pb.GetTaskStatusRequest{TaskId: workflowID}); err != nil {
				s.logger.Debug("watchAndPersist: retry GetTaskStatus failed",
					zap.String("workflow_id", workflowID),
					zap.Error(err))
			}
			time.Sleep(2 * time.Second)
		}

		persistCancel()
		return // Done - workflow persisted
	}
}

// getTaskStatusFromDB retrieves task status from database when Temporal workflow history has expired.
// This allows querying historical completed tasks beyond Temporal's retention period.
func (s *OrchestratorService) getTaskStatusFromDB(ctx context.Context, taskID string) (*pb.GetTaskStatusResponse, error) {
	var (
		dbStatus         string
		dbResult         sql.NullString
		dbError          sql.NullString
		dbQuery          sql.NullString
		dbSessionID      sql.NullString
		dbMode           sql.NullString
		dbModelUsed      sql.NullString
		dbProvider       sql.NullString
		dbTotalTokens    sql.NullInt32
		dbPromptTokens   sql.NullInt32
		dbCompletionToks sql.NullInt32
		dbTotalCost      sql.NullFloat64
		dbTenantID       sql.NullString
	)

	err := s.dbClient.Wrapper().QueryRowContext(ctx, `
		SELECT status, result, error_message, query, session_id, mode,
		       model_used, provider, total_tokens, prompt_tokens, completion_tokens,
		       total_cost_usd, tenant_id
		FROM task_executions
		WHERE workflow_id = $1
		LIMIT 1
	`, taskID).Scan(&dbStatus, &dbResult, &dbError, &dbQuery, &dbSessionID, &dbMode,
		&dbModelUsed, &dbProvider, &dbTotalTokens, &dbPromptTokens, &dbCompletionToks,
		&dbTotalCost, &dbTenantID)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("database error: %v", err))
	}

	// Enforce tenant ownership
	if dbTenantID.Valid && dbTenantID.String != "" {
		if uc, ucErr := auth.GetUserContext(ctx); ucErr == nil && uc != nil {
			if uc.TenantID.String() != dbTenantID.String {
				return nil, status.Error(codes.NotFound, "task not found")
			}
		}
	}

	// Map DB status to proto status
	var pbStatus pb.TaskStatus
	switch dbStatus {
	case "COMPLETED":
		pbStatus = pb.TaskStatus_TASK_STATUS_COMPLETED
	case "FAILED":
		pbStatus = pb.TaskStatus_TASK_STATUS_FAILED
	case "CANCELLED":
		pbStatus = pb.TaskStatus_TASK_STATUS_CANCELLED
	case "TIMEOUT":
		pbStatus = pb.TaskStatus_TASK_STATUS_TIMEOUT
	default:
		pbStatus = pb.TaskStatus_TASK_STATUS_UNSPECIFIED
	}

	resp := &pb.GetTaskStatusResponse{
		TaskId:       taskID,
		Status:       pbStatus,
		Result:       dbResult.String,
		ErrorMessage: dbError.String,
	}

	// Note: Gateway enriches response with metrics from DB, so we only need
	// to return status/result here. The unused dbXxx vars are scanned but
	// intentionally not populated in the proto response.

	s.logger.Info("Retrieved task status from database (Temporal history expired)",
		zap.String("task_id", taskID),
		zap.String("status", dbStatus))

	return resp, nil
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

// detectProviderFromModel determines the provider based on the model name
// Delegates to shared models.DetectProvider for consistent provider detection
func detectProviderFromModel(model string) string {
	return models.DetectProvider(model)
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

// SubmitReviewDecision handles the HITL research review approval.
// It sends a Temporal Signal to the waiting workflow with the review result.
func (s *OrchestratorService) SubmitReviewDecision(
	ctx context.Context,
	req *pb.SubmitReviewDecisionRequest,
) (*pb.SubmitReviewDecisionResponse, error) {
	if req.WorkflowId == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}

	// Authenticate
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	// Validate ownership via Temporal memo (same pattern as GetTask)
	wfDesc, err := s.temporalClient.DescribeWorkflowExecution(ctx, req.WorkflowId, "")
	if err != nil {
		s.logger.Warn("Failed to describe workflow for review decision",
			zap.String("workflow_id", req.WorkflowId),
			zap.Error(err))
		return nil, status.Error(codes.NotFound, "workflow not found")
	}

	// Check tenant ownership from memo
	if wfDesc.WorkflowExecutionInfo.Memo != nil {
		dataConverter := converter.GetDefaultDataConverter()
		if tenantField, ok := wfDesc.WorkflowExecutionInfo.Memo.Fields["tenant_id"]; ok && tenantField != nil {
			var memoTenant string
			_ = dataConverter.FromPayload(tenantField, &memoTenant)
			if memoTenant != "" && uc.TenantID.String() != memoTenant {
				return nil, status.Error(codes.PermissionDenied, "not authorized for this workflow")
			}
		}
	}

	// Build the Signal payload
	var conversation []activities.ReviewRound
	if req.Conversation != "" {
		if err := json.Unmarshal([]byte(req.Conversation), &conversation); err != nil {
			s.logger.Warn("Failed to parse conversation JSON", zap.Error(err))
			// Non-fatal: conversation is informational
		}
	}

	reviewResult := activities.ResearchReviewResult{
		Approved:      req.Approved,
		FinalPlan:     req.FinalPlan,
		Conversation:  conversation,
		ResearchBrief: req.ResearchBrief,
	}

	// Send Signal to workflow
	sigName := "research-plan-approved-" + req.WorkflowId
	err = s.temporalClient.SignalWorkflow(ctx, req.WorkflowId, "", sigName, reviewResult)
	if err != nil {
		s.logger.Error("Failed to signal workflow for review decision",
			zap.String("workflow_id", req.WorkflowId),
			zap.Error(err))
		return nil, status.Errorf(codes.Internal, "failed to signal workflow: %v", err)
	}

	s.logger.Info("HITL review decision submitted",
		zap.String("workflow_id", req.WorkflowId),
		zap.Bool("approved", req.Approved),
		zap.String("approved_by", req.ApprovedBy),
	)

	return &pb.SubmitReviewDecisionResponse{
		Success: true,
		Message: "Review decision submitted",
	}, nil
}

// RecordTokenUsage records token usage from Gateway-side LLM calls (e.g., HITL feedback).
func (s *OrchestratorService) RecordTokenUsage(
	ctx context.Context,
	req *pb.RecordTokenUsageRequest,
) (*pb.RecordTokenUsageResponse, error) {
	if req.WorkflowId == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}

	// Best-effort recording — don't fail if budget manager is unavailable
	s.logger.Info("Recording token usage from gateway",
		zap.String("workflow_id", req.WorkflowId),
		zap.String("agent_id", req.AgentId),
		zap.String("model", req.Model),
		zap.Int32("input_tokens", req.InputTokens),
		zap.Int32("output_tokens", req.OutputTokens),
	)

	return &pb.RecordTokenUsageResponse{Success: true}, nil
}

// CreateSchedule creates a new scheduled task (with ownership)
func (s *OrchestratorService) CreateSchedule(ctx context.Context, req *pb.CreateScheduleRequest) (*pb.CreateScheduleResponse, error) {
	// 1. Auth enforcement
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	// 2. Check if schedule manager is available
	if s.scheduleManager == nil {
		return nil, status.Error(codes.Unavailable, "scheduling feature not enabled")
	}

	// 3. Validate input
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "schedule name required")
	}
	if req.CronExpression == "" {
		return nil, status.Error(codes.InvalidArgument, "cron expression required")
	}
	if req.TaskQuery == "" {
		return nil, status.Error(codes.InvalidArgument, "task query required")
	}

	// 4. Set defaults
	timezone := req.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	timeoutSecs := req.TimeoutSeconds
	if timeoutSecs == 0 {
		timeoutSecs = 3600 // 1 hour default
	}

	// 5. Convert proto map to Go map (decoding JSON-encoded complex values)
	taskContext := decodeTaskContext(req.TaskContext)

	// 6. Create via schedule manager
	input := &schedules.CreateScheduleInput{
		UserID:             uc.UserID,
		TenantID:           uc.TenantID,
		Name:               req.Name,
		Description:        req.Description,
		CronExpression:     req.CronExpression,
		Timezone:           timezone,
		TaskQuery:          req.TaskQuery,
		TaskContext:        taskContext,
		MaxBudgetPerRunUSD: req.MaxBudgetPerRunUsd,
		TimeoutSeconds:     int(timeoutSecs),
	}

	schedule, err := s.scheduleManager.CreateSchedule(ctx, input)
	if err != nil {
		s.logger.Error("Failed to create schedule", zap.Error(err))
		// Map typed errors to proper gRPC codes
		if errors.Is(err, schedules.ErrInvalidCronExpression) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if errors.Is(err, schedules.ErrIntervalTooShort) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if errors.Is(err, schedules.ErrScheduleLimitReached) {
			return nil, status.Error(codes.ResourceExhausted, err.Error())
		}
		if errors.Is(err, schedules.ErrBudgetExceeded) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if errors.Is(err, schedules.ErrInvalidTimezone) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create schedule: %v", err))
	}

	nextRunStr := ""
	if schedule.NextRunAt != nil {
		nextRunStr = schedule.NextRunAt.Format(time.RFC3339)
	}

	return &pb.CreateScheduleResponse{
		ScheduleId: schedule.ID.String(),
		Message:    "Schedule created successfully",
		NextRunAt:  nextRunStr,
	}, nil
}

// GetSchedule retrieves a schedule (with ownership check)
func (s *OrchestratorService) GetSchedule(ctx context.Context, req *pb.GetScheduleRequest) (*pb.GetScheduleResponse, error) {
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if s.scheduleManager == nil {
		return nil, status.Error(codes.Unavailable, "scheduling feature not enabled")
	}

	scheduleID, err := uuid.Parse(req.ScheduleId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid schedule ID")
	}

	schedule, err := s.scheduleManager.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}

	// Ownership check
	if schedule.UserID != uc.UserID || schedule.TenantID != uc.TenantID {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}

	return &pb.GetScheduleResponse{
		Schedule: convertScheduleToProto(schedule),
	}, nil
}

// ListSchedules retrieves schedules for a user
func (s *OrchestratorService) ListSchedules(ctx context.Context, req *pb.ListSchedulesRequest) (*pb.ListSchedulesResponse, error) {
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if s.scheduleManager == nil {
		return nil, status.Error(codes.Unavailable, "scheduling feature not enabled")
	}

	// Set defaults
	pageSize := int(req.PageSize)
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	page := int(req.Page)
	if page <= 0 {
		page = 1
	}

	statusFilter := req.Status
	if statusFilter != "ACTIVE" && statusFilter != "PAUSED" && statusFilter != "ALL" {
		statusFilter = "ALL"
	}

	schedulesList, totalCount, err := s.scheduleManager.ListSchedules(ctx, uc.UserID, uc.TenantID, page, pageSize, statusFilter)
	if err != nil {
		s.logger.Error("Failed to list schedules", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to list schedules")
	}

	scheduleInfos := make([]*pb.ScheduleInfo, 0, len(schedulesList))
	for _, schedule := range schedulesList {
		scheduleInfos = append(scheduleInfos, convertScheduleToProto(schedule))
	}

	return &pb.ListSchedulesResponse{
		Schedules:  scheduleInfos,
		TotalCount: int32(totalCount),
		Page:       int32(page),
		PageSize:   int32(pageSize),
	}, nil
}

// UpdateSchedule updates a schedule
func (s *OrchestratorService) UpdateSchedule(ctx context.Context, req *pb.UpdateScheduleRequest) (*pb.UpdateScheduleResponse, error) {
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if s.scheduleManager == nil {
		return nil, status.Error(codes.Unavailable, "scheduling feature not enabled")
	}

	scheduleID, err := uuid.Parse(req.ScheduleId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid schedule ID")
	}

	// Ownership check
	schedule, err := s.scheduleManager.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}
	if schedule.UserID != uc.UserID || schedule.TenantID != uc.TenantID {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}

	// Build update input
	updateInput := &schedules.UpdateScheduleInput{
		ScheduleID: scheduleID,
	}

	// Handle TaskContext: set if provided with values, or clear if explicitly requested
	if req.ClearTaskContext {
		// Explicit clear: set to empty map (not nil) to overwrite existing
		updateInput.TaskContext = make(map[string]interface{})
	} else if len(req.TaskContext) > 0 {
		// New values provided: decode JSON-encoded complex values and set
		updateInput.TaskContext = decodeTaskContext(req.TaskContext)
	}
	// If neither condition: TaskContext stays nil, preserving existing value

	// Only set fields that are provided
	if req.Name != nil {
		updateInput.Name = req.Name
	}
	if req.Description != nil {
		updateInput.Description = req.Description
	}
	if req.CronExpression != nil {
		updateInput.CronExpression = req.CronExpression
	}
	if req.Timezone != nil {
		updateInput.Timezone = req.Timezone
	}
	if req.TaskQuery != nil {
		updateInput.TaskQuery = req.TaskQuery
	}
	if req.MaxBudgetPerRunUsd != nil {
		updateInput.MaxBudgetPerRunUSD = req.MaxBudgetPerRunUsd
	}
	if req.TimeoutSeconds != nil {
		timeoutInt := int(*req.TimeoutSeconds)
		updateInput.TimeoutSeconds = &timeoutInt
	}

	nextRun, err := s.scheduleManager.UpdateSchedule(ctx, updateInput)
	if err != nil {
		s.logger.Error("Failed to update schedule", zap.Error(err))
		// Map typed errors to proper gRPC codes
		if errors.Is(err, schedules.ErrInvalidCronExpression) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if errors.Is(err, schedules.ErrIntervalTooShort) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if errors.Is(err, schedules.ErrBudgetExceeded) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if errors.Is(err, schedules.ErrInvalidTimezone) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if errors.Is(err, schedules.ErrScheduleNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to update schedule: %v", err))
	}

	nextRunStr := ""
	if nextRun != nil {
		nextRunStr = nextRun.Format(time.RFC3339)
	}

	return &pb.UpdateScheduleResponse{
		Success:   true,
		Message:   "Schedule updated successfully",
		NextRunAt: nextRunStr,
	}, nil
}

// DeleteSchedule deletes a schedule
func (s *OrchestratorService) DeleteSchedule(ctx context.Context, req *pb.DeleteScheduleRequest) (*pb.DeleteScheduleResponse, error) {
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if s.scheduleManager == nil {
		return nil, status.Error(codes.Unavailable, "scheduling feature not enabled")
	}

	scheduleID, err := uuid.Parse(req.ScheduleId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid schedule ID")
	}

	// Ownership check
	schedule, err := s.scheduleManager.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}
	if schedule.UserID != uc.UserID || schedule.TenantID != uc.TenantID {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}

	if err := s.scheduleManager.DeleteSchedule(ctx, scheduleID); err != nil {
		s.logger.Error("Failed to delete schedule", zap.Error(err))
		return nil, status.Error(codes.Internal, "failed to delete schedule")
	}

	return &pb.DeleteScheduleResponse{
		Success: true,
		Message: "Schedule deleted successfully",
	}, nil
}

// PauseSchedule pauses a schedule
func (s *OrchestratorService) PauseSchedule(ctx context.Context, req *pb.PauseScheduleRequest) (*pb.PauseScheduleResponse, error) {
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if s.scheduleManager == nil {
		return nil, status.Error(codes.Unavailable, "scheduling feature not enabled")
	}

	scheduleID, err := uuid.Parse(req.ScheduleId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid schedule ID")
	}

	// Ownership check
	schedule, err := s.scheduleManager.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}
	if schedule.UserID != uc.UserID || schedule.TenantID != uc.TenantID {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}

	if err := s.scheduleManager.PauseSchedule(ctx, scheduleID, req.Reason); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to pause schedule: %v", err))
	}

	return &pb.PauseScheduleResponse{
		Success: true,
		Message: "Schedule paused",
	}, nil
}

// ResumeSchedule resumes a paused schedule
func (s *OrchestratorService) ResumeSchedule(ctx context.Context, req *pb.ResumeScheduleRequest) (*pb.ResumeScheduleResponse, error) {
	uc, err := auth.GetUserContext(ctx)
	if err != nil || uc == nil {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}

	if s.scheduleManager == nil {
		return nil, status.Error(codes.Unavailable, "scheduling feature not enabled")
	}

	scheduleID, err := uuid.Parse(req.ScheduleId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid schedule ID")
	}

	// Ownership check
	schedule, err := s.scheduleManager.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}
	if schedule.UserID != uc.UserID || schedule.TenantID != uc.TenantID {
		return nil, status.Error(codes.NotFound, "schedule not found")
	}

	nextRun, err := s.scheduleManager.ResumeSchedule(ctx, scheduleID, req.Reason)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to resume schedule: %v", err))
	}

	nextRunStr := ""
	if nextRun != nil {
		nextRunStr = nextRun.Format(time.RFC3339)
	}

	return &pb.ResumeScheduleResponse{
		Success:   true,
		Message:   "Schedule resumed",
		NextRunAt: nextRunStr,
	}, nil
}

// Helper to convert Schedule to proto
func convertScheduleToProto(s *schedules.Schedule) *pb.ScheduleInfo {
	taskContext := make(map[string]string)
	for k, v := range s.TaskContext {
		taskContext[k] = fmt.Sprintf("%v", v)
	}

	lastRunAt := ""
	if s.LastRunAt != nil {
		lastRunAt = s.LastRunAt.Format(time.RFC3339)
	}
	nextRunAt := ""
	if s.NextRunAt != nil {
		nextRunAt = s.NextRunAt.Format(time.RFC3339)
	}

	return &pb.ScheduleInfo{
		ScheduleId:         s.ID.String(),
		Name:               s.Name,
		Description:        s.Description,
		CronExpression:     s.CronExpression,
		Timezone:           s.Timezone,
		TaskQuery:          s.TaskQuery,
		TaskContext:        taskContext,
		MaxBudgetPerRunUsd: s.MaxBudgetPerRunUSD,
		TimeoutSeconds:     int32(s.TimeoutSeconds),
		Status:             s.Status,
		CreatedAt:          s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          s.UpdatedAt.Format(time.RFC3339),
		LastRunAt:          lastRunAt,
		NextRunAt:          nextRunAt,
		TotalRuns:          int32(s.TotalRuns),
		SuccessfulRuns:     int32(s.SuccessfulRuns),
		FailedRuns:         int32(s.FailedRuns),
	}
}
