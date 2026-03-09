package daemon

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "daemon_connections_active",
		Help: "Number of currently connected daemons",
	})

	ClaimsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "daemon_claims_total",
		Help: "Total daemon message claims",
	}, []string{"channel_type", "status"})

	ClaimTimeoutsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "daemon_claim_timeouts_total",
		Help: "Total claim timeouts (no daemon claimed within timeout)",
	})

	ReplyLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "daemon_reply_latency_seconds",
		Help:    "Time from message dispatch to daemon reply",
		Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
	}, []string{"channel_type"})

	OutboundFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "daemon_outbound_failures_total",
		Help: "Failed outbound deliveries to channels",
	}, []string{"channel_type"})
)
