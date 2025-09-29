package registry

import (
	"database/sql"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/strategies"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

// OrchestratorRegistry implements the Registry interface
type OrchestratorRegistry struct {
	config         *RegistryConfig
	logger         *zap.Logger
	db             *sql.DB
	sessionManager *session.Manager
}

// NewOrchestratorRegistry creates a new registry instance
func NewOrchestratorRegistry(
	config *RegistryConfig,
	logger *zap.Logger,
	db *sql.DB,
	sessionManager *session.Manager,
) *OrchestratorRegistry {
	return &OrchestratorRegistry{
		config:         config,
		logger:         logger,
		db:             db,
		sessionManager: sessionManager,
	}
}

// RegisterWorkflows registers all workflows based on configuration
func (r *OrchestratorRegistry) RegisterWorkflows(w worker.Worker) error {
	r.logger.Info("Registering workflows")

	// Core workflows - always registered
	w.RegisterWorkflow(workflows.OrchestratorWorkflow)
	w.RegisterWorkflow(workflows.SimpleTaskWorkflow)
	w.RegisterWorkflow(workflows.SupervisorWorkflow)

	// Cognitive workflows that need pattern migration
	w.RegisterWorkflow(workflows.ExploratoryWorkflow)
	w.RegisterWorkflow(workflows.ScientificWorkflow)
	r.logger.Info("Registered core workflows")

	// Optional workflows based on configuration
	if r.config.EnableStreamingWorkflows {
		w.RegisterWorkflow(workflows.StreamingWorkflow)
		w.RegisterWorkflow(workflows.ParallelStreamingWorkflow)
		r.logger.Info("Registered streaming workflows")
	}

	// Strategy workflows (pattern-based)
	w.RegisterWorkflow(strategies.DAGWorkflow)
	w.RegisterWorkflow(strategies.ReactWorkflow)
	w.RegisterWorkflow(strategies.ResearchWorkflow)
	r.logger.Info("Registered strategy workflows")

	r.logger.Info("All workflows registered successfully")
	return nil
}

// RegisterActivities registers all activities based on configuration
func (r *OrchestratorRegistry) RegisterActivities(w worker.Worker) error {
	r.logger.Info("Registering activities")

	// Construct activity receiver with dependencies
	acts := activities.NewActivities(r.sessionManager, r.logger)

	// Core activities
	w.RegisterActivity(activities.ExecuteAgent)
	w.RegisterActivity(activities.ExecuteSimpleTask) // Consolidated activity for simple tasks
	w.RegisterActivity(activities.SynthesizeResults)
	// LLM-backed synthesis (can be selected via workflow versioning)
	w.RegisterActivity(activities.SynthesizeResultsLLM)
	// Reflection activity for quality evaluation
	w.RegisterActivity(acts.EvaluateResult)
	// Configuration activity
	w.RegisterActivity(activities.GetWorkflowConfig)
	// Context compression + store
	w.RegisterActivity(activities.CompressAndStoreContext)
	// Compression rate limiting activities
	w.RegisterActivity(acts.CheckCompressionNeeded)
	w.RegisterActivity(acts.UpdateCompressionStateActivity)

	// Vector intelligence activities
	w.RegisterActivity(activities.RecordQuery)
	w.RegisterActivity(activities.FetchSessionMemory)
	// Agent-scoped memory activities (agent_memory_v1)
	w.RegisterActivity(activities.FetchAgentMemory)
	w.RegisterActivity(activities.RecordAgentMemory)
	// Semantic memory activities (Phase 1.1)
	w.RegisterActivity(activities.FetchSemanticMemory)
	w.RegisterActivity(activities.FetchHierarchicalMemory)

	// Enhanced supervisor memory activities
	w.RegisterActivity(activities.FetchSupervisorMemory)
	w.RegisterActivity(activities.RecordDecomposition)

	// Dynamic team authorization
	w.RegisterActivity(activities.AuthorizeTeamAction)

	// P2P mailbox + workspace (receiver methods)
	w.RegisterActivityWithOptions(acts.SendAgentMessage, activity.RegisterOptions{Name: constants.SendAgentMessageActivity})
	w.RegisterActivityWithOptions(acts.FetchAgentMessages, activity.RegisterOptions{Name: constants.FetchAgentMessagesActivity})
	w.RegisterActivityWithOptions(acts.WorkspaceAppend, activity.RegisterOptions{Name: constants.WorkspaceAppendActivity})
	w.RegisterActivityWithOptions(acts.WorkspaceList, activity.RegisterOptions{Name: constants.WorkspaceListActivity})
	// Structured protocol wrappers
	w.RegisterActivityWithOptions(acts.SendTaskRequest, activity.RegisterOptions{Name: constants.SendTaskRequestActivity})
	w.RegisterActivityWithOptions(acts.SendTaskOffer, activity.RegisterOptions{Name: constants.SendTaskOfferActivity})
	w.RegisterActivityWithOptions(acts.SendTaskAccept, activity.RegisterOptions{Name: constants.SendTaskAcceptActivity})

	// Session activities - register with consistent naming
	w.RegisterActivityWithOptions(acts.DecomposeTask, activity.RegisterOptions{Name: constants.DecomposeTaskActivity})
	// Legacy activity name for Temporal replay compatibility
	w.RegisterActivityWithOptions(acts.AnalyzeComplexity, activity.RegisterOptions{Name: constants.AnalyzeComplexityActivity})
	w.RegisterActivityWithOptions(acts.UpdateSessionResult, activity.RegisterOptions{
		Name: constants.UpdateSessionResultActivity,
	})

	// Human intervention activities
	if r.config.EnableApprovalWorkflows {
		humanActivities := activities.NewHumanInterventionActivities()
		w.RegisterActivityWithOptions(humanActivities.RequestApproval, activity.RegisterOptions{
			Name: constants.RequestApprovalActivity,
		})
		w.RegisterActivityWithOptions(humanActivities.ProcessApprovalResponse, activity.RegisterOptions{
			Name: constants.ProcessApprovalResponseActivity,
		})
		w.RegisterActivityWithOptions(humanActivities.GetApprovalStatus, activity.RegisterOptions{
			Name: constants.GetApprovalStatusActivity,
		})
		r.logger.Info("Registered human intervention activities")
	}

	// Streaming activities
	if r.config.EnableStreamingWorkflows {
		streamingActivities := activities.NewStreamingActivities()
		w.RegisterActivityWithOptions(streamingActivities.StreamExecute, activity.RegisterOptions{
			Name: constants.StreamExecuteActivity,
		})
		w.RegisterActivityWithOptions(streamingActivities.BatchStreamExecute, activity.RegisterOptions{
			Name: constants.BatchStreamExecuteActivity,
		})
		w.RegisterActivityWithOptions(streamingActivities.GetStreamingMetrics, activity.RegisterOptions{
			Name: constants.GetStreamingMetricsActivity,
		})
		r.logger.Info("Registered streaming activities")
	}

	// Minimal streaming_v1 event emitter (always safe to register)
	w.RegisterActivityWithOptions(activities.EmitTaskUpdate, activity.RegisterOptions{
		Name: "EmitTaskUpdate",
	})

	// Pattern metrics activity
	w.RegisterActivityWithOptions(activities.RecordPatternMetrics, activity.RegisterOptions{
		Name: "RecordPatternMetrics",
	})

	// Persistence activities for agent and tool executions
	// These use a global dbClient that must be set during initialization
	w.RegisterActivity(activities.PersistAgentExecutionStandalone)
	w.RegisterActivity(activities.PersistToolExecutionStandalone)

	// Budget activities
    if r.config.EnableBudgetedWorkflows {
        var budgetActivities *activities.BudgetActivities
        if r.config.DefaultTaskBudget > 0 || r.config.DefaultSessionBudget > 0 {
            budgetActivities = activities.NewBudgetActivitiesWithDefaults(r.db, r.logger, r.config.DefaultTaskBudget, r.config.DefaultSessionBudget)
        } else {
            budgetActivities = activities.NewBudgetActivities(r.db, r.logger)
        }
        w.RegisterActivityWithOptions(budgetActivities.CheckTokenBudget, activity.RegisterOptions{
            Name: constants.CheckTokenBudgetActivity,
        })
		w.RegisterActivityWithOptions(budgetActivities.CheckTokenBudgetWithBackpressure, activity.RegisterOptions{
			Name: constants.CheckTokenBudgetWithBackpressureActivity,
		})
		w.RegisterActivityWithOptions(budgetActivities.CheckTokenBudgetWithCircuitBreaker, activity.RegisterOptions{
			Name: constants.CheckTokenBudgetWithCircuitBreakerActivity,
		})
		w.RegisterActivityWithOptions(budgetActivities.RecordTokenUsage, activity.RegisterOptions{
			Name: constants.RecordTokenUsageActivity,
		})
		w.RegisterActivityWithOptions(budgetActivities.GenerateUsageReport, activity.RegisterOptions{
			Name: constants.GenerateUsageReportActivity,
		})
		// Also register ExecuteAgentWithBudget activity
		w.RegisterActivityWithOptions(budgetActivities.ExecuteAgentWithBudget, activity.RegisterOptions{
			Name: constants.ExecuteAgentWithBudgetActivity,
		})
		w.RegisterActivityWithOptions(budgetActivities.UpdateBudgetPolicy, activity.RegisterOptions{
			Name: constants.UpdateBudgetPolicyActivity,
		})
		r.logger.Info("Registered budget activities")
	}

	r.logger.Info("All activities registered successfully")
	return nil
}
