package workflows

import (
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"
)

// ReactWorkflow is a wrapper for strategies.ReactWorkflow to maintain test compatibility
func ReactWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
	})
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(childCtx, strategies.ReactWorkflow, strategiesInput).Get(childCtx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// ResearchWorkflow is a wrapper for strategies.ResearchWorkflow to maintain test compatibility
func ResearchWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
	})
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(childCtx, strategies.ResearchWorkflow, strategiesInput).Get(childCtx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// ExploratoryWorkflow is a wrapper for strategies.ExploratoryWorkflow to maintain test compatibility
func ExploratoryWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
	})
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(childCtx, strategies.ExploratoryWorkflow, strategiesInput).Get(childCtx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// ScientificWorkflow is a wrapper for strategies.ScientificWorkflow to maintain test compatibility
func ScientificWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
	})
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(childCtx, strategies.ScientificWorkflow, strategiesInput).Get(childCtx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// AgentDAGWorkflow is a wrapper for strategies.DAGWorkflow to maintain test compatibility
func AgentDAGWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_REQUEST_CANCEL,
	})
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(childCtx, strategies.DAGWorkflow, strategiesInput).Get(childCtx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}
