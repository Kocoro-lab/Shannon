package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// IdempotencyMiddleware provides idempotency key support
type IdempotencyMiddleware struct {
	redis  *redis.Client
	logger *zap.Logger
	ttl    time.Duration
}

// NewIdempotencyMiddleware creates a new idempotency middleware
func NewIdempotencyMiddleware(redis *redis.Client, logger *zap.Logger) *IdempotencyMiddleware {
	return &IdempotencyMiddleware{
		redis:  redis,
		logger: logger,
		ttl:    24 * time.Hour, // Store idempotency results for 24 hours
	}
}

// IdempotencyResult stores the cached result of an idempotent request
type IdempotencyResult struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
	Timestamp  time.Time           `json:"timestamp"`
}

// responseRecorder captures the response for caching
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       *bytes.Buffer
	written    bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.written {
		r.statusCode = code
		r.written = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// Middleware returns the HTTP middleware function
func (im *IdempotencyMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only process POST requests with idempotency key
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		idempotencyKey := r.Header.Get("Idempotency-Key")
		if idempotencyKey == "" {
			// No idempotency key, proceed normally
			next.ServeHTTP(w, r)
			return
		}

		ctx := r.Context()

		// Generate cache key including request details
		cacheKey := im.generateCacheKey(r, idempotencyKey)

		// Check if we have a cached result
		cached, err := im.getCachedResult(ctx, cacheKey)
		if err == nil && cached != nil {
			// Return cached response
			im.logger.Debug("Returning cached idempotent response",
				zap.String("idempotency_key", idempotencyKey),
				zap.String("path", r.URL.Path),
			)

			// Set headers
			for key, values := range cached.Headers {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}

			// Add idempotency header
			w.Header().Set("X-Idempotency-Cached", "true")
			w.Header().Set("X-Idempotency-Key", idempotencyKey)

			// Write response
			w.WriteHeader(cached.StatusCode)
			w.Write(cached.Body)
			return
		}

		// No cached result, process request and cache the response
		recorder := newResponseRecorder(w)

		// Process the request
		next.ServeHTTP(recorder, r)

		// Only cache successful responses (2xx)
		if recorder.statusCode >= 200 && recorder.statusCode < 300 {
			result := &IdempotencyResult{
				StatusCode: recorder.statusCode,
				Headers:    recorder.Header(),
				Body:       recorder.body.Bytes(),
				Timestamp:  time.Now(),
			}

			// Store result in cache
			if err := im.cacheResult(ctx, cacheKey, result); err != nil {
				im.logger.Error("Failed to cache idempotent response",
					zap.Error(err),
					zap.String("idempotency_key", idempotencyKey),
				)
			} else {
				im.logger.Debug("Cached idempotent response",
					zap.String("idempotency_key", idempotencyKey),
					zap.String("path", r.URL.Path),
					zap.Int("status_code", recorder.statusCode),
				)
			}
		}
	})
}

// generateCacheKey generates a unique cache key for the request
func (im *IdempotencyMiddleware) generateCacheKey(r *http.Request, idempotencyKey string) string {
	// Include user context in the key to prevent cross-user cache pollution
	userID := ""
	if userCtx := r.Context().Value("user"); userCtx != nil {
		if uc, ok := userCtx.(*auth.UserContext); ok {
			userID = uc.UserID.String()
		}
	}

	// Create a hash of the request details
	h := sha256.New()
	h.Write([]byte(idempotencyKey))
	h.Write([]byte(userID))
	h.Write([]byte(r.URL.Path))

	// Include request body in the hash (for extra safety)
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		h.Write(body)
	}

	hash := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("idempotency:%s", hash[:16]) // Use first 16 chars of hash
}

// getCachedResult retrieves a cached result from Redis
func (im *IdempotencyMiddleware) getCachedResult(ctx context.Context, key string) (*IdempotencyResult, error) {
	data, err := im.redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var result IdempotencyResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// cacheResult stores a result in Redis
func (im *IdempotencyMiddleware) cacheResult(ctx context.Context, key string, result *IdempotencyResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return im.redis.Set(ctx, key, data, im.ttl).Err()
}

// ServeHTTP implements http.Handler interface
func (im *IdempotencyMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error":"Direct access not allowed"}`))
}
