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

const (
	// DefaultReviewTimeout is how long the workflow waits for human review before timing out.
	DefaultReviewTimeout = 15 * time.Minute
)

// OrchestratorWorkflow is a thin entrypoint that routes to specialized workflows.
// It performs a single decomposition step, decides the strategy, then delegates
// to an appropriate child workflow. It does not execute agents directly.
func OrchestratorWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID

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

	// Initialize control signal handler for pause/resume/cancel
	controlHandler := &ControlSignalHandler{
		WorkflowID: workflowID,
		AgentID:    "orchestrator",
		Logger:     logger,
		EmitCtx:    emitCtx,
	}
	controlHandler.Setup(ctx)

	if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
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

	// Start async title generation only if session doesn't have a title yet (non-blocking)
	// Title is generated from query, doesn't need task result, so start early for better UX
	// v2: Check SessionCtx for existing title - more reliable than history length
	titleGateVersion := workflow.GetVersion(ctx, "title_gate_v2", workflow.DefaultVersion, 1)
	needsTitle := true
	if titleGateVersion >= 1 {
		// New behavior: check SessionCtx for existing title
		if input.SessionCtx != nil {
			if existingTitle, ok := input.SessionCtx["title"].(string); ok && existingTitle != "" {
				needsTitle = false
			}
		}
	} else {
		// Old behavior: check history length (kept for replay compatibility)
		if len(input.History) > 0 {
			needsTitle = false
		}
	}
	if needsTitle {
		startAsyncTitleGeneration(ctx, input.SessionID, input.Query)
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
			if result, handled, err := routeStrategyWorkflow(ctx, input, rec.Strategy, "learning", emitCtx, controlHandler); handled {
				return result, err
			}
			logger.Warn("Learning router returned unknown strategy", "strategy", rec.Strategy)
		}
	}

	// Check pause/cancel before routing to child workflow
	if err := controlHandler.CheckPausePoint(ctx, "pre_routing"); err != nil {
		return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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
		templateFuture := workflow.ExecuteChildWorkflow(childCtx, TemplateWorkflow, templateInput)
		var templateExec workflow.Execution
		if err := templateFuture.GetChildWorkflowExecution().Get(childCtx, &templateExec); err != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to get child execution: %v", err)}, err
		}
		controlHandler.RegisterChildWorkflow(templateExec.ID)
		if err := templateFuture.Get(childCtx, &result); err != nil {
			controlHandler.UnregisterChildWorkflow(templateExec.ID)
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
			controlHandler.UnregisterChildWorkflow(templateExec.ID)
			if cfg.TemplateFallbackEnabled {
				logger.Warn("Template workflow returned unsuccessful result; falling back to AI decomposition")
				ometrics.TemplateFallbackTriggered.WithLabelValues("unsuccessful").Inc()
				ometrics.TemplateFallbackSuccess.WithLabelValues("unsuccessful").Inc()
				templateFound = false
			} else {
				scheduleStreamEnd(ctx)
				result = AddTaskContextToMetadata(result, input.Context)
				return result, nil
			}
		} else {
			controlHandler.UnregisterChildWorkflow(templateExec.ID)
			scheduleStreamEnd(ctx)
			result = AddTaskContextToMetadata(result, input.Context)
			return result, nil
		}
	}

	// Early route: skip_synthesis forces SimpleTaskWorkflow, bypassing decomposition.
	// Used by Sagasu NL workflow setup and other flows that produce structured JSON
	// and must not be rewritten by a synthesis LLM call.
	skipSynthesisVersion := workflow.GetVersion(ctx, "skip_synthesis_early_route_v1", workflow.DefaultVersion, 1)
	if skipSynthesisVersion >= 1 && GetContextBool(input.Context, "skip_synthesis") {
		logger.Info("Early route: skip_synthesis forces SimpleTaskWorkflow")

		wfID := workflow.GetInfo(ctx).WorkflowExecution.ID
		input.ParentWorkflowID = wfID

		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: wfID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgHandoffSimple(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)

		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		var result TaskResult
		childFuture := workflow.ExecuteChildWorkflow(childCtx, SimpleTaskWorkflow, input)
		if err := childFuture.Get(ctx, &result); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
		scheduleStreamEnd(ctx)
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil
	}

	// Early route: Force SwarmWorkflow bypasses decomposition entirely
	// SwarmWorkflow has its own Lead Agent that does initial planning, so orchestrator-level
	// decomposition is redundant and wastes LLM tokens.
	forceSwarmVersion := workflow.GetVersion(ctx, "force_swarm_early_route_v1", workflow.DefaultVersion, 1)
	if forceSwarmVersion >= 1 && GetContextBool(input.Context, "force_swarm") {
		logger.Info("Force swarm detected - bypassing orchestrator decomposition, Lead Agent will plan")
		swarmWfID := workflow.GetInfo(ctx).WorkflowExecution.ID
		input.ParentWorkflowID = swarmWfID

		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		var result TaskResult
		ometrics.WorkflowsStarted.WithLabelValues("SwarmWorkflow", "swarm").Inc()
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: swarmWfID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgSwarmStarted(),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)
		childFuture := workflow.ExecuteChildWorkflow(childCtx, SwarmWorkflow, input)
		var childExec workflow.Execution
		if err := childFuture.GetChildWorkflowExecution().Get(childCtx, &childExec); err != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to start SwarmWorkflow: %v", err)}, err
		}
		controlHandler.RegisterChildWorkflow(childExec.ID)

		// HITL: forward human-input signals from parent to SwarmWorkflow child.
		// Uses blocking Receive — goroutine sleeps until a signal arrives (zero polling).
		// Goroutine exits naturally when the workflow context is cancelled (child completes).
		humanInputCh := workflow.GetSignalChannel(ctx, "human-input")
		workflow.Go(ctx, func(gCtx workflow.Context) {
			for {
				var humanMsg map[string]string
				humanInputCh.Receive(gCtx, &humanMsg)
				_ = workflow.SignalExternalWorkflow(gCtx, childExec.ID, "", "human-input", humanMsg).Get(gCtx, nil)
			}
		})

		execErr := childFuture.Get(childCtx, &result)
		controlHandler.UnregisterChildWorkflow(childExec.ID)
		if execErr != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("SwarmWorkflow failed: %v", execErr)}, execErr
		}
		scheduleStreamEnd(ctx)
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil
	}

	// Early route: Force ResearchWorkflow bypasses decomposition entirely
	// ResearchWorkflow has its own query refinement + decompose pipeline, so orchestrator-level
	// decomposition is redundant and wastes LLM tokens.
	if GetContextBool(input.Context, "force_research") {
		logger.Info("Force research detected - bypassing orchestrator decomposition")

		// Inject current date for time awareness (use workflow.Now for Temporal determinism).
		// Version-gated to preserve replay for workflows started before this behavior existed.
		forceResearchDateVersion := workflow.GetVersion(ctx, "force_research_current_date_v1", workflow.DefaultVersion, 1)
		if forceResearchDateVersion >= 1 {
			if _, hasDate := input.Context["current_date"]; !hasDate {
				workflowTime := workflow.Now(ctx)
				input.Context["current_date"] = workflowTime.UTC().Format("2006-01-02")
				input.Context["current_date_human"] = workflowTime.UTC().Format("January 2, 2006")
			}
		}

		// ── HITL: Research Plan Review (optional) ──
		// Check both "require_review: true" (legacy) and "review_plan: manual" (frontend)
		requireReview := GetContextBool(input.Context, "require_review") ||
			GetContextString(input.Context, "review_plan") == "manual"
		if requireReview {
			logger.Info("HITL review enabled - generating research plan")

			reviewTimeout := DefaultReviewTimeout
			if t, ok := input.Context["review_timeout"]; ok {
				var seconds int
				switch v := t.(type) {
				case float64:
					seconds = int(v)
				case int:
					seconds = v
				case int32:
					seconds = int(v)
				case int64:
					seconds = int(v)
				case string:
					if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
						seconds = parsed
					}
				}
				if seconds > 0 {
					reviewTimeout = time.Duration(seconds) * time.Second
				}
			}
			// Safety clamp: keep review wait bounded (docs specify max 15 minutes).
			if reviewTimeout > DefaultReviewTimeout {
				reviewTimeout = DefaultReviewTimeout
			}

			// 1) Generate initial research plan via LLM + initialize Redis state
			var plan activities.ResearchPlanResult
			planInput := activities.ResearchPlanInput{
				Query:      input.Query,
				Context:    input.Context,
				WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
				SessionID:  input.SessionID,
				UserID:     input.UserID,
				TenantID:   input.TenantID,
				TTL:        reviewTimeout,
			}
			planCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 60 * time.Second,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
			})
			if err := workflow.ExecuteActivity(planCtx, constants.GenerateResearchPlanActivity, planInput).Get(ctx, &plan); err != nil {
				return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to generate research plan: %v", err)}, err
			}

			// 2) Emit SSE: plan ready (message already stripped of [RESEARCH_BRIEF] and [INTENT:...])
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
				EventType:  activities.StreamEventResearchPlanReady,
				AgentID:    "planner",
				Message:    plan.Message,
				Timestamp:  workflow.Now(ctx),
				Payload:    map[string]interface{}{"round": plan.Round, "intent": plan.Intent},
			}).Get(ctx, nil)

			// 3) Wait for user approval Signal or timeout
			sigName := "research-plan-approved-" + workflow.GetInfo(ctx).WorkflowExecution.ID
			ch := workflow.GetSignalChannel(ctx, sigName)
			timerCtx, cancelTimer := workflow.WithCancel(ctx)
			timer := workflow.NewTimer(timerCtx, reviewTimeout)

			var reviewResult activities.ResearchReviewResult
			var timedOut bool

			sel := workflow.NewSelector(ctx)
			sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {
				c.Receive(ctx, &reviewResult)
				cancelTimer() // Cancel the timer so it doesn't linger in Temporal UI
			})
			sel.AddFuture(timer, func(f workflow.Future) {
				timedOut = true
			})
			sel.Select(ctx)

			if timedOut {
				wfID := workflow.GetInfo(ctx).WorkflowExecution.ID
				logger.Warn("HITL review timed out", "workflow_id", wfID)
				_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
					WorkflowID: wfID,
					EventType:  activities.StreamEventErrorOccurred,
					AgentID:    "planner",
					Message:    activities.MsgResearchTimedOut(),
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
				// Emit WORKFLOW_COMPLETED so frontend/SSE can transition out of "running"
				_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
					WorkflowID: wfID,
					EventType:  activities.StreamEventWorkflowCompleted,
					AgentID:    "orchestrator",
					Message:    activities.MsgWorkflowCompleted(),
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil)
				return TaskResult{Success: false, ErrorMessage: "research plan review timed out"}, nil
			}

			// 4) Inject confirmed plan and research brief into context
			input.Context["confirmed_plan"] = reviewResult.FinalPlan
			input.Context["review_conversation"] = reviewResult.Conversation
			if reviewResult.ResearchBrief != "" {
				input.Context["research_brief"] = reviewResult.ResearchBrief
			}

			// 5) Emit SSE: plan approved
			_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
				EventType:  activities.StreamEventResearchPlanApproved,
				AgentID:    "planner",
				Message:    activities.MsgResearchConfirmed(),
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil)

			logger.Info("HITL review approved, continuing with research",
				"conversation_rounds", len(reviewResult.Conversation),
			)
		}
		// ── End HITL review ──

		// Force research still needs token budget preflight so downstream
		// pattern calls use budgeted execution paths and record tool costs.
		// Version-gated: in-flight workflows started before this code must not
		// encounter the new BudgetPreflight activity during replay.
		forceResearchBudgetVersion := workflow.GetVersion(ctx, "force_research_budget_v1", workflow.DefaultVersion, 1)
		if forceResearchBudgetVersion >= 1 && input.UserID != "" {
			est := EstimateTokensWithConfig(activities.DecompositionResult{
				ComplexityScore: 0.5,
				Subtasks:        []activities.Subtask{{ID: "force_research-1"}},
			}, &cfg)
			if res, err := BudgetPreflight(ctx, input, est); err == nil && res != nil {
				if !res.CanProceed {
					scheduleStreamEnd(ctx)
					out := TaskResult{Success: false, ErrorMessage: res.Reason, Metadata: map[string]interface{}{"budget_blocked": true}}
					out = AddTaskContextToMetadata(out, input.Context)
					return out, nil
				}
				if input.Context == nil {
					input.Context = map[string]interface{}{}
				}
				input.Context["budget_remaining"] = res.RemainingTaskBudget
				// ResearchWorkflow distributes budget internally; pass full remaining.
				agentMax := res.RemainingTaskBudget
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

		// Set parent workflow ID for unified event streaming (must be set before routing)
		parentWorkflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
		input.ParentWorkflowID = parentWorkflowID

		// Emit delegation event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: parentWorkflowID,
			EventType:  activities.StreamEventDelegation,
			AgentID:    "orchestrator",
			Message:    activities.MsgWorkflowRouting("ResearchWorkflow", "force_research"),
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil)

		ometrics.WorkflowsStarted.WithLabelValues("ResearchWorkflow", "force_research").Inc()

		strategiesInput := convertToStrategiesInput(input)
		var strategiesResult strategies.TaskResult
		childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
		})
		researchFuture := workflow.ExecuteChildWorkflow(childCtx, strategies.ResearchWorkflow, strategiesInput)
		var researchExec workflow.Execution
		if err := researchFuture.GetChildWorkflowExecution().Get(childCtx, &researchExec); err != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to get child execution: %v", err)}, err
		}
		controlHandler.RegisterChildWorkflow(researchExec.ID)
		execErr := researchFuture.Get(childCtx, &strategiesResult)
		controlHandler.UnregisterChildWorkflow(researchExec.ID)

		scheduleStreamEnd(ctx)

		if execErr != nil {
			controlHandler.EmitCancelledIfNeeded(ctx, execErr.Error())
			return AddTaskContextToMetadata(TaskResult{}, input.Context), execErr
		}
		result := convertFromStrategiesResult(strategiesResult)
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil
	}

	// Early route: Sagasu strategy workflows bypass decomposition entirely.
	// Scheduled tasks set research_strategy in context (e.g., "competitor_watch").
	// Without early routing, decomposition sees the raw query (e.g., "毎日ptmind.co.jpをチェックして")
	// and the LLM interprets it as a scheduling request instead of executing the research.
	//
	// 1) Decompose the task (planning + complexity)
	// Add history to context for decomposition to be context-aware
	decompContext := make(map[string]interface{})
	if input.Context != nil {
		for k, v := range input.Context {
			decompContext[k] = v
		}
	}
	// Inject current date for time awareness (use workflow.Now for Temporal determinism)
	// Only inject if not already provided (allow user override)
	if _, hasDate := decompContext["current_date"]; !hasDate {
		workflowTime := workflow.Now(ctx)
		decompContext["current_date"] = workflowTime.UTC().Format("2006-01-02")
		decompContext["current_date_human"] = workflowTime.UTC().Format("January 2, 2006")
	}
	// Inject P2P flag so decomposition prompt can include produces/consumes fields
	if cfg.P2PCoordinationEnabled {
		decompContext["p2p_enabled"] = true
	}
	// Add history for context awareness in decomposition
	if len(input.History) > 0 {
		// Convert history to a single string for the decompose endpoint
		historyLines := convertHistoryForAgent(input.History)
		decompContext["history"] = strings.Join(historyLines, "\n")
	}

	var decomp activities.DecompositionResult

	// If a single-purpose agent is specified, bypass LLM decomposition entirely.
	// AgentWorkflow is already fully deterministic and self-contained.
	agentPresent := false
	if input.Context != nil {
		if agentID, ok := input.Context["agent"].(string); ok && agentID != "" {
			agentPresent = true
			logger.Info("Agent specified - bypassing LLM decomposition", "agent_id", agentID)

			// Pass through suggested_tools from API context (e.g., Sagasu sends ["web_search"]
			// for setup conversations so the agent can resolve company names to URLs).
			var agentTools []string
			if toolsVal, ok := input.Context["suggested_tools"]; ok {
				if arr, ok := toolsVal.([]interface{}); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok {
							agentTools = append(agentTools, s)
						}
					}
				}
			}

			decomp = activities.DecompositionResult{
				Mode:              "simple",
				ComplexityScore:   0.0,
				ExecutionStrategy: "sequential",
				ConcurrencyLimit:  1,
				Subtasks: []activities.Subtask{
					{
						ID:              "task-1",
						Description:     fmt.Sprintf("Execute agent %s", agentID),
						Dependencies:    []string{},
						EstimatedTokens: 0,
						SuggestedTools:  agentTools,
						ToolParameters:  map[string]interface{}{},
					},
				},
				TotalEstimatedTokens: 0,
				TokensUsed:           0,
				InputTokens:          0,
				OutputTokens:         0,
			}
		}
	}

	// Check if a role is specified - if so, bypass LLM decomposition and create simple plan
	// Role-specific agents have their own internal multi-step logic, so orchestrator-level
	// decomposition is unnecessary and can cause conflicts (e.g., data_analytics role expects
	// to output dataResult format, which conflicts with decomposition's subtasks format).
	rolePresent := false
	if !agentPresent && input.Context != nil {
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

			// Check pause/cancel after role assignment - signal may have arrived during setup
			if err := controlHandler.CheckPausePoint(ctx, "post_role_assignment"); err != nil {
				return TaskResult{Success: false, ErrorMessage: err.Error()}, err
			}
		}
	}

	// If no role, proceed with normal LLM decomposition
	if !rolePresent && !agentPresent {
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
				Mode:              "simple",
				ComplexityScore:   0.1, // Low complexity to trigger SimpleTaskWorkflow
				ExecutionStrategy: "sequential",
				CognitiveStrategy: "",
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

		// Check pause/cancel after decomposition - signal may have arrived during the activity
		if err := controlHandler.CheckPausePoint(ctx, "post_decomposition"); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
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
				scheduleStreamEnd(ctx)
				out := TaskResult{Success: false, ErrorMessage: res.Reason, Metadata: map[string]interface{}{"budget_blocked": true}}
				out = AddTaskContextToMetadata(out, input.Context)
				return out, nil
			}
			// Pass budget info to child workflows via context
			if input.Context == nil {
				input.Context = map[string]interface{}{}
			}
			// Propagate current_date to all child workflows (if not already set)
			if _, hasDate := input.Context["current_date"]; !hasDate {
				workflowTime := workflow.Now(ctx)
				input.Context["current_date"] = workflowTime.UTC().Format("2006-01-02")
				input.Context["current_date_human"] = workflowTime.UTC().Format("January 2, 2006")
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
				scheduleStreamEnd(ctx)
				out := TaskResult{Success: false, ErrorMessage: fmt.Sprintf("approval request failed: %v", err)}
				out = AddTaskContextToMetadata(out, input.Context)
				return out, err
			} else if ar == nil || !ar.Approved {
				msg := reason
				if ar != nil && ar.Feedback != "" {
					msg = ar.Feedback
				}
				// Best-effort title generation even when approval is denied
				scheduleStreamEnd(ctx)
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

	// NOTE: force_research is handled in early routing (before decomposition) to avoid
	// redundant LLM calls. See "Early route: Force ResearchWorkflow" block above.

	// Route to BrowserUseWorkflow if browser_use role is present (unified agent loop)
	browserUseVersion := workflow.GetVersion(ctx, "browser_use_routing_v1", workflow.DefaultVersion, 2)
	if browserUseVersion >= 2 && rolePresent && input.Context != nil {
		if role, ok := input.Context["role"].(string); ok && role == "browser_use" {
			logger.Info("Routing to BrowserUseWorkflow based on browser_use role")
			// Force LLM to use browser tools (prevents hallucinating completion without tool calls)
			input.Context["force_tools"] = true
			if result, handled, err := routeStrategyWorkflow(ctx, input, "browser_use", decomp.Mode, emitCtx, controlHandler); handled {
				return result, err
			}
		}
	} else if browserUseVersion == 1 && rolePresent && input.Context != nil {
		// Legacy: v1 used ReactWorkflow
		if role, ok := input.Context["role"].(string); ok && role == "browser_use" {
			logger.Info("Routing to ReactWorkflow based on browser_use role (legacy v1)")
			input.Context["force_tools"] = true
			if result, handled, err := routeStrategyWorkflow(ctx, input, "react", decomp.Mode, emitCtx, controlHandler); handled {
				return result, err
			}
		}
	}

	// Auto-detect browser intent and assign browser_use role if not already set
	// IMPORTANT: detectBrowserIntent behavior changed significantly (simplified to JS-required domains only)
	// Use a separate changeID to track this behavior change independently from workflow routing
	autoDetectVersion := workflow.GetVersion(ctx, "browser_auto_detect_v2", workflow.DefaultVersion, 1)
	if autoDetectVersion >= 1 && !rolePresent && detectBrowserIntent(input.Query) {
		logger.Info("Auto-detected browser intent, assigning browser_use role")
		if input.Context == nil {
			input.Context = map[string]interface{}{}
		}
		input.Context["role"] = "browser_use"
		input.Context["role_auto_detected"] = true
		// Force LLM to use browser tools (prevents hallucinating completion without tool calls)
		input.Context["force_tools"] = true

		// Emit role assignment event
		_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
			EventType:  activities.StreamEventRoleAssigned,
			AgentID:    "browser_use",
			Message:    activities.MsgRoleAssigned("browser_use (auto-detected)", 10),
			Timestamp:  workflow.Now(ctx),
			Payload: map[string]interface{}{
				"role":          "browser_use",
				"auto_detected": true,
			},
		}).Get(ctx, nil)

		// Use BrowserUseWorkflow for v2+, ReactWorkflow for legacy v1
		strategy := "browser_use"
		if browserUseVersion == 1 {
			strategy = "react"
		}
		if result, handled, err := routeStrategyWorkflow(ctx, input, strategy, decomp.Mode, emitCtx, controlHandler); handled {
			return result, err
		}
	}

	// Cognitive program takes precedence if specified
	if decomp.CognitiveStrategy != "" && decomp.CognitiveStrategy != "direct" && decomp.CognitiveStrategy != "decompose" {
		if result, handled, err := routeStrategyWorkflow(ctx, input, decomp.CognitiveStrategy, decomp.Mode, emitCtx, controlHandler); handled {
			return result, err
		}
		logger.Warn("Unknown cognitive strategy; continuing routing", "strategy", decomp.CognitiveStrategy)
	}

	// Force ResearchWorkflow via context flag (user-facing via CLI and scheduled tasks)
	// Uses GetContextBool to handle both bool and string "true" (proto map<string,string> converts to string)
	if GetContextBool(input.Context, "force_research") {
		logger.Info("Forcing ResearchWorkflow via context flag")
		if result, handled, err := routeStrategyWorkflow(ctx, input, "research", decomp.Mode, emitCtx, controlHandler); handled {
			return result, err
		}
	}

	// Check if P2P is forced via context
	forceP2P := GetContextBool(input.Context, "force_p2p")
	if forceP2P {
		logger.Info("P2P coordination forced via context flag")
	}

	// NOTE: force_swarm is handled in early routing (before decomposition) to avoid
	// redundant LLM calls. See "Early route: Force SwarmWorkflow" block above.

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
		// Check pause/cancel before starting child workflow
		if err := controlHandler.CheckPausePoint(ctx, "pre_simple_workflow"); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
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
		childFuture := workflow.ExecuteChildWorkflow(childCtx, SimpleTaskWorkflow, input)
		var childExec workflow.Execution
		if err := childFuture.GetChildWorkflowExecution().Get(childCtx, &childExec); err != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to get child execution: %v", err)}, err
		}
		controlHandler.RegisterChildWorkflow(childExec.ID)
		execErr := childFuture.Get(childCtx, &result)
		controlHandler.UnregisterChildWorkflow(childExec.ID)

		// Generate title regardless of success/failure (best-effort)
		scheduleStreamEnd(ctx)

		if execErr != nil {
			// Emit workflow.cancelled if this was a cancellation (child skipped emission due to SkipSSEEmit)
			controlHandler.EmitCancelledIfNeeded(ctx, execErr.Error())
			result = AddTaskContextToMetadata(result, input.Context)
			return result, execErr
		}
		// Add task context to metadata for API exposure
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil

	case false: // Supervisor routing disabled — all multi-task flows use DAGWorkflow
		// Check pause/cancel before starting child workflow
		if err := controlHandler.CheckPausePoint(ctx, "pre_supervisor_workflow"); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
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
		childFuture := workflow.ExecuteChildWorkflow(childCtx, SupervisorWorkflow, input)
		var childExec workflow.Execution
		if err := childFuture.GetChildWorkflowExecution().Get(childCtx, &childExec); err != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to get child execution: %v", err)}, err
		}
		controlHandler.RegisterChildWorkflow(childExec.ID)
		execErr := childFuture.Get(childCtx, &result)
		controlHandler.UnregisterChildWorkflow(childExec.ID)

		// Generate title regardless of success/failure (best-effort)
		scheduleStreamEnd(ctx)

		if execErr != nil {
			// Emit workflow.cancelled if this was a cancellation (child skipped emission due to SkipSSEEmit)
			controlHandler.EmitCancelledIfNeeded(ctx, execErr.Error())
			result = AddTaskContextToMetadata(result, input.Context)
			return result, execErr
		}
		// Add task context to metadata for API exposure
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil

	default:
		// Check pause/cancel before starting child workflow
		if err := controlHandler.CheckPausePoint(ctx, "pre_dag_workflow"); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
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
		dagFuture := workflow.ExecuteChildWorkflow(childCtx, strategies.DAGWorkflow, strategiesInput)
		var dagExec workflow.Execution
		if err := dagFuture.GetChildWorkflowExecution().Get(childCtx, &dagExec); err != nil {
			return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to get child execution: %v", err)}, err
		}
		controlHandler.RegisterChildWorkflow(dagExec.ID)
		execErr := dagFuture.Get(childCtx, &strategiesResult)
		controlHandler.UnregisterChildWorkflow(dagExec.ID)

		// Generate title regardless of success/failure (best-effort)
		scheduleStreamEnd(ctx)

		if execErr != nil {
			// Emit workflow.cancelled if this was a cancellation (child skipped emission due to SkipSSEEmit)
			controlHandler.EmitCancelledIfNeeded(ctx, execErr.Error())
			out := AddTaskContextToMetadata(TaskResult{Success: false, ErrorMessage: execErr.Error()}, input.Context)
			return out, execErr
		}
		// Add task context to metadata for API exposure
		result := convertFromStrategiesResult(strategiesResult)
		result = AddTaskContextToMetadata(result, input.Context)
		return result, nil
	}
}

// startAsyncTitleGeneration fires off title generation at workflow start (non-blocking).
// This runs in parallel with the main workflow so users see titles immediately.
// The activity is best-effort with a short timeout and no retries.
func startAsyncTitleGeneration(ctx workflow.Context, sessionID, query string) {
	// Version gate for deterministic replay - new version for async behavior
	titleVersion := workflow.GetVersion(ctx, "session_title_async_v1", workflow.DefaultVersion, 1)
	if titleVersion < 1 {
		return
	}
	// Skip when sessionID is empty
	if sessionID == "" {
		return
	}

	// Fire-and-forget: run title generation in background goroutine
	workflow.Go(ctx, func(gCtx workflow.Context) {
		titleOpts := workflow.ActivityOptions{
			StartToCloseTimeout: 15 * time.Second,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 1, // Best-effort, don't retry on failure
			},
		}
		titleCtx := workflow.WithActivityOptions(gCtx, titleOpts)

		// Execute title generation - errors are ignored (best-effort)
		_ = workflow.ExecuteActivity(titleCtx, "GenerateSessionTitle", activities.GenerateSessionTitleInput{
			SessionID: sessionID,
			Query:     query,
		}).Get(titleCtx, nil)
	})
}

// scheduleStreamEnd emits the STREAM_END event to signal end of workflow processing.
// This should be called at the end of each workflow path.
func scheduleStreamEnd(ctx workflow.Context) {
	// Version gate for deterministic replay
	streamEndVersion := workflow.GetVersion(ctx, "stream_end_v1", workflow.DefaultVersion, 1)
	if streamEndVersion < 1 {
		return
	}

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

func routeStrategyWorkflow(ctx workflow.Context, input TaskInput, strategy string, mode string, emitCtx workflow.Context, controlHandler *ControlSignalHandler) (TaskResult, bool, error) {
	strategyLower := strings.ToLower(strings.TrimSpace(strategy))
	if strategyLower == "" {
		return TaskResult{}, false, nil
	}

	switch strategyLower {
	case "simple":
		// Check pause/cancel before starting child workflow
		if controlHandler != nil {
			if err := controlHandler.CheckPausePoint(ctx, "pre_simple_strategy"); err != nil {
				return TaskResult{Success: false, ErrorMessage: err.Error()}, true, err
			}
		}
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
		childFuture := workflow.ExecuteChildWorkflow(childCtx, SimpleTaskWorkflow, input)
		var childExecID string
		if controlHandler != nil {
			var childExec workflow.Execution
			if err := childFuture.GetChildWorkflowExecution().Get(childCtx, &childExec); err != nil {
				return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to get child execution: %v", err)}, true, err
			}
			childExecID = childExec.ID
			controlHandler.RegisterChildWorkflow(childExecID)
		}
		execErr := childFuture.Get(childCtx, &result)
		if controlHandler != nil && childExecID != "" {
			controlHandler.UnregisterChildWorkflow(childExecID)
		}

		// Generate title regardless of success/failure (best-effort)
		scheduleStreamEnd(ctx)

		if execErr != nil {
			// Emit workflow.cancelled if this was a cancellation (child skipped emission due to SkipSSEEmit)
			if controlHandler != nil {
				controlHandler.EmitCancelledIfNeeded(ctx, execErr.Error())
			}
			result = AddTaskContextToMetadata(result, input.Context)
			return result, true, execErr
		}
		// Add task context to metadata for API exposure
		result = AddTaskContextToMetadata(result, input.Context)
		return result, true, nil
	case "react", "exploratory", "research", "scientific", "ads_research", "browser_use", "competitor_watch", "morning_brief", "seo_rank":
		// Check pause/cancel before starting child workflow
		if controlHandler != nil {
			if err := controlHandler.CheckPausePoint(ctx, "pre_"+strategyLower+"_workflow"); err != nil {
				return TaskResult{Success: false, ErrorMessage: err.Error()}, true, err
			}
		}
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
		case "browser_use":
			wfName = "BrowserUseWorkflow"
			wfFunc = strategies.BrowserUseWorkflow
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
		strategyFuture := workflow.ExecuteChildWorkflow(childCtx, wfFunc, strategiesInput)
		var strategyExecID string
		if controlHandler != nil {
			var strategyExec workflow.Execution
			if err := strategyFuture.GetChildWorkflowExecution().Get(childCtx, &strategyExec); err != nil {
				return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Failed to get child execution: %v", err)}, true, err
			}
			strategyExecID = strategyExec.ID
			controlHandler.RegisterChildWorkflow(strategyExecID)
		}
		execErr := strategyFuture.Get(childCtx, &strategiesResult)
		if controlHandler != nil && strategyExecID != "" {
			controlHandler.UnregisterChildWorkflow(strategyExecID)
		}

		// Generate title regardless of success/failure (best-effort)
		scheduleStreamEnd(ctx)

		if execErr != nil {
			// Emit workflow.cancelled if this was a cancellation (child skipped emission due to SkipSSEEmit)
			if controlHandler != nil {
				controlHandler.EmitCancelledIfNeeded(ctx, execErr.Error())
			}
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

// detectBrowserIntent checks if the query requires browser automation.
// Only returns true for sites that REQUIRE JavaScript rendering.
// Normal URLs go through decomposition which chooses web_fetch/web_search.
// Users can explicitly set role=browser_use for interactive tasks.
func detectBrowserIntent(query string) bool {
	q := strings.ToLower(query)

	// Sites that REQUIRE browser automation (JavaScript rendering)
	// These sites cannot be fetched with simple HTTP - they need a real browser
	jsRequiredDomains := []string{
		"weixin.qq.com",    // WeChat articles (heavy JS)
		"mp.weixin.qq.com", // WeChat public accounts
		// Add other JS-required domains as discovered
	}
	for _, domain := range jsRequiredDomains {
		if strings.Contains(q, domain) {
			return true
		}
	}

	// All other URLs (including http://, https://, .com, .io, etc.)
	// go through normal decomposition which will choose appropriate tools
	// (web_fetch, web_search, research workflow, etc.)
	return false
}
