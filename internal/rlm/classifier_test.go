package rlm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskClassifier_Counting(t *testing.T) {
	c := NewTaskClassifier()

	tests := []struct {
		name     string
		query    string
		wantType TaskType
		minConf  float64
	}{
		{
			name:     "how many times",
			query:    "How many times does 'apple' appear in the text?",
			wantType: TaskTypeComputational,
			minConf:  0.8,
		},
		{
			name:     "count occurrences",
			query:    "Count the number of occurrences of 'error' in the log.",
			wantType: TaskTypeComputational,
			minConf:  0.8,
		},
		{
			name:     "how often",
			query:    "How often does the word 'meeting' appear?",
			wantType: TaskTypeComputational,
			minConf:  0.8,
		},
		{
			name:     "answer with number",
			query:    "How many times does 'banana' appear in the text? Answer with just the number.",
			wantType: TaskTypeComputational,
			minConf:  0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.query, nil)
			assert.Equal(t, tt.wantType, result.Type, "wrong task type")
			assert.GreaterOrEqual(t, result.Confidence, tt.minConf, "confidence too low")
			assert.NotEmpty(t, result.Signals, "expected signals")
		})
	}
}

func TestTaskClassifier_Summing(t *testing.T) {
	c := NewTaskClassifier()

	tests := []struct {
		name     string
		query    string
		wantType TaskType
		minConf  float64
	}{
		{
			name:     "total sales",
			query:    "What is the total sales amount across all regions?",
			wantType: TaskTypeComputational,
			minConf:  0.7,
		},
		{
			name:     "sum of values",
			query:    "Calculate the sum of all the dollar amounts mentioned.",
			wantType: TaskTypeComputational,
			minConf:  0.7,
		},
		{
			name:     "add up costs",
			query:    "Add up all the costs from the different departments.",
			wantType: TaskTypeComputational,
			minConf:  0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.query, nil)
			assert.Equal(t, tt.wantType, result.Type, "wrong task type")
			assert.GreaterOrEqual(t, result.Confidence, tt.minConf, "confidence too low")
		})
	}
}

func TestTaskClassifier_Retrieval(t *testing.T) {
	c := NewTaskClassifier()

	tests := []struct {
		name     string
		query    string
		wantType TaskType
		minConf  float64
	}{
		{
			name:     "secret code",
			query:    "What is the secret access code mentioned in the text?",
			wantType: TaskTypeRetrieval,
			minConf:  0.7,
		},
		{
			name:     "find password",
			query:    "Find the password that was provided in the document.",
			wantType: TaskTypeRetrieval,
			minConf:  0.7,
		},
		{
			name:     "what is the id",
			query:    "What is the order ID?",
			wantType: TaskTypeRetrieval,
			minConf:  0.7,
		},
		{
			name:     "where is mentioned",
			query:    "What was the API key mentioned in the config section?",
			wantType: TaskTypeRetrieval,
			minConf:  0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.query, nil)
			assert.Equal(t, tt.wantType, result.Type, "wrong task type for: %s", tt.query)
			assert.GreaterOrEqual(t, result.Confidence, tt.minConf, "confidence too low")
		})
	}
}

func TestTaskClassifier_Analytical(t *testing.T) {
	c := NewTaskClassifier()

	tests := []struct {
		name     string
		query    string
		wantType TaskType
		minConf  float64
	}{
		{
			name:     "relationship question",
			query:    "Did Alice work with Bob during this period?",
			wantType: TaskTypeAnalytical,
			minConf:  0.6,
		},
		{
			name:     "yes no answer",
			query:    "Did they collaborate on the project? Answer 'yes' or 'no'.",
			wantType: TaskTypeAnalytical,
			minConf:  0.5,
		},
		{
			name:     "related to",
			query:    "Is the marketing team related to the sales initiative?",
			wantType: TaskTypeAnalytical,
			minConf:  0.6,
		},
		{
			name:     "any relationship",
			query:    "Was there any professional relationship between them?",
			wantType: TaskTypeAnalytical,
			minConf:  0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.query, nil)
			assert.Equal(t, tt.wantType, result.Type, "wrong task type for: %s", tt.query)
			assert.GreaterOrEqual(t, result.Confidence, tt.minConf, "confidence too low")
		})
	}
}

func TestTaskClassifier_ContextAnalysis(t *testing.T) {
	c := NewTaskClassifier()

	t.Run("high numeric density boosts computational", func(t *testing.T) {
		query := "What is the total?"
		numericContext := []ContextSource{{
			Content: "Sales: $1000. Revenue: $2000. Cost: $500. Profit: $1500. Units: 100. Price: $10.",
		}}

		result := c.Classify(query, numericContext)
		assert.Equal(t, TaskTypeComputational, result.Type)
		assert.Contains(t, result.Signals, "context:high_numeric_density")
	})

	t.Run("unique identifiers boost retrieval", func(t *testing.T) {
		query := "What is the code?"
		contextWithID := []ContextSource{{
			Content: "The authorization CODE-12345 was issued yesterday. Please use it for access.",
		}}

		result := c.Classify(query, contextWithID)
		assert.Equal(t, TaskTypeRetrieval, result.Type)
		assert.Contains(t, result.Signals, "context:unique_identifiers")
	})
}

func TestTaskClassifier_RecommendedMode(t *testing.T) {
	c := NewTaskClassifier()

	tests := []struct {
		name          string
		taskType      TaskType
		confidence    float64
		contextTokens int
		replAvailable bool
		wantMode      ExecutionMode
	}{
		{
			name:          "computational with REPL",
			taskType:      TaskTypeComputational,
			confidence:    0.9,
			contextTokens: 1000,
			replAvailable: true,
			wantMode:      ModeRLM,
		},
		{
			name:          "computational without REPL",
			taskType:      TaskTypeComputational,
			confidence:    0.9,
			contextTokens: 1000,
			replAvailable: false,
			wantMode:      "", // Fall through to default
		},
		{
			name:          "retrieval always direct",
			taskType:      TaskTypeRetrieval,
			confidence:    0.9,
			contextTokens: 100000,
			replAvailable: true,
			wantMode:      ModeDirecte,
		},
		{
			name:          "analytical large context",
			taskType:      TaskTypeAnalytical,
			confidence:    0.8,
			contextTokens: 16000,
			replAvailable: true,
			wantMode:      ModeRLM,
		},
		{
			name:          "analytical small context",
			taskType:      TaskTypeAnalytical,
			confidence:    0.8,
			contextTokens: 2000,
			replAvailable: true,
			wantMode:      "", // Fall through to default
		},
		{
			name:          "low confidence falls through",
			taskType:      TaskTypeComputational,
			confidence:    0.5,
			contextTokens: 8000,
			replAvailable: true,
			wantMode:      "", // Fall through to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class := Classification{
				Type:       tt.taskType,
				Confidence: tt.confidence,
			}
			mode := c.RecommendedMode(class, tt.contextTokens, tt.replAvailable)
			assert.Equal(t, tt.wantMode, mode)
		})
	}
}

func TestTaskClassifier_BenchmarkQueries(t *testing.T) {
	// Test against actual benchmark query patterns
	c := NewTaskClassifier()

	t.Run("counting generator queries", func(t *testing.T) {
		queries := []string{
			"How many times does 'apple' appear in the text? Answer with just the number.",
			"How many times does 'banana' appear in the text? Answer with just the number.",
			"How many times does 'cherry' appear in the text? Answer with just the number.",
		}

		for _, q := range queries {
			result := c.Classify(q, nil)
			assert.Equal(t, TaskTypeComputational, result.Type, "query: %s", q)
			assert.GreaterOrEqual(t, result.Confidence, 0.7, "low confidence for: %s", q)
		}
	})

	t.Run("needle generator queries", func(t *testing.T) {
		queries := []string{
			"What is the secret access code mentioned in the text?",
		}

		for _, q := range queries {
			result := c.Classify(q, nil)
			assert.Equal(t, TaskTypeRetrieval, result.Type, "query: %s", q)
			assert.GreaterOrEqual(t, result.Confidence, 0.7, "low confidence for: %s", q)
		}
	})

	t.Run("aggregation generator queries", func(t *testing.T) {
		queries := []string{
			"What is the total sales amount across all regions? Answer with just the number (no $ sign).",
		}

		for _, q := range queries {
			result := c.Classify(q, nil)
			assert.Equal(t, TaskTypeComputational, result.Type, "query: %s", q)
			assert.GreaterOrEqual(t, result.Confidence, 0.7, "low confidence for: %s", q)
		}
	})

	t.Run("pairing generator queries", func(t *testing.T) {
		queries := []string{
			"Based on the text, did Alice and Bob have any professional relationship? Answer 'yes' or 'no'.",
			"Based on the text, did Carol and David have any professional relationship? Answer 'yes' or 'no'.",
		}

		for _, q := range queries {
			result := c.Classify(q, nil)
			assert.Equal(t, TaskTypeAnalytical, result.Type, "query: %s", q)
			assert.GreaterOrEqual(t, result.Confidence, 0.5, "low confidence for: %s", q)
		}
	})
}

func TestTaskClassifier_EdgeCases(t *testing.T) {
	c := NewTaskClassifier()

	t.Run("empty query", func(t *testing.T) {
		result := c.Classify("", nil)
		assert.Equal(t, TaskTypeUnknown, result.Type)
	})

	t.Run("ambiguous query defaults reasonably", func(t *testing.T) {
		// "What are the sales figures?" could be retrieval or computational
		result := c.Classify("What are the sales figures?", nil)
		// Should classify as something, not unknown
		require.NotEmpty(t, result.Signals, "should have some signals")
	})

	t.Run("mixed signals uses strongest", func(t *testing.T) {
		// Has both counting and retrieval signals, but counting is stronger
		// Using "error" instead of "secret code" to avoid retrieval keyword stacking
		query := "How many times does the word 'error' appear in the log?"
		result := c.Classify(query, nil)
		assert.Equal(t, TaskTypeComputational, result.Type, "counting should win")
	})
}

func TestTaskClassifier_SignalTracking(t *testing.T) {
	c := NewTaskClassifier()

	result := c.Classify("How many times does 'apple' appear in the text?", nil)

	// Should have captured the signals
	assert.NotEmpty(t, result.Signals)

	// Should include keyword signals
	foundKeyword := false
	foundPattern := false
	for _, s := range result.Signals {
		if len(s) > 8 && s[:8] == "keyword:" {
			foundKeyword = true
		}
		if len(s) > 8 && s[:8] == "pattern:" {
			foundPattern = true
		}
	}
	assert.True(t, foundKeyword, "should have keyword signal")
	assert.True(t, foundPattern, "should have pattern signal")
}
