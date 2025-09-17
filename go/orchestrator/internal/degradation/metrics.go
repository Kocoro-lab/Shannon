package degradation

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// degradationEventsTotal tracks degradation events
	degradationEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_degradation_events_total",
			Help: "Total number of degradation events by level and reason",
		},
		[]string{"level", "reason"},
	)

	// currentDegradationLevel tracks current system degradation level
	currentDegradationLevel = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "shannon_degradation_level",
			Help: "Current system degradation level (0=none, 1=minor, 2=moderate, 3=severe)",
		},
	)

	// dependencyHealthStatus tracks individual dependency health
	dependencyHealthStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shannon_dependency_health",
			Help: "Dependency health status (1=healthy, 0=unhealthy)",
		},
		[]string{"dependency", "type"},
	)

	// fallbackBehaviorExecuted tracks when fallback behaviors are triggered
	fallbackBehaviorExecuted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_fallback_behavior_total",
			Help: "Total number of fallback behaviors executed by operation and behavior type",
		},
		[]string{"operation", "behavior"},
	)

	// modeDowngradeEvents tracks when workflow modes are downgraded
	modeDowngradeEvents = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_mode_downgrade_total",
			Help: "Total number of mode downgrades by original mode and target mode",
		},
		[]string{"from_mode", "to_mode", "reason"},
	)

	// partialResultsReturned tracks when partial results are returned instead of failures
	partialResultsReturned = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_partial_results_total",
			Help: "Total number of times partial results were returned instead of complete failure",
		},
		[]string{"workflow_type", "reason"},
	)
)

// RecordDependencyHealth updates dependency health metrics
func RecordDependencyHealth(dependency string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	dependencyHealthStatus.WithLabelValues(dependency, "overall").Set(value)
}

// RecordCircuitBreakerHealth updates dependency health based on circuit breaker state
func RecordCircuitBreakerHealth(dependency string, isOpen bool) {
	value := 1.0
	if isOpen {
		value = 0.0
	}
	dependencyHealthStatus.WithLabelValues(dependency, "circuit_breaker").Set(value)
}

// RecordFallbackBehavior records when a fallback behavior is executed
func RecordFallbackBehavior(operation string, behavior FallbackBehavior) {
	fallbackBehaviorExecuted.WithLabelValues(operation, behavior.String()).Inc()
}

// RecordModeDowngrade records when a workflow mode is downgraded
func RecordModeDowngrade(fromMode, toMode, reason string) {
	modeDowngradeEvents.WithLabelValues(fromMode, toMode, reason).Inc()
}

// RecordPartialResults records when partial results are returned
func RecordPartialResults(workflowType, reason string) {
	partialResultsReturned.WithLabelValues(workflowType, reason).Inc()
}
