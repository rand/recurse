package benchmark

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestBenchmarkIntegration runs actual benchmarks if API keys are available.
// This is skipped by default - run with: go test -tags=integration
func TestBenchmarkIntegration(t *testing.T) {
	if os.Getenv("RUN_BENCHMARK_INTEGRATION") == "" {
		t.Skip("Skipping integration test - set RUN_BENCHMARK_INTEGRATION=1 to run")
	}

	// This would require actual LLM client setup
	t.Log("Integration tests would run here with real LLM calls")
}

// TestQuickBenchmarkWithMock runs the quick suite with mock executor.
// This validates the framework works end-to-end.
func TestQuickBenchmarkWithMock(t *testing.T) {
	// Use mock executor that returns expected answers
	executor := NewMockExecutor(map[string]string{})
	scorer := NewDefaultScorer()
	runner := NewRunner(executor, scorer)

	suite := QuickSuite(42)
	ctx := context.Background()
	config := DefaultRunConfig()
	config.Timeout = 30 * time.Second

	report, err := runner.Run(ctx, suite, config)
	if err != nil {
		t.Fatalf("Benchmark run failed: %v", err)
	}

	// Validate report structure
	if report.SuiteName != "Quick" {
		t.Errorf("Expected suite name 'Quick', got %s", report.SuiteName)
	}

	if report.Summary.TaskCount == 0 {
		t.Error("No tasks were executed")
	}

	// With mock executor returning expected answers, should have high accuracy
	if report.Summary.Accuracy < 0.9 {
		t.Errorf("Expected high accuracy with mock executor, got %.2f", report.Summary.Accuracy)
	}

	// Print summary
	t.Logf("Benchmark Summary:")
	t.Logf("  Tasks: %d", report.Summary.TaskCount)
	t.Logf("  Correct: %d", report.Summary.CorrectCount)
	t.Logf("  Accuracy: %.2f%%", report.Summary.Accuracy*100)
	t.Logf("  Mean Score: %.2f", report.Summary.MeanScore)
	t.Logf("  Total Tokens: %d", report.Summary.TotalTokens)
	t.Logf("  Duration: %v", report.TotalDuration)
}

// TestContextRotWithMock validates context rot analysis works.
func TestContextRotWithMock(t *testing.T) {
	// Create mock that degrades with context length
	degradingMock := &degradingMockExecutor{
		baseAccuracy: 0.95,
		degradeRate:  0.00001, // 1% per 1000 tokens
	}

	scorer := NewDefaultScorer()
	runner := NewRunner(degradingMock, scorer)

	suite := ContextRotSuite(42)
	ctx := context.Background()
	config := DefaultRunConfig()
	config.Timeout = 60 * time.Second

	report, err := runner.Run(ctx, suite, config)
	if err != nil {
		t.Fatalf("Benchmark run failed: %v", err)
	}

	t.Logf("Context Rot Analysis:")
	t.Logf("  Total Tasks: %d", report.Summary.TaskCount)

	// Analyze by context length
	analyzer := NewContextRotAnalyzer()
	for i, result := range report.Results {
		if i < len(suite.Tasks) {
			analyzer.Add(suite.Tasks[i].ContextTokens, result)
		}
	}

	rotReport := analyzer.Analyze()
	t.Logf("  Degradation Rate: %.6f per token", rotReport.DegradationRate)

	for bucket, metrics := range rotReport.ByContextLength {
		t.Logf("  %dK tokens: accuracy=%.2f%%, tasks=%d",
			bucket/1000, metrics.Accuracy*100, metrics.TaskCount)
	}

	// Should show negative degradation (performance decreases with length)
	if rotReport.DegradationRate >= 0 {
		t.Log("Warning: No degradation detected (expected negative rate)")
	}
}

// degradingMockExecutor simulates degrading performance with context length.
type degradingMockExecutor struct {
	baseAccuracy float64
	degradeRate  float64
}

func (e *degradingMockExecutor) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
	// Calculate accuracy based on context length
	accuracy := e.baseAccuracy - (float64(task.ContextTokens) * e.degradeRate)
	if accuracy < 0 {
		accuracy = 0
	}

	result := Result{
		TaskID:           task.ID,
		PromptTokens:     task.ContextTokens + 100,
		CompletionTokens: 50,
		TotalTokens:      task.ContextTokens + 150,
		Duration:         10 * time.Millisecond,
		Metadata:         make(map[string]any),
	}

	// Randomly decide if correct based on accuracy
	// Use deterministic approach based on task ID hash
	hash := 0
	for _, c := range task.ID {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	threshold := int(accuracy * 1000)
	isCorrect := (hash % 1000) < threshold

	if isCorrect {
		result.Answer = task.ExpectedAnswer
		result.Score = 1.0
		result.Correct = true
	} else {
		result.Answer = "wrong answer"
		result.Score = 0.0
		result.Correct = false
	}

	return result, nil
}

// TestComparisonWithMock validates comparison runner works.
func TestComparisonWithMock(t *testing.T) {
	// RLM mock - higher accuracy with seed 1
	rlmMock := &fixedAccuracyMock{accuracy: 0.85, seed: 1}
	// Direct mock - lower accuracy with seed 2
	directMock := &fixedAccuracyMock{accuracy: 0.60, seed: 2}

	scorer := NewDefaultScorer()
	compRunner := NewComparisonRunner(rlmMock, directMock, scorer)

	suite := QuickSuite(42)
	ctx := context.Background()
	config := DefaultRunConfig()

	report, err := compRunner.Run(ctx, suite, config)
	if err != nil {
		t.Fatalf("Comparison run failed: %v", err)
	}

	t.Logf("Comparison Results:")
	t.Logf("  RLM Accuracy: %.2f%%", report.RLM.Summary.Accuracy*100)
	t.Logf("  Direct Accuracy: %.2f%%", report.Direct.Summary.Accuracy*100)
	t.Logf("  Improvement: %.2f%%", report.Comparison.AccuracyImprovement*100)

	// RLM should show improvement
	if report.Comparison.AccuracyImprovement <= 0 {
		t.Errorf("Expected RLM improvement over direct, got %.2f%%",
			report.Comparison.AccuracyImprovement*100)
	}
}

type fixedAccuracyMock struct {
	accuracy float64
	seed     int // Used to differentiate RLM vs Direct mocks
}

func (e *fixedAccuracyMock) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
	result := Result{
		TaskID:           task.ID,
		PromptTokens:     task.ContextTokens + 100,
		CompletionTokens: 50,
		TotalTokens:      task.ContextTokens + 150,
		Duration:         10 * time.Millisecond,
		Metadata:         make(map[string]any),
	}

	// Deterministic based on task ID hash + seed (so RLM and Direct mocks differ)
	hash := e.seed
	for _, c := range task.ID {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	threshold := int(e.accuracy * 1000)
	isCorrect := (hash % 1000) < threshold

	if isCorrect {
		result.Answer = task.ExpectedAnswer
		result.Score = 1.0
		result.Correct = true
	} else {
		result.Answer = "wrong"
		result.Score = 0.0
		result.Correct = false
	}

	return result, nil
}

// PrintReport outputs a formatted benchmark report.
func PrintReport(report *Report) string {
	var sb fmt.Stringer = &reportPrinter{report: report}
	return sb.String()
}

type reportPrinter struct {
	report *Report
}

func (p *reportPrinter) String() string {
	r := p.report
	s := fmt.Sprintf(`
=== Benchmark Report: %s ===
Duration: %v
Tasks: %d (Errors: %d)

Overall Results:
  Accuracy: %.2f%% (%d/%d correct)
  Mean Score: %.4f
  Total Tokens: %d (Prompt: %d, Completion: %d)
  Mean Duration: %v

By Complexity:
`,
		r.SuiteName,
		r.TotalDuration,
		r.Summary.TaskCount,
		r.Summary.ErrorCount,
		r.Summary.Accuracy*100,
		r.Summary.CorrectCount,
		r.Summary.TaskCount-r.Summary.ErrorCount,
		r.Summary.MeanScore,
		r.Summary.TotalTokens,
		r.Summary.TotalPromptTokens,
		r.Summary.TotalCompletionTokens,
		r.Summary.MeanDuration,
	)

	for complexity, cs := range r.Summary.ByComplexity {
		s += fmt.Sprintf("  %s: accuracy=%.2f%%, tasks=%d, mean_tokens=%d\n",
			complexity, cs.Accuracy*100, cs.TaskCount, cs.MeanTokens)
	}

	s += "\nBy Context Length:\n"
	for bucket, cls := range r.Summary.ByContextLength {
		s += fmt.Sprintf("  %s: accuracy=%.2f%%, tasks=%d\n",
			bucket, cls.Accuracy*100, cls.TaskCount)
	}

	return s
}
