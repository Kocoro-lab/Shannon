package circuitbreaker

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap/zaptest"
)

func TestRedisWrapper_NormalOperations(t *testing.T) {
	// Start miniredis server
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer s.Close()

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	defer client.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewRedisWrapper(client, logger)
	ctx := context.Background()

	// Test Ping
	result := wrapper.Ping(ctx)
	if result.Err() != nil {
		t.Errorf("Ping failed: %v", result.Err())
	}

	// Test Set
	setResult := wrapper.Set(ctx, "test:key", "test:value", time.Minute)
	if setResult.Err() != nil {
		t.Errorf("Set failed: %v", setResult.Err())
	}

	// Test Get
	getResult := wrapper.Get(ctx, "test:key")
	if getResult.Err() != nil {
		t.Errorf("Get failed: %v", getResult.Err())
	}
	if getResult.Val() != "test:value" {
		t.Errorf("Expected 'test:value', got '%s'", getResult.Val())
	}

	// Test Get non-existent key (should return redis.Nil, not trip breaker)
	nilResult := wrapper.Get(ctx, "nonexistent:key")
	if nilResult.Err() != redis.Nil {
		t.Errorf("Expected redis.Nil for non-existent key, got %v", nilResult.Err())
	}

	// Circuit breaker should remain closed
	if wrapper.IsCircuitBreakerOpen() {
		t.Error("Circuit breaker should remain closed for redis.Nil")
	}

	// Test Keys
	keysResult := wrapper.Keys(ctx, "test:*")
	if keysResult.Err() != nil {
		t.Errorf("Keys failed: %v", keysResult.Err())
	}
	if len(keysResult.Val()) != 1 || keysResult.Val()[0] != "test:key" {
		t.Errorf("Expected ['test:key'], got %v", keysResult.Val())
	}

	// Test Del
	delResult := wrapper.Del(ctx, "test:key")
	if delResult.Err() != nil {
		t.Errorf("Del failed: %v", delResult.Err())
	}
	if delResult.Val() != 1 {
		t.Errorf("Expected 1 deleted key, got %d", delResult.Val())
	}
}

func TestRedisWrapper_CircuitBreakerTriggering(t *testing.T) {
	// Create a client pointing to non-existent server
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:9999", // Non-existent Redis server
	})
	defer client.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewRedisWrapper(client, logger)
	ctx := context.Background()

	// Test multiple failures to trip circuit breaker
	for i := 0; i < 4; i++ {
		result := wrapper.Ping(ctx)
		if result.Err() == nil {
			t.Error("Expected ping to fail against non-existent server")
		}
	}

	// Circuit breaker should be open
	if !wrapper.IsCircuitBreakerOpen() {
		t.Error("Expected circuit breaker to be open after repeated failures")
	}

	// Subsequent calls should fail fast
	result := wrapper.Get(ctx, "any:key")
	if result.Err() != ErrCircuitBreakerOpen {
		t.Errorf("Expected circuit breaker open error, got %v", result.Err())
	}
}

func TestRedisWrapper_RedisNilHandling(t *testing.T) {
	// Start miniredis server
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer s.Close()

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	defer client.Close()

	logger := zaptest.NewLogger(t)
	wrapper := NewRedisWrapper(client, logger)
	ctx := context.Background()

	// Get non-existent key multiple times
	for i := 0; i < 10; i++ {
		result := wrapper.Get(ctx, "nonexistent:key")
		if result.Err() != redis.Nil {
			t.Errorf("Expected redis.Nil, got %v", result.Err())
		}
	}

	// Circuit breaker should remain closed (redis.Nil is not a failure)
	if wrapper.IsCircuitBreakerOpen() {
		t.Error("Circuit breaker should remain closed for redis.Nil results")
	}
}
