package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Extractor processes learning signals and extracts patterns.
type Extractor struct {
	store *Store

	// Configuration
	minConfidence     float64 // Minimum confidence to store (default 0.5)
	similarityThresh  float64 // Threshold for considering patterns similar (default 0.85)
	maxExamples       int     // Maximum examples per pattern (default 5)
}

// ExtractorConfig configures the pattern extractor.
type ExtractorConfig struct {
	MinConfidence    float64
	SimilarityThresh float64
	MaxExamples      int
}

// NewExtractor creates a new pattern extractor.
func NewExtractor(store *Store, cfg ExtractorConfig) *Extractor {
	e := &Extractor{
		store:            store,
		minConfidence:    cfg.MinConfidence,
		similarityThresh: cfg.SimilarityThresh,
		maxExamples:      cfg.MaxExamples,
	}

	// Apply defaults
	if e.minConfidence == 0 {
		e.minConfidence = 0.5
	}
	if e.similarityThresh == 0 {
		e.similarityThresh = 0.85
	}
	if e.maxExamples == 0 {
		e.maxExamples = 5
	}

	return e
}

// ProcessSignal extracts and stores knowledge from a learning signal.
func (e *Extractor) ProcessSignal(ctx context.Context, signal *LearningSignal) error {
	if signal.Confidence < e.minConfidence {
		return nil // Skip low-confidence signals
	}

	// Record the signal itself
	if err := e.store.RecordSignal(ctx, signal); err != nil {
		return fmt.Errorf("record signal: %w", err)
	}

	// Extract knowledge based on signal type
	switch signal.Type {
	case SignalSuccess:
		return e.extractFromSuccess(ctx, signal)
	case SignalCorrection:
		return e.extractFromCorrection(ctx, signal)
	case SignalRejection:
		return e.extractFromRejection(ctx, signal)
	case SignalPreference:
		return e.extractFromPreference(ctx, signal)
	case SignalPattern:
		return e.extractFromPattern(ctx, signal)
	case SignalError:
		return e.extractFromError(ctx, signal)
	default:
		return nil
	}
}

// extractFromSuccess processes a success signal.
func (e *Extractor) extractFromSuccess(ctx context.Context, signal *LearningSignal) error {
	// Extract code patterns from successful output
	if signal.Context.Output != "" {
		patterns := e.detectCodePatterns(signal.Context.Output)
		for _, p := range patterns {
			p.Domains = []string{signal.Domain}
			if err := e.storeOrReinforcePattern(ctx, p); err != nil {
				continue // Log but don't fail
			}
		}
	}

	// Create a fact from the successful execution
	fact := &LearnedFact{
		ID:        uuid.New().String(),
		Content:   fmt.Sprintf("Successfully completed: %s", truncate(signal.Context.Query, 200)),
		Domain:    signal.Domain,
		Source:    SourceInferred,
		Confidence: signal.Confidence,
		SuccessCount: 1,
		CreatedAt: time.Now(),
		Metadata: map[string]interface{}{
			"strategy":    signal.Context.Strategy,
			"model":       signal.Context.Model,
			"tokens_used": signal.Context.TokensUsed,
		},
	}

	return e.storeOrReinforceFact(ctx, fact)
}

// extractFromCorrection processes a correction signal.
func (e *Extractor) extractFromCorrection(ctx context.Context, signal *LearningSignal) error {
	details, ok := signal.GetCorrectionDetails()
	if !ok {
		return nil
	}

	// Create a constraint from the correction
	constraint := &LearnedConstraint{
		ID:             uuid.New().String(),
		ConstraintType: ConstraintAvoid,
		Description:    fmt.Sprintf("Avoid: %s", truncate(details.OriginalOutput, 200)),
		Correction:     truncate(details.CorrectedOutput, 500),
		Domain:         signal.Domain,
		Severity:       details.Severity,
		Source:         SourceCorrection,
		ViolationCount: 1,
		CreatedAt:      time.Now(),
	}

	if details.Explanation != "" {
		constraint.Metadata = map[string]interface{}{
			"explanation": details.Explanation,
		}
	}

	return e.store.StoreConstraint(ctx, constraint)
}

// extractFromRejection processes a rejection signal.
func (e *Extractor) extractFromRejection(ctx context.Context, signal *LearningSignal) error {
	reason, _ := signal.Metadata["reason"].(string)

	constraint := &LearnedConstraint{
		ID:             uuid.New().String(),
		ConstraintType: ConstraintAvoid,
		Description:    fmt.Sprintf("Rejected output for query: %s", truncate(signal.Context.Query, 100)),
		Correction:     reason,
		Domain:         signal.Domain,
		Severity:       0.8, // High severity for rejections
		Source:         SourceCorrection,
		ViolationCount: 1,
		CreatedAt:      time.Now(),
	}

	return e.store.StoreConstraint(ctx, constraint)
}

// extractFromPreference processes a preference signal.
func (e *Extractor) extractFromPreference(ctx context.Context, signal *LearningSignal) error {
	details, ok := signal.GetPreferenceDetails()
	if !ok {
		return nil
	}

	source := SourceInferred
	if details.Explicit {
		source = SourceExplicit
	}

	pref := &UserPreference{
		ID:         uuid.New().String(),
		Key:        details.Key,
		Value:      details.Value,
		Scope:      details.Scope,
		ScopeValue: details.ScopeValue,
		Source:     source,
		Confidence: signal.Confidence,
		UsageCount: 1,
		CreatedAt:  time.Now(),
	}

	// Check for existing preference with same key/scope
	existing, err := e.store.GetPreferenceByKey(ctx, details.Key, details.Scope, details.ScopeValue)
	if err != nil {
		return err
	}

	if existing != nil {
		// Update existing preference
		existing.Value = details.Value
		existing.Confidence = max(existing.Confidence, signal.Confidence)
		existing.UsageCount++
		existing.LastUsed = time.Now()
		// Note: Store doesn't have UpdatePreference, so we'd need to add it
		// For now, just store the new one (duplicate handling in consolidator)
	}

	return e.store.StorePreference(ctx, pref)
}

// extractFromPattern processes a pattern signal.
func (e *Extractor) extractFromPattern(ctx context.Context, signal *LearningSignal) error {
	details, ok := signal.GetPatternDetails()
	if !ok {
		return nil
	}

	pattern := &LearnedPattern{
		ID:          uuid.New().String(),
		Name:        details.Name,
		PatternType: details.PatternType,
		Trigger:     details.Trigger,
		Template:    details.Template,
		Examples:    details.Examples,
		Domains:     []string{signal.Domain},
		SuccessRate: signal.Confidence,
		UsageCount:  1,
		CreatedAt:   time.Now(),
	}

	return e.storeOrReinforcePattern(ctx, pattern)
}

// extractFromError processes an error signal.
func (e *Extractor) extractFromError(ctx context.Context, signal *LearningSignal) error {
	errMsg, _ := signal.Metadata["error"].(string)

	// Create a fact about the failure
	fact := &LearnedFact{
		ID:           uuid.New().String(),
		Content:      fmt.Sprintf("Failed: %s - %s", truncate(signal.Context.Query, 100), truncate(errMsg, 200)),
		Domain:       signal.Domain,
		Source:       SourceInferred,
		Confidence:   0.3, // Low confidence for errors
		FailureCount: 1,
		CreatedAt:    time.Now(),
	}

	return e.store.StoreFact(ctx, fact)
}

// detectCodePatterns extracts code patterns from output.
func (e *Extractor) detectCodePatterns(code string) []*LearnedPattern {
	var patterns []*LearnedPattern

	// Detect error handling patterns
	if p := e.detectErrorHandling(code); p != nil {
		patterns = append(patterns, p)
	}

	// Detect test patterns
	if p := e.detectTestPatterns(code); p != nil {
		patterns = append(patterns, p)
	}

	// Detect structural patterns
	if p := e.detectStructuralPatterns(code); p != nil {
		patterns = append(patterns, p)
	}

	return patterns
}

// detectErrorHandling finds error handling patterns.
func (e *Extractor) detectErrorHandling(code string) *LearnedPattern {
	// Go error handling pattern
	goErrPattern := regexp.MustCompile(`if\s+err\s*!=\s*nil\s*\{[^}]+\}`)
	matches := goErrPattern.FindAllString(code, 3)
	if len(matches) >= 2 {
		return &LearnedPattern{
			ID:          patternHash("go-error-handling", matches[0]),
			Name:        "Go Error Handling",
			PatternType: PatternTypeCode,
			Trigger:     "Go code with error handling",
			Template:    "if err != nil { return fmt.Errorf(\"operation: %w\", err) }",
			Examples:    matches[:min(len(matches), e.maxExamples)],
			SuccessRate: 0.8,
			CreatedAt:   time.Now(),
		}
	}

	// Python try/except pattern
	pyErrPattern := regexp.MustCompile(`try:\s*\n[^e]+except[^:]+:[^e]+`)
	pyMatches := pyErrPattern.FindAllString(code, 3)
	if len(pyMatches) >= 1 {
		return &LearnedPattern{
			ID:          patternHash("python-error-handling", pyMatches[0]),
			Name:        "Python Error Handling",
			PatternType: PatternTypeCode,
			Trigger:     "Python code with error handling",
			Template:    "try:\n    # operation\nexcept SpecificError as e:\n    # handle",
			Examples:    pyMatches[:min(len(pyMatches), e.maxExamples)],
			SuccessRate: 0.8,
			CreatedAt:   time.Now(),
		}
	}

	return nil
}

// detectTestPatterns finds testing patterns.
func (e *Extractor) detectTestPatterns(code string) *LearnedPattern {
	// Table-driven tests in Go
	tableTestPattern := regexp.MustCompile(`(?s)tests?\s*:?=\s*\[\]struct\s*\{[^}]+\}\s*\{[^}]+\}`)
	if tableTestPattern.MatchString(code) {
		return &LearnedPattern{
			ID:          patternHash("go-table-test", code),
			Name:        "Go Table-Driven Test",
			PatternType: PatternTypeCode,
			Trigger:     "Go test function with multiple cases",
			Template:    "tests := []struct{ name string; input T; want T }{{...}}\nfor _, tt := range tests { t.Run(tt.name, func(t *testing.T) {...}) }",
			SuccessRate: 0.9,
			CreatedAt:   time.Now(),
		}
	}

	return nil
}

// detectStructuralPatterns finds structural/architectural patterns.
func (e *Extractor) detectStructuralPatterns(code string) *LearnedPattern {
	// Functional options pattern
	optsPattern := regexp.MustCompile(`func\s+With\w+\([^)]*\)\s+(func\([^)]+\)|Option)`)
	if matches := optsPattern.FindAllString(code, 3); len(matches) >= 2 {
		return &LearnedPattern{
			ID:          patternHash("functional-options", matches[0]),
			Name:        "Functional Options",
			PatternType: PatternTypeStructural,
			Trigger:     "Configurable struct with multiple optional settings",
			Template:    "type Option func(*Config)\nfunc WithX(x T) Option { return func(c *Config) { c.X = x } }",
			Examples:    matches[:min(len(matches), e.maxExamples)],
			SuccessRate: 0.85,
			CreatedAt:   time.Now(),
		}
	}

	return nil
}

// storeOrReinforceFact stores a new fact or reinforces an existing similar one.
func (e *Extractor) storeOrReinforceFact(ctx context.Context, fact *LearnedFact) error {
	// Search for similar facts
	existing, err := e.store.SearchFacts(ctx, fact.Content, 5)
	if err != nil {
		return e.store.StoreFact(ctx, fact)
	}

	// Check for similar content (simple substring match for now)
	for _, ex := range existing {
		if contentSimilar(fact.Content, ex.Content) {
			// Reinforce existing fact
			ex.SuccessCount += fact.SuccessCount
			ex.FailureCount += fact.FailureCount
			ex.Confidence = (ex.Confidence + fact.Confidence) / 2
			ex.LastValidated = time.Now()
			return e.store.UpdateFact(ctx, ex)
		}
	}

	// Store new fact
	return e.store.StoreFact(ctx, fact)
}

// storeOrReinforcePattern stores a new pattern or reinforces an existing similar one.
func (e *Extractor) storeOrReinforcePattern(ctx context.Context, pattern *LearnedPattern) error {
	// Search for similar patterns
	existing, err := e.store.ListPatterns(ctx, pattern.PatternType, 20)
	if err != nil {
		return e.store.StorePattern(ctx, pattern)
	}

	// Check for same pattern (by ID hash or similar name)
	for _, ex := range existing {
		if ex.ID == pattern.ID || strings.EqualFold(ex.Name, pattern.Name) {
			// Reinforce existing pattern
			ex.UsageCount++
			ex.SuccessRate = (ex.SuccessRate*float64(ex.UsageCount-1) + pattern.SuccessRate) / float64(ex.UsageCount)
			ex.LastUsed = time.Now()
			// Add new examples if unique
			for _, example := range pattern.Examples {
				if len(ex.Examples) < e.maxExamples && !contains(ex.Examples, example) {
					ex.Examples = append(ex.Examples, example)
				}
			}
			return e.store.UpdatePattern(ctx, ex)
		}
	}

	// Store new pattern
	return e.store.StorePattern(ctx, pattern)
}

// Helper functions

func patternHash(name, content string) string {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte(content[:min(len(content), 100)]))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func contentSimilar(a, b string) bool {
	// Simple similarity: check if one contains 60%+ of the other
	shorter, longer := a, b
	if len(a) > len(b) {
		shorter, longer = b, a
	}
	if len(shorter) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(longer), strings.ToLower(shorter[:len(shorter)*6/10]))
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
