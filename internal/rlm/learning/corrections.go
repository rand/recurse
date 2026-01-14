package learning

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// CorrectionType categorizes the kind of correction.
type CorrectionType string

const (
	// CorrectionClassifier indicates the query classification was wrong.
	CorrectionClassifier CorrectionType = "classifier"

	// CorrectionExecution indicates the execution strategy was wrong.
	CorrectionExecution CorrectionType = "execution"

	// CorrectionRouting indicates the model routing was wrong.
	CorrectionRouting CorrectionType = "routing"

	// CorrectionOutput indicates the output content was wrong.
	CorrectionOutput CorrectionType = "output"

	// CorrectionStyle indicates the style/format was wrong.
	CorrectionStyle CorrectionType = "style"
)

// UserCorrection represents a user's correction to RLM output.
type UserCorrection struct {
	// Query is the original task/query.
	Query string `json:"query"`

	// RLMOutput is what the RLM produced.
	RLMOutput string `json:"rlm_output"`

	// Correction is the user's corrected version or feedback.
	Correction string `json:"correction"`

	// Type categorizes the correction.
	Type CorrectionType `json:"type"`

	// Severity indicates how severe the error was (0-1).
	Severity float64 `json:"severity"`

	// ModelUsed is the model that produced the output.
	ModelUsed string `json:"model_used"`

	// StrategyUsed is the execution strategy.
	StrategyUsed string `json:"strategy_used"`

	// QueryType is the classified query type.
	QueryType string `json:"query_type"`

	// Timestamp when the correction was made.
	Timestamp time.Time `json:"timestamp"`
}

// CorrectionPattern represents a detected pattern in corrections.
type CorrectionPattern struct {
	// Type is the correction type this pattern relates to.
	Type CorrectionType `json:"type"`

	// Description describes the pattern.
	Description string `json:"description"`

	// Keywords are common words/phrases in affected queries.
	Keywords []string `json:"keywords"`

	// Frequency is how often this pattern occurs.
	Frequency int `json:"frequency"`

	// AverageSeverity is the mean severity of corrections in this pattern.
	AverageSeverity float64 `json:"average_severity"`

	// AffectedModels are models that often need correction for this pattern.
	AffectedModels map[string]int `json:"affected_models"`

	// AffectedStrategies are strategies that often fail for this pattern.
	AffectedStrategies map[string]int `json:"affected_strategies"`

	// LastSeen when this pattern was last observed.
	LastSeen time.Time `json:"last_seen"`
}

// CorrectionLearnerConfig configures the CorrectionLearner.
type CorrectionLearnerConfig struct {
	// MaxCorrections limits stored corrections (default 1000).
	MaxCorrections int

	// PatternThreshold is minimum occurrences to form a pattern (default 3).
	PatternThreshold int

	// Logger for correction learning.
	Logger *slog.Logger
}

// DefaultCorrectionLearnerConfig returns sensible defaults.
func DefaultCorrectionLearnerConfig() CorrectionLearnerConfig {
	return CorrectionLearnerConfig{
		MaxCorrections:   1000,
		PatternThreshold: 3,
	}
}

// CorrectionLearner learns from user corrections to improve future responses.
type CorrectionLearner struct {
	mu sync.RWMutex

	// corrections stores recent corrections.
	corrections []UserCorrection

	// patterns are detected correction patterns.
	patterns map[CorrectionType][]*CorrectionPattern

	// learner is the underlying continuous learner for adjustments.
	learner *ContinuousLearner

	config CorrectionLearnerConfig
	logger *slog.Logger
}

// NewCorrectionLearner creates a new correction learner.
func NewCorrectionLearner(learner *ContinuousLearner, cfg CorrectionLearnerConfig) *CorrectionLearner {
	if cfg.MaxCorrections <= 0 {
		cfg.MaxCorrections = 1000
	}
	if cfg.PatternThreshold <= 0 {
		cfg.PatternThreshold = 3
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &CorrectionLearner{
		corrections: make([]UserCorrection, 0),
		patterns:    make(map[CorrectionType][]*CorrectionPattern),
		learner:     learner,
		config:      cfg,
		logger:      logger,
	}
}

// RecordCorrection records a user correction and updates learning.
func (cl *CorrectionLearner) RecordCorrection(ctx context.Context, correction UserCorrection) {
	if correction.Timestamp.IsZero() {
		correction.Timestamp = time.Now()
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()

	// Add correction
	cl.corrections = append(cl.corrections, correction)

	// Trim to max size
	if len(cl.corrections) > cl.config.MaxCorrections {
		cl.corrections = cl.corrections[len(cl.corrections)-cl.config.MaxCorrections:]
	}

	// Update patterns
	cl.updatePatterns(correction)

	// Send negative signal to learner
	if cl.learner != nil {
		cl.applyLearningSignal(correction)
	}

	cl.logger.Info("recorded correction",
		"type", correction.Type,
		"severity", correction.Severity,
		"model", correction.ModelUsed,
		"strategy", correction.StrategyUsed)
}

// updatePatterns updates detected patterns based on new correction.
func (cl *CorrectionLearner) updatePatterns(correction UserCorrection) {
	// Extract keywords from query
	keywords := extractKeywords(correction.Query)

	// Find or create matching pattern
	var matchedPattern *CorrectionPattern
	patterns := cl.patterns[correction.Type]

	for _, p := range patterns {
		if keywordOverlap(p.Keywords, keywords) > 0.5 {
			matchedPattern = p
			break
		}
	}

	if matchedPattern == nil {
		// Create new pattern
		matchedPattern = &CorrectionPattern{
			Type:               correction.Type,
			Description:        generatePatternDescription(correction),
			Keywords:           keywords,
			Frequency:          0,
			AffectedModels:     make(map[string]int),
			AffectedStrategies: make(map[string]int),
		}
		cl.patterns[correction.Type] = append(cl.patterns[correction.Type], matchedPattern)
	}

	// Update pattern stats
	matchedPattern.Frequency++
	matchedPattern.LastSeen = correction.Timestamp

	// Update running average of severity
	matchedPattern.AverageSeverity = (matchedPattern.AverageSeverity*float64(matchedPattern.Frequency-1) + correction.Severity) / float64(matchedPattern.Frequency)

	// Track affected models and strategies
	if correction.ModelUsed != "" {
		matchedPattern.AffectedModels[correction.ModelUsed]++
	}
	if correction.StrategyUsed != "" {
		matchedPattern.AffectedStrategies[correction.StrategyUsed]++
	}

	// Merge keywords
	for _, kw := range keywords {
		if !containsString(matchedPattern.Keywords, kw) {
			matchedPattern.Keywords = append(matchedPattern.Keywords, kw)
		}
	}

	// Limit keywords
	if len(matchedPattern.Keywords) > 20 {
		matchedPattern.Keywords = matchedPattern.Keywords[:20]
	}
}

// applyLearningSignal sends a negative signal to the learner.
func (cl *CorrectionLearner) applyLearningSignal(correction UserCorrection) {
	// Create a negative outcome to discourage the problematic behavior
	outcome := ExecutionOutcome{
		Query:         correction.Query,
		QueryFeatures: QueryFeatures{Category: correction.QueryType},
		StrategyUsed:  correction.StrategyUsed,
		ModelUsed:     correction.ModelUsed,
		Success:       false,
		QualityScore:  1.0 - correction.Severity, // Low quality for high severity
		Timestamp:     correction.Timestamp,
	}

	cl.learner.RecordOutcome(outcome)
}

// GetPatterns returns detected patterns for a correction type.
func (cl *CorrectionLearner) GetPatterns(corrType CorrectionType) []*CorrectionPattern {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	patterns := cl.patterns[corrType]
	result := make([]*CorrectionPattern, len(patterns))
	copy(result, patterns)
	return result
}

// GetAllPatterns returns all detected patterns.
func (cl *CorrectionLearner) GetAllPatterns() map[CorrectionType][]*CorrectionPattern {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	result := make(map[CorrectionType][]*CorrectionPattern)
	for k, v := range cl.patterns {
		patternsCopy := make([]*CorrectionPattern, len(v))
		copy(patternsCopy, v)
		result[k] = patternsCopy
	}
	return result
}

// GetSignificantPatterns returns patterns that meet the frequency threshold.
func (cl *CorrectionLearner) GetSignificantPatterns() []*CorrectionPattern {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	var significant []*CorrectionPattern
	for _, patterns := range cl.patterns {
		for _, p := range patterns {
			if p.Frequency >= cl.config.PatternThreshold {
				significant = append(significant, p)
			}
		}
	}
	return significant
}

// SuggestAdjustments suggests routing/strategy adjustments based on patterns.
func (cl *CorrectionLearner) SuggestAdjustments() []Adjustment {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	var adjustments []Adjustment

	for corrType, patterns := range cl.patterns {
		for _, p := range patterns {
			if p.Frequency < cl.config.PatternThreshold {
				continue
			}

			// Find the most problematic model
			var worstModel string
			var maxCount int
			for model, count := range p.AffectedModels {
				if count > maxCount {
					maxCount = count
					worstModel = model
				}
			}

			// Find the most problematic strategy
			var worstStrategy string
			maxCount = 0
			for strategy, count := range p.AffectedStrategies {
				if count > maxCount {
					maxCount = count
					worstStrategy = strategy
				}
			}

			if worstModel != "" {
				adjustments = append(adjustments, Adjustment{
					Type:        AdjustmentRouting,
					Target:      worstModel,
					Change:      -p.AverageSeverity * 0.3,
					Reason:      string(corrType) + " corrections: " + p.Description,
					Confidence:  float64(p.Frequency) / float64(cl.config.PatternThreshold+10),
					CorrPattern: p,
				})
			}

			if worstStrategy != "" {
				adjustments = append(adjustments, Adjustment{
					Type:        AdjustmentStrategy,
					Target:      worstStrategy,
					Change:      -p.AverageSeverity * 0.3,
					Reason:      string(corrType) + " corrections: " + p.Description,
					Confidence:  float64(p.Frequency) / float64(cl.config.PatternThreshold+10),
					CorrPattern: p,
				})
			}
		}
	}

	return adjustments
}

// AdjustmentType categorizes the kind of adjustment.
type AdjustmentType string

const (
	AdjustmentRouting  AdjustmentType = "routing"
	AdjustmentStrategy AdjustmentType = "strategy"
)

// Adjustment represents a suggested adjustment based on corrections.
type Adjustment struct {
	// Type is the adjustment type.
	Type AdjustmentType `json:"type"`

	// Target is what should be adjusted (model ID or strategy name).
	Target string `json:"target"`

	// Change is the recommended adjustment value.
	Change float64 `json:"change"`

	// Reason explains why this adjustment is suggested.
	Reason string `json:"reason"`

	// Confidence in this adjustment (0-1).
	Confidence float64 `json:"confidence"`

	// CorrPattern is the pattern that triggered this adjustment.
	CorrPattern *CorrectionPattern `json:"correction_pattern,omitempty"`
}

// Stats returns correction learning statistics.
func (cl *CorrectionLearner) Stats() CorrectionStats {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	byType := make(map[CorrectionType]int)
	byModel := make(map[string]int)
	var totalSeverity float64

	for _, c := range cl.corrections {
		byType[c.Type]++
		if c.ModelUsed != "" {
			byModel[c.ModelUsed]++
		}
		totalSeverity += c.Severity
	}

	var avgSeverity float64
	if len(cl.corrections) > 0 {
		avgSeverity = totalSeverity / float64(len(cl.corrections))
	}

	patternCount := 0
	for _, patterns := range cl.patterns {
		patternCount += len(patterns)
	}

	return CorrectionStats{
		TotalCorrections:  len(cl.corrections),
		CorrectionsByType: byType,
		CorrectionsByModel: byModel,
		AverageSeverity:   avgSeverity,
		PatternCount:      patternCount,
	}
}

// CorrectionStats contains correction learning statistics.
type CorrectionStats struct {
	TotalCorrections   int                     `json:"total_corrections"`
	CorrectionsByType  map[CorrectionType]int  `json:"corrections_by_type"`
	CorrectionsByModel map[string]int          `json:"corrections_by_model"`
	AverageSeverity    float64                 `json:"average_severity"`
	PatternCount       int                     `json:"pattern_count"`
}

// ToJSON returns stats as JSON string.
func (s CorrectionStats) ToJSON() string {
	data, _ := json.MarshalIndent(s, "", "  ")
	return string(data)
}

// Reset clears all corrections and patterns.
func (cl *CorrectionLearner) Reset() {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	cl.corrections = make([]UserCorrection, 0)
	cl.patterns = make(map[CorrectionType][]*CorrectionPattern)

	cl.logger.Info("reset correction learner")
}

// extractKeywords extracts significant words from text.
func extractKeywords(text string) []string {
	// Simple keyword extraction - split and filter
	words := strings.Fields(strings.ToLower(text))

	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"into": true, "through": true, "during": true, "before": true,
		"after": true, "above": true, "below": true, "between": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"this": true, "that": true, "these": true, "those": true,
		"i": true, "you": true, "he": true, "she": true, "it": true,
		"we": true, "they": true, "me": true, "him": true, "her": true,
	}

	var keywords []string
	for _, word := range words {
		// Skip short words and stop words
		if len(word) < 3 || stopWords[word] {
			continue
		}
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:'\"()[]{}")
		if len(word) >= 3 {
			keywords = append(keywords, word)
		}
	}

	// Limit to top 10 keywords
	if len(keywords) > 10 {
		keywords = keywords[:10]
	}

	return keywords
}

// keywordOverlap calculates overlap between two keyword sets.
func keywordOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	setA := make(map[string]bool)
	for _, k := range a {
		setA[k] = true
	}

	matches := 0
	for _, k := range b {
		if setA[k] {
			matches++
		}
	}

	// Jaccard similarity
	union := len(a) + len(b) - matches
	if union == 0 {
		return 0
	}
	return float64(matches) / float64(union)
}

// generatePatternDescription creates a description for a new pattern.
func generatePatternDescription(correction UserCorrection) string {
	switch correction.Type {
	case CorrectionClassifier:
		return "query classification errors"
	case CorrectionExecution:
		return "execution strategy failures"
	case CorrectionRouting:
		return "model routing issues"
	case CorrectionOutput:
		return "output content errors"
	case CorrectionStyle:
		return "style/format issues"
	default:
		return "general corrections"
	}
}

// containsString checks if a slice contains a string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
