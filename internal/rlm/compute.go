// Package rlm provides recursive language model orchestration.
// This file implements adaptive compute allocation based on query difficulty.
package rlm

import (
	"context"
	"regexp"
	"strings"
	"sync"
)

// DifficultyLevel represents query complexity categories.
type DifficultyLevel int

const (
	// DifficultyEasy represents simple, direct queries.
	DifficultyEasy DifficultyLevel = iota
	// DifficultyMedium represents moderate complexity queries.
	DifficultyMedium
	// DifficultyHard represents complex, multi-step queries.
	DifficultyHard
)

func (d DifficultyLevel) String() string {
	switch d {
	case DifficultyEasy:
		return "easy"
	case DifficultyMedium:
		return "medium"
	case DifficultyHard:
		return "hard"
	default:
		return "unknown"
	}
}

// DifficultySignal represents a factor contributing to difficulty estimation.
type DifficultySignal struct {
	Name   string
	Value  float64
	Weight float64
}

// DifficultyEstimate contains the difficulty assessment result.
type DifficultyEstimate struct {
	// Level is the categorized difficulty (easy/medium/hard).
	Level DifficultyLevel
	// Score is the raw difficulty score (0.0 to 1.0).
	Score float64
	// Signals are the factors that contributed to the estimate.
	Signals []DifficultySignal
	// Confidence is how confident we are in the estimate.
	Confidence float64
}

// ComputeAllocation represents the allocated compute resources for a query.
type ComputeAllocation struct {
	// DepthBudget is the maximum recursion depth allowed.
	DepthBudget int
	// ModelTier is the model quality tier ("fast", "balanced", "quality").
	ModelTier string
	// ParallelCalls is the maximum number of parallel LLM calls.
	ParallelCalls int
	// TimeoutMS is the timeout in milliseconds.
	TimeoutMS int
	// EstimatedCost is the estimated cost in arbitrary units.
	EstimatedCost float64
	// Difficulty contains the difficulty estimate used for allocation.
	Difficulty *DifficultyEstimate
}

// ComputeBudget represents available compute resources.
type ComputeBudget struct {
	// MaxTokens is the maximum token budget.
	MaxTokens int
	// MaxDepth is the maximum recursion depth.
	MaxDepth int
	// MaxTimeMS is the maximum execution time.
	MaxTimeMS int
	// CostLimit is the maximum cost in arbitrary units.
	CostLimit float64
}

// DefaultBudget returns a reasonable default compute budget.
func DefaultBudget() ComputeBudget {
	return ComputeBudget{
		MaxTokens: 100000,
		MaxDepth:  10,
		MaxTimeMS: 120000, // 2 minutes
		CostLimit: 1.0,
	}
}

// ComputeAllocator allocates compute resources based on query difficulty.
type ComputeAllocator struct {
	patterns []difficultyPattern
	mu       sync.RWMutex
	stats    AllocatorStats
}

type difficultyPattern struct {
	name   string
	regex  *regexp.Regexp
	level  DifficultyLevel
	weight float64
}

// AllocatorStats tracks allocation statistics.
type AllocatorStats struct {
	TotalAllocations int64
	EasyCount        int64
	MediumCount      int64
	HardCount        int64
	AvgDifficulty    float64
}

// NewComputeAllocator creates a new compute allocator with default patterns.
func NewComputeAllocator() *ComputeAllocator {
	return &ComputeAllocator{
		patterns: defaultDifficultyPatterns(),
	}
}

func defaultDifficultyPatterns() []difficultyPattern {
	return []difficultyPattern{
		// Easy patterns
		{name: "simple_math", regex: regexp.MustCompile(`(?i)what is \d+\s*[\+\-\*\/]\s*\d+`), level: DifficultyEasy, weight: 0.9},
		{name: "direct_lookup", regex: regexp.MustCompile(`(?i)^what is (the|a) \w+\??$`), level: DifficultyEasy, weight: 0.8},
		{name: "yes_no", regex: regexp.MustCompile(`(?i)^(is|are|can|does|do|will|should) .{5,50}\?$`), level: DifficultyEasy, weight: 0.7},
		{name: "simple_question", regex: regexp.MustCompile(`(?i)^(what|who|when|where) is `), level: DifficultyEasy, weight: 0.6},

		// Medium patterns
		{name: "summarize", regex: regexp.MustCompile(`(?i)(summarize|summary|brief|overview|explain)`), level: DifficultyMedium, weight: 0.7},
		{name: "compare", regex: regexp.MustCompile(`(?i)(compare|difference|versus|vs\.?|between)`), level: DifficultyMedium, weight: 0.7},
		{name: "analyze", regex: regexp.MustCompile(`(?i)(analyze|analysis|examine|review)`), level: DifficultyMedium, weight: 0.7},
		{name: "list_items", regex: regexp.MustCompile(`(?i)(list|enumerate|what are the|show me)`), level: DifficultyMedium, weight: 0.6},
		{name: "multi_part", regex: regexp.MustCompile(`(?i)(first|then|next|finally|also|additionally)`), level: DifficultyMedium, weight: 0.5},

		// Hard patterns
		{name: "debug", regex: regexp.MustCompile(`(?i)(debug|fix|error|bug|issue|problem|failing)`), level: DifficultyHard, weight: 0.8},
		{name: "implement", regex: regexp.MustCompile(`(?i)(implement|create|build|develop|write|add)`), level: DifficultyHard, weight: 0.7},
		{name: "architect", regex: regexp.MustCompile(`(?i)(architect|design|system|infrastructure|structure)`), level: DifficultyHard, weight: 0.8},
		{name: "refactor", regex: regexp.MustCompile(`(?i)(refactor|restructure|redesign|rewrite|optimize)`), level: DifficultyHard, weight: 0.8},
		{name: "multi_file", regex: regexp.MustCompile(`(?i)(files?|modules?|components?|packages?).{1,30}(files?|modules?|components?|packages?)`), level: DifficultyHard, weight: 0.8},
		{name: "comprehensive", regex: regexp.MustCompile(`(?i)(comprehensive|thorough|complete|all|entire|full)`), level: DifficultyHard, weight: 0.6},
		{name: "test_suite", regex: regexp.MustCompile(`(?i)(test|tests|testing|coverage|benchmark)`), level: DifficultyHard, weight: 0.6},
	}
}

// EstimateDifficulty analyzes a query and context to estimate difficulty.
func (a *ComputeAllocator) EstimateDifficulty(query string, contextTokens int) *DifficultyEstimate {
	signals := []DifficultySignal{}
	lowerQuery := strings.ToLower(query)

	// Pattern matching
	var maxLevel DifficultyLevel
	var totalWeight float64
	var matchCount int

	for _, p := range a.patterns {
		if p.regex.MatchString(lowerQuery) {
			signals = append(signals, DifficultySignal{
				Name:   p.name,
				Value:  1.0,
				Weight: p.weight,
			})
			if p.level > maxLevel {
				maxLevel = p.level
			}
			totalWeight += p.weight
			matchCount++
		}
	}

	// Query length signal
	queryWords := len(strings.Fields(query))
	lengthSignal := computeQueryLengthSignal(queryWords)
	signals = append(signals, lengthSignal)
	if lengthSignal.Value > 0.6 && maxLevel < DifficultyMedium {
		maxLevel = DifficultyMedium
	}
	if lengthSignal.Value > 0.8 && maxLevel < DifficultyHard {
		maxLevel = DifficultyHard
	}

	// Context size signal
	contextSignal := computeContextSizeSignal(contextTokens)
	signals = append(signals, contextSignal)
	if contextSignal.Value > 0.7 && maxLevel < DifficultyMedium {
		maxLevel = DifficultyMedium
	}
	if contextSignal.Value > 0.9 && maxLevel < DifficultyHard {
		maxLevel = DifficultyHard
	}

	// Compute confidence based on matches
	confidence := 0.5
	if matchCount > 0 {
		confidence = min(0.95, 0.5+float64(matchCount)*0.15)
	}

	// Compute raw score
	score := levelToScore(maxLevel)
	for _, s := range signals {
		score = (score + s.Value*s.Weight) / 2
	}
	score = min(1.0, max(0.0, score))

	return &DifficultyEstimate{
		Level:      maxLevel,
		Score:      score,
		Signals:    signals,
		Confidence: confidence,
	}
}

func computeQueryLengthSignal(words int) DifficultySignal {
	var value float64
	switch {
	case words < 5:
		value = 0.1
	case words < 15:
		value = 0.3
	case words < 30:
		value = 0.5
	case words < 50:
		value = 0.7
	default:
		value = 0.9
	}

	return DifficultySignal{
		Name:   "query_length",
		Value:  value,
		Weight: 0.3,
	}
}

func computeContextSizeSignal(tokens int) DifficultySignal {
	var value float64
	switch {
	case tokens < 1000:
		value = 0.2
	case tokens < 5000:
		value = 0.4
	case tokens < 20000:
		value = 0.6
	case tokens < 50000:
		value = 0.8
	default:
		value = 1.0
	}

	return DifficultySignal{
		Name:   "context_size",
		Value:  value,
		Weight: 0.4,
	}
}

func levelToScore(level DifficultyLevel) float64 {
	switch level {
	case DifficultyEasy:
		return 0.2
	case DifficultyMedium:
		return 0.5
	case DifficultyHard:
		return 0.85
	default:
		return 0.5
	}
}

// AllocateCompute determines compute allocation based on query and budget.
func (a *ComputeAllocator) AllocateCompute(
	ctx context.Context,
	query string,
	contextTokens int,
	budget ComputeBudget,
) *ComputeAllocation {
	difficulty := a.EstimateDifficulty(query, contextTokens)

	allocation := a.computeAllocationForDifficulty(difficulty, budget)
	allocation.Difficulty = difficulty

	// Update stats
	a.updateStats(difficulty)

	return allocation
}

func (a *ComputeAllocator) computeAllocationForDifficulty(
	difficulty *DifficultyEstimate,
	budget ComputeBudget,
) *ComputeAllocation {
	switch difficulty.Level {
	case DifficultyEasy:
		return &ComputeAllocation{
			DepthBudget:   min(2, budget.MaxDepth),
			ModelTier:     "fast",
			ParallelCalls: 1,
			TimeoutMS:     min(10000, budget.MaxTimeMS),
			EstimatedCost: budget.CostLimit * 0.1,
		}

	case DifficultyMedium:
		return &ComputeAllocation{
			DepthBudget:   min(5, budget.MaxDepth),
			ModelTier:     "balanced",
			ParallelCalls: 2,
			TimeoutMS:     min(30000, budget.MaxTimeMS),
			EstimatedCost: budget.CostLimit * 0.4,
		}

	case DifficultyHard:
		return &ComputeAllocation{
			DepthBudget:   budget.MaxDepth,
			ModelTier:     "quality",
			ParallelCalls: 4,
			TimeoutMS:     budget.MaxTimeMS,
			EstimatedCost: budget.CostLimit * 0.8,
		}

	default:
		// Default to medium
		return &ComputeAllocation{
			DepthBudget:   min(5, budget.MaxDepth),
			ModelTier:     "balanced",
			ParallelCalls: 2,
			TimeoutMS:     min(30000, budget.MaxTimeMS),
			EstimatedCost: budget.CostLimit * 0.4,
		}
	}
}

func (a *ComputeAllocator) updateStats(difficulty *DifficultyEstimate) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.stats.TotalAllocations++

	switch difficulty.Level {
	case DifficultyEasy:
		a.stats.EasyCount++
	case DifficultyMedium:
		a.stats.MediumCount++
	case DifficultyHard:
		a.stats.HardCount++
	}

	// Running average of difficulty score
	n := float64(a.stats.TotalAllocations)
	a.stats.AvgDifficulty = a.stats.AvgDifficulty*(n-1)/n + difficulty.Score/n
}

// Stats returns current allocator statistics.
func (a *ComputeAllocator) Stats() AllocatorStats {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stats
}

// Standalone functions for simple usage

// EstimateDifficulty is a convenience function that uses the default allocator.
func EstimateDifficulty(query string, contextTokens int) *DifficultyEstimate {
	allocator := NewComputeAllocator()
	return allocator.EstimateDifficulty(query, contextTokens)
}

// AllocateCompute is a convenience function that uses the default allocator and budget.
func AllocateCompute(ctx context.Context, query string, contextTokens int) *ComputeAllocation {
	allocator := NewComputeAllocator()
	return allocator.AllocateCompute(ctx, query, contextTokens, DefaultBudget())
}

// AllocateComputeWithBudget allocates compute with a custom budget.
func AllocateComputeWithBudget(
	ctx context.Context,
	query string,
	contextTokens int,
	budget ComputeBudget,
) *ComputeAllocation {
	allocator := NewComputeAllocator()
	return allocator.AllocateCompute(ctx, query, contextTokens, budget)
}
