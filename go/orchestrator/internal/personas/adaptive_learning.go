package personas

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AdaptiveLearning implements learning from persona selection outcomes
type AdaptiveLearning struct {
	personaPerformance map[string]*PersonaPerformance
	contextPatterns    map[string]*ContextPattern
	mu                 sync.RWMutex
	logger             *zap.Logger
	windowSize         time.Duration
}

// PersonaPerformance tracks performance metrics for a persona
type PersonaPerformance struct {
	SuccessRate    float64
	AvgConfidence  float64
	SelectionCount int
	RecentResults  []SelectionOutcome
	LastUpdated    time.Time
	TaskSpecialty  map[string]float64 // task type -> success rate
}

// ContextPattern represents learned patterns about request contexts
type ContextPattern struct {
	Keywords      []string
	SuccessfulPersonas map[string]float64 // persona ID -> success rate
	Frequency     int
	LastSeen      time.Time
}

// SelectionOutcome represents the outcome of a persona selection
type SelectionOutcome struct {
	PersonaID   string
	Success     bool
	Confidence  float64
	TaskType    string
	UserFeedback float64 // 0-1 satisfaction score
	Timestamp   time.Time
	Description string
}

// NewAdaptiveLearning creates a new adaptive learning system
func NewAdaptiveLearning(logger *zap.Logger) *AdaptiveLearning {
	return &AdaptiveLearning{
		personaPerformance: make(map[string]*PersonaPerformance),
		contextPatterns:    make(map[string]*ContextPattern),
		logger:             logger,
		windowSize:         24 * time.Hour, // 24-hour learning window
	}
}

// RecordSelection records a persona selection for learning
func (al *AdaptiveLearning) RecordSelection(req *SelectionRequest, result *SelectionResult) {
	al.mu.Lock()
	defer al.mu.Unlock()

	personaID := result.PersonaID
	
	// Initialize persona performance if needed
	if _, exists := al.personaPerformance[personaID]; !exists {
		al.personaPerformance[personaID] = &PersonaPerformance{
			SuccessRate:    0.5, // Neutral starting point
			AvgConfidence:  0.5,
			SelectionCount: 0,
			RecentResults:  make([]SelectionOutcome, 0),
			LastUpdated:    time.Now(),
			TaskSpecialty:  make(map[string]float64),
		}
	}

	perf := al.personaPerformance[personaID]
	perf.SelectionCount++
	perf.LastUpdated = time.Now()

	// For now, assume success based on confidence (real implementation would get feedback)
	success := result.Confidence > 0.6
	
	outcome := SelectionOutcome{
		PersonaID:   personaID,
		Success:     success,
		Confidence:  result.Confidence,
		TaskType:    req.TaskType,
		UserFeedback: result.Confidence, // Placeholder - would be real user feedback
		Timestamp:   time.Now(),
		Description: req.Description,
	}

	// Add to recent results
	perf.RecentResults = append(perf.RecentResults, outcome)
	
	// Keep only recent results within window
	cutoff := time.Now().Add(-al.windowSize)
	validResults := make([]SelectionOutcome, 0)
	for _, result := range perf.RecentResults {
		if result.Timestamp.After(cutoff) {
			validResults = append(validResults, result)
		}
	}
	perf.RecentResults = validResults

	// Update metrics
	al.updatePersonaMetrics(perf)
	
	// Learn context patterns
	al.learnContextPattern(req.Description, personaID, success)

	al.logger.Debug("Recorded selection for adaptive learning",
		zap.String("persona", personaID),
		zap.Bool("success", success),
		zap.Float64("confidence", result.Confidence))
}

// updatePersonaMetrics recalculates persona performance metrics
func (al *AdaptiveLearning) updatePersonaMetrics(perf *PersonaPerformance) {
	if len(perf.RecentResults) == 0 {
		return
	}

	// Calculate success rate
	successCount := 0
	totalConfidence := 0.0
	taskCounts := make(map[string]int)
	taskSuccesses := make(map[string]int)

	for _, result := range perf.RecentResults {
		if result.Success {
			successCount++
		}
		totalConfidence += result.Confidence
		
		if result.TaskType != "" {
			taskCounts[result.TaskType]++
			if result.Success {
				taskSuccesses[result.TaskType]++
			}
		}
	}

	perf.SuccessRate = float64(successCount) / float64(len(perf.RecentResults))
	perf.AvgConfidence = totalConfidence / float64(len(perf.RecentResults))

	// Update task specialties
	for taskType, count := range taskCounts {
		successRate := float64(taskSuccesses[taskType]) / float64(count)
		perf.TaskSpecialty[taskType] = successRate
	}
}

// learnContextPattern extracts and learns from context patterns
func (al *AdaptiveLearning) learnContextPattern(description, personaID string, success bool) {
	// Extract key terms from description
	keywords := al.extractKeyTerms(description)
	if len(keywords) == 0 {
		return
	}

	// Create pattern signature
	signature := strings.Join(keywords, "_")
	
	// Initialize pattern if needed
	if _, exists := al.contextPatterns[signature]; !exists {
		al.contextPatterns[signature] = &ContextPattern{
			Keywords:           keywords,
			SuccessfulPersonas: make(map[string]float64),
			Frequency:          0,
			LastSeen:           time.Now(),
		}
	}

	pattern := al.contextPatterns[signature]
	pattern.Frequency++
	pattern.LastSeen = time.Now()

	// Update persona success rate for this pattern
	if _, exists := pattern.SuccessfulPersonas[personaID]; !exists {
		pattern.SuccessfulPersonas[personaID] = 0.0
	}

	// Exponential moving average for success rate
	alpha := 0.1 // Learning rate
	currentRate := pattern.SuccessfulPersonas[personaID]
	newRate := currentRate
	
	if success {
		newRate = currentRate + alpha*(1.0-currentRate)
	} else {
		newRate = currentRate + alpha*(0.0-currentRate)
	}
	
	pattern.SuccessfulPersonas[personaID] = newRate
}

// extractKeyTerms extracts important terms from a description
func (al *AdaptiveLearning) extractKeyTerms(description string) []string {
	// Simple keyword extraction - in production, use more sophisticated NLP
	words := strings.Fields(strings.ToLower(description))
	
	// Important terms that indicate context
	importantTerms := map[string]bool{
		"algorithm": true, "analyze": true, "api": true, "architecture": true,
		"build": true, "code": true, "data": true, "database": true,
		"debug": true, "design": true, "develop": true, "implement": true,
		"integrate": true, "optimize": true, "research": true, "test": true,
		"visualize": true, "web": true, "machine": true, "learning": true,
		"statistics": true, "model": true, "frontend": true, "backend": true,
		"mobile": true, "security": true, "deploy": true, "scale": true,
	}

	var keywords []string
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:()[]{}\"'")
		if len(word) > 3 && importantTerms[word] {
			keywords = append(keywords, word)
		}
	}

	// Limit to most important terms
	if len(keywords) > 5 {
		keywords = keywords[:5]
	}

	return keywords
}

// GetPersonaAdjustment returns a score adjustment based on learned performance
func (al *AdaptiveLearning) GetPersonaAdjustment(personaID string, req *SelectionRequest) float64 {
	al.mu.RLock()
	defer al.mu.RUnlock()

	perf, exists := al.personaPerformance[personaID]
	if !exists {
		return 0.0 // No adjustment for unknown personas
	}

	// Base adjustment from overall success rate
	baseAdjustment := (perf.SuccessRate - 0.5) * 0.2 // ±10% adjustment

	// Task-specific adjustment
	taskAdjustment := 0.0
	if req.TaskType != "" {
		if taskRate, exists := perf.TaskSpecialty[req.TaskType]; exists {
			taskAdjustment = (taskRate - 0.5) * 0.15 // ±7.5% adjustment
		}
	}

	// Context pattern adjustment
	contextAdjustment := al.getContextAdjustment(req.Description, personaID)

	// Combine adjustments with decay for older data
	age := time.Since(perf.LastUpdated)
	decayFactor := math.Exp(-age.Hours() / 24.0) // Decay over 24 hours

	totalAdjustment := (baseAdjustment + taskAdjustment + contextAdjustment) * decayFactor

	// Limit adjustment range
	return math.Max(-0.3, math.Min(0.3, totalAdjustment))
}

// getContextAdjustment calculates adjustment based on context patterns
func (al *AdaptiveLearning) getContextAdjustment(description, personaID string) float64 {
	keywords := al.extractKeyTerms(description)
	if len(keywords) == 0 {
		return 0.0
	}

	// Check for matching patterns
	bestMatch := 0.0
	for _, pattern := range al.contextPatterns {
		// Calculate pattern similarity
		similarity := al.calculatePatternSimilarity(keywords, pattern.Keywords)
		if similarity > 0.5 { // Threshold for pattern match
			if successRate, exists := pattern.SuccessfulPersonas[personaID]; exists {
				adjustment := (successRate - 0.5) * 0.1 * similarity
				if math.Abs(adjustment) > math.Abs(bestMatch) {
					bestMatch = adjustment
				}
			}
		}
	}

	return bestMatch
}

// calculatePatternSimilarity calculates similarity between keyword sets
func (al *AdaptiveLearning) calculatePatternSimilarity(keywords1, keywords2 []string) float64 {
	if len(keywords1) == 0 || len(keywords2) == 0 {
		return 0.0
	}

	// Simple Jaccard similarity
	set1 := make(map[string]bool)
	for _, word := range keywords1 {
		set1[word] = true
	}

	intersection := 0
	for _, word := range keywords2 {
		if set1[word] {
			intersection++
		}
	}

	union := len(keywords1) + len(keywords2) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// GetConfidenceAdjustment returns confidence adjustment based on historical performance
func (al *AdaptiveLearning) GetConfidenceAdjustment(personaID string) float64 {
	al.mu.RLock()
	defer al.mu.RUnlock()

	perf, exists := al.personaPerformance[personaID]
	if !exists || perf.SelectionCount < 3 {
		return 0.0 // Not enough data
	}

	// Adjust confidence based on actual vs predicted performance
	expectedConfidence := perf.AvgConfidence
	actualSuccess := perf.SuccessRate

	// If actual success is much different from predicted confidence
	confidenceDelta := actualSuccess - expectedConfidence
	
	// Apply small adjustment (max ±0.1)
	adjustment := confidenceDelta * 0.1
	return math.Max(-0.1, math.Min(0.1, adjustment))
}

// GetPersonaRecommendations returns recommended personas for a request
func (al *AdaptiveLearning) GetPersonaRecommendations(req *SelectionRequest, limit int) []PersonaRecommendation {
	al.mu.RLock()
	defer al.mu.RUnlock()

	type PersonaScore struct {
		PersonaID string
		Score     float64
	}

	var scores []PersonaScore
	
	// Score all personas based on learned patterns
	for personaID, perf := range al.personaPerformance {
		score := perf.SuccessRate
		
		// Task-specific bonus
		if req.TaskType != "" {
			if taskRate, exists := perf.TaskSpecialty[req.TaskType]; exists {
				score = (score + taskRate) / 2.0
			}
		}

		// Context pattern bonus
		contextBonus := al.getContextAdjustment(req.Description, personaID)
		score += contextBonus

		scores = append(scores, PersonaScore{
			PersonaID: personaID,
			Score:     score,
		})
	}

	// Sort by score
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// Convert to recommendations
	var recommendations []PersonaRecommendation
	for i, scoreItem := range scores {
		if i >= limit {
			break
		}

		if perf, exists := al.personaPerformance[scoreItem.PersonaID]; exists {
			recommendations = append(recommendations, PersonaRecommendation{
				PersonaID:   scoreItem.PersonaID,
				Confidence:  scoreItem.Score,
				Reasoning:   fmt.Sprintf("Historical success rate: %.1f%%", perf.SuccessRate*100),
				SelectionCount: perf.SelectionCount,
			})
		}
	}

	return recommendations
}

// PersonaRecommendation represents a learned recommendation
type PersonaRecommendation struct {
	PersonaID      string
	Confidence     float64
	Reasoning      string
	SelectionCount int
}

// CleanupOldData removes old learning data to prevent memory bloat
func (al *AdaptiveLearning) CleanupOldData() {
	al.mu.Lock()
	defer al.mu.Unlock()

	cutoff := time.Now().Add(-al.windowSize * 2) // Keep data for 2x window size

	// Clean persona performance data
	for personaID, perf := range al.personaPerformance {
		if perf.LastUpdated.Before(cutoff) && perf.SelectionCount < 5 {
			delete(al.personaPerformance, personaID)
		}
	}

	// Clean context patterns
	for signature, pattern := range al.contextPatterns {
		if pattern.LastSeen.Before(cutoff) && pattern.Frequency < 3 {
			delete(al.contextPatterns, signature)
		}
	}

	al.logger.Debug("Cleaned up old adaptive learning data")
}

// GetLearningStats returns statistics about the learning system
func (al *AdaptiveLearning) GetLearningStats() LearningStats {
	al.mu.RLock()
	defer al.mu.RUnlock()

	stats := LearningStats{
		TrackedPersonas:   len(al.personaPerformance),
		ContextPatterns:   len(al.contextPatterns),
		TotalSelections:   0,
		AvgSuccessRate:    0.0,
	}

	totalSuccessRate := 0.0
	for _, perf := range al.personaPerformance {
		stats.TotalSelections += perf.SelectionCount
		totalSuccessRate += perf.SuccessRate
	}

	if len(al.personaPerformance) > 0 {
		stats.AvgSuccessRate = totalSuccessRate / float64(len(al.personaPerformance))
	}

	return stats
}

// LearningStats provides statistics about the learning system
type LearningStats struct {
	TrackedPersonas  int
	ContextPatterns  int
	TotalSelections  int
	AvgSuccessRate   float64
}