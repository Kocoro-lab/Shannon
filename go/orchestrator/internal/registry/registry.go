package registry

import (
	"database/sql"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/constants"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/session"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/workflows/scheduled"
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
	w.RegisterWorkflow(workflows.TemplateWorkflow)

	// Scheduled task workflow
	w.RegisterWorkflow(scheduled.ScheduledTaskWorkflow)

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
	w.RegisterWorkflow(strategies.DomainAnalysisWorkflow)
	w.RegisterWorkflow(strategies.BrowserUseWorkflow)
	r.logger.Info("Registered strategy workflows")

	// Enterprise features - conditionally compiled
	r.registerEnterpriseWorkflows(w)

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
	// Deep Research 2.0 activities
	w.RegisterActivityWithOptions(acts.EvaluateCoverage, activity.RegisterOptions{Name: "EvaluateCoverage"})
	w.RegisterActivityWithOptions(acts.IntermediateSynthesis, activity.RegisterOptions{Name: "IntermediateSynthesis"})
	w.RegisterActivityWithOptions(acts.GenerateSubqueries, activity.RegisterOptions{Name: "GenerateSubqueries"})
	w.RegisterActivityWithOptions(acts.ExtractFacts, activity.RegisterOptions{Name: "ExtractFacts"})
	w.RegisterActivityWithOptions(acts.DetectEntityLocalization, activity.RegisterOptions{Name: "DetectEntityLocalization"})
	w.RegisterActivityWithOptions(acts.RouteSearch, activity.RegisterOptions{Name: "RouteSearch"})
	w.RegisterActivityWithOptions(acts.MergeSearchResults, activity.RegisterOptions{Name: "MergeSearchResults"})
	// Claim verification activity (Phase 4)
	w.RegisterActivityWithOptions(acts.VerifyClaimsActivity, activity.RegisterOptions{Name: "VerifyClaimsActivity"})
	// Citation Agent activity
	w.RegisterActivityWithOptions(acts.AddCitations, activity.RegisterOptions{Name: "AddCitations"})
	// Citation V2 activities (Deep Research with Verify batch)
	w.RegisterActivityWithOptions(acts.AddCitationsWithVerify, activity.RegisterOptions{Name: "AddCitationsWithVerify"})
	w.RegisterActivityWithOptions(acts.VerifyBatch, activity.RegisterOptions{Name: "VerifyBatch"})
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
	w.RegisterActivity(activities.RecommendWorkflowStrategy)
	w.RegisterActivity(activities.RecordLearningRouterMetrics)
	// Consensus memory for debate pattern
	w.RegisterActivity(activities.PersistDebateConsensus)
	w.RegisterActivity(activities.FetchConsensusMemory)

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
	w.RegisterActivityWithOptions(acts.RefineResearchQuery, activity.RegisterOptions{Name: constants.RefineResearchQueryActivity})
	// Legacy activity name for Temporal replay compatibility
	w.RegisterActivityWithOptions(acts.AnalyzeComplexity, activity.RegisterOptions{Name: constants.AnalyzeComplexityActivity})
	w.RegisterActivityWithOptions(acts.UpdateSessionResult, activity.RegisterOptions{
		Name: constants.UpdateSessionResultActivity,
	})
	w.RegisterActivityWithOptions(acts.GenerateSessionTitle, activity.RegisterOptions{Name: "GenerateSessionTitle"})

	// Schedule activities
	scheduleActivities := activities.NewScheduleActivities(r.db, r.logger)
	w.RegisterActivityWithOptions(scheduleActivities.RecordScheduleExecutionStart, activity.RegisterOptions{
		Name: "RecordScheduleExecutionStart",
	})
	w.RegisterActivityWithOptions(scheduleActivities.RecordScheduleExecutionComplete, activity.RegisterOptions{
		Name: "RecordScheduleExecutionComplete",
	})
	r.logger.Info("Registered schedule activities")

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

	// Enterprise features - conditionally compiled
	r.registerEnterpriseActivities(w)

	// Agent selection activities (performance-based)
	w.RegisterActivity(activities.FetchAgentPerformances)
	w.RegisterActivity(activities.SelectAgentEpsilonGreedy)
	w.RegisterActivity(activities.SelectAgentUCB1)
	w.RegisterActivity(activities.RecordAgentPerformance)

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

// registerEnterpriseWorkflows registers enterprise-only workflows (stub in open source)
func (r *OrchestratorRegistry) registerEnterpriseWorkflows(w worker.Worker) {
	// Open source: no enterprise workflows
	// Enterprise version: override this file with actual registrations
}

// registerEnterpriseActivities registers enterprise-only activities (stub in open source)
func (r *OrchestratorRegistry) registerEnterpriseActivities(w worker.Worker) {
	// Open source: no enterprise activities
	// Enterprise version: override this file with actual registrations
}
