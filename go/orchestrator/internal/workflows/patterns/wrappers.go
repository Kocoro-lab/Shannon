package patterns

import (
    "strings"

    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
    "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
    imodels "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/models"
    pricing "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
    "go.temporal.io/sdk/workflow"
)

// reactPattern wraps ReactLoop to satisfy the Pattern interface
type reactPattern struct{}

func (reactPattern) GetType() PatternType { return PatternReact }

func (reactPattern) GetCapabilities() []PatternCapability {
	return []PatternCapability{CapabilityExploration, CapabilityStepByStep}
}

func (reactPattern) EstimateTokens(input PatternInput) int {
	// Simple heuristic: base per-iteration budget
	if input.BudgetMax > 0 {
		return input.BudgetMax
	}
	return 2000
}

func (reactPattern) Execute(ctx workflow.Context, input PatternInput) (*PatternResult, error) {
	// Config from input.Config when provided
	cfg := ReactConfig{MaxIterations: 8, ObservationWindow: 3, MaxObservations: 100, MaxThoughts: 50, MaxActions: 50}
	if v, ok := input.Config.(ReactConfig); ok {
		cfg = v
	}

	opts := Options{
		BudgetAgentMax: input.BudgetMax,
		SessionID:      input.SessionID,
		UserID:         input.UserID,
		ModelTier:      input.ModelTier,
		Context:        input.Context,
	}

	res, err := ReactLoop(ctx, input.Query, input.Context, input.SessionID, input.History, cfg, opts)
	if err != nil {
		return nil, err
	}
	return &PatternResult{Result: res.FinalResult, TokensUsed: res.TotalTokens, Confidence: 0.6, Metadata: map[string]interface{}{"iterations": res.Iterations}}, nil
}

// chainOfThoughtPattern wraps ChainOfThought
type chainOfThoughtPattern struct{}

func (chainOfThoughtPattern) GetType() PatternType { return PatternChainOfThought }

func (chainOfThoughtPattern) GetCapabilities() []PatternCapability {
	return []PatternCapability{CapabilityStepByStep}
}

func (chainOfThoughtPattern) EstimateTokens(input PatternInput) int {
	if input.BudgetMax > 0 {
		return input.BudgetMax
	}
	return 1500
}

func (chainOfThoughtPattern) Execute(ctx workflow.Context, input PatternInput) (*PatternResult, error) {
	cfg := ChainOfThoughtConfig{MaxSteps: 5, RequireExplanation: false, ShowIntermediateSteps: false}
	if v, ok := input.Config.(ChainOfThoughtConfig); ok {
		cfg = v
	}

	opts := Options{BudgetAgentMax: input.BudgetMax, SessionID: input.SessionID, UserID: input.UserID, ModelTier: input.ModelTier, Context: input.Context}
	res, err := ChainOfThought(ctx, input.Query, input.Context, input.SessionID, input.History, cfg, opts)
	if err != nil {
		return nil, err
	}
	return &PatternResult{Result: res.FinalAnswer, TokensUsed: res.TotalTokens, Confidence: res.Confidence}, nil
}

// debatePattern wraps Debate
type debatePattern struct{}

func (debatePattern) GetType() PatternType { return PatternDebate }

func (debatePattern) GetCapabilities() []PatternCapability {
	return []PatternCapability{CapabilityMultiPerspective, CapabilityConsensusBuilding, CapabilityConflictResolution}
}

func (debatePattern) EstimateTokens(input PatternInput) int {
	if input.BudgetMax > 0 {
		return input.BudgetMax
	}
	return 4000
}

func (debatePattern) Execute(ctx workflow.Context, input PatternInput) (*PatternResult, error) {
	cfg := DebateConfig{NumDebaters: 3, MaxRounds: 3}
	if v, ok := input.Config.(DebateConfig); ok {
		cfg = v
	}

	opts := Options{BudgetAgentMax: input.BudgetMax, SessionID: input.SessionID, UserID: input.UserID, ModelTier: input.ModelTier, Context: input.Context}
	res, err := Debate(ctx, input.Query, input.Context, input.SessionID, input.History, cfg, opts)
	if err != nil {
		return nil, err
	}
	return &PatternResult{Result: res.FinalPosition, TokensUsed: res.TotalTokens, Confidence: 0.6}, nil
}

// treeOfThoughtsPattern wraps TreeOfThoughts
type treeOfThoughtsPattern struct{}

func (treeOfThoughtsPattern) GetType() PatternType { return PatternTreeOfThoughts }

func (treeOfThoughtsPattern) GetCapabilities() []PatternCapability {
	return []PatternCapability{CapabilityExploration}
}

func (treeOfThoughtsPattern) EstimateTokens(input PatternInput) int {
	if input.BudgetMax > 0 {
		return input.BudgetMax
	}
	return 5000
}

func (treeOfThoughtsPattern) Execute(ctx workflow.Context, input PatternInput) (*PatternResult, error) {
	cfg := TreeOfThoughtsConfig{MaxDepth: 3, BranchingFactor: 3}
	if v, ok := input.Config.(TreeOfThoughtsConfig); ok {
		cfg = v
	}

	opts := Options{BudgetAgentMax: input.BudgetMax, SessionID: input.SessionID, UserID: input.UserID, ModelTier: input.ModelTier, Context: input.Context}
	res, err := TreeOfThoughts(ctx, input.Query, input.Context, input.SessionID, input.History, cfg, opts)
	if err != nil {
		return nil, err
	}
	return &PatternResult{Result: res.BestSolution, TokensUsed: res.TotalTokens, Confidence: res.Confidence}, nil
}

// reflectionPattern wraps a single-pass answer and optional reflection improvement
type reflectionPattern struct{}

func (reflectionPattern) GetType() PatternType { return PatternReflection }

func (reflectionPattern) GetCapabilities() []PatternCapability {
	return []PatternCapability{CapabilityIterativeImprovement}
}

func (reflectionPattern) EstimateTokens(input PatternInput) int {
	if input.BudgetMax > 0 {
		return input.BudgetMax
	}
	return 2000
}

func (reflectionPattern) Execute(ctx workflow.Context, input PatternInput) (*PatternResult, error) {
	// Get initial result via a single agent call
	var initial activities.AgentExecutionResult
    if input.BudgetMax > 0 {
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if input.Context != nil {
            if p, ok := input.Context["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        err := workflow.ExecuteActivity(ctx,
            constants.ExecuteAgentWithBudgetActivity,
            activities.BudgetedAgentInput{
                AgentInput: activities.AgentExecutionInput{
                    Query:     input.Query,
                    AgentID:   "reflection-initial",
                    Context:   input.Context,
                    Mode:      "standard",
                    SessionID: input.SessionID,
                    History:   input.History,
                    ParentWorkflowID: wid,
                },
                MaxTokens: input.BudgetMax,
                UserID:    input.UserID,
                TaskID:    wid,
                ModelTier: input.ModelTier,
            },
        ).Get(ctx, &initial)
		if err != nil {
			return nil, err
		}
    } else {
        if err := workflow.ExecuteActivity(ctx,
            activities.ExecuteAgent,
            activities.AgentExecutionInput{
                Query:     input.Query,
                AgentID:   "reflection-initial",
                Context:   input.Context,
                Mode:      "standard",
                SessionID: input.SessionID,
                History:   input.History,
                ParentWorkflowID: func() string {
                    if input.Context != nil {
                        if p, ok := input.Context["parent_workflow_id"].(string); ok && p != "" {
                            return p
                        }
                    }
                    return ""
                }(),
            },
        ).Get(ctx, &initial); err != nil {
            return nil, err
        }

        // Record initial reflection usage when not budgeted (avoid double-recording)
        wid := workflow.GetInfo(ctx).WorkflowExecution.ID
        if input.Context != nil {
            if p, ok := input.Context["parent_workflow_id"].(string); ok && p != "" {
                wid = p
            }
        }
        inTok := initial.InputTokens
        outTok := initial.OutputTokens
        if inTok == 0 && outTok == 0 && initial.TokensUsed > 0 {
            inTok = initial.TokensUsed * 6 / 10
            outTok = initial.TokensUsed - inTok
        }
        model := initial.ModelUsed
        if strings.TrimSpace(model) == "" {
            if m := pricing.GetPriorityOneModel(input.ModelTier); m != "" {
                model = m
            }
        }
        provider := initial.Provider
        if strings.TrimSpace(provider) == "" {
            provider = imodels.DetectProvider(model)
        }
        _ = workflow.ExecuteActivity(ctx, constants.RecordTokenUsageActivity, activities.TokenUsageInput{
            UserID:       input.UserID,
            SessionID:    input.SessionID,
            TaskID:       wid,
            AgentID:      "reflection-initial",
            Model:        model,
            Provider:     provider,
            InputTokens:  inTok,
            OutputTokens: outTok,
            Metadata:     map[string]interface{}{"phase": "reflection_initial"},
        }).Get(ctx, nil)
    }

	// Apply reflection with defaults
	rcfg := ReflectionConfig{Enabled: true, MaxRetries: 1, ConfidenceThreshold: 0.7, Criteria: []string{"completeness", "correctness", "clarity"}, TimeoutMs: 30000}
	if v, ok := input.Config.(ReflectionConfig); ok {
		rcfg = v
	}

	opts := Options{BudgetAgentMax: input.BudgetMax, SessionID: input.SessionID, UserID: input.UserID, ModelTier: input.ModelTier, Context: input.Context}
	improved, score, extraTokens, err := ReflectOnResult(ctx, input.Query, initial.Response, []activities.AgentExecutionResult{initial}, input.Context, rcfg, opts)
	if err != nil {
		// If reflection fails, return initial
		return &PatternResult{Result: initial.Response, TokensUsed: initial.TokensUsed, Confidence: 0.5}, nil
	}
	return &PatternResult{Result: improved, TokensUsed: initial.TokensUsed + extraTokens, Confidence: score}, nil
}
