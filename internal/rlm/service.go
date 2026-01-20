package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rand/recurse/internal/budget"
	"github.com/rand/recurse/internal/learning"
	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/checkpoint"
	"github.com/rand/recurse/internal/rlm/compress"
	"github.com/rand/recurse/internal/rlm/hallucination"
	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/orchestrator"
	"github.com/rand/recurse/internal/rlm/repl"
	"github.com/rand/recurse/internal/tui/components/dialogs/rlmtrace"
)

// ServiceConfig configures the unified RLM service.
type ServiceConfig struct {
	// Controller configuration
	Controller ControllerConfig

	// Memory evolution lifecycle configuration
	Lifecycle evolution.LifecycleConfig

	// Meta-controller configuration
	Meta meta.Config

	// MaxTraceEvents is the maximum trace events to retain (in-memory provider only).
	MaxTraceEvents int

	// StorePath is the path for persistent hypergraph storage (empty for in-memory).
	StorePath string

	// TracePath is the path for persistent trace storage (empty for in-memory).
	// When set, trace events persist across sessions.
	TracePath string

	// OrchestratorEnabled enables RLM orchestration for prompt pre-processing.
	// When enabled, every prompt is analyzed by RLM before being sent to the main agent.
	OrchestratorEnabled bool

	// Checkpoint configures session state persistence for crash recovery.
	Checkpoint checkpoint.Config

	// Learning configures the continuous learning engine.
	Learning learning.EngineConfig

	// LearningEnabled enables continuous learning from interactions.
	LearningEnabled bool

	// Budget configures the budget manager.
	Budget budget.ManagerConfig

	// BudgetEnabled enables budget tracking and enforcement.
	BudgetEnabled bool

	// Compression configures the context compression manager.
	Compression compress.ManagerConfig

	// CompressionEnabled enables context compression for large contexts.
	CompressionEnabled bool

	// CompressionThreshold is the token count above which compression is applied.
	// Default: 8000 tokens.
	CompressionThreshold int

	// Hallucination configures hallucination detection.
	// [SPEC-08.19-22] Output verification integration.
	Hallucination HallucinationConfig
}

// HallucinationConfig configures hallucination detection for the RLM service.
type HallucinationConfig struct {
	// OutputVerificationEnabled enables verification of agent responses.
	// [SPEC-08.19] Agent responses SHALL be verified before returning to the user.
	OutputVerificationEnabled bool

	// TraceAuditEnabled enables auditing of reasoning traces.
	// [SPEC-08.23] RLM execution traces SHALL be audited for procedural hallucinations.
	TraceAuditEnabled bool

	// OutputVerifierConfig configures the output verifier behavior.
	OutputVerifierConfig hallucination.OutputVerifierConfig

	// TraceAuditorConfig configures the trace auditor behavior.
	TraceAuditorConfig hallucination.TraceAuditorConfig

	// DetectorConfig configures the underlying detector.
	DetectorConfig hallucination.DetectorConfig
}

// DefaultServiceConfig returns sensible defaults for the RLM service.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		Controller:          DefaultControllerConfig(),
		Lifecycle:           evolution.DefaultLifecycleConfig(),
		Meta:                meta.DefaultConfig(),
		MaxTraceEvents:      1000,
		StorePath:           "",
		OrchestratorEnabled: true,  // Enable orchestration by default
		Checkpoint:          checkpoint.DefaultConfig(),
		LearningEnabled:     true,  // Enable learning by default
		Learning:            learning.EngineConfig{},
		BudgetEnabled:       true,  // Enable budget tracking by default
		Budget: budget.ManagerConfig{
			Limits:      budget.DefaultLimits(),
			Enforcement: budget.DefaultEnforcementConfig(),
		},
		CompressionEnabled:   true, // Enable compression by default
		CompressionThreshold: 8000, // Compress when context exceeds 8K tokens
		Compression:          compress.DefaultManagerConfig(),
		Hallucination: HallucinationConfig{
			OutputVerificationEnabled: false, // Disabled by default for performance
			TraceAuditEnabled:         false, // Disabled by default for performance
			OutputVerifierConfig:      hallucination.DefaultOutputVerifierConfig(),
			TraceAuditorConfig:        hallucination.DefaultTraceAuditorConfig(),
			DetectorConfig:            hallucination.DefaultDetectorConfig(),
		},
	}
}

// traceRecorder is the interface for recording trace events.
type traceRecorder interface {
	rlmtrace.TraceProvider
	RecordEvent(event TraceEvent) error
}

// Service is the unified RLM service that orchestrates all subsystems.
type Service struct {
	mu sync.RWMutex

	// Core components
	store           *hypergraph.Store
	memoryBridge    *RLMCoreMemoryBridge             // optional rlm-core memory integration
	controller      *Controller
	lifecycle       *evolution.LifecycleManager
	metaEvolution   *evolution.MetaEvolutionManager // meta-evolution for schema adaptation
	tracer          traceRecorder
	persistentTrace *PersistentTraceProvider // non-nil if using persistent storage
	orchestrator    *Orchestrator            // prompt pre-processing
	subCallRouter   *SubCallRouter           // routes REPL llm_call() to models
	wrapper         *Wrapper                 // RLM wrapper for context externalization
	checkpoint      *checkpoint.Manager      // session state persistence
	learner         *learning.Engine         // continuous learning engine
	budgetMgr       *budget.Manager          // budget tracking and enforcement

	// Hallucination detection [SPEC-08.19-26]
	detector       *hallucination.Detector       // main detector orchestrator
	outputVerifier *hallucination.OutputVerifier // verifies agent responses
	traceAuditor   *hallucination.TraceAuditor   // audits reasoning traces [SPEC-08.23-26]

	// Configuration
	config ServiceConfig

	// State
	running   bool
	startTime time.Time
	sessionID string // current session ID for learning

	// Statistics
	stats ServiceStats
}

// ServiceStats contains service-level statistics.
type ServiceStats struct {
	TotalExecutions int
	TotalTokens     int
	TotalDuration   time.Duration
	TasksCompleted  int
	SessionsEnded   int
	Errors          int
}

// NewService creates a new unified RLM service.
func NewService(llmClient meta.LLMClient, config ServiceConfig) (*Service, error) {
	// Create hypergraph store
	storeOpts := hypergraph.Options{}
	if config.StorePath != "" {
		storeOpts.Path = config.StorePath
		storeOpts.CreateIfNotExists = true
	}

	store, err := hypergraph.NewStore(storeOpts)
	if err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}

	// Initialize rlm-core memory bridge if available (RLM_USE_CORE=true)
	// This provides optional integration with rlm-core's MemoryStore for
	// specific operations while the primary hypergraph.Store handles the full feature set.
	memoryBridge := NewRLMCoreMemoryBridge(config.StorePath + ".rlmcore")
	if memoryBridge != nil {
		slog.Info("rlm-core memory bridge initialized")
	}

	// Create meta-controller
	metaCtrl := meta.NewController(llmClient, config.Meta)

	// Create RLM controller with the main LLM client for response generation
	controller := NewController(metaCtrl, llmClient, store, config.Controller)

	// Create trace provider (persistent or in-memory)
	var tracer traceRecorder
	var persistentTrace *PersistentTraceProvider

	if config.TracePath != "" {
		// Use persistent trace provider with file-based database
		pt, err := NewPersistentTraceProvider(PersistentTraceConfig{
			Path: config.TracePath,
		})
		if err != nil {
			store.Close()
			return nil, fmt.Errorf("create persistent trace provider: %w", err)
		}
		tracer = pt
		persistentTrace = pt
	} else {
		// Use in-memory trace provider
		tracer = NewTraceProvider(config.MaxTraceEvents)
	}
	controller.SetTracer(tracer)

	// Create lifecycle manager
	lifecycle, err := evolution.NewLifecycleManager(store, config.Lifecycle)
	if err != nil {
		if persistentTrace != nil {
			persistentTrace.Close()
		}
		store.Close()
		return nil, fmt.Errorf("create lifecycle manager: %w", err)
	}

	// Wire audit logger to hypergraph store for database persistence
	// [recurse-0uv] Fixes: evolution audit log not persisting to database
	lifecycle.AuditLogger().SetStore(store)

	// Create meta-evolution manager for schema adaptation [SPEC-06]
	outcomeStore := evolution.NewSQLiteOutcomeStore(store)
	proposalStore := evolution.NewSQLiteProposalStore(store)
	metaEvolution := evolution.NewMetaEvolutionManager(
		store, proposalStore, outcomeStore,
		lifecycle.AuditLogger(),
		evolution.DefaultMetaEvolutionConfig(),
	)
	lifecycle.SetMetaEvolution(metaEvolution)

	// Create session synthesizer for session summaries [SPEC-09.01]
	synthesizer := NewLLMSynthesizer(llmClient)
	lifecycle.SetSessionSynthesizer(synthesizer)

	// Wire outcome recorder to hybrid search for meta-evolution tracking
	store.SetOutcomeRecorder(outcomeStore)

	// Create orchestrator for prompt pre-processing
	orchestrator := NewOrchestrator(metaCtrl, OrchestratorConfig{
		Enabled:        config.OrchestratorEnabled,
		Models:         meta.DefaultModels(),
		ContextEnabled: true, // Enable context externalization by default
	})

	// Create sub-call router for REPL llm_call() support
	subCallRouter := NewSubCallRouter(SubCallConfig{
		Client:      llmClient,
		Models:      meta.DefaultModels(),
		MaxDepth:    config.Controller.MaxRecursionDepth,
		BudgetLimit: config.Controller.MaxTokenBudget,
	})

	// Create checkpoint manager for session state persistence
	checkpointMgr := checkpoint.NewManager(config.Checkpoint)

	// Create learning engine if enabled
	var learner *learning.Engine
	if config.LearningEnabled {
		learner = learning.NewEngine(store, config.Learning)
	}

	// Create budget manager if enabled
	var budgetMgr *budget.Manager
	if config.BudgetEnabled {
		budgetStore := budget.NewStore(store)
		budgetMgr = budget.NewManager(budgetStore, config.Budget)
	}

	// Create hallucination detection components [SPEC-08.19-22]
	// Create hallucination detection components [SPEC-08.19-26]
	var detector *hallucination.Detector
	var outputVerifier *hallucination.OutputVerifier
	var traceAuditor *hallucination.TraceAuditor

	// Create detector if any hallucination detection is enabled
	if config.Hallucination.OutputVerificationEnabled || config.Hallucination.TraceAuditEnabled {
		// Create backend using the LLM client for probability estimation
		// [SPEC-08.27] Uses self-verification backend by default
		backendCfg := hallucination.DefaultBackendConfig()
		backend, err := hallucination.NewBackend(backendCfg, llmClient)
		if err != nil {
			slog.Warn("failed to create hallucination backend, disabling detection", "error", err)
		} else {
			// Create detector with configured settings
			detector = hallucination.NewDetector(backend, config.Hallucination.DetectorConfig)

			// Create output verifier if enabled [SPEC-08.19-22]
			if config.Hallucination.OutputVerificationEnabled {
				verifierConfig := config.Hallucination.OutputVerifierConfig
				verifierConfig.Enabled = true
				outputVerifier = hallucination.NewOutputVerifier(detector, verifierConfig)
			}

			// Create trace auditor if enabled [SPEC-08.23-26]
			if config.Hallucination.TraceAuditEnabled {
				auditorConfig := config.Hallucination.TraceAuditorConfig
				auditorConfig.Enabled = true
				traceAuditor = hallucination.NewTraceAuditor(detector, auditorConfig)
			}
		}
	}

	svc := &Service{
		store:           store,
		memoryBridge:    memoryBridge,
		controller:      controller,
		lifecycle:       lifecycle,
		metaEvolution:   metaEvolution,
		tracer:          tracer,
		persistentTrace: persistentTrace,
		orchestrator:    orchestrator,
		subCallRouter:   subCallRouter,
		checkpoint:      checkpointMgr,
		learner:         learner,
		budgetMgr:       budgetMgr,
		detector:        detector,
		outputVerifier:  outputVerifier,
		traceAuditor:    traceAuditor,
		config:          config,
	}

	// Create RLM wrapper for context externalization with compression
	wrapperConfig := DefaultWrapperConfig()
	wrapperConfig.CompressionEnabled = config.CompressionEnabled
	wrapperConfig.CompressionThreshold = config.CompressionThreshold
	if config.CompressionEnabled {
		wrapperConfig.CompressionConfig = &config.Compression
	}
	svc.wrapper = NewWrapper(svc, wrapperConfig)

	// Wire ContextPreparer to orchestrator.Core for context externalization [SPEC-09.06]
	// This enables the orchestrator to use the wrapper for context preparation.
	controller.Core().SetContextPreparer(&wrapperContextPreparer{wrapper: svc.wrapper})

	// Wire TraceAuditor to orchestrator.Core for hallucination detection [SPEC-08.23-26]
	if traceAuditor != nil {
		controller.Core().SetTraceAuditor(traceAuditor)
	}

	// Wire up lifecycle callbacks for statistics
	lifecycle.OnTaskComplete(func(result *evolution.LifecycleResult) {
		svc.mu.Lock()
		svc.stats.TasksCompleted++
		svc.mu.Unlock()

		// Learn from task completion
		if svc.learner != nil && result != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			svc.learner.LearnSuccess(ctx,
				svc.sessionID,
				"", // taskID
				"task completed", // query
				"", // output
				"", // model
				"lifecycle", // strategy
				"", // domain
				0.8, // confidence
			)
		}
	})

	lifecycle.OnSessionEnd(func(result *evolution.LifecycleResult) {
		svc.mu.Lock()
		svc.stats.SessionsEnded++
		svc.mu.Unlock()
	})

	return svc, nil
}

// Start starts the RLM service, including background tasks.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("service already running")
	}

	s.running = true
	s.startTime = time.Now()

	// Restore stats from checkpoint if available
	if s.checkpoint != nil {
		if cp, err := s.checkpoint.Load(); err == nil && cp != nil && cp.ServiceStats != nil {
			s.stats = ServiceStats{
				TotalExecutions: cp.ServiceStats.TotalExecutions,
				TotalTokens:     cp.ServiceStats.TotalTokens,
				TotalDuration:   cp.ServiceStats.TotalDuration,
				TasksCompleted:  cp.ServiceStats.TasksCompleted,
				SessionsEnded:   cp.ServiceStats.SessionsEnded,
				Errors:          cp.ServiceStats.Errors,
			}
			slog.Info("Restored RLM stats from checkpoint",
				"executions", s.stats.TotalExecutions,
				"tokens", s.stats.TotalTokens)
		}
	}

	// Start idle maintenance loop
	s.lifecycle.StartIdleLoop(ctx)

	// Start checkpoint manager for periodic saves
	if s.checkpoint != nil {
		s.checkpoint.Start(ctx)
	}

	// Start learning engine background consolidation
	if s.learner != nil {
		s.learner.Start()
	}

	// Start budget session
	if s.budgetMgr != nil {
		if err := s.budgetMgr.StartSession(ctx); err != nil {
			// Log but don't fail - budget tracking is non-critical
			// Budget will still track in-memory
		}
	}

	return nil
}

// Stop stops the RLM service and releases resources.
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	// End budget session first (needs store to persist)
	if s.budgetMgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.budgetMgr.EndSession(ctx); err != nil {
			// Log but continue shutdown
		}
		cancel()
	}

	// Stop learning engine first (before closing store)
	if s.learner != nil {
		s.learner.Stop()
	}

	// Stop checkpoint manager and persist stats for next session
	if s.checkpoint != nil {
		// Save final stats before stopping (clear task/RLM state since those are session-specific)
		s.checkpoint.UpdateServiceStats(&checkpoint.ServiceStats{
			TotalExecutions: s.stats.TotalExecutions,
			TotalTokens:     s.stats.TotalTokens,
			TotalDuration:   s.stats.TotalDuration,
			TasksCompleted:  s.stats.TasksCompleted,
			SessionsEnded:   s.stats.SessionsEnded,
			Errors:          s.stats.Errors,
		})
		// Clear session-specific state but keep stats
		s.checkpoint.UpdateTaskState(nil)
		s.checkpoint.UpdateRLMState(nil)
		// Save to disk before stopping
		if err := s.checkpoint.Save(); err != nil {
			slog.Warn("Failed to save final checkpoint", "error", err)
		}
		s.checkpoint.Stop()
		// Don't clear checkpoint - stats should persist across sessions
	}

	// Stop lifecycle manager (stops idle loop)
	if err := s.lifecycle.Close(); err != nil {
		return fmt.Errorf("close lifecycle: %w", err)
	}

	// Close persistent trace provider if present
	if s.persistentTrace != nil {
		if err := s.persistentTrace.Close(); err != nil {
			return fmt.Errorf("close trace provider: %w", err)
		}
	}

	// Close rlm-core memory bridge if enabled
	if s.memoryBridge != nil {
		s.memoryBridge.Free()
	}

	// Close store
	if err := s.store.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}

	return nil
}

// Execute runs an RLM task and returns the result.
func (s *Service) Execute(ctx context.Context, task string) (*ExecutionResult, error) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil, fmt.Errorf("service not running")
	}
	execNum := s.stats.TotalExecutions + 1
	s.mu.Unlock()

	// Update checkpoint before execution
	if s.checkpoint != nil {
		replActive := s.orchestrator != nil && s.orchestrator.HasREPL()
		s.checkpoint.UpdateRLMState(&checkpoint.RLMState{
			CurrentIteration: execNum,
			MaxIterations:    s.config.Controller.MaxRecursionDepth,
			LastTask:         task,
			REPLActive:       replActive,
			Mode:             "rlm",
		})
	}

	result, err := s.controller.Execute(ctx, task)

	s.mu.Lock()
	s.stats.TotalExecutions++
	if result != nil {
		s.stats.TotalTokens += result.TotalTokens
		s.stats.TotalDuration += result.Duration
	}
	if err != nil {
		s.stats.Errors++
	}
	stats := s.stats
	s.mu.Unlock()

	// Track tokens in budget manager
	if s.budgetMgr != nil && result != nil {
		// Estimate input/output split (controller doesn't provide this breakdown)
		// Use a rough 2:1 input:output ratio as approximation
		inputTokens := int64(result.TotalTokens * 2 / 3)
		outputTokens := int64(result.TotalTokens / 3)
		// TODO: Get actual costs from model pricing
		inputCost := float64(inputTokens) * 0.000003  // $3/M tokens estimate
		outputCost := float64(outputTokens) * 0.000015 // $15/M tokens estimate
		s.budgetMgr.AddTokens(inputTokens, outputTokens, 0, inputCost, outputCost)
	}

	// Update checkpoint after execution with current stats
	if s.checkpoint != nil {
		s.checkpoint.UpdateServiceStats(&checkpoint.ServiceStats{
			TotalExecutions: stats.TotalExecutions,
			TotalTokens:     stats.TotalTokens,
			TotalDuration:   stats.TotalDuration,
			TasksCompleted:  stats.TasksCompleted,
			SessionsEnded:   stats.SessionsEnded,
			Errors:          stats.Errors,
		})
	}

	return result, err
}

// TaskComplete signals task completion to the lifecycle manager.
func (s *Service) TaskComplete(ctx context.Context) (*evolution.LifecycleResult, error) {
	return s.lifecycle.TaskComplete(ctx)
}

// SessionEnd signals session end to the lifecycle manager.
func (s *Service) SessionEnd(ctx context.Context) (*evolution.LifecycleResult, error) {
	return s.lifecycle.SessionEnd(ctx)
}

// TraceProvider returns the trace provider for UI integration.
func (s *Service) TraceProvider() rlmtrace.TraceProvider {
	return s.tracer
}

// Store returns the hypergraph store for direct access.
func (s *Service) Store() *hypergraph.Store {
	return s.store
}

// MemoryBridge returns the rlm-core memory bridge, or nil if not enabled.
// The bridge is enabled when RLM_USE_CORE=true and rlm-core is available.
func (s *Service) MemoryBridge() *RLMCoreMemoryBridge {
	return s.memoryBridge
}

// LifecycleManager returns the lifecycle manager for direct access.
func (s *Service) LifecycleManager() *evolution.LifecycleManager {
	return s.lifecycle
}

// MetaEvolutionManager returns the meta-evolution manager for direct access.
// This enables the TUI to access schema evolution proposals.
func (s *Service) MetaEvolutionManager() *evolution.MetaEvolutionManager {
	return s.metaEvolution
}

// Controller returns the RLM controller for direct access.
func (s *Service) Controller() *Controller {
	return s.controller
}

// Stats returns service statistics.
func (s *Service) Stats() ServiceStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// IsRunning returns whether the service is running.
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Uptime returns how long the service has been running.
func (s *Service) Uptime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.running {
		return 0
	}
	return time.Since(s.startTime)
}

// HealthCheck performs a health check on all subsystems.
func (s *Service) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()

	status := &HealthStatus{
		Running: running,
		Checks:  make(map[string]bool),
	}

	// Check store
	_, err := s.store.ListNodes(ctx, hypergraph.NodeFilter{Limit: 1})
	status.Checks["store"] = err == nil

	// Check tracer
	_, err = s.tracer.GetEvents(1)
	status.Checks["tracer"] = err == nil

	// Check lifecycle
	status.Checks["lifecycle"] = s.lifecycle.AuditLogger() != nil

	// Overall healthy if all checks pass
	status.Healthy = running
	for _, ok := range status.Checks {
		if !ok {
			status.Healthy = false
			break
		}
	}

	return status, nil
}

// HealthStatus contains health check results.
type HealthStatus struct {
	Running bool
	Healthy bool
	Checks  map[string]bool
}

// OnTaskComplete registers a callback for task completion events.
func (s *Service) OnTaskComplete(callback func(*evolution.LifecycleResult)) {
	s.lifecycle.OnTaskComplete(callback)
}

// OnSessionEnd registers a callback for session end events.
func (s *Service) OnSessionEnd(callback func(*evolution.LifecycleResult)) {
	s.lifecycle.OnSessionEnd(callback)
}

// OnIdleMaintenance registers a callback for idle maintenance events.
func (s *Service) OnIdleMaintenance(callback func(*evolution.LifecycleResult)) {
	s.lifecycle.OnIdleMaintenance(callback)
}

// QueryMemory queries the hypergraph for relevant memories.
func (s *Service) QueryMemory(ctx context.Context, query string, limit int) ([]*hypergraph.Node, error) {
	return s.store.ListNodes(ctx, hypergraph.NodeFilter{
		Types: []hypergraph.NodeType{
			hypergraph.NodeTypeFact,
			hypergraph.NodeTypeExperience,
			hypergraph.NodeTypeDecision,
		},
		Limit: limit,
	})
}

// RecordFact records a fact in the hypergraph memory.
func (s *Service) RecordFact(ctx context.Context, content string, confidence float64) error {
	node := hypergraph.NewNode(hypergraph.NodeTypeFact, content)
	node.Confidence = confidence
	node.Tier = hypergraph.TierTask
	return s.store.CreateNode(ctx, node)
}

// RecordExperience records an experience in the hypergraph memory.
func (s *Service) RecordExperience(ctx context.Context, content string) error {
	node := hypergraph.NewNode(hypergraph.NodeTypeExperience, content)
	node.Tier = hypergraph.TierTask
	return s.store.CreateNode(ctx, node)
}

// SessionContext contains context for resuming a session.
// [SPEC-09.08] Returned by ResumeSession for "What was I working on?" queries.
type SessionContext struct {
	// PreviousSession is the most recent session summary
	PreviousSession *evolution.SessionSummary `json:"previous_session,omitempty"`

	// UnfinishedWork describes what wasn't completed
	UnfinishedWork string `json:"unfinished_work,omitempty"`

	// RecommendedStart lists suggested starting points
	RecommendedStart []string `json:"recommended_start,omitempty"`

	// ActiveFiles lists files that were being worked on
	ActiveFiles []string `json:"active_files,omitempty"`

	// RecentSessions lists summaries of recent sessions (beyond the latest)
	RecentSessions []*evolution.SessionSummary `json:"recent_sessions,omitempty"`
}

// ResumeSession queries for session resumption context.
// [SPEC-09.08] Answers "What was I working on?" with actionable context.
func (s *Service) ResumeSession(ctx context.Context) (*SessionContext, error) {
	// Query recent session_summary nodes from longterm tier
	summaries, err := s.store.ListNodes(ctx, hypergraph.NodeFilter{
		Types:    []hypergraph.NodeType{hypergraph.NodeTypeExperience},
		Subtypes: []string{"session_summary"},
		Tiers:    []hypergraph.Tier{hypergraph.TierLongterm},
		Limit:    5,
	})
	if err != nil {
		return nil, fmt.Errorf("query session summaries: %w", err)
	}

	if len(summaries) == 0 {
		return nil, nil // No previous sessions
	}

	// Parse the most recent summary
	latest := summaries[0]
	var latestSummary evolution.SessionSummary
	if len(latest.Metadata) > 0 {
		if err := json.Unmarshal(latest.Metadata, &latestSummary); err != nil {
			// Fallback to basic info from content
			latestSummary.Summary = latest.Content
		}
	} else {
		latestSummary.Summary = latest.Content
	}

	result := &SessionContext{
		PreviousSession:  &latestSummary,
		UnfinishedWork:   latestSummary.UnfinishedWork,
		RecommendedStart: latestSummary.NextSteps,
		ActiveFiles:      latestSummary.ActiveFiles,
	}

	// Include additional recent sessions if available
	if len(summaries) > 1 {
		for _, node := range summaries[1:] {
			var summary evolution.SessionSummary
			if len(node.Metadata) > 0 {
				if err := json.Unmarshal(node.Metadata, &summary); err != nil {
					summary.Summary = node.Content
				}
			} else {
				summary.Summary = node.Content
			}
			result.RecentSessions = append(result.RecentSessions, &summary)
		}
	}

	return result, nil
}

// GetTraceEvents returns recent trace events.
func (s *Service) GetTraceEvents(limit int) ([]rlmtrace.TraceEvent, error) {
	return s.tracer.GetEvents(limit)
}

// GetTraceStats returns trace statistics.
func (s *Service) GetTraceStats() rlmtrace.TraceStats {
	return s.tracer.Stats()
}

// ClearTrace clears all trace events.
func (s *Service) ClearTrace() error {
	return s.tracer.ClearEvents()
}

// RecordTraceEvent records a trace event from external callers (e.g., rlm_execute tool).
func (s *Service) RecordTraceEvent(event rlmtrace.TraceEvent) error {
	internalEvent := TraceEvent{
		ID:        event.ID,
		Type:      string(event.Type),
		Action:    event.Action,
		Details:   event.Details,
		Tokens:    event.Tokens,
		Duration:  event.Duration,
		Timestamp: event.Timestamp,
		Depth:     event.Depth,
		ParentID:  event.ParentID,
		Status:    event.Status,
	}
	return s.tracer.RecordEvent(internalEvent)
}

// Orchestrator returns the RLM orchestrator for prompt pre-processing.
func (s *Service) Orchestrator() *Orchestrator {
	return s.orchestrator
}

// SetREPLManager sets the REPL manager for context externalization.
// This enables the true RLM paradigm where context is externalized to Python.
func (s *Service) SetREPLManager(replMgr *repl.Manager) {
	if s.orchestrator != nil {
		s.orchestrator.SetREPLManager(replMgr)
	}
	if s.wrapper != nil {
		s.wrapper.SetREPLManager(replMgr)
	}

	// Wire up the callback handler so Python's llm_call() works
	if replMgr != nil && s.subCallRouter != nil {
		handler := NewREPLCallbackHandler(s.subCallRouter)
		replMgr.SetCallbackHandler(handler)
	}

	// Wire up the memory handler so Python's memory_* functions work
	if replMgr != nil && s.store != nil {
		memHandler := NewMemoryCallbackHandler(s.store)
		replMgr.SetMemoryHandler(memHandler)
	}
}

// Wrapper returns the RLM wrapper for context externalization.
func (s *Service) Wrapper() *Wrapper {
	return s.wrapper
}

// PrepareContext prepares context for a prompt, potentially externalizing it.
func (s *Service) PrepareContext(ctx context.Context, prompt string, contexts []ContextSource) (*PreparedPrompt, error) {
	if s.wrapper == nil {
		return &PreparedPrompt{
			OriginalPrompt: prompt,
			FinalPrompt:    prompt,
			Mode:           ModeDirecte,
		}, nil
	}
	return s.wrapper.PrepareContext(ctx, prompt, contexts)
}

// SubCallRouter returns the sub-call router for REPL llm_call() support.
func (s *Service) SubCallRouter() *SubCallRouter {
	return s.subCallRouter
}

// MakeSubCall makes a sub-LLM call (used by REPL llm_call handler).
func (s *Service) MakeSubCall(ctx context.Context, req SubCallRequest) *SubCallResponse {
	if s.subCallRouter == nil {
		return &SubCallResponse{Error: "sub-call router not configured"}
	}
	return s.subCallRouter.Call(ctx, req)
}

// MakeBatchSubCall makes batch sub-LLM calls (used by REPL llm_batch handler).
func (s *Service) MakeBatchSubCall(ctx context.Context, requests []SubCallRequest) []*SubCallResponse {
	if s.subCallRouter == nil {
		responses := make([]*SubCallResponse, len(requests))
		for i := range responses {
			responses[i] = &SubCallResponse{Error: "sub-call router not configured"}
		}
		return responses
	}
	return s.subCallRouter.BatchCall(ctx, requests)
}

// SubCallStats returns statistics about sub-LLM calls.
func (s *Service) SubCallStats() SubCallStats {
	if s.subCallRouter == nil {
		return SubCallStats{}
	}
	return s.subCallRouter.Stats()
}

// AnalyzePrompt performs RLM analysis on a user prompt.
// This should be called before sending the prompt to the main agent.
// If learning is enabled, it enhances the prompt with learned knowledge.
func (s *Service) AnalyzePrompt(ctx context.Context, prompt string, contextTokens int) (*AnalysisResult, error) {
	// First enhance with learned knowledge if available
	enhancedPrompt := prompt
	if s.learner != nil {
		enhanced, err := s.learner.EnhancePrompt(ctx, prompt, "", "") // domain and project from context
		if err == nil && enhanced != prompt {
			enhancedPrompt = enhanced
		}
	}

	if s.orchestrator == nil {
		return &AnalysisResult{
			OriginalPrompt: prompt,
			EnhancedPrompt: enhancedPrompt,
		}, nil
	}

	// Run orchestrator analysis on the enhanced prompt
	result, err := s.orchestrator.Analyze(ctx, enhancedPrompt, contextTokens)
	if err != nil {
		return nil, err
	}

	// Preserve original prompt reference
	result.OriginalPrompt = prompt
	return result, nil
}

// IsOrchestrationEnabled returns whether RLM orchestration is enabled.
func (s *Service) IsOrchestrationEnabled() bool {
	return s.orchestrator != nil && s.orchestrator.IsEnabled()
}

// SetOrchestrationEnabled enables or disables RLM orchestration.
func (s *Service) SetOrchestrationEnabled(enabled bool) {
	if s.orchestrator != nil {
		s.orchestrator.SetEnabled(enabled)
	}
}

// CheckpointManager returns the checkpoint manager for recovery checks.
func (s *Service) CheckpointManager() *checkpoint.Manager {
	return s.checkpoint
}

// LoadCheckpoint loads any existing checkpoint for recovery.
func (s *Service) LoadCheckpoint() (*checkpoint.Checkpoint, error) {
	if s.checkpoint == nil {
		return nil, nil
	}
	return s.checkpoint.Load()
}

// SetSessionID sets the session ID for checkpoints and learning.
func (s *Service) SetSessionID(sessionID string) {
	s.mu.Lock()
	s.sessionID = sessionID
	s.mu.Unlock()

	if s.checkpoint != nil {
		s.checkpoint.SetSessionID(sessionID)
	}
}

// UpdateCheckpoint updates the checkpoint with current service state.
func (s *Service) UpdateCheckpoint(ctx context.Context) {
	if s.checkpoint == nil {
		return
	}

	s.mu.RLock()
	stats := s.stats
	s.mu.RUnlock()

	// Update service stats
	s.checkpoint.UpdateServiceStats(&checkpoint.ServiceStats{
		TotalExecutions: stats.TotalExecutions,
		TotalTokens:     stats.TotalTokens,
		TotalDuration:   stats.TotalDuration,
		TasksCompleted:  stats.TasksCompleted,
		SessionsEnded:   stats.SessionsEnded,
		Errors:          stats.Errors,
	})
}

// UpdateCheckpointTask updates the checkpoint with task info.
func (s *Service) UpdateCheckpointTask(taskID string, taskStarted time.Time, nodeCount, factCount, entityCount int) {
	if s.checkpoint == nil {
		return
	}

	s.checkpoint.UpdateTaskState(&checkpoint.TaskState{
		TaskID:      taskID,
		TaskStarted: taskStarted,
		NodeCount:   nodeCount,
		FactCount:   factCount,
		EntityCount: entityCount,
	})
}

// UpdateCheckpointRLM updates the checkpoint with RLM execution state.
func (s *Service) UpdateCheckpointRLM(currentIter, maxIter int, lastTask string, replActive bool, mode string) {
	if s.checkpoint == nil {
		return
	}

	s.checkpoint.UpdateRLMState(&checkpoint.RLMState{
		CurrentIteration: currentIter,
		MaxIterations:    maxIter,
		LastTask:         lastTask,
		REPLActive:       replActive,
		Mode:             mode,
	})
}

// ClearCheckpoint removes the checkpoint file.
func (s *Service) ClearCheckpoint() error {
	if s.checkpoint == nil {
		return nil
	}
	return s.checkpoint.Clear()
}

// Learning-related methods

// LearningEngine returns the learning engine for direct access.
func (s *Service) LearningEngine() *learning.Engine {
	return s.learner
}

// IsLearningEnabled returns whether continuous learning is enabled.
func (s *Service) IsLearningEnabled() bool {
	return s.learner != nil
}

// LearnFromSuccess records a successful task execution for learning.
func (s *Service) LearnFromSuccess(ctx context.Context, taskID, query, output, model, strategy, domain string, confidence float64) error {
	if s.learner == nil {
		return nil
	}
	s.mu.RLock()
	sessionID := s.sessionID
	s.mu.RUnlock()

	return s.learner.LearnSuccess(ctx, sessionID, taskID, query, output, model, strategy, domain, confidence)
}

// LearnFromCorrection records a user correction for learning.
func (s *Service) LearnFromCorrection(ctx context.Context, taskID, query, original, corrected, correctionType, explanation, domain string, severity float64) error {
	if s.learner == nil {
		return nil
	}
	s.mu.RLock()
	sessionID := s.sessionID
	s.mu.RUnlock()

	return s.learner.LearnCorrection(ctx, sessionID, taskID, query, original, corrected, correctionType, explanation, domain, severity)
}

// LearnFromError records an error for learning (to avoid in future).
func (s *Service) LearnFromError(ctx context.Context, taskID, query, domain string, err error) error {
	if s.learner == nil {
		return nil
	}
	s.mu.RLock()
	sessionID := s.sessionID
	s.mu.RUnlock()

	return s.learner.LearnError(ctx, sessionID, taskID, query, domain, err)
}

// LearnPreference records a user preference.
func (s *Service) LearnPreference(ctx context.Context, key string, value interface{}, scope learning.PreferenceScope, scopeValue string, explicit bool) error {
	if s.learner == nil {
		return nil
	}
	s.mu.RLock()
	sessionID := s.sessionID
	s.mu.RUnlock()

	return s.learner.LearnPreference(ctx, sessionID, key, value, scope, scopeValue, explicit)
}

// GetLearnedContext retrieves learned knowledge relevant to a query.
func (s *Service) GetLearnedContext(ctx context.Context, query, domain, projectPath string) ([]string, error) {
	if s.learner == nil {
		return nil, nil
	}
	return s.learner.GetContextEnhancements(ctx, query, domain, projectPath)
}

// LearningStats returns statistics about learned knowledge.
func (s *Service) LearningStats(ctx context.Context) (*learning.EngineStats, error) {
	if s.learner == nil {
		return nil, nil
	}
	return s.learner.Stats(ctx)
}

// Budget-related methods

// BudgetManager returns the budget manager for direct access.
func (s *Service) BudgetManager() *budget.Manager {
	return s.budgetMgr
}

// IsBudgetEnabled returns whether budget tracking is enabled.
func (s *Service) IsBudgetEnabled() bool {
	return s.budgetMgr != nil
}

// BudgetState returns the current budget state.
func (s *Service) BudgetState() budget.State {
	if s.budgetMgr == nil {
		return budget.State{}
	}
	return s.budgetMgr.State()
}

// BudgetLimits returns the current budget limits.
func (s *Service) BudgetLimits() budget.Limits {
	if s.budgetMgr == nil {
		return budget.Limits{}
	}
	return s.budgetMgr.Limits()
}

// BudgetUsage returns current usage percentages.
func (s *Service) BudgetUsage() budget.Usage {
	if s.budgetMgr == nil {
		return budget.Usage{}
	}
	return s.budgetMgr.Usage()
}

// BudgetReport generates a budget report.
func (s *Service) BudgetReport() budget.Report {
	if s.budgetMgr == nil {
		return budget.Report{}
	}
	return s.budgetMgr.Report()
}

// CheckBudget checks if an operation can proceed given estimated cost.
func (s *Service) CheckBudget(estimatedInputTokens, estimatedOutputTokens int64, inputCost, outputCost float64) *budget.BudgetCheck {
	if s.budgetMgr == nil {
		return &budget.BudgetCheck{CanProceed: true}
	}
	return s.budgetMgr.CheckBudget(estimatedInputTokens, estimatedOutputTokens, inputCost, outputCost)
}

// SetBudgetEventCallback sets the callback for budget events.
func (s *Service) SetBudgetEventCallback(cb func(budget.Event)) {
	if s.budgetMgr != nil {
		s.budgetMgr.SetEventCallback(cb)
	}
}

// UpdateBudgetLimits updates the budget limits.
func (s *Service) UpdateBudgetLimits(limits budget.Limits) {
	if s.budgetMgr != nil {
		s.budgetMgr.UpdateLimits(limits)
	}
}

// =============================================================================
// Hallucination Detection [SPEC-08.19-22]
// =============================================================================

// OutputVerifier returns the output verifier for agent response verification.
// Returns nil if output verification is not enabled.
func (s *Service) OutputVerifier() *hallucination.OutputVerifier {
	return s.outputVerifier
}

// Detector returns the hallucination detector.
// Returns nil if hallucination detection is not enabled.
func (s *Service) Detector() *hallucination.Detector {
	return s.detector
}

// IsOutputVerificationEnabled returns whether output verification is enabled.
func (s *Service) IsOutputVerificationEnabled() bool {
	return s.outputVerifier != nil && s.outputVerifier.Enabled()
}

// TraceAuditor returns the trace auditor for reasoning trace verification.
// [SPEC-08.23-26] Returns nil if trace auditing is not enabled.
func (s *Service) TraceAuditor() *hallucination.TraceAuditor {
	return s.traceAuditor
}

// IsTraceAuditEnabled returns whether trace auditing is enabled.
func (s *Service) IsTraceAuditEnabled() bool {
	return s.traceAuditor != nil && s.traceAuditor.Enabled()
}

// VerifyOutput verifies an agent response against conversation context.
// [SPEC-08.19] Agent responses SHALL be verified before returning to the user.
// [SPEC-08.22] SHOULD NOT automatically reject; flags high-risk responses instead.
// Returns nil result if verification is disabled or not applicable.
func (s *Service) VerifyOutput(ctx context.Context, response string, conversationContext string) (*hallucination.OutputVerificationResult, error) {
	if s.outputVerifier == nil {
		return nil, nil
	}
	return s.outputVerifier.VerifyOutput(ctx, response, conversationContext)
}

// GetSelfCorrectionHint returns a hint for agent self-correction based on verification results.
// [SPEC-08.21] Verification results SHOULD be made available to the agent for self-correction.
func (s *Service) GetSelfCorrectionHint(result *hallucination.OutputVerificationResult) *hallucination.SelfCorrectionHint {
	if s.outputVerifier == nil || result == nil {
		return nil
	}
	return s.outputVerifier.GetVerificationResultForSelfCorrection(result)
}

// wrapperContextPreparer adapts the Wrapper to the orchestrator.ContextPreparer interface.
// [SPEC-09.06] This allows the orchestrator.Core to use the wrapper for context preparation.
type wrapperContextPreparer struct {
	wrapper *Wrapper
}

// PrepareContext implements orchestrator.ContextPreparer.
func (w *wrapperContextPreparer) PrepareContext(ctx context.Context, task string, contextTokens int) (*orchestrator.PreparedContext, error) {
	if w.wrapper == nil {
		return nil, fmt.Errorf("wrapper not initialized")
	}

	// Call the wrapper's PrepareContext with task as prompt and no additional contexts
	prepared, err := w.wrapper.PrepareContext(ctx, task, nil)
	if err != nil {
		return nil, err
	}

	// Convert PreparedPrompt to orchestrator.PreparedContext
	result := &orchestrator.PreparedContext{
		Mode:         string(prepared.Mode),
		SystemPrompt: prepared.SystemPrompt,
	}

	// Check if context was externalized (loaded into REPL)
	if prepared.LoadedContext != nil {
		result.Externalized = true
		result.ExternalizedTokens = prepared.LoadedContext.TotalTokens
	}

	return result, nil
}
