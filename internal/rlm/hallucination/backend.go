// Package hallucination provides information-theoretic hallucination detection.
package hallucination

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// VerifierBackend is the interface for probability estimation backends.
// [SPEC-08.28]
type VerifierBackend interface {
	// EstimateProbability estimates P(claim is true | context).
	// Returns a probability in [0, 1].
	EstimateProbability(ctx context.Context, claim, context string) (float64, error)

	// BatchEstimate estimates probabilities for multiple claims.
	// Returns probabilities in the same order as claims.
	// [SPEC-08.29]
	BatchEstimate(ctx context.Context, claims []string, context string) ([]float64, error)

	// Name returns the backend name for logging/metrics.
	Name() string
}

// BackendConfig configures a verification backend.
type BackendConfig struct {
	// Type is the backend type: "self", "haiku", "external", "mock".
	Type string

	// Model is the model identifier (for external backends).
	Model string

	// Timeout is the maximum time for a single estimation.
	Timeout time.Duration

	// CacheTTL is how long to cache results.
	// [SPEC-08.30]
	CacheTTL time.Duration

	// MaxBatchSize limits batch estimation size.
	MaxBatchSize int

	// SamplingFallback enables repeated sampling when logprobs unavailable.
	SamplingFallback bool

	// SamplingCount is the number of samples for fallback estimation.
	SamplingCount int
}

// DefaultBackendConfig returns sensible defaults.
func DefaultBackendConfig() BackendConfig {
	return BackendConfig{
		Type:             "self",
		Timeout:          5 * time.Second,
		CacheTTL:         15 * time.Minute,
		MaxBatchSize:     10,
		SamplingFallback: true,
		SamplingCount:    5,
	}
}

// LLMCompleter is the interface for making LLM calls.
// This matches the existing meta.LLMClient interface.
type LLMCompleter interface {
	// Complete sends a prompt and returns the completion.
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// LogprobsCompleter extends LLMCompleter with logprob support.
type LogprobsCompleter interface {
	LLMCompleter

	// CompleteWithLogprobs returns completion with token logprobs.
	CompleteWithLogprobs(ctx context.Context, prompt string, maxTokens int) (string, map[string]float64, error)
}

// CacheEntry stores a cached probability estimate.
type CacheEntry struct {
	Probability float64
	CreatedAt   time.Time
}

// CachingBackend wraps a backend with an LRU cache.
// [SPEC-08.30]
type CachingBackend struct {
	backend VerifierBackend
	ttl     time.Duration
	maxSize int

	mu     sync.RWMutex
	cache  map[string]CacheEntry
	order  []string // LRU order
	hits   int64    // Cache hit count
	misses int64    // Cache miss count
}

// NewCachingBackend creates a caching wrapper.
func NewCachingBackend(backend VerifierBackend, ttl time.Duration, maxSize int) *CachingBackend {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &CachingBackend{
		backend: backend,
		ttl:     ttl,
		maxSize: maxSize,
		cache:   make(map[string]CacheEntry),
		order:   make([]string, 0, maxSize),
	}
}

func (c *CachingBackend) Name() string {
	return fmt.Sprintf("cached(%s)", c.backend.Name())
}

func (c *CachingBackend) EstimateProbability(ctx context.Context, claim, context string) (float64, error) {
	key := c.cacheKey(claim, context)

	// Check cache
	c.mu.RLock()
	if entry, ok := c.cache[key]; ok {
		if time.Since(entry.CreatedAt) < c.ttl {
			c.hits++
			c.mu.RUnlock()
			return entry.Probability, nil
		}
	}
	c.mu.RUnlock()

	// Cache miss - call backend
	c.mu.Lock()
	c.misses++
	c.mu.Unlock()

	prob, err := c.backend.EstimateProbability(ctx, claim, context)
	if err != nil {
		return 0, err
	}

	// Store in cache
	c.mu.Lock()
	c.set(key, prob)
	c.mu.Unlock()

	return prob, nil
}

func (c *CachingBackend) BatchEstimate(ctx context.Context, claims []string, context string) ([]float64, error) {
	results := make([]float64, len(claims))
	var uncached []int // Indices of uncached claims
	var uncachedClaims []string
	var batchHits int64

	// Check cache for each claim
	c.mu.RLock()
	for i, claim := range claims {
		key := c.cacheKey(claim, context)
		if entry, ok := c.cache[key]; ok && time.Since(entry.CreatedAt) < c.ttl {
			results[i] = entry.Probability
			batchHits++
		} else {
			uncached = append(uncached, i)
			uncachedClaims = append(uncachedClaims, claim)
		}
	}
	c.mu.RUnlock()

	// Update hit/miss counters
	c.mu.Lock()
	c.hits += batchHits
	c.misses += int64(len(uncached))
	c.mu.Unlock()

	// If all cached, return early
	if len(uncached) == 0 {
		return results, nil
	}

	// Batch estimate uncached claims
	probs, err := c.backend.BatchEstimate(ctx, uncachedClaims, context)
	if err != nil {
		return nil, err
	}

	// Store results and update cache
	c.mu.Lock()
	for i, idx := range uncached {
		results[idx] = probs[i]
		key := c.cacheKey(claims[idx], context)
		c.set(key, probs[i])
	}
	c.mu.Unlock()

	return results, nil
}

func (c *CachingBackend) cacheKey(claim, context string) string {
	h := sha256.New()
	h.Write([]byte(claim))
	h.Write([]byte{0}) // Separator
	h.Write([]byte(context))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (c *CachingBackend) set(key string, prob float64) {
	// Evict if at capacity
	for len(c.cache) >= c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.cache, oldest)
	}

	c.cache[key] = CacheEntry{
		Probability: prob,
		CreatedAt:   time.Now(),
	}
	c.order = append(c.order, key)
}

// CacheStats returns cache statistics.
func (c *CachingBackend) CacheStats() (size int, hits int, misses int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache), int(c.hits), int(c.misses)
}

// ClearCache removes all cached entries.
func (c *CachingBackend) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]CacheEntry)
	c.order = c.order[:0]
}

// BackendType constants for configuration.
const (
	BackendTypeSelf     = "self"
	BackendTypeHaiku    = "haiku"
	BackendTypeExternal = "external"
	BackendTypeMock     = "mock"
)

// NewBackend creates a verification backend from config.
// [SPEC-08.27]
func NewBackend(cfg BackendConfig, client LLMCompleter) (VerifierBackend, error) {
	var backend VerifierBackend
	var err error

	switch cfg.Type {
	case BackendTypeSelf:
		backend = NewSelfVerifyBackend(client, cfg.Timeout, cfg.SamplingCount)
	case BackendTypeHaiku:
		backend = NewHaikuBackend(client, cfg.Timeout, cfg.SamplingCount)
	case BackendTypeMock:
		backend = NewMockBackend(0.5) // Default mock probability
	default:
		return nil, fmt.Errorf("unknown backend type: %s", cfg.Type)
	}

	if err != nil {
		return nil, err
	}

	// Wrap with caching if TTL > 0
	if cfg.CacheTTL > 0 {
		backend = NewCachingBackend(backend, cfg.CacheTTL, 1000)
	}

	return backend, nil
}

// VerificationPrompt generates the prompt for probability estimation.
// [SPEC-08.12]
const VerificationPromptTemplate = `Given the following context:
%s

Is the following claim true? Answer only YES or NO.
Claim: %s

Answer:`

// BuildVerificationPrompt creates the verification prompt.
func BuildVerificationPrompt(claim, context string) string {
	return fmt.Sprintf(VerificationPromptTemplate, context, claim)
}
