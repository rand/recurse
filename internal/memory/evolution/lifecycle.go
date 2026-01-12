package evolution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// LifecycleConfig configures the lifecycle manager.
type LifecycleConfig struct {
	// Consolidation settings
	Consolidation ConsolidationConfig

	// Promotion settings
	Promotion PromotionConfig

	// Decay settings
	Decay DecayConfig

	// Audit settings
	Audit AuditConfig

	// IdleInterval is how often to run idle maintenance (0 disables).
	IdleInterval time.Duration

	// RunDecayOnSessionEnd enables decay processing at session end.
	RunDecayOnSessionEnd bool

	// RunArchiveOnIdle enables archiving during idle maintenance.
	RunArchiveOnIdle bool

	// RunPruneOnIdle enables pruning during idle maintenance.
	RunPruneOnIdle bool
}

// DefaultLifecycleConfig returns sensible defaults.
func DefaultLifecycleConfig() LifecycleConfig {
	return LifecycleConfig{
		Consolidation:        DefaultConsolidationConfig(),
		Promotion:            DefaultPromotionConfig(),
		Decay:                DefaultDecayConfig(),
		Audit:                DefaultAuditConfig(),
		IdleInterval:         time.Minute * 30,
		RunDecayOnSessionEnd: true,
		RunArchiveOnIdle:     true,
		RunPruneOnIdle:       true,
	}
}

// LifecycleResult contains the outcome of a lifecycle operation.
type LifecycleResult struct {
	// Operation name (task_complete, session_end, idle)
	Operation string

	// Consolidation result if consolidation ran
	Consolidation *ConsolidationResult

	// Promotion result if promotion ran
	Promotion *PromotionResult

	// Decay result if decay ran
	Decay *DecayResult

	// Duration of the entire operation
	Duration time.Duration

	// Errors encountered (non-fatal)
	Errors []error
}

// LifecycleManager orchestrates memory evolution at task/session boundaries.
type LifecycleManager struct {
	mu           sync.Mutex
	store        *hypergraph.Store
	config       LifecycleConfig
	consolidator *Consolidator
	promoter     *Promoter
	decayer      *Decayer
	audit        *AuditLogger

	// Meta-evolution manager (optional)
	metaEvolution *MetaEvolutionManager

	// Callbacks for lifecycle events
	onTaskComplete  []func(*LifecycleResult)
	onSessionEnd    []func(*LifecycleResult)
	onIdleMaintenance []func(*LifecycleResult)

	// Idle maintenance
	stopIdle chan struct{}
	idleWg   sync.WaitGroup
}

// NewLifecycleManager creates a new lifecycle manager.
func NewLifecycleManager(store *hypergraph.Store, config LifecycleConfig) (*LifecycleManager, error) {
	audit, err := NewAuditLogger(config.Audit)
	if err != nil {
		return nil, fmt.Errorf("create audit logger: %w", err)
	}

	return &LifecycleManager{
		store:        store,
		config:       config,
		consolidator: NewConsolidator(store, config.Consolidation),
		promoter:     NewPromoter(store, config.Promotion),
		decayer:      NewDecayer(store, config.Decay),
		audit:        audit,
		stopIdle:     make(chan struct{}),
	}, nil
}

// OnTaskComplete registers a callback for task completion events.
func (m *LifecycleManager) OnTaskComplete(callback func(*LifecycleResult)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onTaskComplete = append(m.onTaskComplete, callback)
}

// OnSessionEnd registers a callback for session end events.
func (m *LifecycleManager) OnSessionEnd(callback func(*LifecycleResult)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onSessionEnd = append(m.onSessionEnd, callback)
}

// OnIdleMaintenance registers a callback for idle maintenance events.
func (m *LifecycleManager) OnIdleMaintenance(callback func(*LifecycleResult)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onIdleMaintenance = append(m.onIdleMaintenance, callback)
}

// TaskComplete handles task completion lifecycle.
// This consolidates the task tier and promotes high-value nodes to session.
func (m *LifecycleManager) TaskComplete(ctx context.Context) (*LifecycleResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	start := time.Now()
	result := &LifecycleResult{
		Operation: "task_complete",
	}

	// Step 1: Consolidate task tier
	consResult, err := m.consolidator.Consolidate(ctx, hypergraph.TierTask, hypergraph.TierTask)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("consolidate task: %w", err))
		m.audit.LogConsolidation(hypergraph.TierTask, hypergraph.TierTask, nil, err)
	} else {
		result.Consolidation = consResult
		m.audit.LogConsolidation(hypergraph.TierTask, hypergraph.TierTask, consResult, nil)
	}

	// Step 2: Promote task → session
	promResult, err := m.promoter.PromoteTaskToSession(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("promote task to session: %w", err))
		m.audit.LogPromotion(nil, err)
	} else {
		result.Promotion = &PromotionResult{
			TaskToSession: promResult.TaskToSession,
			Duration:      promResult.Duration,
		}
		m.audit.LogPromotion(promResult, nil)
	}

	result.Duration = time.Since(start)

	// Invoke callbacks
	for _, cb := range m.onTaskComplete {
		cb(result)
	}

	if len(result.Errors) > 0 {
		return result, result.Errors[0]
	}
	return result, nil
}

// SessionEnd handles session end lifecycle.
// This consolidates session tier, promotes to long-term, and optionally runs decay.
func (m *LifecycleManager) SessionEnd(ctx context.Context) (*LifecycleResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	start := time.Now()
	result := &LifecycleResult{
		Operation: "session_end",
	}

	// Step 1: Consolidate session tier
	consResult, err := m.consolidator.Consolidate(ctx, hypergraph.TierSession, hypergraph.TierSession)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("consolidate session: %w", err))
		m.audit.LogConsolidation(hypergraph.TierSession, hypergraph.TierSession, nil, err)
	} else {
		result.Consolidation = consResult
		m.audit.LogConsolidation(hypergraph.TierSession, hypergraph.TierSession, consResult, nil)
	}

	// Step 2: Promote session → long-term
	promResult, err := m.promoter.PromoteSessionToLongterm(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("promote session to longterm: %w", err))
		m.audit.LogPromotion(nil, err)
	} else {
		result.Promotion = &PromotionResult{
			SessionToLongterm: promResult.SessionToLongterm,
			Duration:          promResult.Duration,
		}
		m.audit.LogPromotion(promResult, nil)
	}

	// Step 3: Apply decay if configured
	if m.config.RunDecayOnSessionEnd {
		decayResult, err := m.decayer.ApplyDecay(ctx)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("apply decay: %w", err))
			m.audit.LogDecay(nil, err)
		} else {
			result.Decay = decayResult
			m.audit.LogDecay(decayResult, nil)
		}
	}

	result.Duration = time.Since(start)

	// Invoke callbacks
	for _, cb := range m.onSessionEnd {
		cb(result)
	}

	if len(result.Errors) > 0 {
		return result, result.Errors[0]
	}
	return result, nil
}

// IdleMaintenance runs background maintenance tasks.
// This applies decay, archives low-confidence nodes, and prunes old archives.
func (m *LifecycleManager) IdleMaintenance(ctx context.Context) (*LifecycleResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	start := time.Now()
	result := &LifecycleResult{
		Operation: "idle",
		Decay:     &DecayResult{},
	}

	// Step 1: Apply decay
	decayResult, err := m.decayer.ApplyDecay(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("apply decay: %w", err))
		m.audit.LogDecay(nil, err)
	} else {
		result.Decay.NodesProcessed += decayResult.NodesProcessed
		result.Decay.NodesDecayed += decayResult.NodesDecayed
		m.audit.LogDecay(decayResult, nil)
	}

	// Step 2: Archive low-confidence nodes
	if m.config.RunArchiveOnIdle {
		archiveResult, err := m.decayer.ArchiveLowConfidence(ctx)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("archive: %w", err))
			m.audit.LogArchive(nil, err)
		} else {
			result.Decay.NodesArchived += archiveResult.NodesArchived
			m.audit.LogArchive(archiveResult, nil)
		}
	}

	// Step 3: Prune old archives
	if m.config.RunPruneOnIdle {
		pruneResult, err := m.decayer.PruneArchived(ctx)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("prune: %w", err))
			m.audit.LogPrune(nil, err)
		} else {
			result.Decay.NodesPruned += pruneResult.NodesPruned
			m.audit.LogPrune(pruneResult, nil)
		}
	}

	// Step 4: Run meta-evolution analysis (if enabled)
	if m.metaEvolution != nil {
		if _, err := m.metaEvolution.RunAnalysis(ctx); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("meta-evolution: %w", err))
		}
	}

	result.Duration = time.Since(start)
	result.Decay.Duration = result.Duration

	// Invoke callbacks
	for _, cb := range m.onIdleMaintenance {
		cb(result)
	}

	if len(result.Errors) > 0 {
		return result, result.Errors[0]
	}
	return result, nil
}

// StartIdleLoop starts the background idle maintenance loop.
func (m *LifecycleManager) StartIdleLoop(ctx context.Context) {
	if m.config.IdleInterval <= 0 {
		return
	}

	m.idleWg.Add(1)
	go func() {
		defer m.idleWg.Done()
		ticker := time.NewTicker(m.config.IdleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.stopIdle:
				return
			case <-ticker.C:
				// Run idle maintenance, ignore errors (they're logged)
				m.IdleMaintenance(ctx)
			}
		}
	}()
}

// StopIdleLoop stops the background idle maintenance loop.
func (m *LifecycleManager) StopIdleLoop() {
	close(m.stopIdle)
	m.idleWg.Wait()
	// Reset for potential restart
	m.stopIdle = make(chan struct{})
}

// AuditLogger returns the audit logger for external use.
func (m *LifecycleManager) AuditLogger() *AuditLogger {
	return m.audit
}

// Stats returns current evolution statistics.
func (m *LifecycleManager) Stats() LifecycleStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	auditStats := m.audit.GetStats()
	return LifecycleStats{
		TotalOperations:    auditStats.TotalEntries,
		SuccessCount:       auditStats.SuccessCount,
		ErrorCount:         auditStats.ErrorCount,
		TotalNodesAffected: auditStats.TotalNodesAffected,
		AverageDuration:    auditStats.AverageDuration,
	}
}

// LifecycleStats contains lifecycle operation statistics.
type LifecycleStats struct {
	TotalOperations    int
	SuccessCount       int
	ErrorCount         int
	TotalNodesAffected int
	AverageDuration    time.Duration
}

// Close closes the lifecycle manager and its resources.
func (m *LifecycleManager) Close() error {
	m.StopIdleLoop()
	return m.audit.Close()
}

// ForceConsolidate runs consolidation on a specific tier pair.
func (m *LifecycleManager) ForceConsolidate(ctx context.Context, sourceTier, targetTier hypergraph.Tier) (*ConsolidationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.consolidator.Consolidate(ctx, sourceTier, targetTier)
	m.audit.LogConsolidation(sourceTier, targetTier, result, err)
	return result, err
}

// ForcePromote runs promotion on a specific tier transition.
func (m *LifecycleManager) ForcePromote(ctx context.Context, fromTask bool) (*PromotionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result *PromotionResult
	var err error

	if fromTask {
		result, err = m.promoter.PromoteTaskToSession(ctx)
	} else {
		result, err = m.promoter.PromoteSessionToLongterm(ctx)
	}

	m.audit.LogPromotion(result, err)
	return result, err
}

// ForceDecay runs decay immediately.
func (m *LifecycleManager) ForceDecay(ctx context.Context) (*DecayResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result, err := m.decayer.ApplyDecay(ctx)
	m.audit.LogDecay(result, err)
	return result, err
}

// SetMetaEvolution attaches a meta-evolution manager to run during idle maintenance.
func (m *LifecycleManager) SetMetaEvolution(meta *MetaEvolutionManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metaEvolution = meta
}

// MetaEvolution returns the attached meta-evolution manager.
func (m *LifecycleManager) MetaEvolution() *MetaEvolutionManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.metaEvolution
}
