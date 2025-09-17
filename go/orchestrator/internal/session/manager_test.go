package session

import (
	"context"
	"testing"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// getGauge returns a single-sample gauge value by metric name; 0 if missing
func getGauge(name string) float64 {
	mfs, _ := prometheus.DefaultGatherer.Gather()
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.Metric {
				if g := m.GetGauge(); g != nil {
					return g.GetValue()
				}
			}
		}
	}
	return 0
}

// getCounter returns a counter value by metric name; 0 if missing
func getCounter(name string) float64 {
	mfs, _ := prometheus.DefaultGatherer.Gather()
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.Metric {
				if c := m.GetCounter(); c != nil {
					return c.GetValue()
				}
			}
		}
	}
	return 0
}

func TestSessionCacheSizeGaugeUpdatesOnCreateAndDelete(t *testing.T) {
	// Attempt to create a manager; skip test if Redis is unavailable
	mgr, err := NewManager("localhost:6379", zap.NewNop())
	if err != nil {
		t.Skipf("skipping: redis not available: %v", err)
		return
	}
	defer mgr.Close()

	base := getGauge("shannon_session_cache_size")

	s, err := mgr.CreateSession(context.Background(), "u", "t", map[string]interface{}{"x": 1})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Creation should increase local cache and gauge
	afterCreate := getGauge("shannon_session_cache_size")
	if afterCreate < base+1 {
		t.Fatalf("expected cache size to increase, before=%v after=%v", base, afterCreate)
	}

	// Delete and verify decrease
	if err := mgr.DeleteSession(context.Background(), s.ID); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	afterDelete := getGauge("shannon_session_cache_size")
	if afterDelete > afterCreate {
		t.Fatalf("expected cache size to not increase on delete, before=%v after=%v", afterCreate, afterDelete)
	}
}

func TestSessionCacheEvictionIncrementsCounter(t *testing.T) {
	mgr, err := NewManager("localhost:6379", zap.NewNop())
	if err != nil {
		t.Skipf("skipping: redis not available: %v", err)
		return
	}
	defer mgr.Close()

	// Make eviction easy to trigger
	mgr.mu.Lock()
	mgr.maxSessions = 2
	mgr.mu.Unlock()

	before := getCounter("shannon_session_cache_evictions_total")

	// Create multiple sessions
	for i := 0; i < 4; i++ {
		_, err := mgr.CreateSession(context.Background(), "u", "t", map[string]interface{}{"i": i})
		if err != nil {
			t.Fatalf("CreateSession %d failed: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	after := getCounter("shannon_session_cache_evictions_total")
	if after <= before {
		t.Fatalf("expected evictions to increment, before=%v after=%v", before, after)
	}

	// Silence unused import warning for metrics
	_ = metrics.SessionsCreated
}
