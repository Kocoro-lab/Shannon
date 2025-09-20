package personas

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// EnhancedSelector implements advanced persona selection with semantic matching
type EnhancedSelector struct {
	config             *Config
	cache              Cache
	concurrencyControl *ConcurrencyController
	keywordMatcher     *KeywordMatcher
	semanticMatcher    *SemanticMatcher
	budgetCalculator   *BudgetCalculator
	metrics            *Metrics
	logger             *zap.Logger
	errorClassifier    *ErrorClassifier

	// Advanced features
	adaptiveLearning *AdaptiveLearning

	// State management
	mu     sync.RWMutex
	closed bool
}

// NewEnhancedSelector creates a new enhanced persona selector
func NewEnhancedSelector(config *Config, cache Cache, metrics *Metrics, logger *zap.Logger, matcherConfig *MatcherConfig) *EnhancedSelector {
	// Use provided matcher config or create default
	if matcherConfig == nil {
		matcherConfig = &MatcherConfig{
			EmbeddingEnabled: config.Selection.Method == "semantic" || config.Selection.Method == "enhanced",
			LocalFallback:    true,
			APITimeout:       2000,             // 2 second timeout
			EmbeddingConfig:  config.Embedding, // Pass embedding config from main config
		}
	} else if matcherConfig.EmbeddingConfig == nil {
		// If matcher config is provided but no embedding config, use from main config
		matcherConfig.EmbeddingConfig = config.Embedding
	}

	semanticMatcher := NewSemanticMatcher(matcherConfig, logger, metrics)

	// Train TF-IDF model with persona descriptions
	if err := semanticMatcher.TrainTFIDF(config.Personas); err != nil {
		logger.Warn("Failed to train TF-IDF model", zap.Error(err))
	}

	return &EnhancedSelector{
		config:             config,
		cache:              cache,
		concurrencyControl: NewConcurrencyController(config.Selection.MaxConcurrentSelections, metrics, logger),
		keywordMatcher:     NewKeywordMatcher(logger),
		semanticMatcher:    semanticMatcher,
		budgetCalculator:   NewBudgetCalculator(),
		metrics:            metrics,
		logger:             logger,
		errorClassifier:    NewErrorClassifier(),
		adaptiveLearning:   NewAdaptiveLearning(logger),
		closed:             false,
	}
}

// SelectPersona selects the best persona using enhanced matching techniques
func (s *EnhancedSelector) SelectPersona(ctx context.Context, req *SelectionRequest) (*SelectionResult, error) {
	startTime := time.Now()

	// Check if selector is closed
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrManagerClosed
	}
	s.mu.RUnlock()

	// Validate request
	if err := s.validateRequest(req); err != nil {
		s.metrics.RecordError("invalid_request", "")
		return nil, NewSelectionError("", "", err)
	}

	// Apply complexity threshold filter
	if req.ComplexityScore < s.config.Selection.ComplexityThreshold {
		result := &SelectionResult{
			PersonaID:     "generalist",
			Confidence:    1.0,
			Reasoning:     fmt.Sprintf("Low complexity score (%.2f < %.2f)", req.ComplexityScore, s.config.Selection.ComplexityThreshold),
			SelectionTime: time.Since(startTime),
			Method:        "complexity_threshold",
			CacheHit:      false,
		}
		s.metrics.RecordSelection("generalist", "complexity_threshold", time.Since(startTime), true)
		return result, nil
	}

	// Check cache
	cacheKey := s.generateCacheKey(req)
	if cachedPersonaID, hit := s.cache.Get(cacheKey); hit {
		result := &SelectionResult{
			PersonaID:     cachedPersonaID,
			Confidence:    0.95,
			Reasoning:     "Retrieved from cache",
			SelectionTime: time.Since(startTime),
			Method:        "cache",
			CacheHit:      true,
		}
		s.metrics.RecordSelection(cachedPersonaID, "cache", time.Since(startTime), true)
		return result, nil
	}

	// Acquire concurrency slot
	if err := s.concurrencyControl.AcquireSlot(ctx); err != nil {
		s.metrics.RecordError("concurrency_limit", "")
		return nil, NewSelectionError("", "", err)
	}
	defer s.concurrencyControl.ReleaseSlot()

	// Perform enhanced selection
	result, err := s.performEnhancedSelection(ctx, req, startTime)
	if err != nil {
		s.metrics.RecordError("selection_failed", "")
		return s.handleSelectionError(ctx, req, err, startTime)
	}

	// Cache the result
	s.cache.Set(cacheKey, result.PersonaID)

	// Record metrics
	s.metrics.RecordSelection(result.PersonaID, result.Method, result.SelectionTime, true)
	s.metrics.RecordConfidence(result.PersonaID, result.Method, result.Confidence)

	// Learn from this selection for future improvements
	s.adaptiveLearning.RecordSelection(req, result)

	return result, nil
}

// performEnhancedSelection performs advanced persona selection
func (s *EnhancedSelector) performEnhancedSelection(ctx context.Context, req *SelectionRequest, startTime time.Time) (*SelectionResult, error) {
	// Get available personas
	availablePersonas := s.filterByPreferences(req.Preferences)
	if len(availablePersonas) == 0 {
		return nil, fmt.Errorf("no available personas after filtering")
	}

	// Use different strategies based on request characteristics
	var candidates []*CandidatePersona
	var method string

	// Decide on selection strategy
	if s.shouldUseSemanticMatching(req) {
		candidates, method = s.semanticSelection(ctx, req, availablePersonas)
	} else {
		candidates, method = s.keywordSelection(ctx, req, availablePersonas)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable personas found")
	}

	// Sort candidates by score
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Select the best candidate
	best := candidates[0]

	// Validate the selection
	if err := s.validateSelection(best, req); err != nil {
		return nil, err
	}

	// Create result
	result := &SelectionResult{
		PersonaID:     best.PersonaID,
		Confidence:    best.Confidence,
		Reasoning:     fmt.Sprintf("%s (method: %s)", best.Reasoning, method),
		Alternatives:  s.getAlternatives(candidates, 3),
		SelectionTime: time.Since(startTime),
		Method:        method,
		CacheHit:      false,
	}

	return result, nil
}

// shouldUseSemanticMatching determines if semantic matching should be used
func (s *EnhancedSelector) shouldUseSemanticMatching(req *SelectionRequest) bool {
	// Use semantic matching for:
	// 1. Complex requests (high complexity score)
	// 2. Long descriptions (more context for semantic analysis)
	// 3. When semantic matching is enabled

	// Semantic matching is enabled if explicitly set or if method is semantic/enhanced
	if !s.config.Selection.EnableSemanticMatching &&
		s.config.Selection.Method != "semantic" &&
		s.config.Selection.Method != "enhanced" {
		return false
	}

	// High complexity requests benefit from semantic matching
	if req.ComplexityScore > 0.7 {
		return true
	}

	// Long descriptions have more semantic content
	if len(req.Description) > 100 {
		return true
	}

	// Check if description contains technical terms that might benefit from semantic analysis
	technicalTerms := []string{"algorithm", "architecture", "implement", "analyze", "optimize", "integrate"}
	descLower := strings.ToLower(req.Description)
	for _, term := range technicalTerms {
		if strings.Contains(descLower, term) {
			return true
		}
	}

	return false
}

// semanticSelection performs selection using semantic matching
func (s *EnhancedSelector) semanticSelection(ctx context.Context, req *SelectionRequest, personas []*PersonaConfig) ([]*CandidatePersona, string) {
	candidates := make([]*CandidatePersona, 0, len(personas))

	for _, persona := range personas {
		select {
		case <-ctx.Done():
			return candidates, "semantic_timeout"
		default:
		}

		// Use semantic matcher for scoring
		score, err := s.semanticMatcher.CalculateScore(req.Description, persona)
		if err != nil {
			s.logger.Warn("Semantic matching failed for persona",
				zap.String("persona", persona.ID), zap.Error(err))
			// Fallback to keyword matching
			score = s.keywordMatcher.CalculateScore(req.Description, persona.Keywords)
		}

		if score > 0 {
			candidate := &CandidatePersona{
				PersonaID:  persona.ID,
				Config:     persona,
				Score:      score,
				Confidence: s.calculateEnhancedConfidence(score, persona, req),
				Method:     "semantic_matching",
				Reasoning:  fmt.Sprintf("Semantic score: %.2f", score),
			}

			// Apply additional scoring factors
			candidate = s.enhanceCandidate(candidate, req)
			candidates = append(candidates, candidate)
		}
	}

	return candidates, "semantic_matching"
}

// keywordSelection performs traditional keyword-based selection
func (s *EnhancedSelector) keywordSelection(ctx context.Context, req *SelectionRequest, personas []*PersonaConfig) ([]*CandidatePersona, string) {
	candidates := make([]*CandidatePersona, 0, len(personas))

	for _, persona := range personas {
		select {
		case <-ctx.Done():
			return candidates, "keyword_timeout"
		default:
		}

		candidate := s.evaluatePersonaKeywords(req, persona)
		if candidate.Score > 0 {
			candidates = append(candidates, candidate)
		}
	}

	return candidates, "keyword_matching"
}

// enhanceCandidate applies additional scoring factors to a candidate
func (s *EnhancedSelector) enhanceCandidate(candidate *CandidatePersona, req *SelectionRequest) *CandidatePersona {
	// Priority bonus
	if candidate.Config.Priority > 1 {
		priorityBonus := float64(candidate.Config.Priority-1) * 0.05
		candidate.Score += priorityBonus
	}

	// Resource constraint penalty
	if req.ResourceLimits != nil {
		if req.ResourceLimits.MaxTokens > 0 && candidate.Config.MaxTokens > req.ResourceLimits.MaxTokens {
			candidate.Score *= 0.8
		}
	}

	// Task type compatibility bonus
	if req.TaskType != "" {
		typeBonus := s.getTaskTypeCompatibility(candidate.PersonaID, req.TaskType)
		candidate.Score += typeBonus
	}

	// Adaptive learning adjustment
	adjustment := s.adaptiveLearning.GetPersonaAdjustment(candidate.PersonaID, req)
	candidate.Score *= (1.0 + adjustment)

	// Ensure score doesn't exceed 1.0
	candidate.Score = math.Min(candidate.Score, 1.0)

	return candidate
}

// calculateEnhancedConfidence calculates confidence with additional factors
func (s *EnhancedSelector) calculateEnhancedConfidence(score float64, persona *PersonaConfig, req *SelectionRequest) float64 {
	baseConfidence := score

	// Boost confidence for high-quality matches
	if score > 0.8 {
		baseConfidence = 0.9 + (score-0.8)*0.5
	} else if score > 0.5 {
		baseConfidence = 0.7 + (score-0.5)*0.6
	} else {
		baseConfidence = score * 1.4
	}

	// Historical performance adjustment
	historicalBonus := s.adaptiveLearning.GetConfidenceAdjustment(persona.ID)
	baseConfidence += historicalBonus

	// Ensure confidence is within bounds
	return math.Max(0.0, math.Min(1.0, baseConfidence))
}

// Implement other required methods from Selector interface
func (s *EnhancedSelector) evaluatePersonaKeywords(req *SelectionRequest, persona *PersonaConfig) *CandidatePersona {
	keywordScore := s.keywordMatcher.CalculateScore(req.Description, persona.Keywords)

	return &CandidatePersona{
		PersonaID:  persona.ID,
		Config:     persona,
		Score:      keywordScore,
		Confidence: s.calculateEnhancedConfidence(keywordScore, persona, req),
		Method:     "keyword_matching",
		Reasoning:  fmt.Sprintf("Keyword score: %.2f", keywordScore),
	}
}

func (s *EnhancedSelector) getTaskTypeCompatibility(personaID, taskType string) float64 {
	compatibility := map[string]map[string]float64{
		"researcher": {
			"research":    0.3,
			"analysis":    0.2,
			"investigate": 0.2,
			"study":       0.2,
		},
		"coder": {
			"coding":      0.3,
			"programming": 0.3,
			"debugging":   0.25,
			"development": 0.2,
		},
		"analyst": {
			"analysis":      0.3,
			"data":          0.25,
			"statistics":    0.25,
			"visualization": 0.2,
		},
	}

	if personaCompat, exists := compatibility[personaID]; exists {
		if bonus, exists := personaCompat[taskType]; exists {
			return bonus
		}
	}

	return 0.0
}

func (s *EnhancedSelector) validateRequest(req *SelectionRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if req.Description == "" {
		return fmt.Errorf("description cannot be empty")
	}
	if req.ComplexityScore < 0 || req.ComplexityScore > 1 {
		return fmt.Errorf("complexity score must be between 0 and 1")
	}
	return nil
}

func (s *EnhancedSelector) validateSelection(candidate *CandidatePersona, req *SelectionRequest) error {
	if candidate == nil || candidate.Config == nil {
		return fmt.Errorf("invalid candidate")
	}

	// Check resource limits
	if req.ResourceLimits != nil {
		if req.ResourceLimits.MaxTokens > 0 && candidate.Config.MaxTokens > req.ResourceLimits.MaxTokens {
			return fmt.Errorf("persona %s exceeds token limit", candidate.PersonaID)
		}
	}

	// Check user preferences
	if req.Preferences != nil {
		for _, excluded := range req.Preferences.ExcludedPersonas {
			if candidate.PersonaID == excluded {
				return fmt.Errorf("persona %s is excluded", candidate.PersonaID)
			}
		}
	}

	return nil
}

func (s *EnhancedSelector) filterByPreferences(prefs *UserPreferences) []*PersonaConfig {
	var result []*PersonaConfig

	excluded := make(map[string]bool)
	if prefs != nil {
		for _, personaID := range prefs.ExcludedPersonas {
			excluded[personaID] = true
		}
	}

	if prefs != nil && len(prefs.PreferredPersonas) > 0 {
		// Add preferred personas first
		for _, personaID := range prefs.PreferredPersonas {
			if !excluded[personaID] {
				if persona, err := s.config.GetPersona(personaID); err == nil {
					result = append(result, persona)
				}
			}
		}

		// Add non-preferred, non-excluded personas
		for _, persona := range s.config.Personas {
			if !excluded[persona.ID] && !s.isInPreferred(persona.ID, prefs.PreferredPersonas) {
				result = append(result, persona)
			}
		}
	} else {
		// No preferences, just exclude banned ones
		for _, persona := range s.config.Personas {
			if !excluded[persona.ID] {
				result = append(result, persona)
			}
		}
	}

	return result
}

func (s *EnhancedSelector) isInPreferred(personaID string, preferred []string) bool {
	for _, id := range preferred {
		if id == personaID {
			return true
		}
	}
	return false
}

func (s *EnhancedSelector) generateCacheKey(req *SelectionRequest) string {
	return generateCacheKey(req)
}

func (s *EnhancedSelector) getAlternatives(candidates []*CandidatePersona, n int) []AlternativeChoice {
	var alternatives []AlternativeChoice

	start := 1
	if len(candidates) <= start {
		return alternatives
	}

	end := start + n
	if end > len(candidates) {
		end = len(candidates)
	}

	for i := start; i < end; i++ {
		candidate := candidates[i]
		alternatives = append(alternatives, AlternativeChoice{
			PersonaID:  candidate.PersonaID,
			Score:      candidate.Score,
			Confidence: candidate.Confidence,
		})
	}

	return alternatives
}

func (s *EnhancedSelector) handleSelectionError(ctx context.Context, req *SelectionRequest, err error, startTime time.Time) (*SelectionResult, error) {
	s.logger.Warn("Enhanced selection failed, attempting fallback", zap.Error(err))

	fallbackPersonaID := s.config.Selection.FallbackStrategy
	if fallbackPersonaID == "" {
		fallbackPersonaID = "generalist"
	}

	if _, err := s.config.GetPersona(fallbackPersonaID); err != nil {
		for id := range s.config.Personas {
			fallbackPersonaID = id
			break
		}
	}

	s.metrics.RecordFallback("error_fallback", "")

	result := &SelectionResult{
		PersonaID:     fallbackPersonaID,
		Confidence:    0.3,
		Reasoning:     fmt.Sprintf("Fallback due to error: %v", err),
		SelectionTime: time.Since(startTime),
		Method:        "fallback",
		CacheHit:      false,
	}

	return result, nil
}

// Implement remaining Selector interface methods
func (s *EnhancedSelector) GetPersona(personaID string) (*PersonaConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrManagerClosed
	}

	return s.config.GetPersona(personaID)
}

func (s *EnhancedSelector) ListPersonas(filter *PersonaFilter) ([]*PersonaConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrManagerClosed
	}

	return s.config.ListPersonas(filter), nil
}

func (s *EnhancedSelector) ValidateConfig() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrManagerClosed
	}

	return s.config.Validate()
}

func (s *EnhancedSelector) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			s.logger.Error("Failed to close cache", zap.Error(err))
		}
	}

	s.logger.Info("Enhanced selector closed successfully")
	return nil
}
