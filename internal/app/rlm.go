package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"github.com/rand/recurse/internal/rlm"
	"github.com/rand/recurse/internal/rlm/meta"
)

// InitRLM initializes the RLM service if an Anthropic provider is configured.
func (app *App) InitRLM(ctx context.Context) error {
	// Find an Anthropic provider for the meta-controller
	provider, err := app.findAnthropicProvider()
	if err != nil {
		slog.Info("RLM not initialized: no Anthropic provider configured", "error", err)
		return nil // Non-fatal - RLM is optional
	}

	// Create Haiku client for meta-controller
	haikuClient, err := meta.NewHaikuClient(meta.HaikuConfig{
		Provider: provider,
		Model:    "claude-3-5-haiku-latest",
	})
	if err != nil {
		return fmt.Errorf("create haiku client: %w", err)
	}

	// Configure RLM service
	rlmCfg := rlm.DefaultServiceConfig()

	// Use project-specific storage path
	rlmCfg.StorePath = filepath.Join(app.config.Options.DataDirectory, "rlm.db")

	// Create service
	svc, err := rlm.NewService(haikuClient, rlmCfg)
	if err != nil {
		return fmt.Errorf("create RLM service: %w", err)
	}

	// Start the service
	if err := svc.Start(ctx); err != nil {
		svc.Stop()
		return fmt.Errorf("start RLM service: %w", err)
	}

	app.RLM = svc

	// Add cleanup
	app.cleanupFuncs = append(app.cleanupFuncs, func() error {
		if app.RLM != nil {
			return app.RLM.Stop()
		}
		return nil
	})

	slog.Info("RLM service initialized", "store", rlmCfg.StorePath)
	return nil
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
