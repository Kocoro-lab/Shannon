package personas

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"go.uber.org/zap"
)

// SemanticMatcher provides enhanced persona matching using multiple techniques
type SemanticMatcher struct {
	keywordMatcher   *KeywordMatcher
	tfidfModel       *TFIDFModel
	embeddingAPI     EmbeddingAPI
	localEmbeddings  *LocalEmbeddingModel
	config           *MatcherConfig
	logger           *zap.Logger
	metrics          *Metrics
	fallbackMode     bool
}

// NewSemanticMatcher creates a new semantic matcher
func NewSemanticMatcher(config *MatcherConfig, logger *zap.Logger, metrics *Metrics) *SemanticMatcher {
	sm := &SemanticMatcher{
		keywordMatcher:  NewKeywordMatcher(logger),
		tfidfModel:      NewTFIDFModel(),
		localEmbeddings: NewLocalEmbeddingModel(128), // 128-dim local embeddings
		config:          config,
		logger:          logger,
		metrics:         metrics,
		fallbackMode:    false,
	}

	// Initialize with persona descriptions for training
	return sm
}

// CalculateScore calculates semantic similarity score between description and persona
func (sm *SemanticMatcher) CalculateScore(description string, persona *PersonaConfig) (float64, error) {
	startTime := time.Now()
	defer func() {
		sm.metrics.RecordSemanticMatchTime("composite", time.Since(startTime))
	}()

	// 1. Start with keyword matching (fast, reliable baseline)
	keywordScore := sm.keywordMatcher.CalculateScore(description, persona.Keywords)
	
	// 2. Add TF-IDF similarity for better semantic understanding
	tfidfScore := sm.tfidfModel.Similarity(description, persona.Description)
	
	// 3. For uncertain cases, use embedding similarity
	var embeddingScore float64
	if sm.config.EmbeddingEnabled && (keywordScore < 0.4 || tfidfScore < 0.4) {
		var err error
		embeddingScore, err = sm.getEmbeddingSimilarityWithFallback(
			context.Background(), description, persona.Description)
		if err != nil {
			sm.logger.Warn("Embedding similarity failed, using TF-IDF + keywords", 
				zap.Error(err), zap.String("persona", persona.ID))
			return (keywordScore + tfidfScore) / 2, nil
		}
	}

	// 4. Domain-specific boosting
	domainBoost := sm.calculateDomainBoost(description, persona)
	
	// 5. Weighted combination based on confidence
	if embeddingScore > 0 {
		// Use all three methods with weights
		weights := sm.calculateWeights(keywordScore, tfidfScore, embeddingScore)
		finalScore := keywordScore*weights[0] + tfidfScore*weights[1] + embeddingScore*weights[2]
		finalScore += domainBoost
		return math.Min(finalScore, 1.0), nil
	}

	// Use keyword + TF-IDF + domain boost
	baseScore := (keywordScore*0.6 + tfidfScore*0.4) + domainBoost
	return math.Min(baseScore, 1.0), nil
}

// getEmbeddingSimilarityWithFallback tries external API first, then local model
func (sm *SemanticMatcher) getEmbeddingSimilarityWithFallback(ctx context.Context, desc1, desc2 string) (float64, error) {
	// Try external API first if available and not in fallback mode
	if !sm.fallbackMode && sm.embeddingAPI != nil && sm.config.EmbeddingEnabled {
		apiStartTime := time.Now()
		
		// Use short timeout for quick failure
		apiCtx, cancel := context.WithTimeout(ctx, time.Duration(sm.config.APITimeout))
		defer cancel()

		score, err := sm.embeddingAPI.GetSimilarity(apiCtx, desc1, desc2)
		sm.metrics.RecordEmbeddingAPICall("external", "similarity", time.Since(apiStartTime))
		
		if err == nil {
			return score, nil
		}

		sm.logger.Warn("External embedding API failed, falling back to local", zap.Error(err))
		if sm.config.LocalFallback {
			sm.fallbackMode = true // Switch to fallback mode temporarily
		}
	}

	// Use local embedding model
	if sm.localEmbeddings != nil {
		localStartTime := time.Now()
		
		emb1, err := sm.localEmbeddings.GetEmbedding(desc1)
		if err != nil {
			return 0, fmt.Errorf("local embedding for desc1 failed: %w", err)
		}
		
		emb2, err := sm.localEmbeddings.GetEmbedding(desc2)
		if err != nil {
			return 0, fmt.Errorf("local embedding for desc2 failed: %w", err)
		}

		similarity := sm.cosineSimilarity(emb1, emb2)
		sm.metrics.RecordEmbeddingAPICall("local", "similarity", time.Since(localStartTime))
		
		return similarity, nil
	}

	return 0, fmt.Errorf("no embedding method available")
}

// calculateWeights determines optimal weights based on score distribution
func (sm *SemanticMatcher) calculateWeights(keyword, tfidf, embedding float64) [3]float64 {
	// Higher scores get more weight
	total := keyword + tfidf + embedding
	if total == 0 {
		return [3]float64{0.5, 0.3, 0.2} // Default weights
	}

	// Normalize and adjust
	kw := keyword / total
	tf := tfidf / total
	em := embedding / total

	// Boost keyword matching if it's confident
	if keyword > 0.7 {
		kw += 0.1
	}

	// Normalize to sum to 1
	sum := kw + tf + em
	return [3]float64{kw / sum, tf / sum, em / sum}
}

// cosineSimilarity calculates cosine similarity between two vectors
func (sm *SemanticMatcher) cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// TrainTFIDF trains the TF-IDF model with persona descriptions
func (sm *SemanticMatcher) TrainTFIDF(personas map[string]*PersonaConfig) error {
	documents := make([]string, 0, len(personas))
	
	for _, persona := range personas {
		// Combine description and keywords for training
		doc := persona.Description + " " + strings.Join(persona.Keywords, " ")
		documents = append(documents, doc)
	}

	return sm.tfidfModel.Train(documents)
}

// TFIDFModel implements basic TF-IDF similarity calculation
type TFIDFModel struct {
	vocabulary map[string]int
	idf        map[string]float64
	documents  []map[string]float64 // TF scores for each document
	trained    bool
}

// NewTFIDFModel creates a new TF-IDF model
func NewTFIDFModel() *TFIDFModel {
	return &TFIDFModel{
		vocabulary: make(map[string]int),
		idf:        make(map[string]float64),
		documents:  make([]map[string]float64, 0),
		trained:    false,
	}
}

// Train trains the TF-IDF model on a corpus of documents
func (model *TFIDFModel) Train(documents []string) error {
	if len(documents) == 0 {
		return fmt.Errorf("no documents provided for training")
	}

	model.vocabulary = make(map[string]int)
	model.idf = make(map[string]float64)
	model.documents = make([]map[string]float64, len(documents))

	// Build vocabulary and calculate term frequencies
	termDocCount := make(map[string]int)
	
	for docIdx, doc := range documents {
		words := model.tokenize(doc)
		wordCount := make(map[string]int)
		model.documents[docIdx] = make(map[string]float64)

		// Count term frequencies in this document
		for _, word := range words {
			wordCount[word]++
			if _, exists := model.vocabulary[word]; !exists {
				model.vocabulary[word] = len(model.vocabulary)
			}
		}

		// Calculate TF scores for this document
		maxFreq := 0
		for _, freq := range wordCount {
			if freq > maxFreq {
				maxFreq = freq
			}
		}

		for word, freq := range wordCount {
			// Normalized TF: freq / max_freq
			tf := float64(freq) / float64(maxFreq)
			model.documents[docIdx][word] = tf

			// Track document frequency for IDF calculation
			termDocCount[word]++
		}
	}

	// Calculate IDF scores
	totalDocs := float64(len(documents))
	for term, docFreq := range termDocCount {
		model.idf[term] = math.Log(totalDocs / float64(docFreq))
	}

	model.trained = true
	return nil
}

// Similarity calculates TF-IDF cosine similarity between two texts
func (model *TFIDFModel) Similarity(text1, text2 string) float64 {
	if !model.trained {
		return 0.0
	}

	vec1 := model.textToTFIDFVector(text1)
	vec2 := model.textToTFIDFVector(text2)

	return model.cosineSimilarity(vec1, vec2)
}

// tokenize splits text into normalized tokens
func (model *TFIDFModel) tokenize(text string) []string {
	// Simple tokenization - in production, use more sophisticated NLP
	words := strings.Fields(strings.ToLower(text))
	var tokens []string
	
	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:()[]{}\"'")
		if len(word) > 2 { // Skip very short words
			tokens = append(tokens, word)
		}
	}
	
	return tokens
}

// textToTFIDFVector converts text to TF-IDF vector
func (model *TFIDFModel) textToTFIDFVector(text string) map[string]float64 {
	words := model.tokenize(text)
	wordCount := make(map[string]int)
	vector := make(map[string]float64)

	// Count word frequencies
	for _, word := range words {
		wordCount[word]++
	}

	// Find max frequency for normalization
	maxFreq := 0
	for _, freq := range wordCount {
		if freq > maxFreq {
			maxFreq = freq
		}
	}

	// Calculate TF-IDF for each term
	for word, freq := range wordCount {
		if _, exists := model.vocabulary[word]; exists {
			tf := float64(freq) / float64(maxFreq)
			idf := model.idf[word]
			vector[word] = tf * idf
		}
	}

	return vector
}

// cosineSimilarity calculates cosine similarity between sparse vectors
func (model *TFIDFModel) cosineSimilarity(vec1, vec2 map[string]float64) float64 {
	var dotProduct, norm1, norm2 float64

	// Calculate dot product and norms
	for term, val1 := range vec1 {
		norm1 += val1 * val1
		if val2, exists := vec2[term]; exists {
			dotProduct += val1 * val2
		}
	}

	for _, val2 := range vec2 {
		norm2 += val2 * val2
	}

	if norm1 == 0 || norm2 == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

// LocalEmbeddingModel provides simple local embedding functionality
type LocalEmbeddingModel struct {
	model map[string][]float64 // Word vectors
	dim   int                  // Embedding dimension
}

// NewLocalEmbeddingModel creates a new local embedding model
func NewLocalEmbeddingModel(dimension int) *LocalEmbeddingModel {
	lem := &LocalEmbeddingModel{
		model: make(map[string][]float64),
		dim:   dimension,
	}

	// Initialize with some basic embeddings for common words
	lem.initializeBasicVocabulary()
	
	return lem
}

// GetEmbedding generates embedding for text using simple averaging
func (lem *LocalEmbeddingModel) GetEmbedding(text string) ([]float64, error) {
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return make([]float64, lem.dim), nil
	}

	embedding := make([]float64, lem.dim)
	validWords := 0

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:()[]{}\"'")
		if vec, exists := lem.model[word]; exists {
			for i, val := range vec {
				if i < len(embedding) {
					embedding[i] += val
				}
			}
			validWords++
		}
	}

	// Average the vectors
	if validWords > 0 {
		for i := range embedding {
			embedding[i] /= float64(validWords)
		}
	}

	// Normalize
	norm := 0.0
	for _, val := range embedding {
		norm += val * val
	}
	norm = math.Sqrt(norm)

	if norm > 0 {
		for i := range embedding {
			embedding[i] /= norm
		}
	}

	return embedding, nil
}

// initializeBasicVocabulary creates simple embeddings for common words
func (lem *LocalEmbeddingModel) initializeBasicVocabulary() {
	// Create simple semantic clusters using random but consistent vectors
	clusters := map[string][]string{
		"programming": {"code", "program", "develop", "implement", "debug", "software", "algorithm", "api", "rest"},
		"research":    {"research", "study", "investigate", "analyze", "explore", "examine", "find", "trends", "behavior", "market"},
		"ai_ml":       {"machine", "learning", "neural", "network", "model", "training", "classification", "intelligence", "artificial", "deep"},
		"data":        {"data", "analyze", "statistics", "chart", "graph", "visualize", "metrics", "pipeline", "feature", "engineering"},
		"help":        {"help", "assist", "support", "guide", "explain", "teach", "show"},
		"create":      {"create", "build", "make", "generate", "produce", "construct", "design"},
	}

	// Generate consistent vectors for each cluster
	for clusterName, words := range clusters {
		baseVector := lem.generateSeededVector(clusterName)
		
		for i, word := range words {
			// Slight variations around the base vector
			vector := make([]float64, lem.dim)
			copy(vector, baseVector)
			
			// Add small perturbations
			for j := range vector {
				perturbation := 0.1 * math.Sin(float64(i+j)) // Deterministic perturbation
				vector[j] += perturbation
			}
			
			lem.normalizeVector(vector)
			lem.model[word] = vector
		}
	}
}

// generateSeededVector creates a deterministic vector based on a seed string
func (lem *LocalEmbeddingModel) generateSeededVector(seed string) []float64 {
	vector := make([]float64, lem.dim)
	
	// Use seed string to generate deterministic values
	hash := 0
	for _, char := range seed {
		hash = hash*31 + int(char)
	}

	for i := range vector {
		// Generate deterministic pseudo-random values
		hash = hash*1103515245 + 12345
		vector[i] = float64(hash%1000-500) / 500.0 // Values between -1 and 1
	}

	lem.normalizeVector(vector)
	return vector
}

// normalizeVector normalizes a vector to unit length
func (lem *LocalEmbeddingModel) normalizeVector(vector []float64) {
	norm := 0.0
	for _, val := range vector {
		norm += val * val
	}
	norm = math.Sqrt(norm)

	if norm > 0 {
		for i := range vector {
			vector[i] /= norm
		}
	}
}

// calculateDomainBoost provides domain-specific boosting for specialized personas
func (sm *SemanticMatcher) calculateDomainBoost(description string, persona *PersonaConfig) float64 {
	lowerDesc := strings.ToLower(description)
	
	// Define domain-specific keywords with weights
	domainKeywords := map[string]map[string]float64{
		"ai_specialist": {"machine": 0.1, "learning": 0.1, "neural": 0.1, "network": 0.08, "model": 0.08, "training": 0.08, "classification": 0.08, "intelligence": 0.1, "artificial": 0.1, "deep": 0.08},
		"researcher":    {"research": 0.1, "study": 0.08, "investigate": 0.08, "analyze": 0.08, "explore": 0.06, "examine": 0.06, "find": 0.05, "trends": 0.08, "behavior": 0.08, "market": 0.08, "data": 0.08},
		"coder":         {"code": 0.1, "program": 0.08, "develop": 0.08, "implement": 0.1, "debug": 0.08, "software": 0.08, "api": 0.08, "rest": 0.05, "function": 0.08, "error": 0.05, "handling": 0.05, "algorithm": 0.1, "binary": 0.08, "search": 0.06},
	}
	
	keywords, exists := domainKeywords[persona.ID]
	if !exists {
		return 0.0
	}
	
	boost := 0.0
	for keyword, weight := range keywords {
		if strings.Contains(lowerDesc, keyword) {
			boost += weight // Use weighted boost per matching domain keyword
		}
	}
	
	// Cap the boost at 0.3 to avoid overwhelming other factors
	return math.Min(boost, 0.3)
}