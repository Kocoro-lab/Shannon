package openai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// RateLimitPrefix is the Redis key prefix for rate limiting
	RateLimitPrefix = "openai:ratelimit:"

	// RateLimitWindow is the sliding window duration
	RateLimitWindow = time.Minute
)

// RateLimiter handles per-API-key rate limiting for OpenAI endpoints
type RateLimiter struct {
	redis    *redis.Client
	registry *Registry
	logger   *zap.Logger
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(redisClient *redis.Client, registry *Registry, logger *zap.Logger) *RateLimiter {
	return &RateLimiter{
		redis:    redisClient,
		registry: registry,
		logger:   logger,
	}
}

// RateLimitResult contains the result of a rate limit check
type RateLimitResult struct {
	Allowed           bool
	RequestsUsed      int
	RequestsLimit     int
	TokensUsed        int
	TokensLimit       int
	ResetAt           time.Time
	RetryAfterSeconds int
	LimitType         string // "requests" or "tokens" - indicates which limit was exceeded
}

// CheckLimit checks if the request is within rate limits
// Returns whether the request is allowed and rate limit info for headers
func (rl *RateLimiter) CheckLimit(ctx context.Context, apiKeyID, modelName string) (*RateLimitResult, error) {
	limits := rl.registry.GetRateLimit(modelName)

	now := time.Now()
	windowStart := now.Truncate(RateLimitWindow)
	resetAt := windowStart.Add(RateLimitWindow)

	// Build Redis key: openai:ratelimit:{api_key_id}:{model}:{window_minute}
	windowKey := windowStart.Format("200601021504")
	requestKey := fmt.Sprintf("%s%s:%s:%s:req", RateLimitPrefix, apiKeyID, modelName, windowKey)

	// Increment request count
	count, err := rl.redis.Incr(ctx, requestKey).Result()
	if err != nil {
		rl.logger.Error("Rate limit Redis error", zap.Error(err))
		// Fail open on Redis errors
		return &RateLimitResult{Allowed: true}, nil
	}

	// Set expiry on first request
	if count == 1 {
		rl.redis.Expire(ctx, requestKey, RateLimitWindow+time.Second)
	}

	result := &RateLimitResult{
		RequestsUsed:  int(count),
		RequestsLimit: limits.RequestsPerMinute,
		TokensLimit:   limits.TokensPerMinute,
		ResetAt:       resetAt,
	}

	tokensUsed, err := rl.GetTokensUsed(ctx, apiKeyID, modelName)
	if err != nil {
		rl.logger.Error("Rate limit Redis error", zap.Error(err))
		// Fail open on Redis errors
		return &RateLimitResult{Allowed: true}, nil
	}
	result.TokensUsed = tokensUsed

	if int(count) > limits.RequestsPerMinute {
		result.Allowed = false
		result.LimitType = "requests"
		result.RetryAfterSeconds = int(resetAt.Sub(now).Seconds())
		if result.RetryAfterSeconds < 1 {
			result.RetryAfterSeconds = 1
		}

		rl.logger.Warn("Rate limit exceeded",
			zap.String("api_key_id", apiKeyID),
			zap.String("model", modelName),
			zap.Int("requests", int(count)),
			zap.Int("limit", limits.RequestsPerMinute),
		)
		return result, nil
	}

	if limits.TokensPerMinute > 0 && tokensUsed >= limits.TokensPerMinute {
		result.Allowed = false
		result.LimitType = "tokens"
		result.RetryAfterSeconds = int(resetAt.Sub(now).Seconds())
		if result.RetryAfterSeconds < 1 {
			result.RetryAfterSeconds = 1
		}

		rl.logger.Warn("Token rate limit exceeded",
			zap.String("api_key_id", apiKeyID),
			zap.String("model", modelName),
			zap.Int("tokens", tokensUsed),
			zap.Int("limit", limits.TokensPerMinute),
		)
		return result, nil
	}

	result.Allowed = true
	return result, nil
}

// RecordTokens records token usage for rate limiting
func (rl *RateLimiter) RecordTokens(ctx context.Context, apiKeyID, modelName string, tokens int) error {
	now := time.Now()
	windowStart := now.Truncate(RateLimitWindow)
	windowKey := windowStart.Format("200601021504")
	tokenKey := fmt.Sprintf("%s%s:%s:%s:tok", RateLimitPrefix, apiKeyID, modelName, windowKey)

	// Increment token count
	_, err := rl.redis.IncrBy(ctx, tokenKey, int64(tokens)).Result()
	if err != nil {
		return err
	}

	// Set expiry
	rl.redis.Expire(ctx, tokenKey, RateLimitWindow+time.Second)
	return nil
}

// GetTokensUsed returns the current token usage for the window
func (rl *RateLimiter) GetTokensUsed(ctx context.Context, apiKeyID, modelName string) (int, error) {
	now := time.Now()
	windowStart := now.Truncate(RateLimitWindow)
	windowKey := windowStart.Format("200601021504")
	tokenKey := fmt.Sprintf("%s%s:%s:%s:tok", RateLimitPrefix, apiKeyID, modelName, windowKey)

	count, err := rl.redis.Get(ctx, tokenKey).Int()
	if err == redis.Nil {
		return 0, nil
	}
	return count, err
}

// SetRateLimitHeaders sets the rate limit headers on the response
func (rl *RateLimiter) SetRateLimitHeaders(w http.ResponseWriter, result *RateLimitResult) {
	w.Header().Set("X-RateLimit-Limit-Requests", fmt.Sprintf("%d", result.RequestsLimit))
	w.Header().Set("X-RateLimit-Remaining-Requests", fmt.Sprintf("%d", max(0, result.RequestsLimit-result.RequestsUsed)))
	w.Header().Set("X-RateLimit-Limit-Tokens", fmt.Sprintf("%d", result.TokensLimit))
	w.Header().Set("X-RateLimit-Reset-Requests", result.ResetAt.Format(time.RFC3339))

	if result.TokensUsed > 0 {
		w.Header().Set("X-RateLimit-Remaining-Tokens", fmt.Sprintf("%d", max(0, result.TokensLimit-result.TokensUsed)))
	}

	if !result.Allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", result.RetryAfterSeconds))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
