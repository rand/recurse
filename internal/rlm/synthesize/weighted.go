// Package synthesize provides confidence-weighted synthesis for RLM sub-call results.
package synthesize

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"charm.land/fantasy"
)

// ConfidenceLevel represents discrete confidence levels.
type ConfidenceLevel int

const (
	// ConfidenceVeryLow indicates unreliable results.
	ConfidenceVeryLow ConfidenceLevel = iota
	// ConfidenceLow indicates uncertain results.
	ConfidenceLow
	// ConfidenceMedium indicates generally reliable results.
	ConfidenceMedium
	// ConfidenceHigh indicates highly reliable results.
	ConfidenceHigh
)

func (c ConfidenceLevel) String() string {
	switch c {
	case ConfidenceVeryLow:
		return "very_low"
	case ConfidenceLow:
		return "low"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceHigh:
		return "high"
	default:
		return "unknown"
	}
}

// ConfidenceComponent represents a factor contributing to confidence.
type ConfidenceComponent struct {
	Factor string  // e.g., "completeness", "consistency"
	Score  float64 // 0.0 to 1.0
	Weight float64 // relative weight
}

// Confidence represents a reliability score with breakdown.
type Confidence struct {
	// Score is the overall confidence (0.0 to 1.0).
	Score float64
	// Components break down what contributed to the score.
	Components []ConfidenceComponent
}

// Weighted returns the weighted average confidence.
func (c *Confidence) Weighted() float64 {
	if len(c.Components) == 0 {
		return c.Score
	}

	var sum, weightSum float64
	for _, comp := range c.Components {
		sum += comp.Score * comp.Weight
		weightSum += comp.Weight
	}

	if weightSum == 0 {
		return c.Score
	}

	return sum / weightSum
}

// Level returns the discrete confidence level.
func (c *Confidence) Level() ConfidenceLevel {
	score := c.Weighted()
	switch {
	case score >= 0.9:
		return ConfidenceHigh
	case score >= 0.7:
		return ConfidenceMedium
	case score >= 0.5:
		return ConfidenceLow
	default:
		return ConfidenceVeryLow
	}
}

// ScoredResult wraps a SubCallResult with confidence information.
type ScoredResult struct {
	SubCallResult
	Confidence Confidence
}

// Weight returns the normalized weight for this result.
func (r *ScoredResult) Weight() float64 {
	return r.Confidence.Weighted()
}

// Scorer calculates confidence for sub-call results.
type Scorer interface {
	Score(ctx context.Context, result *SubCallResult) (*Confidence, error)
}

// HeuristicScorer calculates confidence using text-based heuristics.
type HeuristicScorer struct {
	hedgeWords     []string
	certaintyWords []string
}

// NewHeuristicScorer creates a heuristic-based confidence scorer.
func NewHeuristicScorer() *HeuristicScorer {
	return &HeuristicScorer{
		hedgeWords: []string{
			"might", "maybe", "perhaps", "possibly", "could be",
			"i think", "i believe", "it seems", "probably",
			"not sure", "uncertain", "unclear", "appears to",
		},
		certaintyWords: []string{
			"definitely", "certainly", "always", "never",
			"must be", "clearly", "obviously", "undoubtedly",
		},
	}
}

// Score implements Scorer.
func (s *HeuristicScorer) Score(ctx context.Context, result *SubCallResult) (*Confidence, error) {
	if result.Error != "" {
		return &Confidence{Score: 0.0}, nil
	}

	content := result.Response
	lowerContent := strings.ToLower(content)

	components := []ConfidenceComponent{
		s.scoreLength(content),
		s.scoreHedging(lowerContent),
		s.scoreStructure(content),
		s.scoreSpecificity(content),
	}

	// Compute weighted average
	var sum, weightSum float64
	for _, c := range components {
		sum += c.Score * c.Weight
		weightSum += c.Weight
	}

	overall := 0.5
	if weightSum > 0 {
		overall = sum / weightSum
	}

	return &Confidence{
		Score:      math.Max(0, math.Min(1, overall)),
		Components: components,
	}, nil
}

func (s *HeuristicScorer) scoreLength(content string) ConfidenceComponent {
	words := len(strings.Fields(content))

	var score float64
	switch {
	case words < 10:
		score = 0.3 // Very short - likely incomplete
	case words < 50:
		score = 0.5
	case words < 200:
		score = 0.7
	case words < 500:
		score = 0.8
	default:
		score = 0.9 // Substantial response
	}

	return ConfidenceComponent{
		Factor: "length",
		Score:  score,
		Weight: 0.15,
	}
}

func (s *HeuristicScorer) scoreHedging(content string) ConfidenceComponent {
	hedgeCount := 0
	for _, word := range s.hedgeWords {
		hedgeCount += strings.Count(content, word)
	}

	// Some hedging is appropriate, but too much suggests uncertainty
	// 0 hedges: 0.8 (confident but maybe overconfident)
	// 1-2 hedges: 0.9 (appropriately calibrated)
	// 3-5 hedges: 0.7
	// 6+: 0.5
	var score float64
	switch {
	case hedgeCount == 0:
		score = 0.8
	case hedgeCount <= 2:
		score = 0.9
	case hedgeCount <= 5:
		score = 0.7
	default:
		score = 0.5
	}

	return ConfidenceComponent{
		Factor: "hedging",
		Score:  score,
		Weight: 0.2,
	}
}

func (s *HeuristicScorer) scoreStructure(content string) ConfidenceComponent {
	// Well-structured responses tend to be more reliable
	hasLists := strings.Contains(content, "- ") || strings.Contains(content, "1.")
	hasHeaders := strings.Contains(content, "##") || strings.Contains(content, "**")
	hasCode := strings.Contains(content, "```")
	hasParagraphs := strings.Count(content, "\n\n") >= 2

	score := 0.5
	if hasLists {
		score += 0.15
	}
	if hasHeaders {
		score += 0.1
	}
	if hasCode {
		score += 0.15
	}
	if hasParagraphs {
		score += 0.1
	}

	return ConfidenceComponent{
		Factor: "structure",
		Score:  math.Min(score, 1.0),
		Weight: 0.25,
	}
}

func (s *HeuristicScorer) scoreSpecificity(content string) ConfidenceComponent {
	// Specific responses tend to be more reliable
	hasNumbers := false
	hasQuotes := strings.Contains(content, "\"") || strings.Contains(content, "'")
	hasFilenames := strings.Contains(content, ".go") || strings.Contains(content, ".py") ||
		strings.Contains(content, ".js") || strings.Contains(content, ".ts")
	hasFunctionNames := strings.Contains(content, "()") || strings.Contains(content, "func ")

	// Check for numbers
	for _, r := range content {
		if r >= '0' && r <= '9' {
			hasNumbers = true
			break
		}
	}

	score := 0.5
	if hasNumbers {
		score += 0.1
	}
	if hasQuotes {
		score += 0.1
	}
	if hasFilenames {
		score += 0.15
	}
	if hasFunctionNames {
		score += 0.15
	}

	return ConfidenceComponent{
		Factor: "specificity",
		Score:  math.Min(score, 1.0),
		Weight: 0.4,
	}
}

// WeightedSynthesisConfig configures weighted synthesis behavior.
type WeightedSynthesisConfig struct {
	// MinConfidence is the minimum confidence to include a result.
	MinConfidence float64
	// IncludeProvenance adds source attribution to synthesis.
	IncludeProvenance bool
	// ShowConfidenceScores includes confidence percentages in synthesis.
	ShowConfidenceScores bool
}

// DefaultWeightedConfig returns sensible defaults.
func DefaultWeightedConfig() WeightedSynthesisConfig {
	return WeightedSynthesisConfig{
		MinConfidence:        0.3,
		IncludeProvenance:    false,
		ShowConfidenceScores: false,
	}
}

// WeightedSynthesisResult extends SynthesisResult with confidence data.
type WeightedSynthesisResult struct {
	SynthesisResult
	// OverallConfidence is the weighted average confidence.
	OverallConfidence float64
	// ScoredResults are the results with confidence scores.
	ScoredResults []*ScoredResult
	// FilteredCount is how many results were filtered due to low confidence.
	FilteredCount int
	// Warnings are alerts about the synthesis quality.
	Warnings []string
}

// WeightedSynthesizer combines results with confidence-based weighting.
type WeightedSynthesizer struct {
	scorer   Scorer
	provider fantasy.Provider
	model    string
	config   WeightedSynthesisConfig
}

// NewWeightedSynthesizer creates a confidence-weighted synthesizer.
func NewWeightedSynthesizer(provider fantasy.Provider, model string) *WeightedSynthesizer {
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}
	return &WeightedSynthesizer{
		scorer:   NewHeuristicScorer(),
		provider: provider,
		model:    model,
		config:   DefaultWeightedConfig(),
	}
}

// WithConfig sets the synthesis configuration.
func (s *WeightedSynthesizer) WithConfig(cfg WeightedSynthesisConfig) *WeightedSynthesizer {
	s.config = cfg
	return s
}

// WithScorer sets a custom confidence scorer.
func (s *WeightedSynthesizer) WithScorer(scorer Scorer) *WeightedSynthesizer {
	s.scorer = scorer
	return s
}

// Synthesize implements Synthesizer with confidence weighting.
func (s *WeightedSynthesizer) Synthesize(ctx context.Context, task string, results []SubCallResult) (*SynthesisResult, error) {
	weighted, err := s.SynthesizeWeighted(ctx, task, results)
	if err != nil {
		return nil, err
	}
	return &weighted.SynthesisResult, nil
}

// SynthesizeWeighted performs confidence-weighted synthesis with detailed results.
func (s *WeightedSynthesizer) SynthesizeWeighted(ctx context.Context, task string, results []SubCallResult) (*WeightedSynthesisResult, error) {
	if len(results) == 0 {
		return &WeightedSynthesisResult{
			SynthesisResult: SynthesisResult{
				Response: "(no results to synthesize)",
			},
		}, nil
	}

	// Score each result
	scored := make([]*ScoredResult, 0, len(results))
	for i := range results {
		conf, err := s.scorer.Score(ctx, &results[i])
		if err != nil {
			conf = &Confidence{Score: 0.5}
		}
		scored = append(scored, &ScoredResult{
			SubCallResult: results[i],
			Confidence:    *conf,
		})
	}

	// Filter by minimum confidence
	filtered, filteredCount := s.filterByConfidence(scored)

	if len(filtered) == 0 {
		// If all filtered, use the highest confidence one anyway
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].Confidence.Weighted() > scored[j].Confidence.Weighted()
		})
		filtered = scored[:1]
		filteredCount = len(scored) - 1
	}

	// Sort by confidence (highest first)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Confidence.Weighted() > filtered[j].Confidence.Weighted()
	})

	// Compute normalized weights
	weights := s.normalizeWeights(filtered)

	// Generate synthesis
	response, synthTokens, err := s.generateWeightedSynthesis(ctx, task, filtered, weights)
	if err != nil {
		return nil, fmt.Errorf("weighted synthesis: %w", err)
	}

	// Compute overall confidence
	overallConf := s.computeOverallConfidence(filtered, weights)

	// Generate warnings
	warnings := s.generateWarnings(filtered, filteredCount, overallConf)

	// Sum tokens
	totalTokens := synthTokens
	for _, r := range filtered {
		totalTokens += r.TokensUsed
	}

	return &WeightedSynthesisResult{
		SynthesisResult: SynthesisResult{
			Response:        response,
			TotalTokensUsed: totalTokens,
			PartCount:       len(filtered),
		},
		OverallConfidence: overallConf,
		ScoredResults:     scored,
		FilteredCount:     filteredCount,
		Warnings:          warnings,
	}, nil
}

func (s *WeightedSynthesizer) filterByConfidence(results []*ScoredResult) ([]*ScoredResult, int) {
	var filtered []*ScoredResult
	filteredCount := 0

	for _, r := range results {
		if r.Confidence.Weighted() >= s.config.MinConfidence {
			filtered = append(filtered, r)
		} else {
			filteredCount++
		}
	}

	return filtered, filteredCount
}

// normalizeWeights computes weights that sum to 1.0.
func (s *WeightedSynthesizer) normalizeWeights(results []*ScoredResult) []float64 {
	if len(results) == 0 {
		return nil
	}

	weights := make([]float64, len(results))
	var sum float64

	for i, r := range results {
		// Use squared confidence to amplify differences
		w := r.Confidence.Weighted()
		weights[i] = w * w
		sum += weights[i]
	}

	// Normalize to sum to 1.0
	if sum > 0 {
		for i := range weights {
			weights[i] /= sum
		}
	} else {
		// Equal weights if all zero
		for i := range weights {
			weights[i] = 1.0 / float64(len(weights))
		}
	}

	return weights
}

func (s *WeightedSynthesizer) generateWeightedSynthesis(
	ctx context.Context,
	task string,
	results []*ScoredResult,
	weights []float64,
) (string, int, error) {
	// If only one result, return it directly
	if len(results) == 1 {
		return results[0].Response, 0, nil
	}

	// Build weighted synthesis prompt
	var sb strings.Builder
	sb.WriteString("You are synthesizing results from multiple analysis passes into a coherent response.\n\n")
	sb.WriteString(fmt.Sprintf("Original task: %s\n\n", task))
	sb.WriteString("Results to synthesize (ordered by confidence, with weights):\n\n")

	for i, r := range results {
		weight := weights[i] * 100
		conf := r.Confidence.Weighted() * 100

		sb.WriteString(fmt.Sprintf("### Source %d (Confidence: %.0f%%, Weight: %.0f%%)\n\n", i+1, conf, weight))

		if r.Name != "" {
			sb.WriteString(fmt.Sprintf("**%s**\n\n", r.Name))
		}
		sb.WriteString(r.Response)
		sb.WriteString("\n\n")
	}

	sb.WriteString("---\n\n")
	sb.WriteString("Create a coherent, unified response that:\n")
	sb.WriteString("1. Prioritizes information from higher-weighted sources\n")
	sb.WriteString("2. Combines insights without redundancy\n")
	sb.WriteString("3. Maintains important details from all sources\n")
	sb.WriteString("4. Presents information in a logical order\n")
	sb.WriteString("5. If sources disagree, prefer the higher-confidence source\n\n")
	sb.WriteString("Synthesized Response:")

	// Call LLM
	lm, err := s.provider.LanguageModel(ctx, s.model)
	if err != nil {
		return "", 0, fmt.Errorf("get language model: %w", err)
	}

	maxTokens := int64(8192) // Allow room for comprehensive synthesis
	call := fantasy.Call{
		Prompt:          fantasy.Prompt{fantasy.NewUserMessage(sb.String())},
		MaxOutputTokens: &maxTokens,
	}

	resp, err := lm.Generate(ctx, call)
	if err != nil {
		return "", 0, fmt.Errorf("synthesis generation: %w", err)
	}

	return resp.Content.Text(), int(resp.Usage.TotalTokens), nil
}

func (s *WeightedSynthesizer) computeOverallConfidence(results []*ScoredResult, weights []float64) float64 {
	if len(results) == 0 || len(weights) == 0 {
		return 0
	}

	var sum float64
	for i, r := range results {
		sum += r.Confidence.Weighted() * weights[i]
	}

	return sum
}

func (s *WeightedSynthesizer) generateWarnings(results []*ScoredResult, filteredCount int, overallConf float64) []string {
	var warnings []string

	// Low confidence warning
	if overallConf < 0.5 {
		warnings = append(warnings,
			fmt.Sprintf("Low overall confidence (%.0f%%). Results may be unreliable.", overallConf*100))
	}

	// Filtered sources warning
	if filteredCount > 0 {
		warnings = append(warnings,
			fmt.Sprintf("%d low-confidence sources excluded from synthesis.", filteredCount))
	}

	// Single source warning
	if len(results) == 1 {
		warnings = append(warnings,
			"Only one source available. Consider verifying independently.")
	}

	// High variance warning
	if len(results) >= 2 {
		minConf := results[len(results)-1].Confidence.Weighted()
		maxConf := results[0].Confidence.Weighted()
		if maxConf-minConf > 0.4 {
			warnings = append(warnings,
				"High variance in source confidence. Synthesis may be dominated by one source.")
		}
	}

	return warnings
}

// ScoreResults scores multiple results and returns them sorted by confidence.
func ScoreResults(ctx context.Context, results []SubCallResult) ([]*ScoredResult, error) {
	scorer := NewHeuristicScorer()
	scored := make([]*ScoredResult, 0, len(results))

	for i := range results {
		conf, err := scorer.Score(ctx, &results[i])
		if err != nil {
			conf = &Confidence{Score: 0.5}
		}
		scored = append(scored, &ScoredResult{
			SubCallResult: results[i],
			Confidence:    *conf,
		})
	}

	// Sort by confidence descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Confidence.Weighted() > scored[j].Confidence.Weighted()
	})

	return scored, nil
}

// NormalizeWeights computes normalized weights from confidence scores.
func NormalizeWeights(results []*ScoredResult) []float64 {
	if len(results) == 0 {
		return nil
	}

	weights := make([]float64, len(results))
	var sum float64

	for i, r := range results {
		w := r.Confidence.Weighted()
		weights[i] = w * w // Square to amplify differences
		sum += weights[i]
	}

	if sum > 0 {
		for i := range weights {
			weights[i] /= sum
		}
	} else {
		for i := range weights {
			weights[i] = 1.0 / float64(len(weights))
		}
	}

	return weights
}
