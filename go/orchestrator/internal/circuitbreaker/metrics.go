package circuitbreaker

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	circuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shannon_circuit_breaker_state",
			Help: "Current state of circuit breaker (0=closed, 1=half-open, 2=open)",
		},
		[]string{"name", "service"},
	)

	circuitBreakerRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_circuit_breaker_requests_total",
			Help: "Total number of requests through circuit breaker",
		},
		[]string{"name", "service", "state", "result"},
	)

	circuitBreakerFailures = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_circuit_breaker_failures_total",
			Help: "Total number of failures in circuit breaker",
		},
		[]string{"name", "service"},
	)

	circuitBreakerStateChanges = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shannon_circuit_breaker_state_changes_total",
			Help: "Total number of state changes in circuit breaker",
		},
		[]string{"name", "service", "from_state", "to_state"},
	)

	circuitBreakerOpenSince = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shannon_circuit_breaker_open_since_seconds",
			Help: "Timestamp when the circuit breaker entered open state (0 if not open)",
		},
		[]string{"name", "service"},
	)
)

// MetricsCollector collects and exports circuit breaker metrics
type MetricsCollector struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// RegisterCircuitBreaker registers a circuit breaker for metrics collection
func (mc *MetricsCollector) RegisterCircuitBreaker(name, service string, cb *CircuitBreaker) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	key := service + ":" + name
	mc.breakers[key] = cb

	// Set up state change callback
	originalCallback := cb.config.OnStateChange
	cb.config.OnStateChange = func(cbName string, from State, to State) {
		// Call original callback if exists
		if originalCallback != nil {
			originalCallback(cbName, from, to)
		}

		// Record metrics
		circuitBreakerStateChanges.WithLabelValues(name, service, from.String(), to.String()).Inc()
		circuitBreakerState.WithLabelValues(name, service).Set(float64(to))

		// Track open time
		if to == StateOpen {
			circuitBreakerOpenSince.WithLabelValues(name, service).SetToCurrentTime()
		} else if from == StateOpen {
			circuitBreakerOpenSince.WithLabelValues(name, service).Set(0)
		}
	}
}

// RecordRequest records a request attempt
func (mc *MetricsCollector) RecordRequest(name, service string, state State, success bool) {
	result := "success"
	if !success {
		result = "failure"
		circuitBreakerFailures.WithLabelValues(name, service).Inc()
	}

	circuitBreakerRequests.WithLabelValues(name, service, state.String(), result).Inc()
}

// UpdateMetrics updates all circuit breaker metrics
func (mc *MetricsCollector) UpdateMetrics() {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()

	for key, cb := range mc.breakers {
		parts := splitKey(key)
		if len(parts) != 2 {
			continue
		}
		service, name := parts[0], parts[1]

		state := cb.State()
		circuitBreakerState.WithLabelValues(name, service).Set(float64(state))

		// Note: Individual request counters are updated in RecordRequest
		// No need to update them here as they are counters, not gauges
	}
}

func splitKey(key string) []string {
	for i, r := range key {
		if r == ':' {
			return []string{key[:i], key[i+1:]}
		}
	}
	return []string{key}
}

// Global metrics collector instance
var GlobalMetricsCollector = NewMetricsCollector()

// StartMetricsCollection starts background metrics collection
func StartMetricsCollection() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			GlobalMetricsCollector.UpdateMetrics()
		}
	}()
}
