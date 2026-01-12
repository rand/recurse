package rlm

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ClassifierAccuracyMetrics tracks classification accuracy across task types.
type ClassifierAccuracyMetrics struct {
	TotalTasks    int
	CorrectTasks  int
	ByType        map[TaskType]*TypeMetrics
	ConfusionMatrix map[TaskType]map[TaskType]int
}

// TypeMetrics tracks per-type classification metrics.
type TypeMetrics struct {
	Expected  int
	Correct   int
	Precision float64
	Recall    float64
}

// NewClassifierAccuracyMetrics creates a new metrics tracker.
func NewClassifierAccuracyMetrics() *ClassifierAccuracyMetrics {
	return &ClassifierAccuracyMetrics{
		ByType: map[TaskType]*TypeMetrics{
			TaskTypeComputational: {},
			TaskTypeRetrieval:     {},
			TaskTypeAnalytical:    {},
		},
		ConfusionMatrix: make(map[TaskType]map[TaskType]int),
	}
}

// Record records a classification result.
func (m *ClassifierAccuracyMetrics) Record(expected, actual TaskType) {
	m.TotalTasks++

	if m.ByType[expected] == nil {
		m.ByType[expected] = &TypeMetrics{}
	}
	m.ByType[expected].Expected++

	if expected == actual {
		m.CorrectTasks++
		m.ByType[expected].Correct++
	}

	// Update confusion matrix
	if m.ConfusionMatrix[expected] == nil {
		m.ConfusionMatrix[expected] = make(map[TaskType]int)
	}
	m.ConfusionMatrix[expected][actual]++
}

// Calculate computes precision and recall for each type.
func (m *ClassifierAccuracyMetrics) Calculate() {
	// Count predictions per type for precision calculation
	predictedCounts := make(map[TaskType]int)
	for _, predictions := range m.ConfusionMatrix {
		for predicted, count := range predictions {
			predictedCounts[predicted] += count
		}
	}

	for taskType, metrics := range m.ByType {
		if metrics.Expected > 0 {
			metrics.Recall = float64(metrics.Correct) / float64(metrics.Expected)
		}
		if predictedCounts[taskType] > 0 {
			metrics.Precision = float64(metrics.Correct) / float64(predictedCounts[taskType])
		}
	}
}

// Accuracy returns overall accuracy.
func (m *ClassifierAccuracyMetrics) Accuracy() float64 {
	if m.TotalTasks == 0 {
		return 0
	}
	return float64(m.CorrectTasks) / float64(m.TotalTasks)
}

// BenchmarkTask represents a task from the benchmark suite for testing.
type BenchmarkTask struct {
	Query        string
	ExpectedType TaskType
	Context      string
}

// generateBenchmarkTasks creates test tasks matching benchmark generator patterns.
func generateBenchmarkTasks() []BenchmarkTask {
	tasks := []BenchmarkTask{}

	// Counting tasks (from CountingGenerator)
	countingQueries := []string{
		"How many times does 'apple' appear in the text? Answer with just the number.",
		"How many times does 'banana' appear in the text? Answer with just the number.",
		"How many times does 'cherry' appear in the text? Answer with just the number.",
		"How many times does 'date' appear in the text? Answer with just the number.",
		"How many times does 'elderberry' appear in the text? Answer with just the number.",
		"Count the occurrences of 'error' in the log file.",
		"How often does the word 'meeting' appear?",
		"What is the total count of 'customer' mentions?",
	}
	for _, q := range countingQueries {
		tasks = append(tasks, BenchmarkTask{
			Query:        q,
			ExpectedType: TaskTypeComputational,
		})
	}

	// Aggregation tasks (from AggregationGenerator)
	aggregationQueries := []string{
		"What is the total sales amount across all regions? Answer with just the number (no $ sign).",
		"Calculate the sum of all revenue figures mentioned.",
		"Add up all the costs from the different departments.",
		"What is the combined total of all the values?",
		"Sum the amounts from North, South, East, West, and Central regions.",
	}
	for _, q := range aggregationQueries {
		tasks = append(tasks, BenchmarkTask{
			Query:        q,
			ExpectedType: TaskTypeComputational,
		})
	}

	// Needle/Retrieval tasks (from NeedleGenerator)
	needleQueries := []string{
		"What is the secret access code mentioned in the text?",
		"Find the password that was provided in the document.",
		"What is the API key mentioned in the config?",
		"What is the order ID?",
		"Find the authorization code in the text.",
		"What is the secret code mentioned?",
		"Locate the access token in the document.",
	}
	for _, q := range needleQueries {
		tasks = append(tasks, BenchmarkTask{
			Query:        q,
			ExpectedType: TaskTypeRetrieval,
		})
	}

	// Pairing/Analytical tasks (from PairingGenerator)
	pairingQueries := []string{
		"Based on the text, did Alice and Bob have any professional relationship? Answer 'yes' or 'no'.",
		"Based on the text, did Carol and David have any professional relationship? Answer 'yes' or 'no'.",
		"Did Eve work with Frank during this period? Answer 'yes' or 'no'.",
		"Was there any collaboration between Grace and Henry?",
		"Did the marketing team have any relationship with sales?",
		"Is there any connection between the two departments mentioned?",
	}
	for _, q := range pairingQueries {
		tasks = append(tasks, BenchmarkTask{
			Query:        q,
			ExpectedType: TaskTypeAnalytical,
		})
	}

	return tasks
}

// TestClassifierAccuracy_BenchmarkCorpus tests classifier against benchmark-style queries.
func TestClassifierAccuracy_BenchmarkCorpus(t *testing.T) {
	classifier := NewTaskClassifier()
	metrics := NewClassifierAccuracyMetrics()

	tasks := generateBenchmarkTasks()

	for _, task := range tasks {
		result := classifier.Classify(task.Query, nil)
		metrics.Record(task.ExpectedType, result.Type)

		// Log misclassifications for debugging
		if result.Type != task.ExpectedType {
			t.Logf("Misclassified: %q\n  Expected: %s, Got: %s (confidence: %.2f)\n  Signals: %v",
				task.Query, task.ExpectedType, result.Type, result.Confidence, result.Signals)
		}
	}

	metrics.Calculate()

	// Report results
	t.Logf("\n=== Classifier Accuracy Report ===")
	t.Logf("Total Tasks: %d", metrics.TotalTasks)
	t.Logf("Correct: %d", metrics.CorrectTasks)
	t.Logf("Overall Accuracy: %.1f%%", metrics.Accuracy()*100)
	t.Logf("")

	for taskType, m := range metrics.ByType {
		if m.Expected > 0 {
			t.Logf("%s:", taskType)
			t.Logf("  Expected: %d, Correct: %d", m.Expected, m.Correct)
			t.Logf("  Precision: %.1f%%, Recall: %.1f%%", m.Precision*100, m.Recall*100)
		}
	}

	// Confusion matrix
	t.Logf("\nConfusion Matrix:")
	for expected, predictions := range metrics.ConfusionMatrix {
		for predicted, count := range predictions {
			if count > 0 {
				t.Logf("  %s → %s: %d", expected, predicted, count)
			}
		}
	}

	// Assert minimum accuracy thresholds
	assert.GreaterOrEqual(t, metrics.Accuracy(), 0.90,
		"Overall accuracy should be >= 90%%")

	// Computational tasks are critical - must not miss them
	if metrics.ByType[TaskTypeComputational].Expected > 0 {
		assert.GreaterOrEqual(t, metrics.ByType[TaskTypeComputational].Recall, 0.90,
			"Computational recall should be >= 90%% (must not miss computational tasks)")
	}

	// Retrieval precision - avoid false positives that would slow things down
	if metrics.ByType[TaskTypeRetrieval].Expected > 0 {
		assert.GreaterOrEqual(t, metrics.ByType[TaskTypeRetrieval].Recall, 0.85,
			"Retrieval recall should be >= 85%%")
	}
}

// TestClassifierAccuracy_NoFalseComputational ensures retrieval tasks aren't misclassified as computational.
func TestClassifierAccuracy_NoFalseComputational(t *testing.T) {
	classifier := NewTaskClassifier()

	// These retrieval queries should NOT be classified as computational
	retrievalQueries := []string{
		"What is the secret access code mentioned in the text?",
		"Find the password in the document.",
		"What is the API key?",
		"Locate the authorization token.",
	}

	falsePositives := 0
	for _, query := range retrievalQueries {
		result := classifier.Classify(query, nil)
		if result.Type == TaskTypeComputational {
			falsePositives++
			t.Logf("False positive (retrieval → computational): %q", query)
		}
	}

	assert.Equal(t, 0, falsePositives,
		"Retrieval queries should not be classified as computational")
}

// TestClassifierAccuracy_ComputationalVariants tests various phrasings of computational queries.
func TestClassifierAccuracy_ComputationalVariants(t *testing.T) {
	classifier := NewTaskClassifier()

	variants := []string{
		// Counting variations
		"How many times does 'x' appear?",
		"Count the occurrences of 'x'.",
		"What is the count of 'x' in the text?",
		"How often does 'x' occur?",
		"Find the number of 'x' mentions.",

		// Summing variations
		"What is the total?",
		"Calculate the sum.",
		"Add up all the values.",
		"What is the combined amount?",
		"Sum all the numbers.",

		// Aggregation variations
		"What is the total sales across all regions?",
		"Calculate the sum of all costs.",
		"Add up the revenue from each department.",
	}

	correct := 0
	for _, query := range variants {
		result := classifier.Classify(query, nil)
		if result.Type == TaskTypeComputational {
			correct++
		} else {
			t.Logf("Missed computational: %q → %s (confidence: %.2f)",
				query, result.Type, result.Confidence)
		}
	}

	accuracy := float64(correct) / float64(len(variants))
	t.Logf("Computational variants accuracy: %.1f%% (%d/%d)",
		accuracy*100, correct, len(variants))

	assert.GreaterOrEqual(t, accuracy, 0.85,
		"Should correctly classify >= 85%% of computational variants")
}

// TestClassifierAccuracy_ContextInfluence tests how context affects classification.
func TestClassifierAccuracy_ContextInfluence(t *testing.T) {
	classifier := NewTaskClassifier()

	// Ambiguous query that could go either way
	query := "What is the total?"

	// With numeric context, should lean computational
	numericContext := []ContextSource{{
		Content: "Sales: $1000. Revenue: $2000. Cost: $500. Total items: 100.",
	}}

	result := classifier.Classify(query, numericContext)
	assert.Equal(t, TaskTypeComputational, result.Type,
		"Query with numeric context should be computational")
	assert.Contains(t, result.Signals, "context:high_numeric_density",
		"Should detect numeric density in context")

	// With identifier context, might lean retrieval
	idContext := []ContextSource{{
		Content: "The authorization CODE-12345 was issued. Use KEY-67890 for access.",
	}}

	result2 := classifier.Classify("What is the code?", idContext)
	assert.Equal(t, TaskTypeRetrieval, result2.Type,
		"Query about code with ID context should be retrieval")
}

// TestClassifierAccuracy_ConfidenceCorrelation tests that confidence correlates with correctness.
func TestClassifierAccuracy_ConfidenceCorrelation(t *testing.T) {
	classifier := NewTaskClassifier()
	tasks := generateBenchmarkTasks()

	var highConfCorrect, highConfTotal int
	var lowConfCorrect, lowConfTotal int

	for _, task := range tasks {
		result := classifier.Classify(task.Query, nil)
		correct := result.Type == task.ExpectedType

		if result.Confidence >= 0.8 {
			highConfTotal++
			if correct {
				highConfCorrect++
			}
		} else if result.Confidence >= 0.5 {
			lowConfTotal++
			if correct {
				lowConfCorrect++
			}
		}
	}

	highConfAccuracy := float64(highConfCorrect) / float64(highConfTotal)
	lowConfAccuracy := float64(lowConfCorrect) / float64(lowConfTotal)

	t.Logf("High confidence (>=0.8) accuracy: %.1f%% (%d/%d)",
		highConfAccuracy*100, highConfCorrect, highConfTotal)
	t.Logf("Low confidence (0.5-0.8) accuracy: %.1f%% (%d/%d)",
		lowConfAccuracy*100, lowConfCorrect, lowConfTotal)

	// High confidence should have higher accuracy than low confidence
	if lowConfTotal > 0 && highConfTotal > 0 {
		assert.GreaterOrEqual(t, highConfAccuracy, lowConfAccuracy,
			"High confidence classifications should be more accurate than low confidence")
	}

	// High confidence should be very accurate
	if highConfTotal > 0 {
		assert.GreaterOrEqual(t, highConfAccuracy, 0.90,
			"High confidence classifications should be >= 90%% accurate")
	}
}

// BenchmarkClassifier benchmarks classification performance.
func BenchmarkClassifier(b *testing.B) {
	classifier := NewTaskClassifier()
	tasks := generateBenchmarkTasks()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := tasks[i%len(tasks)]
		classifier.Classify(task.Query, nil)
	}
}

// BenchmarkClassifierWithContext benchmarks classification with context analysis.
func BenchmarkClassifierWithContext(b *testing.B) {
	classifier := NewTaskClassifier()
	tasks := generateBenchmarkTasks()
	context := []ContextSource{{
		Content: "Sales: $1000. Revenue: $2000. The CODE-12345 was issued. Error count: 42.",
	}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := tasks[i%len(tasks)]
		classifier.Classify(task.Query, context)
	}
}

// PrintClassifierAccuracyReport generates a detailed accuracy report.
func PrintClassifierAccuracyReport() string {
	classifier := NewTaskClassifier()
	metrics := NewClassifierAccuracyMetrics()
	tasks := generateBenchmarkTasks()

	for _, task := range tasks {
		result := classifier.Classify(task.Query, nil)
		metrics.Record(task.ExpectedType, result.Type)
	}

	metrics.Calculate()

	report := fmt.Sprintf(`
Classifier Accuracy Report
==========================
Total Tasks: %d
Correct: %d
Overall Accuracy: %.1f%%

By Type:
`, metrics.TotalTasks, metrics.CorrectTasks, metrics.Accuracy()*100)

	for taskType, m := range metrics.ByType {
		if m.Expected > 0 {
			report += fmt.Sprintf("  %s: Precision=%.1f%%, Recall=%.1f%% (n=%d)\n",
				taskType, m.Precision*100, m.Recall*100, m.Expected)
		}
	}

	return report
}
