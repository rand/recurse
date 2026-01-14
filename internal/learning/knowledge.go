package learning

import (
	"time"
)

// SourceType identifies how knowledge was acquired.
type SourceType string

const (
	// SourceExplicit means the user explicitly provided this knowledge.
	SourceExplicit SourceType = "explicit"

	// SourceInferred means this was inferred from interactions.
	SourceInferred SourceType = "inferred"

	// SourceCorrection means this came from a user correction.
	SourceCorrection SourceType = "correction"

	// SourcePattern means this was extracted from a detected pattern.
	SourcePattern SourceType = "pattern"
)

// PreferenceScope defines where a preference applies.
type PreferenceScope string

const (
	// ScopeGlobal applies to all contexts.
	ScopeGlobal PreferenceScope = "global"

	// ScopeDomain applies to a specific domain (e.g., "go", "python").
	ScopeDomain PreferenceScope = "domain"

	// ScopeProject applies to a specific project.
	ScopeProject PreferenceScope = "project"
)

// LearnedFact represents a fact learned from interactions.
type LearnedFact struct {
	// ID is a unique identifier for the fact.
	ID string `json:"id"`

	// Content is the fact content.
	Content string `json:"content"`

	// Domain categorizes the fact (e.g., "go", "testing", "architecture").
	Domain string `json:"domain,omitempty"`

	// Source indicates how this fact was acquired.
	Source SourceType `json:"source"`

	// Confidence is the confidence score (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// SuccessCount tracks how many times using this fact led to success.
	SuccessCount int `json:"success_count"`

	// FailureCount tracks how many times using this fact led to failure.
	FailureCount int `json:"failure_count"`

	// Embedding is the vector embedding for similarity search.
	Embedding []float32 `json:"embedding,omitempty"`

	// CreatedAt is when the fact was first learned.
	CreatedAt time.Time `json:"created_at"`

	// LastValidated is when the fact was last confirmed to be valid.
	LastValidated time.Time `json:"last_validated"`

	// LastAccessed is when the fact was last used.
	LastAccessed time.Time `json:"last_accessed"`

	// AccessCount is the total number of times this fact was accessed.
	AccessCount int `json:"access_count"`

	// Metadata contains additional fact-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SuccessRate returns the success rate for this fact.
func (f *LearnedFact) SuccessRate() float64 {
	total := f.SuccessCount + f.FailureCount
	if total == 0 {
		return 0.5 // No data, assume neutral
	}
	return float64(f.SuccessCount) / float64(total)
}

// LearnedPattern represents a successful pattern extracted from interactions.
type LearnedPattern struct {
	// ID is a unique identifier for the pattern.
	ID string `json:"id"`

	// Name is a descriptive name for the pattern.
	Name string `json:"name"`

	// PatternType categorizes the pattern.
	PatternType PatternType `json:"pattern_type"`

	// Trigger describes what conditions trigger this pattern.
	Trigger string `json:"trigger"`

	// Template is the pattern template with placeholders.
	Template string `json:"template"`

	// Examples are specific examples of this pattern.
	Examples []string `json:"examples,omitempty"`

	// Domains lists domains where this pattern applies.
	Domains []string `json:"domains,omitempty"`

	// SuccessRate tracks how often this pattern leads to success.
	SuccessRate float64 `json:"success_rate"`

	// UsageCount tracks how many times this pattern was applied.
	UsageCount int `json:"usage_count"`

	// Embedding is the vector embedding for similarity search.
	Embedding []float32 `json:"embedding,omitempty"`

	// CreatedAt is when the pattern was first learned.
	CreatedAt time.Time `json:"created_at"`

	// LastUsed is when the pattern was last applied.
	LastUsed time.Time `json:"last_used"`

	// Metadata contains additional pattern-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// UserPreference represents a user's stated or inferred preference.
type UserPreference struct {
	// ID is a unique identifier for the preference.
	ID string `json:"id"`

	// Key identifies the preference (e.g., "coding_style", "test_framework").
	Key string `json:"key"`

	// Value is the preference value.
	Value interface{} `json:"value"`

	// Scope defines where this preference applies.
	Scope PreferenceScope `json:"scope"`

	// ScopeValue is the specific scope (e.g., domain name, project path).
	ScopeValue string `json:"scope_value,omitempty"`

	// Source indicates how this preference was acquired.
	Source SourceType `json:"source"`

	// Confidence is the confidence score (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// CreatedAt is when the preference was first learned.
	CreatedAt time.Time `json:"created_at"`

	// LastUsed is when the preference was last applied.
	LastUsed time.Time `json:"last_used"`

	// UsageCount tracks how many times this preference was applied.
	UsageCount int `json:"usage_count"`

	// Metadata contains additional preference-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// LearnedConstraint represents a constraint learned from corrections or failures.
type LearnedConstraint struct {
	// ID is a unique identifier for the constraint.
	ID string `json:"id"`

	// ConstraintType categorizes the constraint.
	ConstraintType ConstraintType `json:"constraint_type"`

	// Description describes what should be avoided or enforced.
	Description string `json:"description"`

	// Correction provides the correct approach.
	Correction string `json:"correction,omitempty"`

	// Trigger describes what triggers this constraint check.
	Trigger string `json:"trigger,omitempty"`

	// Domain categorizes the constraint.
	Domain string `json:"domain,omitempty"`

	// Severity indicates how important this constraint is (0.0-1.0).
	Severity float64 `json:"severity"`

	// Source indicates how this constraint was learned.
	Source SourceType `json:"source"`

	// ViolationCount tracks how many times this was violated before learning.
	ViolationCount int `json:"violation_count"`

	// Embedding is the vector embedding for similarity search.
	Embedding []float32 `json:"embedding,omitempty"`

	// CreatedAt is when the constraint was first learned.
	CreatedAt time.Time `json:"created_at"`

	// LastTriggered is when the constraint was last checked.
	LastTriggered time.Time `json:"last_triggered"`

	// Metadata contains additional constraint-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ConstraintType identifies the type of constraint.
type ConstraintType string

const (
	// ConstraintAvoid indicates something to avoid.
	ConstraintAvoid ConstraintType = "avoid"

	// ConstraintRequire indicates something required.
	ConstraintRequire ConstraintType = "require"

	// ConstraintPrefer indicates a preference (softer than require).
	ConstraintPrefer ConstraintType = "prefer"

	// ConstraintFormat indicates a formatting constraint.
	ConstraintFormat ConstraintType = "format"

	// ConstraintSecurity indicates a security constraint.
	ConstraintSecurity ConstraintType = "security"
)

// KnowledgeItem is an interface for all knowledge types.
type KnowledgeItem interface {
	GetID() string
	GetConfidence() float64
	GetCreatedAt() time.Time
	GetDomain() string
}

// GetID implements KnowledgeItem.
func (f *LearnedFact) GetID() string        { return f.ID }
func (p *LearnedPattern) GetID() string     { return p.ID }
func (p *UserPreference) GetID() string     { return p.ID }
func (c *LearnedConstraint) GetID() string  { return c.ID }

// GetConfidence implements KnowledgeItem.
func (f *LearnedFact) GetConfidence() float64        { return f.Confidence }
func (p *LearnedPattern) GetConfidence() float64     { return p.SuccessRate }
func (p *UserPreference) GetConfidence() float64     { return p.Confidence }
func (c *LearnedConstraint) GetConfidence() float64  { return c.Severity }

// GetCreatedAt implements KnowledgeItem.
func (f *LearnedFact) GetCreatedAt() time.Time        { return f.CreatedAt }
func (p *LearnedPattern) GetCreatedAt() time.Time     { return p.CreatedAt }
func (p *UserPreference) GetCreatedAt() time.Time     { return p.CreatedAt }
func (c *LearnedConstraint) GetCreatedAt() time.Time  { return c.CreatedAt }

// GetDomain implements KnowledgeItem.
func (f *LearnedFact) GetDomain() string        { return f.Domain }
func (p *LearnedPattern) GetDomain() string     {
	if len(p.Domains) > 0 {
		return p.Domains[0]
	}
	return ""
}
func (p *UserPreference) GetDomain() string     {
	if p.Scope == ScopeDomain {
		return p.ScopeValue
	}
	return ""
}
func (c *LearnedConstraint) GetDomain() string  { return c.Domain }

// ApplyResult contains the result of applying knowledge to a query.
type ApplyResult struct {
	// RelevantFacts are facts relevant to the query.
	RelevantFacts []*LearnedFact `json:"relevant_facts,omitempty"`

	// ApplicablePatterns are patterns that may apply.
	ApplicablePatterns []*LearnedPattern `json:"applicable_patterns,omitempty"`

	// Preferences are user preferences to consider.
	Preferences []*UserPreference `json:"preferences,omitempty"`

	// Constraints are constraints to enforce.
	Constraints []*LearnedConstraint `json:"constraints,omitempty"`

	// ContextAdditions are text additions to enhance the prompt.
	ContextAdditions []string `json:"context_additions,omitempty"`

	// TotalConfidence is the aggregate confidence of all knowledge.
	TotalConfidence float64 `json:"total_confidence"`

	// ProcessingTime is how long it took to gather this knowledge.
	ProcessingTime time.Duration `json:"processing_time"`
}

// IsEmpty returns true if no knowledge was found.
func (r *ApplyResult) IsEmpty() bool {
	return len(r.RelevantFacts) == 0 &&
		len(r.ApplicablePatterns) == 0 &&
		len(r.Preferences) == 0 &&
		len(r.Constraints) == 0
}

// ItemCount returns the total number of knowledge items.
func (r *ApplyResult) ItemCount() int {
	return len(r.RelevantFacts) +
		len(r.ApplicablePatterns) +
		len(r.Preferences) +
		len(r.Constraints)
}
