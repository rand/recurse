package rlm

import (
	"regexp"
	"strings"
)

// TerminationCheck contains the result of an early termination check.
type TerminationCheck struct {
	// ShouldTerminate indicates whether the RLM loop should terminate early.
	ShouldTerminate bool

	// Reason explains why termination is recommended.
	Reason string

	// Confidence indicates how confident we are in the termination decision.
	Confidence float64
}

// TerminationTracker tracks state across iterations for termination decisions.
type TerminationTracker struct {
	// Previous answers for stability detection
	previousAnswers []string

	// Task classification for context-aware decisions
	taskType TaskType

	// Configuration
	stabilityThreshold int     // Number of stable answers needed
	minConfidence      float64 // Minimum confidence for early termination
}

// NewTerminationTracker creates a new termination tracker.
func NewTerminationTracker(taskType TaskType) *TerminationTracker {
	return &TerminationTracker{
		previousAnswers:    make([]string, 0),
		taskType:           taskType,
		stabilityThreshold: 2, // Need 2 identical answers to terminate
		minConfidence:      0.8,
	}
}

// IterationResult contains information about an iteration for termination checking.
type IterationResult struct {
	// HasFinal indicates if FINAL() was called
	HasFinal bool

	// FinalOutput is the output from FINAL() if called
	FinalOutput string

	// CodeExecuted is the Python code that was executed
	CodeExecuted string

	// REPLOutput is the output from REPL execution
	REPLOutput string

	// REPLError is any error from REPL execution
	REPLError string

	// Iteration is the current iteration number (1-based)
	Iteration int
}

// CheckTermination evaluates whether the RLM loop should terminate early.
func (t *TerminationTracker) CheckTermination(result *IterationResult) TerminationCheck {
	// Priority 1: FINAL() was called - always terminate
	if result.HasFinal {
		return TerminationCheck{
			ShouldTerminate: true,
			Reason:          "FINAL() called",
			Confidence:      1.0,
		}
	}

	// Priority 2: Check for answer stability (same output in consecutive iterations)
	if check := t.checkAnswerStability(result); check.ShouldTerminate {
		return check
	}

	// Priority 3: Check for simple task completion in first iteration
	if result.Iteration == 1 {
		if check := t.checkSimpleTaskCompletion(result); check.ShouldTerminate {
			return check
		}
	}

	// Priority 4: Check for error loops (same error repeating)
	if check := t.checkErrorLoop(result); check.ShouldTerminate {
		return check
	}

	// No early termination
	return TerminationCheck{
		ShouldTerminate: false,
		Reason:          "continue iteration",
		Confidence:      0.0,
	}
}

// checkAnswerStability checks if the answer has stabilized across iterations.
func (t *TerminationTracker) checkAnswerStability(result *IterationResult) TerminationCheck {
	// Use REPL output as the "answer" for stability checking
	currentAnswer := normalizeAnswer(result.REPLOutput)
	if currentAnswer == "" {
		return TerminationCheck{ShouldTerminate: false}
	}

	// Add to history
	t.previousAnswers = append(t.previousAnswers, currentAnswer)

	// Check for stability
	if len(t.previousAnswers) >= t.stabilityThreshold {
		recent := t.previousAnswers[len(t.previousAnswers)-t.stabilityThreshold:]
		if allEqual(recent) {
			return TerminationCheck{
				ShouldTerminate: true,
				Reason:          "answer stabilized across iterations",
				Confidence:      0.85,
			}
		}
	}

	return TerminationCheck{ShouldTerminate: false}
}

// checkSimpleTaskCompletion checks if a simple task was completed in one iteration.
func (t *TerminationTracker) checkSimpleTaskCompletion(result *IterationResult) TerminationCheck {
	// Only apply to computational tasks
	if t.taskType != TaskTypeComputational {
		return TerminationCheck{ShouldTerminate: false}
	}

	// Check if the code is a simple one-liner that produced output
	if isSimpleComputation(result.CodeExecuted) && result.REPLOutput != "" && result.REPLError == "" {
		// Check if output looks like a final answer (number, short string)
		if looksLikeSimpleAnswer(result.REPLOutput) {
			return TerminationCheck{
				ShouldTerminate: true,
				Reason:          "simple computation completed with clear output",
				Confidence:      0.9,
			}
		}
	}

	return TerminationCheck{ShouldTerminate: false}
}

// checkErrorLoop detects if we're stuck in an error loop.
func (t *TerminationTracker) checkErrorLoop(result *IterationResult) TerminationCheck {
	// Track errors in previous answers (we reuse the slice)
	if result.REPLError != "" {
		errorSig := normalizeAnswer(result.REPLError)

		// Count how many times we've seen this error
		errorCount := 0
		for _, prev := range t.previousAnswers {
			if strings.Contains(prev, "ERROR:") && strings.Contains(prev, errorSig[:min(50, len(errorSig))]) {
				errorCount++
			}
		}

		// Mark this as an error for tracking
		t.previousAnswers = append(t.previousAnswers, "ERROR:"+errorSig)

		// If we've seen the same error 3+ times, terminate
		if errorCount >= 2 {
			return TerminationCheck{
				ShouldTerminate: true,
				Reason:          "stuck in error loop",
				Confidence:      0.95,
			}
		}
	}

	return TerminationCheck{ShouldTerminate: false}
}

// normalizeAnswer normalizes an answer for comparison.
func normalizeAnswer(s string) string {
	// Trim whitespace
	s = strings.TrimSpace(s)

	// Normalize newlines
	s = strings.ReplaceAll(s, "\r\n", "\n")

	// Truncate very long outputs for comparison
	if len(s) > 500 {
		s = s[:500]
	}

	return s
}

// allEqual checks if all strings in the slice are equal.
func allEqual(strs []string) bool {
	if len(strs) == 0 {
		return true
	}
	first := strs[0]
	for _, s := range strs[1:] {
		if s != first {
			return false
		}
	}
	return true
}

// isSimpleComputation checks if code is a simple one-liner computation.
func isSimpleComputation(code string) bool {
	lines := strings.Split(strings.TrimSpace(code), "\n")

	// Filter out import statements and comments
	meaningfulLines := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ") {
			continue
		}
		meaningfulLines++
	}

	// Simple computation: 1-3 meaningful lines
	if meaningfulLines > 3 {
		return false
	}

	// Check for computational patterns
	computationalPatterns := []string{
		`len\(`,             // Counting
		`sum\(`,             // Summing
		`re\.findall`,       // Regex matching
		`\.count\(`,         // String counting
		`FINAL\(`,           // Direct final output
		`print\(.*\d`,       // Printing a number
		`str\(\d`,           // Converting number to string
		`len\(re\.findall`,  // Count regex matches
	}

	for _, pattern := range computationalPatterns {
		if matched, _ := regexp.MatchString(pattern, code); matched {
			return true
		}
	}

	return false
}

// looksLikeSimpleAnswer checks if output looks like a final answer.
func looksLikeSimpleAnswer(output string) bool {
	output = strings.TrimSpace(output)

	// Empty output is not a simple answer
	if output == "" {
		return false
	}

	// Very long output is not a simple answer
	if len(output) > 100 {
		return false
	}

	// Check for numeric answer
	numericPattern := regexp.MustCompile(`^-?\d+\.?\d*$`)
	if numericPattern.MatchString(output) {
		return true
	}

	// Check for simple word/phrase answer
	if len(strings.Fields(output)) <= 10 {
		return true
	}

	return false
}
