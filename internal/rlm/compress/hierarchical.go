// Package compress provides context compression for the RLM system.
package compress

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// HierarchicalCompressor creates multi-level summaries for very large content.
// Each level provides progressively more aggressive compression, allowing
// dynamic selection based on available token budget.
type HierarchicalCompressor struct {
	base   *ContextCompressor
	levels []float64 // Target ratios for each level (e.g., [0.5, 0.25, 0.125])

	mu      sync.Mutex
	metrics HierarchicalMetrics
}

// HierarchicalMetrics tracks compression statistics.
type HierarchicalMetrics struct {
	TotalCompressions int64
	LevelSelections   map[int]int64
	AvgLevelsGenerated float64
}

// HierarchicalConfig configures the hierarchical compressor.
type HierarchicalConfig struct {
	// Base compressor configuration.
	Base Config

	// Levels defines the target ratios for each compression level.
	// Default: [0.5, 0.25, 0.125] (50%, 25%, 12.5% of original).
	Levels []float64
}

// DefaultHierarchicalConfig returns sensible defaults.
func DefaultHierarchicalConfig() HierarchicalConfig {
	return HierarchicalConfig{
		Base:   DefaultConfig(),
		Levels: []float64{0.5, 0.25, 0.125},
	}
}

// NewHierarchicalCompressor creates a new hierarchical compressor.
func NewHierarchicalCompressor(cfg HierarchicalConfig) *HierarchicalCompressor {
	levels := cfg.Levels
	if len(levels) == 0 {
		levels = []float64{0.5, 0.25, 0.125}
	}

	return &HierarchicalCompressor{
		base:   NewContextCompressor(cfg.Base),
		levels: levels,
		metrics: HierarchicalMetrics{
			LevelSelections: make(map[int]int64),
		},
	}
}

// HierarchicalResult contains multi-level compression results.
type HierarchicalResult struct {
	// Levels contains the compression result at each level.
	// Level 0 is the least compressed, higher levels are more compressed.
	Levels []*LevelResult

	// OriginalTokens is the token count of the original content.
	OriginalTokens int

	// Duration is how long the full compression took.
	Duration time.Duration
}

// LevelResult contains the compression result for a single level.
type LevelResult struct {
	// Level is the compression level (0 = least compressed).
	Level int

	// Content is the compressed content at this level.
	Content string

	// TokenCount is the token count at this level.
	TokenCount int

	// Ratio is the compression ratio relative to original (compressed/original).
	Ratio float64

	// TargetRatio is what was requested for this level.
	TargetRatio float64

	// Method indicates which compression technique was used.
	Method Method
}

// Compress creates multiple levels of compression for the given content.
func (h *HierarchicalCompressor) Compress(ctx context.Context, content string, opts Options) (*HierarchicalResult, error) {
	start := time.Now()

	originalTokens := estimateTokens(content)
	if originalTokens == 0 {
		return &HierarchicalResult{
			OriginalTokens: 0,
			Duration:       time.Since(start),
		}, nil
	}

	result := &HierarchicalResult{
		OriginalTokens: originalTokens,
		Levels:         make([]*LevelResult, 0, len(h.levels)),
	}

	// Start with original content
	currentContent := content

	for i, targetRatio := range h.levels {
		// Set target for this level
		levelOpts := opts
		levelOpts.TargetRatio = targetRatio
		levelOpts.TargetTokens = 0 // Use ratio

		// Compress from current content (cascading compression)
		compressed, err := h.base.Compress(ctx, currentContent, levelOpts)
		if err != nil {
			return nil, fmt.Errorf("level %d compression: %w", i, err)
		}

		levelResult := &LevelResult{
			Level:       i,
			Content:     compressed.Compressed,
			TokenCount:  compressed.CompressedTokens,
			Ratio:       float64(compressed.CompressedTokens) / float64(originalTokens),
			TargetRatio: targetRatio,
			Method:      compressed.Method,
		}
		result.Levels = append(result.Levels, levelResult)

		// Use this level's output as input for next level
		currentContent = compressed.Compressed

		// Stop if we've reached passthrough (can't compress further)
		if compressed.Method == MethodPassthrough {
			break
		}
	}

	result.Duration = time.Since(start)

	// Update metrics
	h.updateMetrics(result)

	return result, nil
}

// SelectLevel chooses the most appropriate compression level for a token budget.
// Returns the level with the most content that fits within the budget.
// If no level fits, returns the most compressed level.
func (r *HierarchicalResult) SelectLevel(tokenBudget int) *LevelResult {
	if len(r.Levels) == 0 {
		return nil
	}

	// Find the least compressed level that fits
	for _, level := range r.Levels {
		if level.TokenCount <= tokenBudget {
			return level
		}
	}

	// None fit, return most compressed
	return r.Levels[len(r.Levels)-1]
}

// SelectLevelByRatio chooses a level closest to the target ratio.
func (r *HierarchicalResult) SelectLevelByRatio(targetRatio float64) *LevelResult {
	if len(r.Levels) == 0 {
		return nil
	}

	var best *LevelResult
	bestDiff := 1.0

	for _, level := range r.Levels {
		diff := abs(level.Ratio - targetRatio)
		if diff < bestDiff {
			bestDiff = diff
			best = level
		}
	}

	return best
}

// BestLevel returns the level with the best quality/size tradeoff for the budget.
// It prefers levels that are close to but under the budget.
func (r *HierarchicalResult) BestLevel(tokenBudget int) *LevelResult {
	if len(r.Levels) == 0 {
		return nil
	}

	var best *LevelResult
	bestScore := -1.0

	for _, level := range r.Levels {
		if level.TokenCount > tokenBudget {
			continue
		}

		// Score based on how much of the budget we use (more = better content)
		score := float64(level.TokenCount) / float64(tokenBudget)
		if score > bestScore {
			bestScore = score
			best = level
		}
	}

	// If nothing fits, return most compressed
	if best == nil {
		return r.Levels[len(r.Levels)-1]
	}

	return best
}

func (h *HierarchicalCompressor) updateMetrics(result *HierarchicalResult) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.metrics.TotalCompressions++

	// Update running average of levels generated
	n := float64(h.metrics.TotalCompressions)
	levelsGen := float64(len(result.Levels))
	h.metrics.AvgLevelsGenerated = h.metrics.AvgLevelsGenerated*(n-1)/n + levelsGen/n
}

// RecordLevelSelection tracks which level was selected for use.
func (h *HierarchicalCompressor) RecordLevelSelection(level int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metrics.LevelSelections[level]++
}

// Metrics returns current compression metrics.
func (h *HierarchicalCompressor) Metrics() HierarchicalMetrics {
	h.mu.Lock()
	defer h.mu.Unlock()

	m := h.metrics
	m.LevelSelections = make(map[int]int64)
	for k, v := range h.metrics.LevelSelections {
		m.LevelSelections[k] = v
	}
	return m
}

// Base returns the underlying ContextCompressor.
func (h *HierarchicalCompressor) Base() *ContextCompressor {
	return h.base
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
