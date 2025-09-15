package patterns

import (
	"fmt"
	"sync"

	"go.temporal.io/sdk/workflow"
	"github.com/Kocoro-lab/Shannon/go/orchestrator/internal/activities"
)

// PatternType represents the type of multi-agent pattern
type PatternType string

const (
	PatternReflection     PatternType = "reflection"
	PatternReact          PatternType = "react"
	PatternChainOfThought PatternType = "chain_of_thought"
	PatternDebate         PatternType = "debate"
	PatternTreeOfThoughts PatternType = "tree_of_thoughts"
	PatternEnsemble       PatternType = "ensemble"
)

// PatternCapability describes what a pattern can do
type PatternCapability string

const (
	CapabilityIterativeImprovement PatternCapability = "iterative_improvement"
	CapabilityStepByStep          PatternCapability = "step_by_step"
	CapabilityMultiPerspective     PatternCapability = "multi_perspective"
	CapabilityConflictResolution   PatternCapability = "conflict_resolution"
	CapabilityExploration          PatternCapability = "exploration"
	CapabilityConsensusBuilding    PatternCapability = "consensus_building"
)

// Pattern defines the interface for all multi-agent patterns
type Pattern interface {
	// Execute runs the pattern with given context and configuration
	Execute(ctx workflow.Context, input PatternInput) (*PatternResult, error)

	// GetType returns the pattern type
	GetType() PatternType

	// GetCapabilities returns what this pattern can do
	GetCapabilities() []PatternCapability

	// EstimateTokens estimates token usage for this pattern
	EstimateTokens(input PatternInput) int
}

// PatternInput provides input to a pattern
type PatternInput struct {
	Query       string
	Context     map[string]interface{}
	History     []string
	SessionID   string
	UserID      string
	Config      interface{} // Pattern-specific config
	BudgetMax   int
	ModelTier   string
}

// PatternResult contains the output from a pattern
type PatternResult struct {
	Result       string
	TokensUsed   int
	Confidence   float64
	Metadata     map[string]interface{}
	AgentResults []activities.AgentExecutionResult
}

// PatternRegistry manages available patterns
type PatternRegistry struct {
	mu       sync.RWMutex
	patterns map[PatternType]Pattern

	// Pattern selection strategy
	selector PatternSelector
}

// PatternSelector decides which pattern to use
type PatternSelector interface {
	SelectPattern(query string, context map[string]interface{}, available []Pattern) (Pattern, error)
}

// DefaultPatternSelector selects patterns based on task characteristics
type DefaultPatternSelector struct{}

// SelectPattern chooses the best pattern for a task
func (s *DefaultPatternSelector) SelectPattern(query string, context map[string]interface{}, available []Pattern) (Pattern, error) {
	// Simple heuristic-based selection
	// Could be enhanced with ML-based selection

	// Check for explicit pattern hint
	if patternHint, ok := context["pattern"].(string); ok {
		for _, p := range available {
			if string(p.GetType()) == patternHint {
				return p, nil
			}
		}
	}

	// Default to first available pattern
	if len(available) > 0 {
		return available[0], nil
	}

	return nil, fmt.Errorf("no suitable pattern found")
}

var (
	globalRegistry *PatternRegistry
	registryOnce   sync.Once
)

// GetRegistry returns the global pattern registry
func GetRegistry() *PatternRegistry {
	registryOnce.Do(func() {
		globalRegistry = &PatternRegistry{
			patterns: make(map[PatternType]Pattern),
			selector: &DefaultPatternSelector{},
		}
		// Register default patterns
		registerDefaultPatterns()
	})
	return globalRegistry
}

// Register adds a pattern to the registry
func (r *PatternRegistry) Register(pattern Pattern) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pattern == nil {
		return fmt.Errorf("cannot register nil pattern")
	}

	r.patterns[pattern.GetType()] = pattern
	return nil
}

// Get retrieves a pattern by type
func (r *PatternRegistry) Get(patternType PatternType) (Pattern, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pattern, ok := r.patterns[patternType]
	return pattern, ok
}

// List returns all registered patterns
func (r *PatternRegistry) List() []Pattern {
	r.mu.RLock()
	defer r.mu.RUnlock()

	patterns := make([]Pattern, 0, len(r.patterns))
	for _, p := range r.patterns {
		patterns = append(patterns, p)
	}
	return patterns
}

// SelectForTask chooses the best pattern for a given task
func (r *PatternRegistry) SelectForTask(query string, context map[string]interface{}) (Pattern, error) {
	available := r.List()
	if len(available) == 0 {
		return nil, fmt.Errorf("no patterns registered")
	}

	return r.selector.SelectPattern(query, context, available)
}

// SetSelector updates the pattern selection strategy
func (r *PatternRegistry) SetSelector(selector PatternSelector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selector = selector
}

// registerDefaultPatterns registers built-in patterns
func registerDefaultPatterns() {
    // Register existing patterns
    // These will be implemented as Pattern interface wrappers
    // around existing implementations
    _ = globalRegistry.Register(reflectionPattern{})
    _ = globalRegistry.Register(reactPattern {})
    _ = globalRegistry.Register(chainOfThoughtPattern{})
    _ = globalRegistry.Register(debatePattern{})
    _ = globalRegistry.Register(treeOfThoughtsPattern{})
}
