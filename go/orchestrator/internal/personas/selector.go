package personas

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// KeywordSelector implements persona selection using keyword matching
type KeywordSelector struct {
	config             *Config
	cache              Cache
	concurrencyControl *ConcurrencyController
	keywordMatcher     *KeywordMatcher
	budgetCalculator   *BudgetCalculator
	metrics            *Metrics
	logger             *zap.Logger
	errorClassifier    *ErrorClassifier

	// State management
	mu     sync.RWMutex
	closed bool
}

// NewKeywordSelector creates a new keyword-based persona selector
func NewKeywordSelector(config *Config, cache Cache, metrics *Metrics, logger *zap.Logger) *KeywordSelector {
	return &KeywordSelector{
		config:             config,
		cache:              cache,
		concurrencyControl: NewConcurrencyController(config.Selection.MaxConcurrentSelections, metrics, logger),
		keywordMatcher:     NewKeywordMatcher(logger),
		budgetCalculator:   NewBudgetCalculator(),
		metrics:            metrics,
		logger:             logger,
		errorClassifier:    NewErrorClassifier(),
		closed:             false,
	}
}

// SelectPersona selects the best persona for the given request
func (s *KeywordSelector) SelectPersona(ctx context.Context, req *SelectionRequest) (*SelectionResult, error) {
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
			Confidence:    0.95, // High confidence for cached results
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
		s.metrics.RecordTimeout("acquire_slot", "")
		return nil, NewSelectionError("", "", err)
	}
	defer s.concurrencyControl.ReleaseSlot()

	// Perform selection
	result, err := s.performSelection(ctx, req, startTime)
	if err != nil {
		s.metrics.RecordError("selection_failed", "")
		fallbackResult, fallbackErr := s.handleSelectionError(ctx, req, err, startTime)
		if fallbackErr == nil && fallbackResult != nil {
			// Cache the fallback result too
			s.cache.Set(cacheKey, fallbackResult.PersonaID)
		}
		return fallbackResult, fallbackErr
	}

	// Cache the result
	s.cache.Set(cacheKey, result.PersonaID)

	// Record metrics
	s.metrics.RecordSelection(result.PersonaID, result.Method, result.SelectionTime, true)
	s.metrics.RecordConfidence(result.PersonaID, result.Method, result.Confidence)

	return result, nil
}

// performSelection performs the actual persona selection logic
func (s *KeywordSelector) performSelection(ctx context.Context, req *SelectionRequest, startTime time.Time) (*SelectionResult, error) {
	// Get available personas (apply user preferences filter)
	availablePersonas := s.filterByPreferences(req.Preferences)
	if len(availablePersonas) == 0 {
		return nil, fmt.Errorf("no available personas after filtering")
	}

	// Calculate scores for each persona
	candidates := make([]*CandidatePersona, 0, len(availablePersonas))

	for _, persona := range availablePersonas {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		candidate := s.evaluatePersona(req, persona)
		if candidate.Score > 0 {
			candidates = append(candidates, candidate)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable personas found")
	}

	// Sort candidates by score (descending)
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
		Reasoning:     best.Reasoning,
		Alternatives:  s.getAlternatives(candidates, 3),
		SelectionTime: time.Since(startTime),
		Method:        best.Method,
		CacheHit:      false,
	}

	return result, nil
}

// evaluatePersona evaluates a single persona against the request
func (s *KeywordSelector) evaluatePersona(req *SelectionRequest, persona *PersonaConfig) *CandidatePersona {
	// Calculate keyword matching score
	keywordScore := s.keywordMatcher.CalculateScore(req.Description, persona.Keywords)

	// Apply additional scoring factors
	score := keywordScore

	// Priority bonus (higher priority personas get slight boost)
	if persona.Priority > 1 {
		priorityBonus := float64(persona.Priority-1) * 0.05 // 5% per priority level
		score += priorityBonus
	}

	// Resource constraint penalty
	if req.ResourceLimits != nil {
		if req.ResourceLimits.MaxTokens > 0 && persona.MaxTokens > req.ResourceLimits.MaxTokens {
			score *= 0.8 // 20% penalty for exceeding token limits
		}
	}

	// Task type compatibility bonus
	if req.TaskType != "" {
		typeBonus := s.getTaskTypeCompatibility(persona.ID, req.TaskType)
		score += typeBonus
	}

	// Calculate confidence based on score and other factors
	confidence := s.calculateConfidence(keywordScore, score, persona)

	return &CandidatePersona{
		PersonaID:  persona.ID,
		Config:     persona,
		Score:      score,
		Confidence: confidence,
		Method:     "keyword_matching",
		Reasoning:  fmt.Sprintf("Keyword score: %.2f, Final score: %.2f", keywordScore, score),
	}
}

// getTaskTypeCompatibility returns a compatibility bonus for task types
func (s *KeywordSelector) getTaskTypeCompatibility(personaID, taskType string) float64 {
	// Define compatibility matrix
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

// calculateConfidence calculates confidence based on various factors
func (s *KeywordSelector) calculateConfidence(keywordScore, finalScore float64, persona *PersonaConfig) float64 {
	// Base confidence from keyword score
	confidence := keywordScore

	// Boost confidence for high-quality matches
	if keywordScore > 0.8 {
		confidence = 0.9 + (keywordScore-0.8)*0.5 // Scale 0.8-1.0 to 0.9-1.0
	} else if keywordScore > 0.5 {
		confidence = 0.7 + (keywordScore-0.5)*0.6 // Scale 0.5-0.8 to 0.7-0.9
	} else {
		confidence = keywordScore * 1.4 // Scale 0-0.5 to 0-0.7
	}

	// Adjust confidence based on persona characteristics
	if len(persona.Keywords) > 10 {
		confidence += 0.05 // More keywords = more reliable matching
	}

	// Ensure confidence is within bounds
	if confidence > 1.0 {
		confidence = 1.0
	} else if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// validateRequest validates the selection request
func (s *KeywordSelector) validateRequest(req *SelectionRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}

	if req.Description == "" {
		return fmt.Errorf("description cannot be empty")
	}

	if req.ComplexityScore < 0 || req.ComplexityScore > 1 {
		return fmt.Errorf("complexity score must be between 0 and 1, got %f", req.ComplexityScore)
	}

	return nil
}

// validateSelection validates the selected persona
func (s *KeywordSelector) validateSelection(candidate *CandidatePersona, req *SelectionRequest) error {
	if candidate == nil {
		return fmt.Errorf("candidate cannot be nil")
	}

	if candidate.Config == nil {
		return fmt.Errorf("candidate config cannot be nil")
	}

	// Check resource limits
	if req.ResourceLimits != nil {
		if req.ResourceLimits.MaxTokens > 0 && candidate.Config.MaxTokens > req.ResourceLimits.MaxTokens {
			return fmt.Errorf("persona %s exceeds token limit: %d > %d",
				candidate.PersonaID, candidate.Config.MaxTokens, req.ResourceLimits.MaxTokens)
		}
	}

	// Check user preferences
	if req.Preferences != nil && len(req.Preferences.ExcludedPersonas) > 0 {
		for _, excluded := range req.Preferences.ExcludedPersonas {
			if candidate.PersonaID == excluded {
				return fmt.Errorf("persona %s is in user's excluded list", candidate.PersonaID)
			}
		}
	}

	return nil
}

// filterByPreferences filters personas based on user preferences
func (s *KeywordSelector) filterByPreferences(prefs *UserPreferences) []*PersonaConfig {
	var result []*PersonaConfig

	// Create exclusion set
	excluded := make(map[string]bool)
	if prefs != nil {
		for _, personaID := range prefs.ExcludedPersonas {
			excluded[personaID] = true
		}
	}

	// If user has preferred personas, prioritize them
	if prefs != nil && len(prefs.PreferredPersonas) > 0 {
		// First add preferred personas (if they exist and aren't excluded)
		for _, personaID := range prefs.PreferredPersonas {
			if !excluded[personaID] {
				if persona, err := s.config.GetPersona(personaID); err == nil {
					result = append(result, persona)
				}
			}
		}

		// Then add non-preferred, non-excluded personas
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

// isInPreferred checks if a persona ID is in the preferred list
func (s *KeywordSelector) isInPreferred(personaID string, preferred []string) bool {
	for _, id := range preferred {
		if id == personaID {
			return true
		}
	}
	return false
}

// generateCacheKey generates a cache key from the request
func (s *KeywordSelector) generateCacheKey(req *SelectionRequest) string {
	return generateCacheKey(req)
}

// getAlternatives returns top N alternative persona choices
func (s *KeywordSelector) getAlternatives(candidates []*CandidatePersona, n int) []AlternativeChoice {
	var alternatives []AlternativeChoice

	// Skip the first one (it's the selected persona) and take next N
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

// handleSelectionError handles selection errors with fallback strategies
func (s *KeywordSelector) handleSelectionError(ctx context.Context, req *SelectionRequest, err error, startTime time.Time) (*SelectionResult, error) {
	s.logger.Warn("Selection failed, attempting fallback", zap.Error(err))

	// Determine fallback strategy
	fallbackPersonaID := s.config.Selection.FallbackStrategy
	if fallbackPersonaID == "" {
		fallbackPersonaID = "generalist"
	}

	// Validate fallback persona exists
	if _, err := s.config.GetPersona(fallbackPersonaID); err != nil {
		// If fallback doesn't exist, use first available persona
		for id := range s.config.Personas {
			fallbackPersonaID = id
			break
		}
	}

	s.metrics.RecordFallback("error_fallback", "")

	result := &SelectionResult{
		PersonaID:     fallbackPersonaID,
		Confidence:    0.3, // Low confidence for fallback
		Reasoning:     fmt.Sprintf("Fallback due to error: %v", err),
		SelectionTime: time.Since(startTime),
		Method:        "fallback",
		CacheHit:      false,
	}

	return result, nil
}

// GetPersona retrieves a persona by ID
func (s *KeywordSelector) GetPersona(personaID string) (*PersonaConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrManagerClosed
	}

	return s.config.GetPersona(personaID)
}

// ListPersonas returns personas matching the filter
func (s *KeywordSelector) ListPersonas(filter *PersonaFilter) ([]*PersonaConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrManagerClosed
	}

	return s.config.ListPersonas(filter), nil
}

// ValidateConfig validates the current configuration
func (s *KeywordSelector) ValidateConfig() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrManagerClosed
	}

	return s.config.Validate()
}

// Close gracefully shuts down the selector
func (s *KeywordSelector) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	// Close cache
	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			s.logger.Error("Failed to close cache", zap.Error(err))
		}
	}

	s.logger.Info("Keyword selector closed successfully")
	return nil
}
