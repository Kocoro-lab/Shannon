package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
	authpkg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/auth"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
	cfg "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/db"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/health"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/httpapi"
	_ "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/metrics" // Import for side effects
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/registry"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/server"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/temporal"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/tracing"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"

	"context"

	agentpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/agent"
	orchpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/orchestrator"
	"github.com/jmoiron/sqlx"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	// Create a root context for background services
	ctx := context.Background()
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	// Start circuit breaker metrics collection
	circuitbreaker.StartMetricsCollection()

	// ------------------------------------------------------------------
	// Bring up Health manager and HTTP endpoints early so they respond
	// even if later components (Temporal worker, etc.) are still starting.
	// ------------------------------------------------------------------
	hm := health.NewManager(logger)
	healthPort := getEnvOrDefaultInt("HEALTH_PORT", 8081)
	// Shared HTTP mux for admin endpoints (health, approvals, metrics if desired)
	httpMux := http.NewServeMux()
	// Register health endpoints on shared mux
	healthHandler := health.NewHTTPHandler(hm, logger)
	healthHandler.RegisterRoutes(httpMux)
	// Configure streaming ring capacity via env (polish)
	if capStr := os.Getenv("STREAMING_RING_CAPACITY"); capStr != "" {
		if n, err := strconv.Atoi(capStr); err == nil && n > 0 {
			streaming.Configure(n)
		}
	}
    // Register streaming SSE/WS on the shared admin HTTP mux (community-ready)
    httpapi.NewStreamingHandler(streaming.Get(), logger).RegisterRoutes(httpMux)

	// Start background checks and shared HTTP server
	go func() {
		_ = hm.Start(ctx)
		server := &http.Server{
			Addr:         ":" + strconv.Itoa(healthPort),
			Handler:      httpMux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		logger.Info("Admin HTTP server listening", zap.Int("port", healthPort))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Admin HTTP server failed", zap.Error(err))
		}
	}()

	// Initialize database client
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
		logger.Fatal("Failed to initialize database client", zap.Error(err))
	}
	defer dbClient.Close()

	// Register database health checker immediately
	if dbClient != nil {
		dbChecker := health.NewDatabaseHealthChecker(dbClient.GetDB(), dbClient.Wrapper(), logger)
		_ = hm.RegisterChecker(dbChecker)
        // Initialize persistent event store for streaming logs
        streaming.InitializeEventStore(dbClient, logger)
	}

	// Start configuration manager (hot-reload) - ASYNC to prevent deadlock
	var shannonCfgMgr *cfg.ShannonConfigManager
	cfgReady := make(chan struct{})
	go func() {
		configDir := "/app/config" // default inside container; can be overridden later
		configMgr, err := cfg.NewConfigManager(configDir, logger)
		if err != nil {
			logger.Warn("Config manager init failed", zap.Error(err))
			return
		}

		// Use context with timeout to prevent deadlock
		configCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := configMgr.Start(configCtx); err != nil {
			logger.Warn("Config manager start failed", zap.Error(err))
			return
		}

		shannonCfgMgr = cfg.NewShannonConfigManager(configMgr, logger)
		if err := shannonCfgMgr.Initialize(); err != nil {
			logger.Warn("Shannon config init failed", zap.Error(err))
		} else {
			logger.Info("Shannon configuration loaded successfully")
			// Signal that config is available for dependent components
			close(cfgReady)

			// Register pricing validator and hot-reload handler for models.yaml
			configMgr.RegisterValidator("models.yaml", func(m map[string]interface{}) error { return pricing.ValidateMap(m) })
			configMgr.RegisterHandler("models.yaml", func(ev cfg.ChangeEvent) error {
				pricing.Reload()
				logger.Info("Pricing configuration reloaded", zap.String("file", ev.File), zap.String("action", ev.Action))
				return nil
			})

			// Initialize policy engine from Shannon configuration
			if shCfg := shannonCfgMgr.GetConfig(); shCfg != nil {
				if err := activities.InitializePolicyEngineFromShannonConfig(&shCfg.Policy); err != nil {
					logger.Warn("Failed to initialize policy engine from Shannon config", zap.Error(err))
				} else {
					// Register policy reload handler for .rego file changes
					if engine := activities.GetPolicyEngine(); engine != nil {
						configMgr.RegisterPolicyHandler(func() error {
							logger.Info("Reloading policy engine due to .rego file change")
							return engine.LoadPolicies()
						})
						logger.Info("Policy reload handler registered for .rego files")
					}
				}

				// Initialize embedding service and vectordb from Shannon config
				{
					// Embeddings
					base := shCfg.Agents.LLMServiceEndpoint
					if base == "" {
						base = getEnvOrDefault("LLM_SERVICE_URL", "http://llm-service:8000")
					}
					ecfg := embeddings.Config{
						BaseURL:      base,
						DefaultModel: shCfg.Vector.DefaultModel,
						Timeout:      5 * time.Second,
						EnableRedis:  shCfg.Vector.UseRedisCache,
						RedisAddr:    shCfg.Vector.RedisAddr,
						CacheTTL:     shCfg.Vector.CacheTTL,
						MaxLRU:       2048,
					}
					var cache embeddings.EmbeddingCache
					if ecfg.EnableRedis {
						if c, err := embeddings.NewRedisCache(ecfg.RedisAddr); err == nil {
							cache = c
						} else {
							logger.Warn("Embeddings Redis cache init failed", zap.Error(err))
						}
					}
					embeddings.Initialize(ecfg, cache)
					// Vector DB - auto-enable if configuration is present
					vectorEnabled := shCfg.Vector.Enabled
					// If vector section is configured with host/port, assume enabled
					if !vectorEnabled && (shCfg.Vector.Host == "qdrant" || shCfg.Vector.Port == 6333) && shCfg.Vector.TaskEmbeddings != "" {
						vectorEnabled = true
						logger.Info("Vector DB auto-enabled due to configuration presence")
					}
					if vectorEnabled && shCfg.Degradation.FallbackBehaviors["vector_search"] != "skip" {
						vcfg := vectordb.Config{
							Enabled:        true,
							Host:           shCfg.Vector.Host,
							Port:           shCfg.Vector.Port,
							TaskEmbeddings: shCfg.Vector.TaskEmbeddings,
							Summaries:      shCfg.Vector.Summaries,
							ToolResults:    shCfg.Vector.ToolResults,
							Cases:          shCfg.Vector.Cases,
							DocumentChunks: shCfg.Vector.DocumentChunks,
							TopK:           shCfg.Vector.TopK,
							Threshold:      shCfg.Vector.Threshold,
							Timeout:        shCfg.Vector.Timeout,
						}
						vectordb.Initialize(vcfg)
					} else {
						logger.Info("Vector DB disabled or set to skip by fallback")
					}

					// Initialize tracing from Shannon configuration
					tracingCfg := tracing.Config{
						Enabled:      shCfg.Tracing.Enabled,
						ServiceName:  shCfg.Tracing.ServiceName,
						OTLPEndpoint: shCfg.Tracing.OTLPEndpoint,
					}
					if err := tracing.Initialize(tracingCfg, logger); err != nil {
						logger.Warn("Failed to initialize tracing", zap.Error(err))
					}

					// Configure streaming ring capacity from config
					if shCfg.Streaming.RingCapacity > 0 {
						streaming.Configure(shCfg.Streaming.RingCapacity)
					}
				}
			} else {
				// Fallback to environment variables if Shannon config not available
				if err := activities.InitializePolicyEngine(); err != nil {
					logger.Warn("Failed to initialize policy engine from environment", zap.Error(err))
				} else {
					// Register policy reload handler for .rego file changes
					if engine := activities.GetPolicyEngine(); engine != nil {
						configMgr.RegisterPolicyHandler(func() error {
							logger.Info("Reloading policy engine due to .rego file change")
							return engine.LoadPolicies()
						})
						logger.Info("Policy reload handler registered for .rego files (env fallback)")
					}
				}
			}
		}
	}()

	// Create orchestrator service first (Temporal client may not be ready yet)
	orchestratorService, err := server.NewOrchestratorService(nil, dbClient, logger)
	if err != nil {
		logger.Fatal("Failed to create orchestrator service", zap.Error(err))
	}

	// Provide workflow defaults from Shannon config/env at submission time
	orchestratorService.SetWorkflowDefaultsProvider(func() bool {
		// Default
		bypass := true
		// Try Shannon config if available
		if shannonCfgMgr != nil {
			if shCfg := shannonCfgMgr.GetConfig(); shCfg != nil {
				bypass = shCfg.Workflow.BypassSingleResult
			}
		}
		// Env override
		if v := os.Getenv("WORKFLOW_SYNTH_BYPASS_SINGLE"); v != "" {
			bypass = strings.EqualFold(v, "true") || v == "1"
		}
		return bypass
	})

	// Start gRPC server early (independent of Temporal readiness) and expose services
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		logger.Fatal("Failed to listen", zap.Error(err))
	}

	// Initialize authentication middleware (prefer waiting briefly for config)
	var authMiddleware *authpkg.Middleware
	var authService *authpkg.Service
	var jwtManager *authpkg.JWTManager
	var shCfgForAuth *cfg.ShannonConfig

	// Wait for config to be ready (shannonCfgMgr is set in the goroutine)
	select {
	case <-cfgReady:
		// Config is ready, shannonCfgMgr should be set
		if shannonCfgMgr != nil {
			shCfgForAuth = shannonCfgMgr.GetConfig()
			logger.Info("Auth init using loaded config")
		}
	case <-time.After(5 * time.Second):
		// Timeout waiting for config
		logger.Warn("Auth init timeout waiting for config; using defaults")
	}
	if shCfgForAuth != nil {
		dbx := sqlx.NewDb(dbClient.GetDB(), "postgres")
		jwtManager = authpkg.NewJWTManager(shCfgForAuth.Auth.JWTSecret, shCfgForAuth.Auth.AccessTokenExpiry, shCfgForAuth.Auth.RefreshTokenExpiry)
		authService = authpkg.NewService(dbx, logger, shCfgForAuth.Auth.JWTSecret)
		authMiddleware = authpkg.NewMiddleware(authService, jwtManager, shCfgForAuth.Auth.SkipAuth)
		logger.Info("Auth middleware initialized from config",
			zap.Bool("skip_auth", shCfgForAuth.Auth.SkipAuth),
			zap.Bool("enabled", shCfgForAuth.Auth.Enabled))
	} else {
		// Fallback if config manager not available
		dbx := sqlx.NewDb(dbClient.GetDB(), "postgres")
		jwtManager = authpkg.NewJWTManager("change-this-to-a-secure-32-char-minimum-secret", 30*time.Minute, 7*24*time.Hour)
		authService = authpkg.NewService(dbx, logger, "change-this-to-a-secure-32-char-minimum-secret")
		authMiddleware = authpkg.NewMiddleware(authService, jwtManager, true)
		logger.Info("Auth middleware initialized with defaults (skip_auth=true)")
	}

	// Register minimal HTTP auth endpoints on the admin mux
	httpapi.NewAuthHTTPHandler(authService, logger).RegisterRoutes(httpMux)

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(authMiddleware.UnaryServerInterceptor()),
	)
	// Register orchestrator service and session service
	server.RegisterOrchestratorServiceServer(grpcServer, orchestratorService)
	sessionService := server.NewSessionService(orchestratorService.SessionManager(), logger)
	server.RegisterSessionServiceServer(grpcServer, sessionService)
	// Register streaming gRPC service (backed by in-process manager)
	streamingSvc := server.NewStreamingService(streaming.Get(), logger)
	orchpb.RegisterStreamingServiceServer(grpcServer, streamingSvc)
	reflection.Register(grpcServer)

	go func() {
		logger.Info("Orchestrator service listening", zap.String("address", ":50052"))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("Failed to serve", zap.Error(err))
		}
	}()

	// Create and start Health system (augment configuration from Shannon config)
	agentAddr := getEnvOrDefault("AGENT_CORE_ADDR", "agent-core:50051")
	llmBase := getEnvOrDefault("LLM_SERVICE_URL", "http://llm-service:8000")

	// Create health configuration from Shannon config
	var healthConfig *health.HealthConfiguration
	if shannonCfgMgr != nil {
		if shCfg := shannonCfgMgr.GetConfig(); shCfg != nil {
			// Convert Shannon health config to health manager config
			healthConfig = &health.HealthConfiguration{
				Enabled:       shCfg.Health.Enabled,
				CheckInterval: shCfg.Health.CheckInterval,
				GlobalTimeout: shCfg.Health.Timeout,
				Checks:        make(map[string]health.CheckConfig),
			}

			// Convert per-check configuration
			for name, checkCfg := range shCfg.Health.Checks {
				healthConfig.Checks[name] = health.CheckConfig{
					Enabled:  checkCfg.Enabled,
					Critical: checkCfg.Critical,
					Timeout:  checkCfg.Timeout,
					Interval: checkCfg.Interval,
				}
			}

			// Update endpoints and port from config
			if shCfg.Health.Port > 0 {
				healthPort = shCfg.Health.Port
			} else if shCfg.Service.HealthPort > 0 {
				healthPort = shCfg.Service.HealthPort
			}
			if s := shCfg.Agents.AgentCoreEndpoint; s != "" {
				agentAddr = s
			}
			if s := shCfg.Agents.LLMServiceEndpoint; s != "" {
				llmBase = s
			}
		}
	}

	// Apply configuration to existing health manager
	if healthConfig != nil {
		_ = hm.UpdateConfiguration(healthConfig)
	}

	// Register configuration change callbacks
	if shannonCfgMgr != nil {
		shannonCfgMgr.RegisterCallback(func(oldConfig, newConfig *cfg.ShannonConfig) error {
			// Update health configuration
			newHealthConfig := &health.HealthConfiguration{
				Enabled:       newConfig.Health.Enabled,
				CheckInterval: newConfig.Health.CheckInterval,
				GlobalTimeout: newConfig.Health.Timeout,
				Checks:        make(map[string]health.CheckConfig),
			}

			// Convert per-check configuration
			for name, checkCfg := range newConfig.Health.Checks {
				newHealthConfig.Checks[name] = health.CheckConfig{
					Enabled:  checkCfg.Enabled,
					Critical: checkCfg.Critical,
					Timeout:  checkCfg.Timeout,
					Interval: checkCfg.Interval,
				}
			}

			// Update health manager configuration
			if err := hm.UpdateConfiguration(newHealthConfig); err != nil {
				logger.Error("Failed to update health configuration", zap.Error(err))
			}

			// Check for policy configuration changes and reload policy engine
			policyChanged := oldConfig.Policy.Enabled != newConfig.Policy.Enabled ||
				oldConfig.Policy.Mode != newConfig.Policy.Mode ||
				oldConfig.Policy.Path != newConfig.Policy.Path ||
				oldConfig.Policy.FailClosed != newConfig.Policy.FailClosed ||
				oldConfig.Policy.Environment != newConfig.Policy.Environment

			if policyChanged {
				logger.Info("Policy configuration changed, reinitializing policy engine",
					zap.Bool("old_enabled", oldConfig.Policy.Enabled),
					zap.Bool("new_enabled", newConfig.Policy.Enabled),
					zap.String("old_mode", oldConfig.Policy.Mode),
					zap.String("new_mode", newConfig.Policy.Mode),
				)

				if err := activities.InitializePolicyEngineFromShannonConfig(&newConfig.Policy); err != nil {
					logger.Error("Failed to reinitialize policy engine after config change", zap.Error(err))
					return err
				}
			}

			return nil
		})
	}

	// Register remaining health checkers now that dependencies are available
	// Redis checker from session manager
	if orchestratorService != nil && orchestratorService.SessionManager() != nil {
		if rw := orchestratorService.SessionManager().RedisWrapper(); rw != nil {
			rc := health.NewRedisHealthChecker(rw.GetClient(), rw, logger)
			_ = hm.RegisterChecker(rc)

			// Initialize streaming manager with Redis client
			streaming.InitializeRedis(rw.GetClient(), logger)
			logger.Info("Initialized streaming manager with Redis Streams")
		}
	}

	// Agent Core checker (best-effort)
	if agentAddr != "" {
		conn, err := grpc.Dial(agentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			client := agentpb.NewAgentServiceClient(conn)
			ac := health.NewAgentCoreHealthChecker(client, conn, logger)
			_ = hm.RegisterChecker(ac)
		} else {
			logger.Warn("Agent Core health dial failed", zap.Error(err))
		}
	}

	// LLM service checker
	lc := health.NewLLMServiceHealthChecker(llmBase, logger)
	_ = hm.RegisterChecker(lc)
	logger.Info("Health checkers registered; starting gRPC setup")

	// Start Prometheus metrics endpoint on configured port
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		port := cfg.MetricsPort(2112)
		addr := ":" + fmt.Sprintf("%d", port)
		logger.Info("Metrics server listening", zap.String("address", addr))
		if err := http.ListenAndServe(addr, nil); err != nil {
			logger.Error("Failed to start metrics server", zap.Error(err))
		}
	}()

	// Initialize Temporal client and worker in background
	var w worker.Worker
	go func() {
		host := getEnvOrDefault("TEMPORAL_HOST", "temporal:7233")
		// TCP pre-check
		for i := 1; i <= 60; i++ {
			c, err := net.DialTimeout("tcp", host, 2*time.Second)
			if err == nil {
				_ = c.Close()
				break
			}
			logger.Warn("Waiting for Temporal TCP endpoint", zap.String("host", host), zap.Int("attempt", i))
			time.Sleep(1 * time.Second)
		}
		// Dial SDK with retry
		var tClient client.Client
		var err error
		for attempt := 1; ; attempt++ {
			tClient, err = client.Dial(client.Options{HostPort: host, Logger: temporal.NewZapAdapter(logger)})
			if err == nil {
				break
			}
			delay := time.Duration(attempt)
			if delay > 15 {
				delay = 15
			}
			logger.Warn("Temporal not ready, retrying", zap.Int("attempt", attempt), zap.String("host", host), zap.Duration("sleep", delay*time.Second), zap.Error(err))
			time.Sleep(delay * time.Second)
		}
		orchestratorService.SetTemporalClient(tClient)

		// After Temporal client is ready, start Approvals HTTP API if configured
		approvalsToken := getEnvOrDefault("APPROVALS_AUTH_TOKEN", "")
		// Register approvals endpoint on the shared admin mux (same port as health)
		httpapi.NewApprovalHandler(tClient, logger, approvalsToken).RegisterRoutes(httpMux)
		logger.Info("Approvals API registered on admin HTTP server", zap.Int("port", healthPort), zap.String("path", "/approvals/decision"))

		// Register generic event ingestion endpoint for services (Python LLM, etc.)
		eventsToken := getEnvOrDefault("EVENTS_AUTH_TOKEN", approvalsToken)
		httpapi.NewIngestHandler(logger, eventsToken).RegisterRoutes(httpMux)
		logger.Info("Events ingest API registered on admin HTTP server", zap.Int("port", healthPort), zap.String("path", "/events"))

		// Register timeline builder endpoint on admin HTTP mux
		httpapi.NewTimelineHandler(tClient, dbClient, logger).RegisterRoutes(httpMux)
		logger.Info("Timeline API registered on admin HTTP server", zap.Int("port", healthPort), zap.String("path", "/timeline"))

		// Create workers (single queue or priority queues)
		priorityQueues := strings.EqualFold(os.Getenv("PRIORITY_QUEUES"), "on") || os.Getenv("PRIORITY_QUEUES") == "1" || strings.EqualFold(os.Getenv("PRIORITY_QUEUES"), "true")

		// Create and configure registry once
		registryConfig := &registry.RegistryConfig{
			EnableBudgetedWorkflows:  true,
			EnableStreamingWorkflows: true,
			EnableApprovalWorkflows:  true,
		}
		orchestratorRegistry := registry.NewOrchestratorRegistry(
			registryConfig,
			logger,
			dbClient.GetDB(),
			orchestratorService.SessionManager(),
		)

		startWorker := func(queue string, actSize, wfSize int) worker.Worker {
			wk := worker.New(tClient, queue, worker.Options{
				MaxConcurrentActivityExecutionSize:     actSize,
				MaxConcurrentWorkflowTaskExecutionSize: wfSize,
			})
			if err := orchestratorRegistry.RegisterWorkflows(wk); err != nil {
				logger.Error("Failed to register workflows", zap.String("queue", queue), zap.Error(err))
			}
			if err := orchestratorRegistry.RegisterActivities(wk); err != nil {
				logger.Error("Failed to register activities", zap.String("queue", queue), zap.Error(err))
			}
			go func(q string) {
				logger.Info("Temporal worker started", zap.String("queue", q))
				if err := wk.Run(worker.InterruptCh()); err != nil {
					logger.Error("Temporal worker exited with error", zap.String("queue", q), zap.Error(err))
				}
			}(queue)
			return wk
		}

		if priorityQueues {
			// Read concurrency from env (defaults shown)
			ca := getEnvOrDefaultInt("WORKER_ACT_CRITICAL", 12)
			cw := getEnvOrDefaultInt("WORKER_WF_CRITICAL", 12)
			ha := getEnvOrDefaultInt("WORKER_ACT_HIGH", 10)
			hw := getEnvOrDefaultInt("WORKER_WF_HIGH", 10)
			na := getEnvOrDefaultInt("WORKER_ACT_NORMAL", 8)
			nw := getEnvOrDefaultInt("WORKER_WF_NORMAL", 8)
			la := getEnvOrDefaultInt("WORKER_ACT_LOW", 4)
			lw := getEnvOrDefaultInt("WORKER_WF_LOW", 4)

			logger.Info("🚀 Priority queue mode enabled - starting workers for multiple priority levels",
				zap.String("mode", "PRIORITY_QUEUES"),
				zap.Int("critical_activities", ca),
				zap.Int("critical_workflows", cw),
				zap.Int("high_activities", ha),
				zap.Int("high_workflows", hw),
				zap.Int("normal_activities", na),
				zap.Int("normal_workflows", nw),
				zap.Int("low_activities", la),
				zap.Int("low_workflows", lw),
			)

			_ = startWorker("shannon-tasks-critical", ca, cw)
			_ = startWorker("shannon-tasks-high", ha, hw)
			w = startWorker("shannon-tasks", na, nw) // normal
			_ = startWorker("shannon-tasks-low", la, lw)
		} else {
			// Single-queue mode
			sa := getEnvOrDefaultInt("WORKER_ACT", 10)
			sw := getEnvOrDefaultInt("WORKER_WF", 10)

			logger.Info("📦 Single queue mode - all tasks use default priority",
				zap.String("mode", "SINGLE_QUEUE"),
				zap.String("queue", "shannon-tasks"),
				zap.Int("activities", sa),
				zap.Int("workflows", sw),
			)

			w = startWorker("shannon-tasks", sa, sw)
		}
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	logger.Info("Shutting down orchestrator service")

	// Graceful shutdown
	grpcServer.GracefulStop()
	if w != nil {
		w.Stop()
	}

	// Stop degradation manager and other background services
	if orchestratorService != nil {
		if err := orchestratorService.Shutdown(); err != nil {
			logger.Error("Failed to shutdown orchestrator service", zap.Error(err))
		}
	}

	// Config manager runs async and will stop when context is cancelled

	return
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvOrDefaultInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}
