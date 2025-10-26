package activities

// TODO: Add unit tests for:
//   - ExecuteAgentWithForcedTools (forced tool execution path)
//   - Body field mirroring to prompt_params (generic field mirroring logic)
//   - Error handling when /agent/query HTTP endpoint fails
//   - Session context injection and merging

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/circuitbreaker"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/config"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/embeddings"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/interceptors"
	agentpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/agent"
	commonpb "github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pb/common"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/policy"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/streaming"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/vectordb"
	"go.temporal.io/sdk/activity"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	policyEngine   policy.Engine
	policyEngineMu sync.RWMutex // Protects policyEngine reads and writes
)

// --- Minimal tool metadata cache (cost_per_use) ---
type toolCostCacheEntry struct {
	cost      float64
	expiresAt time.Time
}

var toolCostCache sync.Map // key: tool name -> toolCostCacheEntry

func getToolCostPerUse(ctx context.Context, baseURL, toolName string) float64 {
	// TTL from env (seconds), default 300s
	ttlSec := getenvInt("MCP_TOOL_COST_TTL_SECONDS", 300)
	if ttlSec <= 0 {
		ttlSec = 300
	}
	if v, ok := toolCostCache.Load(toolName); ok {
		if ent, ok2 := v.(toolCostCacheEntry); ok2 {
			if time.Now().Before(ent.expiresAt) {
				return ent.cost
			}
			toolCostCache.Delete(toolName)
		}
	}
	// Best-effort HTTP fetch with short timeout
	url := fmt.Sprintf("%s/tools/%s/metadata", baseURL, toolName)
	client := &http.Client{Timeout: 2 * time.Second, Transport: interceptors.NewWorkflowHTTPRoundTripper(nil)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0
	}
	var m struct {
		CostPerUse float64 `json:"cost_per_use"`
	}
	if json.NewDecoder(resp.Body).Decode(&m) != nil {
		return 0
	}
	if m.CostPerUse <= 0 {
		return 0
	}
	ent := toolCostCacheEntry{cost: m.CostPerUse, expiresAt: time.Now().Add(time.Duration(ttlSec) * time.Second)}
	toolCostCache.Store(toolName, ent)
	return m.CostPerUse
}

// validateContext sanitizes user-provided context to prevent injection attacks
func validateContext(ctx map[string]interface{}, logger *zap.Logger) map[string]interface{} {
	if ctx == nil {
		return make(map[string]interface{})
	}

	validated := make(map[string]interface{}, len(ctx))

	for key, value := range ctx {
		if key == "" {
			continue
		}
		if len(key) > 100 {
			logger.Warn("Skipping context key exceeding length", zap.String("key", key[:100]))
			continue
		}

		// Validate and sanitize values while preserving arbitrary keys
		if sanitizedValue := sanitizeContextValue(value, key, logger); sanitizedValue != nil {
			validated[key] = sanitizedValue
		} else {
			logger.Debug("Dropping context key due to unsupported value", zap.String("key", key))
		}
	}

	return validated
}

// sanitizeContextValue validates individual context values
func sanitizeContextValue(value interface{}, key string, logger *zap.Logger) interface{} {
	switch v := value.(type) {
	case nil:
		return nil
	case bool:
		return v
	case string:
		// Limit string length to prevent DoS
		if len(v) > 10000 {
			logger.Warn("Truncating oversized string value", zap.String("key", key), zap.Int("original_length", len(v)))
			return v[:10000]
		}
		return v
	case int, int32, int64, float32, float64:
		return v
	case map[string]interface{}:
		// Recursively validate nested maps
		sanitized := make(map[string]interface{})
		for k, nested := range v {
			if len(k) > 100 {
				logger.Warn("Skipping key with excessive length", zap.String("parent_key", key), zap.Int("key_length", len(k)))
				continue
			}
			if sanitizedNested := sanitizeContextValue(nested, k, logger); sanitizedNested != nil {
				sanitized[k] = sanitizedNested
			}
		}
		return sanitized
	case []interface{}:
		// Validate arrays with size limits
		if len(v) > 100 {
			logger.Warn("Truncating oversized array", zap.String("key", key), zap.Int("original_length", len(v)))
			v = v[:100]
		}
		sanitized := make([]interface{}, 0, len(v))
		for i, item := range v {
			if sanitizedItem := sanitizeContextValue(item, fmt.Sprintf("%s[%d]", key, i), logger); sanitizedItem != nil {
				sanitized = append(sanitized, sanitizedItem)
			}
		}
		return sanitized
	default:
		logger.Warn("Filtering out unsupported context value type",
			zap.String("key", key),
			zap.String("type", fmt.Sprintf("%T", v)))
		return nil
	}
}

// getContextKeys returns keys for logging purposes
func getContextKeys(ctx map[string]interface{}) []string {
	if ctx == nil {
		return nil
	}
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	return keys
}

// sanitizeToolCall validates and sanitizes tool call maps before protobuf conversion
func sanitizeToolCall(call map[string]interface{}, logger *zap.Logger) map[string]interface{} {
	if call == nil {
		return nil
	}

	sanitized := make(map[string]interface{})

	// Validate required "tool" field
	if tool, exists := call["tool"]; exists {
		if toolStr, ok := tool.(string); ok && toolStr != "" && len(toolStr) <= 100 {
			sanitized["tool"] = toolStr
		} else {
			logger.Warn("Invalid tool name in tool_call", zap.Any("tool", tool))
			return nil
		}
	} else {
		logger.Warn("Missing required 'tool' field in tool_call")
		return nil
	}

	// Validate "parameters" field if present
	if params, exists := call["parameters"]; exists {
		if sanitizedParams := sanitizeToolParameters(params, logger); sanitizedParams != nil {
			sanitized["parameters"] = sanitizedParams
		} else {
			logger.Warn("Failed to sanitize tool parameters")
			// Still proceed with empty parameters rather than failing entirely
			sanitized["parameters"] = make(map[string]interface{})
		}
	} else {
		sanitized["parameters"] = make(map[string]interface{})
	}

	return sanitized
}

// sanitizeToolParameters validates tool parameters recursively
func sanitizeToolParameters(params interface{}, logger *zap.Logger) interface{} {
	switch p := params.(type) {
	case nil:
		return nil
	case bool, string, int, int32, int64, float32, float64:
		return p
	case map[string]interface{}:
		if len(p) > 20 {
			logger.Warn("Tool parameters map too large, truncating", zap.Int("size", len(p)))
			// Take first 20 items only
			truncated := make(map[string]interface{})
			count := 0
			for k, v := range p {
				if count >= 20 {
					break
				}
				if len(k) > 100 {
					continue
				}
				if sanitizedValue := sanitizeToolParameters(v, logger); sanitizedValue != nil {
					truncated[k] = sanitizedValue
				}
				count++
			}
			return truncated
		}

		sanitized := make(map[string]interface{})
		for k, v := range p {
			if len(k) > 100 {
				logger.Warn("Tool parameter key too long, skipping", zap.String("key", k[:50]+"..."))
				continue
			}
			if sanitizedValue := sanitizeToolParameters(v, logger); sanitizedValue != nil {
				sanitized[k] = sanitizedValue
			}
		}
		return sanitized
	case []interface{}:
		if len(p) > 50 {
			logger.Warn("Tool parameters array too large, truncating", zap.Int("size", len(p)))
			p = p[:50]
		}
		sanitized := make([]interface{}, 0, len(p))
		for _, item := range p {
			if sanitizedItem := sanitizeToolParameters(item, logger); sanitizedItem != nil {
				sanitized = append(sanitized, sanitizedItem)
			}
		}
		return sanitized
	default:
		logger.Warn("Unsupported tool parameter type", zap.String("type", fmt.Sprintf("%T", p)))
		return nil
	}
}

// InitializePolicyEngine initializes the global policy engine
func InitializePolicyEngine() error {
	config := policy.LoadConfig()
	logger := zap.L()

	engine, err := policy.NewOPAEngine(config, logger)
	if err != nil {
		return fmt.Errorf("failed to create policy engine: %w", err)
	}

	policyEngineMu.Lock()
	policyEngine = engine
	policyEngineMu.Unlock()

	logger.Info("Policy engine initialized",
		zap.Bool("enabled", engine.IsEnabled()),
		zap.String("mode", string(config.Mode)),
		zap.String("path", config.Path),
	)

	return nil
}

// InitializePolicyEngineFromConfig initializes the global policy engine from Shannon config
func InitializePolicyEngineFromConfig(shannonPolicyConfig interface{}) error {
	config := policy.LoadConfigFromShannon(shannonPolicyConfig)
	logger := zap.L()

	engine, err := policy.NewOPAEngine(config, logger)
	if err != nil {
		return fmt.Errorf("failed to create policy engine: %w", err)
	}

	policyEngineMu.Lock()
	policyEngine = engine
	policyEngineMu.Unlock()

	logger.Info("Policy engine initialized from Shannon config",
		zap.Bool("enabled", engine.IsEnabled()),
		zap.String("mode", string(config.Mode)),
		zap.String("path", config.Path),
		zap.Bool("fail_closed", config.FailClosed),
		zap.String("environment", config.Environment),
	)

	return nil
}

// InitializePolicyEngineFromShannonConfig initializes from typed Shannon config
func InitializePolicyEngineFromShannonConfig(shannonPolicyConfig *config.PolicyConfig) error {
	// Convert Shannon config to map format that LoadConfigFromShannon expects
	shannonPolicyMap := map[string]interface{}{
		"enabled":     shannonPolicyConfig.Enabled,
		"mode":        shannonPolicyConfig.Mode,
		"path":        shannonPolicyConfig.Path,
		"fail_closed": shannonPolicyConfig.FailClosed,
		"environment": shannonPolicyConfig.Environment,
	}

	// Use LoadConfigFromShannon which properly merges environment variables
	// This ensures emergency kill-switch and canary settings from env vars work
	policyConfig := policy.LoadConfigFromShannon(shannonPolicyMap)

	logger := zap.L()

	engine, err := policy.NewOPAEngine(policyConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to create policy engine: %w", err)
	}

	policyEngineMu.Lock()
	policyEngine = engine
	policyEngineMu.Unlock()

	logger.Info("Policy engine initialized from Shannon config",
		zap.Bool("enabled", engine.IsEnabled()),
		zap.String("mode", string(policyConfig.Mode)),
		zap.String("path", policyConfig.Path),
		zap.Bool("fail_closed", policyConfig.FailClosed),
		zap.String("environment", policyConfig.Environment),
	)

	return nil
}

// GetPolicyEngine returns the global policy engine instance
func GetPolicyEngine() policy.Engine {
	policyEngineMu.RLock()
	defer policyEngineMu.RUnlock()
	return policyEngine
}

// evaluateAgentPolicy builds policy input and evaluates the agent execution request
func evaluateAgentPolicy(ctx context.Context, input AgentExecutionInput, logger *zap.Logger) (*policy.Decision, error) {
	// Get environment from active policy engine configuration for consistency
	environment := "dev"
	policyEngineMu.RLock()
	engine := policyEngine
	policyEngineMu.RUnlock()

	if engine != nil && engine.IsEnabled() {
		if env := engine.Environment(); env != "" {
			environment = env
		} else if v := os.Getenv("ENVIRONMENT"); v != "" {
			environment = v
		}
	} else if v := os.Getenv("ENVIRONMENT"); v != "" {
		environment = v
	}

	policyInput := &policy.PolicyInput{
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		Query:       input.Query,
		Mode:        input.Mode,
		Context:     input.Context,
		Environment: environment, // Use policy config environment for consistency
		Timestamp:   time.Now(),
	}

	// Extract additional context if available

	// Extract user ID from context if available
	if userID, ok := input.Context["user_id"].(string); ok {
		policyInput.UserID = userID
	}

	// Extract complexity score if available
	if complexityScore, ok := input.Context["complexity_score"].(float64); ok {
		policyInput.ComplexityScore = complexityScore
	}

	// Extract token budget if available
	if tokenBudget, ok := input.Context["token_budget"].(int); ok {
		policyInput.TokenBudget = tokenBudget
	}

	// Optional: Vector context enrichment with strict timeouts (protect policy latency)
	if svc := embeddings.Get(); svc != nil {
		if vdb := vectordb.Get(); vdb != nil {
			// Budget total vector time aggressively
			vecCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
			defer cancel()
			if emb, err := svc.GenerateEmbedding(vecCtx, input.Query, ""); err == nil {
				if sims, err := vdb.FindSimilarQueries(vecCtx, emb, 5); err == nil {
					// Convert to policy.SimilarQuery
					sq := make([]policy.SimilarQuery, 0, len(sims))
					var max float64
					for _, s := range sims {
						if s.Confidence > max {
							max = s.Confidence
						}
						sq = append(sq, policy.SimilarQuery{
							Query:      s.Query,
							Outcome:    s.Outcome,
							Confidence: s.Confidence,
							Timestamp:  s.Timestamp,
						})
					}
					policyInput.SimilarQueries = sq
					policyInput.ContextScore = max
				}
			}
		}
	}

	startTime := time.Now()
	decision, err := engine.Evaluate(ctx, policyInput)
	duration := time.Since(startTime)

	// Record performance metrics
	policy.RecordEvaluationDuration("agent_execution", duration.Seconds())

	if err != nil {
		policy.RecordError("evaluation_error", "agent_execution")
		return nil, err
	}

	logger.Debug("Policy evaluation completed",
		zap.Bool("allow", decision.Allow),
		zap.String("reason", decision.Reason),
		zap.Duration("duration", duration),
		zap.String("agent_id", input.AgentID),
	)

	return decision, nil
}

// executeAgentCore contains the shared logic for executing an agent via gRPC
// This is used by both ExecuteAgent and ExecuteSimpleTask activities to avoid
// activities calling other activities directly
func executeAgentCore(ctx context.Context, input AgentExecutionInput, logger *zap.Logger) (AgentExecutionResult, error) {
	// Ensure we have a valid logger
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	// Apply persona settings if specified
	if input.PersonaID != "" {
		// TODO: Re-enable when personas package is complete
		// persona, err := personas.GetPersona(input.PersonaID)
		type Persona struct {
			SystemPrompt string
			Temperature  float64
			TokenBudget  string
			Tools        []string
		}
		var persona *Persona
		err := fmt.Errorf("personas package not yet implemented")
		if err != nil {
			logger.Warn("Failed to load persona, using defaults",
				zap.String("persona_id", input.PersonaID),
				zap.Error(err))
		} else {
			// Apply persona configuration
			if persona.SystemPrompt != "" {
				if input.Context == nil {
					input.Context = make(map[string]interface{})
				}
				input.Context["system_prompt"] = persona.SystemPrompt
			}

			// Override tools if persona specifies them, but intersect with available tools
			if len(persona.Tools) > 0 {
				// Fetch available tools to intersect with persona tools
				availableTools := fetchAvailableTools(ctx)
				intersectedTools := intersectTools(persona.Tools, availableTools)

				if len(intersectedTools) > 0 {
					input.SuggestedTools = intersectedTools
					logger.Debug("Intersected persona tools with available tools",
						zap.Strings("persona_tools", persona.Tools),
						zap.Strings("available_tools", availableTools),
						zap.Strings("intersected_tools", intersectedTools))
				} else {
					logger.Warn("No valid tools after intersection, using all available tools",
						zap.Strings("persona_tools", persona.Tools),
						zap.Strings("available_tools", availableTools))
					// Don't constrain if no tools match
					input.SuggestedTools = nil
				}
			}

			// Apply temperature setting
			if persona.Temperature > 0 {
				if input.Context == nil {
					input.Context = make(map[string]interface{})
				}
				input.Context["temperature"] = persona.Temperature
			}

			// Apply token budget
			// tokenBudget := personas.GetTokenBudgetValue(persona.TokenBudget)
			tokenBudget := 5000 // Default medium budget
			if input.Context == nil {
				input.Context = make(map[string]interface{})
			}
			input.Context["max_tokens"] = tokenBudget

			logger.Info("Applied persona settings",
				zap.String("persona_id", input.PersonaID),
				zap.String("agent_id", input.AgentID),
				zap.Int("tools_count", len(persona.Tools)),
				zap.Float64("temperature", persona.Temperature),
				zap.Int("token_budget", tokenBudget))
		}
	}

	logger.Info("Executing agent via gRPC",
		zap.String("agent_id", input.AgentID),
		zap.String("query", input.Query),
		zap.String("persona_id", input.PersonaID),
		zap.Strings("suggested_tools_received", input.SuggestedTools),
		zap.Any("tool_parameters_received", input.ToolParameters),
	)

	// Emit human-readable "agent thinking" event
	emitAgentThinkingEvent(ctx, input)

	// Policy check - Phase 0.5: Basic enforcement at agent execution boundary
	policyEngineMu.RLock()
	engine := policyEngine
	policyEngineMu.RUnlock()

	if engine != nil && engine.IsEnabled() {
		decision, err := evaluateAgentPolicy(ctx, input, logger)
		if err != nil {
			logger.Error("Policy evaluation failed", zap.Error(err))
			return AgentExecutionResult{
				AgentID: input.AgentID,
				Success: false,
				Error:   fmt.Sprintf("policy evaluation error: %v", err),
			}, fmt.Errorf("policy evaluation failed: %w", err)
		}

		if !decision.Allow {
			// Check if we're in dry-run mode - if so, don't block execution
			if engine != nil && engine.Mode() == policy.ModeDryRun {
				logger.Info("DRY-RUN: Policy would deny but allowing execution",
					zap.String("reason", decision.Reason),
					zap.String("agent_id", input.AgentID),
					zap.String("session_id", input.SessionID),
					zap.String("mode", "dry-run"),
				)

				// Record dry-run divergence metrics
				policy.RecordEvaluation("dry_run_would_deny", "agent_execution", decision.Reason)

				// Continue execution despite policy denial
			} else {
				// Enforce mode - actually block execution
				logger.Warn("Agent execution denied by policy",
					zap.String("reason", decision.Reason),
					zap.String("agent_id", input.AgentID),
					zap.String("session_id", input.SessionID),
					zap.String("mode", "enforce"),
				)

				// Record enforcement metrics
				policy.RecordEvaluation("deny", "agent_execution", decision.Reason)

				return AgentExecutionResult{
					AgentID: input.AgentID,
					Success: false,
					Error:   fmt.Sprintf("denied by policy: %s", decision.Reason),
				}, nil // Don't return error to avoid workflow failure, just deny execution
			}
		}

		// Record successful evaluation (allow or dry-run)
		if decision.Allow {
			policy.RecordEvaluation("allow", "agent_execution", decision.Reason)
			logger.Debug("Agent execution allowed by policy",
				zap.String("reason", decision.Reason),
				zap.String("agent_id", input.AgentID),
			)
		}

		// Handle approval requirement (future phase)
		if decision.RequireApproval {
			logger.Info("Policy requires approval for agent execution",
				zap.String("agent_id", input.AgentID),
				zap.String("reason", decision.Reason),
			)
			// TODO: Route to human intervention workflow
		}
	}

	start := time.Now()

	addr := os.Getenv("AGENT_CORE_ADDR")
	if addr == "" {
		addr = "agent-core:50051"
	}

	// Create gRPC connection wrapper with circuit breaker
	connWrapper := circuitbreaker.NewGRPCConnectionWrapper(addr, "agent-core", logger)

	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := connWrapper.DialContext(dialCtx,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(interceptors.WorkflowUnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(interceptors.WorkflowStreamClientInterceptor()),
	)
	if err != nil {
		return AgentExecutionResult{AgentID: input.AgentID, Success: false, Error: fmt.Sprintf("dial agent-core: %v", err)}, err
	}
	defer conn.Close()

	client := agentpb.NewAgentServiceClient(conn)

	// Create gRPC call wrapper with circuit breaker
	grpcWrapper := circuitbreaker.NewGRPCWrapper("agent-core-call", "agent-core", logger)

	// Map string mode to enum
	var emode commonpb.ExecutionMode
	switch input.Mode {
	case "simple":
		emode = commonpb.ExecutionMode_EXECUTION_MODE_SIMPLE
	case "complex":
		emode = commonpb.ExecutionMode_EXECUTION_MODE_COMPLEX
	default:
		emode = commonpb.ExecutionMode_EXECUTION_MODE_STANDARD
	}

	// Build session context for agent if available
	var sessionCtx *agentpb.SessionContext
	if input.SessionID != "" || len(input.History) > 0 {
		sessionCtx = &agentpb.SessionContext{
			SessionId: input.SessionID,
			History:   input.History,
			// Context from input already includes merged session context
		}
	}

	// Use LLM-suggested tools if provided, otherwise NO tools
	var allowedByRole []string
	if len(input.SuggestedTools) > 0 {
		// LLM has already suggested tools - use them directly
		allowedByRole = input.SuggestedTools
		logger.Info("Using LLM-suggested tools",
			zap.Strings("tools", allowedByRole),
			zap.String("agent_id", input.AgentID),
		)
	} else {
		// No tools suggested - keep empty to let LLM answer directly
		allowedByRole = []string{}
		logger.Info("No tools suggested by decomposition, using direct LLM response",
			zap.String("agent_id", input.AgentID),
		)
	}
	// Pass tool parameters to context if provided and valid
	if len(input.ToolParameters) > 0 {
		// Check if tool parameters have required fields
		toolName, hasToolName := input.ToolParameters["tool"].(string)
		validParams := false

		if hasToolName {
			switch toolName {
			case "python_executor":
				// Python executor requires code parameter
				if code, hasCode := input.ToolParameters["code"].(string); hasCode && code != "" {
					validParams = true
				}
			case "code_executor":
				// Code executor requires wasm_path or wasm_base64
				_, hasWasmPath := input.ToolParameters["wasm_path"].(string)
				_, hasWasmBase64 := input.ToolParameters["wasm_base64"].(string)
				validParams = hasWasmPath || hasWasmBase64
			case "calculator":
				// Calculator requires expression parameter
				if expr, hasExpr := input.ToolParameters["expression"].(string); hasExpr && expr != "" {
					validParams = true
				}
			default:
				// Other tools may not require specific parameters
				validParams = true
			}
		}

		// Only pass parameters if they're valid
		if validParams {
			if input.Context == nil {
				input.Context = make(map[string]interface{})
			}
			input.Context["tool_parameters"] = input.ToolParameters

			// Mirror critical body fields into prompt_params as a resilience fallback
			// This helps Python OpenAPI tools reconstruct the body if arrays get lost upstream.
			if bodyRaw, ok := input.ToolParameters["body"]; ok {
				if body, ok2 := bodyRaw.(map[string]interface{}); ok2 {
					// Type assert prompt_params with error handling
					pp, ok := input.Context["prompt_params"].(map[string]interface{})
					if !ok {
						// prompt_params is missing or wrong type, create new map
						logger.Warn("prompt_params missing or invalid type, creating new map",
							zap.String("workflow_id", input.ParentWorkflowID))
						pp = make(map[string]interface{})
						input.Context["prompt_params"] = pp
					}

					// Safe field allowlist to prevent leaking sensitive data
					// Only mirror fields that are safe for vendor adapters
					safeFields := map[string]bool{
						"account_id":   true,
						"tenant_id":    true,
						"user_id":      true,
						"session_id":   true,
						"profile_id":   true,
						"workspace_id": true,
						"project_id":   true,
						"aid":          true, // Application ID
						"current_date": true,
						"role":         true,
						"limit":        true,
						"offset":       true,
						"page":         true,
						"page_size":    true,
						"sort":         true,
						"order":        true,
						"filter":       true,
					}

					// Mirror only safe fields from body into prompt_params when missing
					// This enables vendor adapters to access request body fields safely
					for key, val := range body {
						// Skip fields containing sensitive keywords
						keyLower := strings.ToLower(key)
						if strings.Contains(keyLower, "token") ||
							strings.Contains(keyLower, "secret") ||
							strings.Contains(keyLower, "password") ||
							strings.Contains(keyLower, "key") ||
							strings.Contains(keyLower, "credential") {
							continue
						}

						// Only mirror if field is in safe list or already exists
						if safeFields[key] {
							if _, exists := pp[key]; !exists {
								pp[key] = val
							}
						}
					}
				}
			}
			logger.Info("Passing valid tool parameters to context",
				zap.String("tool", toolName),
				zap.String("agent_id", input.AgentID),
			)
		} else {
			logger.Info("Skipping invalid/incomplete tool parameters",
				zap.String("tool", toolName),
				zap.String("agent_id", input.AgentID),
				zap.Any("params", input.ToolParameters),
			)
		}
	}

	// Auto-populate tool_calls via /tools/select only if tools were suggested by decomposition
	// Respect the decomposition decision: if no tools suggested, don't override with tool selection
	var selectedToolCalls []map[string]interface{}
	// Skip tool selection if we already have tool_parameters from decomposition
	if len(input.SuggestedTools) > 0 && len(allowedByRole) > 0 && (input.ToolParameters == nil || len(input.ToolParameters) == 0) {
		if getenvInt("ENABLE_TOOL_SELECTION", 1) > 0 {
			// Only select tools if we have valid parameters or the tool doesn't require them
			// Skip tools that require parameters when none are provided to avoid execution errors
			toolsToSelect := allowedByRole
			if input.ToolParameters == nil || len(input.ToolParameters) == 0 {
				// Filter out tools that require parameters when none are provided
				filtered := make([]string, 0, len(allowedByRole))
				for _, tool := range allowedByRole {
					// Skip tools that require specific parameters when none are provided
					switch tool {
					case "calculator":
						// Calculator requires an expression parameter
						logger.Info("Skipping calculator tool - no parameters provided",
							zap.String("agent_id", input.AgentID),
						)
						continue
					case "code_executor":
						// Code executor requires wasm_path or wasm_base64
						logger.Info("Skipping code_executor tool - no parameters provided",
							zap.String("agent_id", input.AgentID),
						)
						continue
					case "python_executor":
						// Python executor requires code parameter
						logger.Info("Skipping python_executor tool - no parameters provided",
							zap.String("agent_id", input.AgentID),
						)
						continue
						// web_search and file_read can work with minimal/inferred parameters
						// so we don't skip them
					}
					filtered = append(filtered, tool)
				}
				toolsToSelect = filtered
			}
			if len(toolsToSelect) > 0 {
				selectedToolCalls = selectToolsForQuery(ctx, input.Query, toolsToSelect, logger, input.ParentWorkflowID)
				// Emit tool selection events
				if len(selectedToolCalls) > 0 {
					emitToolSelectionEvent(ctx, input, selectedToolCalls)
				}
			}
		}
	} else if len(input.SuggestedTools) == 0 {
		logger.Info("No tools suggested by decomposition, skipping tool selection",
			zap.String("agent_id", input.AgentID),
			zap.String("query", input.Query),
		)
	} else if input.ToolParameters != nil && len(input.ToolParameters) > 0 {
		logger.Info("Using tool_parameters from decomposition, skipping tool selection",
			zap.String("agent_id", input.AgentID),
			zap.Any("tool_parameters", input.ToolParameters),
		)
	}

	// Create protobuf struct from context AFTER adding tool_parameters and tool_calls
	// Ensure context is not nil
	if input.Context == nil {
		input.Context = make(map[string]interface{})
	}

	// Validate and sanitize context before protobuf conversion to prevent injection
	validatedContext := validateContext(input.Context, logger)
	st, err := structpb.NewStruct(validatedContext)
	if err != nil {
		logger.Error("Failed to create protobuf struct from validated context",
			zap.Error(err),
			zap.Any("original_context_keys", getContextKeys(input.Context)),
			zap.Any("validated_context_keys", getContextKeys(validatedContext)),
		)
		// Try to manually add tool_parameters if present
		st = &structpb.Struct{
			Fields: make(map[string]*structpb.Value),
		}
		if tp, ok := input.Context["tool_parameters"]; ok {
			// Convert tool_parameters manually
			if tpMap, ok := tp.(map[string]interface{}); ok {
				tpStruct, err := structpb.NewStruct(tpMap)
				if err == nil {
					st.Fields["tool_parameters"] = structpb.NewStructValue(tpStruct)
					logger.Info("Manually added tool_parameters to protobuf struct")
				} else {
					logger.Error("Failed to convert tool_parameters to protobuf",
						zap.Error(err),
						zap.Any("tool_parameters", tp),
					)
				}
			}
		}
	}

	// If we have selectedToolCalls, inject them as a protobuf ListValue under "tool_calls"
	if len(selectedToolCalls) > 0 {
		// Build []*structpb.Value where each element is a StructValue for one call
		values := make([]*structpb.Value, 0, len(selectedToolCalls))
		for _, call := range selectedToolCalls {
			if call == nil {
				continue
			}

			// Validate tool call structure before protobuf conversion
			sanitizedCall := sanitizeToolCall(call, logger)
			if sanitizedCall == nil {
				logger.Debug("Skipping invalid tool_call after sanitization")
				continue
			}

			// Safely convert to protobuf with additional error handling
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Panic in tool_call struct conversion",
							zap.Any("panic", r),
							zap.Any("call", sanitizedCall),
						)
					}
				}()

				if cs, err := structpb.NewStruct(sanitizedCall); err == nil {
					values = append(values, structpb.NewStructValue(cs))
				} else {
					logger.Debug("Skipping tool_call due to struct conversion error", zap.Error(err))
				}
			}()
		}
		if len(values) > 0 {
			lv := &structpb.ListValue{Values: values}
			if st.Fields == nil {
				st.Fields = make(map[string]*structpb.Value)
			}
			st.Fields["tool_calls"] = structpb.NewListValue(lv)
			logger.Info("Injected tool_calls into protobuf context",
				zap.Int("num_tool_calls", len(values)),
				zap.String("agent_id", input.AgentID),
			)
		}
	}

	// Agent runtime config derived from env (or can be made dynamic by policy in future)
	timeoutSec := getenvInt("AGENT_TIMEOUT_SECONDS", 30)
	memLimitMB := getenvInt("AGENT_MEMORY_LIMIT_MB", 256)

	req := &agentpb.ExecuteTaskRequest{
		Metadata: &commonpb.TaskMetadata{ // minimal metadata
			TaskId:    fmt.Sprintf("%s-%d", input.AgentID, time.Now().UnixNano()),
			UserId:    "orchestrator",
			SessionId: input.SessionID,
		},
		Query:          input.Query,
		Context:        st,
		Mode:           emode,
		SessionContext: sessionCtx,
		AvailableTools: allowedByRole,
		Config: &agentpb.AgentConfig{
			MaxIterations:  10,
			TimeoutSeconds: int32(timeoutSec),
			EnableSandbox:  true,
			MemoryLimitMb:  int64(memLimitMB),
			EnableLearning: false,
		},
	}

	// Emit LLM prompt (sanitized) using parent workflow ID when provided
	wfID := ""
	if input.ParentWorkflowID != "" {
		wfID = input.ParentWorkflowID
	} else if input.Context != nil {
		if v, ok := input.Context["parent_workflow_id"]; ok {
			if s, ok := v.(string); ok && s != "" {
				wfID = s
			}
		}
	}
	if wfID == "" {
		if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
			wfID = info.WorkflowExecution.ID
		}
	}
	if wfID != "" {
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMPrompt),
			AgentID:    input.AgentID,
			Message:    truncateQuery(input.Query, 2000),
			Timestamp:  time.Now(),
		})
	}

	// Create a timeout context for gRPC call - use agent timeout + buffer
	grpcTimeout := time.Duration(timeoutSec+30) * time.Second // Agent timeout + 30s buffer
	grpcCtx, grpcCancel := context.WithTimeout(ctx, grpcTimeout)
	defer grpcCancel()

	var resp *agentpb.ExecuteTaskResponse
	err = grpcWrapper.Execute(grpcCtx, func() error {
		var execErr error
		resp, execErr = client.ExecuteTask(grpcCtx, req)
		return execErr
	})
	if err != nil {
		return AgentExecutionResult{AgentID: input.AgentID, Success: false, Error: fmt.Sprintf("ExecuteTask error: %v", err)}, err
	}

	// Emit LLM partials + final output (best-effort)
	if wfID == "" {
		if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
			wfID = info.WorkflowExecution.ID
		}
	}
	if wfID != "" {
		out := ""
		if resp != nil && resp.Result != "" {
			out = resp.Result
		}
		if out != "" {
			chunk := getenvInt("PARTIAL_CHUNK_CHARS", 512)
			if chunk <= 0 {
				chunk = 512
			}
			if getenvInt("ENABLE_LLM_PARTIALS", 1) == 1 {
				for i := 0; i < len(out); i += chunk {
					j := i + chunk
					if j > len(out) {
						j = len(out)
					}
					streaming.Get().Publish(wfID, streaming.Event{
						WorkflowID: wfID,
						Type:       string(StreamEventLLMPartial),
						AgentID:    input.AgentID,
						Message:    out[i:j],
						Timestamp:  time.Now(),
					})
				}
			}
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    input.AgentID,
				Message:    truncateQuery(out, 4000),
				Timestamp:  time.Now(),
			})
		}
	}

	duration := time.Since(start).Milliseconds()

	tokens := 0
	model := ""
	promptTokens := 0
	completionTokens := 0
	var costUsd float64
	if resp.Metrics != nil && resp.Metrics.TokenUsage != nil {
		tokens = int(resp.Metrics.TokenUsage.TotalTokens)
		model = resp.Metrics.TokenUsage.Model
		costUsd = resp.Metrics.TokenUsage.CostUsd
		promptTokens = int(resp.Metrics.TokenUsage.PromptTokens)
		completionTokens = int(resp.Metrics.TokenUsage.CompletionTokens)

		logger.Info("Token usage from agent",
			zap.Int("prompt_tokens", int(resp.Metrics.TokenUsage.PromptTokens)),
			zap.Int("completion_tokens", int(resp.Metrics.TokenUsage.CompletionTokens)),
			zap.Int("total_tokens", tokens),
			zap.Float64("cost_usd", costUsd),
			zap.String("model", model),
		)
	}

	// Capture tool usage and outputs if present
	var toolsUsed []string
	var toolExecs []ToolExecution
	if resp != nil && len(resp.ToolResults) > 0 {
		toolsUsed = make([]string, 0, len(resp.ToolResults))
		toolExecs = make([]ToolExecution, 0, len(resp.ToolResults))
		for _, tr := range resp.ToolResults {
			if tr == nil {
				continue
			}
			tool := tr.ToolId
			toolsUsed = append(toolsUsed, tool)
			success := tr.Status == commonpb.StatusCode_STATUS_CODE_OK

			// Emit human-readable tool invocation event
			if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
				message := humanizeToolCall(tool, nil)
				eventData := EmitTaskUpdateInput{
					WorkflowID: info.WorkflowExecution.ID,
					EventType:  StreamEventToolInvoked,
					AgentID:    input.AgentID,
					Message:    message,
					Timestamp:  time.Now(),
				}
				activity.RecordHeartbeat(ctx, eventData)
				// Also publish to Redis Streams for SSE
				streaming.Get().Publish(info.WorkflowExecution.ID, streaming.Event{
					WorkflowID: eventData.WorkflowID,
					Type:       string(eventData.EventType),
					AgentID:    eventData.AgentID,
					Message:    eventData.Message,
					Timestamp:  eventData.Timestamp,
				})
			}

			var out interface{}
			if tr.Output != nil {
				// Safely handle potential panic from malformed protobuf
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Error("Panic in AsInterface() - malformed protobuf output",
								zap.Any("panic", r),
								zap.String("tool_id", tr.ToolId),
							)
							out = fmt.Sprintf("Error: malformed tool output (%v)", r)
						}
					}()
					out = tr.Output.AsInterface()
				}()
			}
			// Emit tool observation (truncated)
			if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
				obs := ""
				switch v := out.(type) {
				case string:
					obs = v
				default:
					if b, err := json.Marshal(v); err == nil {
						obs = string(b)
					}
				}
				streaming.Get().Publish(info.WorkflowExecution.ID, streaming.Event{
					WorkflowID: info.WorkflowExecution.ID,
					Type:       string(StreamEventToolObs),
					AgentID:    input.AgentID,
					Message:    truncateQuery(fmt.Sprintf("%s: %s", tool, obs), 2000),
					Timestamp:  time.Now(),
				})
			}

			toolExecs = append(toolExecs, ToolExecution{
				Tool:    tool,
				Success: success,
				Output:  out,
				Error:   tr.ErrorMessage,
			})
		}
	}

	// Optional: map tool cost_per_use (USD) to token-equivalent (cost*1000) for budget accounting.
	// Guarded by MCP_COST_TO_TOKENS env (default: 0/off). Uses a small TTL cache to avoid hot-path HTTP.
	if getenvInt("MCP_COST_TO_TOKENS", 0) > 0 {
		extraTokens := 0
		if resp != nil && len(resp.ToolResults) > 0 {
			base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
			for _, tr := range resp.ToolResults {
				name := tr.ToolId
				if name == "" {
					continue
				}
				if cost := getToolCostPerUse(ctx, base, name); cost > 0 {
					extraTokens += int(cost * 1000.0)
				}
			}
			if extraTokens > 0 {
				tokens += extraTokens
				logger.Debug("Applied MCP tool cost tokens",
					zap.Int("extra_tokens", extraTokens),
					zap.String("agent_id", input.AgentID),
				)
			}
		}
	}

	success := resp.Status == commonpb.StatusCode_STATUS_CODE_OK

	return AgentExecutionResult{
		AgentID:        input.AgentID,
		Response:       resp.Result,
		TokensUsed:     tokens,
		ModelUsed:      model,
		InputTokens:    promptTokens,
		OutputTokens:   completionTokens,
		DurationMs:     duration,
		Success:        success,
		Error:          resp.ErrorMessage,
		ToolsUsed:      toolsUsed,
		ToolExecutions: toolExecs,
	}, nil
}

// ExecuteAgent is the activity that executes an agent by calling Agent-Core over gRPC
// This is a Temporal activity that wraps the core logic
// intersectTools returns the intersection of two tool lists
func intersectTools(personaTools, availableTools []string) []string {
	// Create a map for fast lookup
	availableMap := make(map[string]bool)
	for _, tool := range availableTools {
		availableMap[tool] = true
	}

	// Find intersection
	var result []string
	for _, tool := range personaTools {
		if availableMap[tool] {
			result = append(result, tool)
		}
	}
	return result
}

func ExecuteAgent(ctx context.Context, input AgentExecutionInput) (AgentExecutionResult, error) {
	// Use activity logger for proper Temporal correlation
	activity.GetLogger(ctx).Info("ExecuteAgent activity started",
		"agent_id", input.AgentID,
		"query", input.Query,
	)

	// Use forced tools path if ToolParameters are pre-computed (analytics queries)
	if input.ToolParameters != nil && len(input.ToolParameters) > 0 && len(input.SuggestedTools) > 0 {
		return ExecuteAgentWithForcedTools(ctx, input)
	}

	// Standard execution through agent-core gRPC
	logger := zap.L()
	return executeAgentCore(ctx, input, logger)
}

// ExecuteAgentWithForcedTools bypasses agent-core gRPC and calls /agent/query directly
// with forced_tool_calls to avoid serialization issues. Use when ToolParameters are
// pre-computed from decomposition (e.g., analytics queries).
func ExecuteAgentWithForcedTools(ctx context.Context, input AgentExecutionInput) (AgentExecutionResult, error) {
	logger := activity.GetLogger(ctx)
	logger.Info("ExecuteAgentWithForcedTools activity started",
		"agent_id", input.AgentID,
		"query", input.Query,
		"tool_count", len(input.SuggestedTools),
	)

	// Bail if no tool parameters to use
	if input.ToolParameters == nil || len(input.ToolParameters) == 0 {
		logger.Warn("No ToolParameters provided; falling back to regular ExecuteAgent")
		zapLogger := zap.L()
		return executeAgentCore(ctx, input, zapLogger)
	}

	// Determine which tool to execute (typically just one from decomposition)
	toolName := ""
	if len(input.SuggestedTools) > 0 {
		toolName = input.SuggestedTools[0]
	} else {
		logger.Error("No SuggestedTools provided with ToolParameters; cannot proceed")
		return AgentExecutionResult{Success: false, Error: "No tool specified for forced execution"}, nil
	}

	// Build forced_tool_calls payload for /agent/query
	forcedToolCalls := []map[string]interface{}{
		{
			"tool":       toolName,
			"parameters": input.ToolParameters,
		},
	}

	// Prepare request to /agent/query
	llmServiceURL := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
	url := fmt.Sprintf("%s/agent/query", llmServiceURL)

	agentQueryPayload := map[string]interface{}{
		"query":             input.Query,
		"context":           input.Context,
		"agent_id":          input.AgentID,
		"allowed_tools":     input.SuggestedTools,
		"forced_tool_calls": forcedToolCalls,
	}

	payloadBytes, err := json.Marshal(agentQueryPayload)
	if err != nil {
		logger.Error("Failed to marshal agent query payload", "error", err)
		return AgentExecutionResult{Success: false, Error: "Failed to construct request"}, nil
	}

	client := &http.Client{Timeout: 2 * time.Minute, Transport: interceptors.NewWorkflowHTTPRoundTripper(nil)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		logger.Error("Failed to create HTTP request", "error", err)
		return AgentExecutionResult{Success: false, Error: "Failed to create request"}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", input.AgentID)

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("HTTP request failed", "error", err)
		return AgentExecutionResult{Success: false, Error: fmt.Sprintf("Request failed: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error("Non-2xx response from /agent/query", "status", resp.StatusCode)
		return AgentExecutionResult{Success: false, Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}, nil
	}

	// Parse response
	var agentResponse struct {
		Success    bool        `json:"success"`
		Response   string      `json:"response"`
		TokensUsed int         `json:"tokens_used"`
		ModelUsed  string      `json:"model_used"`
		Metadata   interface{} `json:"metadata"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&agentResponse); err != nil {
		logger.Error("Failed to decode /agent/query response", "error", err)
		return AgentExecutionResult{Success: false, Error: "Failed to parse response"}, nil
	}

	logger.Info("ExecuteAgentWithForcedTools completed",
		"success", agentResponse.Success,
		"tokens", agentResponse.TokensUsed,
		"model", agentResponse.ModelUsed,
	)

	return AgentExecutionResult{
		Success:    agentResponse.Success,
		Response:   agentResponse.Response,
		TokensUsed: agentResponse.TokensUsed,
		ModelUsed:  agentResponse.ModelUsed,
		ToolsUsed:  []string{toolName},
	}, nil
}

// fetchAvailableTools queries Python LLM service for a list of non-dangerous tools.
func fetchAvailableTools(ctx context.Context) []string {
	base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
	url := fmt.Sprintf("%s/tools/list?exclude_dangerous=true", base)
	client := &http.Client{Timeout: 5 * time.Second, Transport: interceptors.NewWorkflowHTTPRoundTripper(nil)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	var tools []string
	if err := json.NewDecoder(resp.Body).Decode(&tools); err != nil {
		return nil
	}
	return tools
}

// selectToolsForQuery queries Python LLM service to select appropriate tools for the given query
// and returns structured tool calls that can be executed in parallel by agent-core.
func selectToolsForQuery(ctx context.Context, query string, availableTools []string, logger *zap.Logger, parentWorkflowID string) []map[string]interface{} {
	base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
	url := fmt.Sprintf("%s/tools/select", base)

	// Prepare request payload compatible with llm-service ToolSelectRequest
	// We pass the task (query), and limit max_tools to a small number to keep execution bounded.
	payload := map[string]interface{}{
		"task":              query,
		"context":           map[string]interface{}{},
		"exclude_dangerous": true,
		"max_tools":         3,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Debug("Failed to marshal tool selection request", zap.Error(err))
		return nil
	}

	client := &http.Client{Timeout: 5 * time.Second, Transport: interceptors.NewWorkflowHTTPRoundTripper(nil)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		logger.Debug("Failed to create tool selection request", zap.Error(err))
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	// Prefer parent workflow ID when available for unified event streaming in llm-service
	if parentWorkflowID != "" {
		req.Header.Set("X-Parent-Workflow-ID", parentWorkflowID)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Debug("Tool selection request failed", zap.Error(err))
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Debug("Tool selection returned non-2xx status", zap.Int("status", resp.StatusCode))
		return nil
	}

	// Parse response: { selected_tools: [...], calls: [{tool_name, parameters}...] }
	var sel struct {
		SelectedTools []string                 `json:"selected_tools"`
		Calls         []map[string]interface{} `json:"calls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sel); err != nil {
		logger.Debug("Failed to decode tool selection response", zap.Error(err))
		return nil
	}

	// Transform calls into agent-core format: [{"tool": name, "parameters": {...}}]
	out := make([]map[string]interface{}, 0, len(sel.Calls))
	allow := map[string]struct{}{}
	for _, t := range availableTools {
		allow[t] = struct{}{}
	}
	for _, c := range sel.Calls {
		name, _ := c["tool_name"].(string)
		if name == "" {
			continue
		}
		// Enforce role/allowlist from orchestrator
		if len(allow) > 0 {
			if _, ok := allow[name]; !ok {
				continue
			}
		}
		params, _ := c["parameters"].(map[string]interface{})
		out = append(out, map[string]interface{}{
			"tool":       name,
			"parameters": params,
		})
	}

	logger.Info("Tool selection completed",
		zap.Int("num_tools", len(out)),
		zap.String("query", query),
	)
	return out
}

// filterToolsByRole returns the intersection of service-available tools and the
// static role allowlist. This is a minimal, deterministic enforcement until a
// shared role schema is introduced.
func filterToolsByRole(role string, serviceTools []string) []string {
	list, ok := getRoleAllowlist()[strings.ToLower(role)]
	if !ok {
		list = roleAllowlist["generalist"]
	}
	if len(list) == 0 {
		return []string{}
	}
	// Build set of service tools
	svc := make(map[string]struct{}, len(serviceTools))
	for _, t := range serviceTools {
		svc[t] = struct{}{}
	}
	// Intersect
	out := make([]string, 0, len(list))
	for _, t := range list {
		if _, ok := svc[t]; ok {
			out = append(out, t)
		}
	}
	return out
}

// --- Role allowlist cache (fetched from LLM service /roles; fallback to static) ---
var (
	roleAllowlist = map[string][]string{
		"analysis":   {"web_search", "code_reader"},
		"research":   {"web_search"},
		"writer":     {"code_reader"},
		"critic":     {"code_reader"},
		"generalist": {},
	}
	roleAllowlistMu   sync.RWMutex
	roleAllowlistOnce sync.Once
)

func getRoleAllowlist() map[string][]string {
	// Use sync.Once to ensure initialization happens exactly once
	roleAllowlistOnce.Do(func() {
		// Try fetching from LLM service
		base := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
		url := fmt.Sprintf("%s/roles", base)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			client := &http.Client{Timeout: 2 * time.Second, Transport: interceptors.NewWorkflowHTTPRoundTripper(nil)}
			if resp, err := client.Do(req); err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
				defer resp.Body.Close()
				var payload map[string]struct {
					AllowedTools []string `json:"allowed_tools"`
				}
				if json.NewDecoder(resp.Body).Decode(&payload) == nil {
					tmp := map[string][]string{}
					for k, v := range payload {
						tmp[strings.ToLower(k)] = v.AllowedTools
					}
					if len(tmp) > 0 {
						roleAllowlistMu.Lock()
						roleAllowlist = tmp
						roleAllowlistMu.Unlock()
					}
				}
			}
		}
	})

	// Return a copy to prevent external modifications
	roleAllowlistMu.RLock()
	defer roleAllowlistMu.RUnlock()
	result := make(map[string][]string, len(roleAllowlist))
	for k, v := range roleAllowlist {
		result[k] = v
	}
	return result
}

// getenv returns env var or default
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getenvInt returns integer env var or default
func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// emitAgentThinkingEvent emits a human-readable thinking event
func emitAgentThinkingEvent(ctx context.Context, input AgentExecutionInput) {
	if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
		message := fmt.Sprintf("Analyzing: %s", truncateQuery(input.Query, 80))
		eventData := EmitTaskUpdateInput{
			WorkflowID: info.WorkflowExecution.ID,
			EventType:  StreamEventAgentThinking,
			AgentID:    input.AgentID,
			Message:    message,
			Timestamp:  time.Now(),
		}
		activity.RecordHeartbeat(ctx, eventData)
		// Also publish to Redis Streams for SSE
		streaming.Get().Publish(info.WorkflowExecution.ID, streaming.Event{
			WorkflowID: eventData.WorkflowID,
			Type:       string(eventData.EventType),
			AgentID:    eventData.AgentID,
			Message:    eventData.Message,
			Timestamp:  eventData.Timestamp,
		})
	}
}

// emitToolSelectionEvent emits events for selected tools
func emitToolSelectionEvent(ctx context.Context, input AgentExecutionInput, toolCalls []map[string]interface{}) {
	if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
		for _, call := range toolCalls {
			toolName, _ := call["tool"].(string)
			if toolName == "" {
				continue
			}
			message := humanizeToolCall(toolName, call["parameters"])
			eventData := EmitTaskUpdateInput{
				WorkflowID: info.WorkflowExecution.ID,
				EventType:  StreamEventToolInvoked,
				AgentID:    input.AgentID,
				Message:    message,
				Timestamp:  time.Now(),
			}
			activity.RecordHeartbeat(ctx, eventData)
			// Also publish to Redis Streams for SSE
			streaming.Get().Publish(info.WorkflowExecution.ID, streaming.Event{
				WorkflowID: eventData.WorkflowID,
				Type:       string(eventData.EventType),
				AgentID:    eventData.AgentID,
				Message:    eventData.Message,
				Timestamp:  eventData.Timestamp,
			})
		}
	}
}

// humanizeToolCall creates a human-readable description of a tool invocation
func humanizeToolCall(toolName string, params interface{}) string {
	paramsMap, _ := params.(map[string]interface{})

	switch toolName {
	case "web_search":
		if query, ok := paramsMap["query"].(string); ok {
			return fmt.Sprintf("Searching web for '%s'", truncateQuery(query, 50))
		}
		return "Searching the web"
	case "calculator":
		if expr, ok := paramsMap["expression"].(string); ok {
			return fmt.Sprintf("Calculating: %s", expr)
		}
		return "Performing calculation"
	case "python_code", "code_executor", "python_executor":
		return "Executing Python code"
	case "read_file", "file_reader":
		if path, ok := paramsMap["path"].(string); ok {
			return fmt.Sprintf("Reading file: %s", path)
		}
		return "Reading file"
	case "web_fetch":
		if url, ok := paramsMap["url"].(string); ok {
			return fmt.Sprintf("Fetching content from: %s", truncateURL(url))
		}
		return "Fetching web content"
	case "code_reader":
		return "Analyzing code structure"
	default:
		return fmt.Sprintf("Using %s tool", toolName)
	}
}

// truncateQuery truncates a query to a specified length
func truncateQuery(query string, maxLen int) string {
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen-3] + "..."
}

// truncateURL shortens a URL for display
func truncateURL(url string) string {
	if len(url) <= 50 {
		return url
	}
	// Try to preserve domain
	if idx := strings.Index(url, "?"); idx > 0 && idx < 50 {
		return url[:idx] + "?..."
	}
	return url[:47] + "..."
}
