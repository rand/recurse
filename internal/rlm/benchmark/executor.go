package benchmark

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LLMClient is the interface for making LLM calls.
type LLMClient interface {
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// RLMExecutor runs tasks using the RLM approach.
type RLMExecutor struct {
	client    LLMClient
	rlmRunner RLMRunner
}

// RLMRunner executes RLM-style prompts with Python REPL.
type RLMRunner interface {
	// ExecuteRLM runs a prompt in RLM mode and returns the result.
	ExecuteRLM(ctx context.Context, prompt string, contextData string, config RLMRunConfig) (RLMResult, error)
}

// RLMRunConfig configures RLM execution.
type RLMRunConfig struct {
	MaxIterations    int
	MaxTokensPerCall int
	Timeout          time.Duration
}

// RLMResult is the result of an RLM execution.
type RLMResult struct {
	Answer           string
	Iterations       int
	PromptTokens     int
	CompletionTokens int
	Duration         time.Duration
	Error            string
}

// NewRLMExecutor creates an executor that uses RLM.
func NewRLMExecutor(client LLMClient, runner RLMRunner) *RLMExecutor {
	return &RLMExecutor{
		client:    client,
		rlmRunner: runner,
	}
}

// Execute runs a single benchmark task.
func (e *RLMExecutor) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
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

	if config.UseRLM && e.rlmRunner != nil {
		// RLM mode
		rlmResult, err := e.rlmRunner.ExecuteRLM(ctx, task.Query, task.Context, RLMRunConfig{
			MaxIterations:    config.MaxIterations,
			MaxTokensPerCall: config.MaxTokensPerCall,
			Timeout:          config.Timeout,
		})

		result.Duration = time.Since(start)

		if err != nil {
			result.Error = err.Error()
			return result, nil
		}

		result.Answer = rlmResult.Answer
		result.Iterations = rlmResult.Iterations
		result.PromptTokens = rlmResult.PromptTokens
		result.CompletionTokens = rlmResult.CompletionTokens
		result.TotalTokens = rlmResult.PromptTokens + rlmResult.CompletionTokens

		if rlmResult.Error != "" {
			result.Error = rlmResult.Error
		}
	} else {
		// Direct prompting mode (baseline)
		prompt := buildDirectPrompt(task)

		response, err := e.client.Complete(ctx, prompt, config.MaxTokensPerCall)
		result.Duration = time.Since(start)

		if err != nil {
			result.Error = err.Error()
			return result, nil
		}

		result.Answer = extractAnswer(response)
		result.Iterations = 0

		// Estimate tokens (rough approximation)
		result.PromptTokens = estimateTokens(prompt)
		result.CompletionTokens = estimateTokens(response)
		result.TotalTokens = result.PromptTokens + result.CompletionTokens
	}

	return result, nil
}

// buildDirectPrompt creates a prompt for direct (non-RLM) mode.
func buildDirectPrompt(task Task) string {
	var sb strings.Builder

	sb.WriteString("You are given a document and a question. Read the document carefully and answer the question.\n\n")
	sb.WriteString("=== DOCUMENT ===\n")
	sb.WriteString(task.Context)
	sb.WriteString("\n=== END DOCUMENT ===\n\n")
	sb.WriteString("Question: ")
	sb.WriteString(task.Query)
	sb.WriteString("\n\nAnswer:")

	return sb.String()
}

// extractAnswer extracts the answer from a model response.
// It returns the full response (trimmed) since answers may span multiple lines.
// The scorer handles the actual matching logic.
func extractAnswer(response string) string {
	return strings.TrimSpace(response)
}

// estimateTokens provides a rough token count estimate.
func estimateTokens(text string) int {
	// Rough estimate: ~4 characters per token
	return len(text) / 4
}

// DirectExecutor runs tasks using direct prompting only (no RLM).
type DirectExecutor struct {
	client LLMClient
}

// NewDirectExecutor creates an executor for direct prompting.
func NewDirectExecutor(client LLMClient) *DirectExecutor {
	return &DirectExecutor{client: client}
}

// Execute runs a task using direct prompting.
func (e *DirectExecutor) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
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

	response, err := e.client.Complete(ctx, prompt, config.MaxTokensPerCall)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	result.Answer = extractAnswer(response)
	result.PromptTokens = estimateTokens(prompt)
	result.CompletionTokens = estimateTokens(response)
	result.TotalTokens = result.PromptTokens + result.CompletionTokens

	return result, nil
}

// MockExecutor is a test executor that returns predetermined results.
type MockExecutor struct {
	responses map[string]string
	delay     time.Duration
}

// NewMockExecutor creates a mock executor for testing.
func NewMockExecutor(responses map[string]string) *MockExecutor {
	return &MockExecutor{
		responses: responses,
		delay:     10 * time.Millisecond,
	}
}

// Execute returns a mock result.
func (e *MockExecutor) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
	start := time.Now()

	// Simulate some work
	select {
	case <-time.After(e.delay):
	case <-ctx.Done():
		return Result{TaskID: task.ID, Error: ctx.Err().Error()}, nil
	}

	result := Result{
		TaskID:   task.ID,
		Duration: time.Since(start),
		Metadata: make(map[string]any),
	}

	// Look up response by task ID or use default
	if answer, ok := e.responses[task.ID]; ok {
		result.Answer = answer
	} else if answer, ok := e.responses["default"]; ok {
		result.Answer = answer
	} else {
		result.Answer = task.ExpectedAnswer // Perfect mock
	}

	result.PromptTokens = task.ContextTokens + 100
	result.CompletionTokens = 50
	result.TotalTokens = result.PromptTokens + result.CompletionTokens

	if config.UseRLM {
		result.Iterations = 3
	}

	return result, nil
}

// ComparisonRunner runs benchmarks with both RLM and direct modes for comparison.
type ComparisonRunner struct {
	rlmExecutor    Executor
	directExecutor Executor
	scorer         Scorer
}

// NewComparisonRunner creates a runner that compares RLM vs direct prompting.
func NewComparisonRunner(rlmExecutor, directExecutor Executor, scorer Scorer) *ComparisonRunner {
	return &ComparisonRunner{
		rlmExecutor:    rlmExecutor,
		directExecutor: directExecutor,
		scorer:         scorer,
	}
}

// ComparisonReport contains results from both approaches.
type ComparisonReport struct {
	SuiteName string
	RLM       *Report
	Direct    *Report
	Comparison ComparisonSummary
}

// ComparisonSummary compares RLM vs direct results.
type ComparisonSummary struct {
	// AccuracyImprovement is (RLM accuracy - Direct accuracy) / Direct accuracy.
	AccuracyImprovement float64

	// ScoreImprovement is (RLM score - Direct score) / Direct score.
	ScoreImprovement float64

	// TokenOverhead is (RLM tokens - Direct tokens) / Direct tokens.
	TokenOverhead float64

	// SpeedupFactor is Direct duration / RLM duration.
	SpeedupFactor float64

	// ByComplexity breaks down improvements by task complexity.
	ByComplexity map[TaskComplexity]ComplexityComparison
}

// ComplexityComparison compares results for a specific complexity level.
type ComplexityComparison struct {
	RLMAccuracy       float64
	DirectAccuracy    float64
	AccuracyImprovement float64
}

// Run executes the benchmark suite with both approaches.
func (r *ComparisonRunner) Run(ctx context.Context, suite Suite, baseConfig RunConfig) (*ComparisonReport, error) {
	report := &ComparisonReport{
		SuiteName: suite.Name,
	}

	// Run with RLM
	rlmConfig := baseConfig
	rlmConfig.UseRLM = true
	rlmRunner := NewRunner(r.rlmExecutor, r.scorer)
	rlmReport, err := rlmRunner.Run(ctx, suite, rlmConfig)
	if err != nil {
		return nil, fmt.Errorf("RLM run failed: %w", err)
	}
	report.RLM = rlmReport

	// Run with direct prompting
	directConfig := baseConfig
	directConfig.UseRLM = false
	directRunner := NewRunner(r.directExecutor, r.scorer)
	directReport, err := directRunner.Run(ctx, suite, directConfig)
	if err != nil {
		return nil, fmt.Errorf("direct run failed: %w", err)
	}
	report.Direct = directReport

	// Compute comparison
	report.Comparison = r.computeComparison(rlmReport, directReport)

	return report, nil
}

func (r *ComparisonRunner) computeComparison(rlm, direct *Report) ComparisonSummary {
	summary := ComparisonSummary{
		ByComplexity: make(map[TaskComplexity]ComplexityComparison),
	}

	// Overall improvements
	if direct.Summary.Accuracy > 0 {
		summary.AccuracyImprovement = (rlm.Summary.Accuracy - direct.Summary.Accuracy) / direct.Summary.Accuracy
	}
	if direct.Summary.MeanScore > 0 {
		summary.ScoreImprovement = (rlm.Summary.MeanScore - direct.Summary.MeanScore) / direct.Summary.MeanScore
	}
	if direct.Summary.TotalTokens > 0 {
		summary.TokenOverhead = float64(rlm.Summary.TotalTokens-direct.Summary.TotalTokens) / float64(direct.Summary.TotalTokens)
	}
	if rlm.Summary.MeanDuration > 0 {
		summary.SpeedupFactor = float64(direct.Summary.MeanDuration) / float64(rlm.Summary.MeanDuration)
	}

	// By complexity
	for complexity, rlmCS := range rlm.Summary.ByComplexity {
		directCS, ok := direct.Summary.ByComplexity[complexity]
		if !ok {
			continue
		}

		cc := ComplexityComparison{
			RLMAccuracy:    rlmCS.Accuracy,
			DirectAccuracy: directCS.Accuracy,
		}
		if directCS.Accuracy > 0 {
			cc.AccuracyImprovement = (rlmCS.Accuracy - directCS.Accuracy) / directCS.Accuracy
		}
		summary.ByComplexity[complexity] = cc
	}

	return summary
}
