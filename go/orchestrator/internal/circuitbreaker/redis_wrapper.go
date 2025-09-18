package circuitbreaker

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// RedisWrapper wraps Redis client with circuit breaker
type RedisWrapper struct {
	client *redis.Client
	cb     *CircuitBreaker
	logger *zap.Logger
}

// NewRedisWrapper creates a Redis wrapper with circuit breaker
func NewRedisWrapper(client *redis.Client, logger *zap.Logger) *RedisWrapper {
	config := GetRedisConfig().ToConfig()
	cb := NewCircuitBreaker("redis", config, logger)

	// Register with metrics collector
	GlobalMetricsCollector.RegisterCircuitBreaker("redis", "session-manager", cb)

	return &RedisWrapper{
		client: client,
		cb:     cb,
		logger: logger,
	}
}

// Ping wraps Redis Ping with circuit breaker
func (rw *RedisWrapper) Ping(ctx context.Context) *redis.StatusCmd {
	var result *redis.StatusCmd

	err := rw.cb.Execute(ctx, func() error {
		result = rw.client.Ping(ctx)
		return result.Err()
	})

	// Record metrics
	state := rw.cb.State()
	success := err == nil && (result == nil || result.Err() == nil)
	GlobalMetricsCollector.RecordRequest("redis", "session-manager", state, success)

	if err != nil {
		// Return a failed status cmd if circuit breaker is open
		result = redis.NewStatusCmd(ctx)
		result.SetErr(err)
	}

	return result
}

// Get wraps Redis Get with circuit breaker
func (rw *RedisWrapper) Get(ctx context.Context, key string) *redis.StringCmd {
	var result *redis.StringCmd

	err := rw.cb.Execute(ctx, func() error {
		result = rw.client.Get(ctx, key)
		// Redis Nil is not considered an error for circuit breaker
		if result.Err() == redis.Nil {
			return nil
		}
		return result.Err()
	})

	// Record metrics
	state := rw.cb.State()
	success := err == nil && (result == nil || result.Err() == nil || result.Err() == redis.Nil)
	GlobalMetricsCollector.RecordRequest("redis", "session-manager", state, success)

	if err != nil {
		result = redis.NewStringCmd(ctx)
		result.SetErr(err)
	}

	return result
}

// Set wraps Redis Set with circuit breaker
func (rw *RedisWrapper) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	var result *redis.StatusCmd

	err := rw.cb.Execute(ctx, func() error {
		result = rw.client.Set(ctx, key, value, expiration)
		return result.Err()
	})

	// Record metrics
	state := rw.cb.State()
	success := err == nil && (result == nil || result.Err() == nil)
	GlobalMetricsCollector.RecordRequest("redis", "session-manager", state, success)

	if err != nil {
		result = redis.NewStatusCmd(ctx)
		result.SetErr(err)
	}

	return result
}

// Del wraps Redis Del with circuit breaker
func (rw *RedisWrapper) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	var result *redis.IntCmd

	err := rw.cb.Execute(ctx, func() error {
		result = rw.client.Del(ctx, keys...)
		return result.Err()
	})

	// Record metrics
	state := rw.cb.State()
	success := err == nil && (result == nil || result.Err() == nil)
	GlobalMetricsCollector.RecordRequest("redis", "session-manager", state, success)

	if err != nil {
		result = redis.NewIntCmd(ctx)
		result.SetErr(err)
	}

	return result
}

// Keys wraps Redis Keys with circuit breaker
func (rw *RedisWrapper) Keys(ctx context.Context, pattern string) *redis.StringSliceCmd {
	var result *redis.StringSliceCmd

	err := rw.cb.Execute(ctx, func() error {
		result = rw.client.Keys(ctx, pattern)
		return result.Err()
	})

	// Record metrics
	state := rw.cb.State()
	success := err == nil && (result == nil || result.Err() == nil)
	GlobalMetricsCollector.RecordRequest("redis", "session-manager", state, success)

	if err != nil {
		result = redis.NewStringSliceCmd(ctx)
		result.SetErr(err)
	}

	return result
}

// Close wraps Redis Close
func (rw *RedisWrapper) Close() error {
	return rw.client.Close()
}

// GetClient returns the underlying Redis client for operations not covered by wrapper
func (rw *RedisWrapper) GetClient() *redis.Client {
	return rw.client
}

// IsCircuitBreakerOpen returns true if the circuit breaker is open
func (rw *RedisWrapper) IsCircuitBreakerOpen() bool {
	return rw.cb.State() == StateOpen
}
