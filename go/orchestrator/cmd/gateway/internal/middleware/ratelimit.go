package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RateLimiter provides rate limiting middleware
type RateLimiter struct {
	redis  *redis.Client
	logger *zap.Logger
	// Default limits (can be overridden per tenant/key)
	defaultRequestsPerMinute int
	defaultBurstSize         int
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(redis *redis.Client, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		redis:                    redis,
		logger:                   logger,
		defaultRequestsPerMinute: 60,  // 60 requests per minute default
		defaultBurstSize:         10,  // Allow burst of 10 requests
	}
}

// Middleware returns the HTTP middleware function
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Get user context from auth middleware
		userCtx, ok := ctx.Value("user").(*auth.UserContext)
		if !ok {
			// If no user context, skip rate limiting (auth will handle it)
			next.ServeHTTP(w, r)
			return
		}

		// Create rate limit key based on user ID (per-user rate limiting)
		key := fmt.Sprintf("ratelimit:user:%s", userCtx.UserID.String())

		// Check rate limit
		allowed, remaining, resetAt := rl.checkRateLimit(ctx, key)

		// Set rate limit headers
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.defaultRequestsPerMinute))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))

		if !allowed {
			// Rate limit exceeded
			rl.logger.Warn("Rate limit exceeded",
				zap.String("user_id", userCtx.UserID.String()),
				zap.String("tenant_id", userCtx.TenantID.String()),
				zap.String("path", r.URL.Path),
			)

			w.Header().Set("Retry-After", fmt.Sprintf("%d", resetAt.Unix()-time.Now().Unix()))
			rl.sendRateLimitError(w)
			return
		}

		// Continue with request
		next.ServeHTTP(w, r)
	})
}

// checkRateLimit checks if the request is allowed under rate limits
func (rl *RateLimiter) checkRateLimit(ctx context.Context, key string) (allowed bool, remaining int, resetAt time.Time) {
	now := time.Now()
	window := now.Truncate(time.Minute) // 1-minute window
	windowKey := fmt.Sprintf("%s:%d", key, window.Unix())

	// Use Redis INCR with expiry for simple rate limiting
	pipe := rl.redis.Pipeline()
	incr := pipe.Incr(ctx, windowKey)
	pipe.Expire(ctx, windowKey, time.Minute+time.Second) // Expire after window + buffer
	_, err := pipe.Exec(ctx)

	if err != nil {
		rl.logger.Error("Rate limit check failed", zap.Error(err))
		// On error, allow the request (fail open)
		return true, rl.defaultRequestsPerMinute, window.Add(time.Minute)
	}

	count := incr.Val()
	remaining = rl.defaultRequestsPerMinute - int(count)
	if remaining < 0 {
		remaining = 0
	}

	resetAt = window.Add(time.Minute)
	allowed = count <= int64(rl.defaultRequestsPerMinute)

	return allowed, remaining, resetAt
}

// sendRateLimitError sends a rate limit exceeded error response
func (rl *RateLimiter) sendRateLimitError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"error": "Rate limit exceeded",
		"message": "Too many requests. Please retry after the rate limit window resets.",
	}

	json.NewEncoder(w).Encode(response)
}

// ServeHTTP implements http.Handler interface
func (rl *RateLimiter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rl.sendRateLimitError(w)
}