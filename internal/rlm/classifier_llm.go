package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rand/recurse/internal/rlm/meta"
)

// LLMClassifier provides LLM-based task classification for ambiguous queries.
// Used as a fallback when rule-based classification has low confidence.
type LLMClassifier struct {
	client meta.LLMClient

	// Cache for classification results
	cacheMu sync.RWMutex
	cache   map[string]cachedClassification
}

type cachedClassification struct {
	result    Classification
	timestamp time.Time
}

const (
	// LLM classification cache TTL
	classificationCacheTTL = 10 * time.Minute

	// Maximum tokens for LLM classification call
	classificationMaxTokens = 200
)

// NewLLMClassifier creates a new LLM-based classifier.
func NewLLMClassifier(client meta.LLMClient) *LLMClassifier {
	return &LLMClassifier{
		client: client,
		cache:  make(map[string]cachedClassification),
	}
}

// llmClassificationResponse is the expected JSON response from the LLM.
type llmClassificationResponse struct {
	TaskType   string  `json:"task_type"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// Classify uses the LLM to classify an ambiguous query.
// It returns the classification or an error if the LLM call fails.
func (c *LLMClassifier) Classify(ctx context.Context, query string, ruleBasedHint *Classification) (Classification, error) {
	if c.client == nil {
		return Classification{Type: TaskTypeUnknown}, fmt.Errorf("LLM client not available")
	}

	// Check cache first
	cacheKey := normalizeQuery(query)
	if cached, ok := c.getFromCache(cacheKey); ok {
		return cached, nil
	}

	// Build prompt
	prompt := c.buildClassificationPrompt(query, ruleBasedHint)

	// Call LLM
	response, err := c.client.Complete(ctx, prompt, classificationMaxTokens)
	if err != nil {
		return Classification{Type: TaskTypeUnknown}, fmt.Errorf("LLM classification failed: %w", err)
	}

	// Parse response
	classification, err := c.parseResponse(response)
	if err != nil {
		return Classification{Type: TaskTypeUnknown}, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Add signal indicating LLM was used
	classification.Signals = append(classification.Signals, "source:llm_fallback")
	if ruleBasedHint != nil {
		classification.Signals = append(classification.Signals,
			fmt.Sprintf("rule_hint:%s@%.0f%%", ruleBasedHint.Type, ruleBasedHint.Confidence*100))
	}

	// Cache the result
	c.putInCache(cacheKey, classification)

	return classification, nil
}

// buildClassificationPrompt constructs the prompt for LLM classification.
func (c *LLMClassifier) buildClassificationPrompt(query string, hint *Classification) string {
	var hintText string
	if hint != nil && hint.Type != TaskTypeUnknown {
		hintText = fmt.Sprintf(`
The rule-based classifier suggests this might be a "%s" task with %.0f%% confidence,
but this confidence is too low for automatic selection. Please provide your assessment.
Signals detected: %s
`, hint.Type, hint.Confidence*100, strings.Join(hint.Signals, ", "))
	}

	return fmt.Sprintf(`Classify this query into one of four task types for optimal LLM execution mode selection.

TASK TYPES:
1. "computational" - Requires exact computation: counting, summing, averaging, pattern matching, aggregation
   - Example: "How many times does 'apple' appear?" → computational (code can count exactly)
   - Example: "What is the total sales?" → computational (code can sum reliably)

2. "retrieval" - Requires finding specific information: locating facts, codes, names, quotes
   - Example: "What is the secret code?" → retrieval (find and return specific value)
   - Example: "Who is the CEO?" → retrieval (locate specific named entity)

3. "analytical" - Requires reasoning about relationships: comparisons, cause-effect, connections
   - Example: "Did Alice work with Bob?" → analytical (requires relationship reasoning)
   - Example: "Why did revenue decline?" → analytical (requires causal reasoning)

4. "transformational" - Requires reformatting or restructuring: summarizing, translating, rewriting
   - Example: "Summarize this document" → transformational
   - Example: "Convert to JSON" → transformational

QUERY TO CLASSIFY:
"%s"
%s
Respond with ONLY a JSON object (no markdown, no explanation outside JSON):
{"task_type": "<type>", "confidence": <0.0-1.0>, "reasoning": "<brief explanation>"}`, query, hintText)
}

// parseResponse extracts the classification from the LLM response.
func (c *LLMClassifier) parseResponse(response string) (Classification, error) {
	// Clean the response - remove any markdown formatting
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Try to find JSON in the response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return Classification{Type: TaskTypeUnknown}, fmt.Errorf("no JSON found in response")
	}
	response = response[start : end+1]

	var llmResponse llmClassificationResponse
	if err := json.Unmarshal([]byte(response), &llmResponse); err != nil {
		return Classification{Type: TaskTypeUnknown}, fmt.Errorf("JSON parse error: %w", err)
	}

	// Map string to TaskType
	taskType := c.mapTaskType(llmResponse.TaskType)

	// Validate confidence
	confidence := llmResponse.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	return Classification{
		Type:       taskType,
		Confidence: confidence,
		Signals:    []string{"reasoning:" + llmResponse.Reasoning},
	}, nil
}

// mapTaskType converts string to TaskType, handling variations.
func (c *LLMClassifier) mapTaskType(s string) TaskType {
	s = strings.ToLower(strings.TrimSpace(s))

	switch s {
	case "computational", "compute", "calculation":
		return TaskTypeComputational
	case "retrieval", "retrieve", "lookup", "find":
		return TaskTypeRetrieval
	case "analytical", "analysis", "reasoning":
		return TaskTypeAnalytical
	case "transformational", "transform", "transformation", "reformat":
		return TaskTypeTransformational
	default:
		return TaskTypeUnknown
	}
}

// getFromCache retrieves a cached classification if valid.
func (c *LLMClassifier) getFromCache(key string) (Classification, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	cached, ok := c.cache[key]
	if !ok {
		return Classification{}, false
	}

	// Check if cache entry is still valid
	if time.Since(cached.timestamp) > classificationCacheTTL {
		return Classification{}, false
	}

	result := cached.result
	result.Signals = append(result.Signals, "source:cache")
	return result, true
}

// putInCache stores a classification result.
func (c *LLMClassifier) putInCache(key string, result Classification) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.cache[key] = cachedClassification{
		result:    result,
		timestamp: time.Now(),
	}

	// Simple cache cleanup - remove expired entries if cache is large
	if len(c.cache) > 1000 {
		c.cleanupCacheLocked()
	}
}

// cleanupCacheLocked removes expired entries. Must be called with lock held.
func (c *LLMClassifier) cleanupCacheLocked() {
	now := time.Now()
	for key, cached := range c.cache {
		if now.Sub(cached.timestamp) > classificationCacheTTL {
			delete(c.cache, key)
		}
	}
}

// normalizeQuery creates a cache key from a query.
func normalizeQuery(query string) string {
	// Lowercase and trim whitespace for consistent caching
	return strings.ToLower(strings.TrimSpace(query))
}

// ClearCache clears the classification cache.
func (c *LLMClassifier) ClearCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.cache = make(map[string]cachedClassification)
}
