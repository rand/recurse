package routing

import (
	"time"
)

// TaskCategory represents a category of tasks for routing decisions.
type TaskCategory string

const (
	CategorySimple       TaskCategory = "simple"       // Direct answers, lookups
	CategoryReasoning    TaskCategory = "reasoning"    // Multi-step logic
	CategoryCoding       TaskCategory = "coding"       // Code generation/review
	CategoryCreative     TaskCategory = "creative"     // Writing, brainstorming
	CategoryAnalysis     TaskCategory = "analysis"     // Data analysis, summarization
	CategoryConversation TaskCategory = "conversation" // Chat, clarification
)

// AllCategories returns all task categories.
func AllCategories() []TaskCategory {
	return []TaskCategory{
		CategorySimple,
		CategoryReasoning,
		CategoryCoding,
		CategoryCreative,
		CategoryAnalysis,
		CategoryConversation,
	}
}

// ModelProfile tracks model capabilities and observed performance.
type ModelProfile struct {
	// Identification
	ID       string `json:"id"`
	Provider string `json:"provider"` // anthropic, openai, local
	Name     string `json:"name"`

	// Capabilities
	MaxTokens       int  `json:"max_tokens"`
	ContextWindow   int  `json:"context_window"`
	SupportsVision  bool `json:"supports_vision"`
	SupportsTools   bool `json:"supports_tools"`

	// Cost (per million tokens)
	InputCostPer1M  float64 `json:"input_cost_per_1m"`
	OutputCostPer1M float64 `json:"output_cost_per_1m"`

	// Observed performance (updated from routing history)
	MedianLatency time.Duration `json:"median_latency"`
	P95Latency    time.Duration `json:"p95_latency"`
	SuccessRate   float64       `json:"success_rate"` // 0.0-1.0

	// Category scores (learned per-category performance)
	CategoryScores map[TaskCategory]float64 `json:"category_scores"`

	// Metadata
	UpdatedAt time.Time `json:"updated_at"`
}

// GetCategoryScore returns the learned score for a category (default 0.5).
func (p *ModelProfile) GetCategoryScore(cat TaskCategory) float64 {
	if p.CategoryScores == nil {
		return 0.5
	}
	if score, ok := p.CategoryScores[cat]; ok {
		return score
	}
	return 0.5
}

// SetCategoryScore updates the score for a category.
func (p *ModelProfile) SetCategoryScore(cat TaskCategory, score float64) {
	if p.CategoryScores == nil {
		p.CategoryScores = make(map[TaskCategory]float64)
	}
	p.CategoryScores[cat] = clamp(score, 0, 1)
	p.UpdatedAt = time.Now()
}

// TaskFeatures contains extracted features for routing decisions.
type TaskFeatures struct {
	// Content features
	TokenCount int      `json:"token_count"`
	HasCode    bool     `json:"has_code"`
	HasMath    bool     `json:"has_math"`
	HasImages  bool     `json:"has_images"`
	Languages  []string `json:"languages,omitempty"` // Detected programming languages

	// Classification
	Category           TaskCategory `json:"category"`
	CategoryConfidence float64      `json:"category_confidence"` // 0.0-1.0

	// Complexity indicators
	EstimatedDepth int     `json:"estimated_depth"` // Reasoning depth needed
	Ambiguity      float64 `json:"ambiguity"`       // 0=clear, 1=ambiguous

	// Context
	ConversationTurns int `json:"conversation_turns"`
	PriorFailures     int `json:"prior_failures"`

	// Embedding for similarity matching
	Embedding []float32 `json:"embedding,omitempty"`
}

// RoutingConstraints specifies constraints for model selection.
type RoutingConstraints struct {
	MaxCostPerRequest    float64       `json:"max_cost_per_request,omitempty"`
	MaxLatency           time.Duration `json:"max_latency,omitempty"`
	MinQuality           float64       `json:"min_quality,omitempty"` // 0.0-1.0
	RequiredCapabilities []string      `json:"required_capabilities,omitempty"`
	ExcludedModels       []string      `json:"excluded_models,omitempty"`
	PreferredModels      []string      `json:"preferred_models,omitempty"`
}

// RoutingOutcome represents the outcome of a routed task.
type RoutingOutcome string

const (
	OutcomeSuccess   RoutingOutcome = "success"   // Task completed successfully
	OutcomeCorrected RoutingOutcome = "corrected" // Required user correction
	OutcomeFailed    RoutingOutcome = "failed"    // Task failed
)

// OutcomeWeight returns the learning weight for an outcome.
func (o RoutingOutcome) OutcomeWeight() float64 {
	switch o {
	case OutcomeSuccess:
		return 1.0
	case OutcomeCorrected:
		return 0.5
	case OutcomeFailed:
		return 0.0
	default:
		return 0.5
	}
}

// RoutingHistoryEntry records a routing decision and its outcome.
type RoutingHistoryEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`

	// Task information
	Task     string        `json:"task"`
	Features *TaskFeatures `json:"features"`

	// Routing decision
	ModelUsed    string   `json:"model_used"`
	Alternatives []string `json:"alternatives,omitempty"`
	Score        float64  `json:"score"`
	Reason       string   `json:"reason,omitempty"`

	// Outcome
	Outcome    RoutingOutcome `json:"outcome"`
	LatencyMS  int64          `json:"latency_ms"`
	TokensUsed int            `json:"tokens_used"`
	Cost       float64        `json:"cost"`
	UserRating *int           `json:"user_rating,omitempty"` // 1-5 rating if provided
}

// ScoringWeights configures the routing scoring algorithm.
type ScoringWeights struct {
	Quality    float64 `json:"quality"`    // Weight for model quality (default 0.4)
	Cost       float64 `json:"cost"`       // Weight for cost efficiency (default 0.3)
	Latency    float64 `json:"latency"`    // Weight for latency (default 0.1)
	Historical float64 `json:"historical"` // Weight for historical performance (default 0.2)
}

// DefaultScoringWeights returns the default scoring weights.
func DefaultScoringWeights() ScoringWeights {
	return ScoringWeights{
		Quality:    0.4,
		Cost:       0.3,
		Latency:    0.1,
		Historical: 0.2,
	}
}

// Normalize ensures weights sum to 1.0.
func (w *ScoringWeights) Normalize() {
	total := w.Quality + w.Cost + w.Latency + w.Historical
	if total == 0 {
		*w = DefaultScoringWeights()
		return
	}
	w.Quality /= total
	w.Cost /= total
	w.Latency /= total
	w.Historical /= total
}
