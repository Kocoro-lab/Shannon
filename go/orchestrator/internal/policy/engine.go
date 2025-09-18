package policy

import (
	"container/list"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-policy-agent/opa/rego"
	"go.uber.org/zap"
)

// Engine defines the policy evaluation interface
type Engine interface {
	Evaluate(ctx context.Context, input *PolicyInput) (*Decision, error)
	LoadPolicies() error
	IsEnabled() bool
	// Environment returns the configured environment (e.g., dev|staging|prod)
	Environment() string
	// Mode returns the current enforcement mode (off|dry-run|enforce)
	Mode() Mode
}

// PolicyInput represents the input context for policy evaluation
type PolicyInput struct {
	// Core identifiers
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id,omitempty"`
	AgentID   string `json:"agent_id"`

	// Request details
	Query   string                 `json:"query"`
	Mode    string                 `json:"mode"` // simple, standard, complex
	Context map[string]interface{} `json:"context,omitempty"`

	// Security context
	Environment string `json:"environment"` // dev, staging, prod
	IPAddress   string `json:"ip_address,omitempty"`

	// Resource constraints
	ComplexityScore float64 `json:"complexity_score,omitempty"`
	TokenBudget     int     `json:"token_budget,omitempty"`

	// Vector-enhanced fields (optional, feature-gated)
	SimilarQueries  []SimilarQuery `json:"similar_queries,omitempty"`
	ContextScore    float64        `json:"context_score,omitempty"`
	SemanticCluster string         `json:"semantic_cluster,omitempty"`
	RiskProfile     string         `json:"risk_profile,omitempty"`

	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

// SimilarQuery represents a semantically similar historical query or decision context
type SimilarQuery struct {
	Query      string    `json:"query"`
	Outcome    string    `json:"outcome"`
	Confidence float64   `json:"confidence"`
	Timestamp  time.Time `json:"timestamp"`
}

// Decision represents the policy evaluation result
type Decision struct {
	// Core decision
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`

	// Obligations (future phase)
	RequireApproval bool                   `json:"require_approval,omitempty"`
	Obligations     map[string]interface{} `json:"obligations,omitempty"`

	// Audit
	PolicyVersion string            `json:"policy_version,omitempty"`
	AuditTags     map[string]string `json:"audit_tags,omitempty"`
}

// OPAEngine implements the Engine interface using OPA rego
type OPAEngine struct {
	config   *Config
	logger   *zap.Logger
	compiled *rego.PreparedEvalQuery
	enabled  bool
	// simple in-memory LRU cache for decisions
	cache *decisionCache
}

// NewOPAEngine creates a new OPA-based policy engine
func NewOPAEngine(config *Config, logger *zap.Logger) (*OPAEngine, error) {
	engine := &OPAEngine{
		config:  config,
		logger:  logger,
		enabled: config.Enabled && config.Mode != ModeOff,
		cache:   newDecisionCache(1000, 5*time.Minute), // 1K entries, 5min TTL
	}

	if engine.enabled {
		if err := engine.LoadPolicies(); err != nil {
			if config.FailClosed {
				return nil, fmt.Errorf("failed to load policies in fail-closed mode: %w", err)
			}
			logger.Warn("Failed to load policies, running in fail-open mode", zap.Error(err))
			engine.enabled = false
		}
	}

	return engine, nil
}

// LoadPolicies loads and compiles all policy files from the configured directory
func (e *OPAEngine) LoadPolicies() error {
	if !e.config.Enabled {
		return nil
	}

	policies := make(map[string]string)

	// Load all .rego files from the policy directory
	err := filepath.Walk(e.config.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".rego") {
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read policy file %s: %w", path, err)
			}

			// Use relative path as module name
			relPath, _ := filepath.Rel(e.config.Path, path)
			moduleName := strings.TrimSuffix(relPath, ".rego")
			policies[moduleName] = string(content)

			e.logger.Debug("Loaded policy file",
				zap.String("path", path),
				zap.String("module", moduleName),
			)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk policy directory: %w", err)
	}

	if len(policies) == 0 {
		e.logger.Warn("No policy files found", zap.String("path", e.config.Path))
		if e.config.FailClosed {
			return fmt.Errorf("no policies found in fail-closed mode")
		}
		return nil
	}

	// Compile policies
	regoOptions := []func(*rego.Rego){
		rego.Query("data.shannon.task.decision"),
	}

	for moduleName, content := range policies {
		regoOptions = append(regoOptions, rego.Module(moduleName, content))
	}

	regoBuilder := rego.New(regoOptions...)

	compiled, err := regoBuilder.PrepareForEval(context.Background())
	if err != nil {
		return fmt.Errorf("failed to compile policies: %w", err)
	}

	e.compiled = &compiled

	e.logger.Info("Policies loaded and compiled successfully",
		zap.Int("policy_count", len(policies)),
		zap.String("decision_query", "data.shannon.task.decision"),
	)

	// Record policy load metrics
	RecordPolicyLoad(e.config.Path, len(policies), float64(time.Now().Unix()))

	// Record policy version for tracking deployments
	versionHash := e.calculatePolicyVersion(policies)
	loadTimestamp := fmt.Sprintf("%d", time.Now().Unix())
	RecordPolicyVersion(e.config.Path, versionHash, loadTimestamp)

	return nil
}

// Evaluate evaluates the policy against the given input
func (e *OPAEngine) Evaluate(ctx context.Context, input *PolicyInput) (*Decision, error) {
	startTime := time.Now()

	// Default decision based on configuration
	defaultDecision := &Decision{
		Allow:  !e.config.FailClosed, // fail-open allows by default, fail-closed denies
		Reason: "policy engine disabled or no policies loaded",
		AuditTags: map[string]string{
			"policy_enabled": fmt.Sprintf("%t", e.enabled),
			"mode":           string(e.config.Mode),
		},
	}

	if !e.enabled || e.compiled == nil {
		e.logger.Debug("Policy evaluation skipped",
			zap.Bool("enabled", e.enabled),
			zap.Bool("compiled", e.compiled != nil),
		)
		return defaultDecision, nil
	}

	// Try cache first
	if d, ok := e.cache.Get(input); ok {
		// Record cache hit metrics
		RecordCacheHit(string(e.config.Mode))
		RecordSLOLatency(string(e.config.Mode), true, time.Since(startTime).Seconds())
		return d, nil
	}

	// Record cache miss
	RecordCacheMiss(string(e.config.Mode))

	// Convert input to map for OPA
	inputMap, err := e.inputToMap(input)
	if err != nil {
		e.logger.Error("Failed to convert input to map", zap.Error(err))
		// Record conversion error
		RecordSLOError("input_conversion", string(e.config.Mode))
		RecordError("input_conversion", string(e.config.Mode))

		if e.config.FailClosed {
			return &Decision{Allow: false, Reason: "input conversion failed"}, err
		}
		return defaultDecision, nil
	}

	// Evaluate policy
	results, err := e.compiled.Eval(ctx, rego.EvalInput(inputMap))
	if err != nil {
		e.logger.Error("Policy evaluation failed", zap.Error(err))
		// Record evaluation error
		RecordSLOError("policy_evaluation", string(e.config.Mode))
		RecordError("policy_evaluation", string(e.config.Mode))

		if e.config.FailClosed {
			return &Decision{Allow: false, Reason: "policy evaluation error"}, err
		}
		return defaultDecision, nil
	}

	// Parse results
	decision := e.parseResults(results, input)
	originalDecision := *decision // Save original for comparison metrics

	// Apply canary enforcement logic
	effectiveMode := e.determineEffectiveMode(input)
	decision = e.applyModeToDecision(decision, effectiveMode, input)

	// Record comprehensive metrics
	duration := time.Since(startTime)
	e.recordComprehensiveMetrics(input, &originalDecision, decision, effectiveMode, duration)

	e.logger.Debug("Policy evaluated",
		zap.Bool("allow", decision.Allow),
		zap.String("reason", decision.Reason),
		zap.Duration("duration", duration),
		zap.String("session_id", input.SessionID),
		zap.String("agent_id", input.AgentID),
		zap.String("effective_mode", string(effectiveMode)),
	)

	// Store in cache
	e.cache.Set(input, decision)
	return decision, nil
}

// IsEnabled returns whether the policy engine is enabled and ready
func (e *OPAEngine) IsEnabled() bool {
	return e.enabled && e.compiled != nil
}

// Environment returns the configured environment for the engine
func (e *OPAEngine) Environment() string { return e.config.Environment }

// Mode returns the configured enforcement mode for the engine
func (e *OPAEngine) Mode() Mode { return e.config.Mode }

// inputToMap converts PolicyInput to a map for OPA evaluation
func (e *OPAEngine) inputToMap(input *PolicyInput) (map[string]interface{}, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// parseResults parses OPA evaluation results into a Decision
func (e *OPAEngine) parseResults(results rego.ResultSet, input *PolicyInput) *Decision {
	decision := &Decision{
		Allow:  false, // Default deny
		Reason: "no matching policy rules",
		AuditTags: map[string]string{
			"session_id": input.SessionID,
			"agent_id":   input.AgentID,
			"mode":       input.Mode,
		},
	}

	if len(results) == 0 {
		e.logger.Debug("No policy results returned")
		return decision
	}

	// Parse the first result
	result := results[0]
	if len(result.Expressions) == 0 {
		return decision
	}

	// Try to parse structured decision
	value := result.Expressions[0].Value
	if valueMap, ok := value.(map[string]interface{}); ok {
		if allow, ok := valueMap["allow"].(bool); ok {
			decision.Allow = allow
		}
		if reason, ok := valueMap["reason"].(string); ok {
			decision.Reason = reason
		}
		if requireApproval, ok := valueMap["require_approval"].(bool); ok {
			decision.RequireApproval = requireApproval
		}
		if obligations, ok := valueMap["obligations"].(map[string]interface{}); ok {
			decision.Obligations = obligations
		}
	} else if allow, ok := value.(bool); ok {
		// Simple boolean result
		decision.Allow = allow
		if allow {
			decision.Reason = "allowed by policy"
		} else {
			decision.Reason = "denied by policy"
		}
	}

	return decision
}

// --- internal decision cache (simple LRU with TTL) ---

// The cache key includes environment, mode, user, agent, token budget, rounded complexity and a hash of the query
// to avoid query-pattern related false positives.

type decisionCache struct {
	cap    int
	ttl    time.Duration
	mu     sync.Mutex
	list   *list.List               // MRU at front
	m      map[string]*list.Element // key -> element
	hits   int64
	misses int64
}

type cacheEntry struct {
	key       string
	expiresAt time.Time
	decision  *Decision
}

func newDecisionCache(cap int, ttl time.Duration) *decisionCache {
	if cap <= 0 {
		cap = 1024
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &decisionCache{
		cap:  cap,
		ttl:  ttl,
		list: list.New(),
		m:    make(map[string]*list.Element),
	}
}

func (c *decisionCache) makeKey(input *PolicyInput) string {
	// Hash query to keep key small
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(input.Query)))
	qh := h.Sum64()
	// Round complexity to 2 decimals to reduce key churn
	comp := fmt.Sprintf("%.2f", input.ComplexityScore)
	return fmt.Sprintf("%s|%s|%s|%s|%d|%s|%x",
		input.Environment, input.Mode, input.UserID, input.AgentID, input.TokenBudget, comp, qh,
	)
}

func (c *decisionCache) Get(input *PolicyInput) (*Decision, bool) {
	key := c.makeKey(input)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		ce := el.Value.(cacheEntry)
		if ce.expiresAt.After(now) {
			c.list.MoveToFront(el)
			atomic.AddInt64(&c.hits, 1)
			return ce.decision, true
		}
		// expired
		c.list.Remove(el)
		delete(c.m, key)
	}
	atomic.AddInt64(&c.misses, 1)
	return nil, false
}

func (c *decisionCache) Set(input *PolicyInput, d *Decision) {
	key := c.makeKey(input)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[key]; ok {
		// update existing
		el.Value = cacheEntry{key: key, expiresAt: time.Now().Add(c.ttl), decision: d}
		c.list.MoveToFront(el)
		return
	}
	// insert new
	el := c.list.PushFront(cacheEntry{key: key, expiresAt: time.Now().Add(c.ttl), decision: d})
	c.m[key] = el
	if c.list.Len() > c.cap {
		// evict LRU
		lru := c.list.Back()
		if lru != nil {
			ce := lru.Value.(cacheEntry)
			delete(c.m, ce.key)
			c.list.Remove(lru)
		}
	}
}

// Stats returns cumulative cache hit/miss counts
func (c *decisionCache) Stats() (hits, misses int64) {
	return atomic.LoadInt64(&c.hits), atomic.LoadInt64(&c.misses)
}

// --- Canary Enforcement Implementation ---

// determineEffectiveMode determines the effective enforcement mode for a specific request
// based on canary configuration, emergency controls, and rollout rules
func (e *OPAEngine) determineEffectiveMode(input *PolicyInput) Mode {
	// Emergency kill switch overrides everything - forces dry-run
	if e.config.EmergencyKillSwitch {
		e.logger.Debug("Emergency kill switch active - forcing dry-run mode",
			zap.String("user_id", input.UserID),
			zap.String("agent_id", input.AgentID),
		)
		return ModeDryRun
	}

	// If canary is not enabled, use the configured global mode
	if !e.config.Canary.Enabled {
		return e.config.Mode
	}

	// Check explicit dry-run user overrides (these always get dry-run)
	for _, dryRunUser := range e.config.Canary.DryRunUsers {
		if input.UserID == dryRunUser {
			e.logger.Debug("User in dry-run override list",
				zap.String("user_id", input.UserID),
			)
			return ModeDryRun
		}
	}

	// Check explicit enforce user overrides (these always get enforce mode)
	for _, enforceUser := range e.config.Canary.EnforceUsers {
		if input.UserID == enforceUser {
			e.logger.Debug("User in enforce list - using enforce mode",
				zap.String("user_id", input.UserID),
			)
			return ModeEnforce
		}
	}

	// Check explicit enforce agent overrides (these always get enforce mode)
	for _, enforceAgent := range e.config.Canary.EnforceAgents {
		if input.AgentID == enforceAgent {
			e.logger.Debug("Agent in enforce list - using enforce mode",
				zap.String("agent_id", input.AgentID),
			)
			return ModeEnforce
		}
	}

	// Apply percentage-based rollout using deterministic hash
	if e.config.Canary.EnforcePercentage > 0 {
		// Create deterministic hash based on user and agent to ensure consistent routing
		hash := e.calculateCanaryHash(input.UserID, input.AgentID, input.SessionID)
		percentage := int(hash % 100)

		if percentage < e.config.Canary.EnforcePercentage {
			e.logger.Debug("Request selected for enforce mode via percentage rollout",
				zap.String("user_id", input.UserID),
				zap.String("agent_id", input.AgentID),
				zap.Int("percentage", percentage),
				zap.Int("enforce_threshold", e.config.Canary.EnforcePercentage),
			)
			return ModeEnforce
		}

		e.logger.Debug("Request selected for dry-run mode via percentage rollout",
			zap.String("user_id", input.UserID),
			zap.String("agent_id", input.AgentID),
			zap.Int("percentage", percentage),
			zap.Int("enforce_threshold", e.config.Canary.EnforcePercentage),
		)
	}

	// Default to dry-run for safety during canary rollout
	return ModeDryRun
}

// applyModeToDecision applies the effective enforcement mode to the policy decision
func (e *OPAEngine) applyModeToDecision(decision *Decision, effectiveMode Mode, input *PolicyInput) *Decision {
	// Add audit tags for tracking
	if decision.AuditTags == nil {
		decision.AuditTags = make(map[string]string)
	}
	decision.AuditTags["effective_mode"] = string(effectiveMode)
	decision.AuditTags["configured_mode"] = string(e.config.Mode)
	decision.AuditTags["canary_enabled"] = fmt.Sprintf("%t", e.config.Canary.Enabled)

	switch effectiveMode {
	case ModeEnforce:
		// In enforce mode, respect the policy decision as-is
		e.logger.Debug("Enforcing policy decision",
			zap.Bool("allow", decision.Allow),
			zap.String("reason", decision.Reason),
			zap.String("user_id", input.UserID),
		)
		return decision

	case ModeDryRun:
		// In dry-run mode, always allow but log what would have happened
		originalDecision := *decision // Copy original decision

		decision.Allow = true // Override to allow in dry-run
		if !originalDecision.Allow {
			// Add dry-run context to reason
			decision.Reason = fmt.Sprintf("DRY-RUN: would have been denied - %s", originalDecision.Reason)
		} else {
			decision.Reason = fmt.Sprintf("DRY-RUN: would have been allowed - %s", originalDecision.Reason)
		}

		// Log dry-run decision for analysis
		e.logger.Info("Dry-run policy evaluation",
			zap.Bool("would_allow", originalDecision.Allow),
			zap.Bool("actual_allow", decision.Allow),
			zap.String("original_reason", originalDecision.Reason),
			zap.String("dry_run_reason", decision.Reason),
			zap.String("user_id", input.UserID),
			zap.String("agent_id", input.AgentID),
		)

		return decision

	case ModeOff:
		// Engine should be disabled if mode is off, but handle gracefully
		decision.Allow = !e.config.FailClosed
		decision.Reason = "policy engine disabled"
		return decision

	default:
		// Unknown mode, default to safe behavior
		e.logger.Warn("Unknown effective mode, defaulting to dry-run",
			zap.String("mode", string(effectiveMode)),
		)
		decision.Allow = true
		decision.Reason = fmt.Sprintf("unknown mode %s, defaulting to allow", effectiveMode)
		return decision
	}
}

// calculateCanaryHash creates a deterministic hash for canary percentage calculation
func (e *OPAEngine) calculateCanaryHash(userID, agentID, sessionID string) uint32 {
	// Create a deterministic string for hashing
	// Include user and agent for consistent routing, but not session to avoid per-session variance
	hashInput := fmt.Sprintf("%s|%s", userID, agentID)

	// Use MD5 for consistent percentage distribution
	h := md5.Sum([]byte(hashInput))

	// Convert first 4 bytes to uint32
	return uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
}

// recordComprehensiveMetrics records all detailed metrics for dashboard and SLO tracking
func (e *OPAEngine) recordComprehensiveMetrics(input *PolicyInput, originalDecision, finalDecision *Decision, effectiveMode Mode, duration time.Duration) {
	// Basic evaluation metrics
	decisionLabel := "allow"
	if !finalDecision.Allow {
		decisionLabel = "deny"
	}

	// Record basic evaluation
	RecordEvaluation(decisionLabel, string(effectiveMode), finalDecision.Reason)
	RecordEvaluationDuration(string(effectiveMode), duration.Seconds())
	RecordSLOLatency(string(effectiveMode), false, duration.Seconds()) // false = cache miss

	// Record canary routing decision
	routingReason := e.determineCanaryRoutingReason(input, effectiveMode)
	RecordCanaryDecision(string(e.config.Mode), string(effectiveMode), routingReason, decisionLabel)

	// Record top deny reasons if denied
	if !finalDecision.Allow {
		RecordDenyReason(finalDecision.Reason, string(effectiveMode))
	}

	// Record mode comparison for dry-run analysis
	originalDecisionLabel := "allow"
	if !originalDecision.Allow {
		originalDecisionLabel = "deny"
	}

	userType := e.classifyUserType(input)
	RecordModeComparison(originalDecisionLabel, string(effectiveMode), originalDecisionLabel, userType)

	// Record dry-run divergence if applicable
	if effectiveMode == ModeDryRun && originalDecisionLabel != decisionLabel {
		if originalDecisionLabel == "deny" {
			RecordDryRunDivergence("would_deny")
		} else {
			RecordDryRunDivergence("would_allow")
		}
	}

	// Record cache size periodically (every 100th request to avoid overhead)
	if e.cache != nil {
		e.cache.mu.Lock()
		cacheSize := e.cache.list.Len()
		e.cache.mu.Unlock()
		RecordCacheSize("policy_decisions", cacheSize)
	}
}

// determineCanaryRoutingReason determines why a request was routed to enforce vs dry-run
func (e *OPAEngine) determineCanaryRoutingReason(input *PolicyInput, effectiveMode Mode) string {
	if e.config.EmergencyKillSwitch {
		return "emergency_kill_switch"
	}

	if !e.config.Canary.Enabled {
		return "canary_disabled"
	}

	// Check explicit overrides
	for _, user := range e.config.Canary.DryRunUsers {
		if input.UserID == user {
			return "explicit_dry_run_user"
		}
	}

	for _, user := range e.config.Canary.EnforceUsers {
		if input.UserID == user {
			return "explicit_enforce_user"
		}
	}

	for _, agent := range e.config.Canary.EnforceAgents {
		if input.AgentID == agent {
			return "explicit_enforce_agent"
		}
	}

	if e.config.Canary.EnforcePercentage > 0 {
		return "percentage_rollout"
	}

	return "default_dry_run"
}

// classifyUserType categorizes users for metrics
func (e *OPAEngine) classifyUserType(input *PolicyInput) string {
	// Check if user is in enforce lists (privileged)
	for _, user := range e.config.Canary.EnforceUsers {
		if input.UserID == user {
			return "enforce_user"
		}
	}

	// Check if user is in dry-run override list
	for _, user := range e.config.Canary.DryRunUsers {
		if input.UserID == user {
			return "dry_run_user"
		}
	}

	// Check if it's a system user
	if input.UserID == "" || input.UserID == "system" || input.UserID == "orchestrator" {
		return "system_user"
	}

	return "regular_user"
}

// calculatePolicyVersion creates a version hash from policy content for tracking
func (e *OPAEngine) calculatePolicyVersion(policies map[string]string) string {
	// Create a deterministic hash from all policy content
	h := md5.New()

	// Sort policy names for deterministic ordering
	policyNames := make([]string, 0, len(policies))
	for name := range policies {
		policyNames = append(policyNames, name)
	}

	// Sort to ensure consistent hash regardless of map iteration order
	for i := 0; i < len(policyNames); i++ {
		for j := i + 1; j < len(policyNames); j++ {
			if policyNames[i] > policyNames[j] {
				policyNames[i], policyNames[j] = policyNames[j], policyNames[i]
			}
		}
	}

	// Hash each policy in sorted order
	for _, name := range policyNames {
		content := policies[name]
		h.Write([]byte(name))
		h.Write([]byte(content))
	}

	// Return first 8 hex chars for reasonable uniqueness
	return fmt.Sprintf("%x", h.Sum(nil)[:4])
}
