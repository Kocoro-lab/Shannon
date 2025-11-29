package activities

// TODO: Add unit tests for:
//   - ExecuteAgentWithForcedTools (forced tool execution path)
//   - Body field mirroring to prompt_params (generic field mirroring logic)
//   - Error handling when /agent/query HTTP endpoint fails
//   - Session context injection and merging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"reflect"
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
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/pricing"
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

// parseFlexibleFloat attempts to parse a value as float64 from various JSON types
func parseFlexibleFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// parseFlexibleInt attempts to parse a value as int from various JSON types
func parseFlexibleInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	case int64:
		return int(val), true
	case string:
		if i, err := strconv.Atoi(val); err == nil {
			return i, true
		}
	}
	return 0, false
}

// validateContext sanitizes user-provided context to prevent injection attacks
func validateContext(ctx map[string]interface{}, logger *zap.Logger) map[string]interface{} {
	if ctx == nil {
		return make(map[string]interface{})
	}

	// Blacklist of internal metadata fields that should never reach the LLM
	internalFields := map[string]bool{
		"budget_agent_max":       true,
		"budget_agent_remaining": true,
		"budget_agent_min":       true,
		"last_tokens_used":       true,
		"total_cost_usd":         true,
		"model_used":             true,
		"provider":               true,
		"total_tokens":           true,
		"input_tokens":           true,
		"output_tokens":          true,
		"prompt_tokens":          true,
		"completion_tokens":      true,
	}

	validated := make(map[string]interface{}, len(ctx))

	for key, value := range ctx {
		if key == "" {
			continue
		}

		// Filter out internal metadata fields
		if internalFields[key] {
			logger.Debug("Filtering internal metadata field from context", zap.String("key", key))
			continue
		}

		if len(key) > 100 {
			keyRunes := []rune(key)
			truncatedKey := key
			if len(keyRunes) > 100 {
				truncatedKey = string(keyRunes[:100])
			}
			logger.Warn("Skipping context key exceeding length", zap.String("key", truncatedKey))
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
		// Limit string length to prevent DoS (UTF-8 safe)
		runes := []rune(v)
		if len(runes) > 10000 {
			logger.Warn("Truncating oversized string value", zap.String("key", key), zap.Int("original_length", len(runes)))
			return string(runes[:10000])
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
		return sanitizeSlice(v, key, logger)
	default:
		reflected := reflect.ValueOf(v)
		// Security: Filter out unsafe types that could cause panics or security issues
		switch reflected.Kind() {
		case reflect.Chan, reflect.Func, reflect.UnsafePointer:
			logger.Warn("Filtering unsafe type from context (security)",
				zap.String("key", key),
				zap.String("type", fmt.Sprintf("%T", v)),
				zap.String("kind", reflected.Kind().String()))
			return nil
		case reflect.Slice, reflect.Array:
			return sanitizeReflectedSlice(reflected, key, logger)
		case reflect.Map:
			return sanitizeReflectedMap(reflected, key, logger)
		default:
			logger.Warn("Filtering out unsupported context value type",
				zap.String("key", key),
				zap.String("type", fmt.Sprintf("%T", v)))
			return nil
		}
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
			if k == "" {
				continue
			}
			if len(k) > 100 {
				runes := []rune(k)
				truncated := k
				if len(runes) > 50 {
					truncated = string(runes[:50]) + "..."
				}
				logger.Warn("Tool parameter key too long, skipping", zap.String("key", truncated))
				continue
			}
			if sanitizedValue := sanitizeToolParameters(v, logger); sanitizedValue != nil {
				sanitized[k] = sanitizedValue
			}
		}
		return sanitized
	case []interface{}:
		return sanitizeToolSlice(p, logger)
	default:
		reflected := reflect.ValueOf(p)
		switch reflected.Kind() {
		case reflect.Slice, reflect.Array:
			return sanitizeToolReflectedSlice(reflected, logger)
		case reflect.Map:
			return sanitizeToolReflectedMap(reflected, logger)
		default:
			logger.Warn("Unsupported tool parameter type", zap.String("type", fmt.Sprintf("%T", p)))
			return nil
		}
	}
}

func sanitizeSlice(values []interface{}, key string, logger *zap.Logger) []interface{} {
	if len(values) > 100 {
		logger.Warn("Truncating oversized array", zap.String("key", key), zap.Int("original_length", len(values)))
		values = values[:100]
	}
	sanitized := make([]interface{}, 0, len(values))
	for idx, item := range values {
		if sanitizedItem := sanitizeContextValue(item, fmt.Sprintf("%s[%d]", key, idx), logger); sanitizedItem != nil {
			sanitized = append(sanitized, sanitizedItem)
		}
	}
	return sanitized
}

func sanitizeReflectedSlice(reflected reflect.Value, key string, logger *zap.Logger) []interface{} {
	length := reflected.Len()
	if length > 100 {
		logger.Warn("Truncating oversized array", zap.String("key", key), zap.Int("original_length", length))
		length = 100
	}
	sanitized := make([]interface{}, 0, length)
	for idx := 0; idx < length; idx++ {
		item := reflected.Index(idx).Interface()
		if sanitizedItem := sanitizeContextValue(item, fmt.Sprintf("%s[%d]", key, idx), logger); sanitizedItem != nil {
			sanitized = append(sanitized, sanitizedItem)
		}
	}
	return sanitized
}

func sanitizeReflectedMap(reflected reflect.Value, key string, logger *zap.Logger) map[string]interface{} {
	if reflected.Type().Key().Kind() != reflect.String {
		logger.Warn("Skipping map with non-string keys", zap.String("key", key), zap.String("type", reflected.Type().String()))
		return nil
	}
	sanitized := make(map[string]interface{}, reflected.Len())
	iter := reflected.MapRange()
	for iter.Next() {
		mapKey := iter.Key().String()
		if mapKey == "" {
			continue
		}
		if len(mapKey) > 100 {
			runes := []rune(mapKey)
			truncated := mapKey
			if len(runes) > 50 {
				truncated = string(runes[:50]) + "..."
			}
			logger.Warn("Map key too long, skipping", zap.String("parent_key", key), zap.String("key", truncated))
			continue
		}
		if sanitizedValue := sanitizeContextValue(iter.Value().Interface(), mapKey, logger); sanitizedValue != nil {
			sanitized[mapKey] = sanitizedValue
		}
	}
	return sanitized
}

func sanitizeToolSlice(values []interface{}, logger *zap.Logger) []interface{} {
	if len(values) > 50 {
		logger.Warn("Tool parameters array too large, truncating", zap.Int("size", len(values)))
		values = values[:50]
	}
	sanitized := make([]interface{}, 0, len(values))
	for _, item := range values {
		if sanitizedItem := sanitizeToolParameters(item, logger); sanitizedItem != nil {
			sanitized = append(sanitized, sanitizedItem)
		}
	}
	return sanitized
}

func sanitizeToolReflectedSlice(reflected reflect.Value, logger *zap.Logger) []interface{} {
	length := reflected.Len()
	if length > 50 {
		logger.Warn("Tool parameters array too large, truncating", zap.Int("size", length))
		length = 50
	}
	sanitized := make([]interface{}, 0, length)
	for idx := 0; idx < length; idx++ {
		if sanitizedItem := sanitizeToolParameters(reflected.Index(idx).Interface(), logger); sanitizedItem != nil {
			sanitized = append(sanitized, sanitizedItem)
		}
	}
	return sanitized
}

func sanitizeToolReflectedMap(reflected reflect.Value, logger *zap.Logger) map[string]interface{} {
	if reflected.Type().Key().Kind() != reflect.String {
		logger.Warn("Unsupported tool parameter map type", zap.String("type", reflected.Type().String()))
		return nil
	}
	sanitized := make(map[string]interface{}, reflected.Len())
	iter := reflected.MapRange()
	for iter.Next() {
		k := iter.Key().String()
		if k == "" {
			continue
		}
		if len(k) > 100 {
			runes := []rune(k)
			truncated := k
			if len(runes) > 50 {
				truncated = string(runes[:50]) + "..."
			}
			logger.Warn("Tool parameter key too long, skipping", zap.String("key", truncated))
			continue
		}
		if sanitizedValue := sanitizeToolParameters(iter.Value().Interface(), logger); sanitizedValue != nil {
			sanitized[k] = sanitizedValue
		}
	}
	return sanitized
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

	// Universal guard: If web_fetch is suggested but web_search is not, add web_search
	// web_fetch requires URLs that typically come from web_search results
	hasWebFetch := false
	hasWebSearch := false
	for _, tool := range allowedByRole {
		if tool == "web_fetch" {
			hasWebFetch = true
		}
		if tool == "web_search" {
			hasWebSearch = true
		}
	}
	if hasWebFetch && !hasWebSearch {
		allowedByRole = append(allowedByRole, "web_search")
		logger.Info("Added web_search alongside web_fetch to enable URL discovery",
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

	// Detect research-style contexts where we prefer LLM-native tool selection
	isResearch := false
	if input.Context != nil {
		if fr, ok := input.Context["force_research"].(bool); ok && fr {
			isResearch = true
		} else if rs, ok := input.Context["research_strategy"].(string); ok && strings.TrimSpace(rs) != "" {
			isResearch = true
		} else if rm, ok := input.Context["research_mode"].(string); ok && strings.TrimSpace(rm) != "" {
			isResearch = true
		}
	}

	// Detect ptengine-style roles that use forced tool execution (ExecuteAgentWithForcedTools).
	// These roles have pre-computed tool parameters and don't need /tools/select.
	// GA4-style roles (ga4_analytics, etc.) still need /tools/select to work correctly.
	disableToolSelectRole := false
	if input.Context != nil {
		if roleVal, ok := input.Context["role"]; ok {
			if roleStr, ok := roleVal.(string); ok {
				role := strings.ToLower(strings.TrimSpace(roleStr))
				switch role {
				case "data_analytics": // ptengine-style analytics with forced tool execution
					disableToolSelectRole = true
					logger.Info("Ptengine-style role detected - skipping /tools/select (uses forced tools)",
						zap.String("role", role),
						zap.String("agent_id", input.AgentID),
					)
				}
			}
		}
	}

	// Skip tool selection if we already have tool_parameters from decomposition
	if !disableToolSelectRole && !isResearch && len(input.SuggestedTools) > 0 && len(allowedByRole) > 0 && (input.ToolParameters == nil || len(input.ToolParameters) == 0) {
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
						// web_search, web_fetch and file_read can work with minimal/inferred parameters
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
			Message:    truncateQuery(input.Query, MaxPromptChars),
			Timestamp:  time.Now(),
		})
	}

	// Create a timeout context for gRPC call - use agent timeout + buffer
	grpcTimeout := time.Duration(timeoutSec+30) * time.Second // Agent timeout + 30s buffer
	llmServiceURL := getenv("LLM_SERVICE_URL", "http://llm-service:8000")
	mcpCostToTokens := getenvInt("MCP_COST_TO_TOKENS", 0)

	// Streaming is opt-in and only used when no tools are required.
	useStreaming := getenvInt("ENABLE_AGENT_STREAMING", 1) > 0
	if len(allowedByRole) > 0 || len(selectedToolCalls) > 0 || (input.ToolParameters != nil && len(input.ToolParameters) > 0) {
		useStreaming = false
	}

	runStreaming := func() (AgentExecutionResult, error) {
		callStart := time.Now()
		grpcCtx, grpcCancel := context.WithTimeout(ctx, grpcTimeout)
		defer grpcCancel()

		var stream agentpb.AgentService_StreamExecuteTaskClient
		err := grpcWrapper.Execute(grpcCtx, func() error {
			var execErr error
			stream, execErr = client.StreamExecuteTask(grpcCtx, req)
			return execErr
		})
		if err != nil {
			return AgentExecutionResult{}, err
		}

		var outBuilder strings.Builder
		finalMessage := ""
		partialBuf := strings.Builder{}
		partialChunk := getenvInt("PARTIAL_PUBLISH_CHARS", 1)
		if partialChunk <= 0 {
			partialChunk = 1
		}

		flushPartial := func() {
			if wfID == "" || partialBuf.Len() == 0 {
				return
			}
			msg := partialBuf.String()
			partialBuf.Reset()
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMPartial),
				AgentID:    input.AgentID,
				Message:    msg,
				Timestamp:  time.Now(),
			})
		}

		tokens := 0
		model := ""
		provider := ""
		promptTokens := 0
		completionTokens := 0
		costUsd := 0.0
		toolsUsed := []string{}
		toolExecs := []ToolExecution{}
		success := true
		toolCostUsd := 0.0
		toolTokenBump := 0
		seenTools := map[string]struct{}{}

		for {
			upd, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				break
			}
			if recvErr != nil {
				// Flush any buffered content before returning error
				flushPartial()
				return AgentExecutionResult{}, recvErr
			}

			if delta := upd.GetDelta(); delta != "" {
				outBuilder.WriteString(delta)
				partialBuf.WriteString(delta)
				if partialBuf.Len() >= partialChunk {
					flushPartial()
				}
			}

			if tr := upd.GetToolResult(); tr != nil {
				toolName := tr.GetToolId()
				if toolName == "usage_metrics" && tr.Output != nil {
					if m, ok := tr.Output.AsInterface().(map[string]interface{}); ok {
						// Use flexible parsing to handle int/float/string from JSON
						if v, ok := parseFlexibleInt(m["total_tokens"]); ok {
							tokens = v
						}
						if v, ok := parseFlexibleInt(m["input_tokens"]); ok {
							promptTokens = v
						}
						if v, ok := parseFlexibleInt(m["output_tokens"]); ok {
							completionTokens = v
						}
						if v, ok := parseFlexibleFloat(m["cost_usd"]); ok {
							costUsd = v
						}
						if v, ok2 := m["model"].(string); ok2 && v != "" {
							model = v
						}
						if v, ok2 := m["provider"].(string); ok2 && v != "" {
							provider = v
						}
					}
				} else {
					output := interface{}(nil)
					if tr.Output != nil {
						output = tr.Output.AsInterface()
					}

					// Emit TOOL_OBSERVATION event with output/error for UI visibility
					if wfID != "" && toolName != "" {
						msg := ""
						if tr.Status == commonpb.StatusCode_STATUS_CODE_OK {
							// Format output for message
							if output != nil {
								if str, ok := output.(string); ok {
									msg = str
								} else if bytes, err := json.Marshal(output); err == nil {
									msg = string(bytes)
								}
							}
						} else {
							msg = tr.ErrorMessage
						}

						// Truncate message if too long (UTF-8 safe, match Python's 2000 char limit)
						msg = truncateQuery(msg, 2000)

						streaming.Get().Publish(wfID, streaming.Event{
							WorkflowID: wfID,
							Type:       string(StreamEventToolObs),
							AgentID:    input.AgentID,
							Message:    msg,
							Payload: map[string]interface{}{
								"tool":    toolName,
								"success": tr.Status == commonpb.StatusCode_STATUS_CODE_OK,
							},
							Timestamp: time.Now(),
						})
					}

					toolExecs = append(toolExecs, ToolExecution{
						Tool:    toolName,
						Success: tr.Status == commonpb.StatusCode_STATUS_CODE_OK,
						Output:  output,
						Error:   tr.ErrorMessage,
					})

					if toolName != "" {
						// Track unique tools
						if _, ok := seenTools[toolName]; !ok {
							seenTools[toolName] = struct{}{}
							toolsUsed = append(toolsUsed, toolName)
						}

						// Apply MCP cost-to-token bump (parity with unary path)
						if mcpCostToTokens > 0 {
							if costPerUse := getToolCostPerUse(ctx, llmServiceURL, toolName); costPerUse > 0 {
								toolCostUsd += costPerUse
								toolTokenBump += int(math.Round(costPerUse * float64(mcpCostToTokens)))
							}
						}
					}
				}
			}

			if upd.Message != "" && upd.State == agentpb.AgentState_AGENT_STATE_COMPLETED {
				finalMessage = upd.Message
			}
			if upd.State == agentpb.AgentState_AGENT_STATE_FAILED {
				success = false
			}
		}

		// Flush any buffered partials
		flushPartial()

		out := strings.TrimSpace(finalMessage)
		if out == "" {
			out = strings.TrimSpace(outBuilder.String())
		}

		duration := time.Since(callStart).Milliseconds()
		if tokens == 0 && promptTokens+completionTokens > 0 {
			tokens = promptTokens + completionTokens
		}

		// Fallback: if provider is still empty, use provider_override from context when present
		if provider == "" && input.Context != nil {
			if v, ok := input.Context["provider_override"].(string); ok {
				if pv := strings.TrimSpace(strings.ToLower(v)); pv != "" {
					provider = pv
				}
			}
		}

		if model == "" && input.Context != nil {
			if v, ok := input.Context["model_override"].(string); ok {
				if mv := strings.TrimSpace(v); mv != "" {
					model = mv
				}
			}
		}

		// Note: toolsUsed already deduplicated via seenTools map during collection (line 1371-1373)

		if costUsd == 0 && (promptTokens > 0 || completionTokens > 0) {
			if model != "" {
				costUsd = pricing.CostForSplit(model, promptTokens, completionTokens)
			} else {
				costUsd = pricing.CostForSplit("", promptTokens, completionTokens)
			}
		} else if costUsd == 0 && tokens > 0 {
			costUsd = pricing.CostForTokens(model, tokens)
		}

		// Add tool costs (MCP cost bump)
		costUsd += toolCostUsd
		tokens += toolTokenBump

		// Emit LLM_OUTPUT event with usage metadata in Payload
		if wfID != "" && out != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    input.AgentID,
				Message:    truncateQuery(out, MaxLLMOutputChars),
				Payload: map[string]interface{}{
					"tokens_used":   tokens,
					"model_used":    model,
					"provider":      provider,
					"input_tokens":  promptTokens,
					"output_tokens": completionTokens,
					"cost_usd":      costUsd,
					"duration_ms":   duration,
				},
				Timestamp: time.Now(),
			})
		}

		return AgentExecutionResult{
			AgentID:        input.AgentID,
			Response:       out,
			TokensUsed:     tokens,
			ModelUsed:      model,
			Provider:       provider,
			InputTokens:    promptTokens,
			OutputTokens:   completionTokens,
			DurationMs:     duration,
			Success:        success,
			Error:          "",
			ToolsUsed:      toolsUsed,
			ToolExecutions: toolExecs,
		}, nil
	}

	runUnary := func() (AgentExecutionResult, error) {
		callStart := time.Now()
		grpcCtx, grpcCancel := context.WithTimeout(ctx, grpcTimeout)
		defer grpcCancel()

		var resp *agentpb.ExecuteTaskResponse
		err := grpcWrapper.Execute(grpcCtx, func() error {
			var execErr error
			resp, execErr = client.ExecuteTask(grpcCtx, req)
			return execErr
		})
		if err != nil {
			return AgentExecutionResult{}, err
		}

		out := strings.TrimSpace(resp.GetResult())
		duration := time.Since(callStart).Milliseconds()
		if resp.GetMetrics() != nil && resp.GetMetrics().LatencyMs > 0 {
			duration = resp.GetMetrics().LatencyMs
		}

		tokens := 0
		model := ""
		provider := ""
		promptTokens := 0
		completionTokens := 0
		costUsd := 0.0

		if mu := resp.GetMetrics(); mu != nil && mu.TokenUsage != nil {
			tokens = int(mu.TokenUsage.TotalTokens)
			promptTokens = int(mu.TokenUsage.PromptTokens)
			completionTokens = int(mu.TokenUsage.CompletionTokens)
			costUsd = mu.TokenUsage.CostUsd
			model = mu.TokenUsage.Model
			provider = mu.TokenUsage.Provider
		}
		if tokens == 0 && (promptTokens+completionTokens) > 0 {
			tokens = promptTokens + completionTokens
		}

		toolExecs := []ToolExecution{}
		toolsUsed := []string{}
		seenTools := map[string]struct{}{}
		toolCostUsd := 0.0
		toolTokenBump := 0
		for _, tr := range resp.ToolResults {
			toolName := tr.GetToolId()
			output := interface{}(nil)
			if tr.Output != nil {
				output = tr.Output.AsInterface()
			}
			toolExecs = append(toolExecs, ToolExecution{
				Tool:    toolName,
				Success: tr.Status == commonpb.StatusCode_STATUS_CODE_OK,
				Output:  output,
				Error:   tr.ErrorMessage,
			})
			if toolName != "" {
				if _, ok := seenTools[toolName]; !ok {
					seenTools[toolName] = struct{}{}
					toolsUsed = append(toolsUsed, toolName)
				}
				if mcpCostToTokens > 0 {
					if costPerUse := getToolCostPerUse(ctx, llmServiceURL, toolName); costPerUse > 0 {
						toolCostUsd += costPerUse
						toolTokenBump += int(math.Round(costPerUse * float64(mcpCostToTokens)))
					}
				}
			}
		}
		if len(toolsUsed) == 0 {
			for _, tc := range resp.ToolCalls {
				if tc.Name != "" {
					if _, ok := seenTools[tc.Name]; !ok {
						seenTools[tc.Name] = struct{}{}
						toolsUsed = append(toolsUsed, tc.Name)
					}
				}
			}
		}

		// Fallback: if provider is still empty, use provider_override from context when present
		if provider == "" && input.Context != nil {
			if v, ok := input.Context["provider_override"].(string); ok {
				if pv := strings.TrimSpace(strings.ToLower(v)); pv != "" {
					provider = pv
				}
			}
		}

		if model == "" && input.Context != nil {
			if v, ok := input.Context["model_override"].(string); ok {
				if mv := strings.TrimSpace(v); mv != "" {
					model = mv
				}
			}
		}

		if costUsd == 0 && (promptTokens > 0 || completionTokens > 0) {
			if model != "" {
				costUsd = pricing.CostForSplit(model, promptTokens, completionTokens)
			} else {
				costUsd = pricing.CostForSplit("", promptTokens, completionTokens)
			}
		} else if costUsd == 0 && tokens > 0 {
			costUsd = pricing.CostForTokens(model, tokens)
		}

		tokens += toolTokenBump
		costUsd += toolCostUsd

		success := resp.Status == commonpb.StatusCode_STATUS_CODE_OK
		errMsg := ""
		if !success && resp.ErrorMessage != "" {
			errMsg = resp.ErrorMessage
		} else if !success {
			errMsg = "agent execution failed"
		}

		if wfID != "" && out != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventLLMOutput),
				AgentID:    input.AgentID,
				Message:    truncateQuery(out, MaxLLMOutputChars),
				Payload: map[string]interface{}{
					"tokens_used":   tokens,
					"model_used":    model,
					"provider":      provider,
					"input_tokens":  promptTokens,
					"output_tokens": completionTokens,
					"cost_usd":      costUsd,
					"duration_ms":   duration,
				},
				Timestamp: time.Now(),
			})
		}

		return AgentExecutionResult{
			AgentID:        input.AgentID,
			Response:       out,
			TokensUsed:     tokens,
			ModelUsed:      model,
			Provider:       provider,
			InputTokens:    promptTokens,
			OutputTokens:   completionTokens,
			DurationMs:     duration,
			Success:        success,
			Error:          errMsg,
			ToolsUsed:      toolsUsed,
			ToolExecutions: toolExecs,
		}, nil
	}

	if useStreaming {
		if res, serr := runStreaming(); serr == nil {
			return res, nil
		} else {
			logger.Warn("Streaming execution failed, falling back to unary ExecuteTask", zap.Error(serr))
		}
	}

	return runUnary()
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

	// Publish TOOL_INVOKED event
	// Prefer ParentWorkflowID (task ID) for SSE streaming, fall back to Temporal workflow ID
	wfID := ""
	if input.ParentWorkflowID != "" {
		wfID = input.ParentWorkflowID
	} else if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
		wfID = info.WorkflowExecution.ID
	}

	// Helper to sanitize parameters for event emission
	sanitizeParams := func(params map[string]interface{}) map[string]interface{} {
		sanitized := make(map[string]interface{})
		for k, v := range params {
			// Redact common secret keys
			keyLower := strings.ToLower(k)
			if strings.Contains(keyLower, "key") ||
				strings.Contains(keyLower, "token") ||
				strings.Contains(keyLower, "secret") ||
				strings.Contains(keyLower, "password") ||
				strings.Contains(keyLower, "auth") {
				sanitized[k] = "[REDACTED]"
			} else if s, ok := v.(string); ok && len(s) > 500 {
				// Truncate long strings
				sanitized[k] = s[:500] + "...(truncated)"
			} else {
				sanitized[k] = v
			}
		}
		return sanitized
	}

	if wfID != "" {
		message := humanizeToolCall(toolName, input.ToolParameters)
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventToolInvoked),
			AgentID:    input.AgentID,
			Message:    message,
			Payload: map[string]interface{}{
				"tool":   toolName,
				"params": sanitizeParams(input.ToolParameters),
			},
			Timestamp: time.Now(),
		})
	}

	toolStartTime := time.Now()
	client := &http.Client{Timeout: 2 * time.Minute, Transport: interceptors.NewWorkflowHTTPRoundTripper(nil)}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		logger.Error("Failed to create HTTP request", "error", err)
		// Emit failure observation
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventToolObs),
				AgentID:    input.AgentID,
				Message:    "Tool execution failed: request creation error",
				Payload: map[string]interface{}{
					"tool":        toolName,
					"success":     false,
					"duration_ms": time.Since(toolStartTime).Milliseconds(),
				},
				Timestamp: time.Now(),
			})
		}
		return AgentExecutionResult{Success: false, Error: "Failed to create request"}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", input.AgentID)

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("HTTP request failed", "error", err)
		// Emit failure observation
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventToolObs),
				AgentID:    input.AgentID,
				Message:    fmt.Sprintf("Tool execution failed: %v", err),
				Payload: map[string]interface{}{
					"tool":        toolName,
					"success":     false,
					"duration_ms": time.Since(toolStartTime).Milliseconds(),
				},
				Timestamp: time.Now(),
			})
		}
		return AgentExecutionResult{Success: false, Error: fmt.Sprintf("Request failed: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error("Non-2xx response from /agent/query", "status", resp.StatusCode)
		// Emit failure observation
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventToolObs),
				AgentID:    input.AgentID,
				Message:    fmt.Sprintf("Tool execution failed: HTTP %d", resp.StatusCode),
				Payload: map[string]interface{}{
					"tool":        toolName,
					"success":     false,
					"duration_ms": time.Since(toolStartTime).Milliseconds(),
				},
				Timestamp: time.Now(),
			})
		}
		return AgentExecutionResult{Success: false, Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}, nil
	}

	// Parse response
	var agentResponse struct {
		Success      bool                   `json:"success"`
		Response     string                 `json:"response"`
		TokensUsed   int                    `json:"tokens_used"`
		ModelUsed    string                 `json:"model_used"`
		Provider     string                 `json:"provider"`
		FinishReason string                 `json:"finish_reason"`
		Metadata     map[string]interface{} `json:"metadata"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&agentResponse); err != nil {
		logger.Error("Failed to decode /agent/query response", "error", err)
		// Emit failure observation
		if wfID != "" {
			streaming.Get().Publish(wfID, streaming.Event{
				WorkflowID: wfID,
				Type:       string(StreamEventToolObs),
				AgentID:    input.AgentID,
				Message:    "Tool execution failed: invalid response",
				Payload: map[string]interface{}{
					"tool":        toolName,
					"success":     false,
					"duration_ms": time.Since(toolStartTime).Milliseconds(),
				},
				Timestamp: time.Now(),
			})
		}
		return AgentExecutionResult{Success: false, Error: "Failed to parse response"}, nil
	}

	// Extract detailed token metrics from metadata
	inputTokens := 0
	outputTokens := 0
	if agentResponse.Metadata != nil {
		if v, ok := agentResponse.Metadata["input_tokens"].(float64); ok {
			inputTokens = int(v)
		}
		if v, ok := agentResponse.Metadata["output_tokens"].(float64); ok {
			outputTokens = int(v)
		}
	}

	logger.Info("ExecuteAgentWithForcedTools completed",
		"success", agentResponse.Success,
		"tokens", agentResponse.TokensUsed,
		"model", agentResponse.ModelUsed,
		"provider", agentResponse.Provider,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
	)

	// Publish TOOL_OBSERVATION event
	if wfID != "" {
		msg := "Tool execution completed"
		if !agentResponse.Success {
			msg = "Tool execution failed"
		}
		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventToolObs),
			AgentID:    input.AgentID,
			Message:    msg,
			Payload: map[string]interface{}{
				"tool":        toolName,
				"success":     agentResponse.Success,
				"duration_ms": time.Since(toolStartTime).Milliseconds(),
			},
			Timestamp: time.Now(),
		})
	}

	// Publish LLM_OUTPUT SSE event with complete metadata
	if info := activity.GetInfo(ctx); info.WorkflowExecution.ID != "" {
		wfID = info.WorkflowExecution.ID
	}
	if wfID != "" && agentResponse.Response != "" {
		// Calculate cost using pricing service
		var costUsd float64
		if agentResponse.ModelUsed != "" && inputTokens > 0 && outputTokens > 0 {
			costUsd = pricing.CostForSplit(agentResponse.ModelUsed, inputTokens, outputTokens)
		}

		streaming.Get().Publish(wfID, streaming.Event{
			WorkflowID: wfID,
			Type:       string(StreamEventLLMOutput),
			AgentID:    input.AgentID,
			Message:    truncateQuery(agentResponse.Response, MaxLLMOutputChars),
			Payload: map[string]interface{}{
				"tokens_used":   agentResponse.TokensUsed,
				"model_used":    agentResponse.ModelUsed,
				"provider":      agentResponse.Provider,
				"input_tokens":  inputTokens,
				"output_tokens": outputTokens,
				"cost_usd":      costUsd,
			},
			Timestamp: time.Now(),
		})
	}

	// Extract tools used + executions from metadata when present
	toolsUsed := []string{toolName}
	var toolExecs []ToolExecution
	if agentResponse.Metadata != nil {
		if tu, ok := agentResponse.Metadata["tools_used"].([]interface{}); ok {
			toolsUsed = toolsUsed[:0]
			for _, t := range tu {
				if s, ok2 := t.(string); ok2 && s != "" {
					toolsUsed = append(toolsUsed, s)
				}
			}
		}
		if te, ok := agentResponse.Metadata["tool_executions"].([]interface{}); ok {
			for _, raw := range te {
				if m, ok2 := raw.(map[string]interface{}); ok2 {
					name, _ := m["tool"].(string)
					success, _ := m["success"].(bool)
					output := m["output"]
					errStr, _ := m["error"].(string)
					toolExecs = append(toolExecs, ToolExecution{
						Tool:    name,
						Success: success,
						Output:  output,
						Error:   errStr,
					})
				}
			}
		}
	}

	return AgentExecutionResult{
		Success:        agentResponse.Success,
		Response:       agentResponse.Response,
		TokensUsed:     agentResponse.TokensUsed,
		ModelUsed:      agentResponse.ModelUsed,
		Provider:       agentResponse.Provider,
		InputTokens:    inputTokens,
		OutputTokens:   outputTokens,
		ToolsUsed:      toolsUsed,
		ToolExecutions: toolExecs,
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
		// Determine the correct workflow ID for streaming (prefer parent)
		wfID := ""
		if input.ParentWorkflowID != "" {
			wfID = input.ParentWorkflowID
		} else if input.Context != nil {
			if p, ok := input.Context["parent_workflow_id"].(string); ok && p != "" {
				wfID = p
			}
		}
		if wfID == "" {
			wfID = info.WorkflowExecution.ID
		}

		message := fmt.Sprintf("Thinking: %s", truncateQuery(input.Query, MaxThinkingChars))
		eventData := EmitTaskUpdateInput{
			WorkflowID: wfID,
			EventType:  StreamEventAgentThinking,
			AgentID:    input.AgentID,
			Message:    message,
			Timestamp:  time.Now(),
		}
		activity.RecordHeartbeat(ctx, eventData)
		// Also publish to Redis Streams for SSE (use parent workflow ID when available)
		streaming.Get().Publish(wfID, streaming.Event{
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
		// Determine the correct workflow ID for streaming (prefer parent)
		wfID := ""
		if input.ParentWorkflowID != "" {
			wfID = input.ParentWorkflowID
		} else if input.Context != nil {
			if p, ok := input.Context["parent_workflow_id"].(string); ok && p != "" {
				wfID = p
			}
		}
		if wfID == "" {
			wfID = info.WorkflowExecution.ID
		}

		for _, call := range toolCalls {
			toolName, _ := call["tool"].(string)
			if toolName == "" {
				continue
			}
			message := humanizeToolCall(toolName, call["parameters"])
			eventData := EmitTaskUpdateInput{
				WorkflowID: wfID,
				EventType:  StreamEventToolInvoked,
				AgentID:    input.AgentID,
				Message:    message,
				Timestamp:  time.Now(),
			}
			activity.RecordHeartbeat(ctx, eventData)
			// Also publish to Redis Streams for SSE (use parent workflow ID when available)
			streaming.Get().Publish(wfID, streaming.Event{
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
			return fmt.Sprintf("Looking this up: '%s'", truncateQuery(query, 50))
		}
		return "Looking this up"
	case "calculator":
		if expr, ok := paramsMap["expression"].(string); ok {
			return fmt.Sprintf("Doing the math: %s", expr)
		}
		return "Doing the math"
	case "python_code", "code_executor", "python_executor":
		return "Running some code"
	case "read_file", "file_reader":
		if path, ok := paramsMap["path"].(string); ok {
			return fmt.Sprintf("Opening file: %s", path)
		}
		return "Opening a file"
	case "web_fetch":
		if url, ok := paramsMap["url"].(string); ok {
			return fmt.Sprintf("Grabbing content from: %s", truncateURL(url))
		}
		return "Grabbing something from the web"
	case "code_reader":
		return "Reviewing code"
	default:
		return fmt.Sprintf("Using %s", toolName)
	}
}

// truncateQuery truncates a query to a specified length (UTF-8 safe)
func truncateQuery(query string, maxLen int) string {
	runes := []rune(query)
	if len(runes) <= maxLen {
		return query
	}
	return string(runes[:maxLen-3]) + "..."
}

// truncateURL shortens a URL for display (UTF-8 safe)
func truncateURL(url string) string {
	runes := []rune(url)
	if len(runes) <= 50 {
		return url
	}
	// Try to preserve domain by cutting at query parameter boundary
	if idx := strings.Index(url, "?"); idx > 0 {
		runesBeforeQuery := []rune(url[:idx])
		if len(runesBeforeQuery) < 50 {
			return url[:idx] + "?..."
		}
	}
	// Otherwise truncate at character boundary
	return string(runes[:47]) + "..."
}
