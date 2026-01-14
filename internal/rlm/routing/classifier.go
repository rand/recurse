package routing

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// CategoryClassifier classifies tasks into categories for routing.
type CategoryClassifier struct {
	mu sync.RWMutex

	// Cache for repeated patterns
	cache     map[string]classificationResult
	cacheSize int

	// Statistics
	heuristicHits int64
	cachHits      int64
	totalCalls    int64
}

type classificationResult struct {
	category   TaskCategory
	confidence float64
}

// ClassifierConfig configures the category classifier.
type ClassifierConfig struct {
	// CacheSize is the maximum cache entries (default 1000).
	CacheSize int

	// MinHeuristicConfidence is the minimum confidence for heuristic classification.
	// Below this, LLM fallback would be used (default 0.85).
	MinHeuristicConfidence float64
}

// NewCategoryClassifier creates a new category classifier.
func NewCategoryClassifier(cfg ClassifierConfig) *CategoryClassifier {
	cacheSize := cfg.CacheSize
	if cacheSize <= 0 {
		cacheSize = 1000
	}

	return &CategoryClassifier{
		cache:     make(map[string]classificationResult),
		cacheSize: cacheSize,
	}
}

// Classify determines the task category with confidence score.
func (c *CategoryClassifier) Classify(ctx context.Context, query string) (TaskCategory, float64) {
	c.mu.Lock()
	c.totalCalls++
	c.mu.Unlock()

	// Check cache first
	cacheKey := normalizeForCache(query)
	c.mu.RLock()
	if result, ok := c.cache[cacheKey]; ok {
		c.mu.RUnlock()
		c.mu.Lock()
		c.cachHits++
		c.mu.Unlock()
		return result.category, result.confidence
	}
	c.mu.RUnlock()

	// Heuristic classification
	category, confidence := c.classifyHeuristic(query)

	c.mu.Lock()
	c.heuristicHits++

	// Cache result
	if len(c.cache) < c.cacheSize {
		c.cache[cacheKey] = classificationResult{category, confidence}
	}
	c.mu.Unlock()

	return category, confidence
}

// classifyHeuristic uses keyword patterns for fast classification.
func (c *CategoryClassifier) classifyHeuristic(query string) (TaskCategory, float64) {
	queryLower := strings.ToLower(query)
	queryLen := len(query)

	// Score each category
	scores := make(map[TaskCategory]float64)

	// Simple category - direct questions, lookups
	simplePatterns := []string{
		"what is", "what's", "define", "meaning of",
		"how many", "when was", "who is", "where is",
		"list", "name", "tell me",
	}
	scores[CategorySimple] = matchScore(queryLower, simplePatterns, 0.9)

	// Reasoning category - logic, math, proofs
	reasoningPatterns := []string{
		"prove", "theorem", "derive", "calculate",
		"logic", "why does", "reason", "deduce",
		"if.*then", "therefore", "implies", "conclusion",
		"step by step", "mathematical",
	}
	scores[CategoryReasoning] = matchScore(queryLower, reasoningPatterns, 0.95)

	// Coding category - code generation, debugging
	codingPatterns := []string{
		"code", "function", "implement", "bug", "error",
		"compile", "test", "debug", "refactor",
		"class", "method", "variable", "algorithm",
		"```", "syntax", "programming",
	}
	scores[CategoryCoding] = matchScore(queryLower, codingPatterns, 0.95)

	// Check for actual code blocks
	if hasCodeBlock(query) {
		scores[CategoryCoding] = max(scores[CategoryCoding], 0.98)
	}

	// Creative category - writing, design, brainstorming
	creativePatterns := []string{
		"write", "create", "design", "brainstorm",
		"story", "poem", "essay", "creative",
		"imagine", "invent", "compose", "draft",
	}
	scores[CategoryCreative] = matchScore(queryLower, creativePatterns, 0.9)

	// Analysis category - summarization, evaluation
	analysisPatterns := []string{
		"analyze", "summarize", "evaluate", "assess",
		"compare", "contrast", "review", "critique",
		"examine", "interpret", "breakdown",
	}
	scores[CategoryAnalysis] = matchScore(queryLower, analysisPatterns, 0.9)

	// Conversation category - clarification, follow-up
	conversationPatterns := []string{
		"can you", "could you", "would you",
		"please", "thanks", "help me",
		"i don't understand", "what do you mean",
		"clarify", "explain more",
	}
	scores[CategoryConversation] = matchScore(queryLower, conversationPatterns, 0.85)

	// Find best category
	bestCategory := CategorySimple
	bestScore := scores[CategorySimple]

	for cat, score := range scores {
		if score > bestScore {
			bestCategory = cat
			bestScore = score
		}
	}

	// Adjust confidence based on query length and specificity
	confidence := bestScore
	if queryLen < 20 {
		confidence *= 0.9 // Short queries are less certain
	}
	if bestScore < 0.5 {
		// No strong match, default to conversation with lower confidence
		bestCategory = CategoryConversation
		confidence = 0.6
	}

	return bestCategory, clamp(confidence, 0, 1)
}

// matchScore calculates how well a query matches a set of patterns.
func matchScore(query string, patterns []string, maxScore float64) float64 {
	matches := 0
	for _, pattern := range patterns {
		if strings.Contains(query, pattern) {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	// Diminishing returns for multiple matches
	score := float64(matches) / float64(len(patterns))
	return min(score*2, 1.0) * maxScore
}

// hasCodeBlock checks if the query contains a code block.
func hasCodeBlock(query string) bool {
	return strings.Contains(query, "```") ||
		strings.Contains(query, "    ") || // Indented code
		regexp.MustCompile(`func\s+\w+|def\s+\w+|class\s+\w+`).MatchString(query)
}

// normalizeForCache creates a cache key from a query.
func normalizeForCache(query string) string {
	// Use first 100 chars lowercase for cache key
	normalized := strings.ToLower(query)
	if len(normalized) > 100 {
		normalized = normalized[:100]
	}
	return normalized
}

// Stats returns classifier statistics.
func (c *CategoryClassifier) Stats() ClassifierStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return ClassifierStats{
		TotalCalls:    c.totalCalls,
		HeuristicHits: c.heuristicHits,
		CacheHits:     c.cachHits,
		CacheSize:     len(c.cache),
	}
}

// ClassifierStats contains classification statistics.
type ClassifierStats struct {
	TotalCalls    int64 `json:"total_calls"`
	HeuristicHits int64 `json:"heuristic_hits"`
	CacheHits     int64 `json:"cache_hits"`
	CacheSize     int   `json:"cache_size"`
}

// CacheHitRate returns the cache hit rate as a percentage.
func (s ClassifierStats) CacheHitRate() float64 {
	if s.TotalCalls == 0 {
		return 0
	}
	return float64(s.CacheHits) / float64(s.TotalCalls) * 100
}

// ClearCache clears the classification cache.
func (c *CategoryClassifier) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]classificationResult)
}
