package personas

import (
	"math"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// KeywordMatcher handles keyword-based persona matching
type KeywordMatcher struct {
	synonyms  map[string][]string
	negations []string
	stemmer   Stemmer
	stopWords map[string]bool
	logger    *zap.Logger
}

// NewKeywordMatcher creates a new keyword matcher
func NewKeywordMatcher(logger *zap.Logger) *KeywordMatcher {
	return &KeywordMatcher{
		synonyms:  initSynonyms(),
		negations: initNegations(),
		stemmer:   NewSimpleStemmer(),
		stopWords: initStopWords(),
		logger:    logger,
	}
}

// CalculateScore calculates the keyword matching score for a description against persona keywords
func (km *KeywordMatcher) CalculateScore(description string, keywords []string) float64 {
	if len(keywords) == 0 {
		return 0.0
	}

	startTime := time.Now()
	defer func() {
		if km.logger != nil {
			km.logger.Debug("Keyword matching completed",
				zap.Duration("duration", time.Since(startTime)),
				zap.Int("keyword_count", len(keywords)))
		}
	}()

	// Preprocess description text
	processedDesc := km.preprocessText(description)

	// Check for negation words that would disqualify this persona
	if km.containsNegation(processedDesc, keywords) {
		return 0.0
	}

	totalScore := 0.0
	maxPossibleScore := 0.0

	for _, keyword := range keywords {
		weight := km.getKeywordWeight(keyword)
		maxPossibleScore += weight

		if km.matchesKeyword(processedDesc, keyword) {
			totalScore += weight
		} else if synonymScore := km.matchesSynonyms(processedDesc, keyword); synonymScore > 0 {
			totalScore += synonymScore * weight
		}
	}

	if maxPossibleScore == 0 {
		return 0.0
	}

	score := totalScore / maxPossibleScore

	// Apply boost for multiple keyword matches
	matchCount := km.countMatches(processedDesc, keywords)
	if matchCount > 1 {
		boost := 1.0 + (float64(matchCount-1) * 0.1) // 10% boost per additional match
		score = math.Min(score*boost, 1.0)
	}

	return score
}

// preprocessText preprocesses text for matching
func (km *KeywordMatcher) preprocessText(text string) []string {
	// Convert to lowercase and split into words
	words := strings.Fields(strings.ToLower(text))
	var processed []string

	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:()[]{}\"'")

		// Skip empty words
		if word == "" {
			continue
		}

		// Skip stop words
		if km.stopWords[word] {
			continue
		}

		// Apply stemming
		stemmed := km.stemmer.Stem(word)
		processed = append(processed, stemmed)
	}

	return processed
}

// containsNegation checks if the text contains negation patterns that disqualify keywords
func (km *KeywordMatcher) containsNegation(processedWords []string, keywords []string) bool {
	wordSet := make(map[string]bool)
	for _, word := range processedWords {
		wordSet[word] = true
	}

	// Check for explicit negations
	for _, negation := range km.negations {
		if wordSet[negation] {
			// Check if negation is near any of our keywords
			for _, keyword := range keywords {
				stemmedKeyword := km.stemmer.Stem(strings.ToLower(keyword))
				if wordSet[stemmedKeyword] {
					return true
				}
			}
		}
	}

	// Check for negation patterns like "not coding" or "don't search"
	text := strings.Join(processedWords, " ")
	for _, keyword := range keywords {
		stemmedKeyword := km.stemmer.Stem(strings.ToLower(keyword))

		negationPatterns := []string{
			"not " + stemmedKeyword,
			"don't " + stemmedKeyword,
			"won't " + stemmedKeyword,
			"can't " + stemmedKeyword,
			"never " + stemmedKeyword,
		}

		for _, pattern := range negationPatterns {
			if strings.Contains(text, pattern) {
				return true
			}
		}
	}

	return false
}

// matchesKeyword checks if processed words contain the keyword
func (km *KeywordMatcher) matchesKeyword(processedWords []string, keyword string) bool {
	stemmedKeyword := km.stemmer.Stem(strings.ToLower(keyword))

	for _, word := range processedWords {
		if word == stemmedKeyword {
			return true
		}
		// Also check for partial matches for compound keywords
		if strings.Contains(word, stemmedKeyword) || strings.Contains(stemmedKeyword, word) {
			if len(word) > 3 && len(stemmedKeyword) > 3 { // Only for longer words
				return true
			}
		}
	}

	return false
}

// matchesSynonyms checks if the text matches any synonyms of the keyword
func (km *KeywordMatcher) matchesSynonyms(processedWords []string, keyword string) float64 {
	synonyms, exists := km.synonyms[strings.ToLower(keyword)]
	if !exists {
		return 0.0
	}

	bestMatch := 0.0
	wordSet := make(map[string]bool)
	for _, word := range processedWords {
		wordSet[word] = true
	}

	for _, synonym := range synonyms {
		stemmedSynonym := km.stemmer.Stem(strings.ToLower(synonym))
		if wordSet[stemmedSynonym] {
			// Synonym matches are worth less than direct matches
			bestMatch = math.Max(bestMatch, 0.8)
		}
	}

	return bestMatch
}

// countMatches counts the number of keyword matches
func (km *KeywordMatcher) countMatches(processedWords []string, keywords []string) int {
	count := 0
	wordSet := make(map[string]bool)
	for _, word := range processedWords {
		wordSet[word] = true
	}

	for _, keyword := range keywords {
		stemmedKeyword := km.stemmer.Stem(strings.ToLower(keyword))
		if wordSet[stemmedKeyword] {
			count++
		}
	}

	return count
}

// getKeywordWeight calculates the weight of a keyword based on its characteristics
func (km *KeywordMatcher) getKeywordWeight(keyword string) float64 {
	baseWeight := 1.0

	// Longer, more specific keywords get higher weight
	lengthBonus := float64(len(keyword)) / 20.0 // Max bonus of ~0.5 for 10-char words

	// Multi-word keywords (more specific) get bonus
	wordCount := len(strings.Fields(keyword))
	specificityBonus := float64(wordCount-1) * 0.3

	// Technical terms get slightly higher weight
	if km.isTechnicalTerm(keyword) {
		return baseWeight + lengthBonus + specificityBonus + 0.2
	}

	return baseWeight + lengthBonus + specificityBonus
}

// isTechnicalTerm checks if a keyword is a technical term
func (km *KeywordMatcher) isTechnicalTerm(keyword string) bool {
	technicalTerms := map[string]bool{
		"debug":      true,
		"implement":  true,
		"algorithm":  true,
		"database":   true,
		"api":        true,
		"analyze":    true,
		"visualize":  true,
		"statistics": true,
		"research":   true,
		"compile":    true,
		"deploy":     true,
		"optimize":   true,
	}

	return technicalTerms[strings.ToLower(keyword)]
}

// SimpleStemmer provides basic word stemming
type SimpleStemmer struct {
	suffixes []string
}

// NewSimpleStemmer creates a new simple stemmer
func NewSimpleStemmer() *SimpleStemmer {
	return &SimpleStemmer{
		suffixes: []string{
			"ing", "ed", "er", "est", "ly", "tion", "sion", "ness", "ment", "able", "ible",
		},
	}
}

// Stem applies basic stemming to a word
func (s *SimpleStemmer) Stem(word string) string {
	if len(word) <= 3 {
		return word
	}

	// Sort suffixes by length (longest first) for better matching
	suffixes := make([]string, len(s.suffixes))
	copy(suffixes, s.suffixes)
	sort.Slice(suffixes, func(i, j int) bool {
		return len(suffixes[i]) > len(suffixes[j])
	})

	for _, suffix := range suffixes {
		if strings.HasSuffix(word, suffix) && len(word) > len(suffix)+2 {
			return word[:len(word)-len(suffix)]
		}
	}

	return word
}

// initSynonyms initializes the synonym dictionary
func initSynonyms() map[string][]string {
	return map[string][]string{
		"search":    {"find", "lookup", "query", "seek", "discover"},
		"research":  {"investigate", "study", "explore", "examine", "analyze"},
		"code":      {"program", "develop", "build", "create", "implement"},
		"debug":     {"fix", "troubleshoot", "resolve", "repair"},
		"analyze":   {"examine", "study", "review", "evaluate", "assess"},
		"write":     {"create", "compose", "draft", "author"},
		"calculate": {"compute", "determine", "figure", "solve"},
		"visualize": {"chart", "graph", "plot", "display", "show"},
		"test":      {"verify", "check", "validate", "examine"},
		"optimize":  {"improve", "enhance", "refine", "tune"},
		"implement": {"build", "create", "develop", "construct"},
		"design":    {"plan", "architect", "blueprint", "model"},
	}
}

// initNegations initializes the negation words list
func initNegations() []string {
	return []string{
		"not", "no", "never", "none", "nothing", "nowhere", "nobody",
		"don't", "doesn't", "didn't", "won't", "wouldn't", "can't",
		"cannot", "couldn't", "shouldn't", "mustn't", "needn't",
		"avoid", "prevent", "stop", "exclude", "skip", "ignore",
	}
}

// initStopWords initializes the stop words set
func initStopWords() map[string]bool {
	stopWords := []string{
		"a", "an", "and", "are", "as", "at", "be", "by", "for", "from",
		"has", "he", "in", "is", "it", "its", "of", "on", "that", "the",
		"to", "was", "will", "with", "the", "this", "but", "they", "have",
		"had", "what", "said", "each", "which", "she", "do", "how", "their",
		"if", "up", "out", "many", "then", "them", "these", "so", "some", "her",
		"would", "make", "like", "into", "him", "time", "two", "more", "go", "no",
		"way", "could", "my", "than", "first", "been", "call", "who", "oil", "sit",
		"now", "find", "down", "day", "did", "get", "may", "part", "over", "new",
		"sound", "take", "only", "little", "work", "know", "place", "year", "live",
		"me", "back", "give", "most", "very", "after", "thing", "our", "just", "name",
		"good", "sentence", "man", "think", "say", "great", "where", "help", "through",
		"much", "before", "line", "right", "too", "mean", "old", "any", "same", "tell",
		"boy", "follow", "came", "want", "show", "also", "around", "form", "three",
		"small", "set", "put", "end", "why", "again", "turn", "here", "off", "went",
		"come", "about", "need", "should", "home", "house", "picture", "try", "us",
		"again", "animal", "point", "mother", "world", "near", "build", "self",
		"earth", "father", "head", "stand", "own", "page", "should", "country",
		"found", "answer", "school", "grow", "study", "still", "learn", "plant",
		"cover", "food", "sun", "four", "between", "state", "keep", "eye", "never",
		"last", "let", "thought", "city", "tree", "cross", "farm", "hard", "start",
		"might", "story", "saw", "far", "sea", "draw", "left", "late", "run",
		"while", "press", "close", "night", "real", "life", "few", "north", "book",
		"carry", "took", "science", "eat", "room", "friend", "began", "idea", "fish",
		"mountain", "stop", "once", "base", "hear", "horse", "cut", "sure", "watch",
		"color", "face", "wood", "main", "open", "seem", "together", "next", "white",
		"children", "begin", "got", "walk", "example", "ease", "paper", "group",
		"always", "music", "those", "both", "mark", "often", "letter", "until",
		"mile", "river", "car", "feet", "care", "second", "enough", "plain", "girl",
		"usual", "young", "ready", "above", "ever", "red", "list", "though", "feel",
		"talk", "bird", "soon", "body", "dog", "family", "direct", "pose", "leave",
		"song", "measure", "door", "product", "black", "short", "numeral", "class",
		"wind", "question", "happen", "complete", "ship", "area", "half", "rock",
		"order", "fire", "south", "problem", "piece", "told", "knew", "pass", "since",
		"top", "whole", "king", "space", "heard", "best", "hour", "better", "during",
		"hundred", "five", "remember", "step", "early", "hold", "west", "ground",
		"interest", "reach", "fast", "verb", "sing", "listen", "six", "table",
		"travel", "less", "morning", "ten", "simple", "several", "vowel", "toward",
		"war", "lay", "against", "pattern", "slow", "center", "love", "person",
		"money", "serve", "appear", "road", "map", "rain", "rule", "govern", "pull",
		"cold", "notice", "voice", "unit", "power", "town", "fine", "certain", "fly",
		"fall", "lead", "cry", "dark", "machine", "note", "wait", "plan", "figure",
		"star", "box", "noun", "field", "rest", "correct", "able", "pound", "done",
		"beauty", "drive", "stood", "contain", "front", "teach", "week", "final",
		"gave", "green", "oh", "quick", "develop", "ocean", "warm", "free", "minute",
		"strong", "special", "mind", "behind", "clear", "tail", "produce", "fact",
		"street", "inch", "multiply", "nothing", "course", "stay", "wheel", "full",
		"force", "blue", "object", "decide", "surface", "deep", "moon", "island",
		"foot", "system", "busy", "test", "record", "boat", "common", "gold",
		"possible", "plane", "stead", "dry", "wonder", "laugh", "thousands", "ago",
		"ran", "check", "game", "shape", "equate", "hot", "miss", "brought", "heat",
		"snow", "tire", "bring", "yes", "distant", "fill", "east", "paint", "language",
		"among", "grand", "ball", "yet", "wave", "drop", "heart", "am", "present",
		"heavy", "dance", "engine", "position", "arm", "wide", "sail", "material",
		"size", "vary", "settle", "speak", "weight", "general", "ice", "matter",
		"circle", "pair", "include", "divide", "syllable", "felt", "perhaps", "pick",
		"sudden", "count", "square", "reason", "length", "represent", "art", "subject",
		"region", "energy", "hunt", "probable", "bed", "brother", "egg", "ride",
		"cell", "believe", "fraction", "forest", "sit", "race", "window", "store",
		"summer", "train", "sleep", "prove", "lone", "leg", "exercise", "wall",
		"catch", "mount", "wish", "sky", "board", "joy", "winter", "sat", "written",
		"wild", "instrument", "kept", "glass", "grass", "cow", "job", "edge", "sign",
		"visit", "past", "soft", "fun", "bright", "gas", "weather", "month", "million",
		"bear", "finish", "happy", "hope", "flower", "clothe", "strange", "gone",
		"jump", "baby", "eight", "village", "meet", "root", "buy", "raise", "solve",
		"metal", "whether", "push", "seven", "paragraph", "third", "shall", "held",
		"hair", "describe", "cook", "floor", "either", "result", "burn", "hill",
		"safe", "cat", "century", "consider", "type", "law", "bit", "coast", "copy",
		"phrase", "silent", "tall", "sand", "soil", "roll", "temperature", "finger",
		"industry", "value", "fight", "lie", "beat", "excite", "natural", "view",
		"sense", "ear", "else", "quite", "broke", "case", "middle", "kill", "son",
		"lake", "moment", "scale", "loud", "spring", "observe", "child", "straight",
		"consonant", "nation", "dictionary", "milk", "speed", "method", "organ",
		"pay", "age", "section", "dress", "cloud", "surprise", "quiet", "stone",
		"tiny", "climb", "bad", "oil", "blood", "touch", "grew", "cent", "mix",
		"team", "wire", "cost", "lost", "brown", "wear", "garden", "equal", "sent",
		"choose", "fell", "fit", "flow", "fair", "bank", "collect", "save", "control",
		"decimal", "gentle", "woman", "captain", "practice", "separate", "difficult",
		"doctor", "please", "protect", "noon", "whose", "locate", "ring", "character",
		"insect", "caught", "period", "indicate", "radio", "spoke", "atom", "human",
		"history", "effect", "electric", "expect", "crop", "modern", "element", "hit",
		"student", "corner", "party", "supply", "bone", "rail", "imagine", "provide",
		"agree", "thus", "capital", "won't", "chair", "danger", "fruit", "rich",
		"thick", "soldier", "process", "operate", "guess", "necessary", "sharp",
		"wing", "create", "neighbor", "wash", "bat", "rather", "crowd", "corn",
		"compare", "poem", "string", "bell", "depend", "meat", "rub", "tube",
		"famous", "dollar", "stream", "fear", "sight", "thin", "triangle", "planet",
		"hurry", "chief", "colony", "clock", "mine", "tie", "enter", "major", "fresh",
		"search", "send", "yellow", "gun", "allow", "print", "dead", "spot", "desert",
		"suit", "current", "lift", "rose", "continue", "block", "chart", "hat",
		"sell", "success", "company", "subtract", "event", "particular", "deal",
		"swim", "term", "opposite", "wife", "shoe", "shoulder", "spread", "arrange",
		"camp", "invent", "cotton", "born", "determine", "quart", "nine", "truck",
		"noise", "level", "chance", "gather", "shop", "stretch", "throw", "shine",
		"property", "column", "molecule", "select", "wrong", "gray", "repeat", "require",
		"broad", "prepare", "salt", "nose", "plural", "anger", "claim", "continent",
		"oxygen", "sugar", "death", "pretty", "skill", "women", "season", "solution",
		"magnet", "silver", "thank", "branch", "match", "suffix", "especially", "fig",
		"afraid", "huge", "sister", "steel", "discuss", "forward", "similar", "guide",
		"experience", "score", "apple", "bought", "led", "pitch", "coat", "mass",
		"card", "band", "rope", "slip", "win", "dream", "evening", "condition", "feed",
		"tool", "total", "basic", "smell", "valley", "nor", "double", "seat", "arrive",
		"master", "track", "parent", "shore", "division", "sheet", "substance", "favor",
		"connect", "post", "spend", "chord", "fat", "glad", "original", "share",
		"station", "dad", "bread", "charge", "proper", "bar", "offer", "segment",
		"slave", "duck", "instant", "market", "degree", "populate", "chick", "dear",
		"enemy", "reply", "drink", "occur", "support", "speech", "nature", "range",
		"steam", "motion", "path", "liquid", "log", "meant", "quotient", "teeth",
		"shell", "neck",
	}

	stopWordsMap := make(map[string]bool)
	for _, word := range stopWords {
		stopWordsMap[word] = true
	}
	return stopWordsMap
}
