package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/cmd/gateway/internal/handlers"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/cmd/gateway/internal/middleware"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/cmd/gateway/internal/proxy"
	authpkg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	cfg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Discover configuration defaults from features.yaml (best effort)
	var gatewaySkipAuthDefault *bool
	if featuresCfg, err := cfg.Load(); err != nil {
		logger.Warn("Failed to load feature configuration", zap.Error(err))
	} else if featuresCfg != nil && featuresCfg.Gateway.SkipAuth != nil {
		gatewaySkipAuthDefault = featuresCfg.Gateway.SkipAuth
	}
	if envVal := os.Getenv("GATEWAY_SKIP_AUTH"); envVal != "" {
		logger.Warn("Environment variable overrides gateway authentication setting",
			zap.String("env", "GATEWAY_SKIP_AUTH"),
			zap.String("config_key", "gateway.skip_auth"),
			zap.String("value", envVal))
	} else if gatewaySkipAuthDefault != nil {
		if *gatewaySkipAuthDefault {
			_ = os.Setenv("GATEWAY_SKIP_AUTH", "1")
		} else {
			_ = os.Setenv("GATEWAY_SKIP_AUTH", "0")
		}
	}

	// Initialize database
	dbConfig := &db.Config{
		Host:     getEnvOrDefault("POSTGRES_HOST", "postgres"),
		Port:     getEnvOrDefaultInt("POSTGRES_PORT", 5432),
		User:     getEnvOrDefault("POSTGRES_USER", "shannon"),
		Password: getEnvOrDefault("POSTGRES_PASSWORD", "shannon"),
		Database: getEnvOrDefault("POSTGRES_DB", "shannon"),
		SSLMode:  getEnvOrDefault("POSTGRES_SSLMODE", "disable"),
	}

	dbClient, err := db.NewClient(dbConfig, logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer dbClient.Close()

	// Create sqlx.DB wrapper for auth service
	pgDB := sqlx.NewDb(dbClient.GetDB(), "postgres")

	// Initialize Redis client for rate limiting and idempotency
	redisURL := getEnvOrDefault("REDIS_URL", "redis://redis:6379")
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Fatal("Failed to parse Redis URL", zap.Error(err))
	}
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()

	// Test Redis connection
	ctx := context.Background()
	if _, err := redisClient.Ping(ctx).Result(); err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}

	// Initialize auth service (direct access to internal package)
	jwtSecret := getEnvOrDefault("JWT_SECRET", "your-secret-key")
	authService := authpkg.NewService(pgDB, logger, jwtSecret)

	// Connect to orchestrator gRPC
	orchAddr := getEnvOrDefault("ORCHESTRATOR_GRPC", "orchestrator:50052")
	conn, err := grpc.Dial(orchAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50*1024*1024)), // 50MB
	)
	if err != nil {
		logger.Fatal("Failed to connect to orchestrator", zap.Error(err))
	}
	defer conn.Close()

	orchClient := orchpb.NewOrchestratorServiceClient(conn)

	// Create handlers
	taskHandler := handlers.NewTaskHandler(orchClient, pgDB, redisClient, logger)
	sessionHandler := handlers.NewSessionHandler(pgDB, redisClient, logger)
	approvalHandler := handlers.NewApprovalHandler(orchClient, logger)
	scheduleHandler := handlers.NewScheduleHandler(orchClient, pgDB, logger)
	healthHandler := handlers.NewHealthHandler(orchClient, logger)
	openapiHandler := handlers.NewOpenAPIHandler()

	// Create middlewares
	authMiddleware := middleware.NewAuthMiddleware(authService, logger).Middleware
	rateLimiter := middleware.NewRateLimiter(redisClient, logger).Middleware
	idempotencyMiddleware := middleware.NewIdempotencyMiddleware(redisClient, logger).Middleware
	tracingMiddleware := middleware.NewTracingMiddleware(logger).Middleware
	validationMiddleware := middleware.NewValidationMiddleware(logger).Middleware

	// Setup HTTP mux
	mux := http.NewServeMux()

	// Health check (no auth required)
	mux.HandleFunc("GET /health", healthHandler.Health)
	mux.HandleFunc("GET /readiness", healthHandler.Readiness)

	// OpenAPI spec (no auth required)
	mux.HandleFunc("GET /openapi.json", openapiHandler.ServeSpec)

	// Task endpoints (require auth)
	mux.Handle("POST /api/v1/tasks",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(taskHandler.SubmitTask),
						),
					),
				),
			),
		),
	)

	// Unified submit that returns the SSE stream URL (no long-lived stream here)
	mux.Handle("POST /api/v1/tasks/stream",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(taskHandler.SubmitTaskAndGetStreamURL),
						),
					),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/tasks",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(taskHandler.ListTasks),
					),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/tasks/{id}",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					http.HandlerFunc(taskHandler.GetTaskStatus),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/tasks/{id}/stream",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					http.HandlerFunc(taskHandler.StreamTask),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/tasks/{id}/events",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(taskHandler.GetTaskEvents),
					),
				),
			),
		),
	)

	mux.Handle("POST /api/v1/tasks/{id}/cancel",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(taskHandler.CancelTask),
						),
					),
				),
			),
		),
	)

	// Pause workflow
	mux.Handle("POST /api/v1/tasks/{id}/pause",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(taskHandler.PauseTask),
						),
					),
				),
			),
		),
	)

	// Resume workflow
	mux.Handle("POST /api/v1/tasks/{id}/resume",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(taskHandler.ResumeTask),
						),
					),
				),
			),
		),
	)

	// Get control state
	mux.Handle("GET /api/v1/tasks/{id}/control-state",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(taskHandler.GetControlState),
					),
				),
			),
		),
	)

	// Approval endpoints (require auth)
	mux.Handle("POST /api/v1/approvals/decision",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(approvalHandler.SubmitDecision),
						),
					),
				),
			),
		),
	)

	// Session endpoints (require auth)
	mux.Handle("GET /api/v1/sessions",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(sessionHandler.ListSessions),
					),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/sessions/{sessionId}",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					http.HandlerFunc(sessionHandler.GetSession),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/sessions/{sessionId}/history",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					http.HandlerFunc(sessionHandler.GetSessionHistory),
				),
			),
		),
	)

	// Session events (chat history-like, excludes LLM_PARTIAL)
	mux.Handle("GET /api/v1/sessions/{sessionId}/events",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					http.HandlerFunc(sessionHandler.GetSessionEvents),
				),
			),
		),
	)

	// Update session title
	mux.Handle("PATCH /api/v1/sessions/{sessionId}",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(sessionHandler.UpdateSessionTitle),
					),
				),
			),
		),
	)

	// Soft delete session (idempotent)
	mux.Handle("DELETE /api/v1/sessions/{sessionId}",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(sessionHandler.DeleteSession),
					),
				),
			),
		),
	)

	// Schedule endpoints (require auth)
	mux.Handle("POST /api/v1/schedules",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(scheduleHandler.CreateSchedule),
						),
					),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/schedules",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(scheduleHandler.ListSchedules),
					),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/schedules/{id}",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					http.HandlerFunc(scheduleHandler.GetSchedule),
				),
			),
		),
	)

	mux.Handle("GET /api/v1/schedules/{id}/runs",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					http.HandlerFunc(scheduleHandler.GetScheduleRuns),
				),
			),
		),
	)

	mux.Handle("PUT /api/v1/schedules/{id}",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(scheduleHandler.UpdateSchedule),
						),
					),
				),
			),
		),
	)

	mux.Handle("POST /api/v1/schedules/{id}/pause",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(scheduleHandler.PauseSchedule),
						),
					),
				),
			),
		),
	)

	mux.Handle("POST /api/v1/schedules/{id}/resume",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						idempotencyMiddleware(
							http.HandlerFunc(scheduleHandler.ResumeSchedule),
						),
					),
				),
			),
		),
	)

	mux.Handle("DELETE /api/v1/schedules/{id}",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					rateLimiter(
						http.HandlerFunc(scheduleHandler.DeleteSchedule),
					),
				),
			),
		),
	)

	// SSE/WebSocket reverse proxy to admin server
	adminURL := getEnvOrDefault("ADMIN_SERVER", "http://orchestrator:8081")
	streamProxy, err := proxy.NewStreamingProxy(adminURL, logger)
	if err != nil {
		logger.Fatal("Failed to create streaming proxy", zap.Error(err))
	}

	// Proxy SSE and WebSocket endpoints
	mux.Handle("/api/v1/stream/sse",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					streamProxy,
				),
			),
		),
	)

	mux.Handle("/api/v1/stream/ws",
		tracingMiddleware(
			authMiddleware(
				validationMiddleware(
					streamProxy,
				),
			),
		),
	)

	// Timeline proxy: GET /api/v1/tasks/{id}/timeline -> admin /timeline
	mux.Handle("GET /api/v1/tasks/{id}/timeline",
		tracingMiddleware(
			authMiddleware(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					id := r.PathValue("id")
					if id == "" {
						http.Error(w, "{\"error\":\"Task ID required\"}", http.StatusBadRequest)
						return
					}
					// Rebuild target URL
					target := strings.TrimRight(adminURL, "/") + "/timeline?workflow_id=" + id
					if raw := r.URL.RawQuery; raw != "" {
						target += "&" + raw
					}
					req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						logger.Error("timeline proxy error", zap.Error(err))
						http.Error(w, "{\"error\":\"upstream unavailable\"}", http.StatusBadGateway)
						return
					}
					defer resp.Body.Close()
					for k, v := range resp.Header {
						for _, vv := range v {
							w.Header().Add(k, vv)
						}
					}
					w.WriteHeader(resp.StatusCode)
					_, _ = io.Copy(w, resp.Body)
				}),
			),
		),
	)

	// CORS middleware for all routes (development friendly)
	corsHandler := corsMiddleware(mux)

	// Create HTTP server
	port := getEnvOrDefaultInt("PORT", 8080)
	server := &http.Server{
		Addr:         ":" + strconv.Itoa(port),
		Handler:      corsHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,  // No write timeout for SSE/streaming support
		IdleTimeout:  300 * time.Second, // 5 minutes idle for long-lived SSE connections
	}

	// Start server in goroutine
	go func() {
		logger.Info("Gateway starting", zap.Int("port", port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start gateway", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Gateway shutting down...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Gateway forced to shutdown", zap.Error(err))
	}

	logger.Info("Gateway stopped")
}

// corsMiddleware adds CORS headers for development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isStreaming := strings.HasPrefix(r.URL.Path, "/api/v1/stream/")

		allowedHeaders := "Content-Type, Authorization, X-API-Key, X-User-Id, Idempotency-Key, traceparent, tracestate, Cache-Control, Last-Event-ID"

		if !isStreaming {
			// Allow CORS for development
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
			w.Header().Set("Access-Control-Max-Age", "3600")
		} else {
			// Streaming endpoints also need CORS headers for GET requests
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
			w.Header().Set("Access-Control-Max-Age", "3600")
		}

		if r.Method == http.MethodOptions {
			// Handle preflight - headers already set above
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Helper functions
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvOrDefaultInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
