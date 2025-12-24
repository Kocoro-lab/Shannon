package openai

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Request metrics
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_compat_requests_total",
			Help: "Total number of OpenAI-compatible API requests",
		},
		[]string{"model", "endpoint", "status"}, // status: success, error, rate_limited
	)

	RequestLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "openai_compat_latency_seconds",
			Help:    "OpenAI-compatible API request latency in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		},
		[]string{"model", "endpoint", "stream"},
	)

	// Token metrics
	TokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_compat_tokens_total",
			Help: "Total tokens used via OpenAI-compatible API",
		},
		[]string{"model", "type"}, // type: prompt, completion
	)

	TokensPerRequest = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "openai_compat_tokens_per_request",
			Help:    "Token usage per request",
			Buckets: []float64{10, 50, 100, 500, 1000, 2000, 5000, 10000, 20000},
		},
		[]string{"model", "type"},
	)

	// Error metrics
	ErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_compat_errors_total",
			Help: "Total number of OpenAI-compatible API errors",
		},
		[]string{"model", "error_type", "error_code"},
	)

	// Rate limiting metrics
	RateLimitHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_compat_rate_limit_hits_total",
			Help: "Total number of rate limit hits",
		},
		[]string{"model", "limit_type"}, // limit_type: requests, tokens
	)

	// Note: RateLimitRemaining was removed to avoid high-cardinality issues
	// with per-api-key labels. Use rate limit headers in responses instead.

	// Session metrics
	SessionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "openai_compat_sessions_active",
			Help: "Number of active OpenAI sessions",
		},
	)

	SessionsCreated = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "openai_compat_sessions_created_total",
			Help: "Total number of OpenAI sessions created",
		},
	)

	SessionCollisions = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "openai_compat_session_collisions_total",
			Help: "Total number of session ID collisions detected",
		},
	)

	// Streaming metrics
	StreamChunksTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_compat_stream_chunks_total",
			Help: "Total number of SSE chunks sent",
		},
		[]string{"model"},
	)

	StreamDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "openai_compat_stream_duration_seconds",
			Help:    "Duration of streaming responses",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
		},
		[]string{"model"},
	)

	StreamErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_compat_stream_errors_total",
			Help: "Total number of streaming errors",
		},
		[]string{"model", "error_type"},
	)

	// Model usage metrics
	ModelRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "openai_compat_model_requests_total",
			Help: "Total requests per model",
		},
		[]string{"model"},
	)

	// Time to first token (streaming)
	TimeToFirstToken = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "openai_compat_time_to_first_token_seconds",
			Help:    "Time from request start to first token in streaming mode",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{"model"},
	)
)

// MetricsRecorder provides methods to record OpenAI API metrics
type MetricsRecorder struct {
	startTime time.Time
	model     string
	endpoint  string
	stream    bool
}

// NewMetricsRecorder creates a new metrics recorder for a request
func NewMetricsRecorder(model, endpoint string, stream bool) *MetricsRecorder {
	ModelRequests.WithLabelValues(model).Inc()
	return &MetricsRecorder{
		startTime: time.Now(),
		model:     model,
		endpoint:  endpoint,
		stream:    stream,
	}
}

// RecordSuccess records a successful request
func (m *MetricsRecorder) RecordSuccess() {
	duration := time.Since(m.startTime).Seconds()
	streamLabel := "false"
	if m.stream {
		streamLabel = "true"
	}

	RequestsTotal.WithLabelValues(m.model, m.endpoint, "success").Inc()
	RequestLatency.WithLabelValues(m.model, m.endpoint, streamLabel).Observe(duration)
}

// RecordError records an error
func (m *MetricsRecorder) RecordError(errorType, errorCode string) {
	duration := time.Since(m.startTime).Seconds()
	streamLabel := "false"
	if m.stream {
		streamLabel = "true"
	}

	RequestsTotal.WithLabelValues(m.model, m.endpoint, "error").Inc()
	RequestLatency.WithLabelValues(m.model, m.endpoint, streamLabel).Observe(duration)
	ErrorsTotal.WithLabelValues(m.model, errorType, errorCode).Inc()
}

// RecordRateLimited records a rate limited request
func (m *MetricsRecorder) RecordRateLimited(limitType string) {
	RequestsTotal.WithLabelValues(m.model, m.endpoint, "rate_limited").Inc()
	RateLimitHits.WithLabelValues(m.model, limitType).Inc()
}

// RecordTokens records token usage
func (m *MetricsRecorder) RecordTokens(promptTokens, completionTokens int) {
	if promptTokens > 0 {
		TokensTotal.WithLabelValues(m.model, "prompt").Add(float64(promptTokens))
		TokensPerRequest.WithLabelValues(m.model, "prompt").Observe(float64(promptTokens))
	}
	if completionTokens > 0 {
		TokensTotal.WithLabelValues(m.model, "completion").Add(float64(completionTokens))
		TokensPerRequest.WithLabelValues(m.model, "completion").Observe(float64(completionTokens))
	}
}

// RecordStreamChunk records a stream chunk
func (m *MetricsRecorder) RecordStreamChunk() {
	StreamChunksTotal.WithLabelValues(m.model).Inc()
}

// RecordStreamComplete records stream completion
func (m *MetricsRecorder) RecordStreamComplete() {
	duration := time.Since(m.startTime).Seconds()
	StreamDuration.WithLabelValues(m.model).Observe(duration)
}

// RecordStreamError records a stream error
func (m *MetricsRecorder) RecordStreamError(errorType string) {
	StreamErrors.WithLabelValues(m.model, errorType).Inc()
}

// RecordFirstToken records time to first token
func (m *MetricsRecorder) RecordFirstToken() {
	duration := time.Since(m.startTime).Seconds()
	TimeToFirstToken.WithLabelValues(m.model).Observe(duration)
}

// RecordSessionCreated records a new session
func RecordSessionCreated() {
	SessionsCreated.Inc()
}

// RecordSessionCollision records a session collision
func RecordSessionCollision() {
	SessionCollisions.Inc()
}

// UpdateActiveSessionsGauge updates the active sessions gauge
func UpdateActiveSessionsGauge(count float64) {
	SessionsActive.Set(count)
}
