package personas

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConcurrencyController manages concurrent persona selections
type ConcurrencyController struct {
	maxConcurrent int
	current       int
	semaphore     chan struct{}
	mu            sync.Mutex
	metrics       *Metrics
	logger        *zap.Logger
}

// NewConcurrencyController creates a new concurrency controller
func NewConcurrencyController(maxConcurrent int, metrics *Metrics, logger *zap.Logger) *ConcurrencyController {
	return &ConcurrencyController{
		maxConcurrent: maxConcurrent,
		semaphore:     make(chan struct{}, maxConcurrent),
		metrics:       metrics,
		logger:        logger,
	}
}

// AcquireSlot acquires a slot for persona selection
func (cc *ConcurrencyController) AcquireSlot(ctx context.Context) error {
	select {
	case cc.semaphore <- struct{}{}:
		cc.mu.Lock()
		cc.current++
		if cc.metrics != nil {
			cc.metrics.ConcurrentSelections.Set(float64(cc.current))
		}
		cc.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second): // 5 second timeout
		if cc.logger != nil {
			cc.logger.Warn("Concurrency slot acquisition timeout",
				zap.Int("current", cc.current),
				zap.Int("max", cc.maxConcurrent))
		}
		return ErrTooManySelections
	}
}

// ReleaseSlot releases a persona selection slot
func (cc *ConcurrencyController) ReleaseSlot() {
	select {
	case <-cc.semaphore:
		cc.mu.Lock()
		cc.current--
		if cc.metrics != nil {
			cc.metrics.ConcurrentSelections.Set(float64(cc.current))
		}
		cc.mu.Unlock()
	default:
		// This should not happen in normal operation
		if cc.logger != nil {
			cc.logger.Error("Attempted to release more slots than acquired")
		}
	}
}

// CurrentLoad returns the current number of active selections
func (cc *ConcurrencyController) CurrentLoad() int {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.current
}

// LoadPercentage returns the current load as a percentage
func (cc *ConcurrencyController) LoadPercentage() float64 {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.maxConcurrent == 0 {
		return 0
	}
	return float64(cc.current) / float64(cc.maxConcurrent) * 100
}

// IsAtCapacity returns true if at maximum capacity
func (cc *ConcurrencyController) IsAtCapacity() bool {
	return cc.CurrentLoad() >= cc.maxConcurrent
}
