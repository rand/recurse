package routing

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// Router selects the best model for a given task based on features and constraints.
type Router struct {
	mu sync.RWMutex

	profiles  map[string]*ModelProfile
	extractor *FeatureExtractor
	weights   ScoringWeights
	config    ScoringRouterConfig

	// Statistics
	routingCount int64
	fallbacks    int64
}

// ScoringRouterConfig configures the scoring router behavior.
type ScoringRouterConfig struct {
	// DefaultModel is used when no suitable model is found.
	DefaultModel string

	// MinConfidence is the minimum classification confidence to use category scores.
	MinConfidence float64

	// ExplorationRate is the probability of trying a non-optimal model (0-1).
	ExplorationRate float64

	// Weights for scoring (optional, uses defaults if nil).
	Weights *ScoringWeights
}

// RoutingResult contains the result of a routing decision.
type RoutingResult struct {
	ModelID      string        `json:"model_id"`
	Score        float64       `json:"score"`
	Reason       string        `json:"reason"`
	Alternatives []Alternative `json:"alternatives,omitempty"`
	Features     *TaskFeatures `json:"features"`
}

// Alternative represents an alternative model choice.
type Alternative struct {
	ModelID string  `json:"model_id"`
	Score   float64 `json:"score"`
}

// NewRouter creates a new router with the given profiles.
func NewRouter(profiles []*ModelProfile, extractor *FeatureExtractor, cfg ScoringRouterConfig) *Router {
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 0.7
	}

	weights := DefaultScoringWeights()
	if cfg.Weights != nil {
		weights = *cfg.Weights
	}
	weights.Normalize()

	profileMap := make(map[string]*ModelProfile)
	for _, p := range profiles {
		profileMap[p.ID] = p
	}

	return &Router{
		profiles:  profileMap,
		extractor: extractor,
		weights:   weights,
		config:    cfg,
	}
}

// Route selects the best model for a query.
func (r *Router) Route(ctx context.Context, query string, constraints *RoutingConstraints) (*RoutingResult, error) {
	r.mu.Lock()
	r.routingCount++
	r.mu.Unlock()

	// Extract features
	features := r.extractor.Extract(ctx, query, 0, 0)

	return r.RouteWithFeatures(ctx, features, constraints)
}

// RouteWithFeatures selects the best model given pre-extracted features.
func (r *Router) RouteWithFeatures(ctx context.Context, features *TaskFeatures, constraints *RoutingConstraints) (*RoutingResult, error) {
	r.mu.RLock()
	profiles := make([]*ModelProfile, 0, len(r.profiles))
	for _, p := range r.profiles {
		profiles = append(profiles, p)
	}
	weights := r.weights
	config := r.config
	r.mu.RUnlock()

	if constraints == nil {
		constraints = &RoutingConstraints{}
	}

	// Filter and score models
	candidates := r.filterCandidates(profiles, features, constraints)
	if len(candidates) == 0 {
		r.mu.Lock()
		r.fallbacks++
		r.mu.Unlock()

		if config.DefaultModel != "" {
			return &RoutingResult{
				ModelID:  config.DefaultModel,
				Score:    0.5,
				Reason:   "fallback: no candidates matched constraints",
				Features: features,
			}, nil
		}
		return nil, fmt.Errorf("no models match constraints")
	}

	// Score each candidate
	scored := make([]scoredModel, 0, len(candidates))
	for _, profile := range candidates {
		score := r.scoreModel(profile, features, weights, constraints)
		scored = append(scored, scoredModel{
			profile: profile,
			score:   score,
		})
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Build result
	best := scored[0]
	result := &RoutingResult{
		ModelID:  best.profile.ID,
		Score:    best.score,
		Reason:   r.explainChoice(best.profile, features, weights),
		Features: features,
	}

	// Add alternatives
	for i := 1; i < len(scored) && i < 4; i++ {
		result.Alternatives = append(result.Alternatives, Alternative{
			ModelID: scored[i].profile.ID,
			Score:   scored[i].score,
		})
	}

	return result, nil
}

type scoredModel struct {
	profile *ModelProfile
	score   float64
}

// filterCandidates removes models that don't meet constraints.
func (r *Router) filterCandidates(profiles []*ModelProfile, features *TaskFeatures, constraints *RoutingConstraints) []*ModelProfile {
	var candidates []*ModelProfile

	excluded := make(map[string]bool)
	for _, id := range constraints.ExcludedModels {
		excluded[id] = true
	}

	preferred := make(map[string]bool)
	for _, id := range constraints.PreferredModels {
		preferred[id] = true
	}

	for _, p := range profiles {
		// Skip excluded models
		if excluded[p.ID] {
			continue
		}

		// Check required capabilities
		if !r.hasCapabilities(p, constraints.RequiredCapabilities, features) {
			continue
		}

		// Check context window
		if features.TokenCount > 0 && p.ContextWindow > 0 && features.TokenCount > p.ContextWindow {
			continue
		}

		// Check max latency constraint
		if constraints.MaxLatency > 0 && p.MedianLatency > 0 && p.MedianLatency > constraints.MaxLatency {
			continue
		}

		// Estimate cost and check constraint
		if constraints.MaxCostPerRequest > 0 {
			estimatedCost := r.estimateCost(p, features.TokenCount)
			if estimatedCost > constraints.MaxCostPerRequest {
				continue
			}
		}

		candidates = append(candidates, p)
	}

	// If preferred models specified and some are candidates, filter to only preferred
	if len(preferred) > 0 {
		var preferredCandidates []*ModelProfile
		for _, c := range candidates {
			if preferred[c.ID] {
				preferredCandidates = append(preferredCandidates, c)
			}
		}
		if len(preferredCandidates) > 0 {
			return preferredCandidates
		}
	}

	return candidates
}

// hasCapabilities checks if a model has all required capabilities.
func (r *Router) hasCapabilities(p *ModelProfile, required []string, features *TaskFeatures) bool {
	for _, cap := range required {
		switch cap {
		case "vision":
			if !p.SupportsVision {
				return false
			}
		case "tools", "function_calling":
			if !p.SupportsTools {
				return false
			}
		case "long_context":
			if p.ContextWindow < 100000 {
				return false
			}
		}
	}

	// Check implicit capabilities from features
	if features.HasImages && !p.SupportsVision {
		return false
	}

	return true
}

// estimateCost estimates the cost for a request.
func (r *Router) estimateCost(p *ModelProfile, inputTokens int) float64 {
	// Estimate output tokens as 2x input (rough average)
	outputTokens := inputTokens * 2
	if outputTokens > p.MaxTokens && p.MaxTokens > 0 {
		outputTokens = p.MaxTokens
	}

	inputCost := float64(inputTokens) / 1_000_000 * p.InputCostPer1M
	outputCost := float64(outputTokens) / 1_000_000 * p.OutputCostPer1M
	return inputCost + outputCost
}

// scoreModel calculates a weighted score for a model.
func (r *Router) scoreModel(p *ModelProfile, features *TaskFeatures, weights ScoringWeights, constraints *RoutingConstraints) float64 {
	var score float64

	// Quality score (from category performance)
	qualityScore := p.GetCategoryScore(features.Category)
	if features.CategoryConfidence < r.config.MinConfidence {
		// Lower confidence means rely less on category-specific scores
		qualityScore = (qualityScore + 0.5) / 2
	}
	score += weights.Quality * qualityScore

	// Cost score (lower is better, normalized)
	costScore := r.costScore(p, features.TokenCount, constraints)
	score += weights.Cost * costScore

	// Latency score (lower is better)
	latencyScore := r.latencyScore(p, constraints)
	score += weights.Latency * latencyScore

	// Historical success rate
	histScore := p.SuccessRate
	if histScore == 0 {
		histScore = 0.5 // Unknown
	}
	score += weights.Historical * histScore

	return score
}

// costScore calculates a normalized cost score (1=cheapest, 0=most expensive).
func (r *Router) costScore(p *ModelProfile, tokens int, constraints *RoutingConstraints) float64 {
	cost := r.estimateCost(p, tokens)
	if cost == 0 {
		return 1.0 // Free
	}

	// Normalize against max cost constraint if provided
	if constraints.MaxCostPerRequest > 0 {
		// Score 1.0 at 0 cost, 0.0 at max cost
		return 1.0 - (cost / constraints.MaxCostPerRequest)
	}

	// Otherwise, use a logarithmic scale
	// Assume $0.001 is cheap (score 1.0), $1.00 is expensive (score 0.0)
	logCost := math.Log10(cost * 1000) // Scale so $0.001 = 0
	if logCost < 0 {
		return 1.0
	}
	if logCost > 3 {
		return 0.0
	}
	return 1.0 - (logCost / 3.0)
}

// latencyScore calculates a normalized latency score (1=fast, 0=slow).
func (r *Router) latencyScore(p *ModelProfile, constraints *RoutingConstraints) float64 {
	if p.MedianLatency == 0 {
		return 0.5 // Unknown
	}

	// Normalize against constraint if provided
	if constraints.MaxLatency > 0 {
		ratio := float64(p.MedianLatency) / float64(constraints.MaxLatency)
		return 1.0 - ratio
	}

	// Otherwise, use thresholds
	// <500ms = excellent (1.0), >10s = poor (0.0)
	ms := float64(p.MedianLatency.Milliseconds())
	if ms < 500 {
		return 1.0
	}
	if ms > 10000 {
		return 0.0
	}
	return 1.0 - (ms-500)/9500
}

// explainChoice generates a human-readable explanation for the routing decision.
func (r *Router) explainChoice(p *ModelProfile, features *TaskFeatures, weights ScoringWeights) string {
	var reasons []string

	// Category match
	catScore := p.GetCategoryScore(features.Category)
	if catScore >= 0.8 {
		reasons = append(reasons, fmt.Sprintf("excellent %s performance (%.0f%%)", features.Category, catScore*100))
	} else if catScore >= 0.6 {
		reasons = append(reasons, fmt.Sprintf("good %s performance (%.0f%%)", features.Category, catScore*100))
	}

	// Cost
	if p.InputCostPer1M < 1.0 {
		reasons = append(reasons, "cost-effective")
	}

	// Latency
	if p.MedianLatency > 0 && p.MedianLatency < 2*time.Second {
		reasons = append(reasons, "low latency")
	}

	// Success rate
	if p.SuccessRate >= 0.9 {
		reasons = append(reasons, fmt.Sprintf("%.0f%% success rate", p.SuccessRate*100))
	}

	if len(reasons) == 0 {
		return "best overall score"
	}

	return reasons[0]
}

// AddProfile adds or updates a model profile.
func (r *Router) AddProfile(p *ModelProfile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[p.ID] = p
}

// RemoveProfile removes a model profile.
func (r *Router) RemoveProfile(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.profiles, id)
}

// GetProfile returns a model profile by ID.
func (r *Router) GetProfile(id string) *ModelProfile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.profiles[id]
}

// UpdateWeights updates the scoring weights.
func (r *Router) UpdateWeights(weights ScoringWeights) {
	weights.Normalize()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.weights = weights
}

// Stats returns routing statistics.
func (r *Router) Stats() ScoringRouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return ScoringRouterStats{
		TotalRoutings:  r.routingCount,
		Fallbacks:      r.fallbacks,
		ProfileCount:   len(r.profiles),
		CurrentWeights: r.weights,
	}
}

// ScoringRouterStats contains router statistics.
type ScoringRouterStats struct {
	TotalRoutings  int64          `json:"total_routings"`
	Fallbacks      int64          `json:"fallbacks"`
	ProfileCount   int            `json:"profile_count"`
	CurrentWeights ScoringWeights `json:"current_weights"`
}
