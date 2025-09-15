package workflows

import (
	"go.temporal.io/sdk/workflow"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"
)

// ReactWorkflow is a wrapper for strategies.ReactWorkflow to maintain test compatibility
func ReactWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(ctx, strategies.ReactWorkflow, strategiesInput).Get(ctx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// ResearchWorkflow is a wrapper for strategies.ResearchWorkflow to maintain test compatibility
func ResearchWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(ctx, strategies.ResearchWorkflow, strategiesInput).Get(ctx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// ExploratoryWorkflow is a wrapper for strategies.ExploratoryWorkflow to maintain test compatibility
func ExploratoryWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(ctx, strategies.ExploratoryWorkflow, strategiesInput).Get(ctx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// ScientificWorkflow is a wrapper for strategies.ScientificWorkflow to maintain test compatibility
func ScientificWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(ctx, strategies.ScientificWorkflow, strategiesInput).Get(ctx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}

// AgentDAGWorkflow is a wrapper for strategies.DAGWorkflow to maintain test compatibility
func AgentDAGWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	strategiesInput := convertToStrategiesInput(input)
	var strategiesResult strategies.TaskResult
	err := workflow.ExecuteChildWorkflow(ctx, strategies.DAGWorkflow, strategiesInput).Get(ctx, &strategiesResult)
	if err != nil {
		return TaskResult{}, err
	}
	return convertFromStrategiesResult(strategiesResult), nil
}