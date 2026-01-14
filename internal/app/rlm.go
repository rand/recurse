package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"github.com/rand/recurse/internal/rlm"
	"github.com/rand/recurse/internal/rlm/meta"
)

// InitRLM initializes the RLM service with intelligent model routing.
// Prefers OpenRouter (multi-model routing) when available, falls back to Anthropic.
func (app *App) InitRLM(ctx context.Context) error {
	// Try OpenRouter first for intelligent model routing
	llmClient, clientType, err := app.createRLMClient()
	if err != nil {
		slog.Info("RLM not initialized: no LLM provider available", "error", err)
		return nil // Non-fatal - RLM is optional
	}

	// Configure RLM service
	rlmCfg := rlm.DefaultServiceConfig()

	// Use project-specific storage paths
	rlmCfg.StorePath = filepath.Join(app.config.Options.DataDirectory, "rlm.db")
	rlmCfg.TracePath = filepath.Join(app.config.Options.DataDirectory, "rlm_trace.db")

	// Configure checkpoint for session state persistence
	rlmCfg.Checkpoint.Path = app.config.Options.DataDirectory

	// Create service
	svc, err := rlm.NewService(llmClient, rlmCfg)
	if err != nil {
		return fmt.Errorf("create RLM service: %w", err)
	}

	// Start the service
	if err := svc.Start(ctx); err != nil {
		svc.Stop()
		return fmt.Errorf("start RLM service: %w", err)
	}

	app.RLM = svc

	// Create memory store adapter for TUI memory inspector
	if store := svc.Store(); store != nil {
		app.MemoryStore = NewMemoryStoreAdapter(store)
	}

	// Add cleanup
	app.cleanupFuncs = append(app.cleanupFuncs, func() error {
		if app.RLM != nil {
			return app.RLM.Stop()
		}
		return nil
	})

	slog.Info("RLM service initialized",
		"provider", clientType,
		"store", rlmCfg.StorePath,
		"trace", rlmCfg.TracePath)

	// Check for recoverable checkpoint from previous session
	if cp, err := svc.LoadCheckpoint(); err == nil && cp != nil && cp.IsRecoverable() {
		slog.Info("Recoverable session checkpoint found",
			"session_id", cp.SessionID,
			"summary", cp.Summary())
	}

	return nil
}

// createRLMClient creates the best available LLM client for RLM.
// Tries OpenRouter first (intelligent routing), then Anthropic (single model).
func (app *App) createRLMClient() (meta.LLMClient, string, error) {
	// Try OpenRouter first (enables intelligent multi-model routing)
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		client, err := meta.NewOpenRouterClient(meta.OpenRouterConfig{
			APIKey: apiKey,
		})
		if err == nil {
			slog.Info("RLM using OpenRouter with intelligent model routing")
			return client, "openrouter", nil
		}
		slog.Warn("Failed to create OpenRouter client, trying Anthropic", "error", err)
	}

	// Fall back to Anthropic (single model - Haiku)
	provider, err := app.findAnthropicProvider()
	if err != nil {
		return nil, "", fmt.Errorf("no LLM provider available: %w", err)
	}

	client, err := meta.NewHaikuClient(meta.HaikuConfig{
		Provider: provider,
		Model:    "claude-3-5-haiku-latest",
	})
	if err != nil {
		return nil, "", fmt.Errorf("create Haiku client: %w", err)
	}

	slog.Info("RLM using Anthropic Haiku (single model)")
	return client, "anthropic-haiku", nil
}

// findAnthropicProvider finds an Anthropic provider from the config.
func (app *App) findAnthropicProvider() (fantasy.Provider, error) {
	// Look for an Anthropic provider
	for _, providerCfg := range app.config.EnabledProviders() {
		if providerCfg.Type != anthropic.Name {
			continue
		}

		apiKey, _ := app.config.Resolve(providerCfg.APIKey)
		if apiKey == "" {
			continue
		}

		var opts []anthropic.Option
		opts = append(opts, anthropic.WithAPIKey(apiKey))

		if baseURL, _ := app.config.Resolve(providerCfg.BaseURL); baseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(baseURL))
		}

		provider, err := anthropic.New(opts...)
		if err != nil {
			slog.Warn("Failed to create Anthropic provider", "error", err)
			continue
		}

		return provider, nil
	}

	return nil, fmt.Errorf("no Anthropic provider found")
}

// RLMExecute executes a task through the RLM service.
func (app *App) RLMExecute(ctx context.Context, task string) (*rlm.ExecutionResult, error) {
	if app.RLM == nil {
		return nil, fmt.Errorf("RLM service not initialized")
	}
	return app.RLM.Execute(ctx, task)
}

// RLMTaskComplete signals task completion to the RLM lifecycle manager.
func (app *App) RLMTaskComplete(ctx context.Context) error {
	if app.RLM == nil {
		return nil
	}
	_, err := app.RLM.TaskComplete(ctx)
	return err
}

// RLMSessionEnd signals session end to the RLM lifecycle manager.
func (app *App) RLMSessionEnd(ctx context.Context) error {
	if app.RLM == nil {
		return nil
	}
	_, err := app.RLM.SessionEnd(ctx)
	return err
}
