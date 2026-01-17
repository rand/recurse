package rlmcore

import (
	"fmt"
)

// ComplexityLevel represents query complexity tiers.
type ComplexityLevel int

const (
	ComplexitySimple ComplexityLevel = iota
	ComplexityModerate
	ComplexityComplex
	ComplexityCritical
)

func (c ComplexityLevel) String() string {
	switch c {
	case ComplexitySimple:
		return "simple"
	case ComplexityModerate:
		return "moderate"
	case ComplexityComplex:
		return "complex"
	case ComplexityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// PatternClassifier wraps the rlm-core PatternClassifier.
// Uses pattern matching and heuristics to classify query complexity.
type PatternClassifier struct {
	// handle unsafe.Pointer
}

// NewPatternClassifier creates a new rlm-core backed classifier.
func NewPatternClassifier() (*PatternClassifier, error) {
	if !Available() {
		return nil, fmt.Errorf("rlm-core not available")
	}
	// TODO: Call C.rlm_pattern_classifier_new()
	return nil, fmt.Errorf("not implemented: awaiting FFI")
}

// Classify determines the complexity level of a query.
func (p *PatternClassifier) Classify(query string) (ComplexityLevel, float64, error) {
	// TODO: Call C.rlm_pattern_classifier_classify()
	return ComplexitySimple, 0.0, fmt.Errorf("not implemented")
}

// ClassifyWithContext includes conversation context in classification.
func (p *PatternClassifier) ClassifyWithContext(query string, context []string) (ComplexityLevel, float64, error) {
	// TODO: Call C.rlm_pattern_classifier_classify_with_context()
	return ComplexitySimple, 0.0, fmt.Errorf("not implemented")
}
