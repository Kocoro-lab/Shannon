package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Workflow metrics
	WorkflowsStarted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_workflows_started_total",
			Help: "Total number of workflows started",
		},
		[]string{"workflow_type", "mode"},
	)

	WorkflowsCompleted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_workflows_completed_total",
			Help: "Total number of workflows completed",
		},
		[]string{"workflow_type", "mode", "status"},
	)

	WorkflowDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_workflow_duration_seconds",
			Help:    "Workflow execution duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"workflow_type", "mode"},
	)

	// Task metrics
	TasksSubmitted = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_tasks_submitted_total",
			Help: "Total number of tasks submitted",
		},
	)

	TaskTokensUsed = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_task_tokens_used",
			Help:    "Number of tokens used per task",
			Buckets: []float64{10, 50, 100, 500, 1000, 5000, 10000},
		},
	)

	TaskCostUSD = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_task_cost_usd",
			Help:    "Cost in USD per task",
			Buckets: []float64{0.001, 0.01, 0.1, 1, 10},
		},
	)

	// Agent metrics
	AgentExecutions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_agent_executions_total",
			Help: "Total number of agent executions",
		},
		[]string{"agent_id", "mode"},
	)

	AgentExecutionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_agent_execution_duration_ms",
			Help:    "Agent execution duration in milliseconds",
			Buckets: []float64{100, 500, 1000, 2000, 5000, 10000, 30000},
		},
		[]string{"agent_id", "mode"},
	)

	// Session metrics
	SessionsCreated = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_sessions_created_total",
			Help: "Total number of sessions created",
		},
	)

	SessionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "shannon_sessions_active",
			Help: "Number of active sessions",
		},
	)

	SessionTokensTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_session_tokens_total",
			Help: "Total tokens used across all sessions",
		},
	)

	// gRPC metrics
	GRPCRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_grpc_requests_total",
			Help: "Total number of gRPC requests",
		},
		[]string{"service", "method", "status"},
	)

	GRPCRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_grpc_request_duration_seconds",
			Help:    "gRPC request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "method"},
	)

	// Cache metrics
	CacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_cache_hits_total",
			Help: "Total number of cache hits",
		},
	)

	// Session cache metrics
	SessionCacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_session_cache_hits_total",
			Help: "Total number of session cache hits",
		},
	)

	SessionCacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_session_cache_misses_total",
			Help: "Total number of session cache misses",
		},
	)

	SessionCacheSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "shannon_session_cache_size",
			Help: "Current number of sessions in local cache",
		},
	)

	SessionCacheEvictions = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_session_cache_evictions_total",
			Help: "Total number of sessions evicted from cache",
		},
	)

	CacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_cache_misses_total",
			Help: "Total number of cache misses",
		},
	)

	// Vector DB metrics
	VectorSearches = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_vector_search_total",
			Help: "Total number of vector searches",
		},
		[]string{"collection", "status"},
	)

	VectorSearchLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_vector_search_latency_seconds",
			Help:    "Vector search latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"collection"},
	)

	// Pricing fallback metrics
	PricingFallbacks = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_pricing_fallback_total",
			Help: "Total number of pricing fallbacks (missing/unknown model)",
		},
		[]string{"reason"},
	)

	// Embedding metrics
	EmbeddingRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_embedding_requests_total",
			Help: "Total number of embedding requests",
		},
		[]string{"model", "status"},
	)

	EmbeddingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_embedding_latency_seconds",
			Help:    "Embedding generation latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"model"},
	)

	// Decomposition metrics
	DecompositionLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_decomposition_latency_seconds",
			Help:    "Task decomposition latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)

	DecompositionErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_decomposition_errors_total",
			Help: "Total number of decomposition errors",
		},
	)

	// Complexity metrics
	ComplexityLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_complexity_latency_seconds",
			Help:    "Complexity analysis latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)
	ComplexityErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_complexity_errors_total",
			Help: "Total number of complexity analysis errors",
		},
	)
)

// RecordWorkflowMetrics records metrics for a completed workflow
func RecordWorkflowMetrics(workflowType, mode, status string, durationSeconds float64, tokensUsed int, costUSD float64) {
	WorkflowsCompleted.WithLabelValues(workflowType, mode, status).Inc()
	WorkflowDuration.WithLabelValues(workflowType, mode).Observe(durationSeconds)

	if tokensUsed > 0 {
		TaskTokensUsed.Observe(float64(tokensUsed))
		// Don't add to SessionTokensTotal here - it's tracked in session updates to avoid double-counting
	}

	if costUSD > 0 {
		TaskCostUSD.Observe(costUSD)
	}
}

// RecordAgentMetrics records metrics for an agent execution
func RecordAgentMetrics(agentID, mode string, durationMs float64) {
	AgentExecutions.WithLabelValues(agentID, mode).Inc()
	AgentExecutionDuration.WithLabelValues(agentID, mode).Observe(durationMs)
}

// RecordGRPCMetrics records metrics for a gRPC request
func RecordGRPCMetrics(service, method, status string, durationSeconds float64) {
	GRPCRequestsTotal.WithLabelValues(service, method, status).Inc()
	GRPCRequestDuration.WithLabelValues(service, method).Observe(durationSeconds)
}

// RecordSessionTokens increments the session tokens counter
func RecordSessionTokens(tokens int) {
	if tokens > 0 {
		SessionTokensTotal.Add(float64(tokens))
	}
}

// RecordVectorSearchMetrics records vector search metrics
func RecordVectorSearchMetrics(collection, status string, durationSeconds float64) {
	VectorSearches.WithLabelValues(collection, status).Inc()
	if durationSeconds > 0 {
		VectorSearchLatency.WithLabelValues(collection).Observe(durationSeconds)
	}
}

// RecordEmbeddingMetrics records embedding metrics
func RecordEmbeddingMetrics(model, status string, durationSeconds float64) {
	EmbeddingRequests.WithLabelValues(model, status).Inc()
	if durationSeconds > 0 {
		EmbeddingLatency.WithLabelValues(model).Observe(durationSeconds)
	}
}
