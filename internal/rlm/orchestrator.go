// Package rlm provides the orchestrator for intelligent prompt pre-processing.
//
// This file provides backwards-compatible exports from the modular orchestrator package.
// New code should import github.com/rand/recurse/internal/rlm/orchestrator directly.
package rlm

import (
	"context"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/orchestrator"
	"github.com/rand/recurse/internal/rlm/repl"
)

// Re-export types from orchestrator package for backwards compatibility.
type (
	AnalysisResult = orchestrator.AnalysisResult
	ContextNeeds   = orchestrator.ContextNeeds
	TaskRouting    = orchestrator.TaskRouting
	Subtask        = orchestrator.Subtask
	LoadedContext  = orchestrator.LoadedContext
	VariableInfo   = orchestrator.VariableInfo
	ContextType    = orchestrator.ContextType
	ContextSource  = orchestrator.ContextSource
)

// Re-export constants.
const (
	ContextTypeFile   = orchestrator.ContextTypeFile
	ContextTypeSearch = orchestrator.ContextTypeSearch
	ContextTypeMemory = orchestrator.ContextTypeMemory
	ContextTypeCustom = orchestrator.ContextTypeCustom
	ContextTypePrompt = orchestrator.ContextTypePrompt
)

// Orchestrator handles intelligent prompt pre-processing and task routing.
// This wraps the modular orchestrator.Intelligent and orchestrator.Steering types.
type Orchestrator struct {
	intelligent    *orchestrator.Intelligent
	steering       *orchestrator.Steering
	models         []meta.ModelSpec
	enabled        bool
	contextEnabled bool
}

// OrchestratorConfig configures the orchestrator.
type OrchestratorConfig struct {
	// Enabled controls whether orchestration is active.
	Enabled bool

	// Models is the model catalog for routing decisions.
	Models []meta.ModelSpec

	// ContextEnabled enables context externalization to REPL.
	ContextEnabled bool
}

// NewOrchestrator creates a new RLM orchestrator.
func NewOrchestrator(metaCtrl *meta.Controller, cfg OrchestratorConfig) *Orchestrator {
	models := cfg.Models
	if len(models) == 0 {
		models = meta.DefaultModels()
	}

	return &Orchestrator{
		intelligent: orchestrator.NewIntelligent(metaCtrl, orchestrator.IntelligentConfig{
			Enabled: cfg.Enabled,
			Models:  models,
		}),
		steering: orchestrator.NewSteering(orchestrator.SteeringConfig{
			ContextEnabled: cfg.ContextEnabled,
		}),
		models:         models,
		enabled:        cfg.Enabled,
		contextEnabled: cfg.ContextEnabled,
	}
}

// SetREPLManager sets the REPL manager for context externalization.
func (o *Orchestrator) SetREPLManager(replMgr *repl.Manager) {
	o.steering.SetREPLManager(replMgr)
}

// ContextLoader returns the context loader.
func (o *Orchestrator) ContextLoader() *orchestrator.ContextLoader {
	return o.steering.ContextLoader()
}

// IsContextEnabled returns whether context externalization is enabled.
func (o *Orchestrator) IsContextEnabled() bool {
	return o.steering.IsContextEnabled()
}

// SetContextEnabled enables or disables context externalization.
func (o *Orchestrator) SetContextEnabled(enabled bool) {
	o.contextEnabled = enabled
	o.steering.SetContextEnabled(enabled)
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

// ExternalizeContext loads context sources into the REPL for manipulation.
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

// IsEnabled returns whether orchestration is enabled.
func (o *Orchestrator) IsEnabled() bool {
	return o.intelligent.IsEnabled()
}

// SetEnabled enables or disables orchestration.
func (o *Orchestrator) SetEnabled(enabled bool) {
	o.enabled = enabled
	o.intelligent.SetEnabled(enabled)
}

// HasREPL returns whether a REPL manager is available.
func (o *Orchestrator) HasREPL() bool {
	return o.steering.HasREPL()
}
