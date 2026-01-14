package rlm

import (
	"context"
	"testing"

	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_Analyze_Disabled(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled: false,
	})

	result, err := orchestrator.Analyze(context.Background(), "test prompt", 100)
	require.NoError(t, err)
	assert.Equal(t, "test prompt", result.OriginalPrompt)
	assert.Equal(t, "test prompt", result.EnhancedPrompt)
	assert.Nil(t, result.Decision)
}

func TestOrchestrator_EnableDisable(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled: true,
	})

	assert.True(t, orchestrator.IsEnabled())

	orchestrator.SetEnabled(false)
	assert.False(t, orchestrator.IsEnabled())

	orchestrator.SetEnabled(true)
	assert.True(t, orchestrator.IsEnabled())
}

func TestOrchestrator_ContextEnabled(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled:        true,
		ContextEnabled: true,
	})

	// Initially enabled in config, but no REPL manager
	assert.False(t, orchestrator.IsContextEnabled())

	orchestrator.SetContextEnabled(false)
	assert.False(t, orchestrator.IsContextEnabled())
}

func TestOrchestrator_Models(t *testing.T) {
	models := meta.DefaultModels()
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{
		Enabled: true,
		Models:  models,
	})

	assert.NotNil(t, orchestrator)
}

func TestNewOrchestrator(t *testing.T) {
	orchestrator := NewOrchestrator(nil, OrchestratorConfig{})
	require.NotNil(t, orchestrator)
}

func TestOrchestratorConfig_Defaults(t *testing.T) {
	cfg := OrchestratorConfig{}

	// Default values
	assert.False(t, cfg.Enabled)
	assert.False(t, cfg.ContextEnabled)
	assert.Nil(t, cfg.Models)
}
