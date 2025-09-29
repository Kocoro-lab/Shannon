package workflows

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
)

// Note: parseNumericValue is defined in dag_workflow.go and shared across workflows

// MailboxMessage is a minimal deterministic message record used by SupervisorWorkflow.
type MailboxMessage struct {
	From    string
	To      string
	Role    string
	Content string
}

// SendMailboxMessage helper to signal another workflow's mailbox.
func SendMailboxMessage(ctx workflow.Context, targetWorkflowID string, msg MailboxMessage) error {
	return workflow.SignalExternalWorkflow(ctx, targetWorkflowID, "", "mailbox_v1", msg).Get(ctx, nil)
}

// SupervisorWorkflow orchestrates sub-teams using child workflows.
// v1: decompose → delegate subtasks to SimpleTaskWorkflow children → synthesize.
func SupervisorWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting SupervisorWorkflow", "query", input.Query, "user_id", input.UserID)

	// Capture workflow start time for duration tracking
	workflowStartTime := workflow.Now(ctx)

	// ENTERPRISE TIMEOUT STRATEGY:
	// - No overall workflow timeout (complex tasks may take hours/days)
	// - Per-task retry limits (3 max) prevent infinite loops
	// - Failure threshold (50%+1) provides intelligent abort criteria
	// - See docs/timeout-retry-strategy.md for full details

	// Determine workflow ID for event streaming
	// Use parent workflow ID if this is a child workflow, otherwise use own ID
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}

	// Emit WORKFLOW_STARTED event
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowStarted,
		AgentID:    "supervisor",
		Message:    "SupervisorWorkflow started",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil); err != nil {
		logger.Warn("Failed to emit workflow started event", "error", err)
	}

	// Mailbox v1 (optional): accept messages via signal and expose via query handler
	var messages []MailboxMessage
	// Agent directory (role metadata)
	type AgentInfo struct {
		AgentID string
		Role    string
	}
	var teamAgents []AgentInfo
	// Dependency sync (selectors) — topic notifications
	topicChans := make(map[string]workflow.Channel)
	if workflow.GetVersion(ctx, "mailbox_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		sig := workflow.GetSignalChannel(ctx, "mailbox_v1")
		msgChan := workflow.NewChannel(ctx)
		workflow.Go(ctx, func(ctx workflow.Context) {
			for {
				var msg MailboxMessage
				sig.Receive(ctx, &msg)
				// Non-blocking send to prevent goroutine deadlock
				sel := workflow.NewSelector(ctx)
				sel.AddSend(msgChan, msg, func() {})
				sel.AddDefault(func() {
					logger.Debug("Mailbox channel send would block, skipping message", "from", msg.From, "to", msg.To)
				})
				sel.Select(ctx)
			}
		})
		workflow.Go(ctx, func(ctx workflow.Context) {
			for {
				var msg MailboxMessage
				msgChan.Receive(ctx, &msg)
				messages = append(messages, msg) // Single goroutine for slice modification
			}
		})
		_ = workflow.SetQueryHandler(ctx, "getMailbox", func() ([]MailboxMessage, error) {
			// Return a copy to avoid race conditions
			result := make([]MailboxMessage, len(messages))
			copy(result, messages)
			return result, nil
		})
	}
	_ = workflow.SetQueryHandler(ctx, "listTeamAgents", func() ([]AgentInfo, error) {
		// Return a copy to avoid race conditions
		result := make([]AgentInfo, len(teamAgents))
		copy(result, teamAgents)
		return result, nil
	})
	_ = workflow.SetQueryHandler(ctx, "findTeamAgentsByRole", func(role string) ([]AgentInfo, error) {
		out := make([]AgentInfo, 0)
		for _, a := range teamAgents {
			if a.Role == role {
				out = append(out, a)
			}
		}
		return out, nil
	})

	// Configure activities
	actOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	ctx = workflow.WithActivityOptions(ctx, actOpts)

	// Version gate for enhanced supervisor memory
	supervisorMemoryVersion := workflow.GetVersion(ctx, "supervisor_memory_v2", workflow.DefaultVersion, 2)

	var decompositionAdvisor *activities.DecompositionAdvisor
	var decompositionSuggestion activities.DecompositionSuggestion

	if supervisorMemoryVersion >= 2 && input.SessionID != "" {
		// Fetch enhanced supervisor memory with strategic insights
		var supervisorMemory *activities.SupervisorMemoryContext
		supervisorMemoryInput := activities.FetchSupervisorMemoryInput{
			SessionID: input.SessionID,
			UserID:    input.UserID,
			TenantID:  input.TenantID,
			Query:     input.Query,
		}

		// Execute enhanced memory fetch
		if err := workflow.ExecuteActivity(ctx, "FetchSupervisorMemory", supervisorMemoryInput).Get(ctx, &supervisorMemory); err == nil {
			// Store conversation history in context
			if len(supervisorMemory.ConversationHistory) > 0 {
				if input.Context == nil {
					input.Context = make(map[string]interface{})
				}
				input.Context["agent_memory"] = supervisorMemory.ConversationHistory
			}

			// Create decomposition advisor for intelligent task breakdown
			decompositionAdvisor = activities.NewDecompositionAdvisor(supervisorMemory)
			decompositionSuggestion = decompositionAdvisor.SuggestDecomposition(input.Query)

			// Log strategic memory insights
			logger.Info("Enhanced supervisor memory loaded",
				"decomposition_patterns", len(supervisorMemory.DecompositionHistory),
				"strategies_tracked", len(supervisorMemory.StrategyPerformance),
				"failure_patterns", len(supervisorMemory.FailurePatterns),
				"user_expertise", supervisorMemory.UserPreferences.ExpertiseLevel)
		} else {
			logger.Warn("Failed to fetch enhanced supervisor memory, falling back to basic", "error", err)
			// Fall back to basic hierarchical memory
			fallbackToBasicMemory(ctx, &input, logger)
		}
	} else if supervisorMemoryVersion >= 1 && input.SessionID != "" {
		// Use basic memory for older versions
		fallbackToBasicMemory(ctx, &input, logger)
	}

	// Dynamic team v1: handle recruit/retire signals
	type RecruitRequest struct {
		Description string
		Role        string
	}
	type RetireRequest struct{ AgentID string }
	recruitCh := workflow.GetSignalChannel(ctx, "recruit_v1")
	retireCh := workflow.GetSignalChannel(ctx, "retire_v1")
	var childResults []activities.AgentExecutionResult
	if workflow.GetVersion(ctx, "dynamic_team_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
		workflow.Go(ctx, func(ctx workflow.Context) {
			for {
				sel := workflow.NewSelector(ctx)
				sel.AddReceive(recruitCh, func(c workflow.ReceiveChannel, more bool) {
					var req RecruitRequest
					c.Receive(ctx, &req)
					role := req.Role
					if role == "" {
						role = "generalist"
					}
					// Policy authorization
					var dec activities.TeamActionDecision
					if err := workflow.ExecuteActivity(ctx, activities.AuthorizeTeamAction, activities.TeamActionInput{
						Action: "recruit", SessionID: input.SessionID, UserID: input.UserID, AgentID: "supervisor", Role: role,
						Metadata: map[string]interface{}{"reason": "dynamic recruit", "description": req.Description},
					}).Get(ctx, &dec); err != nil {
						logger.Error("Team action authorization failed", "error", err)
						return
					}
					if !dec.Allow {
						logger.Warn("Recruit denied by policy", "reason", dec.Reason)
						return
					}
					// Stream event
					emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
						StartToCloseTimeout: 30 * time.Second,
						RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
					})
					if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventTeamRecruited,
						AgentID:    role,
						Message:    req.Description,
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil); err != nil {
						logger.Warn("Failed to emit team recruited event", "error", err)
					}
					// Start child simple task
					var res TaskResult
					if err := workflow.ExecuteChildWorkflow(ctx, SimpleTaskWorkflow, TaskInput{
						Query: req.Description, UserID: input.UserID, SessionID: input.SessionID,
						Context: map[string]interface{}{"role": role}, Mode: input.Mode, History: input.History, SessionCtx: input.SessionCtx,
						ParentWorkflowID: workflowID, // Preserve parent workflow ID for event streaming
					}).Get(ctx, &res); err != nil {
						logger.Error("Dynamic child workflow failed", "error", err)
						return
					}
					childResults = append(childResults, activities.AgentExecutionResult{AgentID: "dynamic", Response: res.Result, TokensUsed: res.TokensUsed, Success: res.Success})
				})
				sel.AddReceive(retireCh, func(c workflow.ReceiveChannel, more bool) {
					var req RetireRequest
					c.Receive(ctx, &req)
					var dec activities.TeamActionDecision
					if err := workflow.ExecuteActivity(ctx, activities.AuthorizeTeamAction, activities.TeamActionInput{
						Action: "retire", SessionID: input.SessionID, UserID: input.UserID, AgentID: req.AgentID,
						Metadata: map[string]interface{}{"reason": "dynamic retire"},
					}).Get(ctx, &dec); err != nil {
						logger.Error("Team action authorization failed", "error", err)
						return
					}
					if !dec.Allow {
						logger.Warn("Retire denied by policy", "reason", dec.Reason)
						return
					}
					emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
						StartToCloseTimeout: 30 * time.Second,
						RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
					})
					if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventTeamRetired,
						AgentID:    req.AgentID,
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil); err != nil {
						logger.Warn("Failed to emit team retired event", "error", err)
					}
				})
				sel.Select(ctx)
			}
		})
	}

	// Prepare decomposition input with advisor suggestions
	decomposeInput := activities.DecompositionInput{
		Query:          input.Query,
		Context:        input.Context,
		AvailableTools: []string{},
	}

	// Apply decomposition advisor suggestions if available
	if decompositionAdvisor != nil {
		if decompositionSuggestion.UsesPreviousSuccess {
			// Add suggested subtasks to context for LLM to consider
			if decomposeInput.Context == nil {
				decomposeInput.Context = make(map[string]interface{})
			}
			decomposeInput.Context["suggested_subtasks"] = decompositionSuggestion.SuggestedSubtasks
			decomposeInput.Context["suggested_strategy"] = decompositionSuggestion.Strategy
			decomposeInput.Context["confidence"] = decompositionSuggestion.Confidence
		}

		if len(decompositionSuggestion.Warnings) > 0 {
			decomposeInput.Context["decomposition_warnings"] = decompositionSuggestion.Warnings
		}

		logger.Info("Using decomposition advisor suggestions",
			"strategy", decompositionSuggestion.Strategy,
			"confidence", decompositionSuggestion.Confidence,
			"uses_previous", decompositionSuggestion.UsesPreviousSuccess)
	}

	// Decompose the task to get subtasks and agent types
	var decomp activities.DecompositionResult
	if err := workflow.ExecuteActivity(ctx, constants.DecomposeTaskActivity, decomposeInput).Get(ctx, &decomp); err != nil {
		logger.Error("Task decomposition failed", "error", err)
		return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("decomposition failed: %v", err)}, err
	}

	// Override strategy if advisor has high confidence
	if decompositionAdvisor != nil && decompositionSuggestion.Confidence > 0.8 {
		decomp.ExecutionStrategy = decompositionSuggestion.Strategy
		logger.Info("Overriding execution strategy based on advisor", "strategy", decomp.ExecutionStrategy)
	}

	// Emit team status event after decomposition
	if len(decomp.Subtasks) > 1 {
		message := fmt.Sprintf("Coordinating %d agents to handle subtasks", len(decomp.Subtasks))
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventTeamStatus,
			AgentID:    "supervisor",
			Message:    message,
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil); err != nil {
			logger.Warn("Failed to emit team status event", "error", err)
		}
	}

	// If simple task, delegate full task to DAGWorkflow (reuse behavior)
	// Route simple tasks properly: check mode, complexity score, or single subtask
	isSimpleTask := decomp.Mode == "simple" || decomp.ComplexityScore < 0.3 || len(decomp.Subtasks) <= 1

	if isSimpleTask {
		// Convert to strategies.TaskInput
		strategiesInput := convertToStrategiesInput(input)
		var strategiesResult strategies.TaskResult
		if err := workflow.ExecuteChildWorkflow(ctx, strategies.DAGWorkflow, strategiesInput).Get(ctx, &strategiesResult); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
		return convertFromStrategiesResult(strategiesResult), nil
	}

	// Execute each subtask as a child SimpleTaskWorkflow sequentially (deterministic)
	var lastWSSeq uint64

	// Track running budget usage across agents if a task-level budget is present
	totalUsed := 0
	taskBudget := 0
	if v, ok := input.Context["budget_remaining"].(int); ok && v > 0 {
		taskBudget = v
	}
	if v, ok := input.Context["budget_remaining"].(float64); ok && v > 0 {
		taskBudget = int(v)
	}

	// INTELLIGENT RETRY STRATEGY: Prevents infinite loops while supporting complex tasks
	failedTasks := 0
	maxFailures := len(decomp.Subtasks)/2 + 1 // Allow up to 50%+1 tasks to fail before aborting
	taskRetries := make(map[string]int)       // Track retry count per task ID (prevents infinite retries)
	maxRetriesPerTask := 3                    // Max 3 retries per individual task (handles transient failures)

	// Build a set of topics actually produced by this plan to avoid waiting
	// on dependencies that will never be satisfied.
	producesSet := make(map[string]struct{})
	for _, s := range decomp.Subtasks {
		for _, t := range s.Produces {
			if t == "" {
				continue
			}
			producesSet[t] = struct{}{}
		}
	}

    // Version gate for context compression determinism
    compressionVersion := workflow.GetVersion(ctx, "context_compress_v1", workflow.DefaultVersion, 1)

    for i, st := range decomp.Subtasks {
		// Emit progress event for this subtask
		progressMessage := fmt.Sprintf("Starting subtask %d of %d: %s", i+1, len(decomp.Subtasks), st.Description)
		if len(st.Description) > 50 {
			progressMessage = fmt.Sprintf("Starting subtask %d of %d: %s...", i+1, len(decomp.Subtasks), st.Description[:47])
		}
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventProgress,
			AgentID:    fmt.Sprintf("agent-%s", st.ID),
			Message:    progressMessage,
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil); err != nil {
			logger.Warn("Failed to emit progress event", "error", err)
		}

		// Build context, injecting role when enabled
		childCtx := make(map[string]interface{})
		for k, v := range input.Context {
			childCtx[k] = v
		}
		if workflow.GetVersion(ctx, "roles_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
			role := "generalist"
			if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" {
				role = decomp.AgentTypes[i]
			}
			childCtx["role"] = role
			teamAgents = append(teamAgents, AgentInfo{AgentID: fmt.Sprintf("agent-%s", st.ID), Role: role})
			// Optional: record role assignment in mailbox
			if workflow.GetVersion(ctx, "mailbox_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
				messages = append(messages, MailboxMessage{From: "supervisor", To: fmt.Sprintf("agent-%s", st.ID), Role: role, Content: "role_assigned"})
			}
			// Stream role assignment
			emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: 30 * time.Second,
				RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
			})
			if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
				WorkflowID: workflowID,
				EventType:  activities.StreamEventRoleAssigned,
				AgentID:    fmt.Sprintf("agent-%s", st.ID),
				Message:    role,
				Timestamp:  workflow.Now(ctx),
			}).Get(ctx, nil); err != nil {
				logger.Warn("Failed to emit role assigned event", "error", err)
			}
		}

		// Budget hinting: set token_budget for policy + agent if per-agent budget is present
		agentMax := 0
		if v, ok := childCtx["budget_agent_max"].(int); ok {
			agentMax = v
		}
		if v, ok := childCtx["budget_agent_max"].(float64); ok && v > 0 {
			agentMax = int(v)
		}
        if agentMax > 0 && compressionVersion >= 1 {
			childCtx["token_budget"] = agentMax
		}

		// Sliding-window shaping with optional middle summary when nearing per-agent budget
		historyForAgent := convertHistoryForAgent(input.History)
        if agentMax > 0 {
            est := activities.EstimateTokens(historyForAgent)
            trig, tgt := getCompressionRatios(childCtx, 0.75, 0.375)
            if est >= int(float64(agentMax)*trig) {
                var compressResult activities.CompressContextResult
                _ = workflow.ExecuteActivity(ctx, activities.CompressAndStoreContext, activities.CompressContextInput{
                    SessionID:    input.SessionID,
                    History:      convertHistoryMapForCompression(input.History),
                    TargetTokens: int(float64(agentMax) * tgt),
                    ParentWorkflowID: workflowID,
                }).Get(ctx, &compressResult)
                if compressResult.Summary != "" {
                    childCtx["context_summary"] = fmt.Sprintf("Previous context summary: %s", compressResult.Summary)
                    prim, rec := getPrimersRecents(childCtx, 3, 20)
                    shaped := shapeHistory(input.History, prim, rec)
                    historyForAgent = convertHistoryForAgent(shaped)
                    // Emit compression applied event
                    _ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
                        WorkflowID: workflowID,
                        EventType:  activities.StreamEventDataProcessing,
                        AgentID:    fmt.Sprintf("agent-%s", st.ID),
                        Message:    activities.MsgCompressionApplied(),
                        Timestamp:  workflow.Now(ctx),
                    }).Get(ctx, nil)
                    // Emit summary injected event
                    _ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
                        WorkflowID: workflowID,
                        EventType:  activities.StreamEventDataProcessing,
                        AgentID:    fmt.Sprintf("agent-%s", st.ID),
                        Message:    activities.MsgSummaryAdded(),
                        Timestamp:  workflow.Now(ctx),
                    }).Get(ctx, nil)
                }
			}
		}

		// P2P Coordination: wait on declared Consumes topics before starting this subtask
		// Only enabled if P2PCoordinationEnabled is true in config and decomposition has valid Produces/Consumes
		var p2pConfig activities.WorkflowConfig
		if err := workflow.ExecuteActivity(ctx, activities.GetWorkflowConfig).Get(ctx, &p2pConfig); err != nil {
			logger.Warn("Failed to load P2P config, skipping coordination", "error", err)
			p2pConfig.P2PCoordinationEnabled = false
		}

		// Check version gates first for determinism, but only execute P2P if enabled
		p2pSyncVersion := workflow.GetVersion(ctx, "p2p_sync_v1", workflow.DefaultVersion, 1)
		teamWorkspaceVersion := workflow.GetVersion(ctx, "team_workspace_v1", workflow.DefaultVersion, 1)

		// Only proceed with P2P coordination if:
		// 1. P2P is enabled in config AND
		// 2. Version gates indicate P2P code exists
			if p2pConfig.P2PCoordinationEnabled &&
				p2pSyncVersion != workflow.DefaultVersion &&
				teamWorkspaceVersion != workflow.DefaultVersion &&
				i < len(decomp.Subtasks) && len(decomp.Subtasks[i].Consumes) > 0 {
				logger.Debug("P2P coordination enabled, checking dependencies",
					"subtask_id", decomp.Subtasks[i].ID,
					"consumes", decomp.Subtasks[i].Consumes)
				for _, topic := range decomp.Subtasks[i].Consumes {
					// Skip waiting if no subtask produces this topic
					if _, ok := producesSet[topic]; !ok {
						logger.Info("Skipping P2P wait: no producer in plan", "topic", topic, "subtask_id", st.ID)
						continue
					}
					// Use configured timeout or default
					maxWaitTime := time.Duration(p2pConfig.P2PTimeoutSeconds) * time.Second
					if maxWaitTime == 0 {
						maxWaitTime = 6 * time.Minute
					}
				startTime := workflow.Now(ctx)
				backoff := 1 * time.Second
				maxBackoff := 30 * time.Second
				attempts := 0

				for workflow.Now(ctx).Sub(startTime) < maxWaitTime {
					// Emit waiting event on first attempt
					if attempts == 0 {
						waitMessage := fmt.Sprintf("Waiting for dependency: %s", topic)
						emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
							StartToCloseTimeout: 30 * time.Second,
							RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
						})
						if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
							WorkflowID: workflowID,
							EventType:  activities.StreamEventWaiting,
							AgentID:    fmt.Sprintf("agent-%s", st.ID),
							Message:    waitMessage,
							Timestamp:  workflow.Now(ctx),
						}).Get(ctx, nil); err != nil {
							logger.Warn("Failed to emit waiting event", "error", err)
						}
					}

					// Check if entries already exist
					var entries []activities.WorkspaceEntry
					if err := workflow.ExecuteActivity(ctx, constants.WorkspaceListActivity, activities.WorkspaceListInput{
						WorkflowID: workflowID,
						Topic:      topic,
						SinceSeq:   0,
						Limit:      1,
					}).Get(ctx, &entries); err != nil {
						logger.Warn("Failed to check workspace", "topic", topic, "error", err)
						break
					}
					if len(entries) > 0 {
						break
					}

					// Check if we've exceeded the time limit before waiting
					if workflow.Now(ctx).Sub(startTime) >= maxWaitTime {
						break
					}

					// Setup selector wait using a topic channel + exponential backoff timer
					ch, ok := topicChans[topic]
					if !ok {
						ch = workflow.NewChannel(ctx)
						topicChans[topic] = ch
					}
					sel := workflow.NewSelector(ctx)
					sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {})
					// Exponential backoff to reduce polling frequency
					timer := workflow.NewTimer(ctx, backoff)
					sel.AddFuture(timer, func(f workflow.Future) {})
					sel.Select(ctx)
					attempts++

					// Increase backoff up to max
					backoff = backoff * 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
				}
				if workflow.Now(ctx).Sub(startTime) >= maxWaitTime {
					logger.Warn("Dependency wait timeout", "topic", topic, "wait_time", maxWaitTime, "attempts", attempts)
				}
				// Stream dependency satisfied
				emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
					StartToCloseTimeout: 30 * time.Second,
					RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
				})
				if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
					WorkflowID: workflowID,
					EventType:  activities.StreamEventDependencySatisfied,
					AgentID:    fmt.Sprintf("agent-%s", st.ID),
					Message:    topic,
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, nil); err != nil {
					logger.Warn("Failed to emit dependency satisfied event", "error", err)
				}
			}
		} else if i < len(decomp.Subtasks) && len(decomp.Subtasks[i].Consumes) > 0 {
			// Log when P2P dependencies exist but P2P is disabled
			logger.Debug("Skipping P2P dependency wait (P2P disabled)",
				"p2p_enabled", p2pConfig.P2PCoordinationEnabled,
				"subtask_id", decomp.Subtasks[i].ID,
				"would_consume", decomp.Subtasks[i].Consumes)
		}

		// P2P demo code removed - use P2PCoordinationEnabled config instead

		// Add previous results to context for sequential dependencies
		if len(childResults) > 0 {
			previousResults := make(map[string]interface{})
			for j, prevResult := range childResults {
				if j < i && j < len(decomp.Subtasks) {
					resultMap := map[string]interface{}{
						"response": prevResult.Response,
						"tokens":   prevResult.TokensUsed,
						"success":  prevResult.Success,
					}
                // Try to extract numeric value from response (standardize key name)
                if numVal, ok := parseNumericValue(prevResult.Response); ok {
                    resultMap["numeric_value"] = numVal
                }
					previousResults[decomp.Subtasks[j].ID] = resultMap
				}
			}
			childCtx["previous_results"] = previousResults
		}

		// Clear tool_parameters for dependent tasks to avoid placeholder issues
		if len(st.Dependencies) > 0 && st.ToolParameters != nil {
			logger.Info("Clearing tool_parameters for dependent task",
				"task_id", st.ID,
				"dependencies", st.Dependencies,
			)
			st.ToolParameters = nil
		}

		var res activities.AgentExecutionResult
		// Retry loop within the same iteration to avoid relying on range index mutation
		var execErr error
		for {
			// Use budgeted agent when a per-agent budget hint is present
			agentMax := 0
			if v, ok := childCtx["budget_agent_max"].(int); ok {
				agentMax = v
			}
			if v, ok := childCtx["budget_agent_max"].(float64); ok && v > 0 {
				agentMax = int(v)
			}
			if agentMax > 0 {
				wid := workflowID
				execErr = workflow.ExecuteActivity(ctx, constants.ExecuteAgentWithBudgetActivity, activities.BudgetedAgentInput{
					AgentInput: activities.AgentExecutionInput{
						Query:          st.Description,
						AgentID:        fmt.Sprintf("agent-%s", st.ID),
						Context:        childCtx,
						Mode:           input.Mode,
						SessionID:      input.SessionID,
						History:        historyForAgent,
						SuggestedTools: st.SuggestedTools,
						ToolParameters: st.ToolParameters,
						ParentWorkflowID: workflowID,
					},
					MaxTokens: agentMax,
					UserID:    input.UserID,
					TaskID:    wid,
					ModelTier: "medium",
				}).Get(ctx, &res)
			} else {
				execErr = workflow.ExecuteActivity(ctx, activities.ExecuteAgent, activities.AgentExecutionInput{
					Query:          st.Description,
					AgentID:        fmt.Sprintf("agent-%s", st.ID),
					Context:        childCtx,
					Mode:           input.Mode,
					SessionID:      input.SessionID,
					History:        historyForAgent,
					SuggestedTools: st.SuggestedTools,
					ToolParameters: st.ToolParameters,
					ParentWorkflowID: workflowID,
				}).Get(ctx, &res)
			}
			if execErr == nil {
				// Emit budget usage progress if available
				if agentMax > 0 {
					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventProgress,
						AgentID:    fmt.Sprintf("agent-%s", st.ID),
						Message:    activities.MsgBudget(res.TokensUsed, agentMax),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
				}
				// Update and emit running total if task budget is known
				totalUsed += res.TokensUsed
				if taskBudget > 0 {
					_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
						WorkflowID: workflowID,
						EventType:  activities.StreamEventProgress,
						AgentID:    "supervisor",
						Message:    activities.MsgBudget(totalUsed, taskBudget),
						Timestamp:  workflow.Now(ctx),
					}).Get(ctx, nil)
				}
				// Persist agent execution (fire-and-forget)
				persistCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
					StartToCloseTimeout: 5 * time.Second,
					RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
				})

				state := "COMPLETED"
				if !res.Success {
					state = "FAILED"
				}

				workflow.ExecuteActivity(
					persistCtx,
					activities.PersistAgentExecutionStandalone,
					activities.PersistAgentExecutionInput{
						WorkflowID:   workflowID,
						AgentID:      fmt.Sprintf("agent-%s", st.ID),
						Input:        st.Description,
						Output:       res.Response,
						State:        state,
						TokensUsed:   res.TokensUsed,
						ModelUsed:    res.ModelUsed,
						DurationMs:   res.DurationMs,
						Error:        res.Error,
						Metadata: map[string]interface{}{
							"workflow": "supervisor",
							"strategy": "supervisor",
							"task_id":  st.ID,
						},
					},
				)
				break
			}
			taskRetries[st.ID]++
			logger.Error("Child SimpleTaskWorkflow failed", "subtask_id", st.ID, "error", execErr, "retry_count", taskRetries[st.ID])

			if taskRetries[st.ID] >= maxRetriesPerTask {
				logger.Error("Task exceeded retry limit, marking as failed", "subtask_id", st.ID, "retries", taskRetries[st.ID])
				failedTasks++
				if failedTasks >= maxFailures {
					logger.Error("Too many subtask failures, aborting workflow", "failed_tasks", failedTasks, "max_failures", maxFailures)
					return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("Too many subtask failures (%d/%d)", failedTasks, len(decomp.Subtasks))}, fmt.Errorf("workflow aborted due to excessive failures")
				}
				// Give up on this task and move to the next one
				execErr = fmt.Errorf("max retries reached")
				break
			}
			// Retry immediately (deterministic). Optionally sleep if desired.
			logger.Info("Retrying failed task", "subtask_id", st.ID, "retry_count", taskRetries[st.ID])
		}
		if execErr != nil {
			continue
		}
		// Capture agent result for synthesis directly
		childResults = append(childResults, res)

			// Produce outputs to workspace per plan
			if teamWorkspaceVersion != workflow.DefaultVersion &&
				i < len(decomp.Subtasks) && len(decomp.Subtasks[i].Produces) > 0 {
			for _, topic := range decomp.Subtasks[i].Produces {
				var wr activities.WorkspaceAppendResult
				if err := workflow.ExecuteActivity(ctx, constants.WorkspaceAppendActivity, activities.WorkspaceAppendInput{
					WorkflowID: workflowID,
					Topic:      topic,
					Entry:      map[string]interface{}{"subtask_id": st.ID, "summary": res.Response},
					Timestamp:  workflow.Now(ctx),
				}).Get(ctx, &wr); err != nil {
					logger.Warn("Failed to append to workspace", "topic", topic, "error", err)
					continue
				}
				lastWSSeq = wr.Seq
				_ = lastWSSeq
				// Notify any selector waiting on this topic (non-blocking)
				if ch, ok := topicChans[topic]; ok {
					sel := workflow.NewSelector(ctx)
					sel.AddSend(ch, true, func() {})
					sel.AddDefault(func() {
						logger.Debug("Channel send would block, skipping notification", "topic", topic)
					})
					sel.Select(ctx)
				}
			}
		}
	}

	// Emit data processing event for synthesis
	if len(childResults) > 1 {
		synthMessage := fmt.Sprintf("Synthesizing results from %d agents", len(childResults))
		emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 30 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})
		if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
			WorkflowID: workflowID,
			EventType:  activities.StreamEventDataProcessing,
			AgentID:    "supervisor",
			Message:    synthMessage,
			Timestamp:  workflow.Now(ctx),
		}).Get(ctx, nil); err != nil {
			logger.Warn("Failed to emit data processing event", "error", err)
		}
	}

	// Synthesize results using configured mode
	var synth activities.SynthesisResult

	// Check if the decomposition included a synthesis/summarization subtask
	// This commonly happens when users request specific output formats (e.g., "summarize in Chinese")
	// Following SOTA patterns: if decomposition includes synthesis, use that instead of duplicating
	hasSynthesisSubtask := false
	var synthesisTaskIdx int

	for i, subtask := range decomp.Subtasks {
		taskLower := strings.ToLower(subtask.Description)
		// Check if this subtask is a synthesis/summary task
		if strings.Contains(taskLower, "synthesize") ||
			strings.Contains(taskLower, "synthesis") ||
			strings.Contains(taskLower, "summarize") ||
			strings.Contains(taskLower, "summary") ||
			strings.Contains(taskLower, "combine") ||
			strings.Contains(taskLower, "aggregate") {
			hasSynthesisSubtask = true
			synthesisTaskIdx = i
			logger.Info("Detected synthesis subtask in decomposition",
				"task_id", subtask.ID,
				"description", subtask.Description,
				"index", i,
			)
		}
	}

	if input.BypassSingleResult && len(childResults) == 1 && childResults[0].Success {
		// Single result bypass
		synth = activities.SynthesisResult{FinalResult: childResults[0].Response, TokensUsed: childResults[0].TokensUsed}
	} else if hasSynthesisSubtask && synthesisTaskIdx < len(childResults) && childResults[synthesisTaskIdx].Success {
		// Use the synthesis subtask's result as the final result
		// This prevents double synthesis and respects the user's requested format
		synthesisResult := childResults[synthesisTaskIdx]
		synth = activities.SynthesisResult{
			FinalResult: synthesisResult.Response,
			TokensUsed:  0, // Don't double-count tokens as they're already counted in agent execution
		}
		logger.Info("Using synthesis subtask result as final output",
			"agent_id", synthesisResult.AgentID,
			"response_length", len(synthesisResult.Response),
		)
	} else {
		// No synthesis subtask in decomposition, perform standard synthesis
		logger.Info("Performing standard synthesis of agent results")
		if err := workflow.ExecuteActivity(ctx, activities.SynthesizeResultsLLM, activities.SynthesisInput{Query: input.Query, AgentResults: childResults}).Get(ctx, &synth); err != nil {
			return TaskResult{Success: false, ErrorMessage: err.Error()}, err
		}
	}

	// Update session with token usage (include per-agent usage for accurate cost)
	if input.SessionID != "" {
		var sessionUpdateResult activities.SessionUpdateResult
		// Build per-agent usage list (model + tokens)
		usages := make([]activities.AgentUsage, 0, len(childResults))
		for _, cr := range childResults {
			usages = append(usages, activities.AgentUsage{Model: cr.ModelUsed, Tokens: cr.TokensUsed, InputTokens: cr.InputTokens, OutputTokens: cr.OutputTokens})
		}
		err := workflow.ExecuteActivity(ctx,
			constants.UpdateSessionResultActivity,
			activities.SessionUpdateInput{
				SessionID:  input.SessionID,
				Result:     synth.FinalResult,
				TokensUsed: synth.TokensUsed,
				AgentsUsed: len(childResults),
				AgentUsage: usages,
			},
		).Get(ctx, &sessionUpdateResult)
		if err != nil {
			logger.Warn("Failed to update session with tokens",
				"session_id", input.SessionID,
				"error", err,
			)
		}
	}

	// Record decomposition results for future learning (fire-and-forget)
	if supervisorMemoryVersion >= 2 && input.SessionID != "" && len(decomp.Subtasks) > 0 {
		recordCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 5 * time.Second,
			RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
		})

		// Calculate workflow duration
		workflowDuration := workflow.Now(ctx).Sub(workflowStartTime).Milliseconds()

		// Extract subtask descriptions
		subtaskDescriptions := make([]string, len(decomp.Subtasks))
		for i, st := range decomp.Subtasks {
			subtaskDescriptions[i] = st.Description
		}

		// Fire and forget - don't wait for result
		workflow.ExecuteActivity(recordCtx, "RecordDecomposition", activities.RecordDecompositionInput{
			SessionID:  input.SessionID,
			Query:      input.Query,
			Subtasks:   subtaskDescriptions,
			Strategy:   decomp.ExecutionStrategy,
			Success:    true,
			DurationMs: workflowDuration,
			TokensUsed: synth.TokensUsed,
		})

		logger.Info("Recorded decomposition outcome",
			"strategy", decomp.ExecutionStrategy,
			"subtasks", len(decomp.Subtasks),
			"duration_ms", workflowDuration)
	}

	// Emit workflow completed event for dashboards
	emitCtx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	_ = workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
		WorkflowID: workflowID,
		EventType:  activities.StreamEventWorkflowCompleted,
		AgentID:    "supervisor",
		Message:    "Workflow completed",
		Timestamp:  workflow.Now(ctx),
	}).Get(ctx, nil)

	return TaskResult{Result: synth.FinalResult, Success: true, TokensUsed: synth.TokensUsed, Metadata: map[string]interface{}{
		"num_children": len(childResults),
	}}, nil
}

// Note: convertToStrategiesInput and convertFromStrategiesResult are defined in orchestrator_router.go
