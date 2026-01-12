package benchmark

import (
	"context"
	"fmt"
	"time"

	"github.com/rand/recurse/internal/rlm"
	"github.com/rand/recurse/internal/rlm/meta"
	"github.com/rand/recurse/internal/rlm/repl"
)

// RealRLMExecutor runs benchmark tasks using the actual RLM system.
type RealRLMExecutor struct {
	service   *rlm.Service
	replMgr   *repl.Manager
	llmClient meta.LLMClient
}

// RealExecutorConfig configures the real RLM executor.
type RealExecutorConfig struct {
	// OpenRouterAPIKey is the API key for OpenRouter.
	OpenRouterAPIKey string

	// UseREPL enables the Python REPL for context externalization.
	UseREPL bool

	// REPLOptions configures the Python REPL.
	REPLOptions repl.Options

	// REPLTimeout overrides the default REPL execution timeout.
	// For RLM mode with LLM callbacks, this should be several minutes.
	// Default is 5 minutes if not set.
	REPLTimeout time.Duration
}

// NewRealRLMExecutor creates an executor using the actual RLM system.
func NewRealRLMExecutor(cfg RealExecutorConfig) (*RealRLMExecutor, error) {
	// Create OpenRouter client
	llmClient, err := meta.NewOpenRouterClient(meta.OpenRouterConfig{
		APIKey: cfg.OpenRouterAPIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create LLM client: %w", err)
	}

	// Create RLM service
	svcConfig := rlm.DefaultServiceConfig()
	service, err := rlm.NewService(llmClient, svcConfig)
	if err != nil {
		return nil, fmt.Errorf("create RLM service: %w", err)
	}

	executor := &RealRLMExecutor{
		service:   service,
		llmClient: llmClient,
	}

	// Configure wrapper with LLM client for RLM execution
	if service.Wrapper() != nil {
		service.Wrapper().SetLLMClient(llmClient)
	}

	// Optionally set up REPL with appropriate timeout for LLM callbacks
	if cfg.UseREPL {
		replOpts := cfg.REPLOptions

		// Set timeout for REPL operations - must be long enough for LLM callbacks
		// Default to 5 minutes if not specified, since LLM calls can take minutes
		replTimeout := cfg.REPLTimeout
		if replTimeout == 0 {
			replTimeout = 5 * time.Minute
		}
		replOpts.Sandbox.Timeout = replTimeout

		replMgr, err := repl.NewManager(replOpts)
		if err != nil {
			service.Stop()
			return nil, fmt.Errorf("create REPL manager: %w", err)
		}
		executor.replMgr = replMgr
		service.SetREPLManager(replMgr)
	}

	return executor, nil
}

// Start starts the RLM service.
func (e *RealRLMExecutor) Start(ctx context.Context) error {
	if e.replMgr != nil {
		if err := e.replMgr.Start(ctx); err != nil {
			return fmt.Errorf("start REPL: %w", err)
		}
	}
	return e.service.Start(ctx)
}

// Stop stops the RLM service and cleans up.
func (e *RealRLMExecutor) Stop() error {
	var errs []error
	if e.replMgr != nil {
		if err := e.replMgr.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := e.service.Stop(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("stop errors: %v", errs)
	}
	return nil
}

// Execute runs a benchmark task using the RLM system.
func (e *RealRLMExecutor) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
	start := time.Now()

	result := Result{
		TaskID:   task.ID,
		Metadata: make(map[string]any),
	}

	// Apply timeout
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	// Determine execution mode
	if config.UseRLM && e.replMgr != nil {
		// Full RLM mode with context externalization
		rlmResult, err := e.executeRLM(ctx, task, config)
		result.Duration = time.Since(start)

		if err != nil {
			result.Error = err.Error()
			return result, nil
		}

		result.Answer = rlmResult.Answer
		result.Iterations = rlmResult.Iterations
		result.PromptTokens = rlmResult.PromptTokens
		result.CompletionTokens = rlmResult.CompletionTokens
		result.TotalTokens = rlmResult.TotalTokens
		result.Metadata["rlm_mode"] = true
	} else {
		// Direct prompting mode
		directResult, err := e.executeDirect(ctx, task, config)
		result.Duration = time.Since(start)

		if err != nil {
			result.Error = err.Error()
			return result, nil
		}

		result.Answer = directResult.Answer
		result.PromptTokens = directResult.PromptTokens
		result.CompletionTokens = directResult.CompletionTokens
		result.TotalTokens = directResult.TotalTokens
		result.Iterations = 0
		result.Metadata["rlm_mode"] = false
	}

	return result, nil
}

// executeRLM runs a task using full RLM context externalization.
func (e *RealRLMExecutor) executeRLM(ctx context.Context, task Task, config RunConfig) (*rlmExecutionResult, error) {
	wrapper := e.service.Wrapper()
	if wrapper == nil {
		return nil, fmt.Errorf("RLM wrapper not available")
	}

	// Prepare context sources
	contexts := []rlm.ContextSource{
		{
			Name:    "benchmark_context",
			Type:    rlm.ContextTypeCustom,
			Content: task.Context,
			Metadata: map[string]any{
				"source": "benchmark_task",
				"tokens": task.ContextTokens,
			},
		},
	}

	// Prepare the context (externalize to REPL if needed)
	prepared, err := wrapper.PrepareContext(ctx, task.Query, contexts)
	if err != nil {
		return nil, fmt.Errorf("prepare context: %w", err)
	}

	result := &rlmExecutionResult{}

	if prepared.Mode == rlm.ModeRLM {
		// Execute RLM loop
		rlmConfig := rlm.RLMConfig{
			MaxIterations:    config.MaxIterations,
			MaxTokensPerCall: config.MaxTokensPerCall,
			Timeout:          config.Timeout,
		}

		execResult, err := wrapper.ExecuteRLMWithConfig(ctx, prepared, rlmConfig)
		if err != nil {
			return nil, fmt.Errorf("RLM execution: %w", err)
		}

		result.Answer = execResult.FinalOutput
		result.Iterations = execResult.Iterations
		result.TotalTokens = execResult.TotalTokens
		// Estimate token split (rough)
		result.PromptTokens = result.TotalTokens * 3 / 4
		result.CompletionTokens = result.TotalTokens / 4

		if execResult.Error != "" {
			return nil, fmt.Errorf("RLM error: %s", execResult.Error)
		}
	} else {
		// Direct mode (small context)
		return e.executeDirect(ctx, task, config)
	}

	return result, nil
}

// executeDirect runs a task using direct prompting.
func (e *RealRLMExecutor) executeDirect(ctx context.Context, task Task, config RunConfig) (*rlmExecutionResult, error) {
	// Build direct prompt
	prompt := buildDirectPrompt(task)

	// Call LLM directly
	response, err := e.llmClient.Complete(ctx, prompt, config.MaxTokensPerCall)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	result := &rlmExecutionResult{
		Answer:           extractAnswer(response),
		PromptTokens:     estimateTokens(prompt),
		CompletionTokens: estimateTokens(response),
		Iterations:       0,
	}
	result.TotalTokens = result.PromptTokens + result.CompletionTokens

	return result, nil
}

// rlmExecutionResult is the internal result from RLM execution.
type rlmExecutionResult struct {
	Answer           string
	Iterations       int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Service returns the underlying RLM service.
func (e *RealRLMExecutor) Service() *rlm.Service {
	return e.service
}

// RealDirectExecutor runs benchmark tasks using direct LLM calls only.
type RealDirectExecutor struct {
	llmClient meta.LLMClient
}

// NewRealDirectExecutor creates an executor for direct prompting.
func NewRealDirectExecutor(apiKey string) (*RealDirectExecutor, error) {
	llmClient, err := meta.NewOpenRouterClient(meta.OpenRouterConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create LLM client: %w", err)
	}

	return &RealDirectExecutor{
		llmClient: llmClient,
	}, nil
}

// Execute runs a task using direct prompting.
func (e *RealDirectExecutor) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
	start := time.Now()

	result := Result{
		TaskID:   task.ID,
		Metadata: make(map[string]any),
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	prompt := buildDirectPrompt(task)

	response, err := e.llmClient.Complete(ctx, prompt, config.MaxTokensPerCall)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	result.Answer = extractAnswer(response)
	result.PromptTokens = estimateTokens(prompt)
	result.CompletionTokens = estimateTokens(response)
	result.TotalTokens = result.PromptTokens + result.CompletionTokens
	result.Iterations = 0
	result.Metadata["direct_mode"] = true

	return result, nil
}
