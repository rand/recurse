// Package benchmark provides evaluation tools for RLM effectiveness.
//
// This package implements benchmarks inspired by OOLONG (Evaluating Long Context
// Reasoning and Aggregation Capabilities) to measure:
//   - Context rot: performance degradation vs context length
//   - Multi-hop reasoning across large document sets
//   - Partition+map efficiency
//   - Cost/accuracy tradeoffs
//   - RLM vs direct prompting comparisons
package benchmark

import (
	"context"
	"fmt"
	"time"
)

// TaskComplexity describes how task difficulty scales with context length.
type TaskComplexity string

const (
	// ComplexityConstant - task difficulty doesn't increase with context (e.g., needle-in-haystack).
	ComplexityConstant TaskComplexity = "constant"

	// ComplexityLinear - task difficulty scales linearly with context length.
	ComplexityLinear TaskComplexity = "linear"

	// ComplexityQuadratic - task difficulty scales quadratically (e.g., pairwise comparisons).
	ComplexityQuadratic TaskComplexity = "quadratic"
)

// Task represents a benchmark task to be evaluated.
type Task struct {
	// ID uniquely identifies this task instance.
	ID string

	// Name is the human-readable task name.
	Name string

	// Description explains what the task tests.
	Description string

	// Complexity describes how difficulty scales with context.
	Complexity TaskComplexity

	// Context is the input context for the task.
	Context string

	// ContextTokens is the approximate token count of the context.
	ContextTokens int

	// Query is the question or instruction to answer.
	Query string

	// ExpectedAnswer is the ground truth answer.
	ExpectedAnswer string

	// AnswerType indicates how to evaluate the answer.
	AnswerType AnswerType

	// Metadata contains task-specific information.
	Metadata map[string]any
}

// AnswerType specifies how to evaluate task answers.
type AnswerType string

const (
	// AnswerExact requires exact string match.
	AnswerExact AnswerType = "exact"

	// AnswerNumeric requires numeric equality within tolerance.
	AnswerNumeric AnswerType = "numeric"

	// AnswerF1 uses F1 score for set-based answers.
	AnswerF1 AnswerType = "f1"

	// AnswerContains checks if answer contains expected substring.
	AnswerContains AnswerType = "contains"

	// AnswerCustom uses a custom evaluation function.
	AnswerCustom AnswerType = "custom"
)

// Result captures the outcome of running a single task.
type Result struct {
	// TaskID identifies which task was run.
	TaskID string

	// Answer is the model's response.
	Answer string

	// Correct indicates if the answer matched expected.
	Correct bool

	// Score is a numeric score (0-1) for partial credit.
	Score float64

	// PromptTokens is the number of input tokens used.
	PromptTokens int

	// CompletionTokens is the number of output tokens generated.
	CompletionTokens int

	// TotalTokens is the sum of prompt and completion tokens.
	TotalTokens int

	// Duration is how long the task took to complete.
	Duration time.Duration

	// Iterations is the number of RLM iterations used (0 for direct).
	Iterations int

	// Error contains any error message if the task failed.
	Error string

	// Metadata contains additional result information.
	Metadata map[string]any
}

// RunConfig configures how benchmarks are executed.
type RunConfig struct {
	// UseRLM enables RLM mode (vs direct prompting).
	UseRLM bool

	// MaxIterations limits RLM iteration count.
	MaxIterations int

	// MaxTokensPerCall limits tokens per LLM call.
	MaxTokensPerCall int

	// Timeout is the maximum time per task.
	Timeout time.Duration

	// ModelTier specifies which model tier to use.
	ModelTier string

	// RecordTrace enables trace recording.
	RecordTrace bool

	// Parallel allows running tasks in parallel.
	Parallel int
}

// DefaultRunConfig returns sensible defaults for benchmark execution.
func DefaultRunConfig() RunConfig {
	return RunConfig{
		UseRLM:           true,
		MaxIterations:    10,
		MaxTokensPerCall: 4096,
		Timeout:          5 * time.Minute,
		ModelTier:        "balanced",
		RecordTrace:      true,
		Parallel:         1,
	}
}

// Suite is a collection of related benchmark tasks.
type Suite struct {
	// Name identifies this benchmark suite.
	Name string

	// Description explains what the suite tests.
	Description string

	// Tasks contains the individual benchmark tasks.
	Tasks []Task

	// Generator can create tasks dynamically (optional).
	Generator TaskGenerator
}

// TaskGenerator creates benchmark tasks dynamically.
type TaskGenerator interface {
	// Generate creates tasks for the given context length.
	Generate(contextTokens int, count int) ([]Task, error)
}

// Report summarizes benchmark results.
type Report struct {
	// SuiteName identifies which suite was run.
	SuiteName string

	// Config is the configuration used.
	Config RunConfig

	// Results contains individual task results.
	Results []Result

	// StartTime is when the benchmark started.
	StartTime time.Time

	// EndTime is when the benchmark completed.
	EndTime time.Time

	// TotalDuration is the total benchmark time.
	TotalDuration time.Duration

	// Summary contains aggregate metrics.
	Summary ReportSummary
}

// ReportSummary contains aggregate metrics from a benchmark run.
type ReportSummary struct {
	// TaskCount is the number of tasks run.
	TaskCount int

	// CorrectCount is the number of correct answers.
	CorrectCount int

	// Accuracy is CorrectCount / TaskCount.
	Accuracy float64

	// MeanScore is the average score across all tasks.
	MeanScore float64

	// MeanF1 is the average F1 score (for F1-type tasks).
	MeanF1 float64

	// TotalPromptTokens is the sum of all prompt tokens.
	TotalPromptTokens int

	// TotalCompletionTokens is the sum of all completion tokens.
	TotalCompletionTokens int

	// TotalTokens is the sum of all tokens.
	TotalTokens int

	// MeanIterations is the average RLM iterations per task.
	MeanIterations float64

	// MeanDuration is the average time per task.
	MeanDuration time.Duration

	// ErrorCount is the number of failed tasks.
	ErrorCount int

	// ByComplexity breaks down metrics by task complexity.
	ByComplexity map[TaskComplexity]ComplexitySummary

	// ByContextLength breaks down metrics by context length buckets.
	ByContextLength map[string]ContextLengthSummary
}

// ComplexitySummary contains metrics for a specific complexity level.
type ComplexitySummary struct {
	TaskCount    int
	Accuracy     float64
	MeanScore    float64
	MeanTokens   int
	MeanDuration time.Duration
}

// ContextLengthSummary contains metrics for a context length bucket.
type ContextLengthSummary struct {
	MinTokens    int
	MaxTokens    int
	TaskCount    int
	Accuracy     float64
	MeanScore    float64
	MeanTokens   int
	MeanDuration time.Duration
}

// Runner executes benchmark suites.
type Runner struct {
	executor Executor
	scorer   Scorer
}

// Executor runs individual tasks and returns results.
type Executor interface {
	// Execute runs a single task and returns the result.
	Execute(ctx context.Context, task Task, config RunConfig) (Result, error)
}

// Scorer evaluates answers against expected values.
type Scorer interface {
	// Score evaluates an answer and returns a score (0-1).
	Score(answer, expected string, answerType AnswerType) (float64, bool)
}

// NewRunner creates a new benchmark runner.
func NewRunner(executor Executor, scorer Scorer) *Runner {
	return &Runner{
		executor: executor,
		scorer:   scorer,
	}
}

// Run executes a benchmark suite and returns a report.
func (r *Runner) Run(ctx context.Context, suite Suite, config RunConfig) (*Report, error) {
	report := &Report{
		SuiteName: suite.Name,
		Config:    config,
		StartTime: time.Now(),
		Results:   make([]Result, 0, len(suite.Tasks)),
	}

	tasks := suite.Tasks

	// Generate additional tasks if generator is provided
	if suite.Generator != nil {
		// Generate tasks at various context lengths
		for _, tokens := range []int{4000, 16000, 64000, 128000} {
			generated, err := suite.Generator.Generate(tokens, 10)
			if err != nil {
				return nil, fmt.Errorf("generating tasks at %d tokens: %w", tokens, err)
			}
			tasks = append(tasks, generated...)
		}
	}

	// Execute tasks
	for _, task := range tasks {
		select {
		case <-ctx.Done():
			report.EndTime = time.Now()
			report.TotalDuration = report.EndTime.Sub(report.StartTime)
			return report, ctx.Err()
		default:
		}

		result, err := r.executor.Execute(ctx, task, config)
		if err != nil {
			result = Result{
				TaskID: task.ID,
				Error:  err.Error(),
			}
		} else {
			// Score the result
			score, correct := r.scorer.Score(result.Answer, task.ExpectedAnswer, task.AnswerType)
			result.Score = score
			result.Correct = correct
		}

		report.Results = append(report.Results, result)
	}

	report.EndTime = time.Now()
	report.TotalDuration = report.EndTime.Sub(report.StartTime)
	report.Summary = r.computeSummary(tasks, report.Results)

	return report, nil
}

// computeSummary calculates aggregate metrics from results.
func (r *Runner) computeSummary(tasks []Task, results []Result) ReportSummary {
	summary := ReportSummary{
		TaskCount:       len(results),
		ByComplexity:    make(map[TaskComplexity]ComplexitySummary),
		ByContextLength: make(map[string]ContextLengthSummary),
	}

	if len(results) == 0 {
		return summary
	}

	// Build task lookup
	taskMap := make(map[string]Task)
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	var totalScore float64
	var totalIterations int
	var totalDuration time.Duration

	// Complexity buckets
	complexityResults := make(map[TaskComplexity][]Result)

	// Context length buckets (in K tokens)
	contextBuckets := map[string]struct {
		min, max int
		results  []Result
	}{
		"0-4K":    {0, 4000, nil},
		"4-16K":   {4000, 16000, nil},
		"16-64K":  {16000, 64000, nil},
		"64-128K": {64000, 128000, nil},
		"128K+":   {128000, 10000000, nil},
	}

	for _, result := range results {
		if result.Error != "" {
			summary.ErrorCount++
			continue
		}

		if result.Correct {
			summary.CorrectCount++
		}
		totalScore += result.Score
		summary.TotalPromptTokens += result.PromptTokens
		summary.TotalCompletionTokens += result.CompletionTokens
		summary.TotalTokens += result.TotalTokens
		totalIterations += result.Iterations
		totalDuration += result.Duration

		// Group by complexity
		if task, ok := taskMap[result.TaskID]; ok {
			complexityResults[task.Complexity] = append(complexityResults[task.Complexity], result)

			// Group by context length
			for bucket, info := range contextBuckets {
				if task.ContextTokens >= info.min && task.ContextTokens < info.max {
					info.results = append(info.results, result)
					contextBuckets[bucket] = info
					break
				}
			}
		}
	}

	validCount := summary.TaskCount - summary.ErrorCount
	if validCount > 0 {
		summary.Accuracy = float64(summary.CorrectCount) / float64(validCount)
		summary.MeanScore = totalScore / float64(validCount)
		summary.MeanIterations = float64(totalIterations) / float64(validCount)
		summary.MeanDuration = totalDuration / time.Duration(validCount)
	}

	// Compute complexity summaries
	for complexity, results := range complexityResults {
		cs := ComplexitySummary{TaskCount: len(results)}
		var correct int
		var score float64
		var tokens int
		var duration time.Duration

		for _, r := range results {
			if r.Correct {
				correct++
			}
			score += r.Score
			tokens += r.TotalTokens
			duration += r.Duration
		}

		if cs.TaskCount > 0 {
			cs.Accuracy = float64(correct) / float64(cs.TaskCount)
			cs.MeanScore = score / float64(cs.TaskCount)
			cs.MeanTokens = tokens / cs.TaskCount
			cs.MeanDuration = duration / time.Duration(cs.TaskCount)
		}

		summary.ByComplexity[complexity] = cs
	}

	// Compute context length summaries
	for bucket, info := range contextBuckets {
		if len(info.results) == 0 {
			continue
		}

		cls := ContextLengthSummary{
			MinTokens: info.min,
			MaxTokens: info.max,
			TaskCount: len(info.results),
		}

		var correct int
		var score float64
		var tokens int
		var duration time.Duration

		for _, r := range info.results {
			if r.Correct {
				correct++
			}
			score += r.Score
			tokens += r.TotalTokens
			duration += r.Duration
		}

		cls.Accuracy = float64(correct) / float64(cls.TaskCount)
		cls.MeanScore = score / float64(cls.TaskCount)
		cls.MeanTokens = tokens / cls.TaskCount
		cls.MeanDuration = duration / time.Duration(cls.TaskCount)

		summary.ByContextLength[bucket] = cls
	}

	return summary
}
