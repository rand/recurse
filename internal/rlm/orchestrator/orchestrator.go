package orchestrator

import (
	"context"

	"github.com/rand/recurse/internal/memory/hypergraph"
	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
)

// Orchestrator is the main entry point that composes all orchestration modules.
// It provides a unified interface for RLM orchestration with:
// - Core execution loop (Core)
// - Intelligent decision making (Intelligent)
// - User interaction and steering (Steering)
// - Async execution (via Core)
// - Checkpointing (CheckpointManager)
type Orchestrator struct {
	core        *Core
	intelligent *Intelligent
	steering    *Steering
	checkpoint  *CheckpointManager
}

// Config configures the unified orchestrator.
type Config struct {
	// Core configuration
	Core CoreConfig

	// Intelligent analysis configuration
	Intelligent IntelligentConfig

	// Steering configuration
	Steering SteeringConfig

	// Checkpoint configuration
	Checkpoint CheckpointConfig
}

// DefaultConfig returns sensible defaults for all modules.
func DefaultConfig() Config {
	return Config{
		Core:        DefaultCoreConfig(),
		Intelligent: IntelligentConfig{Enabled: true},
		Steering:    SteeringConfig{ContextEnabled: true},
		Checkpoint:  DefaultCheckpointConfig(),
	}
}

// New creates a new unified orchestrator.
func New(
	metaCtrl *meta.Controller,
	mainClient meta.LLMClient,
	store *hypergraph.Store,
	cfg Config,
) *Orchestrator {
	return &Orchestrator{
		core:        NewCore(metaCtrl, mainClient, store, cfg.Core),
		intelligent: NewIntelligent(metaCtrl, cfg.Intelligent),
		steering:    NewSteering(cfg.Steering),
		checkpoint:  NewCheckpointManager(cfg.Checkpoint),
	}
}

// SetREPLManager sets the REPL manager for context externalization.
func (o *Orchestrator) SetREPLManager(replMgr *repl.Manager) {
	o.steering.SetREPLManager(replMgr)
}

// SetTracer sets the trace recorder for execution tracing.
func (o *Orchestrator) SetTracer(tracer TraceRecorder) {
	o.core.SetTracer(tracer)
}

// Execute runs the RLM orchestration loop for a task.
func (o *Orchestrator) Execute(ctx context.Context, task string) (*ExecutionResult, error) {
	return o.core.Execute(ctx, task)
}

// Analyze performs intelligent analysis of a user prompt.
func (o *Orchestrator) Analyze(ctx context.Context, prompt string, contextTokens int) (*AnalysisResult, error) {
	result, err := o.intelligent.Analyze(ctx, prompt, contextTokens)
	if err != nil {
		return nil, err
	}

	// Enhance prompt with steering if context externalization is enabled
	if o.steering.IsContextEnabled() && result != nil {
		result.EnhancedPrompt = o.steering.EnhancePrompt(prompt, result)
	}

	return result, nil
}

// ExternalizeContext loads context sources into the REPL.
func (o *Orchestrator) ExternalizeContext(ctx context.Context, sources []ContextSource) (*LoadedContext, error) {
	return o.steering.ExternalizeContext(ctx, sources)
}

// ExternalizePrompt stores the user's prompt as a REPL variable.
func (o *Orchestrator) ExternalizePrompt(ctx context.Context, prompt string) (*LoadedContext, error) {
	return o.steering.ExternalizePrompt(ctx, prompt)
}

// GenerateRLMSystemPrompt generates a system prompt for RLM-style execution.
func (o *Orchestrator) GenerateRLMSystemPrompt(loaded *LoadedContext) string {
	return o.steering.GenerateRLMSystemPrompt(loaded)
}

// StartCheckpointing begins periodic checkpointing.
func (o *Orchestrator) StartCheckpointing(ctx context.Context) {
	o.checkpoint.Start(ctx)
}

// StopCheckpointing stops periodic checkpointing.
func (o *Orchestrator) StopCheckpointing() error {
	return o.checkpoint.Stop()
}

// SaveCheckpoint saves the current state immediately.
func (o *Orchestrator) SaveCheckpoint() error {
	return o.checkpoint.Save()
}

// LoadCheckpoint loads the most recent checkpoint.
func (o *Orchestrator) LoadCheckpoint() (*Checkpoint, error) {
	return o.checkpoint.Load()
}

// HasCheckpoint returns true if a valid checkpoint exists.
func (o *Orchestrator) HasCheckpoint() bool {
	return o.checkpoint.HasCheckpoint()
}

// ClearCheckpoint removes the checkpoint file.
func (o *Orchestrator) ClearCheckpoint() error {
	return o.checkpoint.Clear()
}

// IsEnabled returns whether intelligent orchestration is enabled.
func (o *Orchestrator) IsEnabled() bool {
	return o.intelligent.IsEnabled()
}

// SetEnabled enables or disables intelligent orchestration.
func (o *Orchestrator) SetEnabled(enabled bool) {
	o.intelligent.SetEnabled(enabled)
}

// IsContextEnabled returns whether context externalization is enabled.
func (o *Orchestrator) IsContextEnabled() bool {
	return o.steering.IsContextEnabled()
}

// SetContextEnabled enables or disables context externalization.
func (o *Orchestrator) SetContextEnabled(enabled bool) {
	o.steering.SetContextEnabled(enabled)
}

// HasREPL returns whether a REPL manager is available.
func (o *Orchestrator) HasREPL() bool {
	return o.steering.HasREPL()
}

// ContextLoader returns the context loader for direct access.
func (o *Orchestrator) ContextLoader() *ContextLoader {
	return o.steering.ContextLoader()
}

// Core returns the core orchestration module for direct access.
func (o *Orchestrator) Core() *Core {
	return o.core
}

// Intelligent returns the intelligent analysis module for direct access.
func (o *Orchestrator) Intelligent() *Intelligent {
	return o.intelligent
}

// Steering returns the steering module for direct access.
func (o *Orchestrator) Steering() *Steering {
	return o.steering
}

// Checkpoint returns the checkpoint manager for direct access.
func (o *Orchestrator) Checkpoint() *CheckpointManager {
	return o.checkpoint
}
