package benchmark

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// DefaultScorer implements the Scorer interface with common evaluation logic.
type DefaultScorer struct {
	// NumericTolerance is the allowed difference for numeric comparisons.
	NumericTolerance float64
}

// NewDefaultScorer creates a scorer with default settings.
func NewDefaultScorer() *DefaultScorer {
	return &DefaultScorer{
		NumericTolerance: 0.01, // 1% tolerance
	}
}

// Score evaluates an answer against the expected value.
func (s *DefaultScorer) Score(answer, expected string, answerType AnswerType) (float64, bool) {
	// Normalize whitespace
	answer = strings.TrimSpace(answer)
	expected = strings.TrimSpace(expected)

	switch answerType {
	case AnswerExact:
		return s.scoreExact(answer, expected)
	case AnswerNumeric:
		return s.scoreNumeric(answer, expected)
	case AnswerF1:
		return s.scoreF1(answer, expected)
	case AnswerContains:
		return s.scoreContains(answer, expected)
	default:
		// Default to exact match
		return s.scoreExact(answer, expected)
	}
}

func (s *DefaultScorer) scoreExact(answer, expected string) (float64, bool) {
	// Case-insensitive exact match
	if strings.EqualFold(answer, expected) {
		return 1.0, true
	}
	return 0.0, false
}

func (s *DefaultScorer) scoreNumeric(answer, expected string) (float64, bool) {
	// Extract numbers from both strings
	answerNum := extractNumber(answer)
	expectedNum := extractNumber(expected)

	if math.IsNaN(answerNum) || math.IsNaN(expectedNum) {
		return 0.0, false
	}

	// Check if within tolerance
	if expectedNum == 0 {
		if answerNum == 0 {
			return 1.0, true
		}
		return 0.0, false
	}

	diff := math.Abs(answerNum-expectedNum) / math.Abs(expectedNum)
	if diff <= s.NumericTolerance {
		return 1.0, true
	}

	// Partial credit based on how close
	if diff < 0.1 { // Within 10%
		return 1.0 - diff, false
	}
	if diff < 0.5 { // Within 50%
		return 0.5 - diff, false
	}

	return 0.0, false
}

func (s *DefaultScorer) scoreF1(answer, expected string) (float64, bool) {
	// Parse both as comma/space separated sets
	answerSet := parseSet(answer)
	expectedSet := parseSet(expected)

	if len(expectedSet) == 0 {
		if len(answerSet) == 0 {
			return 1.0, true
		}
		return 0.0, false
	}

	// Calculate precision and recall
	truePositives := 0
	for item := range answerSet {
		if expectedSet[item] {
			truePositives++
		}
	}

	precision := float64(truePositives) / float64(len(answerSet))
	recall := float64(truePositives) / float64(len(expectedSet))

	if precision+recall == 0 {
		return 0.0, false
	}

	f1 := 2 * precision * recall / (precision + recall)
	return f1, f1 >= 0.99 // Consider correct if F1 >= 0.99
}

func (s *DefaultScorer) scoreContains(answer, expected string) (float64, bool) {
	// Check if answer contains expected (case-insensitive)
	if strings.Contains(strings.ToLower(answer), strings.ToLower(expected)) {
		return 1.0, true
	}
	return 0.0, false
}

// extractNumber extracts the first number from a string.
func extractNumber(s string) float64 {
	// Remove common formatting
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, "%", "")

	// Find number pattern
	re := regexp.MustCompile(`-?\d+\.?\d*`)
	match := re.FindString(s)
	if match == "" {
		return math.NaN()
	}

	num, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return math.NaN()
	}
	return num
}

// parseSet parses a string into a set of items.
func parseSet(s string) map[string]bool {
	set := make(map[string]bool)

	// Split by common delimiters
	s = strings.ReplaceAll(s, ",", " ")
	s = strings.ReplaceAll(s, ";", " ")
	s = strings.ReplaceAll(s, "\n", " ")

	for _, item := range strings.Fields(s) {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && item != "and" && item != "or" {
			set[item] = true
		}
	}

	return set
}

// ContextRotAnalyzer analyzes performance degradation across context lengths.
type ContextRotAnalyzer struct {
	results map[int][]Result // Results by context length bucket
}

// NewContextRotAnalyzer creates a new context rot analyzer.
func NewContextRotAnalyzer() *ContextRotAnalyzer {
	return &ContextRotAnalyzer{
		results: make(map[int][]Result),
	}
}

// Add records a result for analysis.
func (a *ContextRotAnalyzer) Add(contextTokens int, result Result) {
	// Bucket by nearest 4K
	bucket := (contextTokens / 4000) * 4000
	if bucket == 0 {
		bucket = 4000
	}
	a.results[bucket] = append(a.results[bucket], result)
}

// Analyze computes context rot metrics.
func (a *ContextRotAnalyzer) Analyze() ContextRotReport {
	report := ContextRotReport{
		ByContextLength: make(map[int]ContextRotMetrics),
	}

	var allScores []float64
	var allLengths []int

	for bucket, results := range a.results {
		var totalScore float64
		var correctCount int

		for _, r := range results {
			totalScore += r.Score
			if r.Correct {
				correctCount++
			}
			allScores = append(allScores, r.Score)
			allLengths = append(allLengths, bucket)
		}

		metrics := ContextRotMetrics{
			ContextTokens: bucket,
			TaskCount:     len(results),
			MeanScore:     totalScore / float64(len(results)),
			Accuracy:      float64(correctCount) / float64(len(results)),
		}
		report.ByContextLength[bucket] = metrics
	}

	// Calculate degradation rate (linear regression of score vs context length)
	if len(allScores) > 1 {
		report.DegradationRate = linearRegressionSlope(allLengths, allScores)
	}

	return report
}

// ContextRotReport contains context rot analysis results.
type ContextRotReport struct {
	// ByContextLength contains metrics for each context length bucket.
	ByContextLength map[int]ContextRotMetrics

	// DegradationRate is the slope of score vs context length.
	// Negative values indicate performance degradation with longer contexts.
	DegradationRate float64
}

// ContextRotMetrics contains metrics for a specific context length.
type ContextRotMetrics struct {
	ContextTokens int
	TaskCount     int
	MeanScore     float64
	Accuracy      float64
}

// linearRegressionSlope computes the slope of y vs x using least squares.
func linearRegressionSlope(x []int, y []float64) float64 {
	if len(x) != len(y) || len(x) < 2 {
		return 0
	}

	n := float64(len(x))
	var sumX, sumY, sumXY, sumX2 float64

	for i := range x {
		xi := float64(x[i])
		yi := y[i]
		sumX += xi
		sumY += yi
		sumXY += xi * yi
		sumX2 += xi * xi
	}

	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0
	}

	return (n*sumXY - sumX*sumY) / denominator
}
