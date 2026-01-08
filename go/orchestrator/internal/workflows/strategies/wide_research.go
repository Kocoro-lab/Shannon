package strategies

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/control"
)

// WideResearchConfig holds configuration for wide research workflows.
type WideResearchConfig struct {
	// MaxParallelAgents is the maximum number of concurrent research agents.
	MaxParallelAgents int
	// ResearchDepth controls how deep each agent goes (0-3).
	ResearchDepth int
	// EnableCrossVerification enables cross-agent result verification.
	EnableCrossVerification bool
	// TimeoutPerAgent is the timeout for each individual agent.
	TimeoutPerAgent time.Duration
}

// DefaultWideResearchConfig returns sensible defaults for wide research.
func DefaultWideResearchConfig() WideResearchConfig {
	return WideResearchConfig{
		MaxParallelAgents:       10,
		ResearchDepth:           2,
		EnableCrossVerification: true,
		TimeoutPerAgent:         10 * time.Minute,
	}
}

// WideResearchWorkflow implements Manus-style wide research with parallel agents.
// It spawns multiple research agents in parallel, each exploring different aspects
// of the query, then synthesizes results with optional cross-verification.
func WideResearchWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting WideResearchWorkflow",
		"query", input.Query,
		"session_id", input.SessionID,
	)

	// Extract config from context or use defaults
	config := DefaultWideResearchConfig()
	if maxAgents, ok := input.Context["max_parallel_agents"].(float64); ok {
		config.MaxParallelAgents = int(maxAgents)
	}
	if depth, ok := input.Context["research_depth"].(float64); ok {
		config.ResearchDepth = int(depth)
	}
	if verify, ok := input.Context["cross_verification"].(bool); ok {
		config.EnableCrossVerification = verify
	}

	// Configure activity options for parallel research
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: config.TimeoutPerAgent,
		HeartbeatTimeout:    60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    60 * time.Second,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Set up workflow ID for event streaming
	workflowID := input.ParentWorkflowID
	if workflowID == "" {
		workflowID = workflow.GetInfo(ctx).WorkflowExecution.ID
	}
	emitCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	// Initialize control signal handler
	controlHandler := &control.SignalHandler{
		WorkflowID:  workflowID,
		AgentID:     "wide_research",
		Logger:      logger,
		EmitCtx:     emitCtx,
		SkipSSEEmit: input.ParentWorkflowID != "",
	}
	controlHandler.Setup(ctx)

	// Emit workflow started
	_ = workflow.ExecuteActivity(emitCtx, "EmitEvent", map[string]interface{}{
		"workflow_id": workflowID,
		"event_type":  "wide_research_started",
		"payload": map[string]interface{}{
			"query":               input.Query,
			"max_parallel_agents": config.MaxParallelAgents,
			"research_depth":      config.ResearchDepth,
		},
	}).Get(ctx, nil)

	// Step 1: Decompose the research query into parallel research facets
	var decomposition activities.DecompositionResult
	if input.PreplannedDecomposition != nil {
		decomposition = *input.PreplannedDecomposition
	} else {
		decompInput := activities.DecompositionInput{
			Query:          input.Query,
			Context:        input.Context,
			AvailableTools: []string{"web_search", "web_fetch", "calculator"},
		}
		if err := workflow.ExecuteActivity(ctx, "DecomposeTask", decompInput).Get(ctx, &decomposition); err != nil {
			return TaskResult{
				Success:      false,
				ErrorMessage: fmt.Sprintf("failed to decompose research query: %v", err),
			}, nil
		}
	}

	logger.Info("Decomposed query into research facets",
		"num_facets", len(decomposition.Subtasks),
		"complexity", decomposition.ComplexityScore,
	)

	// Limit parallel agents
	numAgents := len(decomposition.Subtasks)
	if numAgents > config.MaxParallelAgents {
		numAgents = config.MaxParallelAgents
	}

	// Step 2: Launch parallel research agents
	resultChan := workflow.NewChannel(ctx)

	for i := 0; i < numAgents; i++ {
		subtask := decomposition.Subtasks[i]
		agentIndex := i

		// Launch each agent as a separate goroutine with its own context
		workflow.Go(ctx, func(gCtx workflow.Context) {

			agentID := fmt.Sprintf("research_agent_%d", agentIndex)
			logger.Info("Launching research agent",
				"agent_id", agentID,
				"subtask", subtask.Description,
			)

			// Emit agent started event
			_ = workflow.ExecuteActivity(emitCtx, "EmitEvent", map[string]interface{}{
				"workflow_id": workflowID,
				"event_type":  "agent_started",
				"payload": map[string]interface{}{
					"agent_id": agentID,
					"subtask":  subtask.Description,
					"index":    agentIndex,
				},
			}).Get(gCtx, nil)

			// Execute the research agent
			agentInput := activities.AgentExecutionInput{
				Query:   subtask.Description,
				AgentID: agentID,
				Context: input.Context,
				Mode:    "research",
			}

			var result activities.AgentExecutionResult
			err := workflow.ExecuteActivity(gCtx, "ExecuteAgent", agentInput).Get(gCtx, &result)

			// Emit agent completed event
			_ = workflow.ExecuteActivity(emitCtx, "EmitEvent", map[string]interface{}{
				"workflow_id": workflowID,
				"event_type":  "agent_completed",
				"payload": map[string]interface{}{
					"agent_id": agentID,
					"success":  err == nil && result.Success,
					"index":    agentIndex,
				},
			}).Get(gCtx, nil)

			resultChan.Send(gCtx, parallelAgentResult{
				Index:   agentIndex,
				Result:  result,
				Subtask: subtask,
				Err:     err,
			})
		})
	}

	// Collect results from all agents
	results := make([]parallelAgentResult, 0, numAgents)
	for i := 0; i < numAgents; i++ {
		var res parallelAgentResult
		resultChan.Receive(ctx, &res)
		results = append(results, res)

		// Emit progress
		_ = workflow.ExecuteActivity(emitCtx, "EmitEvent", map[string]interface{}{
			"workflow_id": workflowID,
			"event_type":  "research_progress",
			"payload": map[string]interface{}{
				"completed": i + 1,
				"total":     numAgents,
				"percent":   float64(i+1) / float64(numAgents) * 100,
			},
		}).Get(ctx, nil)
	}

	// Check for cancellation
	if controlHandler.IsCancelled() {
		return TaskResult{
			Success:      false,
			ErrorMessage: "workflow cancelled by user",
		}, nil
	}

	// Step 3: Aggregate and synthesize results
	logger.Info("Synthesizing research results",
		"num_results", len(results),
	)

	// Collect successful results
	var successfulResults []string
	var totalTokens int
	for _, res := range results {
		if res.Err == nil && res.Result.Success {
			successfulResults = append(successfulResults, res.Result.Response)
			totalTokens += res.Result.TokensUsed
		}
	}

	if len(successfulResults) == 0 {
		return TaskResult{
			Success:      false,
			ErrorMessage: "all research agents failed",
			TokensUsed:   totalTokens,
		}, nil
	}

	// Step 4: Synthesize final report
	synthesisInput := activities.AgentExecutionInput{
		Query: fmt.Sprintf(
			"Synthesize the following research findings into a comprehensive report:\n\n%s\n\nOriginal query: %s",
			formatResearchFindings(results),
			input.Query,
		),
		AgentID: "synthesis_agent",
		Context: input.Context,
		Mode:    "synthesis",
	}

	var synthesisResult activities.AgentExecutionResult
	if err := workflow.ExecuteActivity(ctx, "ExecuteAgent", synthesisInput).Get(ctx, &synthesisResult); err != nil {
		// If synthesis fails, return concatenated results
		return TaskResult{
			Success:    true,
			Result:     fmt.Sprintf("Research findings (synthesis failed):\n\n%s", formatResearchFindings(results)),
			TokensUsed: totalTokens,
			Metadata: map[string]interface{}{
				"num_agents":       numAgents,
				"successful_count": len(successfulResults),
				"synthesis_error":  err.Error(),
			},
		}, nil
	}

	totalTokens += synthesisResult.TokensUsed

	// Step 5: Optional cross-verification
	if config.EnableCrossVerification && len(successfulResults) >= 2 {
		logger.Info("Running cross-verification")

		verifyInput := activities.AgentExecutionInput{
			Query: fmt.Sprintf(
				"Verify the accuracy and consistency of this research report. Identify any conflicting information or unsupported claims:\n\n%s",
				synthesisResult.Response,
			),
			AgentID: "verification_agent",
			Context: input.Context,
			Mode:    "verification",
		}

		var verifyResult activities.AgentExecutionResult
		if err := workflow.ExecuteActivity(ctx, "ExecuteAgent", verifyInput).Get(ctx, &verifyResult); err == nil && verifyResult.Success {
			totalTokens += verifyResult.TokensUsed
			// Append verification notes if there are issues
			if verifyResult.Response != "" && verifyResult.Response != "No issues found" {
				synthesisResult.Response = fmt.Sprintf("%s\n\n---\n**Verification Notes:**\n%s", synthesisResult.Response, verifyResult.Response)
			}
		}
	}

	// Emit workflow completed
	_ = workflow.ExecuteActivity(emitCtx, "EmitEvent", map[string]interface{}{
		"workflow_id": workflowID,
		"event_type":  "wide_research_completed",
		"payload": map[string]interface{}{
			"success":          true,
			"num_agents":       numAgents,
			"successful_count": len(successfulResults),
			"tokens_used":      totalTokens,
		},
	}).Get(ctx, nil)

	return TaskResult{
		Success:    true,
		Result:     synthesisResult.Response,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"num_agents":         numAgents,
			"successful_count":   len(successfulResults),
			"research_depth":     config.ResearchDepth,
			"cross_verified":     config.EnableCrossVerification,
			"decomposition_mode": decomposition.Mode,
			"complexity_score":   decomposition.ComplexityScore,
			"cognitive_strategy": decomposition.CognitiveStrategy,
			"execution_strategy": decomposition.ExecutionStrategy,
		},
	}, nil
}

// agentResult holds the result from a parallel research agent.
type parallelAgentResult struct {
	Index   int
	Result  activities.AgentExecutionResult
	Subtask activities.Subtask
	Err     error
}

// formatResearchFindings formats agent results for synthesis.
func formatResearchFindings(results []parallelAgentResult) string {
	var findings string
	for i, res := range results {
		if res.Err == nil && res.Result.Success {
			findings += fmt.Sprintf("## Finding %d: %s\n\n%s\n\n", i+1, res.Subtask.Description, res.Result.Response)
		}
	}
	return findings
}
