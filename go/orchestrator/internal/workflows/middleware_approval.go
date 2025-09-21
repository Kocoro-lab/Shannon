package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
)

// CheckApprovalPolicy evaluates whether human approval is required based on
// decomposition results and simple policy defaults.
func CheckApprovalPolicy(_ workflow.Context, input TaskInput, decomp activities.DecompositionResult) (bool, string) {
	policy := activities.ApprovalPolicy{
		ComplexityThreshold: 0.8,
		TokenBudgetExceeded: false,
		RequireForTools:     []string{"file_system", "code_execution"},
	}
	return CheckApprovalPolicyWith(policy, input, decomp)
}

// CheckApprovalPolicyWith evaluates approval against a provided policy
func CheckApprovalPolicyWith(policy activities.ApprovalPolicy, input TaskInput, decomp activities.DecompositionResult) (bool, string) {

	ctx := map[string]interface{}{
		"complexity_score": decomp.ComplexityScore,
		"query":            input.Query,
	}
	// Aggregate suggested tools (best-effort)
	tools := []string{}
	for _, st := range decomp.Subtasks {
		if len(st.SuggestedTools) > 0 {
			tools = append(tools, st.SuggestedTools...)
		}
	}
	if len(tools) > 0 {
		ctx["tools_to_use"] = tools
	}

	return activities.EvaluateApprovalPolicy(policy, ctx)
}

// RequestAndWaitApproval requests approval and waits on a signal until the
// timeout is reached. Returns the final approval result.
func RequestAndWaitApproval(ctx workflow.Context, input TaskInput, reason string) (*activities.HumanApprovalResult, error) {
    logger := workflow.GetLogger(ctx)

	// Request approval via activity
	var approval activities.HumanApprovalResult
	err := workflow.ExecuteActivity(ctx, constants.RequestApprovalActivity, activities.HumanApprovalInput{
		SessionID:      input.SessionID,
		WorkflowID:     workflow.GetInfo(ctx).WorkflowExecution.ID,
		RunID:          workflow.GetInfo(ctx).WorkflowExecution.RunID,
		Query:          input.Query,
		Context:        input.Context,
		ProposedAction: "execute_decomposition_plan",
		Reason:         reason,
		Metadata:       map[string]interface{}{"subtasks": len(input.History)},
	}).Get(ctx, &approval)
    if err != nil {
        return nil, err
    }

    // Emit APPROVAL_REQUESTED event via activity
    _ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
        EventType:  activities.StreamEventApprovalRequested,
        AgentID:    "orchestrator",
        Message:    "Approval requested: " + reason + ", id=" + approval.ApprovalID,
        Timestamp:  workflow.Now(ctx),
    }).Get(ctx, nil)

    logger.Info("Waiting for human approval", "approval_id", approval.ApprovalID)

	// Await either signal or timeout
	sigName := "human-approval-" + approval.ApprovalID
	ch := workflow.GetSignalChannel(ctx, sigName)
	sel := workflow.NewSelector(ctx)

	timeout := 30 * time.Minute
	if input.ApprovalTimeout > 0 {
		timeout = time.Duration(input.ApprovalTimeout) * time.Second
	}
	timer := workflow.NewTimer(ctx, timeout)

	var result activities.HumanApprovalResult
	var timedOut bool

	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {
		c.Receive(ctx, &result)
	})
	sel.AddFuture(timer, func(f workflow.Future) {
		timedOut = true
		result = activities.HumanApprovalResult{Approved: false, Feedback: "approval timeout"}
	})
	sel.Select(ctx)

    if timedOut {
        logger.Warn("Approval timed out", "approval_id", approval.ApprovalID)
    }
    // Emit APPROVAL_DECISION event
    decision := "denied"
    if result.Approved {
        decision = "approved"
    }
    _ = workflow.ExecuteActivity(ctx, "EmitTaskUpdate", activities.EmitTaskUpdateInput{
        WorkflowID: workflow.GetInfo(ctx).WorkflowExecution.ID,
        EventType:  activities.StreamEventApprovalDecision,
        AgentID:    "orchestrator",
        Message:    "Approval " + decision + ": id=" + approval.ApprovalID + ", feedback=" + result.Feedback,
        Timestamp:  workflow.Now(ctx),
    }).Get(ctx, nil)
    return &result, nil
}
