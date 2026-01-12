package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rand/recurse/internal/rlm/repl"
)

// TestRealBenchmark_Quick runs a quick benchmark with actual LLM calls.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_Quick ./internal/rlm/benchmark/
func TestRealBenchmark_Quick(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	// Create direct executor (simpler, no REPL needed)
	executor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	scorer := NewDefaultScorer()
	runner := NewRunner(executor, scorer)

	suite := QuickSuite(42)
	ctx := context.Background()
	config := RunConfig{
		UseRLM:           false, // Direct mode
		MaxTokensPerCall: 4096,
		Timeout:          2 * time.Minute,
		ModelTier:        "balanced",
	}

	t.Logf("Running quick benchmark suite with %d tasks...", len(suite.Tasks))
	start := time.Now()

	report, err := runner.Run(ctx, suite, config)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}

	t.Logf("Completed in %v", time.Since(start))
	printReport(t, report)
}

// TestRealBenchmark_RLMComparison compares RLM vs direct prompting.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMComparison ./internal/rlm/benchmark/
func TestRealBenchmark_RLMComparison(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	// For this test we use mock executors with different characteristics
	// to validate the comparison framework works before spending on real API calls
	rlmMock := &accuracyMockWithTokens{
		accuracy:     0.85,
		avgTokens:    2000,
		avgIterations: 3,
	}
	directMock := &accuracyMockWithTokens{
		accuracy:     0.65,
		avgTokens:    5000,
		avgIterations: 0,
	}

	scorer := NewDefaultScorer()
	compRunner := NewComparisonRunner(rlmMock, directMock, scorer)

	suite := QuickSuite(42)
	ctx := context.Background()
	config := DefaultRunConfig()

	t.Log("Running RLM vs Direct comparison...")
	report, err := compRunner.Run(ctx, suite, config)
	if err != nil {
		t.Fatalf("Comparison failed: %v", err)
	}

	printComparisonReport(t, report)

	// Validate that comparison metrics are computed correctly
	if report.Comparison.AccuracyImprovement <= 0 {
		t.Errorf("Expected positive accuracy improvement, got %.2f%%",
			report.Comparison.AccuracyImprovement*100)
	}
}

// TestRealBenchmark_ContextRot measures performance degradation with context length.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_ContextRot ./internal/rlm/benchmark/
func TestRealBenchmark_ContextRot(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	executor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	scorer := NewDefaultScorer()
	runner := NewRunner(executor, scorer)

	// Use a smaller context rot suite for testing
	suite := createSmallContextRotSuite(42)

	ctx := context.Background()
	config := RunConfig{
		UseRLM:           false,
		MaxTokensPerCall: 4096,
		Timeout:          5 * time.Minute,
		ModelTier:        "balanced",
	}

	t.Logf("Running context rot benchmark with %d tasks...", len(suite.Tasks))

	report, err := runner.Run(ctx, suite, config)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}

	// Analyze context rot
	analyzer := NewContextRotAnalyzer()
	for i, result := range report.Results {
		if i < len(suite.Tasks) {
			analyzer.Add(suite.Tasks[i].ContextTokens, result)
		}
	}

	rotReport := analyzer.Analyze()

	t.Log("=== Context Rot Analysis ===")
	t.Logf("Degradation Rate: %.8f per token", rotReport.DegradationRate)

	for bucket, metrics := range rotReport.ByContextLength {
		t.Logf("  %dK tokens: accuracy=%.2f%%, mean_score=%.2f, tasks=%d",
			bucket/1000, metrics.Accuracy*100, metrics.MeanScore, metrics.TaskCount)
	}
}

// TestRealBenchmark_SingleTask runs a single task for quick validation.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_SingleTask ./internal/rlm/benchmark/
func TestRealBenchmark_SingleTask(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	executor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	// Create a simple counting task
	gen := NewCountingGenerator(42)
	tasks, err := gen.Generate(2000, 1)
	if err != nil {
		t.Fatalf("Failed to generate task: %v", err)
	}

	task := tasks[0]
	t.Logf("Task: %s", task.Name)
	t.Logf("Query: %s", task.Query)
	t.Logf("Expected Answer: %s", task.ExpectedAnswer)
	t.Logf("Context Tokens: %d", task.ContextTokens)

	ctx := context.Background()
	config := RunConfig{
		MaxTokensPerCall: 2048,
		Timeout:          60 * time.Second,
	}

	result, err := executor.Execute(ctx, task, config)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	t.Logf("Answer: %s", result.Answer)
	t.Logf("Duration: %v", result.Duration)
	t.Logf("Tokens: %d", result.TotalTokens)

	// Score the result
	scorer := NewDefaultScorer()
	score, correct := scorer.Score(result.Answer, task.ExpectedAnswer, task.AnswerType)
	t.Logf("Score: %.2f, Correct: %v", score, correct)
}

// TestRealBenchmark_RLMMode runs a single task with full RLM mode (Python REPL + context externalization).
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMMode ./internal/rlm/benchmark/
func TestRealBenchmark_RLMMode(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	// Create RLM executor with Python REPL and extended timeout for LLM callbacks
	executor, err := NewRealRLMExecutor(RealExecutorConfig{
		OpenRouterAPIKey: apiKey,
		UseREPL:          true,
		REPLOptions:      repl.Options{},
		REPLTimeout:      5 * time.Minute, // Extended timeout for LLM callbacks
	})
	if err != nil {
		t.Fatalf("Failed to create RLM executor: %v", err)
	}

	ctx := context.Background()

	// Start the executor (starts REPL and service)
	if err := executor.Start(ctx); err != nil {
		t.Fatalf("Failed to start executor: %v", err)
	}
	defer executor.Stop()

	// Create a needle task (good for testing RLM's grep capability)
	gen := NewNeedleGenerator(42)
	tasks, err := gen.Generate(8000, 1) // 8K tokens
	if err != nil {
		t.Fatalf("Failed to generate task: %v", err)
	}

	task := tasks[0]
	t.Logf("=== RLM Mode Test ===")
	t.Logf("Task: %s", task.Name)
	t.Logf("Query: %s", task.Query)
	t.Logf("Expected Answer: %s", task.ExpectedAnswer)
	t.Logf("Context Tokens: %d", task.ContextTokens)

	config := RunConfig{
		UseRLM:           true,
		MaxIterations:    10,
		MaxTokensPerCall: 4096,
		Timeout:          3 * time.Minute,
		ModelTier:        "balanced",
	}

	result, err := executor.Execute(ctx, task, config)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	t.Logf("Answer: %s", result.Answer)
	t.Logf("Duration: %v", result.Duration)
	t.Logf("Iterations: %d", result.Iterations)
	t.Logf("Tokens: %d", result.TotalTokens)
	t.Logf("RLM Mode: %v", result.Metadata["rlm_mode"])

	// Score the result
	scorer := NewDefaultScorer()
	score, correct := scorer.Score(result.Answer, task.ExpectedAnswer, task.AnswerType)
	t.Logf("Score: %.2f, Correct: %v", score, correct)

	if result.Error != "" {
		t.Logf("Error: %s", result.Error)
	}
}

// TestRealBenchmark_RLMvsDirectComparison runs actual RLM vs Direct comparison with real LLM calls.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMvsDirectComparison ./internal/rlm/benchmark/
func TestRealBenchmark_RLMvsDirectComparison(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	ctx := context.Background()

	// Create RLM executor with extended timeout for LLM callbacks
	rlmExecutor, err := NewRealRLMExecutor(RealExecutorConfig{
		OpenRouterAPIKey: apiKey,
		UseREPL:          true,
		REPLOptions:      repl.Options{},
		REPLTimeout:      5 * time.Minute, // Extended timeout for LLM callbacks
	})
	if err != nil {
		t.Fatalf("Failed to create RLM executor: %v", err)
	}
	if err := rlmExecutor.Start(ctx); err != nil {
		t.Fatalf("Failed to start RLM executor: %v", err)
	}
	defer rlmExecutor.Stop()

	// Create Direct executor
	directExecutor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		t.Fatalf("Failed to create direct executor: %v", err)
	}

	scorer := NewDefaultScorer()

	// Create a small test suite targeting 8K context
	gen := NewNeedleGenerator(42)
	tasks8k, _ := gen.Generate(8000, 2)

	suite := Suite{
		Name:        "RLM-vs-Direct-8K",
		Description: "Comparison at 8K context length",
		Tasks:       tasks8k,
	}

	t.Logf("=== RLM vs Direct Comparison (8K context) ===")
	t.Logf("Tasks: %d", len(suite.Tasks))

	// Run with Direct mode
	t.Log("\n--- Direct Mode ---")
	directRunner := NewRunner(directExecutor, scorer)
	directConfig := RunConfig{
		UseRLM:           false,
		MaxTokensPerCall: 4096,
		Timeout:          2 * time.Minute,
	}
	directReport, err := directRunner.Run(ctx, suite, directConfig)
	if err != nil {
		t.Fatalf("Direct run failed: %v", err)
	}
	t.Logf("Direct Accuracy: %.2f%%", directReport.Summary.Accuracy*100)
	t.Logf("Direct Tokens: %d", directReport.Summary.TotalTokens)
	t.Logf("Direct Duration: %v", directReport.TotalDuration)

	// Run with RLM mode
	t.Log("\n--- RLM Mode ---")
	rlmRunner := NewRunner(rlmExecutor, scorer)
	rlmConfig := RunConfig{
		UseRLM:           true,
		MaxIterations:    10,
		MaxTokensPerCall: 4096,
		Timeout:          3 * time.Minute,
	}
	rlmReport, err := rlmRunner.Run(ctx, suite, rlmConfig)
	if err != nil {
		t.Fatalf("RLM run failed: %v", err)
	}
	t.Logf("RLM Accuracy: %.2f%%", rlmReport.Summary.Accuracy*100)
	t.Logf("RLM Tokens: %d", rlmReport.Summary.TotalTokens)
	t.Logf("RLM Duration: %v", rlmReport.TotalDuration)
	t.Logf("RLM Mean Iterations: %.1f", float64(rlmReport.Summary.TotalTokens)/float64(rlmReport.Summary.TaskCount))

	// Comparison
	t.Log("\n--- Comparison ---")
	accuracyDiff := rlmReport.Summary.Accuracy - directReport.Summary.Accuracy
	t.Logf("Accuracy Improvement: %.2f%% (RLM: %.2f%% vs Direct: %.2f%%)",
		accuracyDiff*100, rlmReport.Summary.Accuracy*100, directReport.Summary.Accuracy*100)

	if rlmReport.Summary.Accuracy > directReport.Summary.Accuracy {
		t.Log("RLM outperformed Direct prompting!")
	} else if rlmReport.Summary.Accuracy == directReport.Summary.Accuracy {
		t.Log("RLM and Direct performed equally")
	} else {
		t.Log("Direct outperformed RLM (unexpected)")
	}
}

// TestRealBenchmark_RLM_LargeContext tests RLM at 32K and 64K context lengths.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLM_LargeContext ./internal/rlm/benchmark/
func TestRealBenchmark_RLM_LargeContext(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	ctx := context.Background()

	// Create RLM executor with extended timeout for LLM callbacks
	// Large context operations can take several minutes per iteration
	rlmExecutor, err := NewRealRLMExecutor(RealExecutorConfig{
		OpenRouterAPIKey: apiKey,
		UseREPL:          true,
		REPLOptions:      repl.Options{},
		REPLTimeout:      10 * time.Minute, // Long timeout for LLM callbacks
	})
	if err != nil {
		t.Fatalf("Failed to create RLM executor: %v", err)
	}
	if err := rlmExecutor.Start(ctx); err != nil {
		t.Fatalf("Failed to start RLM executor: %v", err)
	}
	defer rlmExecutor.Stop()

	// Create Direct executor
	directExecutor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		t.Fatalf("Failed to create direct executor: %v", err)
	}

	scorer := NewDefaultScorer()

	// Test at different context lengths
	contextLengths := []int{32000, 64000}

	for _, contextTokens := range contextLengths {
		t.Run(fmt.Sprintf("%dK", contextTokens/1000), func(t *testing.T) {
			t.Logf("=== Testing at %dK context ===", contextTokens/1000)

			// Generate needle task at this context length
			gen := NewNeedleGenerator(42)
			tasks, err := gen.Generate(contextTokens, 1)
			if err != nil {
				t.Fatalf("Failed to generate task: %v", err)
			}
			task := tasks[0]

			t.Logf("Task: %s", task.Name)
			t.Logf("Expected: %s", task.ExpectedAnswer)
			t.Logf("Context Tokens: %d", task.ContextTokens)

			// Test Direct mode
			t.Log("\n--- Direct Mode ---")
			directConfig := RunConfig{
				UseRLM:           false,
				MaxTokensPerCall: 4096,
				Timeout:          2 * time.Minute,
			}
			directResult, err := directExecutor.Execute(ctx, task, directConfig)
			if err != nil {
				t.Logf("Direct execution error: %v", err)
			}
			directScore, directCorrect := scorer.Score(directResult.Answer, task.ExpectedAnswer, task.AnswerType)
			t.Logf("Direct Answer: %s", truncateString(directResult.Answer, 100))
			t.Logf("Direct Score: %.2f, Correct: %v", directScore, directCorrect)
			t.Logf("Direct Duration: %v", directResult.Duration)

			// Test RLM mode
			t.Log("\n--- RLM Mode ---")
			rlmConfig := RunConfig{
				UseRLM:           true,
				MaxIterations:    15, // More iterations for larger context
				MaxTokensPerCall: 4096,
				Timeout:          5 * time.Minute,
			}
			rlmResult, err := rlmExecutor.Execute(ctx, task, rlmConfig)
			if err != nil {
				t.Logf("RLM execution error: %v", err)
			}
			rlmScore, rlmCorrect := scorer.Score(rlmResult.Answer, task.ExpectedAnswer, task.AnswerType)
			t.Logf("RLM Answer: %s", truncateString(rlmResult.Answer, 100))
			t.Logf("RLM Score: %.2f, Correct: %v", rlmScore, rlmCorrect)
			t.Logf("RLM Iterations: %d", rlmResult.Iterations)
			t.Logf("RLM Duration: %v", rlmResult.Duration)

			if rlmResult.Error != "" {
				t.Logf("RLM Error: %s", rlmResult.Error)
			}

			// Summary
			t.Log("\n--- Summary ---")
			t.Logf("Direct: %v (%.2f)", directCorrect, directScore)
			t.Logf("RLM: %v (%.2f)", rlmCorrect, rlmScore)

			if rlmCorrect && !directCorrect {
				t.Logf("✓ RLM succeeded where Direct failed!")
			} else if rlmCorrect && directCorrect {
				t.Logf("Both succeeded")
			} else if !rlmCorrect && !directCorrect {
				t.Logf("Both failed")
			} else {
				t.Logf("Direct succeeded, RLM failed (unexpected)")
			}
		})
	}
}

// truncateString truncates a string to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TestRealBenchmark_RLMvsDirectEvaluation runs a comprehensive evaluation of RLM vs Direct
// across different task types to identify when RLM provides advantages.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_RLMvsDirectEvaluation ./internal/rlm/benchmark/
func TestRealBenchmark_RLMvsDirectEvaluation(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	ctx := context.Background()

	// Create RLM executor with extended timeout
	rlmExecutor, err := NewRealRLMExecutor(RealExecutorConfig{
		OpenRouterAPIKey: apiKey,
		UseREPL:          true,
		REPLOptions:      repl.Options{},
		REPLTimeout:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Failed to create RLM executor: %v", err)
	}
	if err := rlmExecutor.Start(ctx); err != nil {
		t.Fatalf("Failed to start RLM executor: %v", err)
	}
	defer rlmExecutor.Stop()

	// Create Direct executor
	directExecutor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		t.Fatalf("Failed to create direct executor: %v", err)
	}

	scorer := NewDefaultScorer()

	// Define task types to evaluate
	type taskTypeConfig struct {
		name      string
		generator func(int64) TaskGenerator
		contexts  []int // Context lengths to test
	}

	taskTypes := []taskTypeConfig{
		{
			name:      "Needle",
			generator: func(seed int64) TaskGenerator { return NewNeedleGenerator(seed) },
			contexts:  []int{8000, 32000},
		},
		{
			name:      "Counting",
			generator: func(seed int64) TaskGenerator { return NewCountingGenerator(seed) },
			contexts:  []int{8000, 32000},
		},
		{
			name:      "Aggregation",
			generator: func(seed int64) TaskGenerator { return NewAggregationGenerator(seed) },
			contexts:  []int{8000, 32000},
		},
	}

	// Results storage
	type result struct {
		TaskType      string
		ContextTokens int
		DirectCorrect bool
		DirectScore   float64
		DirectTokens  int
		DirectTime    time.Duration
		RLMCorrect    bool
		RLMScore      float64
		RLMTokens     int
		RLMTime       time.Duration
		RLMIters      int
		Winner        string
	}

	var results []result

	for _, tt := range taskTypes {
		for _, contextTokens := range tt.contexts {
			t.Run(fmt.Sprintf("%s/%dK", tt.name, contextTokens/1000), func(t *testing.T) {
				gen := tt.generator(42)
				tasks, err := gen.Generate(contextTokens, 1)
				if err != nil {
					t.Fatalf("Failed to generate task: %v", err)
				}
				task := tasks[0]

				t.Logf("=== %s at %dK ===", tt.name, contextTokens/1000)
				t.Logf("Query: %s", task.Query)
				t.Logf("Expected: %s", task.ExpectedAnswer)

				// Direct mode
				directConfig := RunConfig{
					UseRLM:           false,
					MaxTokensPerCall: 4096,
					Timeout:          3 * time.Minute,
				}
				directResult, _ := directExecutor.Execute(ctx, task, directConfig)
				directScore, directCorrect := scorer.Score(directResult.Answer, task.ExpectedAnswer, task.AnswerType)

				t.Logf("Direct: correct=%v score=%.2f tokens=%d time=%v",
					directCorrect, directScore, directResult.TotalTokens, directResult.Duration)
				t.Logf("Direct Answer: %s", truncateString(directResult.Answer, 80))

				// RLM mode
				rlmConfig := RunConfig{
					UseRLM:           true,
					MaxIterations:    15,
					MaxTokensPerCall: 4096,
					Timeout:          5 * time.Minute,
				}
				rlmResult, _ := rlmExecutor.Execute(ctx, task, rlmConfig)
				rlmScore, rlmCorrect := scorer.Score(rlmResult.Answer, task.ExpectedAnswer, task.AnswerType)

				t.Logf("RLM: correct=%v score=%.2f tokens=%d iters=%d time=%v",
					rlmCorrect, rlmScore, rlmResult.TotalTokens, rlmResult.Iterations, rlmResult.Duration)
				t.Logf("RLM Answer: %s", truncateString(rlmResult.Answer, 80))

				if rlmResult.Error != "" {
					t.Logf("RLM Error: %s", rlmResult.Error)
				}

				// Determine winner
				winner := "Tie"
				if rlmCorrect && !directCorrect {
					winner = "RLM"
				} else if directCorrect && !rlmCorrect {
					winner = "Direct"
				} else if rlmCorrect && directCorrect {
					// Both correct - compare efficiency
					if rlmResult.TotalTokens < directResult.TotalTokens {
						winner = "RLM (fewer tokens)"
					} else if directResult.Duration < rlmResult.Duration {
						winner = "Direct (faster)"
					}
				}

				t.Logf("Winner: %s", winner)

				results = append(results, result{
					TaskType:      tt.name,
					ContextTokens: contextTokens,
					DirectCorrect: directCorrect,
					DirectScore:   directScore,
					DirectTokens:  directResult.TotalTokens,
					DirectTime:    directResult.Duration,
					RLMCorrect:    rlmCorrect,
					RLMScore:      rlmScore,
					RLMTokens:     rlmResult.TotalTokens,
					RLMTime:       rlmResult.Duration,
					RLMIters:      rlmResult.Iterations,
					Winner:        winner,
				})
			})
		}
	}

	// Print summary
	t.Log("\n========== EVALUATION SUMMARY ==========")
	t.Log("| Task Type | Context | Direct | RLM | Winner |")
	t.Log("|-----------|---------|--------|-----|--------|")
	for _, r := range results {
		directStatus := "✗"
		if r.DirectCorrect {
			directStatus = "✓"
		}
		rlmStatus := "✗"
		if r.RLMCorrect {
			rlmStatus = "✓"
		}
		t.Logf("| %s | %dK | %s (%.0f) | %s (%.0f) | %s |",
			r.TaskType, r.ContextTokens/1000, directStatus, r.DirectScore*100,
			rlmStatus, r.RLMScore*100, r.Winner)
	}

	// Analysis
	t.Log("\n========== ANALYSIS ==========")
	rlmWins := 0
	directWins := 0
	ties := 0
	for _, r := range results {
		if strings.HasPrefix(r.Winner, "RLM") {
			rlmWins++
		} else if strings.HasPrefix(r.Winner, "Direct") {
			directWins++
		} else {
			ties++
		}
	}
	t.Logf("RLM Wins: %d, Direct Wins: %d, Ties: %d", rlmWins, directWins, ties)
}

// createSmallContextRotSuite creates a smaller context rot suite for faster testing.
func createSmallContextRotSuite(seed int64) Suite {
	gen := NewNeedleGenerator(seed)

	var tasks []Task
	// Only test a few context lengths
	for _, tokens := range []int{2000, 8000, 32000} {
		generated, _ := gen.Generate(tokens, 2)
		tasks = append(tasks, generated...)
	}

	return Suite{
		Name:        "Context-Rot-Small",
		Description: "Small context rot suite for testing",
		Tasks:       tasks,
	}
}

// accuracyMockWithTokens is a mock that simulates realistic token usage.
type accuracyMockWithTokens struct {
	accuracy      float64
	avgTokens     int
	avgIterations int
	callNum       int
}

func (e *accuracyMockWithTokens) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
	e.callNum++

	result := Result{
		TaskID:           task.ID,
		PromptTokens:     task.ContextTokens + 100,
		CompletionTokens: e.avgTokens / 4,
		TotalTokens:      e.avgTokens,
		Duration:         50 * time.Millisecond,
		Iterations:       e.avgIterations,
		Metadata:         make(map[string]any),
	}

	// Deterministic correctness based on call number
	isCorrect := float64(e.callNum%100)/100.0 < e.accuracy

	if isCorrect {
		result.Answer = task.ExpectedAnswer
		result.Score = 1.0
		result.Correct = true
	} else {
		result.Answer = "incorrect"
		result.Score = 0.0
		result.Correct = false
	}

	return result, nil
}

func printReport(t *testing.T, report *Report) {
	t.Log("")
	t.Logf("=== Benchmark Report: %s ===", report.SuiteName)
	t.Logf("Duration: %v", report.TotalDuration)
	t.Logf("Tasks: %d (Errors: %d)", report.Summary.TaskCount, report.Summary.ErrorCount)
	t.Log("")
	t.Log("Overall Results:")
	t.Logf("  Accuracy: %.2f%% (%d/%d correct)",
		report.Summary.Accuracy*100,
		report.Summary.CorrectCount,
		report.Summary.TaskCount-report.Summary.ErrorCount)
	t.Logf("  Mean Score: %.4f", report.Summary.MeanScore)
	t.Logf("  Total Tokens: %d", report.Summary.TotalTokens)
	t.Logf("  Mean Duration: %v", report.Summary.MeanDuration)
	t.Log("")

	if len(report.Summary.ByComplexity) > 0 {
		t.Log("By Complexity:")
		for complexity, cs := range report.Summary.ByComplexity {
			t.Logf("  %s: accuracy=%.2f%%, tasks=%d, mean_tokens=%d",
				complexity, cs.Accuracy*100, cs.TaskCount, cs.MeanTokens)
		}
	}

	// Output JSON for further analysis
	jsonBytes, _ := json.MarshalIndent(report.Summary, "", "  ")
	t.Logf("\nJSON Summary:\n%s", string(jsonBytes))
}

func printComparisonReport(t *testing.T, report *ComparisonReport) {
	t.Log("")
	t.Logf("=== Comparison Report: %s ===", report.SuiteName)
	t.Log("")
	t.Log("RLM Results:")
	t.Logf("  Accuracy: %.2f%%", report.RLM.Summary.Accuracy*100)
	t.Logf("  Mean Score: %.4f", report.RLM.Summary.MeanScore)
	t.Logf("  Total Tokens: %d", report.RLM.Summary.TotalTokens)
	t.Logf("  Mean Duration: %v", report.RLM.Summary.MeanDuration)
	t.Log("")
	t.Log("Direct Results:")
	t.Logf("  Accuracy: %.2f%%", report.Direct.Summary.Accuracy*100)
	t.Logf("  Mean Score: %.4f", report.Direct.Summary.MeanScore)
	t.Logf("  Total Tokens: %d", report.Direct.Summary.TotalTokens)
	t.Logf("  Mean Duration: %v", report.Direct.Summary.MeanDuration)
	t.Log("")
	t.Log("Comparison:")
	t.Logf("  Accuracy Improvement: %.2f%%", report.Comparison.AccuracyImprovement*100)
	t.Logf("  Score Improvement: %.2f%%", report.Comparison.ScoreImprovement*100)
	t.Logf("  Token Overhead: %.2f%%", report.Comparison.TokenOverhead*100)
	t.Logf("  Speedup Factor: %.2fx", report.Comparison.SpeedupFactor)
	t.Log("")

	if len(report.Comparison.ByComplexity) > 0 {
		t.Log("By Complexity:")
		for complexity, cc := range report.Comparison.ByComplexity {
			t.Logf("  %s: RLM=%.2f%% Direct=%.2f%% Improvement=%.2f%%",
				complexity, cc.RLMAccuracy*100, cc.DirectAccuracy*100, cc.AccuracyImprovement*100)
		}
	}
}

// BenchmarkResult contains the results of a benchmark run for reporting.
type BenchmarkResult struct {
	SuiteName           string             `json:"suite_name"`
	Timestamp           time.Time          `json:"timestamp"`
	Duration            time.Duration      `json:"duration"`
	TaskCount           int                `json:"task_count"`
	Accuracy            float64            `json:"accuracy"`
	MeanScore           float64            `json:"mean_score"`
	TotalTokens         int                `json:"total_tokens"`
	MeanDuration        time.Duration      `json:"mean_duration"`
	ByComplexity        map[string]float64 `json:"by_complexity"`
	ContextRotRate      float64            `json:"context_rot_rate,omitempty"`
	RLMVsDirect         *ComparisonResult  `json:"rlm_vs_direct,omitempty"`
}

// ComparisonResult summarizes RLM vs Direct comparison.
type ComparisonResult struct {
	RLMAccuracy         float64 `json:"rlm_accuracy"`
	DirectAccuracy      float64 `json:"direct_accuracy"`
	AccuracyImprovement float64 `json:"accuracy_improvement"`
	TokenOverhead       float64 `json:"token_overhead"`
}

// TestRealBenchmark_DiagnosticOutputs runs tasks and logs full model outputs for debugging.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_DiagnosticOutputs ./internal/rlm/benchmark/
func TestRealBenchmark_DiagnosticOutputs(t *testing.T) {
	if os.Getenv("RUN_REAL_BENCHMARK") == "" {
		t.Skip("Skipping real benchmark - set RUN_REAL_BENCHMARK=1 to run")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY not set")
	}

	executor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		t.Fatalf("Failed to create executor: %v", err)
	}

	scorer := NewDefaultScorer()

	// Test needle tasks at different context lengths
	contextLengths := []int{4000, 8000, 16000}

	for _, contextTokens := range contextLengths {
		t.Run(fmt.Sprintf("%dK", contextTokens/1000), func(t *testing.T) {
			gen := NewNeedleGenerator(42)
			tasks, err := gen.Generate(contextTokens, 1)
			if err != nil {
				t.Fatalf("Failed to generate task: %v", err)
			}
			task := tasks[0]

			t.Logf("=== Diagnostic at %dK ===", contextTokens/1000)
			t.Logf("Query: %s", task.Query)
			t.Logf("Expected: %s", task.ExpectedAnswer)
			t.Logf("AnswerType: %s", task.AnswerType)
			t.Logf("Needle Position: %.2f%%", task.Metadata["needle_position"].(float32)*100)

			ctx := context.Background()
			config := RunConfig{
				MaxTokensPerCall: 4096,
				Timeout:          2 * time.Minute,
			}

			result, err := executor.Execute(ctx, task, config)
			if err != nil {
				t.Logf("Execution error: %v", err)
				return
			}

			// Log the FULL answer, not truncated
			t.Logf("\n--- FULL MODEL RESPONSE ---")
			t.Logf("%s", result.Answer)
			t.Logf("--- END RESPONSE ---\n")

			// Score it
			score, correct := scorer.Score(result.Answer, task.ExpectedAnswer, task.AnswerType)
			t.Logf("Score: %.2f, Correct: %v", score, correct)
			t.Logf("Duration: %v, Tokens: %d", result.Duration, result.TotalTokens)

			// Debug: check if answer contains expected manually
			answerLower := strings.ToLower(result.Answer)
			expectedLower := strings.ToLower(task.ExpectedAnswer)
			t.Logf("Manual check: answer contains expected? %v", strings.Contains(answerLower, expectedLower))
			t.Logf("Manual check: expected contains answer? %v", strings.Contains(expectedLower, answerLower))
		})
	}
}

// TestRealBenchmark_PartialMatchAnalysis investigates partial match cases.
// Run with: RUN_REAL_BENCHMARK=1 go test -v -run TestRealBenchmark_PartialMatchAnalysis ./internal/rlm/benchmark/
func TestRealBenchmark_PartialMatchAnalysis(t *testing.T) {
	// This test analyzes scoring behavior without API calls
	scorer := NewDefaultScorer()

	testCases := []struct {
		name     string
		answer   string
		expected string
		wantOK   bool
	}{
		{"exact match", "CODE-2305", "CODE-2305", true},
		{"answer contains expected", "The code is CODE-2305.", "CODE-2305", true},
		{"partial - number only", "2305", "CODE-2305", false},
		{"partial - with prefix", "The answer is 2305", "CODE-2305", false},
		{"case insensitive", "code-2305", "CODE-2305", true},
		{"extra context", "Based on my analysis, the secret access code is CODE-2305 as mentioned in the document.", "CODE-2305", true},
		{"hallucinated different code", "The code is CODE-1234", "CODE-2305", false},
		{"no code found", "I could not find any secret code in the document.", "CODE-2305", false},
	}

	t.Log("=== AnswerContains Scoring Analysis ===")
	for _, tc := range testCases {
		score, correct := scorer.Score(tc.answer, tc.expected, AnswerContains)
		status := "✓"
		if correct != tc.wantOK {
			status = "✗ UNEXPECTED"
		}
		t.Logf("%s %s: answer=%q expected=%q => score=%.2f correct=%v",
			status, tc.name, truncateString(tc.answer, 50), tc.expected, score, correct)
	}
}

// RunBenchmarkSuite runs a complete benchmark and returns structured results.
func RunBenchmarkSuite(ctx context.Context, apiKey string, suiteName string, seed int64) (*BenchmarkResult, error) {
	var suite Suite
	switch suiteName {
	case "quick":
		suite = QuickSuite(seed)
	case "context-rot":
		suite = ContextRotSuite(seed)
	case "aggregation":
		suite = AggregationSuite(seed)
	case "pairing":
		suite = PairingSuite(seed)
	case "full":
		suite = FullSuite(seed)
	default:
		return nil, fmt.Errorf("unknown suite: %s", suiteName)
	}

	executor, err := NewRealDirectExecutor(apiKey)
	if err != nil {
		return nil, err
	}

	scorer := NewDefaultScorer()
	runner := NewRunner(executor, scorer)

	config := RunConfig{
		MaxTokensPerCall: 4096,
		Timeout:          5 * time.Minute,
		ModelTier:        "balanced",
	}

	report, err := runner.Run(ctx, suite, config)
	if err != nil {
		return nil, err
	}

	result := &BenchmarkResult{
		SuiteName:    report.SuiteName,
		Timestamp:    time.Now(),
		Duration:     report.TotalDuration,
		TaskCount:    report.Summary.TaskCount,
		Accuracy:     report.Summary.Accuracy,
		MeanScore:    report.Summary.MeanScore,
		TotalTokens:  report.Summary.TotalTokens,
		MeanDuration: report.Summary.MeanDuration,
		ByComplexity: make(map[string]float64),
	}

	for complexity, cs := range report.Summary.ByComplexity {
		result.ByComplexity[string(complexity)] = cs.Accuracy
	}

	return result, nil
}
