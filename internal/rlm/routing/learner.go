package routing

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Learner updates model profiles based on routing outcomes.
type Learner struct {
	mu sync.Mutex

	store  *Store
	router *Router
	config LearnerConfig

	// Pending updates batched for efficiency
	pending []pendingUpdate
}

// LearnerConfig configures the learner behavior.
type LearnerConfig struct {
	// LearningRate controls how much each outcome affects scores (default 0.1).
	LearningRate float64

	// MinSamples is minimum samples before updating category scores (default 5).
	MinSamples int

	// DecayFactor for exponential moving average (default 0.95).
	DecayFactor float64

	// BatchSize is updates to accumulate before persisting (default 10).
	BatchSize int

	// SuccessBoost is bonus for successful outcomes (default 0.1).
	SuccessBoost float64

	// FailurePenalty is penalty for failed outcomes (default 0.15).
	FailurePenalty float64
}

type pendingUpdate struct {
	modelID  string
	category TaskCategory
	outcome  RoutingOutcome
	latency  time.Duration
}

// NewLearner creates a new routing learner.
func NewLearner(store *Store, router *Router, cfg LearnerConfig) *Learner {
	if cfg.LearningRate <= 0 {
		cfg.LearningRate = 0.1
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 5
	}
	if cfg.DecayFactor <= 0 {
		cfg.DecayFactor = 0.95
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.SuccessBoost <= 0 {
		cfg.SuccessBoost = 0.1
	}
	if cfg.FailurePenalty <= 0 {
		cfg.FailurePenalty = 0.15
	}

	return &Learner{
		store:   store,
		router:  router,
		config:  cfg,
		pending: make([]pendingUpdate, 0, cfg.BatchSize),
	}
}

// RecordOutcome records the outcome of a routing decision.
func (l *Learner) RecordOutcome(ctx context.Context, entry *RoutingHistoryEntry) error {
	// Store the history entry
	if l.store != nil {
		if err := l.store.RecordHistory(ctx, entry); err != nil {
			return fmt.Errorf("record history: %w", err)
		}
	}

	// Queue update
	l.mu.Lock()
	category := CategoryConversation
	if entry.Features != nil {
		category = entry.Features.Category
	}
	l.pending = append(l.pending, pendingUpdate{
		modelID:  entry.ModelUsed,
		category: category,
		outcome:  entry.Outcome,
		latency:  time.Duration(entry.LatencyMS) * time.Millisecond,
	})

	// Flush if batch is full
	shouldFlush := len(l.pending) >= l.config.BatchSize
	l.mu.Unlock()

	if shouldFlush {
		return l.Flush(ctx)
	}
	return nil
}

// Flush applies pending updates to model profiles.
func (l *Learner) Flush(ctx context.Context) error {
	l.mu.Lock()
	updates := l.pending
	l.pending = make([]pendingUpdate, 0, l.config.BatchSize)
	l.mu.Unlock()

	if len(updates) == 0 {
		return nil
	}

	// Group updates by model
	byModel := make(map[string][]pendingUpdate)
	for _, u := range updates {
		byModel[u.modelID] = append(byModel[u.modelID], u)
	}

	// Apply updates to each model
	for modelID, modelUpdates := range byModel {
		if err := l.applyUpdates(ctx, modelID, modelUpdates); err != nil {
			// Log but continue with other models
			continue
		}
	}

	return nil
}

// applyUpdates applies accumulated updates to a model profile.
func (l *Learner) applyUpdates(ctx context.Context, modelID string, updates []pendingUpdate) error {
	// Get current profile from router or store
	profile := l.router.GetProfile(modelID)
	if profile == nil && l.store != nil {
		var err error
		profile, err = l.store.GetProfile(ctx, modelID)
		if err != nil {
			return err
		}
	}
	if profile == nil {
		// Create minimal profile for unknown model
		profile = &ModelProfile{
			ID:             modelID,
			CategoryScores: make(map[TaskCategory]float64),
		}
	}

	// Group by category
	byCategory := make(map[TaskCategory][]pendingUpdate)
	for _, u := range updates {
		byCategory[u.category] = append(byCategory[u.category], u)
	}

	// Update category scores
	for category, catUpdates := range byCategory {
		l.updateCategoryScore(profile, category, catUpdates)
	}

	// Update overall success rate
	l.updateSuccessRate(profile, updates)

	// Update latency stats
	l.updateLatencyStats(profile, updates)

	profile.UpdatedAt = time.Now()

	// Update router's in-memory profile
	l.router.AddProfile(profile)

	// Persist if store available
	if l.store != nil {
		return l.store.SaveProfile(ctx, profile)
	}

	return nil
}

// updateCategoryScore updates the score for a specific category.
func (l *Learner) updateCategoryScore(profile *ModelProfile, category TaskCategory, updates []pendingUpdate) {
	if profile.CategoryScores == nil {
		profile.CategoryScores = make(map[TaskCategory]float64)
	}

	currentScore := profile.GetCategoryScore(category)

	// Calculate average outcome weight
	var totalWeight float64
	for _, u := range updates {
		totalWeight += u.outcome.OutcomeWeight()
	}
	avgWeight := totalWeight / float64(len(updates))

	// Apply exponential moving average
	newScore := l.config.DecayFactor*currentScore + (1-l.config.DecayFactor)*avgWeight

	// Apply boost/penalty based on outcomes
	successCount := 0
	failCount := 0
	for _, u := range updates {
		if u.outcome == OutcomeSuccess {
			successCount++
		} else if u.outcome == OutcomeFailed {
			failCount++
		}
	}

	if successCount > failCount {
		newScore += l.config.SuccessBoost * l.config.LearningRate
	} else if failCount > successCount {
		newScore -= l.config.FailurePenalty * l.config.LearningRate
	}

	profile.SetCategoryScore(category, newScore)
}

// updateSuccessRate updates the overall success rate.
func (l *Learner) updateSuccessRate(profile *ModelProfile, updates []pendingUpdate) {
	successCount := 0
	for _, u := range updates {
		if u.outcome == OutcomeSuccess {
			successCount++
		}
	}

	newRate := float64(successCount) / float64(len(updates))

	// Exponential moving average with existing rate
	if profile.SuccessRate == 0 {
		profile.SuccessRate = newRate
	} else {
		profile.SuccessRate = l.config.DecayFactor*profile.SuccessRate + (1-l.config.DecayFactor)*newRate
	}
}

// updateLatencyStats updates latency statistics.
func (l *Learner) updateLatencyStats(profile *ModelProfile, updates []pendingUpdate) {
	var totalLatency time.Duration
	var maxLatency time.Duration
	count := 0

	for _, u := range updates {
		if u.latency > 0 {
			totalLatency += u.latency
			count++
			if u.latency > maxLatency {
				maxLatency = u.latency
			}
		}
	}

	if count == 0 {
		return
	}

	newMedian := totalLatency / time.Duration(count)

	// Update with EMA
	if profile.MedianLatency == 0 {
		profile.MedianLatency = newMedian
	} else {
		profile.MedianLatency = time.Duration(
			l.config.DecayFactor*float64(profile.MedianLatency) +
				(1-l.config.DecayFactor)*float64(newMedian),
		)
	}

	// Update P95 (approximation using max from batch)
	if profile.P95Latency == 0 || maxLatency > profile.P95Latency {
		profile.P95Latency = maxLatency
	} else {
		profile.P95Latency = time.Duration(
			l.config.DecayFactor*float64(profile.P95Latency) +
				(1-l.config.DecayFactor)*float64(maxLatency),
		)
	}
}

// LearnFromFeedback processes explicit user feedback.
func (l *Learner) LearnFromFeedback(ctx context.Context, modelID string, category TaskCategory, rating int) error {
	// Convert 1-5 rating to outcome
	var outcome RoutingOutcome
	switch {
	case rating >= 4:
		outcome = OutcomeSuccess
	case rating >= 2:
		outcome = OutcomeCorrected
	default:
		outcome = OutcomeFailed
	}

	entry := &RoutingHistoryEntry{
		Timestamp: time.Now(),
		ModelUsed: modelID,
		Features:  &TaskFeatures{Category: category},
		Outcome:   outcome,
		UserRating: &rating,
	}

	return l.RecordOutcome(ctx, entry)
}

// GetModelPerformance returns performance metrics for a model.
func (l *Learner) GetModelPerformance(ctx context.Context, modelID string) (*ModelPerformance, error) {
	if l.store == nil {
		return nil, fmt.Errorf("no store configured")
	}

	history, err := l.store.GetHistoryByModel(ctx, modelID, 100)
	if err != nil {
		return nil, err
	}

	perf := &ModelPerformance{
		ModelID:          modelID,
		TotalRequests:    len(history),
		CategoryBreakdown: make(map[TaskCategory]CategoryPerformance),
	}

	// Calculate metrics
	var totalLatency time.Duration
	var totalCost float64
	categoryStats := make(map[TaskCategory]*categoryAcc)

	for _, h := range history {
		totalLatency += time.Duration(h.LatencyMS) * time.Millisecond
		totalCost += h.Cost

		switch h.Outcome {
		case OutcomeSuccess:
			perf.SuccessCount++
		case OutcomeCorrected:
			perf.CorrectedCount++
		case OutcomeFailed:
			perf.FailedCount++
		}

		if h.Features != nil {
			cat := h.Features.Category
			if categoryStats[cat] == nil {
				categoryStats[cat] = &categoryAcc{}
			}
			categoryStats[cat].total++
			if h.Outcome == OutcomeSuccess {
				categoryStats[cat].success++
			}
		}
	}

	if perf.TotalRequests > 0 {
		perf.SuccessRate = float64(perf.SuccessCount) / float64(perf.TotalRequests)
		perf.AvgLatency = totalLatency / time.Duration(perf.TotalRequests)
		perf.TotalCost = totalCost
	}

	for cat, acc := range categoryStats {
		perf.CategoryBreakdown[cat] = CategoryPerformance{
			Requests:    acc.total,
			SuccessRate: float64(acc.success) / float64(acc.total),
		}
	}

	return perf, nil
}

type categoryAcc struct {
	total   int
	success int
}

// ModelPerformance contains performance metrics for a model.
type ModelPerformance struct {
	ModelID           string                           `json:"model_id"`
	TotalRequests     int                              `json:"total_requests"`
	SuccessCount      int                              `json:"success_count"`
	CorrectedCount    int                              `json:"corrected_count"`
	FailedCount       int                              `json:"failed_count"`
	SuccessRate       float64                          `json:"success_rate"`
	AvgLatency        time.Duration                    `json:"avg_latency"`
	TotalCost         float64                          `json:"total_cost"`
	CategoryBreakdown map[TaskCategory]CategoryPerformance `json:"category_breakdown"`
}

// CategoryPerformance contains performance metrics for a category.
type CategoryPerformance struct {
	Requests    int     `json:"requests"`
	SuccessRate float64 `json:"success_rate"`
}

// SuggestWeightAdjustments analyzes history and suggests weight changes.
func (l *Learner) SuggestWeightAdjustments(ctx context.Context) (*WeightSuggestion, error) {
	if l.store == nil {
		return nil, fmt.Errorf("no store configured")
	}

	history, err := l.store.GetRecentHistory(ctx, 200)
	if err != nil {
		return nil, err
	}

	if len(history) < 20 {
		return nil, fmt.Errorf("insufficient history for analysis (need 20+, have %d)", len(history))
	}

	stats := l.router.Stats()
	suggestion := &WeightSuggestion{
		CurrentWeights:   stats.CurrentWeights,
		SuggestedWeights: stats.CurrentWeights,
		Reasons:          make([]string, 0),
	}

	// Analyze cost vs quality trade-off
	var highCostSuccess, lowCostSuccess int
	var highCostTotal, lowCostTotal int
	medianCost := l.calculateMedianCost(history)

	for _, h := range history {
		if h.Cost > medianCost {
			highCostTotal++
			if h.Outcome == OutcomeSuccess {
				highCostSuccess++
			}
		} else {
			lowCostTotal++
			if h.Outcome == OutcomeSuccess {
				lowCostSuccess++
			}
		}
	}

	// If low-cost models perform nearly as well, suggest increasing cost weight
	if lowCostTotal > 0 && highCostTotal > 0 {
		lowCostRate := float64(lowCostSuccess) / float64(lowCostTotal)
		highCostRate := float64(highCostSuccess) / float64(highCostTotal)

		if lowCostRate >= highCostRate*0.9 {
			suggestion.SuggestedWeights.Cost += 0.1
			suggestion.SuggestedWeights.Quality -= 0.05
			suggestion.Reasons = append(suggestion.Reasons,
				fmt.Sprintf("cheaper models perform nearly as well (%.0f%% vs %.0f%%)",
					lowCostRate*100, highCostRate*100))
		} else if highCostRate > lowCostRate*1.2 {
			suggestion.SuggestedWeights.Quality += 0.1
			suggestion.SuggestedWeights.Cost -= 0.05
			suggestion.Reasons = append(suggestion.Reasons,
				fmt.Sprintf("expensive models significantly outperform (%.0f%% vs %.0f%%)",
					highCostRate*100, lowCostRate*100))
		}
	}

	suggestion.SuggestedWeights.Normalize()
	return suggestion, nil
}

func (l *Learner) calculateMedianCost(history []*RoutingHistoryEntry) float64 {
	if len(history) == 0 {
		return 0
	}

	costs := make([]float64, 0, len(history))
	for _, h := range history {
		costs = append(costs, h.Cost)
	}

	// Simple median calculation (could use sort for exact median)
	var total float64
	for _, c := range costs {
		total += c
	}
	return total / float64(len(costs))
}

// WeightSuggestion contains weight adjustment suggestions.
type WeightSuggestion struct {
	CurrentWeights   ScoringWeights `json:"current_weights"`
	SuggestedWeights ScoringWeights `json:"suggested_weights"`
	Reasons          []string       `json:"reasons"`
}
