package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Pattern execution counters
	PatternExecutions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_pattern_executions_total",
			Help: "Total number of pattern executions by type",
		},
		[]string{"pattern", "workflow_version"},
	)

	// Pattern execution duration
	PatternDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_pattern_duration_seconds",
			Help:    "Duration of pattern executions in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"pattern", "workflow_version"},
	)

	// Agent executions by pattern
	AgentExecutionsByPattern = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_agents_by_pattern_total",
			Help: "Total number of agents executed by pattern type",
		},
		[]string{"pattern", "workflow_version"},
	)

	// Token usage by pattern
	TokenUsageByPattern = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_tokens_by_pattern_total",
			Help: "Total tokens used by pattern type",
		},
		[]string{"pattern", "workflow_version"},
	)

	// Reflection improvements
	ReflectionImprovements = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_reflection_improvements_total",
			Help: "Number of times reflection improved quality",
		},
		[]string{"workflow_version"},
	)

	// Workflow version distribution
	WorkflowVersions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_workflow_versions_total",
			Help: "Distribution of workflow executions by version",
		},
		[]string{"workflow", "version"},
	)
)

// RecordPatternExecution records a pattern execution
func RecordPatternExecution(pattern, version string) {
	PatternExecutions.WithLabelValues(pattern, version).Inc()
}

// RecordPatternDuration records pattern execution duration
func RecordPatternDuration(pattern, version string, seconds float64) {
	PatternDuration.WithLabelValues(pattern, version).Observe(seconds)
}

// RecordAgentExecution records agent executions for a pattern
func RecordAgentExecution(pattern, version string, count int) {
	AgentExecutionsByPattern.WithLabelValues(pattern, version).Add(float64(count))
}

// RecordTokenUsage records token usage for a pattern
func RecordTokenUsage(pattern, version string, tokens int) {
	TokenUsageByPattern.WithLabelValues(pattern, version).Add(float64(tokens))
}

// RecordReflectionImprovement records when reflection improves quality
func RecordReflectionImprovement(version string) {
	ReflectionImprovements.WithLabelValues(version).Inc()
}

// RecordWorkflowVersion records workflow version usage
func RecordWorkflowVersion(workflow, version string) {
	WorkflowVersions.WithLabelValues(workflow, version).Inc()
}