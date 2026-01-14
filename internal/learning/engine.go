// Package learning implements continuous learning for the RLM system.
// It captures learning signals from interactions, extracts patterns,
// and applies learned knowledge to improve future executions.
package learning

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// Engine orchestrates the continuous learning system.
// It provides a unified interface for the RLM to learn from and apply knowledge.
type Engine struct {
	store        *Store
	extractor    *Extractor
	consolidator *Consolidator
	applier      *Applier
	logger       *slog.Logger

	mu      sync.RWMutex
	started bool
}

// EngineConfig configures the learning engine.
type EngineConfig struct {
	// Extractor configuration
	Extractor ExtractorConfig

	// Consolidator configuration
	Consolidator ConsolidatorConfig

	// Applier configuration
	Applier ApplierConfig

	// Logger for engine operations
	Logger *slog.Logger
}

// NewEngine creates a new learning engine backed by the given hypergraph store.
func NewEngine(graph *hypergraph.Store, cfg EngineConfig) *Engine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	store := NewStore(graph)

	return &Engine{
		store:        store,
		extractor:    NewExtractor(store, cfg.Extractor),
		consolidator: NewConsolidator(store, cfg.Consolidator),
		applier:      NewApplier(store, cfg.Applier),
		logger:       cfg.Logger,
	}
}

// Start begins background processing (consolidation).
func (e *Engine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return
	}

	e.consolidator.Start()
	e.started = true
	e.logger.Info("learning engine started")
}

// Stop halts background processing.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.started {
		return
	}

	e.consolidator.Stop()
	e.started = false
	e.logger.Info("learning engine stopped")
}

// Learn processes a learning signal and extracts knowledge.
func (e *Engine) Learn(ctx context.Context, signal *LearningSignal) error {
	return e.extractor.ProcessSignal(ctx, signal)
}

// LearnSuccess records a successful task completion.
func (e *Engine) LearnSuccess(ctx context.Context, sessionID, taskID, query, output, model, strategy, domain string, confidence float64) error {
	signal := NewSuccessSignal(SignalContext{
		SessionID: sessionID,
		TaskID:    taskID,
		Query:     query,
		Output:    output,
		Model:     model,
		Strategy:  strategy,
	}, confidence)
	signal.Domain = domain

	return e.Learn(ctx, signal)
}

// LearnCorrection records a user correction.
func (e *Engine) LearnCorrection(ctx context.Context, sessionID, taskID, query, original, corrected, correctionType, explanation, domain string, severity float64) error {
	signal := NewCorrectionSignal(SignalContext{
		SessionID: sessionID,
		TaskID:    taskID,
		Query:     query,
		Output:    original,
	}, CorrectionDetails{
		OriginalOutput:  original,
		CorrectedOutput: corrected,
		CorrectionType:  correctionType,
		Severity:        severity,
		Explanation:     explanation,
	})
	signal.Domain = domain

	return e.Learn(ctx, signal)
}

// LearnPreference records a user preference.
func (e *Engine) LearnPreference(ctx context.Context, sessionID, key string, value interface{}, scope PreferenceScope, scopeValue string, explicit bool) error {
	signal := NewPreferenceSignal(SignalContext{
		SessionID: sessionID,
	}, PreferenceDetails{
		Key:        key,
		Value:      value,
		Scope:      scope,
		ScopeValue: scopeValue,
		Explicit:   explicit,
	})

	return e.Learn(ctx, signal)
}

// LearnError records an error for future avoidance.
func (e *Engine) LearnError(ctx context.Context, sessionID, taskID, query, domain string, err error) error {
	signal := NewErrorSignal(SignalContext{
		SessionID: sessionID,
		TaskID:    taskID,
		Query:     query,
	}, err)
	signal.Domain = domain

	return e.Learn(ctx, signal)
}

// Apply retrieves relevant knowledge for a query.
func (e *Engine) Apply(ctx context.Context, query, domain, projectPath string) (*ApplyResult, error) {
	return e.applier.Apply(ctx, query, domain, projectPath)
}

// GetContextEnhancements returns formatted knowledge to add to LLM context.
// This is the primary integration point for RLM.
func (e *Engine) GetContextEnhancements(ctx context.Context, query, domain, projectPath string) ([]string, error) {
	result, err := e.Apply(ctx, query, domain, projectPath)
	if err != nil {
		return nil, err
	}
	return result.ContextAdditions, nil
}

// EnhancePrompt adds learned knowledge context to a prompt.
func (e *Engine) EnhancePrompt(ctx context.Context, prompt, domain, projectPath string) (string, error) {
	enhancements, err := e.GetContextEnhancements(ctx, prompt, domain, projectPath)
	if err != nil {
		return prompt, err
	}

	if len(enhancements) == 0 {
		return prompt, nil
	}

	// Prepend learned context
	var sb strings.Builder
	sb.WriteString("## Learned Context\n\n")
	for _, e := range enhancements {
		sb.WriteString(e)
		sb.WriteString("\n")
	}
	sb.WriteString("\n## Task\n\n")
	sb.WriteString(prompt)

	return sb.String(), nil
}

// Stats returns learning statistics.
func (e *Engine) Stats(ctx context.Context) (*EngineStats, error) {
	storeStats, err := e.store.Stats(ctx)
	if err != nil {
		return nil, fmt.Errorf("store stats: %w", err)
	}

	consolidatorStats, err := e.consolidator.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("consolidator stats: %w", err)
	}

	return &EngineStats{
		TotalKnowledge:   storeStats.TotalNodes,
		Facts:            storeStats.FactCount,
		Patterns:         storeStats.PatternCount,
		Preferences:      storeStats.PreferenceCount,
		Constraints:      storeStats.ConstraintCount,
		Signals:          storeStats.SignalCount,
		ConsolidatorInfo: consolidatorStats,
	}, nil
}

// EngineStats contains learning engine statistics.
type EngineStats struct {
	TotalKnowledge   int                  `json:"total_knowledge"`
	Facts            int                  `json:"facts"`
	Patterns         int                  `json:"patterns"`
	Preferences      int                  `json:"preferences"`
	Constraints      int                  `json:"constraints"`
	Signals          int                  `json:"signals"`
	ConsolidatorInfo *ConsolidationStats  `json:"consolidator"`
}

// Consolidate triggers a manual consolidation pass.
func (e *Engine) Consolidate(ctx context.Context) error {
	return e.consolidator.ConsolidateAll(ctx)
}

// MergeSimilar merges similar knowledge items.
func (e *Engine) MergeSimilar(ctx context.Context, domain string) (int, error) {
	return e.consolidator.MergeSimilarFacts(ctx, domain)
}

// Store returns the underlying store for advanced operations.
func (e *Engine) Store() *Store {
	return e.store
}
