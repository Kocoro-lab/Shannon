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
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/templates"
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

	// Emit workflow started event
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
		EventType:  activities.StreamEventWorkflowStarted,
		AgentID:    "orchestrator",
		Message:    "Task processing started",
		Timestamp:  workflow.Now(ctx),
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
			Message:    fmt.Sprintf("Handing off to template (%s)", templateEntry.Template.Name),
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
				return result, err
			}
		} else if !result.Success {
			if cfg.TemplateFallbackEnabled {
				logger.Warn("Template workflow returned unsuccessful result; falling back to AI decomposition")
				ometrics.TemplateFallbackTriggered.WithLabelValues("unsuccessful").Inc()
				ometrics.TemplateFallbackSuccess.WithLabelValues("unsuccessful").Inc()
				templateFound = false
			} else {
				return result, nil
			}
		} else {
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
	if err := workflow.ExecuteActivity(actx, constants.DecomposeTaskActivity, activities.DecompositionInput{
		Query:          input.Query,
		Context:        decompContext,
		AvailableTools: []string{},
	}).Get(ctx, &decomp); err != nil {
		logger.Error("Task decomposition failed", "error", err)
		// Emit error event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			EventType:  activities.StreamEventErrorOccurred,
			Message:    "Couldn't create a plan: " + err.Error(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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
			Message:    fmt.Sprintf("Created a plan with %d steps", len(steps)),
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
				return TaskResult{Success: false, ErrorMessage: res.Reason, Metadata: map[string]interface{}{"budget_blocked": true}}, nil
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
				return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("approval request failed: %v", err)}, err
			} else if ar == nil || !ar.Approved {
				msg := reason
				if ar != nil && ar.Feedback != "" {
					msg = ar.Feedback
				}
				return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("approval denied: %s", msg)}, nil
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
	simpleByShape := len(decomp.Subtasks) == 0 || (len(decomp.Subtasks) == 1 && !needsTools)
	isSimple := decomp.ComplexityScore < simpleThreshold && simpleByShape

	// Cognitive program takes precedence if specified
	if decomp.CognitiveStrategy != "" && decomp.CognitiveStrategy != "direct" && decomp.CognitiveStrategy != "decompose" {
		if result, handled, err := routeStrategyWorkflow(ctx, input, decomp.CognitiveStrategy, decomp.Mode, emitCtx); handled {
			return result, err
		}
		logger.Warn("Unknown cognitive strategy; continuing routing", "strategy", decomp.CognitiveStrategy)
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

	// Set parent workflow ID for child workflows to use for unified event streaming
	parentWorkflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	input.ParentWorkflowID = parentWorkflowID

	switch {
	case isSimple && !forceP2P:
		// Keep simple path lightweight as a child for isolation (unless P2P is forced)
		var result TaskResult
		ometrics.WorkflowsStarted.WithLabelValues("SimpleTaskWorkflow", "simple").Inc()
		// Emit delegation event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: parentWorkflowID,
			EventType:  activities.StreamEventDelegation,
			Message:    "Handing off to simple task",
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
		if err := workflow.ExecuteChildWorkflow(childCtx, SimpleTaskWorkflow, input).Get(childCtx, &result); err != nil {
			return result, err
		}
		return result, nil

	case len(decomp.Subtasks) > 5 || hasDeps:
		var result TaskResult
		ometrics.WorkflowsStarted.WithLabelValues("SupervisorWorkflow", "complex").Inc()
		// Emit delegation event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: parentWorkflowID,
			EventType:  activities.StreamEventDelegation,
			Message:    "Handing off to supervisor",
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		if err := workflow.ExecuteChildWorkflow(childCtx, SupervisorWorkflow, input).Get(childCtx, &result); err != nil {
			return result, err
		}
		return result, nil

	default:
		// Standard DAG strategy (fan-out/fan-in)
		ometrics.WorkflowsStarted.WithLabelValues("DAGWorkflow", "standard").Inc()
		// Emit delegation event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: parentWorkflowID,
			EventType:  activities.StreamEventDelegation,
			Message:    "Handing off to team plan",
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		strategiesInput := convertToStrategiesInput(input)
		var strategiesResult strategies.TaskResult
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		if err := workflow.ExecuteChildWorkflow(childCtx, strategies.DAGWorkflow, strategiesInput).Get(childCtx, &strategiesResult); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
		return convertFromStrategiesResult(strategiesResult), nil
	}
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
			Message:    "Routing to SimpleTaskWorkflow (learning)",
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		if err := workflow.ExecuteChildWorkflow(childCtx, SimpleTaskWorkflow, input).Get(childCtx, &result); err != nil {
			return result, true, err
		}
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
			Message:    fmt.Sprintf("Routing to %s (%s)", wfName, mode),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		if err := workflow.ExecuteChildWorkflow(childCtx, wfFunc, strategiesInput).Get(childCtx, &strategiesResult); err != nil {
			return TaskResult{}, true, err
		}
		return convertFromStrategiesResult(strategiesResult), true, nil
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
