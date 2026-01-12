package rlm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTerminationTracker_FinalCalled(t *testing.T) {
	tracker := NewTerminationTracker(TaskTypeComputational)

	result := &IterationResult{
		HasFinal:    true,
		FinalOutput: "42",
		Iteration:   1,
	}

	check := tracker.CheckTermination(result)

	assert.True(t, check.ShouldTerminate)
	assert.Equal(t, "FINAL() called", check.Reason)
	assert.Equal(t, 1.0, check.Confidence)
}

func TestTerminationTracker_AnswerStability(t *testing.T) {
	tracker := NewTerminationTracker(TaskTypeComputational)

	// First iteration - should not terminate
	result1 := &IterationResult{
		HasFinal:   false,
		REPLOutput: "42",
		Iteration:  1,
	}
	check1 := tracker.CheckTermination(result1)
	assert.False(t, check1.ShouldTerminate)

	// Second iteration with same output - should terminate
	result2 := &IterationResult{
		HasFinal:   false,
		REPLOutput: "42",
		Iteration:  2,
	}
	check2 := tracker.CheckTermination(result2)
	assert.True(t, check2.ShouldTerminate)
	assert.Equal(t, "answer stabilized across iterations", check2.Reason)
}

func TestTerminationTracker_AnswerChanging(t *testing.T) {
	tracker := NewTerminationTracker(TaskTypeComputational)

	// First iteration
	result1 := &IterationResult{
		HasFinal:   false,
		REPLOutput: "41",
		Iteration:  1,
	}
	check1 := tracker.CheckTermination(result1)
	assert.False(t, check1.ShouldTerminate)

	// Second iteration with different output - should NOT terminate
	result2 := &IterationResult{
		HasFinal:   false,
		REPLOutput: "42",
		Iteration:  2,
	}
	check2 := tracker.CheckTermination(result2)
	assert.False(t, check2.ShouldTerminate)
}

func TestTerminationTracker_SimpleComputation(t *testing.T) {
	tracker := NewTerminationTracker(TaskTypeComputational)

	result := &IterationResult{
		HasFinal:     false,
		CodeExecuted: "import re\ncount = len(re.findall(r'apple', text))\nprint(count)",
		REPLOutput:   "42",
		Iteration:    1,
	}

	check := tracker.CheckTermination(result)

	assert.True(t, check.ShouldTerminate)
	assert.Equal(t, "simple computation completed with clear output", check.Reason)
	assert.Equal(t, 0.9, check.Confidence)
}

func TestTerminationTracker_SimpleComputation_NonComputationalTask(t *testing.T) {
	// Simple computation detection should only work for computational tasks
	tracker := NewTerminationTracker(TaskTypeAnalytical)

	result := &IterationResult{
		HasFinal:     false,
		CodeExecuted: "import re\ncount = len(re.findall(r'apple', text))\nprint(count)",
		REPLOutput:   "42",
		Iteration:    1,
	}

	check := tracker.CheckTermination(result)

	// Should NOT terminate because task type is analytical
	assert.False(t, check.ShouldTerminate)
}

func TestTerminationTracker_ErrorLoop(t *testing.T) {
	tracker := NewTerminationTracker(TaskTypeComputational)

	// Same error 3 times should trigger termination
	errorResult := &IterationResult{
		HasFinal:   false,
		REPLError:  "NameError: name 'undefined_var' is not defined",
		Iteration:  1,
	}

	check1 := tracker.CheckTermination(errorResult)
	assert.False(t, check1.ShouldTerminate)

	errorResult.Iteration = 2
	check2 := tracker.CheckTermination(errorResult)
	assert.False(t, check2.ShouldTerminate)

	errorResult.Iteration = 3
	check3 := tracker.CheckTermination(errorResult)
	assert.True(t, check3.ShouldTerminate)
	assert.Equal(t, "stuck in error loop", check3.Reason)
}

func TestTerminationTracker_EmptyOutput(t *testing.T) {
	tracker := NewTerminationTracker(TaskTypeComputational)

	result := &IterationResult{
		HasFinal:   false,
		REPLOutput: "",
		Iteration:  1,
	}

	check := tracker.CheckTermination(result)
	assert.False(t, check.ShouldTerminate)
}

func TestIsSimpleComputation(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected bool
	}{
		{
			name:     "simple count with regex",
			code:     "import re\ncount = len(re.findall(r'word', text))",
			expected: true,
		},
		{
			name:     "simple sum",
			code:     "total = sum(numbers)",
			expected: true,
		},
		{
			name:     "FINAL call",
			code:     "FINAL('42')",
			expected: true,
		},
		{
			name:     "string count",
			code:     "text.count('word')",
			expected: true,
		},
		{
			name:     "complex multi-line code",
			code:     "for i in range(10):\n    if condition:\n        do_something()\n    else:\n        do_other()\nresult = process()",
			expected: false,
		},
		{
			name:     "llm_call (not simple)",
			code:     "result = llm_call('analyze', text, 'fast')",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSimpleComputation(tt.code)
			assert.Equal(t, tt.expected, result, "isSimpleComputation(%q)", tt.code)
		})
	}
}

func TestLooksLikeSimpleAnswer(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "integer",
			output:   "42",
			expected: true,
		},
		{
			name:     "float",
			output:   "3.14159",
			expected: true,
		},
		{
			name:     "negative number",
			output:   "-123",
			expected: true,
		},
		{
			name:     "short phrase",
			output:   "yes",
			expected: true,
		},
		{
			name:     "few words",
			output:   "The answer is 42",
			expected: true,
		},
		{
			name:     "empty",
			output:   "",
			expected: false,
		},
		{
			name:     "very long output",
			output:   "This is a very long output that goes on and on and contains more than one hundred characters which is too long to be considered a simple answer.",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeSimpleAnswer(tt.output)
			assert.Equal(t, tt.expected, result, "looksLikeSimpleAnswer(%q)", tt.output)
		})
	}
}

func TestNormalizeAnswer(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "trims whitespace",
			input:    "  42  \n",
			expected: "42",
		},
		{
			name:     "normalizes newlines",
			input:    "line1\r\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "truncates long output",
			input:    string(make([]byte, 600)),
			expected: string(make([]byte, 500)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeAnswer(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAllEqual(t *testing.T) {
	assert.True(t, allEqual([]string{}))
	assert.True(t, allEqual([]string{"a"}))
	assert.True(t, allEqual([]string{"a", "a", "a"}))
	assert.False(t, allEqual([]string{"a", "b"}))
	assert.False(t, allEqual([]string{"a", "a", "b"}))
}

func TestNewTerminationTracker(t *testing.T) {
	tracker := NewTerminationTracker(TaskTypeRetrieval)

	assert.Equal(t, TaskTypeRetrieval, tracker.taskType)
	assert.Equal(t, 2, tracker.stabilityThreshold)
	assert.Equal(t, 0.8, tracker.minConfidence)
	assert.Empty(t, tracker.previousAnswers)
}
