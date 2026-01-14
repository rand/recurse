package rlm

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewComputationAdvisor(t *testing.T) {
	advisor := NewComputationAdvisor()
	require.NotNil(t, advisor)
	assert.NotEmpty(t, advisor.Patterns())
}

func TestComputationAdvisor_SuggestREPL_Counting(t *testing.T) {
	advisor := NewComputationAdvisor()

	tests := []struct {
		name           string
		query          string
		wantPattern    string
		wantConfidence float64
	}{
		{
			name:           "how many times",
			query:          "How many times does 'error' appear in the log?",
			wantPattern:    "count_occurrences",
			wantConfidence: 0.9,
		},
		{
			name:           "count occurrences",
			query:          "Count the number of occurrences of the word 'function'",
			wantPattern:    "count_occurrences",
			wantConfidence: 0.9,
		},
		{
			name:           "count functions",
			query:          "How many functions are defined in this file?",
			wantPattern:    "count_items",
			wantConfidence: 0.85,
		},
		{
			name:           "list all classes",
			query:          "List all classes in this module",
			wantPattern:    "count_items",
			wantConfidence: 0.85,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestREPL(tt.query, nil)
			require.NotNil(t, suggestion, "expected suggestion for: %s", tt.query)
			assert.Equal(t, tt.wantPattern, suggestion.Pattern)
			assert.GreaterOrEqual(t, suggestion.Confidence, tt.wantConfidence-0.1)
		})
	}
}

func TestComputationAdvisor_SuggestREPL_Arithmetic(t *testing.T) {
	advisor := NewComputationAdvisor()

	tests := []struct {
		name        string
		query       string
		wantPattern string
	}{
		{
			name:        "sum dollars",
			query:       "What is the total amount in dollars?",
			wantPattern: "sum_values",
		},
		{
			name:        "sum sales",
			query:       "Sum all the sales values from Q1",
			wantPattern: "sum_values",
		},
		{
			name:        "average calculation",
			query:       "What's the average of all test scores?",
			wantPattern: "average_calculation",
		},
		{
			name:        "percentage",
			query:       "What percentage of users are active?",
			wantPattern: "percentage_calculation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestREPL(tt.query, nil)
			require.NotNil(t, suggestion, "expected suggestion for: %s", tt.query)
			assert.Equal(t, tt.wantPattern, suggestion.Pattern)
		})
	}
}

func TestComputationAdvisor_SuggestREPL_Sorting(t *testing.T) {
	advisor := NewComputationAdvisor()

	tests := []struct {
		name        string
		query       string
		wantPattern string
	}{
		{
			name:        "sort by date",
			query:       "Sort the items by date",
			wantPattern: "sort_by_attribute",
		},
		{
			name:        "rank by score",
			query:       "Rank employees by performance score",
			wantPattern: "sort_by_attribute",
		},
		{
			name:        "highest value",
			query:       "What is the highest sales value?",
			wantPattern: "find_extremes",
		},
		{
			name:        "top 5",
			query:       "Show me the top 5 performers",
			wantPattern: "find_extremes",
		},
		{
			name:        "lowest score",
			query:       "Find the lowest test score",
			wantPattern: "find_extremes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestREPL(tt.query, nil)
			require.NotNil(t, suggestion, "expected suggestion for: %s", tt.query)
			assert.Equal(t, tt.wantPattern, suggestion.Pattern)
		})
	}
}

func TestComputationAdvisor_SuggestREPL_Filtering(t *testing.T) {
	advisor := NewComputationAdvisor()

	tests := []struct {
		name        string
		query       string
		wantPattern string
	}{
		{
			name:        "filter greater than",
			query:       "Filter items where value is greater than 100",
			wantPattern: "filter_by_condition",
		},
		{
			name:        "find with condition",
			query:       "Find all entries that contain 'error'",
			wantPattern: "filter_by_condition",
		},
		{
			name:        "filter by date",
			query:       "Get records from January 2024",
			wantPattern: "filter_by_date",
		},
		{
			name:        "since date",
			query:       "Show transactions since March",
			wantPattern: "filter_by_date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestREPL(tt.query, nil)
			require.NotNil(t, suggestion, "expected suggestion for: %s", tt.query)
			assert.Equal(t, tt.wantPattern, suggestion.Pattern)
		})
	}
}

func TestComputationAdvisor_SuggestREPL_Aggregation(t *testing.T) {
	advisor := NewComputationAdvisor()

	tests := []struct {
		name        string
		query       string
		wantPattern string
	}{
		{
			name:        "group by category",
			query:       "Group the sales by region",
			wantPattern: "group_by",
		},
		{
			name:        "summarize per department",
			query:       "Summarize tasks per department",
			wantPattern: "group_by",
		},
		{
			name:        "unique values",
			query:       "Get unique items from the list",
			wantPattern: "unique_items",
		},
		{
			name:        "distinct entries",
			query:       "Find distinct values in the column",
			wantPattern: "unique_items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestREPL(tt.query, nil)
			require.NotNil(t, suggestion, "expected suggestion for: %s", tt.query)
			assert.Equal(t, tt.wantPattern, suggestion.Pattern)
		})
	}
}

func TestComputationAdvisor_SuggestREPL_NoMatch(t *testing.T) {
	advisor := NewComputationAdvisor()

	queries := []string{
		"What is the purpose of life?",
		"Explain this code",
		"How does this work?",
		"Tell me about the project",
	}

	for _, query := range queries {
		suggestion := advisor.SuggestREPL(query, nil)
		assert.Nil(t, suggestion, "expected no suggestion for: %s", query)
	}
}

func TestComputationAdvisor_SuggestREPL_WithContext(t *testing.T) {
	advisor := NewComputationAdvisor()

	// Context with high numeric density should boost counting confidence
	numericContext := ContextSource{
		Type:    ContextTypeFile,
		Name:    "sales.txt",
		Content: "Q1: $100, Q2: $200, Q3: $150, Q4: $250. Total: 1000 units sold. Revenue: $45000.",
	}

	query := "How many times does a dollar amount appear?"
	suggestion := advisor.SuggestREPL(query, []ContextSource{numericContext})
	require.NotNil(t, suggestion)
	// Confidence should be boosted by numeric-dense context
	assert.GreaterOrEqual(t, suggestion.Confidence, 0.9)
}

func TestComputationAdvisor_AddPattern(t *testing.T) {
	advisor := NewComputationAdvisor()
	initialCount := len(advisor.Patterns())

	customPattern := ComputationPattern{
		Name:         "custom_pattern",
		Regex:        regexp.MustCompile(`(?i)custom operation`),
		Approach:     "Use custom approach",
		CodeTemplate: "# Custom code",
		Confidence:   0.9,
	}

	advisor.AddPattern(customPattern)
	assert.Len(t, advisor.Patterns(), initialCount+1)

	// Test that new pattern matches
	suggestion := advisor.SuggestREPL("Please do custom operation", nil)
	require.NotNil(t, suggestion)
	assert.Equal(t, "custom_pattern", suggestion.Pattern)
}

func TestComputationAdvisor_FrequencyAnalysis(t *testing.T) {
	advisor := NewComputationAdvisor()

	tests := []struct {
		name  string
		query string
	}{
		{"how often", "How often does this error occur?"},
		{"most common", "What is the most common word?"},
		{"frequency", "What's the frequency of API calls?"},
		{"least common", "Find the least common element"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestREPL(tt.query, nil)
			require.NotNil(t, suggestion, "expected suggestion for: %s", tt.query)
			assert.Equal(t, "frequency_analysis", suggestion.Pattern)
		})
	}
}

func TestComputationAdvisor_CompareValues(t *testing.T) {
	advisor := NewComputationAdvisor()

	tests := []struct {
		name  string
		query string
	}{
		{"compare between", "Compare revenue between Q1 and Q2"},
		{"difference from", "What's the difference from last month?"},
		{"growth over", "Show growth over the past year"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := advisor.SuggestREPL(tt.query, nil)
			require.NotNil(t, suggestion, "expected suggestion for: %s", tt.query)
			assert.Equal(t, "compare_values", suggestion.Pattern)
		})
	}
}

func TestExtractCountTarget(t *testing.T) {
	tests := []struct {
		query    string
		expected []string
	}{
		{`How many times does "error" appear?`, []string{"error"}},
		{`Count occurrences of 'warning' in the log`, []string{"warning"}},
		{`Number of functions in the file`, []string{"functions"}},
	}

	for _, tt := range tests {
		result := extractCountTarget(tt.query)
		if len(tt.expected) > 0 {
			assert.NotEmpty(t, result, "query: %s", tt.query)
			// Check at least one expected value is found
			found := false
			for _, exp := range tt.expected {
				for _, r := range result {
					if r == exp {
						found = true
						break
					}
				}
			}
			assert.True(t, found, "expected one of %v in %v for query: %s", tt.expected, result, tt.query)
		}
	}
}

func TestExtractCodeElement(t *testing.T) {
	tests := []struct {
		query    string
		expected []string
	}{
		{"How many functions are defined?", []string{"function"}},
		{"List all classes and methods", []string{"class", "method"}},
		{"Count imports in the file", []string{"import", "file"}},
	}

	for _, tt := range tests {
		result := extractCodeElement(tt.query)
		for _, exp := range tt.expected {
			assert.Contains(t, result, exp, "query: %s", tt.query)
		}
	}
}

func TestREPLSuggestion_FormatForPrompt(t *testing.T) {
	suggestion := &REPLSuggestion{
		Pattern:    "count_occurrences",
		Approach:   "Use Counter to count items",
		CodeHint:   "counter = Counter(items)",
		Confidence: 0.9,
		Variables:  []string{"error", "warning"},
	}

	formatted := suggestion.FormatForPrompt()
	assert.Contains(t, formatted, "count_occurrences")
	assert.Contains(t, formatted, "Use Counter to count items")
	assert.Contains(t, formatted, "counter = Counter(items)")
	assert.Contains(t, formatted, "error")
	assert.Contains(t, formatted, "warning")
}

func TestREPLSuggestion_FormatForPrompt_Nil(t *testing.T) {
	var suggestion *REPLSuggestion
	formatted := suggestion.FormatForPrompt()
	assert.Empty(t, formatted)
}

func TestComputationAdvisor_ConfidenceThreshold(t *testing.T) {
	advisor := NewComputationAdvisor()

	// Add a low-confidence pattern
	lowConfidencePattern := ComputationPattern{
		Name:       "low_confidence",
		Regex:      regexp.MustCompile(`(?i)maybe calculate`),
		Approach:   "Low confidence approach",
		Confidence: 0.3,
	}
	advisor.AddPattern(lowConfidencePattern)

	// Should return nil due to confidence threshold
	suggestion := advisor.SuggestREPL("Maybe calculate something", nil)
	assert.Nil(t, suggestion)
}

func TestComputationAdvisor_BestMatchSelection(t *testing.T) {
	advisor := NewComputationAdvisor()

	// Query that could match multiple patterns - should pick highest confidence
	query := "How many times does the total amount appear?"

	suggestion := advisor.SuggestREPL(query, nil)
	require.NotNil(t, suggestion)

	// Should match count_occurrences (0.9) over sum_values (0.9) because it appears first
	// or whichever has higher contextual relevance
	assert.Contains(t, []string{"count_occurrences", "sum_values"}, suggestion.Pattern)
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkComputationAdvisor_SuggestREPL(b *testing.B) {
	advisor := NewComputationAdvisor()
	queries := []string{
		"How many times does 'error' appear in the log?",
		"What is the total sales amount?",
		"Sort items by date",
		"Filter records where value is greater than 100",
		"Group the results by category",
		"What is the most common word?",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		query := queries[i%len(queries)]
		_ = advisor.SuggestREPL(query, nil)
	}
}

func BenchmarkComputationAdvisor_SuggestREPL_WithContext(b *testing.B) {
	advisor := NewComputationAdvisor()
	contexts := []ContextSource{
		{
			Type:    ContextTypeFile,
			Name:    "data.txt",
			Content: "Q1: $100, Q2: $200, Q3: $150. Revenue: $45000. Items: 1000.",
		},
	}
	query := "How many dollar amounts appear in the data?"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = advisor.SuggestREPL(query, contexts)
	}
}

func BenchmarkComputationAdvisor_PatternMatching(b *testing.B) {
	advisor := NewComputationAdvisor()

	// Test pattern matching performance across different query types
	b.Run("counting", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = advisor.SuggestREPL("How many times does X occur?", nil)
		}
	})

	b.Run("arithmetic", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = advisor.SuggestREPL("What is the total sum of all values?", nil)
		}
	})

	b.Run("sorting", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = advisor.SuggestREPL("Sort items by price ascending", nil)
		}
	})

	b.Run("no_match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = advisor.SuggestREPL("Explain this code to me", nil)
		}
	})
}
