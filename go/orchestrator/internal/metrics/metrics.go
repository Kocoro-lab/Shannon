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

	// Template metrics
	TemplatesLoaded = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_template_loaded_total",
			Help: "Total number of templates successfully loaded",
		},
		[]string{"name"},
	)

	// Template fallback metrics
	TemplateFallbackTriggered = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "template_fallback_triggered_total",
			Help: "Number of times execution fell back from template to AI",
		},
		[]string{"reason"}, // reason: error|unsuccessful
	)

	TemplateFallbackSuccess = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "template_fallback_success_total",
			Help: "Number of successful fallbacks from template to AI",
		},
		[]string{"reason"},
	)

	TemplateValidationErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_template_validation_errors_total",
			Help: "Total number of template validation failures",
		},
		[]string{"reason"},
	)

	// Note: Template compilation cache not implemented yet
	// Templates are compiled on-demand; compilation is fast enough
	// that caching provides minimal benefit. May add in future if needed.

	PatternDegraded = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_pattern_degraded_total",
			Help: "Number of times a pattern degraded to a simpler strategy",
		},
		[]string{"from", "to", "template", "node"},
	)

	// Rate control metrics
	RateLimitDelay = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_rate_limit_delay_seconds",
			Help:    "Rate limit delay applied per provider and tier",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"provider", "tier"},
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

	// Memory metrics
	MemoryFetches = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_memory_fetches_total",
			Help: "Total number of memory fetch operations",
		},
		[]string{"type", "source", "result"}, // type: session/semantic/hierarchical-recent, source: qdrant, result: hit/miss
		// Note: hierarchical-semantic reuses "semantic" type to avoid double-counting
	)

	MemoryItemsRetrieved = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_memory_items_retrieved",
			Help:    "Number of memory items retrieved per fetch",
			Buckets: []float64{0, 1, 5, 10, 20, 50, 100},
		},
		[]string{"type", "source"},
	)

	// Note: Memory hit rate is calculated via Prometheus query:
	// rate(shannon_memory_fetches_total{result="hit"}[5m]) / rate(shannon_memory_fetches_total[5m])

	CompressionEvents = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_compression_events_total",
			Help: "Total number of context compression events",
		},
		[]string{"status"}, // status: triggered/skipped/failed
	)

	CompressionTokensSaved = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_compression_tokens_saved",
			Help:    "Estimated tokens saved by compression per event",
			Buckets: []float64{100, 500, 1000, 2000, 5000, 10000, 20000},
		},
	)

	CompressionRatio = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_compression_ratio",
			Help:    "Compression ratio achieved (original_tokens / compressed_tokens)",
			Buckets: []float64{1.5, 2, 3, 5, 10, 20},
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

	// Decomposition pattern metrics
	DecompositionPatternCacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_decomposition_pattern_cache_hits_total",
			Help: "Total number of decomposition pattern cache hits",
		},
	)

	DecompositionPatternCacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_decomposition_pattern_cache_misses_total",
			Help: "Total number of decomposition pattern cache misses",
		},
	)

	StrategySelectionDistribution = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_strategy_selection_total",
			Help: "Distribution of selected execution strategies",
		},
		[]string{"strategy"},
	)

	// Learning router metrics
	LearningRouterLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_learning_router_latency_seconds",
			Help:    "Latency of learning router strategy recommendation",
			Buckets: prometheus.DefBuckets,
		},
	)

	LearningRouterRecommendations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_learning_router_recommendations_total",
			Help: "Total learning router recommendations by strategy and source",
		},
		[]string{"strategy", "source", "success"},
	)

	LearningRouterConfidence = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_learning_router_confidence",
			Help:    "Confidence score of learning router recommendations",
			Buckets: []float64{0.1, 0.3, 0.5, 0.7, 0.8, 0.9, 0.95, 1.0},
		},
		[]string{"strategy"},
	)

	DecompositionPatternsRecorded = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shannon_decomposition_patterns_recorded_total",
			Help: "Total number of decomposition patterns recorded for learning",
		},
	)

	UserPreferenceInferenceAccuracy = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "shannon_user_preference_inference_accuracy",
			Help: "Accuracy of user preference inference (0-1)",
		},
	)

	// Chunking metrics
	ChunksPerQA = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_chunks_per_qa",
			Help:    "Number of chunks created per Q&A pair",
			Buckets: []float64{1, 2, 3, 5, 10, 20, 50},
		},
	)

	ChunkSize = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_chunk_size_tokens",
			Help:    "Size of each chunk in tokens",
			Buckets: []float64{100, 250, 500, 1000, 1500, 2000, 3000},
		},
	)

	ChunkedQAPairs = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_chunked_qa_pairs_total",
			Help: "Total number of Q&A pairs that were chunked",
		},
		[]string{"session_id"},
	)

	RetrievalTokenBudget = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_retrieval_token_budget",
			Help:    "Token budget used in retrieval",
			Buckets: []float64{100, 500, 1000, 2000, 5000, 10000, 20000},
		},
		[]string{"retrieval_type"},
	)

	ChunkAggregationLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "shannon_chunk_aggregation_latency_seconds",
			Help:    "Latency of aggregating chunks during retrieval",
			Buckets: prometheus.DefBuckets,
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

	MemoryWritesSkipped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_memory_writes_skipped_total",
			Help: "Total number of memory writes skipped due to filtering",
		},
		[]string{"reason"}, // reason: duplicate, low_value, error
	)

	// Model tier selection metrics (regression prevention)
	ModelTierRequested = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_model_tier_requested_total",
			Help: "Total number of times each model tier was requested",
		},
		[]string{"tier"}, // tier: small/medium/large
	)

	ModelTierSelected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_model_tier_selected_total",
			Help: "Total number of times each model tier was actually selected",
		},
		[]string{"tier", "provider"}, // tier: small/medium/large, provider: openai/anthropic/etc
	)

	TierSelectionDrift = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_tier_selection_drift_total",
			Help: "Number of times selected tier differed from requested tier",
		},
		[]string{"requested_tier", "selected_tier", "reason"}, // reason: provider_unavailable/tier_unavailable/fallback
	)

	ProviderOverrideRequested = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_provider_override_requested_total",
			Help: "Total number of times provider override was requested",
		},
		[]string{"provider"}, // provider: openai/anthropic/google/etc
	)

	ProviderOverrideRespected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_provider_override_respected_total",
			Help: "Total number of times provider override was successfully respected",
		},
		[]string{"provider"},
	)

	ProviderSelectionDrift = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_provider_selection_drift_total",
			Help: "Number of times selected provider differed from requested provider",
		},
		[]string{"requested_provider", "selected_provider", "reason"}, // reason: unavailable/rate_limited/fallback
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

// RecordChunkingMetrics records metrics for Q&A chunking
func RecordChunkingMetrics(sessionID string, numChunks int, avgChunkSize float64) {
	if numChunks > 1 {
		ChunksPerQA.Observe(float64(numChunks))
		ChunkedQAPairs.WithLabelValues(sessionID).Inc()
	}
	if avgChunkSize > 0 {
		ChunkSize.Observe(avgChunkSize)
	}
}

// RecordRetrievalTokens records the token budget used in retrieval
func RecordRetrievalTokens(retrievalType string, tokens int) {
	if tokens > 0 {
		RetrievalTokenBudget.WithLabelValues(retrievalType).Observe(float64(tokens))
	}
}

// RecordChunkAggregation records chunk aggregation latency
func RecordChunkAggregation(durationSeconds float64) {
	if durationSeconds > 0 {
		ChunkAggregationLatency.Observe(durationSeconds)
	}
}

// RecordModelTierRequest records when a specific tier is requested
func RecordModelTierRequest(tier string) {
	if tier != "" {
		ModelTierRequested.WithLabelValues(tier).Inc()
	}
}

// RecordModelTierSelection records when a specific tier+provider is selected
func RecordModelTierSelection(tier, provider string) {
	if tier != "" && provider != "" {
		ModelTierSelected.WithLabelValues(tier, provider).Inc()
	}
}

// RecordTierDrift records when selected tier differs from requested
func RecordTierDrift(requestedTier, selectedTier, reason string) {
	if requestedTier != "" && selectedTier != "" && requestedTier != selectedTier {
		TierSelectionDrift.WithLabelValues(requestedTier, selectedTier, reason).Inc()
	}
}

// RecordProviderOverride records when provider override is requested and respected
func RecordProviderOverride(provider string, respected bool) {
	if provider != "" {
		ProviderOverrideRequested.WithLabelValues(provider).Inc()
		if respected {
			ProviderOverrideRespected.WithLabelValues(provider).Inc()
		}
	}
}

// RecordProviderDrift records when selected provider differs from requested
func RecordProviderDrift(requestedProvider, selectedProvider, reason string) {
	if requestedProvider != "" && selectedProvider != "" && requestedProvider != selectedProvider {
		ProviderSelectionDrift.WithLabelValues(requestedProvider, selectedProvider, reason).Inc()
	}
}
