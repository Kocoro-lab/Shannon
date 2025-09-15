package policy

import (
	"crypto/sha1"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Policy evaluation metrics
	policyEvaluations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_evaluations_total",
			Help: "Total number of policy evaluations",
		},
		[]string{"decision", "mode", "reason"},
	)
	
	policyEvaluationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shannon_policy_evaluation_duration_seconds",
			Help:    "Time spent evaluating policies",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		},
		[]string{"mode"},
	)
	
	policyErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_errors_total",
			Help: "Total number of policy evaluation errors",
		},
		[]string{"error_type", "mode"},
	)
	
	// Dry-run comparison metrics
	policyDryRunDivergence = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_dry_run_divergence_total",
			Help: "Cases where dry-run decision differs from default allow",
		},
		[]string{"divergence_type"}, // "would_deny", "would_allow"
	)
	
	// Policy load metrics
	policyLoadTime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shannon_policy_load_timestamp_seconds",
			Help: "Timestamp of last successful policy load",
		},
		[]string{"policy_path"},
	)
	
	policyCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shannon_policy_files_loaded",
			Help: "Number of policy files currently loaded",
		},
		[]string{"policy_path"},
	)
	
	// Cache performance metrics
	policyCacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_cache_hits_total",
			Help: "Total number of policy cache hits",
		},
		[]string{"effective_mode"},
	)
	
	policyCacheMisses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_cache_misses_total", 
			Help: "Total number of policy cache misses",
		},
		[]string{"effective_mode"},
	)
	
	policyCacheSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shannon_policy_cache_entries",
			Help: "Current number of entries in policy cache",
		},
		[]string{"cache_type"},
	)
	
	// Canary deployment metrics
	policyCanaryDecisions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_canary_decisions_total",
			Help: "Policy decisions broken down by canary routing",
		},
		[]string{"configured_mode", "effective_mode", "routing_reason", "decision"},
	)
	
	// Top deny reasons tracking (top 10 most frequent)
	policyDenyReasons = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_deny_reasons_total",
			Help: "Count of policy denials by reason (top reasons only)",
		},
		[]string{"reason_hash", "mode", "truncated_reason"},
	)
	
	// Policy version tracking
	policyVersionInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shannon_policy_version_info",
			Help: "Policy version information (value always 1, labels contain version data)",
		},
		[]string{"policy_path", "version_hash", "load_timestamp"},
	)
	
	// Enhanced dry-run analysis
	policyModeComparison = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_mode_comparison_total",
			Help: "Comparison of what decisions would be made in enforce vs dry-run",
		},
		[]string{"original_decision", "effective_mode", "would_enforce_decision", "user_type"},
	)
	
	// SLO tracking metrics  
	policyLatencyObjective = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "shannon_policy_latency_slo_seconds",
			Help: "Policy evaluation latency for SLO tracking",
			Buckets: []float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"effective_mode", "cache_hit"},
	)
	
	// Error rate SLO tracking
	policyErrorRate = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_policy_slo_errors_total", 
			Help: "Policy errors for SLO error rate calculation",
		},
		[]string{"error_category", "effective_mode"},
	)
)

// RecordEvaluation records a policy evaluation result
func RecordEvaluation(decision string, mode string, reason string) {
	policyEvaluations.WithLabelValues(decision, mode, reason).Inc()
}

// RecordEvaluationDuration records the time spent evaluating a policy
func RecordEvaluationDuration(mode string, duration float64) {
	policyEvaluationDuration.WithLabelValues(mode).Observe(duration)
}

// RecordError records a policy evaluation error
func RecordError(errorType string, mode string) {
	policyErrors.WithLabelValues(errorType, mode).Inc()
}

// RecordDryRunDivergence records when dry-run differs from default behavior
func RecordDryRunDivergence(divergenceType string) {
	policyDryRunDivergence.WithLabelValues(divergenceType).Inc()
}

// RecordPolicyLoad records successful policy loading
func RecordPolicyLoad(policyPath string, count int, timestamp float64) {
	policyLoadTime.WithLabelValues(policyPath).Set(timestamp)
	policyCount.WithLabelValues(policyPath).Set(float64(count))
}

// RecordCacheHit records a policy cache hit
func RecordCacheHit(effectiveMode string) {
	policyCacheHits.WithLabelValues(effectiveMode).Inc()
}

// RecordCacheMiss records a policy cache miss
func RecordCacheMiss(effectiveMode string) {
	policyCacheMisses.WithLabelValues(effectiveMode).Inc()
}

// RecordCacheSize records current cache size
func RecordCacheSize(cacheType string, size int) {
	policyCacheSize.WithLabelValues(cacheType).Set(float64(size))
}

// RecordCanaryDecision records a canary routing decision
func RecordCanaryDecision(configuredMode, effectiveMode, routingReason, decision string) {
	policyCanaryDecisions.WithLabelValues(configuredMode, effectiveMode, routingReason, decision).Inc()
}

// RecordDenyReason records a denial reason (top reasons tracking)
func RecordDenyReason(reason, mode string) {
	// Hash the reason for consistent labeling while limiting cardinality
	reasonHash := hashString(reason)
	truncatedReason := truncateString(reason, 50) // Limit label size
	policyDenyReasons.WithLabelValues(reasonHash, mode, truncatedReason).Inc()
}

// RecordPolicyVersion records policy version information
func RecordPolicyVersion(policyPath, versionHash, loadTimestamp string) {
	policyVersionInfo.WithLabelValues(policyPath, versionHash, loadTimestamp).Set(1)
}

// RecordModeComparison records comparison between dry-run and enforce decisions
func RecordModeComparison(originalDecision, effectiveMode, wouldEnforceDecision, userType string) {
	policyModeComparison.WithLabelValues(originalDecision, effectiveMode, wouldEnforceDecision, userType).Inc()
}

// RecordSLOLatency records latency for SLO tracking with enhanced labels
func RecordSLOLatency(effectiveMode string, cacheHit bool, duration float64) {
	cacheLabel := "miss"
	if cacheHit {
		cacheLabel = "hit"
	}
	policyLatencyObjective.WithLabelValues(effectiveMode, cacheLabel).Observe(duration)
}

// RecordSLOError records an error for SLO error rate calculation
func RecordSLOError(errorCategory, effectiveMode string) {
	policyErrorRate.WithLabelValues(errorCategory, effectiveMode).Inc()
}

// Helper functions

// hashString creates a consistent hash for high-cardinality strings
func hashString(s string) string {
	h := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", h[:4]) // Use first 8 hex chars for reasonable uniqueness
}

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}