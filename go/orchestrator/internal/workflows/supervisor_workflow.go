package workflows

import (
    "fmt"
    "time"

    "go.temporal.io/sdk/workflow"
    "go.temporal.io/sdk/temporal"

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
    
    // ENTERPRISE TIMEOUT STRATEGY:
    // - No overall workflow timeout (complex tasks may take hours/days)
    // - Per-task retry limits (3 max) prevent infinite loops  
    // - Failure threshold (50%+1) provides intelligent abort criteria
    // - See docs/timeout-retry-strategy.md for full details
    
    // Emit WORKFLOW_STARTED event
    emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 30 * time.Second,
        RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
    })
    if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
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
    type AgentInfo struct { AgentID string; Role string }
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
        for _, a := range teamAgents { if a.Role == role { out = append(out, a) } }
        return out, nil
    })

    // Configure activities
    actOpts := workflow.ActivityOptions{
        StartToCloseTimeout: 10 * time.Minute,
        RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 3},
    }
    ctx = workflow.WithActivityOptions(ctx, actOpts)

    // Dynamic team v1: handle recruit/retire signals
    type RecruitRequest struct { Description string; Role string }
    type RetireRequest  struct { AgentID string }
    recruitCh := workflow.GetSignalChannel(ctx, "recruit_v1")
    retireCh  := workflow.GetSignalChannel(ctx, "retire_v1")
    var childResults []activities.AgentExecutionResult
    if workflow.GetVersion(ctx, "dynamic_team_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
        workflow.Go(ctx, func(ctx workflow.Context) {
            for {
                sel := workflow.NewSelector(ctx)
                sel.AddReceive(recruitCh, func(c workflow.ReceiveChannel, more bool) {
                    var req RecruitRequest
                    c.Receive(ctx, &req)
                    role := req.Role; if role == "" { role = "generalist" }
                    // Policy authorization
                    var dec activities.TeamActionDecision
                    if err := workflow.ExecuteActivity(ctx, activities.AuthorizeTeamAction, activities.TeamActionInput{
                        Action:   "recruit", SessionID: input.SessionID, UserID: input.UserID, AgentID: "supervisor", Role: role,
                        Metadata: map[string]interface{}{ "reason": "dynamic recruit", "description": req.Description },
                    }).Get(ctx, &dec); err != nil {
                        logger.Error("Team action authorization failed", "error", err)
                        return
                    }
                    if !dec.Allow { logger.Warn("Recruit denied by policy", "reason", dec.Reason); return }
                    // Stream event
                    emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
                        StartToCloseTimeout: 30 * time.Second,
                        RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
                    })
                    if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
                        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
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
                        Context: map[string]interface{}{ "role": role }, Mode: input.Mode, History: input.History, SessionCtx: input.SessionCtx,
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
                        Action:   "retire", SessionID: input.SessionID, UserID: input.UserID, AgentID: req.AgentID,
                        Metadata: map[string]interface{}{ "reason": "dynamic retire" },
                    }).Get(ctx, &dec); err != nil {
                        logger.Error("Team action authorization failed", "error", err)
                        return
                    }
                    if !dec.Allow { logger.Warn("Retire denied by policy", "reason", dec.Reason); return }
                    emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
                        StartToCloseTimeout: 30 * time.Second,
                        RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
                    })
                    if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
                        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
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

    // Decompose the task to get subtasks and agent types
    var decomp activities.DecompositionResult
    if err := workflow.ExecuteActivity(ctx, constants.DecomposeTaskActivity, activities.DecompositionInput{
        Query:          input.Query,
        Context:        input.Context,
        AvailableTools: []string{},
    }).Get(ctx, &decomp); err != nil {
        logger.Error("Task decomposition failed", "error", err)
        return TaskResult{Success: false, ErrorMessage: fmt.Sprintf("decomposition failed: %v", err)}, err
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
    
    // INTELLIGENT RETRY STRATEGY: Prevents infinite loops while supporting complex tasks
    failedTasks := 0
    maxFailures := len(decomp.Subtasks)/2 + 1 // Allow up to 50%+1 tasks to fail before aborting
    taskRetries := make(map[string]int)       // Track retry count per task ID (prevents infinite retries)
    maxRetriesPerTask := 3                    // Max 3 retries per individual task (handles transient failures)
    
    for i, st := range decomp.Subtasks {
        // Build context, injecting role when enabled
        childCtx := make(map[string]interface{})
        for k, v := range input.Context { childCtx[k] = v }
        if workflow.GetVersion(ctx, "roles_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
            role := "generalist"
            if i < len(decomp.AgentTypes) && decomp.AgentTypes[i] != "" { role = decomp.AgentTypes[i] }
            childCtx["role"] = role
            teamAgents = append(teamAgents, AgentInfo{AgentID: fmt.Sprintf("agent-%s", st.ID), Role: role})
            // Optional: record role assignment in mailbox
            if workflow.GetVersion(ctx, "mailbox_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
                messages = append(messages, MailboxMessage{From: "supervisor", To: fmt.Sprintf("agent-%s", st.ID), Role: role, Content: "role_assigned"})
            }
            // Stream role assignment
            emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
                StartToCloseTimeout: 30 * time.Second,
                RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
            })
            if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
                WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
                EventType:  activities.StreamEventRoleAssigned,
                AgentID:    fmt.Sprintf("agent-%s", st.ID),
                Message:    role,
                Timestamp:  workflow.Now(ctx),
            }).Get(ctx, nil); err != nil {
                logger.Warn("Failed to emit role assigned event", "error", err)
            }
        }

        // Dependency sync v1: wait on declared Consumes topics before starting this subtask
        if workflow.GetVersion(ctx, "p2p_sync_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion &&
           workflow.GetVersion(ctx, "team_workspace_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
            if i < len(decomp.Subtasks) && len(decomp.Subtasks[i].Consumes) > 0 {
                for _, topic := range decomp.Subtasks[i].Consumes {
                    // Add timeout protection with exponential backoff for efficiency
                    maxWaitTime := 6 * time.Minute
                    startTime := workflow.Now(ctx)
                    backoff := 1 * time.Second
                    maxBackoff := 30 * time.Second
                    attempts := 0
                    
                    for workflow.Now(ctx).Sub(startTime) < maxWaitTime {
                        // Check if entries already exist
                        var entries []activities.WorkspaceEntry
                        if err := workflow.ExecuteActivity(ctx, constants.WorkspaceListActivity, activities.WorkspaceListInput{
                            WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
                            Topic:      topic,
                            SinceSeq:   0,
                            Limit:      1,
                        }).Get(ctx, &entries); err != nil {
                            logger.Warn("Failed to check workspace", "topic", topic, "error", err)
                            break
                        }
                        if len(entries) > 0 { break }
                        // Setup selector wait using a topic channel + exponential backoff timer
                        ch, ok := topicChans[topic]
                        if !ok { ch = workflow.NewChannel(ctx); topicChans[topic] = ch }
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
                        RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
                    })
                    if err := workflow.ExecuteActivity(emitCtx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
                        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
                        EventType:  activities.StreamEventDependencySatisfied,
                        AgentID:    fmt.Sprintf("agent-%s", st.ID),
                        Message:    topic,
                        Timestamp:  workflow.Now(ctx),
                    }).Get(ctx, nil); err != nil {
                        logger.Warn("Failed to emit dependency satisfied event", "error", err)
                    }
                }
            }
        }

        // P2P demo: for the second subtask (i==1), send a TaskRequest message and wait for a workspace topic to have entries
        if workflow.GetVersion(ctx, "mailbox_v2", workflow.DefaultVersion, 1) != workflow.DefaultVersion &&
           workflow.GetVersion(ctx, "team_workspace_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion &&
           workflow.GetVersion(ctx, "p2p_sync_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
            if i == 1 {
                // Send TaskRequest from supervisor to the agent
                if err := workflow.ExecuteActivity(ctx, constants.SendAgentMessageActivity, activities.SendAgentMessageInput{
                    WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
                    From:       "supervisor",
                    To:         fmt.Sprintf("agent-%s", st.ID),
                    Type:       activities.MessageTypeRequest,
                    Payload:    map[string]interface{}{"topic": "team:findings", "note": "please proceed when findings are available"},
                }).Get(ctx, nil); err != nil {
                    logger.Warn("Failed to send agent message", "error", err)
                }
                // Wait until workspace has at least one entry on the topic (with timeout)
                maxWaitAttempts := 300 // 5 minutes max wait
                waitAttempts := 0
                for waitAttempts < maxWaitAttempts {
                    var entries []activities.WorkspaceEntry
                    if err := workflow.ExecuteActivity(ctx, constants.WorkspaceListActivity, activities.WorkspaceListInput{
                        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
                        Topic:      "team:findings",
                        SinceSeq:   0,
                        Limit:      50,
                    }).Get(ctx, &entries); err != nil {
                        logger.Warn("Failed to check workspace for findings", "error", err)
                        break
                    }
                    if len(entries) > 0 { break }
                    // Sleep deterministically
                    if err := workflow.Sleep(ctx, time.Second); err != nil {
                        logger.Warn("Sleep interrupted", "error", err)
                        break
                    }
                    waitAttempts++
                }
                if waitAttempts >= maxWaitAttempts {
                    logger.Warn("Timeout waiting for team findings", "max_attempts", maxWaitAttempts)
                }
            }
        }

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
                    // Try to extract numeric value from response
                    if numVal, ok := parseNumericValue(prevResult.Response); ok {
                        resultMap["value"] = numVal
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
            if v, ok := childCtx["budget_agent_max"].(int); ok { agentMax = v }
            if v, ok := childCtx["budget_agent_max"].(float64); ok { if v > 0 { agentMax = int(v) } }
            if agentMax > 0 {
                wid := workflow.GetInfo(ctx).WorkflowExecution.ID
                execErr = workflow.ExecuteActivity(ctx, constants.ExecuteAgentWithBudgetActivity, activities.BudgetedAgentInput{
                    AgentInput: activities.AgentExecutionInput{
                        Query:          st.Description,
                        AgentID:        fmt.Sprintf("agent-%s", st.ID),
                        Context:        childCtx,
                        Mode:           input.Mode,
                        SessionID:      input.SessionID,
                        History:        convertHistoryForAgent(input.History),
                        SuggestedTools: st.SuggestedTools,
                        ToolParameters: st.ToolParameters,
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
                    History:        convertHistoryForAgent(input.History),
                    SuggestedTools: st.SuggestedTools,
                    ToolParameters: st.ToolParameters,
                }).Get(ctx, &res)
            }
            if execErr == nil {
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
        if workflow.GetVersion(ctx, "team_workspace_v1", workflow.DefaultVersion, 1) != workflow.DefaultVersion {
            if i < len(decomp.Subtasks) && len(decomp.Subtasks[i].Produces) > 0 {
                for _, topic := range decomp.Subtasks[i].Produces {
                    var wr activities.WorkspaceAppendResult
                    if err := workflow.ExecuteActivity(ctx, constants.WorkspaceAppendActivity, activities.WorkspaceAppendInput{
                        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
                        Topic:      topic,
                        Entry:      map[string]interface{}{"subtask_id": st.ID, "summary": res.Response},
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
    }

    // Synthesize results using configured mode
    var synth activities.SynthesisResult
    if input.BypassSingleResult && len(childResults) == 1 && childResults[0].Success {
        synth = activities.SynthesisResult{FinalResult: childResults[0].Response, TokensUsed: childResults[0].TokensUsed}
    } else {
        if err := workflow.ExecuteActivity(ctx, activities.SynthesizeResultsLLM, activities.SynthesisInput{Query: input.Query, AgentResults: childResults}).Get(ctx, &synth); err != nil {
            return TaskResult{Success: false, ErrorMessage: err.Error()}, err
        }
    }

    return TaskResult{Result: synth.FinalResult, Success: true, TokensUsed: synth.TokensUsed, Metadata: map[string]interface{}{
        "num_children": len(childResults),
    }}, nil
}

// Note: convertToStrategiesInput and convertFromStrategiesResult are defined in orchestrator_router.go
