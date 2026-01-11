package rlm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rand/recurse/internal/memory/evolution"
	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/meta"
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
}

// DefaultServiceConfig returns sensible defaults for the RLM service.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		Controller:          DefaultControllerConfig(),
		Lifecycle:           evolution.DefaultLifecycleConfig(),
		Meta:                meta.DefaultConfig(),
		MaxTraceEvents:      1000,
		StorePath:           "",
		OrchestratorEnabled: true, // Enable orchestration by default
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
	controller      *Controller
	lifecycle       *evolution.LifecycleManager
	tracer          traceRecorder
	persistentTrace *PersistentTraceProvider // non-nil if using persistent storage
	orchestrator    *Orchestrator            // prompt pre-processing

	// Configuration
	config ServiceConfig

	// State
	running   bool
	startTime time.Time

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

	// Create meta-controller
	metaCtrl := meta.NewController(llmClient, config.Meta)

	// Create RLM controller
	controller := NewController(metaCtrl, store, config.Controller)

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

	// Create orchestrator for prompt pre-processing
	orchestrator := NewOrchestrator(metaCtrl, OrchestratorConfig{
		Enabled:        config.OrchestratorEnabled,
		Models:         meta.DefaultModels(),
		ContextEnabled: true, // Enable context externalization by default
	})

	svc := &Service{
		store:           store,
		controller:      controller,
		lifecycle:       lifecycle,
		tracer:          tracer,
		persistentTrace: persistentTrace,
		orchestrator:    orchestrator,
		config:          config,
	}

	// Wire up lifecycle callbacks for statistics
	lifecycle.OnTaskComplete(func(result *evolution.LifecycleResult) {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		svc.stats.TasksCompleted++
	})

	lifecycle.OnSessionEnd(func(result *evolution.LifecycleResult) {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		svc.stats.SessionsEnded++
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

	// Start idle maintenance loop
	s.lifecycle.StartIdleLoop(ctx)

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
	s.mu.Unlock()

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
	s.mu.Unlock()

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

// LifecycleManager returns the lifecycle manager for direct access.
func (s *Service) LifecycleManager() *evolution.LifecycleManager {
	return s.lifecycle
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
}

// AnalyzePrompt performs RLM analysis on a user prompt.
// This should be called before sending the prompt to the main agent.
func (s *Service) AnalyzePrompt(ctx context.Context, prompt string, contextTokens int) (*AnalysisResult, error) {
	if s.orchestrator == nil {
		return &AnalysisResult{
			OriginalPrompt: prompt,
			EnhancedPrompt: prompt,
		}, nil
	}
	return s.orchestrator.Analyze(ctx, prompt, contextTokens)
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
