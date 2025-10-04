package strategies

import (
	"fmt"
	"strings"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/patterns"
	"go.temporal.io/sdk/workflow"
)

// ScientificWorkflow implements hypothesis-driven investigation using patterns
// This workflow generates competing hypotheses with Chain-of-Thought, tests them with Debate,
// and refines understanding through Reflection
func ScientificWorkflow(ctx workflow.Context, input TaskInput) (TaskResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting ScientificWorkflow with patterns",
		"query", input.Query,
		"user_id", input.UserID,
	)

	// Input validation
	if err := validateInput(input); err != nil {
		return TaskResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}, err
	}

	// Load configuration
	config := getWorkflowConfig(ctx)

	// Prepare pattern options
	opts := patterns.Options{
		UserID:         input.UserID,
		BudgetAgentMax: getBudgetMax(input.Context),
		ModelTier:      determineModelTier(input.Context, "medium"),
	}

	totalTokens := 0

	// Phase 1: Generate hypotheses using Chain-of-Thought pattern
	logger.Info("Phase 1: Generating hypotheses with Chain-of-Thought")

	cotConfig := patterns.ChainOfThoughtConfig{
		MaxSteps:              config.ScientificMaxHypotheses,
		RequireExplanation:    true,
		ShowIntermediateSteps: true,
		ModelTier:             opts.ModelTier,
		PromptTemplate: `Generate {query} distinct, testable hypotheses for: %s
Think step-by-step:
→ What are the key aspects of this problem?
→ What could be different explanations?
→ How can each hypothesis be tested?
Therefore: List exactly %d hypotheses, each starting with "Hypothesis N:"`,
	}

	hypothesisQuery := fmt.Sprintf(
		"Generate exactly %d distinct, testable hypotheses for: %s",
		config.ScientificMaxHypotheses,
		input.Query,
	)

	// Ensure parent workflow ID is included in context for downstream activities
	cotCtx := make(map[string]interface{})
	for k, v := range input.Context {
		cotCtx[k] = v
	}
	if input.ParentWorkflowID != "" {
		cotCtx["parent_workflow_id"] = input.ParentWorkflowID
	}

	// Memory retrieval with gate precedence (hierarchical > simple session)
	hierarchicalVersion := workflow.GetVersion(ctx, "memory_retrieval_v1", workflow.DefaultVersion, 1)
	sessionVersion := workflow.GetVersion(ctx, "session_memory_v1", workflow.DefaultVersion, 1)

	var memoryItems []map[string]interface{}
	if hierarchicalVersion >= 1 && input.SessionID != "" {
		// Use hierarchical memory (combines recent + semantic)
		var hierMemory activities.FetchHierarchicalMemoryResult
		_ = workflow.ExecuteActivity(ctx, activities.FetchHierarchicalMemory,
			activities.FetchHierarchicalMemoryInput{
				Query:        input.Query,
				SessionID:    input.SessionID,
				TenantID:     input.TenantID,
				RecentTopK:   5,    // Fixed for determinism
				SemanticTopK: 5,    // Fixed for determinism
				Threshold:    0.75, // Fixed semantic threshold
			}).Get(ctx, &hierMemory)

		if len(hierMemory.Items) > 0 {
			memoryItems = hierMemory.Items
			cotCtx["agent_memory"] = memoryItems
			logger.Info("Injected hierarchical memory into scientific CoT context",
				"session_id", input.SessionID,
				"memory_items", len(hierMemory.Items),
				"sources", hierMemory.Sources,
			)
		}
	} else if sessionVersion >= 1 && input.SessionID != "" {
		// Fallback to simple session memory if hierarchical not enabled
		var sessionMemory activities.FetchSessionMemoryResult
		_ = workflow.ExecuteActivity(ctx, activities.FetchSessionMemory,
			activities.FetchSessionMemoryInput{
				SessionID: input.SessionID,
				TenantID:  input.TenantID,
				TopK:      20, // Fixed for determinism
			}).Get(ctx, &sessionMemory)

		if len(sessionMemory.Items) > 0 {
			memoryItems = sessionMemory.Items
			cotCtx["agent_memory"] = memoryItems
			logger.Info("Injected session memory into scientific CoT context",
				"session_id", input.SessionID,
				"memory_items", len(sessionMemory.Items),
			)
		}
	}

	cotResult, err := patterns.ChainOfThought(
		ctx,
		hypothesisQuery,
		cotCtx,
		input.SessionID,
		convertHistoryForAgent(input.History),
		cotConfig,
		opts,
	)

	if err != nil {
		logger.Error("Hypothesis generation failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to generate hypotheses: %v", err),
		}, err
	}

	totalTokens += cotResult.TotalTokens

	// Extract hypotheses from Chain-of-Thought reasoning
	hypotheses := extractHypothesesFromSteps(cotResult.ReasoningSteps, cotResult.FinalAnswer)

	logger.Info("Generated hypotheses",
		"count", len(hypotheses),
		"confidence", cotResult.Confidence,
	)

	// Phase 2: Test competing hypotheses using Debate pattern
	logger.Info("Phase 2: Testing hypotheses with multi-agent Debate")

	// Create perspectives for each hypothesis
	perspectives := make([]string, 0, len(hypotheses))
	for i := range hypotheses {
		perspectives = append(perspectives, fmt.Sprintf("hypothesis_%d_advocate", i+1))
	}

	debateConfig := patterns.DebateConfig{
		NumDebaters:      len(hypotheses),
		MaxRounds:        config.ScientificMaxIterations,
		Perspectives:     perspectives,
		RequireConsensus: false,
		ModeratorEnabled: true,
		VotingEnabled:    true,
		ModelTier:        opts.ModelTier,
	}

	// Prepare debate context with hypotheses
	debateContext := make(map[string]interface{})
	for k, v := range input.Context {
		debateContext[k] = v
	}
	if input.ParentWorkflowID != "" {
		debateContext["parent_workflow_id"] = input.ParentWorkflowID
	}
	debateContext["hypotheses"] = hypotheses
	debateContext["original_query"] = input.Query
	debateContext["confidence_threshold"] = config.ScientificConfidenceThreshold

	// Inject memory into debate context (reuse from earlier fetch)
	if len(memoryItems) > 0 {
		debateContext["agent_memory"] = memoryItems
	}

	debateQuery := fmt.Sprintf(
		"Test and evaluate these competing hypotheses for '%s':\n%s\n"+
			"Each debater should:\n"+
			"1. Present evidence supporting their hypothesis\n"+
			"2. Challenge contradictory hypotheses\n"+
			"3. Acknowledge limitations",
		input.Query,
		strings.Join(hypotheses, "\n"),
	)

	debateResult, err := patterns.Debate(
		ctx,
		debateQuery,
		debateContext,
		input.SessionID,
		convertHistoryForAgent(input.History),
		debateConfig,
		opts,
	)

	if err != nil {
		logger.Error("Hypothesis testing via debate failed", "error", err)
		return TaskResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Hypothesis testing failed: %v", err),
		}, err
	}

	totalTokens += debateResult.TotalTokens

	// Phase 3: Synthesize findings with Tree-of-Thoughts for exploration
	logger.Info("Phase 3: Exploring implications with Tree-of-Thoughts")

	totConfig := patterns.TreeOfThoughtsConfig{
		MaxDepth:          3,
		BranchingFactor:   2,
		EvaluationMethod:  "scoring",
		PruningThreshold:  1.0 - config.ScientificConfidenceThreshold,
		ExplorationBudget: 10,
		BacktrackEnabled:  false,
		ModelTier:         opts.ModelTier,
	}

	// Explore implications of winning hypothesis
	totQuery := fmt.Sprintf(
		"Based on the winning hypothesis: %s\n"+
			"What are the implications and next steps for: %s",
		debateResult.WinningArgument,
		input.Query,
	)

	totContext := make(map[string]interface{})
	for k, v := range input.Context {
		totContext[k] = v
	}
	if input.ParentWorkflowID != "" {
		totContext["parent_workflow_id"] = input.ParentWorkflowID
	}
	totContext["winning_hypothesis"] = debateResult.WinningArgument
	totContext["debate_positions"] = debateResult.Positions
	totContext["consensus_reached"] = debateResult.ConsensusReached

	// Inject memory into ToT context (reuse from earlier fetch)
	if len(memoryItems) > 0 {
		totContext["agent_memory"] = memoryItems
	}

	// Context compression before Tree-of-Thoughts (version-gated for determinism)
	compressionVersion := workflow.GetVersion(ctx, "context_compress_v1", workflow.DefaultVersion, 1)
	if compressionVersion >= 1 && input.SessionID != "" && len(input.History) > 20 {
		// Check if compression is needed with rate limiting
		estimatedTokens := activities.EstimateTokens(convertHistoryForAgent(input.History))

		var checkResult activities.CheckCompressionNeededResult
		err := workflow.ExecuteActivity(ctx, "CheckCompressionNeeded",
			activities.CheckCompressionNeededInput{
				SessionID:       input.SessionID,
				MessageCount:    len(input.History),
				EstimatedTokens: estimatedTokens,
				ModelTier:       opts.ModelTier,
			}).Get(ctx, &checkResult)

		if err == nil && checkResult.ShouldCompress {
			logger.Info("Triggering context compression in scientific workflow",
				"session_id", input.SessionID,
				"reason", checkResult.Reason,
				"message_count", len(input.History),
			)

			// Compress context via activity
			var compressResult activities.CompressContextResult
			err = workflow.ExecuteActivity(ctx, activities.CompressAndStoreContext,
				activities.CompressContextInput{
					SessionID:        input.SessionID,
					History:          convertHistoryMapForCompression(input.History),
					TargetTokens:     int(float64(activities.GetModelWindowSize(opts.ModelTier)) * 0.375),
					ParentWorkflowID: input.ParentWorkflowID,
				}).Get(ctx, &compressResult)

			if err == nil && compressResult.Summary != "" && compressResult.Stored {
				logger.Info("Context compressed and stored",
					"session_id", input.SessionID,
					"summary_length", len(compressResult.Summary),
				)

				// Update compression state in session
				var updateResult activities.UpdateCompressionStateResult
				_ = workflow.ExecuteActivity(ctx, "UpdateCompressionStateActivity",
					activities.UpdateCompressionStateInput{
						SessionID:    input.SessionID,
						MessageCount: len(input.History),
					}).Get(ctx, &updateResult)
			}
		}
	}

	totResult, err := patterns.TreeOfThoughts(
		ctx,
		totQuery,
		totContext,
		input.SessionID,
		convertHistoryForAgent(input.History),
		totConfig,
		opts,
	)

	if err != nil {
		logger.Warn("Tree-of-Thoughts exploration failed, using debate result", "error", err)
		// Fall back to debate result
		totResult = &patterns.TreeOfThoughtsResult{
			BestSolution: debateResult.FinalPosition,
			Confidence:   0.7,
		}
	}

	totalTokens += totResult.TotalTokens

	// Phase 4: Final quality check with Reflection
	logger.Info("Phase 4: Applying reflection for final synthesis")

	reflectionConfig := patterns.ReflectionConfig{
		Enabled:             true,
		MaxRetries:          2,
		ConfidenceThreshold: config.ScientificConfidenceThreshold,
		Criteria:            []string{"scientific_rigor", "evidence_quality", "logical_consistency"},
		TimeoutMs:           30000,
	}

	// Create comprehensive result for reflection
	comprehensiveResult := fmt.Sprintf(
		"Scientific Investigation Results:\n\n"+
			"Hypotheses Tested:\n%s\n\n"+
			"Debate Outcome:\n%s\n\n"+
			"Implications:\n%s\n\n"+
			"Confidence Level: %.2f%%",
		strings.Join(hypotheses, "\n"),
		debateResult.FinalPosition,
		totResult.BestSolution,
		totResult.Confidence*100,
	)

	// Mock agent results for reflection
	agentResults := []activities.AgentExecutionResult{
		{
			Response:   comprehensiveResult,
			Success:    true,
			TokensUsed: totalTokens,
		},
	}

	finalResult, finalConfidence, reflectionTokens, err := patterns.ReflectOnResult(
		ctx,
		input.Query,
		comprehensiveResult,
		agentResults,
		input.Context,
		reflectionConfig,
		opts,
	)

	if err != nil {
		logger.Warn("Reflection failed, using synthesis result", "error", err)
		finalResult = comprehensiveResult
		finalConfidence = totResult.Confidence
	} else {
		totalTokens += reflectionTokens
	}

	// Build structured scientific report
	scientificReport := buildScientificReport(
		input.Query,
		hypotheses,
		debateResult,
		totResult,
		finalResult,
		finalConfidence,
	)

	// Update session
	if input.SessionID != "" {
		if err := updateSession(ctx, input.SessionID, scientificReport, totalTokens, len(hypotheses)*3); err != nil {
			logger.Warn("Failed to update session",
				"error", err,
				"session_id", input.SessionID,
			)
		}
	}

	logger.Info("ScientificWorkflow completed",
		"total_tokens", totalTokens,
		"hypotheses_tested", len(hypotheses),
		"consensus_reached", debateResult.ConsensusReached,
		"final_confidence", finalConfidence,
	)

	return TaskResult{
		Result:     scientificReport,
		Success:    true,
		TokensUsed: totalTokens,
		Metadata: map[string]interface{}{
			"workflow_type":     "scientific",
			"patterns_used":     []string{"chain_of_thought", "debate", "tree_of_thoughts", "reflection"},
			"hypotheses_count":  len(hypotheses),
			"consensus_reached": debateResult.ConsensusReached,
			"final_confidence":  finalConfidence,
			"debate_rounds":     debateResult.Rounds,
			"exploration_depth": totResult.TreeDepth,
		},
	}, nil
}

// extractHypothesesFromSteps extracts hypotheses from Chain-of-Thought reasoning
func extractHypothesesFromSteps(steps []string, finalAnswer string) []string {
	hypotheses := []string{}

	// First check the final answer for structured hypotheses
	lines := strings.Split(finalAnswer, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(line), "hypothesis") {
			// Extract the hypothesis after the colon
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				hypothesis := strings.TrimSpace(parts[1])
				if hypothesis != "" {
					hypotheses = append(hypotheses, hypothesis)
				}
			}
		}
	}

	// If not enough in final answer, check reasoning steps
	if len(hypotheses) < 3 {
		for _, step := range steps {
			if strings.Contains(strings.ToLower(step), "hypothesis") {
				parts := strings.SplitN(step, ":", 2)
				if len(parts) == 2 {
					hypothesis := strings.TrimSpace(parts[1])
					if hypothesis != "" && !contains(hypotheses, hypothesis) {
						hypotheses = append(hypotheses, hypothesis)
					}
				}
			}
		}
	}

	// Fallback: if still no hypotheses, use the reasoning steps themselves
	if len(hypotheses) == 0 && len(steps) > 0 {
		for i, step := range steps {
			if i >= 3 {
				break
			}
			hypotheses = append(hypotheses, step)
		}
	}

	return hypotheses
}

// buildScientificReport creates a structured scientific investigation report
func buildScientificReport(
	query string,
	hypotheses []string,
	debateResult *patterns.DebateResult,
	totResult *patterns.TreeOfThoughtsResult,
	finalSynthesis string,
	confidence float64,
) string {
	var report strings.Builder

	report.WriteString(fmt.Sprintf("# Scientific Investigation Report\n\n"))
	report.WriteString(fmt.Sprintf("**Research Question:** %s\n\n", query))

	report.WriteString("## Hypotheses Tested\n\n")
	for i, hypothesis := range hypotheses {
		report.WriteString(fmt.Sprintf("%d. %s\n", i+1, hypothesis))
	}

	report.WriteString("\n## Investigation Results\n\n")
	report.WriteString(fmt.Sprintf("**Winning Hypothesis:** %s\n\n", debateResult.WinningArgument))
	report.WriteString(fmt.Sprintf("**Consensus Reached:** %v\n", debateResult.ConsensusReached))
	report.WriteString(fmt.Sprintf("**Debate Rounds:** %d\n\n", debateResult.Rounds))

	if len(debateResult.Votes) > 0 {
		report.WriteString("### Hypothesis Support (Votes)\n")
		for agent, votes := range debateResult.Votes {
			report.WriteString(fmt.Sprintf("- %s: %d\n", agent, votes))
		}
		report.WriteString("\n")
	}

	report.WriteString("## Implications and Next Steps\n\n")
	report.WriteString(totResult.BestSolution)
	report.WriteString("\n\n")

	report.WriteString("## Final Synthesis\n\n")
	report.WriteString(finalSynthesis)
	report.WriteString("\n\n")

	report.WriteString(fmt.Sprintf("## Confidence Assessment\n\n"))
	report.WriteString(fmt.Sprintf("**Overall Confidence:** %.1f%%\n", confidence*100))
	report.WriteString(fmt.Sprintf("**Exploration Depth:** %d levels\n", totResult.TreeDepth))
	report.WriteString(fmt.Sprintf("**Total Thoughts Explored:** %d\n", totResult.TotalThoughts))

	return report.String()
}

// contains checks if a string slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// convertHistoryMapForCompression converts Message history to map format for compression
func convertHistoryMapForCompression(messages []Message) []map[string]string {
	result := make([]map[string]string, len(messages))
	for i, msg := range messages {
		result[i] = map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}
	return result
}
