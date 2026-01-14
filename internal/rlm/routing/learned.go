// Package routing implements learned model routing with cascading.
package routing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/rand/recurse/internal/rlm/learning"
	"github.com/rand/recurse/internal/rlm/meta"
)

// RoutingDecision represents a model routing decision.
type RoutingDecision struct {
	// Model is the selected model specification.
	Model *meta.ModelSpec

	// Score is the routing score (higher = more confident).
	Score float64

	// Reason explains why this model was chosen.
	Reason string

	// Alternatives are other considered models.
	Alternatives []*meta.ModelSpec
}

// CascadeResult represents the result of a cascading call.
type CascadeResult struct {
	// Response is the final response text.
	Response string

	// FinalModel is the model that produced the response.
	FinalModel *meta.ModelSpec

	// Attempts is the number of models tried.
	Attempts int

	// TotalCost is the cumulative cost across all attempts.
	TotalCost float64

	// TotalLatencyMS is the cumulative latency.
	TotalLatencyMS int64

	// Escalated indicates if escalation occurred.
	Escalated bool
}

// RouterConfig configures the LearnedRouter.
type RouterConfig struct {
	// Models is the model catalog (uses DefaultModels if empty).
	Models []meta.ModelSpec

	// Learner provides learned routing adjustments.
	Learner *learning.ContinuousLearner

	// ConfidenceThreshold triggers escalation below this value (default 0.6).
	ConfidenceThreshold float64

	// CostSensitivity controls cost vs quality tradeoff (0-1, default 0.5).
	// 0 = prefer quality, 1 = prefer cost savings.
	CostSensitivity float64

	// CascadeOrder defines model escalation order by tier.
	// Default: [Fast, Balanced, Powerful, Reasoning]
	CascadeOrder []meta.ModelTier

	// Logger for routing decisions.
	Logger *slog.Logger
}

// DefaultRouterConfig returns sensible defaults.
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		ConfidenceThreshold: 0.6,
		CostSensitivity:     0.5,
		CascadeOrder: []meta.ModelTier{
			meta.TierFast,
			meta.TierBalanced,
			meta.TierPowerful,
			meta.TierReasoning,
		},
	}
}

// LearnedRouter routes tasks to models using learned preferences.
type LearnedRouter struct {
	mu sync.RWMutex

	models              []meta.ModelSpec
	modelsByTier        map[meta.ModelTier][]*meta.ModelSpec
	learner             *learning.ContinuousLearner
	confidenceThreshold float64
	costSensitivity     float64
	cascadeOrder        []meta.ModelTier
	logger              *slog.Logger

	// Statistics
	totalRoutes     int64
	totalEscalations int64
	routesByModel   map[string]int64
}

// NewLearnedRouter creates a new learned router.
func NewLearnedRouter(cfg RouterConfig) *LearnedRouter {
	models := cfg.Models
	if len(models) == 0 {
		models = meta.DefaultModels()
	}

	threshold := cfg.ConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.6
	}

	costSens := cfg.CostSensitivity
	if costSens < 0 {
		costSens = 0
	} else if costSens > 1 {
		costSens = 1
	}

	cascadeOrder := cfg.CascadeOrder
	if len(cascadeOrder) == 0 {
		cascadeOrder = []meta.ModelTier{
			meta.TierFast,
			meta.TierBalanced,
			meta.TierPowerful,
			meta.TierReasoning,
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Build tier index
	modelsByTier := make(map[meta.ModelTier][]*meta.ModelSpec)
	for i := range models {
		tier := models[i].Tier
		modelsByTier[tier] = append(modelsByTier[tier], &models[i])
	}

	return &LearnedRouter{
		models:              models,
		modelsByTier:        modelsByTier,
		learner:             cfg.Learner,
		confidenceThreshold: threshold,
		costSensitivity:     costSens,
		cascadeOrder:        cascadeOrder,
		logger:              logger,
		routesByModel:       make(map[string]int64),
	}
}

// Route selects the best model for a task based on learned preferences.
func (r *LearnedRouter) Route(ctx context.Context, query string, queryType string, costSensitivity float64) *RoutingDecision {
	r.mu.Lock()
	r.totalRoutes++
	r.mu.Unlock()

	// Determine base tier from task complexity
	baseTier := r.determineBaseTier(query, costSensitivity)

	// Get candidates for this tier
	candidates := r.modelsByTier[baseTier]
	if len(candidates) == 0 {
		// Fall back to fast tier
		candidates = r.modelsByTier[meta.TierFast]
	}

	// Score each candidate
	var best *meta.ModelSpec
	var bestScore float64
	var alternatives []*meta.ModelSpec
	var reason string

	for _, model := range candidates {
		score := r.scoreModel(model, queryType, costSensitivity)

		if best == nil || score > bestScore {
			if best != nil {
				alternatives = append(alternatives, best)
			}
			best = model
			bestScore = score
		} else {
			alternatives = append(alternatives, model)
		}
	}

	if best != nil {
		reason = r.explainChoice(best, queryType, bestScore)

		r.mu.Lock()
		r.routesByModel[best.ID]++
		r.mu.Unlock()

		r.logger.Debug("route decision",
			"query_type", queryType,
			"model", best.ID,
			"score", bestScore,
			"tier", baseTier)
	}

	return &RoutingDecision{
		Model:        best,
		Score:        bestScore,
		Reason:       reason,
		Alternatives: alternatives,
	}
}

// scoreModel computes a routing score for a model.
func (r *LearnedRouter) scoreModel(model *meta.ModelSpec, queryType string, costSensitivity float64) float64 {
	// Base score from model quality
	var baseScore float64
	switch model.Tier {
	case meta.TierFast:
		baseScore = 0.6
	case meta.TierBalanced:
		baseScore = 0.75
	case meta.TierPowerful:
		baseScore = 0.9
	case meta.TierReasoning:
		baseScore = 0.95
	}

	// Adjust for cost sensitivity
	// Normalize cost to 0-1 range (assuming max $50/M tokens)
	avgCost := (model.InputCost + model.OutputCost) / 2
	costPenalty := (avgCost / 50.0) * costSensitivity

	score := baseScore - costPenalty

	// Apply learned adjustments
	if r.learner != nil && queryType != "" {
		adjustment := r.learner.GetRoutingAdjustment(queryType, model.ID)
		score += adjustment * 0.5 // Scale adjustment impact
	}

	// Bonus for matching strengths
	queryLower := strings.ToLower(queryType)
	for _, strength := range model.Strengths {
		if strings.Contains(queryLower, strength) {
			score += 0.1
			break
		}
	}

	return clamp(score, 0, 1)
}

// determineBaseTier determines the base tier from task characteristics.
func (r *LearnedRouter) determineBaseTier(query string, costSensitivity float64) meta.ModelTier {
	queryLower := strings.ToLower(query)

	// High cost sensitivity prefers cheaper models
	if costSensitivity > 0.7 {
		return meta.TierFast
	}

	// Check for reasoning keywords
	reasoningKeywords := []string{"prove", "theorem", "logic", "math", "calculate", "derive"}
	for _, kw := range reasoningKeywords {
		if strings.Contains(queryLower, kw) {
			return meta.TierReasoning
		}
	}

	// Check for complex keywords
	complexKeywords := []string{"analyze", "refactor", "design", "architect", "complex", "comprehensive"}
	for _, kw := range complexKeywords {
		if strings.Contains(queryLower, kw) && costSensitivity < 0.5 {
			return meta.TierPowerful
		}
	}

	// Default to balanced
	return meta.TierBalanced
}

// explainChoice generates a human-readable explanation.
func (r *LearnedRouter) explainChoice(model *meta.ModelSpec, queryType string, score float64) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("Selected %s (tier: %s)", model.Name, tierName(model.Tier)))

	if r.learner != nil && queryType != "" {
		adj := r.learner.GetRoutingAdjustment(queryType, model.ID)
		if adj > 0.1 {
			parts = append(parts, "boosted by positive learning signal")
		} else if adj < -0.1 {
			parts = append(parts, "despite negative learning signal")
		}
	}

	if len(model.Strengths) > 0 {
		parts = append(parts, fmt.Sprintf("strengths: %s", strings.Join(model.Strengths[:min(3, len(model.Strengths))], ", ")))
	}

	return strings.Join(parts, "; ")
}

// SelectModel implements meta.ModelSelector interface.
func (r *LearnedRouter) SelectModel(ctx context.Context, task string, budget int, depth int) *meta.ModelSpec {
	// Determine query type from task
	queryType := classifyQuery(task)

	// Adjust cost sensitivity based on budget and depth
	costSensitivity := r.costSensitivity
	if budget < 1000 {
		costSensitivity = 0.9 // Very cost sensitive
	} else if budget < 5000 {
		costSensitivity = 0.7
	}
	if depth >= 3 {
		costSensitivity = max(costSensitivity, 0.8) // Prefer cheap at high depth
	}

	decision := r.Route(ctx, task, queryType, costSensitivity)
	return decision.Model
}

// CascadeRoute tries models in order until confidence threshold is met.
func (r *LearnedRouter) CascadeRoute(
	ctx context.Context,
	query string,
	queryType string,
	executor func(ctx context.Context, model *meta.ModelSpec, query string) (response string, confidence float64, cost float64, latencyMS int64, err error),
) (*CascadeResult, error) {
	result := &CascadeResult{}
	start := time.Now()

	for _, tier := range r.cascadeOrder {
		candidates := r.modelsByTier[tier]
		if len(candidates) == 0 {
			continue
		}

		// Pick best model for this tier
		var best *meta.ModelSpec
		var bestScore float64

		for _, model := range candidates {
			score := r.scoreModel(model, queryType, r.costSensitivity)
			if best == nil || score > bestScore {
				best = model
				bestScore = score
			}
		}

		if best == nil {
			continue
		}

		result.Attempts++

		r.logger.Debug("cascade attempt",
			"tier", tierName(tier),
			"model", best.ID,
			"attempt", result.Attempts)

		// Execute with this model
		response, confidence, cost, latencyMS, err := executor(ctx, best, query)
		result.TotalCost += cost
		result.TotalLatencyMS += latencyMS

		if err != nil {
			r.logger.Warn("cascade attempt failed",
				"model", best.ID,
				"error", err)
			continue // Try next tier
		}

		// Check confidence threshold
		if confidence >= r.confidenceThreshold {
			result.Response = response
			result.FinalModel = best
			result.Escalated = result.Attempts > 1

			r.logger.Info("cascade complete",
				"model", best.ID,
				"confidence", confidence,
				"attempts", result.Attempts,
				"escalated", result.Escalated,
				"total_cost", result.TotalCost,
				"duration_ms", time.Since(start).Milliseconds())

			// Record successful outcome for learning
			if r.learner != nil {
				r.learner.RecordOutcome(learning.ExecutionOutcome{
					Query:         query,
					QueryFeatures: learning.QueryFeatures{Category: queryType},
					StrategyUsed:  "cascade",
					ModelUsed:     best.ID,
					DepthReached:  result.Attempts,
					Success:       true,
					QualityScore:  confidence,
					Cost:          result.TotalCost,
					LatencyMS:     result.TotalLatencyMS,
					Timestamp:     time.Now(),
				})
			}

			r.mu.Lock()
			if result.Escalated {
				r.totalEscalations++
			}
			r.mu.Unlock()

			return result, nil
		}

		r.logger.Debug("confidence below threshold, escalating",
			"model", best.ID,
			"confidence", confidence,
			"threshold", r.confidenceThreshold)
	}

	// All tiers exhausted
	if result.Response != "" {
		return result, nil
	}

	return nil, fmt.Errorf("all cascade tiers exhausted without successful response")
}

// Stats returns routing statistics.
func (r *LearnedRouter) Stats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routesCopy := make(map[string]int64)
	for k, v := range r.routesByModel {
		routesCopy[k] = v
	}

	return RouterStats{
		TotalRoutes:      r.totalRoutes,
		TotalEscalations: r.totalEscalations,
		RoutesByModel:    routesCopy,
	}
}

// RouterStats contains routing statistics.
type RouterStats struct {
	TotalRoutes      int64            `json:"total_routes"`
	TotalEscalations int64            `json:"total_escalations"`
	RoutesByModel    map[string]int64 `json:"routes_by_model"`
}

// EscalationRate returns the percentage of routes that required escalation.
func (s RouterStats) EscalationRate() float64 {
	if s.TotalRoutes == 0 {
		return 0
	}
	return float64(s.TotalEscalations) / float64(s.TotalRoutes) * 100
}

// classifyQuery determines the query type from content.
func classifyQuery(query string) string {
	queryLower := strings.ToLower(query)

	// Check for code-related queries
	codeKeywords := []string{"code", "function", "implement", "bug", "error", "compile", "test"}
	for _, kw := range codeKeywords {
		if strings.Contains(queryLower, kw) {
			return "code"
		}
	}

	// Check for analysis queries
	analysisKeywords := []string{"analyze", "explain", "review", "assess", "evaluate"}
	for _, kw := range analysisKeywords {
		if strings.Contains(queryLower, kw) {
			return "analysis"
		}
	}

	// Check for reasoning queries
	reasoningKeywords := []string{"prove", "derive", "calculate", "logic", "math"}
	for _, kw := range reasoningKeywords {
		if strings.Contains(queryLower, kw) {
			return "reasoning"
		}
	}

	// Default
	return "general"
}

// tierName returns the tier as a string.
func tierName(tier meta.ModelTier) string {
	switch tier {
	case meta.TierFast:
		return "fast"
	case meta.TierBalanced:
		return "balanced"
	case meta.TierPowerful:
		return "powerful"
	case meta.TierReasoning:
		return "reasoning"
	default:
		return "unknown"
	}
}

// clamp constrains value to [min, max].
func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
