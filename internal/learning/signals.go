// Package learning implements continuous learning for the RLM system.
// It captures learning signals from interactions, extracts patterns,
// and applies learned knowledge to improve future executions.
package learning

import (
	"encoding/json"
	"time"
)

// SignalType identifies the type of learning signal.
type SignalType int

const (
	// SignalSuccess indicates a task completed successfully.
	SignalSuccess SignalType = iota
	// SignalCorrection indicates the user provided a correction.
	SignalCorrection
	// SignalRejection indicates the user rejected the output.
	SignalRejection
	// SignalPreference indicates an explicit preference was stated.
	SignalPreference
	// SignalPattern indicates a successful pattern was detected.
	SignalPattern
	// SignalError indicates an error or failure occurred.
	SignalError
)

// String returns the string representation of the signal type.
func (s SignalType) String() string {
	switch s {
	case SignalSuccess:
		return "success"
	case SignalCorrection:
		return "correction"
	case SignalRejection:
		return "rejection"
	case SignalPreference:
		return "preference"
	case SignalPattern:
		return "pattern"
	case SignalError:
		return "error"
	default:
		return "unknown"
	}
}

// LearningSignal represents a signal that can be used for learning.
type LearningSignal struct {
	// ID is a unique identifier for the signal.
	ID string `json:"id"`

	// Type identifies what kind of signal this is.
	Type SignalType `json:"type"`

	// Context provides the execution context for the signal.
	Context SignalContext `json:"context"`

	// Embedding is the vector embedding for semantic matching.
	Embedding []float32 `json:"embedding,omitempty"`

	// Domain categorizes the signal (e.g., "go", "python", "testing").
	Domain string `json:"domain,omitempty"`

	// Confidence indicates how confident we are in this signal (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// Timestamp is when the signal was captured.
	Timestamp time.Time `json:"timestamp"`

	// Metadata contains additional signal-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SignalContext captures the context in which a signal was generated.
type SignalContext struct {
	// SessionID is the session where the signal was generated.
	SessionID string `json:"session_id"`

	// TaskID is the specific task being executed.
	TaskID string `json:"task_id,omitempty"`

	// Query is the user's original query/prompt.
	Query string `json:"query"`

	// Output is the system's output that triggered the signal.
	Output string `json:"output,omitempty"`

	// Model is the model that generated the output.
	Model string `json:"model,omitempty"`

	// Strategy is the execution strategy used.
	Strategy string `json:"strategy,omitempty"`

	// Duration is how long the execution took.
	Duration time.Duration `json:"duration,omitempty"`

	// TokensUsed is the number of tokens consumed.
	TokensUsed int `json:"tokens_used,omitempty"`
}

// CorrectionDetails contains details about a user correction.
type CorrectionDetails struct {
	// OriginalOutput is what the system produced.
	OriginalOutput string `json:"original_output"`

	// CorrectedOutput is what the user provided as correction.
	CorrectedOutput string `json:"corrected_output"`

	// CorrectionType categorizes the correction (e.g., "code", "reasoning", "format").
	CorrectionType string `json:"correction_type"`

	// Severity indicates how significant the correction was (0.0-1.0).
	Severity float64 `json:"severity"`

	// Explanation is the user's explanation of the correction (if provided).
	Explanation string `json:"explanation,omitempty"`
}

// PreferenceDetails contains details about a stated preference.
type PreferenceDetails struct {
	// Key is the preference identifier.
	Key string `json:"key"`

	// Value is the preference value.
	Value interface{} `json:"value"`

	// Scope is where this preference applies.
	Scope PreferenceScope `json:"scope"`

	// ScopeValue is the specific scope value (e.g., domain name, project path).
	ScopeValue string `json:"scope_value,omitempty"`

	// Explicit indicates whether this was explicitly stated by the user.
	Explicit bool `json:"explicit"`
}

// PatternDetails contains details about a detected pattern.
type PatternDetails struct {
	// Name is the pattern identifier.
	Name string `json:"name"`

	// PatternType categorizes the pattern.
	PatternType PatternType `json:"pattern_type"`

	// Trigger describes what triggers this pattern.
	Trigger string `json:"trigger"`

	// Template is the pattern template.
	Template string `json:"template"`

	// Examples are specific examples of this pattern.
	Examples []string `json:"examples,omitempty"`
}

// PatternType identifies the type of pattern.
type PatternType string

const (
	// PatternTypeCode is a code pattern (e.g., error handling template).
	PatternTypeCode PatternType = "code"

	// PatternTypeReasoning is a reasoning pattern (e.g., decomposition strategy).
	PatternTypeReasoning PatternType = "reasoning"

	// PatternTypeStructural is a structural pattern (e.g., API usage sequence).
	PatternTypeStructural PatternType = "structural"

	// PatternTypeNaming is a naming convention pattern.
	PatternTypeNaming PatternType = "naming"

	// PatternTypeWorkflow is a workflow/process pattern.
	PatternTypeWorkflow PatternType = "workflow"
)

// NewSuccessSignal creates a new success signal.
func NewSuccessSignal(ctx SignalContext, confidence float64) *LearningSignal {
	return &LearningSignal{
		Type:       SignalSuccess,
		Context:    ctx,
		Confidence: confidence,
		Timestamp:  time.Now(),
		Metadata:   make(map[string]interface{}),
	}
}

// NewCorrectionSignal creates a new correction signal.
func NewCorrectionSignal(ctx SignalContext, details CorrectionDetails) *LearningSignal {
	return &LearningSignal{
		Type:       SignalCorrection,
		Context:    ctx,
		Confidence: 1.0 - details.Severity, // Lower confidence for more severe corrections
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"correction": details,
		},
	}
}

// NewRejectionSignal creates a new rejection signal.
func NewRejectionSignal(ctx SignalContext, reason string) *LearningSignal {
	return &LearningSignal{
		Type:       SignalRejection,
		Context:    ctx,
		Confidence: 0.0, // Rejection means zero confidence in the output
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"reason": reason,
		},
	}
}

// NewPreferenceSignal creates a new preference signal.
func NewPreferenceSignal(ctx SignalContext, details PreferenceDetails) *LearningSignal {
	conf := 0.8 // Inferred preference
	if details.Explicit {
		conf = 1.0 // Explicit preference
	}
	return &LearningSignal{
		Type:       SignalPreference,
		Context:    ctx,
		Confidence: conf,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"preference": details,
		},
	}
}

// NewPatternSignal creates a new pattern signal.
func NewPatternSignal(ctx SignalContext, details PatternDetails, confidence float64) *LearningSignal {
	return &LearningSignal{
		Type:       SignalPattern,
		Context:    ctx,
		Confidence: confidence,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"pattern": details,
		},
	}
}

// NewErrorSignal creates a new error signal.
func NewErrorSignal(ctx SignalContext, err error) *LearningSignal {
	return &LearningSignal{
		Type:       SignalError,
		Context:    ctx,
		Confidence: 0.0,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"error": err.Error(),
		},
	}
}

// GetCorrectionDetails extracts correction details from a correction signal.
func (s *LearningSignal) GetCorrectionDetails() (*CorrectionDetails, bool) {
	if s.Type != SignalCorrection {
		return nil, false
	}
	v, ok := s.Metadata["correction"]
	if !ok {
		return nil, false
	}
	// Handle both direct struct and JSON-decoded map
	switch details := v.(type) {
	case CorrectionDetails:
		return &details, true
	case *CorrectionDetails:
		return details, true
	case map[string]interface{}:
		// Re-marshal and unmarshal to convert
		data, _ := json.Marshal(details)
		var cd CorrectionDetails
		if json.Unmarshal(data, &cd) == nil {
			return &cd, true
		}
	}
	return nil, false
}

// GetPreferenceDetails extracts preference details from a preference signal.
func (s *LearningSignal) GetPreferenceDetails() (*PreferenceDetails, bool) {
	if s.Type != SignalPreference {
		return nil, false
	}
	v, ok := s.Metadata["preference"]
	if !ok {
		return nil, false
	}
	switch details := v.(type) {
	case PreferenceDetails:
		return &details, true
	case *PreferenceDetails:
		return details, true
	case map[string]interface{}:
		data, _ := json.Marshal(details)
		var pd PreferenceDetails
		if json.Unmarshal(data, &pd) == nil {
			return &pd, true
		}
	}
	return nil, false
}

// GetPatternDetails extracts pattern details from a pattern signal.
func (s *LearningSignal) GetPatternDetails() (*PatternDetails, bool) {
	if s.Type != SignalPattern {
		return nil, false
	}
	v, ok := s.Metadata["pattern"]
	if !ok {
		return nil, false
	}
	switch details := v.(type) {
	case PatternDetails:
		return &details, true
	case *PatternDetails:
		return details, true
	case map[string]interface{}:
		data, _ := json.Marshal(details)
		var pd PatternDetails
		if json.Unmarshal(data, &pd) == nil {
			return &pd, true
		}
	}
	return nil, false
}
