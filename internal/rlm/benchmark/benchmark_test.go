package benchmark

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultScorer_ExactMatch(t *testing.T) {
	scorer := NewDefaultScorer()

	tests := []struct {
		name     string
		answer   string
		expected string
		wantScore float64
		wantCorrect bool
	}{
		{"exact match", "yes", "yes", 1.0, true},
		{"case insensitive", "YES", "yes", 1.0, true},
		{"mismatch", "no", "yes", 0.0, false},
		{"whitespace", "  yes  ", "yes", 1.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, correct := scorer.Score(tt.answer, tt.expected, AnswerExact)
			assert.Equal(t, tt.wantScore, score)
			assert.Equal(t, tt.wantCorrect, correct)
		})
	}
}

func TestDefaultScorer_NumericMatch(t *testing.T) {
	scorer := NewDefaultScorer()

	tests := []struct {
		name     string
		answer   string
		expected string
		wantCorrect bool
	}{
		{"exact number", "42", "42", true},
		{"with text", "The answer is 42", "42", true},
		{"with formatting", "$1,234", "1234", true},
		{"close enough", "100", "100", true}, // Exact match
		{"within tolerance", "1005", "1000", true}, // 0.5% difference, within 1%
		{"too far", "200", "100", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, correct := scorer.Score(tt.answer, tt.expected, AnswerNumeric)
			assert.Equal(t, tt.wantCorrect, correct)
		})
	}
}

func TestDefaultScorer_ContainsMatch(t *testing.T) {
	scorer := NewDefaultScorer()

	tests := []struct {
		name     string
		answer   string
		expected string
		wantCorrect bool
	}{
		{"contains", "The code is ABC123", "ABC123", true},
		{"case insensitive", "the code is abc123", "ABC123", true},
		{"not found", "The code is XYZ", "ABC123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, correct := scorer.Score(tt.answer, tt.expected, AnswerContains)
			assert.Equal(t, tt.wantCorrect, correct)
		})
	}
}

func TestDefaultScorer_F1Score(t *testing.T) {
	scorer := NewDefaultScorer()

	tests := []struct {
		name     string
		answer   string
		expected string
		wantScore float64
	}{
		{"perfect match", "apple, banana, cherry", "apple, banana, cherry", 1.0},
		{"partial match", "apple, banana", "apple, banana, cherry", 0.8}, // 2/2 precision, 2/3 recall
		{"no match", "date, elderberry", "apple, banana, cherry", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, _ := scorer.Score(tt.answer, tt.expected, AnswerF1)
			assert.InDelta(t, tt.wantScore, score, 0.01)
		})
	}
}

func TestCountingGenerator(t *testing.T) {
	gen := NewCountingGenerator(42)

	tasks, err := gen.Generate(4000, 5)
	require.NoError(t, err)
	assert.Len(t, tasks, 5)

	for _, task := range tasks {
		assert.Equal(t, ComplexityLinear, task.Complexity)
		assert.NotEmpty(t, task.Context)
		assert.NotEmpty(t, task.Query)
		assert.NotEmpty(t, task.ExpectedAnswer)
		assert.Equal(t, AnswerNumeric, task.AnswerType)
	}
}

func TestNeedleGenerator(t *testing.T) {
	gen := NewNeedleGenerator(42)

	tasks, err := gen.Generate(8000, 3)
	require.NoError(t, err)
	assert.Len(t, tasks, 3)

	for _, task := range tasks {
		assert.Equal(t, ComplexityConstant, task.Complexity)
		assert.Contains(t, task.Context, task.ExpectedAnswer)
		assert.Equal(t, AnswerContains, task.AnswerType)
	}
}

func TestPairingGenerator(t *testing.T) {
	gen := NewPairingGenerator(42)

	tasks, err := gen.Generate(16000, 5)
	require.NoError(t, err)
	assert.Len(t, tasks, 5)

	for _, task := range tasks {
		assert.Equal(t, ComplexityQuadratic, task.Complexity)
		assert.Contains(t, []string{"yes", "no"}, task.ExpectedAnswer)
		assert.Equal(t, AnswerExact, task.AnswerType)
	}
}

func TestAggregationGenerator(t *testing.T) {
	gen := NewAggregationGenerator(42)

	tasks, err := gen.Generate(8000, 3)
	require.NoError(t, err)
	assert.Len(t, tasks, 3)

	for _, task := range tasks {
		assert.Equal(t, ComplexityLinear, task.Complexity)
		assert.Equal(t, AnswerNumeric, task.AnswerType)

		// Verify total is sum of regional sales
		if sales, ok := task.Metadata["regional_sales"].(map[string]int); ok {
			total := 0
			for _, v := range sales {
				total += v
			}
			assert.Equal(t, task.Metadata["total_sales"], total)
		}
	}
}

func TestMockExecutor(t *testing.T) {
	executor := NewMockExecutor(map[string]string{
		"test-1": "42",
		"default": "unknown",
	})

	ctx := context.Background()
	config := DefaultRunConfig()

	// Test specific response
	task1 := Task{ID: "test-1", ExpectedAnswer: "42"}
	result1, err := executor.Execute(ctx, task1, config)
	require.NoError(t, err)
	assert.Equal(t, "42", result1.Answer)

	// Test default response
	task2 := Task{ID: "test-2", ExpectedAnswer: "hello"}
	result2, err := executor.Execute(ctx, task2, config)
	require.NoError(t, err)
	assert.Equal(t, "unknown", result2.Answer)
}

func TestRunner_BasicExecution(t *testing.T) {
	executor := NewMockExecutor(map[string]string{})
	scorer := NewDefaultScorer()
	runner := NewRunner(executor, scorer)

	suite := Suite{
		Name: "Test Suite",
		Tasks: []Task{
			{
				ID:             "task-1",
				Name:           "Simple Test",
				Complexity:     ComplexityConstant,
				Context:        "The answer is 42.",
				ContextTokens:  100,
				Query:          "What is the answer?",
				ExpectedAnswer: "42",
				AnswerType:     AnswerContains,
			},
			{
				ID:             "task-2",
				Name:           "Yes/No Test",
				Complexity:     ComplexityLinear,
				Context:        "The sky is blue.",
				ContextTokens:  50,
				Query:          "Is the sky blue?",
				ExpectedAnswer: "yes",
				AnswerType:     AnswerExact,
			},
		},
	}

	ctx := context.Background()
	config := DefaultRunConfig()

	report, err := runner.Run(ctx, suite, config)
	require.NoError(t, err)

	assert.Equal(t, "Test Suite", report.SuiteName)
	assert.Len(t, report.Results, 2)
	assert.Equal(t, 2, report.Summary.TaskCount)
}

func TestRunner_WithTimeout(t *testing.T) {
	// Create a slow executor
	executor := &slowExecutor{delay: 100 * time.Millisecond}
	scorer := NewDefaultScorer()
	runner := NewRunner(executor, scorer)

	suite := Suite{
		Name: "Timeout Test",
		Tasks: []Task{
			{ID: "task-1", ExpectedAnswer: "test"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	config := DefaultRunConfig()
	report, err := runner.Run(ctx, suite, config)

	// Should return partial results, not error
	assert.NoError(t, err)
	assert.NotNil(t, report)
}

type slowExecutor struct {
	delay time.Duration
}

func (e *slowExecutor) Execute(ctx context.Context, task Task, config RunConfig) (Result, error) {
	select {
	case <-time.After(e.delay):
		return Result{TaskID: task.ID, Answer: task.ExpectedAnswer}, nil
	case <-ctx.Done():
		return Result{TaskID: task.ID, Error: ctx.Err().Error()}, nil
	}
}

func TestContextRotAnalyzer(t *testing.T) {
	analyzer := NewContextRotAnalyzer()

	// Add results with decreasing scores as context increases
	analyzer.Add(4000, Result{Score: 0.95, Correct: true})
	analyzer.Add(4000, Result{Score: 0.90, Correct: true})
	analyzer.Add(16000, Result{Score: 0.80, Correct: true})
	analyzer.Add(16000, Result{Score: 0.75, Correct: true})
	analyzer.Add(64000, Result{Score: 0.60, Correct: false})
	analyzer.Add(64000, Result{Score: 0.55, Correct: false})

	report := analyzer.Analyze()

	// Should show negative degradation rate (performance decreasing)
	assert.Less(t, report.DegradationRate, 0.0)

	// Check buckets
	assert.Contains(t, report.ByContextLength, 4000)
	assert.Contains(t, report.ByContextLength, 16000)
	assert.Contains(t, report.ByContextLength, 64000)

	// 4K should have better accuracy than 64K
	assert.Greater(t, report.ByContextLength[4000].Accuracy, report.ByContextLength[64000].Accuracy)
}

func TestReportSummary_ByComplexity(t *testing.T) {
	executor := NewMockExecutor(map[string]string{})
	scorer := NewDefaultScorer()
	runner := NewRunner(executor, scorer)

	suite := Suite{
		Name: "Complexity Test",
		Tasks: []Task{
			{ID: "const-1", Complexity: ComplexityConstant, ExpectedAnswer: "a", AnswerType: AnswerExact, ContextTokens: 1000},
			{ID: "const-2", Complexity: ComplexityConstant, ExpectedAnswer: "b", AnswerType: AnswerExact, ContextTokens: 1000},
			{ID: "linear-1", Complexity: ComplexityLinear, ExpectedAnswer: "c", AnswerType: AnswerExact, ContextTokens: 2000},
			{ID: "quad-1", Complexity: ComplexityQuadratic, ExpectedAnswer: "d", AnswerType: AnswerExact, ContextTokens: 4000},
		},
	}

	ctx := context.Background()
	report, err := runner.Run(ctx, suite, DefaultRunConfig())
	require.NoError(t, err)

	// Should have entries for each complexity
	assert.Contains(t, report.Summary.ByComplexity, ComplexityConstant)
	assert.Contains(t, report.Summary.ByComplexity, ComplexityLinear)
	assert.Contains(t, report.Summary.ByComplexity, ComplexityQuadratic)

	assert.Equal(t, 2, report.Summary.ByComplexity[ComplexityConstant].TaskCount)
	assert.Equal(t, 1, report.Summary.ByComplexity[ComplexityLinear].TaskCount)
	assert.Equal(t, 1, report.Summary.ByComplexity[ComplexityQuadratic].TaskCount)
}

func TestExtractNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"42", 42},
		{"$1,234", 1234},
		{"The answer is 100.", 100},
		{"-50", -50},
		{"3.14159", 3.14159},
		{"no number here", 0}, // NaN case handled in scorer
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractNumber(tt.input)
			if tt.input == "no number here" {
				assert.True(t, result != result) // NaN check
			} else {
				assert.InDelta(t, tt.expected, result, 0.0001)
			}
		})
	}
}
