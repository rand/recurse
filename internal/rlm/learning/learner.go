// Package learning implements continuous learning from execution outcomes.
package learning

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

//go:embed schema.sql
var schemaSQL string

// QueryFeatures represents characteristics of a query for learning.
type QueryFeatures struct {
	// Category is the classified query type (e.g., "code", "analysis", "chat").
	Category string `json:"category"`

	// Complexity is an estimate of task complexity (0.0-1.0).
	Complexity float64 `json:"complexity"`

	// EstimatedTokens is the expected output size.
	EstimatedTokens int `json:"estimated_tokens"`

	// RequiresReasoning indicates if multi-step reasoning is needed.
	RequiresReasoning bool `json:"requires_reasoning"`

	// RequiresCode indicates if code generation is involved.
	RequiresCode bool `json:"requires_code"`

	// Domain is the subject area (e.g., "go", "python", "kubernetes").
	Domain string `json:"domain"`
}

// ExecutionOutcome captures the result of an RLM execution for learning.
type ExecutionOutcome struct {
	// Query is the original task/prompt.
	Query string `json:"query"`

	// QueryFeatures are the extracted features of the query.
	QueryFeatures QueryFeatures `json:"query_features"`

	// StrategyUsed is the orchestration strategy (e.g., "chain", "tree", "lats").
	StrategyUsed string `json:"strategy_used"`

	// ModelUsed is the primary model ID used.
	ModelUsed string `json:"model_used"`

	// DepthReached is the recursion depth achieved.
	DepthReached int `json:"depth_reached"`

	// ToolsUsed lists tools/capabilities invoked.
	ToolsUsed []string `json:"tools_used"`

	// Success indicates if the task completed successfully.
	Success bool `json:"success"`

	// QualityScore is the output quality (0.0-1.0, may be from user feedback).
	QualityScore float64 `json:"quality_score"`

	// Cost is the total cost in USD.
	Cost float64 `json:"cost"`

	// LatencyMS is the execution time in milliseconds.
	LatencyMS int64 `json:"latency_ms"`

	// Timestamp is when the execution occurred.
	Timestamp time.Time `json:"timestamp"`
}

// LearnerConfig configures the ContinuousLearner.
type LearnerConfig struct {
	// LearningRate controls how quickly adjustments change (default 0.1).
	LearningRate float64

	// DecayRate controls how old observations decay (default 0.95 per day).
	DecayRate float64

	// MinObservations is required before making routing adjustments (default 3).
	MinObservations int

	// MaxAdjustment limits how much any adjustment can deviate from 0 (default 0.5).
	MaxAdjustment float64

	// DBPath is the path to the SQLite database for persistence.
	// If empty, uses in-memory storage (no persistence).
	DBPath string

	// Logger for learning operations.
	Logger *slog.Logger
}

// DefaultLearnerConfig returns sensible defaults.
func DefaultLearnerConfig() LearnerConfig {
	return LearnerConfig{
		LearningRate:    0.1,
		DecayRate:       0.95,
		MinObservations: 3,
		MaxAdjustment:   0.5,
	}
}

// ContinuousLearner learns from execution outcomes to improve routing decisions.
type ContinuousLearner struct {
	mu sync.RWMutex

	// routingAdjustments maps "queryType:model" to adjustment factor (-1 to 1).
	// Positive = prefer this model for this query type.
	routingAdjustments map[string]float64

	// strategyPreferences maps "queryType:strategy" to preference score.
	strategyPreferences map[string]float64

	// observationCounts tracks how many observations per key.
	observationCounts map[string]int

	// lastObservation tracks when each key was last updated.
	lastObservation map[string]time.Time

	config     LearnerConfig
	db         *sql.DB
	logger     *slog.Logger
	closeOnce  sync.Once
	shutdownCh chan struct{}
}

// NewContinuousLearner creates a new learner with the given config.
func NewContinuousLearner(cfg LearnerConfig) (*ContinuousLearner, error) {
	if cfg.LearningRate <= 0 {
		cfg.LearningRate = 0.1
	}
	if cfg.DecayRate <= 0 {
		cfg.DecayRate = 0.95
	}
	if cfg.MinObservations <= 0 {
		cfg.MinObservations = 3
	}
	if cfg.MaxAdjustment <= 0 {
		cfg.MaxAdjustment = 0.5
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	learner := &ContinuousLearner{
		routingAdjustments:  make(map[string]float64),
		strategyPreferences: make(map[string]float64),
		observationCounts:   make(map[string]int),
		lastObservation:     make(map[string]time.Time),
		config:              cfg,
		logger:              logger,
		shutdownCh:          make(chan struct{}),
	}

	// Initialize persistence if path provided
	if cfg.DBPath != "" {
		if err := learner.initDB(cfg.DBPath); err != nil {
			return nil, fmt.Errorf("init database: %w", err)
		}

		// Load persisted state
		if err := learner.loadState(); err != nil {
			logger.Warn("failed to load persisted state", "error", err)
		}
	}

	return learner, nil
}

// initDB initializes the SQLite database.
func (l *ContinuousLearner) initDB(path string) error {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("ping database: %w", err)
	}

	// Initialize schema
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return fmt.Errorf("execute schema: %w", err)
	}

	l.db = db
	return nil
}

// loadState loads persisted adjustments from the database.
func (l *ContinuousLearner) loadState() error {
	if l.db == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Load routing adjustments
	rows, err := l.db.Query(`
		SELECT key, adjustment, observation_count, last_observed
		FROM routing_adjustments
	`)
	if err != nil {
		return fmt.Errorf("query routing adjustments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var adjustment float64
		var count int
		var lastObserved time.Time

		if err := rows.Scan(&key, &adjustment, &count, &lastObserved); err != nil {
			return fmt.Errorf("scan routing adjustment: %w", err)
		}

		// Apply decay based on time since last observation
		age := time.Since(lastObserved)
		decayedAdjustment := l.applyDecay(adjustment, age)

		l.routingAdjustments[key] = decayedAdjustment
		l.observationCounts[key] = count
		l.lastObservation[key] = lastObserved
	}

	// Load strategy preferences
	rows2, err := l.db.Query(`
		SELECT key, preference, observation_count, last_observed
		FROM strategy_preferences
	`)
	if err != nil {
		return fmt.Errorf("query strategy preferences: %w", err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var key string
		var pref float64
		var count int
		var lastObserved time.Time

		if err := rows2.Scan(&key, &pref, &count, &lastObserved); err != nil {
			return fmt.Errorf("scan strategy preference: %w", err)
		}

		age := time.Since(lastObserved)
		decayedPref := l.applyDecay(pref, age)

		l.strategyPreferences[key] = decayedPref
		l.observationCounts["strat:"+key] = count
		l.lastObservation["strat:"+key] = lastObserved
	}

	l.logger.Info("loaded learning state",
		"routing_adjustments", len(l.routingAdjustments),
		"strategy_preferences", len(l.strategyPreferences))

	return nil
}

// applyDecay applies temporal decay to a value.
func (l *ContinuousLearner) applyDecay(value float64, age time.Duration) float64 {
	days := age.Hours() / 24.0
	decayFactor := math.Pow(l.config.DecayRate, days)
	return value * decayFactor
}

// RecordOutcome records an execution outcome and updates adjustments.
func (l *ContinuousLearner) RecordOutcome(outcome ExecutionOutcome) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Extract routing key: "category:model"
	routingKey := fmt.Sprintf("%s:%s", outcome.QueryFeatures.Category, outcome.ModelUsed)

	// Extract strategy key: "category:strategy"
	strategyKey := fmt.Sprintf("%s:%s", outcome.QueryFeatures.Category, outcome.StrategyUsed)

	// Compute reward signal based on outcome
	reward := l.computeReward(outcome)

	// Update routing adjustment
	currentAdj := l.routingAdjustments[routingKey]
	newAdj := currentAdj + l.config.LearningRate*(reward-currentAdj)
	newAdj = clamp(newAdj, -l.config.MaxAdjustment, l.config.MaxAdjustment)
	l.routingAdjustments[routingKey] = newAdj
	l.observationCounts[routingKey]++
	l.lastObservation[routingKey] = time.Now()

	// Update strategy preference
	currentPref := l.strategyPreferences[strategyKey]
	newPref := currentPref + l.config.LearningRate*(reward-currentPref)
	newPref = clamp(newPref, -l.config.MaxAdjustment, l.config.MaxAdjustment)
	l.strategyPreferences[strategyKey] = newPref
	l.observationCounts["strat:"+strategyKey]++
	l.lastObservation["strat:"+strategyKey] = time.Now()

	l.logger.Debug("recorded outcome",
		"routing_key", routingKey,
		"strategy_key", strategyKey,
		"reward", reward,
		"new_routing_adj", newAdj,
		"new_strategy_pref", newPref)

	// Persist if database is available
	if l.db != nil {
		go l.persistAdjustments(routingKey, strategyKey)
	}
}

// computeReward calculates a reward signal from an outcome.
func (l *ContinuousLearner) computeReward(outcome ExecutionOutcome) float64 {
	// Base reward from success/failure
	var baseReward float64
	if outcome.Success {
		baseReward = 0.5
	} else {
		baseReward = -0.5
	}

	// Quality contribution (0-1 scaled to -0.3 to 0.3)
	qualityContrib := (outcome.QualityScore - 0.5) * 0.6

	// Cost efficiency contribution
	// Lower cost relative to complexity is good
	var costContrib float64
	if outcome.QueryFeatures.Complexity > 0 {
		expectedCost := outcome.QueryFeatures.Complexity * 0.01 // $0.01 per complexity unit
		if outcome.Cost < expectedCost {
			costContrib = 0.1 // Under budget
		} else if outcome.Cost > expectedCost*2 {
			costContrib = -0.1 // Way over budget
		}
	}

	// Latency contribution
	var latencyContrib float64
	expectedLatencyMS := int64(1000 + outcome.QueryFeatures.EstimatedTokens*2) // Rough estimate
	if outcome.LatencyMS < expectedLatencyMS {
		latencyContrib = 0.05
	} else if outcome.LatencyMS > expectedLatencyMS*3 {
		latencyContrib = -0.05
	}

	reward := baseReward + qualityContrib + costContrib + latencyContrib
	return clamp(reward, -1.0, 1.0)
}

// persistAdjustments saves adjustments to the database.
func (l *ContinuousLearner) persistAdjustments(routingKey, strategyKey string) {
	l.mu.RLock()
	routingAdj := l.routingAdjustments[routingKey]
	routingCount := l.observationCounts[routingKey]
	routingLast := l.lastObservation[routingKey]

	stratPref := l.strategyPreferences[strategyKey]
	stratCount := l.observationCounts["strat:"+strategyKey]
	stratLast := l.lastObservation["strat:"+strategyKey]
	l.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Upsert routing adjustment
	_, err := l.db.ExecContext(ctx, `
		INSERT INTO routing_adjustments (key, adjustment, observation_count, last_observed)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			adjustment = excluded.adjustment,
			observation_count = excluded.observation_count,
			last_observed = excluded.last_observed
	`, routingKey, routingAdj, routingCount, routingLast)

	if err != nil {
		l.logger.Error("failed to persist routing adjustment", "key", routingKey, "error", err)
	}

	// Upsert strategy preference
	_, err = l.db.ExecContext(ctx, `
		INSERT INTO strategy_preferences (key, preference, observation_count, last_observed)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			preference = excluded.preference,
			observation_count = excluded.observation_count,
			last_observed = excluded.last_observed
	`, strategyKey, stratPref, stratCount, stratLast)

	if err != nil {
		l.logger.Error("failed to persist strategy preference", "key", strategyKey, "error", err)
	}
}

// GetRoutingAdjustment returns the learned adjustment for a query type and model.
// Returns 0 if insufficient observations.
func (l *ContinuousLearner) GetRoutingAdjustment(queryType, model string) float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", queryType, model)

	// Check if we have enough observations
	if l.observationCounts[key] < l.config.MinObservations {
		return 0
	}

	// Apply decay based on time since last observation
	if lastObs, ok := l.lastObservation[key]; ok {
		age := time.Since(lastObs)
		return l.applyDecay(l.routingAdjustments[key], age)
	}

	return l.routingAdjustments[key]
}

// GetStrategyPreference returns the learned preference for a query type and strategy.
// Returns 0 if insufficient observations.
func (l *ContinuousLearner) GetStrategyPreference(queryType, strategy string) float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", queryType, strategy)
	countKey := "strat:" + key

	// Check if we have enough observations
	if l.observationCounts[countKey] < l.config.MinObservations {
		return 0
	}

	// Apply decay
	if lastObs, ok := l.lastObservation[countKey]; ok {
		age := time.Since(lastObs)
		return l.applyDecay(l.strategyPreferences[key], age)
	}

	return l.strategyPreferences[key]
}

// BestModelForQueryType returns the model with highest adjustment for a query type.
// Returns empty string if no preferences learned.
func (l *ContinuousLearner) BestModelForQueryType(queryType string, candidates []string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var bestModel string
	bestAdj := -math.MaxFloat64

	for _, model := range candidates {
		key := fmt.Sprintf("%s:%s", queryType, model)

		// Only consider models with enough observations
		if l.observationCounts[key] < l.config.MinObservations {
			continue
		}

		adj := l.routingAdjustments[key]
		if lastObs, ok := l.lastObservation[key]; ok {
			adj = l.applyDecay(adj, time.Since(lastObs))
		}

		if adj > bestAdj {
			bestAdj = adj
			bestModel = model
		}
	}

	return bestModel
}

// BestStrategyForQueryType returns the strategy with highest preference for a query type.
func (l *ContinuousLearner) BestStrategyForQueryType(queryType string, candidates []string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var bestStrategy string
	bestPref := -math.MaxFloat64

	for _, strategy := range candidates {
		key := fmt.Sprintf("%s:%s", queryType, strategy)
		countKey := "strat:" + key

		if l.observationCounts[countKey] < l.config.MinObservations {
			continue
		}

		pref := l.strategyPreferences[key]
		if lastObs, ok := l.lastObservation[countKey]; ok {
			pref = l.applyDecay(pref, time.Since(lastObs))
		}

		if pref > bestPref {
			bestPref = pref
			bestStrategy = strategy
		}
	}

	return bestStrategy
}

// Stats returns current learning statistics.
func (l *ContinuousLearner) Stats() LearnerStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	routingCopy := make(map[string]float64)
	for k, v := range l.routingAdjustments {
		routingCopy[k] = v
	}

	strategyCopy := make(map[string]float64)
	for k, v := range l.strategyPreferences {
		strategyCopy[k] = v
	}

	countCopy := make(map[string]int)
	for k, v := range l.observationCounts {
		countCopy[k] = v
	}

	return LearnerStats{
		RoutingAdjustments:  routingCopy,
		StrategyPreferences: strategyCopy,
		ObservationCounts:   countCopy,
	}
}

// LearnerStats contains learning statistics.
type LearnerStats struct {
	RoutingAdjustments  map[string]float64 `json:"routing_adjustments"`
	StrategyPreferences map[string]float64 `json:"strategy_preferences"`
	ObservationCounts   map[string]int     `json:"observation_counts"`
}

// ToJSON returns stats as JSON string.
func (s LearnerStats) ToJSON() string {
	data, _ := json.MarshalIndent(s, "", "  ")
	return string(data)
}

// Reset clears all learned data.
func (l *ContinuousLearner) Reset() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.routingAdjustments = make(map[string]float64)
	l.strategyPreferences = make(map[string]float64)
	l.observationCounts = make(map[string]int)
	l.lastObservation = make(map[string]time.Time)

	if l.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := l.db.ExecContext(ctx, "DELETE FROM routing_adjustments"); err != nil {
			return fmt.Errorf("clear routing adjustments: %w", err)
		}
		if _, err := l.db.ExecContext(ctx, "DELETE FROM strategy_preferences"); err != nil {
			return fmt.Errorf("clear strategy preferences: %w", err)
		}
		if _, err := l.db.ExecContext(ctx, "DELETE FROM outcomes"); err != nil {
			return fmt.Errorf("clear outcomes: %w", err)
		}
	}

	l.logger.Info("reset all learning data")
	return nil
}

// Close closes the learner and persists final state.
func (l *ContinuousLearner) Close() error {
	var err error
	l.closeOnce.Do(func() {
		close(l.shutdownCh)

		if l.db != nil {
			err = l.db.Close()
		}
	})
	return err
}

// clamp constrains a value between min and max.
func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
