package workflows

import (
    "fmt"
    "time"

    "go.temporal.io/sdk/temporal"
    "go.temporal.io/sdk/workflow"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
    ometrics "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
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

    // Conservative activity options for fast planning
    actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 60 * time.Second,
        RetryPolicy: &temporal.RetryPolicy{ MaximumAttempts: 3 },
    })

    // (Optional) Load router/approval config
    var cfg activities.WorkflowConfig
    if err := workflow.ExecuteActivity(actx, activities.GetWorkflowConfig).Get(ctx, &cfg); err != nil {
        // Continue with defaults on failure
    }
    simpleThreshold := cfg.SimpleThreshold
    if simpleThreshold == 0 { simpleThreshold = 0.3 }

    // 1) Decompose the task (planning + complexity)
    var decomp activities.DecompositionResult
    if err := workflow.ExecuteActivity(actx, constants.DecomposeTaskActivity, activities.DecompositionInput{
        Query:          input.Query,
        Context:        input.Context,
        AvailableTools: []string{},
    }).Get(ctx, &decomp); err != nil {
        logger.Error("Task decomposition failed", "error", err)
        return TaskResult{ Success: false, ErrorMessage: err.Error() }, err
    }

    logger.Info("Routing decision",
        "complexity", decomp.ComplexityScore,
        "mode", decomp.Mode,
        "num_subtasks", len(decomp.Subtasks),
        "cognitive_strategy", decomp.CognitiveStrategy,
    )

    // 1.5) Budget preflight (estimate based on plan)
    if input.UserID != "" { // Only check when we have a user scope
        est := EstimateTokens(decomp)
        if res, err := BudgetPreflight(ctx, input, est); err == nil && res != nil {
            if !res.CanProceed {
                return TaskResult{ Success: false, ErrorMessage: res.Reason, Metadata: map[string]interface{}{"budget_blocked": true} }, nil
            }
            // Pass budget info to child workflows via context
            if input.Context == nil { input.Context = map[string]interface{}{} }
            input.Context["budget_remaining"] = res.RemainingTaskBudget
            n := len(decomp.Subtasks); if n == 0 { n = 1 }
            input.Context["budget_agent_max"] = res.RemainingTaskBudget / n
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
                return TaskResult{ Success: false, ErrorMessage: fmt.Sprintf("approval request failed: %v", err) }, err
            } else if ar == nil || !ar.Approved {
                msg := reason
                if ar != nil && ar.Feedback != "" { msg = ar.Feedback }
                return TaskResult{ Success: false, ErrorMessage: fmt.Sprintf("approval denied: %s", msg) }, nil
            }
        }
    }

    // 2) Routing rules (simple, cognitive, supervisor, dag)
    // Simple threshold: prefer explicit single-subtask or low complexity
    isSimple := len(decomp.Subtasks) <= 1 || decomp.ComplexityScore < simpleThreshold

    // Cognitive program takes precedence if specified
    if decomp.CognitiveStrategy != "" && decomp.CognitiveStrategy != "direct" && decomp.CognitiveStrategy != "decompose" {
        var result TaskResult
        switch decomp.CognitiveStrategy {
        case "exploratory":
            ometrics.WorkflowsStarted.WithLabelValues("ExploratoryWorkflow", decomp.Mode).Inc()
            strategiesInput := convertToStrategiesInput(input)
            var strategiesResult strategies.TaskResult
            if err := workflow.ExecuteChildWorkflow(ctx, strategies.ExploratoryWorkflow, strategiesInput).Get(ctx, &strategiesResult); err != nil { return result, err }
            return convertFromStrategiesResult(strategiesResult), nil
        case "react":
            ometrics.WorkflowsStarted.WithLabelValues("ReactWorkflow", decomp.Mode).Inc()
            strategiesInput := convertToStrategiesInput(input)
            var strategiesResult strategies.TaskResult
            if err := workflow.ExecuteChildWorkflow(ctx, strategies.ReactWorkflow, strategiesInput).Get(ctx, &strategiesResult); err != nil { return result, err }
            return convertFromStrategiesResult(strategiesResult), nil
        case "research":
            ometrics.WorkflowsStarted.WithLabelValues("ResearchWorkflow", decomp.Mode).Inc()
            strategiesInput := convertToStrategiesInput(input)
            var strategiesResult strategies.TaskResult
            if err := workflow.ExecuteChildWorkflow(ctx, strategies.ResearchWorkflow, strategiesInput).Get(ctx, &strategiesResult); err != nil { return result, err }
            return convertFromStrategiesResult(strategiesResult), nil
        case "scientific":
            ometrics.WorkflowsStarted.WithLabelValues("ScientificWorkflow", decomp.Mode).Inc()
            strategiesInput := convertToStrategiesInput(input)
            var strategiesResult strategies.TaskResult
            if err := workflow.ExecuteChildWorkflow(ctx, strategies.ScientificWorkflow, strategiesInput).Get(ctx, &strategiesResult); err != nil { return result, err }
            return convertFromStrategiesResult(strategiesResult), nil
        default:
            logger.Warn("Unknown cognitive strategy; continuing routing", "strategy", decomp.CognitiveStrategy)
        }
    }

    // Supervisor heuristic: very large plans or explicit dependencies
    hasDeps := false
    for _, st := range decomp.Subtasks {
        if len(st.Dependencies) > 0 || len(st.Consumes) > 0 {
            hasDeps = true
            break
        }
    }

    switch {
    case isSimple:
        // Keep simple path lightweight as a child for isolation
        var result TaskResult
        ometrics.WorkflowsStarted.WithLabelValues("SimpleTaskWorkflow", "simple").Inc()
        if err := workflow.ExecuteChildWorkflow(ctx, SimpleTaskWorkflow, input).Get(ctx, &result); err != nil { return result, err }
        return result, nil

    case len(decomp.Subtasks) > 5 || hasDeps:
        var result TaskResult
        ometrics.WorkflowsStarted.WithLabelValues("SupervisorWorkflow", "complex").Inc()
        if err := workflow.ExecuteChildWorkflow(ctx, SupervisorWorkflow, input).Get(ctx, &result); err != nil { return result, err }
        return result, nil

    default:
        // Standard DAG strategy (fan-out/fan-in)
        ometrics.WorkflowsStarted.WithLabelValues("DAGWorkflow", "standard").Inc()
        strategiesInput := convertToStrategiesInput(input)
        var strategiesResult strategies.TaskResult
        if err := workflow.ExecuteChildWorkflow(ctx, strategies.DAGWorkflow, strategiesInput).Get(ctx, &strategiesResult); err != nil {
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
        Query:              input.Query,
        UserID:             input.UserID,
        TenantID:           input.TenantID,
        SessionID:          input.SessionID,
        Context:            input.Context,
        Mode:               input.Mode,
        History:            history,
        SessionCtx:         input.SessionCtx,
        RequireApproval:    input.RequireApproval,
        ApprovalTimeout:    input.ApprovalTimeout,
        BypassSingleResult: input.BypassSingleResult,
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
