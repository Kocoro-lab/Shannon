package constants

// Activity names used for workflow registration and execution.
// Using constants eliminates magic strings and ensures consistency.
const (
	// Session Management Activities
	UpdateSessionResultActivity = "UpdateSessionResult"
	
	// Budget Management Activities
	CheckTokenBudgetActivity                   = "CheckTokenBudget"
	CheckTokenBudgetWithBackpressureActivity   = "CheckTokenBudgetWithBackpressure"
	CheckTokenBudgetWithCircuitBreakerActivity = "CheckTokenBudgetWithCircuitBreaker"
	RecordTokenUsageActivity                   = "RecordTokenUsage"
	GenerateUsageReportActivity                = "GenerateUsageReport"
	UpdateBudgetPolicyActivity                 = "UpdateBudgetPolicy"
	
	// Agent Execution Activities
	ExecuteAgentWithBudgetActivity = "ExecuteAgentWithBudget"

	// Planning/Decomposition Activities
	DecomposeTaskActivity     = "DecomposeTask"
	AnalyzeComplexityActivity = "AnalyzeComplexity" // legacy compatibility for replay
	
	// Human Intervention Activities
	RequestApprovalActivity        = "RequestApproval"
	ProcessApprovalResponseActivity = "ProcessApprovalResponse"
	GetApprovalStatusActivity      = "GetApprovalStatus"
	
	// Streaming Activities
	StreamExecuteActivity         = "StreamExecute"
	BatchStreamExecuteActivity    = "BatchStreamExecute"
	GetStreamingMetricsActivity   = "GetStreamingMetrics"

	// P2P Activities
	SendAgentMessageActivity      = "SendAgentMessage"
	FetchAgentMessagesActivity    = "FetchAgentMessages"
	WorkspaceAppendActivity       = "WorkspaceAppend"
	WorkspaceListActivity         = "WorkspaceList"
	SendTaskRequestActivity       = "SendTaskRequest"
	SendTaskOfferActivity         = "SendTaskOffer"
	SendTaskAcceptActivity        = "SendTaskAccept"
)
