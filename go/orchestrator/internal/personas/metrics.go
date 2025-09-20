package personas

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the personas system
type Metrics struct {
	// Basic counters
	SelectionCount   *prometheus.CounterVec
	SelectionLatency *prometheus.HistogramVec
	CacheHits        prometheus.Counter
	CacheMisses      prometheus.Counter

	// Quality indicators
	SelectionAccuracy *prometheus.GaugeVec
	ConfidenceScore   *prometheus.HistogramVec

	// Error metrics
	ErrorRate    *prometheus.CounterVec
	FallbackRate *prometheus.CounterVec
	TimeoutRate  *prometheus.CounterVec

	// Resource usage
	CacheSize            prometheus.Gauge
	ConcurrentSelections prometheus.Gauge
	MemoryUsage          prometheus.Gauge

	// Algorithm performance
	KeywordMatchTime    *prometheus.HistogramVec
	SemanticMatchTime   *prometheus.HistogramVec
	EmbeddingAPILatency *prometheus.HistogramVec

	// Business metrics
	PersonaSuccessRate *prometheus.GaugeVec
	TokenEfficiency    *prometheus.GaugeVec
	UserSatisfaction   *prometheus.GaugeVec
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	factory := promauto.With(registry)

	return &Metrics{
		SelectionCount: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "selection_total",
				Help:      "Total number of persona selections",
			},
			[]string{"persona_id", "method", "status"},
		),

		SelectionLatency: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "selection_duration_seconds",
				Help:      "Duration of persona selection in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
			},
			[]string{"persona_id", "method"},
		),

		CacheHits: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "shannon",
			Subsystem: "personas",
			Name:      "cache_hits_total",
			Help:      "Total number of cache hits",
		}),

		CacheMisses: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "shannon",
			Subsystem: "personas",
			Name:      "cache_misses_total",
			Help:      "Total number of cache misses",
		}),

		SelectionAccuracy: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "selection_accuracy",
				Help:      "Accuracy of persona selection (0-1)",
			},
			[]string{"persona_id", "task_type"},
		),

		ConfidenceScore: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "confidence_score",
				Help:      "Confidence score distribution",
				Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
			},
			[]string{"persona_id", "method"},
		),

		ErrorRate: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "errors_total",
				Help:      "Total number of persona selection errors",
			},
			[]string{"error_type", "persona_id"},
		),

		FallbackRate: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "fallback_total",
				Help:      "Total number of fallback selections",
			},
			[]string{"fallback_type", "original_persona"},
		),

		TimeoutRate: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "timeout_total",
				Help:      "Total number of selection timeouts",
			},
			[]string{"operation", "persona_id"},
		),

		CacheSize: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "shannon",
			Subsystem: "personas",
			Name:      "cache_entries",
			Help:      "Number of entries in persona cache",
		}),

		ConcurrentSelections: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "shannon",
			Subsystem: "personas",
			Name:      "concurrent_selections",
			Help:      "Number of concurrent persona selections",
		}),

		MemoryUsage: factory.NewGauge(prometheus.GaugeOpts{
			Namespace: "shannon",
			Subsystem: "personas",
			Name:      "memory_usage_bytes",
			Help:      "Memory usage of personas system in bytes",
		}),

		KeywordMatchTime: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "keyword_match_duration_seconds",
				Help:      "Duration of keyword matching in seconds",
				Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1},
			},
			[]string{"method"},
		),

		SemanticMatchTime: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "semantic_match_duration_seconds",
				Help:      "Duration of semantic matching in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
			},
			[]string{"method"},
		),

		EmbeddingAPILatency: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "embedding_api_duration_seconds",
				Help:      "Duration of embedding API calls in seconds",
				Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
			},
			[]string{"provider", "operation"},
		),

		PersonaSuccessRate: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "success_rate",
				Help:      "Success rate by persona over time window",
			},
			[]string{"persona_id", "time_window"},
		),

		TokenEfficiency: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "token_efficiency",
				Help:      "Token efficiency (output quality / tokens used)",
			},
			[]string{"persona_id", "task_type"},
		),

		UserSatisfaction: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "shannon",
				Subsystem: "personas",
				Name:      "user_satisfaction",
				Help:      "User satisfaction score by persona",
			},
			[]string{"persona_id", "user_id"},
		),
	}
}

// RecordSelection records a persona selection event
func (m *Metrics) RecordSelection(personaID, method string, latency time.Duration, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}

	m.SelectionCount.WithLabelValues(personaID, method, status).Inc()
	m.SelectionLatency.WithLabelValues(personaID, method).Observe(latency.Seconds())
}

// RecordError records an error event
func (m *Metrics) RecordError(errorType, personaID string) {
	m.ErrorRate.WithLabelValues(errorType, personaID).Inc()
}

// RecordFallback records a fallback event
func (m *Metrics) RecordFallback(fallbackType, originalPersona string) {
	m.FallbackRate.WithLabelValues(fallbackType, originalPersona).Inc()
}

// RecordTimeout records a timeout event
func (m *Metrics) RecordTimeout(operation, personaID string) {
	m.TimeoutRate.WithLabelValues(operation, personaID).Inc()
}

// RecordConfidence records a confidence score
func (m *Metrics) RecordConfidence(personaID, method string, confidence float64) {
	m.ConfidenceScore.WithLabelValues(personaID, method).Observe(confidence)
}

// UpdateSuccessRate updates the success rate for a persona
func (m *Metrics) UpdateSuccessRate(personaID, timeWindow string, rate float64) {
	m.PersonaSuccessRate.WithLabelValues(personaID, timeWindow).Set(rate)
}

// UpdateTokenEfficiency updates token efficiency for a persona
func (m *Metrics) UpdateTokenEfficiency(personaID, taskType string, efficiency float64) {
	m.TokenEfficiency.WithLabelValues(personaID, taskType).Set(efficiency)
}

// RecordKeywordMatchTime records keyword matching duration
func (m *Metrics) RecordKeywordMatchTime(method string, duration time.Duration) {
	m.KeywordMatchTime.WithLabelValues(method).Observe(duration.Seconds())
}

// RecordSemanticMatchTime records semantic matching duration
func (m *Metrics) RecordSemanticMatchTime(method string, duration time.Duration) {
	m.SemanticMatchTime.WithLabelValues(method).Observe(duration.Seconds())
}

// RecordEmbeddingAPICall records embedding API call duration
func (m *Metrics) RecordEmbeddingAPICall(provider, operation string, duration time.Duration) {
	m.EmbeddingAPILatency.WithLabelValues(provider, operation).Observe(duration.Seconds())
}

// Note: Cache hit rate can be calculated in monitoring dashboards
// using the formula: rate(cache_hits_total) / (rate(cache_hits_total) + rate(cache_misses_total))

// GetCacheHitRateFromCounters calculates hit rate from raw counter values
func GetCacheHitRateFromCounters(hits, misses float64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return hits / total * 100
}
