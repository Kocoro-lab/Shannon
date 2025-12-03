package workflows

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/roles"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/templates"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/opts"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
)

// OrchestratorWorkflow is a thin entrypoint that routes to specialized workflows.
// It performs a single decomposition step, decides the strategy, then delegates
// to an appropriate child workflow. It does not execute agents directly.
func OrchestratorWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting OrchestratorWorkflow",
		"query", input.Query,
		"user_id", input.UserID,
		"session_id", input.SessionID,
	)

	// Emit workflow started event with task context
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
		EventType:  activities.StreamEventWorkflowStarted,
		AgentID:    "orchestrator",
		Message:    activities.MsgWorkflowStarted(),
		Timestamp:  workflow.Now(ctx),
		Payload: map[string]interface{}{
			"task_context": input.Context, // Include context for frontend
		},
	}).Get(ctx, nil); err != nil {
		logger.Warn("Failed to emit workflow started event", "error", err)
	}

	// Conservative activity options for fast planning
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})

	// (Optional) Load router/approval config
	var cfg activities.WorkflowConfig
	if err := workflow.ExecuteActivity(actx, activities.GetWorkflowConfig).Get(ctx, &cfg); err != nil {
		// Continue with defaults on failure
	}
	simpleThreshold := cfg.SimpleThreshold
	if simpleThreshold == 0 {
		simpleThreshold = 0.3
	}

	templateVersionGate := workflow.GetVersion(ctx, "template_router_v1", workflow.DefaultVersion, 1)
	var templateEntry templates.Entry
	templateFound := false
	templateRequested := false
	var requestedTemplateName, requestedTemplateVersion string
	if templateVersionGate >= 1 {
		requestedTemplateName, requestedTemplateVersion = extractTemplateRequest(input)
		if requestedTemplateName != "" {
			templateRequested = true
			if entry, ok := TemplateRegistry().Find(requestedTemplateName, requestedTemplateVersion); ok {
				templateEntry = entry
				templateFound = true
				if input.Context == nil {
					input.Context = map[string]interface{}{}
				}
				input.Context["template_resolved"] = entry.Key
				input.Context["template_content_hash"] = entry.ContentHash
			}
		}
		if input.DisableAI && !templateFound {
			msg := fmt.Sprintf("requested template '%s' not found", requestedTemplateName)
			if requestedTemplateName == "" {
				msg = "template execution required but no template specified"
			}
			logger.Error("Template requirement cannot be satisfied",
				"template", requestedTemplateName,
				"version", requestedTemplateVersion,
			)
			return TaskResult{
				Success:      false,
				ErrorMessage: msg,
				Metadata: map[string]interface{}{
					"template_requested": requestedTemplateName,
					"template_version":   requestedTemplateVersion,
				},
			}, nil
		}
		if templateRequested && !templateFound {
			logger.Warn("Requested template not found; continuing with heuristic routing",
				"template", requestedTemplateName,
				"version", requestedTemplateVersion,
			)
		}
	}

	learningVersionGate := workflow.GetVersion(ctx, "learning_router_v1", workflow.DefaultVersion, 1)
	if learningVersionGate >= 1 && !templateFound && cfg.ContinuousLearningEnabled {
		if rec, err := recommendStrategy(ctx, input); err == nil && rec != nil && rec.Strategy != "" {
			if input.Context == nil {
				input.Context = map[string]interface{}{}
			}
			input.Context["learning_strategy"] = rec.Strategy
			input.Context["learning_confidence"] = rec.Confidence
			if rec.Source != "" {
				input.Context["learning_source"] = rec.Source
			}
			if result, handled, err := routeStrategyWorkflow(ctx, input, rec.Strategy, "learning", emitCtx); handled {
				return result, err
			}
			logger.Warn("Learning router returned unknown strategy", "strategy", rec.Strategy)
		}
	}

	// 1) Decompose the task (planning + complexity)
	if templateFound {
		input.TemplateName = templateEntry.Template.Name
		input.TemplateVersion = templateEntry.Template.Version

		templateInput := TemplateWorkflowInput{
			Task:         input,
			TemplateKey:  templateEntry.Key,
			TemplateHash: templateEntry.ContentHash,
		}

		ometrics.WorkflowsStarted.WithLabelValues("TemplateWorkflow", "template").Inc()
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgHandoffTemplate(templateEntry.Template.Name),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)

		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		var result TaskResult
		if err := workflow.ExecuteChildWorkflow(childCtx, TemplateWorkflow, templateInput).Get(childCtx, &result); err != nil {
			if cfg.TemplateFallbackEnabled {
				logger.Warn("Template workflow failed; falling back to AI decomposition", "error", err)
				ometrics.TemplateFallbackTriggered.WithLabelValues("error").Inc()
				ometrics.TemplateFallbackSuccess.WithLabelValues("error").Inc()
				// Allow router to proceed to decomposition path below
				templateFound = false
			} else {
				result = AddTaskContextToMetadata(result, input.Context)
				return result, err
			}
		} else if !result.Success {
			if cfg.TemplateFallbackEnabled {
				logger.Warn("Template workflow returned unsuccessful result; falling back to AI decomposition")
				ometrics.TemplateFallbackTriggered.WithLabelValues("unsuccessful").Inc()
				ometrics.TemplateFallbackSuccess.WithLabelValues("unsuccessful").Inc()
				templateFound = false
			} else {
				scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)
				result = AddTaskContextToMetadata(result, input.Context)
				return result, nil
			}
		} else {
			scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)
			result = AddTaskContextToMetadata(result, input.Context)
			return result, nil
		}
	}

	// 1) Decompose the task (planning + complexity)
	// Add history to context for decomposition to be context-aware
	decompContext := make(map[string]interface{})
	if input.Context != nil {
		for k, v := range input.Context {
			decompContext[k] = v
		}
	}
	// Add history for context awareness in decomposition
	if len(input.History) > 0 {
		// Convert history to a single string for the decompose endpoint
		historyLines := convertHistoryForAgent(input.History)
		decompContext["history"] = strings.Join(historyLines, "\n")
	}

	var decomp activities.DecompositionResult

	// Check if a role is specified - if so, bypass LLM decomposition and create simple plan
	// Role-specific agents have their own internal multi-step logic, so orchestrator-level
	// decomposition is unnecessary and can cause conflicts (e.g., data_analytics role expects
	// to output dataResult format, which conflicts with decomposition's subtasks format).
	rolePresent := false
	if input.Context != nil {
		if role, ok := input.Context["role"].(string); ok && role != "" {
			rolePresent = true
			roleTools := roles.AllowedTools(role)
			logger.Info("Role specified - bypassing LLM decomposition", "role", role, "tool_count", len(roleTools))

			// Emit ROLE_ASSIGNED event
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
				EventType:  activities.StreamEventRoleAssigned,
				AgentID:    role,
				Message:    activities.MsgRoleAssigned(role, len(roleTools)),
				Timestamp:  workflow.Now(ctx),
				Payload: map[string]interface{}{
					"role":       role,
					"tools":      roleTools,
					"tool_count": len(roleTools),
				},
			}).Get(ctx, nil)

			// Create a simple single-subtask plan
			decomp = activities.DecompositionResult{
				Mode:              "simple",
				ComplexityScore:   0.5,
				ExecutionStrategy: "sequential",
				ConcurrencyLimit:  1,
				Subtasks: []activities.Subtask{
					{
						ID:              "task-1",
						Description:     input.Query,
						Dependencies:    []string{},
						EstimatedTokens: 5000,
						SuggestedTools:  append([]string(nil), roleTools...),
						ToolParameters:  map[string]interface{}{}, // Agent constructs from context
					},
				},
				TotalEstimatedTokens: 5000,
				TokensUsed:           0, // No LLM call for decomposition
				InputTokens:          0,
				OutputTokens:         0,
			}
		}
	}

	// If no role, proceed with normal LLM decomposition
	if !rolePresent {
		// Emit "Understanding your request" before decomposition
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			EventType:  activities.StreamEventProgress,
			AgentID:    "planner",
			Message:    activities.MsgUnderstandingRequest(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)

		if err := workflow.ExecuteActivity(actx, constants.DecomposeTaskActivity, activities.DecompositionInput{
			Query:          input.Query,
			Context:        decompContext,
			AvailableTools: nil, // Let llm-service derive tools from registry + role preset
		}).Get(ctx, &decomp); err != nil {
			logger.Warn("Task decomposition failed, falling back to SimpleTaskWorkflow", "error", err)
			// Emit warning event
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
				EventType:  activities.StreamEventProgress,
				AgentID:    "planner",
				Message:    activities.MsgDecompositionFailed(),
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)

			// Create fallback decomposition for SimpleTaskWorkflow
			decomp = activities.DecompositionResult{
				Mode:                 "simple",
				ComplexityScore:      0.1, // Low complexity to trigger SimpleTaskWorkflow
				ExecutionStrategy:    "sequential",
				CognitiveStrategy:    "",
				Subtasks: []activities.Subtask{
					{
						ID:           "1",
						Description:  input.Query,
						TaskType:     "generic",
						Dependencies: []string{},
					},
				},
				TotalEstimatedTokens: 5000,
				TokensUsed:           0, // No LLM call for fallback decomposition
				InputTokens:          0,
				OutputTokens:         0,
			}
			logger.Info("Created fallback decomposition for simple execution", "query", input.Query)
		}
	}

	// Record decomposition usage if provided
	if decomp.TokensUsed > 0 || decomp.InputTokens > 0 || decomp.OutputTokens > 0 {
		inTok := decomp.InputTokens
		outTok := decomp.OutputTokens
		if inTok == 0 && outTok == 0 && decomp.TokensUsed > 0 {
			inTok = int(float64(decomp.TokensUsed) * 0.6)
			outTok = decomp.TokensUsed - inTok
		}
		wid := workflow.GetInfo(ctx).WorkflowExecution.ID
		recCtx := opts.WithTokenRecordOptions(ctx)
		_ = workflow.ExecuteActivity(recCtx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
			UserID:       input.UserID,
			SessionID:    input.SessionID,
			TaskID:       wid,
			AgentID:      "decompose",
			Model:        decomp.ModelUsed,
			Provider:     decomp.Provider,
			InputTokens:  inTok,
			OutputTokens: outTok,
			Metadata:     map[string]interface{}{"phase": "decompose"},
		}).Get(ctx, nil)
	}

	logger.Info("Routing decision",
		"complexity", decomp.ComplexityScore,
		"mode", decomp.Mode,
		"num_subtasks", len(decomp.Subtasks),
		"cognitive_strategy", decomp.CognitiveStrategy,
	)

	// Emit a human-friendly plan summary with payload (steps + deps)
	{
		steps := make([]map[string]interface{}, 0, len(decomp.Subtasks))
		deps := make([]map[string]string, 0, 4)
		for _, st := range decomp.Subtasks {
			steps = append(steps, map[string]interface{}{
				"id":   st.ID,
				"name": st.Description,
				"type": st.TaskType,
			})
			for _, d := range st.Dependencies {
				deps = append(deps, map[string]string{"from": d, "to": st.ID})
			}
		}
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			EventType:  activities.StreamEventProgress,
			AgentID:    "planner",
			Message:    activities.MsgPlanCreated(len(steps)),
			Timestamp:  workflow.Now(ctx),
			Payload:    map[string]interface{}{"plan": steps, "deps": deps},
		}).Get(ctx, nil)
	}

	// Propagate the plan to child workflows to avoid a second decompose
	input.PreplannedDecomposition = &decomp

	// 1.5) Budget preflight (estimate based on plan)
	if input.UserID != "" { // Only check when we have a user scope
		est := EstimateTokensWithConfig(decomp, &cfg)
		if res, err := BudgetPreflight(ctx, input, est); err == nil && res != nil {
			if !res.CanProceed {
				// Best-effort title generation even when budget preflight blocks execution
				scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)
				out := TaskResult{Success: false, ErrorMessage: res.Reason, Metadata: map[string]interface{}{"budget_blocked": true}}
				out = AddTaskContextToMetadata(out, input.Context)
				return out, nil
			}
			// Pass budget info to child workflows via context
			if input.Context == nil {
				input.Context = map[string]interface{}{}
			}
			input.Context["budget_remaining"] = res.RemainingTaskBudget
			n := len(decomp.Subtasks)
			if n == 0 {
				n = 1
			}
			agentMax := res.RemainingTaskBudget / n
			// Optional clamp: environment or request context can cap per-agent budget
			if v := os.Getenv("TOKEN_BUDGET_PER_AGENT"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 && n < agentMax {
					agentMax = n
				}
			}
			if capv, ok := input.Context["token_budget_per_agent"].(int); ok && capv > 0 && capv < agentMax {
				agentMax = capv
			}
			if capv, ok := input.Context["token_budget_per_agent"].(float64); ok && capv > 0 && int(capv) < agentMax {
				agentMax = int(capv)
			}
			input.Context["budget_agent_max"] = agentMax
		}
	}

	// 1.6) Approval gate (optional, config-driven or explicit request)
	if cfg.ApprovalEnabled {
		// Override policy thresholds via config if provided
		// Note: current CheckApprovalPolicy uses default thresholds; we gate invocation here
	}
	if cfg.ApprovalEnabled || input.RequireApproval {
		// Build policy from config
		pol := activities.ApprovalPolicy{
			ComplexityThreshold: cfg.ApprovalComplexityThreshold,
			TokenBudgetExceeded: false,
			RequireForTools:     cfg.ApprovalDangerousTools,
		}
		if need, reason := CheckApprovalPolicyWith(pol, input, decomp); need {
			if ar, err := RequestAndWaitApproval(ctx, input, reason); err != nil {
				// Best-effort title generation even on approval flow errors
				scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)
				out := TaskResult{Success: false, ErrorMessage: fmt.Sprintf("approval request failed: %v", err)}
				out = AddTaskContextToMetadata(out, input.Context)
				return out, err
			} else if ar == nil || !ar.Approved {
				msg := reason
				if ar != nil && ar.Feedback != "" {
					msg = ar.Feedback
				}
				// Best-effort title generation even when approval is denied
				scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)
				out := TaskResult{Success: false, ErrorMessage: fmt.Sprintf("approval denied: %s", msg)}
				out = AddTaskContextToMetadata(out, input.Context)
				return out, nil
			}
		}
	}

	// 2) Routing rules (simple, cognitive, supervisor, dag)
	// Treat as simple ONLY when truly one-shot (no tools, no deps) AND below threshold
	needsTools := false
	for _, st := range decomp.Subtasks {
		if len(st.SuggestedTools) > 0 || len(st.Dependencies) > 0 || len(st.Consumes) > 0 || len(st.Produces) > 0 {
			needsTools = true
			break
		}
		if st.ToolParameters != nil && len(st.ToolParameters) > 0 {
			needsTools = true
			break
		}
	}
	if rolePresent {
		needsTools = false
	}
	simpleByShape := len(decomp.Subtasks) == 0 || (len(decomp.Subtasks) == 1 && !needsTools)
	isSimple := decomp.ComplexityScore < simpleThreshold && simpleByShape

	// Set parent workflow ID for child workflows to use for unified event streaming
	// MUST be set BEFORE any strategy workflow routing to ensure events go to parent
	parentWorkflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	input.ParentWorkflowID = parentWorkflowID

	// Cognitive program takes precedence if specified
	if decomp.CognitiveStrategy != "" && decomp.CognitiveStrategy != "direct" && decomp.CognitiveStrategy != "decompose" {
		if result, handled, err := routeStrategyWorkflow(ctx, input, decomp.CognitiveStrategy, decomp.Mode, emitCtx); handled {
			return result, err
		}
		logger.Warn("Unknown cognitive strategy; continuing routing", "strategy", decomp.CognitiveStrategy)
	}

	// Force ResearchWorkflow via context flag (user-facing via CLI)
	if v, ok := input.Context["force_research"]; ok {
		if b, ok := v.(bool); ok && b {
			logger.Info("Forcing ResearchWorkflow via context flag (test mode)")
			if result, handled, err := routeStrategyWorkflow(ctx, input, "research", decomp.Mode, emitCtx); handled {
				return result, err
			}
		}
	}

	// Check if P2P is forced via context
	forceP2P := false
	if v, ok := input.Context["force_p2p"]; ok {
		if b, ok := v.(bool); ok && b {
			forceP2P = true
			logger.Info("P2P coordination forced via context flag")
		}
		// Also check string values for flexibility
		if s, ok := v.(string); ok && (s == "true" || s == "1") {
			forceP2P = true
			logger.Info("P2P coordination forced via context flag")
		}
	}

	// Supervisor heuristic: very large plans, explicit dependencies, or forced P2P
	hasDeps := forceP2P // Start with force flag
	if !hasDeps {
		for _, st := range decomp.Subtasks {
			if len(st.Dependencies) > 0 || len(st.Consumes) > 0 {
				hasDeps = true
				break
			}
		}
	}

	switch {
	case isSimple && !forceP2P:
		// Keep simple path lightweight as a child for isolation (unless P2P is forced)
		var result TaskResult
		ometrics.WorkflowsStarted.WithLabelValues("SimpleTaskWorkflow", "simple").Inc()
		// Emit delegation event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: parentWorkflowID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgHandoffSimple(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)

		// Pass suggested tools from decomposition to SimpleTaskWorkflow
		if len(decomp.Subtasks) > 0 && len(decomp.Subtasks[0].SuggestedTools) > 0 {
			input.SuggestedTools = decomp.Subtasks[0].SuggestedTools
			input.ToolParameters = decomp.Subtasks[0].ToolParameters
		}

		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		execErr := workflow.ExecuteChildWorkflow(childCtx, SimpleTaskWorkflow, input).Get(childCtx, &result)

		// Generate title regardless of success/failure (best-effort)
		scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)

		if execErr != nil {
			result = AddTaskContextToMetadata(result, input.Context)
			return result, execErr
		}
		// Add task context to metadata for API exposure
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil

	case len(decomp.Subtasks) > 5 || hasDeps:
		var result TaskResult
		ometrics.WorkflowsStarted.WithLabelValues("SupervisorWorkflow", "complex").Inc()
		// Emit delegation event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: parentWorkflowID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgHandoffSupervisor(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		execErr := workflow.ExecuteChildWorkflow(childCtx, SupervisorWorkflow, input).Get(childCtx, &result)

		// Generate title regardless of success/failure (best-effort)
		scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)

		if execErr != nil {
			result = AddTaskContextToMetadata(result, input.Context)
			return result, execErr
		}
		// Add task context to metadata for API exposure
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil

	default:
		// Standard DAG strategy (fan-out/fan-in)
		ometrics.WorkflowsStarted.WithLabelValues("DAGWorkflow", "standard").Inc()
		// Emit delegation event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: parentWorkflowID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgHandoffTeamPlan(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		strategiesInput := convertToStrategiesInput(input)
		var strategiesResult strategies.TaskResult
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		execErr := workflow.ExecuteChildWorkflow(childCtx, strategies.DAGWorkflow, strategiesInput).Get(childCtx, &strategiesResult)

		// Generate title regardless of success/failure (best-effort)
		scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)

		if execErr != nil {
			out := AddTaskContextToMetadata(TaskResult{Success: false, ErrorMessage: execErr.Error()}, input.Context)
			return out, execErr
		}
		// Add task context to metadata for API exposure
		result := convertFromStrategiesResult(strategiesResult)
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil
	}
}

// scheduleSessionTitleGeneration schedules the session title generation activity.
// This is called after the first task completes, regardless of success or failure.
// The activity is best-effort with a short timeout and no retries.
func scheduleSessionTitleGeneration(ctx workflow.Context, sessionID, query string) {
	// Version gate for deterministic replay
	titleVersion := workflow.GetVersion(ctx, "session_title_v1", workflow.DefaultVersion, 1)
	if titleVersion < 1 {
		return
	}
	// Skip when sessionID is empty
	if sessionID == "" {
		return
	}

	// Use a short timeout and no retries for best-effort execution
	// 15s timeout provides buffer for LLM call (~1-2s with fix) + Redis save (~0.5s)
	titleOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1, // Best-effort, don't retry on failure
		},
	}
	titleCtx := workflow.WithActivityOptions(ctx, titleOpts)

	// Execute and wait (non-detached) to ensure it runs even if workflow fails
	// Ignore errors since this is best-effort
	_ = workflow.ExecuteActivity(titleCtx, "GenerateSessionTitle", activities.GenerateSessionTitleInput{
		SessionID: sessionID,
		Query:     query,
	}).Get(titleCtx, nil)

	// Emit STREAM_END to explicitly signal the end of post-completion processing
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
		EventType:  activities.StreamEventStreamEnd,
		AgentID:    "orchestrator",
		Message:    activities.MsgStreamEnd(),
		Timestamp:  workflow.Now(ctx),
	}).Get(emitCtx, nil)
}

// convertToStrategiesInput converts workflows.TaskInput to strategies.TaskInput
func convertToStrategiesInput(input TaskInput) strategies.TaskInput {
	// Convert History messages
	history := make([]strategies.Message, len(input.History))
	for i, msg := range input.History {
		history[i] = strategies.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		}
	}

	return strategies.TaskInput{
		Query:                   input.Query,
		UserID:                  input.UserID,
		TenantID:                input.TenantID,
		SessionID:               input.SessionID,
		Context:                 input.Context,
		Mode:                    input.Mode,
		TemplateName:            input.TemplateName,
		TemplateVersion:         input.TemplateVersion,
		DisableAI:               input.DisableAI,
		History:                 history,
		SessionCtx:              input.SessionCtx,
		RequireApproval:         input.RequireApproval,
		ApprovalTimeout:         input.ApprovalTimeout,
		BypassSingleResult:      input.BypassSingleResult,
		ParentWorkflowID:        input.ParentWorkflowID,
		PreplannedDecomposition: input.PreplannedDecomposition,
	}
}

// convertFromStrategiesResult converts strategies.TaskResult to workflows.TaskResult
func convertFromStrategiesResult(result strategies.TaskResult) TaskResult {
	return TaskResult{
		Result:       result.Result,
		Success:      result.Success,
		TokensUsed:   result.TokensUsed,
		ErrorMessage: result.ErrorMessage,
		Metadata:     result.Metadata,
	}
}

func extractTemplateRequest(input TaskInput) (string, string) {
	name := strings.TrimSpace(input.TemplateName)
	version := strings.TrimSpace(input.TemplateVersion)

	if name == "" && input.Context != nil {
		if v, ok := input.Context["template"].(string); ok {
			name = strings.TrimSpace(v)
		}
		// Accept legacy/alias key: template_name
		if name == "" {
			if v2, ok2 := input.Context["template_name"].(string); ok2 {
				name = strings.TrimSpace(v2)
			}
		}
	}
	if version == "" && input.Context != nil {
		if v, ok := input.Context["template_version"].(string); ok {
			version = strings.TrimSpace(v)
		}
	}
	return name, version
}

func routeStrategyWorkflow(ctx workflow.Context, input TaskInput, strategy string, mode string, emitCtx workflow.Context) (TaskResult, bool, error) {
	strategyLower := strings.ToLower(strings.TrimSpace(strategy))
	if strategyLower == "" {
		return TaskResult{}, false, nil
	}

	switch strategyLower {
	case "simple":
		var result TaskResult
		ometrics.WorkflowsStarted.WithLabelValues("SimpleTaskWorkflow", mode).Inc()
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgWorkflowRouting("simple", mode),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		execErr := workflow.ExecuteChildWorkflow(childCtx, SimpleTaskWorkflow, input).Get(childCtx, &result)

		// Generate title regardless of success/failure (best-effort)
		scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)

		if execErr != nil {
			result = AddTaskContextToMetadata(result, input.Context)
			return result, true, execErr
		}
		// Add task context to metadata for API exposure
		result = AddTaskContextToMetadata(result, input.Context)
		return result, true, nil
	case "react", "exploratory", "research", "scientific":
		var wfName string
		var wfFunc interface{}
		switch strategyLower {
		case "react":
			wfName = "ReactWorkflow"
			wfFunc = strategies.ReactWorkflow
		case "exploratory":
			wfName = "ExploratoryWorkflow"
			wfFunc = strategies.ExploratoryWorkflow
		case "research":
			wfName = "ResearchWorkflow"
			wfFunc = strategies.ResearchWorkflow
		case "scientific":
			wfName = "ScientificWorkflow"
			wfFunc = strategies.ScientificWorkflow
		}

		strategiesInput := convertToStrategiesInput(input)
		var strategiesResult strategies.TaskResult
		ometrics.WorkflowsStarted.WithLabelValues(wfName, mode).Inc()
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgWorkflowRouting(wfName, mode),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		execErr := workflow.ExecuteChildWorkflow(childCtx, wfFunc, strategiesInput).Get(childCtx, &strategiesResult)

		// Generate title regardless of success/failure (best-effort)
		scheduleSessionTitleGeneration(ctx, input.SessionID, input.Query)

		if execErr != nil {
			res := AddTaskContextToMetadata(TaskResult{}, input.Context)
			return res, true, execErr
		}
		// Add task context to metadata for API exposure
		result := convertFromStrategiesResult(strategiesResult)
		result = AddTaskContextToMetadata(result, input.Context)
		return result, true, nil
	default:
		return TaskResult{}, false, nil
	}
}

func recommendStrategy(ctx workflow.Context, input TaskInput) (*activities.RecommendStrategyOutput, error) {
	startTime := workflow.Now(ctx)

	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	})

	var rec activities.RecommendStrategyOutput
	err := workflow.ExecuteActivity(actx, activities.RecommendWorkflowStrategy, activities.RecommendStrategyInput{
		SessionID: input.SessionID,
		UserID:    input.UserID,
		TenantID:  input.TenantID,
		Query:     input.Query,
	}).Get(ctx, &rec)

	// Record metrics (fire-and-forget)
	metricsCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	latency := workflow.Now(ctx).Sub(startTime).Seconds()
	strategy := "none"
	source := "none"
	confidence := 0.0
	success := false

	if err == nil && rec.Strategy != "" {
		strategy = rec.Strategy
		source = rec.Source
		confidence = rec.Confidence
		success = true
	}

	workflow.ExecuteActivity(
		metricsCtx,
		"RecordLearningRouterMetrics",
		map[string]interface{}{
			"latency_seconds": latency,
			"strategy":        strategy,
			"source":          source,
			"confidence":      confidence,
			"success":         success,
		},
	)

	if err != nil {
		return nil, err
	}
	return &rec, nil
}
