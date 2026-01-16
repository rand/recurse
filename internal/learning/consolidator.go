package learning

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"
)

// Consolidator merges similar knowledge, applies decay, and prunes stale items.
type Consolidator struct {
	store  *Store
	logger *slog.Logger

	// Configuration
	cfg ConsolidatorConfig

	// Background worker
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// ConsolidatorConfig configures the knowledge consolidator.
type ConsolidatorConfig struct {
	// DecayHalfLife is the time after which confidence halves without access.
	// Default: 7 days (Ebbinghaus-inspired).
	DecayHalfLife time.Duration

	// MinConfidence is the threshold below which items are pruned.
	// Default: 0.1
	MinConfidence float64

	// MergeSimilarityThreshold is cosine similarity above which facts merge.
	// Default: 0.9
	MergeSimilarityThreshold float64

	// ConsolidationInterval is how often background consolidation runs.
	// Default: 1 hour
	ConsolidationInterval time.Duration

	// MaxItemsPerRun limits items processed per consolidation run.
	// Default: 1000
	MaxItemsPerRun int

	// Logger for consolidation operations.
	Logger *slog.Logger
}

// NewConsolidator creates a new knowledge consolidator.
func NewConsolidator(store *Store, cfg ConsolidatorConfig) *Consolidator {
	// Apply defaults
	if cfg.DecayHalfLife == 0 {
		cfg.DecayHalfLife = 7 * 24 * time.Hour
	}
	if cfg.MinConfidence == 0 {
		cfg.MinConfidence = 0.1
	}
	if cfg.MergeSimilarityThreshold == 0 {
		cfg.MergeSimilarityThreshold = 0.9
	}
	if cfg.ConsolidationInterval == 0 {
		cfg.ConsolidationInterval = 1 * time.Hour
	}
	if cfg.MaxItemsPerRun == 0 {
		cfg.MaxItemsPerRun = 1000
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Consolidator{
		store:  store,
		logger: cfg.Logger,
		cfg:    cfg,
	}
}

// Start begins background consolidation.
func (c *Consolidator) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return
	}

	c.running = true
	c.stopCh = make(chan struct{})
	c.doneCh = make(chan struct{})

	go c.run()
}

// Stop halts background consolidation.
func (c *Consolidator) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	close(c.stopCh)
	c.mu.Unlock()

	<-c.doneCh
}

// run is the background consolidation loop.
func (c *Consolidator) run() {
	defer close(c.doneCh)

	ticker := time.NewTicker(c.cfg.ConsolidationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := c.ConsolidateAll(ctx); err != nil {
				c.logger.Error("consolidation failed", "error", err)
			}
			cancel()
		}
	}
}

// ConsolidateAll runs a full consolidation pass.
func (c *Consolidator) ConsolidateAll(ctx context.Context) error {
	c.logger.Info("starting consolidation")
	start := time.Now()

	var totalProcessed, totalDecayed, totalPruned, totalMerged int

	// Process facts
	facts, err := c.store.ListFacts(ctx, "", 0, c.cfg.MaxItemsPerRun)
	if err != nil {
		return err
	}
	for _, fact := range facts {
		processed, action := c.processFact(ctx, fact)
		if processed {
			totalProcessed++
			switch action {
			case "decayed":
				totalDecayed++
			case "pruned":
				totalPruned++
			case "merged":
				totalMerged++
			}
		}
	}

	// Process patterns
	patterns, err := c.store.ListPatterns(ctx, "", c.cfg.MaxItemsPerRun)
	if err != nil {
		return err
	}
	for _, pattern := range patterns {
		if c.processPattern(ctx, pattern) {
			totalProcessed++
		}
	}

	// Process constraints
	constraints, err := c.store.ListConstraints(ctx, "", 0)
	if err != nil {
		return err
	}
	for _, constraint := range constraints {
		if c.processConstraint(ctx, constraint) {
			totalProcessed++
		}
	}

	c.logger.Info("consolidation complete",
		"duration", time.Since(start),
		"processed", totalProcessed,
		"decayed", totalDecayed,
		"pruned", totalPruned,
		"merged", totalMerged,
	)

	return nil
}

// processFact applies decay and pruning to a fact.
func (c *Consolidator) processFact(ctx context.Context, fact *LearnedFact) (bool, string) {
	// Calculate decay based on last access
	newConfidence := c.calculateDecay(fact.Confidence, fact.LastAccessed, fact.AccessCount)

	if newConfidence < c.cfg.MinConfidence {
		// Prune low-confidence facts
		if err := c.store.DeleteFact(ctx, fact.ID); err != nil {
			c.logger.Debug("failed to prune fact", "id", fact.ID, "error", err)
		}
		return true, "pruned"
	}

	if newConfidence != fact.Confidence {
		fact.Confidence = newConfidence
		if err := c.store.UpdateFact(ctx, fact); err != nil {
			c.logger.Debug("failed to update fact", "id", fact.ID, "error", err)
		}
		return true, "decayed"
	}

	return false, ""
}

// processPattern applies decay to a pattern.
func (c *Consolidator) processPattern(ctx context.Context, pattern *LearnedPattern) bool {
	// Patterns decay based on last use
	newRate := c.calculateDecay(pattern.SuccessRate, pattern.LastUsed, pattern.UsageCount)

	if newRate < c.cfg.MinConfidence {
		if err := c.store.DeletePattern(ctx, pattern.ID); err != nil {
			c.logger.Debug("failed to prune pattern", "id", pattern.ID, "error", err)
		} else {
			c.logger.Debug("pruned pattern", "id", pattern.ID, "name", pattern.Name)
		}
		return true
	}

	if newRate != pattern.SuccessRate {
		pattern.SuccessRate = newRate
		if err := c.store.UpdatePattern(ctx, pattern); err != nil {
			c.logger.Debug("failed to decay pattern", "id", pattern.ID, "error", err)
		} else {
			c.logger.Debug("decayed pattern", "id", pattern.ID, "old", pattern.SuccessRate, "new", newRate)
		}
		return true
	}

	return false
}

// processConstraint applies decay to a constraint.
func (c *Consolidator) processConstraint(ctx context.Context, constraint *LearnedConstraint) bool {
	// Constraints decay more slowly (severity-based)
	if constraint.Source == SourceExplicit {
		// Explicit constraints don't decay
		return false
	}

	newSeverity := c.calculateDecay(constraint.Severity, constraint.LastTriggered, constraint.ViolationCount)

	if newSeverity < c.cfg.MinConfidence/2 { // Constraints have lower prune threshold
		if err := c.store.DeleteConstraint(ctx, constraint.ID); err != nil {
			c.logger.Debug("failed to prune constraint", "id", constraint.ID, "error", err)
		} else {
			c.logger.Debug("pruned constraint", "id", constraint.ID)
		}
		return true
	}

	// Update constraint with decayed severity if changed significantly
	if newSeverity < constraint.Severity*0.9 { // Only update if decayed by more than 10%
		constraint.Severity = newSeverity
		if err := c.store.UpdateConstraint(ctx, constraint); err != nil {
			c.logger.Debug("failed to decay constraint", "id", constraint.ID, "error", err)
		} else {
			c.logger.Debug("decayed constraint", "id", constraint.ID, "new_severity", newSeverity)
		}
		return true
	}

	return false
}

// calculateDecay computes new confidence using Ebbinghaus-inspired decay.
// Decay is slower with more access (spaced repetition effect).
func (c *Consolidator) calculateDecay(currentConfidence float64, lastAccess time.Time, accessCount int) float64 {
	if lastAccess.IsZero() {
		return currentConfidence
	}

	elapsed := time.Since(lastAccess)
	if elapsed < 0 {
		return currentConfidence
	}

	// Ebbinghaus forgetting curve: R = e^(-t/S)
	// where S (stability) increases with repetition
	// S = halfLife * (1 + log(1 + accessCount))
	stability := c.cfg.DecayHalfLife.Seconds() * (1 + math.Log(1+float64(accessCount)))
	retention := math.Exp(-elapsed.Seconds() / stability)

	// Apply retention to confidence
	newConfidence := currentConfidence * retention

	// Also factor in success rate for facts
	return math.Max(newConfidence, c.cfg.MinConfidence/2)
}

// MergeSimilarFacts finds and merges similar facts.
func (c *Consolidator) MergeSimilarFacts(ctx context.Context, domain string) (int, error) {
	facts, err := c.store.ListFacts(ctx, domain, 0, c.cfg.MaxItemsPerRun)
	if err != nil {
		return 0, err
	}

	merged := 0
	seen := make(map[string]bool)

	for i, f1 := range facts {
		if seen[f1.ID] {
			continue
		}

		for j := i + 1; j < len(facts); j++ {
			f2 := facts[j]
			if seen[f2.ID] {
				continue
			}

			// Check content similarity (simple for now, would use embeddings)
			if contentSimilar(f1.Content, f2.Content) {
				// Merge f2 into f1
				f1.SuccessCount += f2.SuccessCount
				f1.FailureCount += f2.FailureCount
				f1.Confidence = (f1.Confidence + f2.Confidence) / 2
				f1.AccessCount += f2.AccessCount

				if err := c.store.UpdateFact(ctx, f1); err != nil {
					continue
				}
				if err := c.store.DeleteFact(ctx, f2.ID); err != nil {
					continue
				}

				seen[f2.ID] = true
				merged++
			}
		}
	}

	return merged, nil
}

// ConsolidationStats returns statistics about the knowledge store.
type ConsolidationStats struct {
	TotalFacts       int     `json:"total_facts"`
	TotalPatterns    int     `json:"total_patterns"`
	TotalConstraints int     `json:"total_constraints"`
	TotalPreferences int     `json:"total_preferences"`
	AvgConfidence    float64 `json:"avg_confidence"`
	StaleItems       int     `json:"stale_items"` // Items below decay threshold
}

// GetStats returns current consolidation statistics.
func (c *Consolidator) GetStats(ctx context.Context) (*ConsolidationStats, error) {
	stats, err := c.store.Stats(ctx)
	if err != nil {
		return nil, err
	}

	return &ConsolidationStats{
		TotalFacts:       stats.FactCount,
		TotalPatterns:    stats.PatternCount,
		TotalConstraints: stats.ConstraintCount,
		TotalPreferences: stats.PreferenceCount,
	}, nil
}
