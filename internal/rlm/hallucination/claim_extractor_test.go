package hallucination

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimExtractor_Extract(t *testing.T) {
	extractor := NewClaimExtractor()

	t.Run("basic sentence extraction", func(t *testing.T) {
		text := "The sky is blue. Water is wet. Fire is hot."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 3)
		assert.Equal(t, "The sky is blue.", claims[0].Content)
		assert.Equal(t, "Water is wet.", claims[1].Content)
		assert.Equal(t, "Fire is hot.", claims[2].Content)

		// All should be assertive
		for _, c := range claims {
			assert.True(t, c.IsAssertive, "claim should be assertive: %s", c.Content)
			assert.Equal(t, "test", c.Source)
		}
	})

	t.Run("question detection", func(t *testing.T) {
		text := "What is the capital of France? Paris is the capital of France."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 2)
		assert.False(t, claims[0].IsAssertive, "question should not be assertive")
		assert.True(t, claims[1].IsAssertive, "statement should be assertive")
	})

	t.Run("imperative detection", func(t *testing.T) {
		text := "Please run the tests. The tests should pass. Make sure to check the output."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 3)
		assert.False(t, claims[0].IsAssertive, "imperative should not be assertive")
		assert.True(t, claims[1].IsAssertive, "statement should be assertive")
		assert.False(t, claims[2].IsAssertive, "imperative should not be assertive")
	})

	t.Run("meta-commentary detection", func(t *testing.T) {
		text := "I'll explain this concept. The concept is straightforward. Here's an example."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 3)
		assert.False(t, claims[0].IsAssertive, "meta-commentary should not be assertive")
		assert.True(t, claims[1].IsAssertive, "statement should be assertive")
		assert.False(t, claims[2].IsAssertive, "meta-commentary should not be assertive")
	})

	t.Run("short claims filtered", func(t *testing.T) {
		text := "OK. Yes. The quick brown fox jumps over the lazy dog."
		claims := extractor.Extract(text, "test")

		// Only the long sentence should be extracted
		require.Len(t, claims, 1)
		assert.Contains(t, claims[0].Content, "quick brown fox")
	})
}

func TestClaimExtractor_ExtractAssertive(t *testing.T) {
	extractor := NewClaimExtractor()

	text := `What is Go? Go is a programming language. Please see the documentation.
It was developed by Google. I'll show you an example. The syntax is simple.`

	claims := extractor.ExtractAssertive(text, "test")

	// Should only include assertive statements
	require.Len(t, claims, 3)
	assert.Equal(t, "Go is a programming language.", claims[0].Content)
	assert.Equal(t, "It was developed by Google.", claims[1].Content)
	assert.Equal(t, "The syntax is simple.", claims[2].Content)
}

func TestClaimExtractor_Citations(t *testing.T) {
	extractor := NewClaimExtractor()

	t.Run("numeric citations", func(t *testing.T) {
		text := "The Earth is round [1]. The sun is a star [2]."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 2)

		require.Len(t, claims[0].Citations, 1)
		assert.Equal(t, "1", claims[0].Citations[0].SpanID)

		require.Len(t, claims[1].Citations, 1)
		assert.Equal(t, "2", claims[1].Citations[0].SpanID)
	})

	t.Run("named citations", func(t *testing.T) {
		text := "According to the study [Smith2020], this is true."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 1)
		require.Len(t, claims[0].Citations, 1)
		assert.Equal(t, "Smith2020", claims[0].Citations[0].SpanID)
	})

	t.Run("multiple citations in one claim", func(t *testing.T) {
		text := "This fact is well documented [1][2][source-a]."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 1)
		require.Len(t, claims[0].Citations, 3)
		assert.Equal(t, "1", claims[0].Citations[0].SpanID)
		assert.Equal(t, "2", claims[0].Citations[1].SpanID)
		assert.Equal(t, "source-a", claims[0].Citations[2].SpanID)
	})

	t.Run("non-citation brackets ignored", func(t *testing.T) {
		text := "This needs work [TODO]. The data is correct [sic]."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 2)
		assert.Empty(t, claims[0].Citations, "TODO should not be a citation")
		assert.Empty(t, claims[1].Citations, "sic should not be a citation")
	})
}

func TestClaimExtractor_Confidence(t *testing.T) {
	extractor := NewClaimExtractor()

	t.Run("default confidence", func(t *testing.T) {
		text := "The Earth orbits the Sun."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 1)
		assert.Equal(t, 0.9, claims[0].Confidence)
	})

	t.Run("hedged statements lower confidence", func(t *testing.T) {
		hedged := []string{
			"I think this might be correct.",
			"Perhaps this is the answer.",
			"It seems like the right approach.",
			"Maybe we should try this.",
		}

		for _, text := range hedged {
			claims := extractor.Extract(text, "test")
			require.Len(t, claims, 1, "text: %s", text)
			assert.Equal(t, 0.6, claims[0].Confidence,
				"hedged statement should have lower confidence: %s", text)
		}
	})

	t.Run("strong certainty higher confidence", func(t *testing.T) {
		certain := []string{
			"This is definitely the correct answer.",
			"The result is certainly accurate.",
			"This must be the solution.",
		}

		for _, text := range certain {
			claims := extractor.Extract(text, "test")
			require.Len(t, claims, 1, "text: %s", text)
			assert.Equal(t, 0.95, claims[0].Confidence,
				"certain statement should have higher confidence: %s", text)
		}
	})

	t.Run("moderate certainty", func(t *testing.T) {
		moderate := []string{
			"This typically works well.",
			"The function usually returns true.",
			"Errors are generally handled gracefully.",
		}

		for _, text := range moderate {
			claims := extractor.Extract(text, "test")
			require.Len(t, claims, 1, "text: %s", text)
			assert.Equal(t, 0.8, claims[0].Confidence,
				"moderate statement should have 0.8 confidence: %s", text)
		}
	})
}

func TestClaimExtractor_ResolveCitations(t *testing.T) {
	extractor := NewClaimExtractor()

	text := "The Earth is round [1]. It orbits the Sun [2]."
	claims := extractor.Extract(text, "test")

	evidenceMap := map[string]string{
		"1": "Scientific consensus from NASA and other space agencies.",
		"2": "Kepler's laws of planetary motion demonstrate this.",
	}

	resolved := extractor.ResolveCitations(claims, evidenceMap)

	require.Len(t, resolved, 2)
	assert.Equal(t, "Scientific consensus from NASA and other space agencies.", resolved[0].Evidence)
	assert.Equal(t, "Kepler's laws of planetary motion demonstrate this.", resolved[1].Evidence)

	// Citation text should be populated
	assert.Equal(t, "Scientific consensus from NASA and other space agencies.",
		resolved[0].Claim.Citations[0].Text)
}

func TestClaimExtractor_StripCitations(t *testing.T) {
	extractor := NewClaimExtractor()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "The Earth is round [1].",
			expected: "The Earth is round .",
		},
		{
			input:    "Multiple [1] citations [2] here [3].",
			expected: "Multiple  citations  here .",
		},
		{
			input:    "No citations here.",
			expected: "No citations here.",
		},
	}

	for _, tt := range tests {
		result := extractor.StripCitations(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

func TestClaimExtractor_SentenceSplitting(t *testing.T) {
	extractor := NewClaimExtractor()

	t.Run("handles abbreviations", func(t *testing.T) {
		// Note: simple splitter may over-split on abbreviations
		// This tests current behavior, which may need improvement
		text := "Dr. Smith works at NASA. He is a scientist."
		claims := extractor.Extract(text, "test")

		// Current implementation may split on "Dr." - document this behavior
		// For production, consider using a proper NLP sentence tokenizer
		assert.GreaterOrEqual(t, len(claims), 1)
	})

	t.Run("handles newlines", func(t *testing.T) {
		text := "First sentence.\nSecond sentence.\nThird sentence."
		claims := extractor.Extract(text, "test")

		require.Len(t, claims, 3)
	})

	t.Run("handles multiple punctuation", func(t *testing.T) {
		text := "Really?! Yes, really! That's amazing."
		claims := extractor.Extract(text, "test")

		// Questions/exclamations should be split
		assert.GreaterOrEqual(t, len(claims), 2)
	})
}

func TestClaimExtractor_Position(t *testing.T) {
	extractor := NewClaimExtractor()

	text := "First sentence. Second sentence. Third sentence."
	claims := extractor.Extract(text, "test")

	require.Len(t, claims, 3)

	// Positions should be increasing
	assert.Equal(t, 0, claims[0].Position)
	assert.Greater(t, claims[1].Position, claims[0].Position)
	assert.Greater(t, claims[2].Position, claims[1].Position)
}

func TestClaimExtractor_EdgeCases(t *testing.T) {
	extractor := NewClaimExtractor()

	t.Run("empty text", func(t *testing.T) {
		claims := extractor.Extract("", "test")
		assert.Empty(t, claims)
	})

	t.Run("whitespace only", func(t *testing.T) {
		claims := extractor.Extract("   \n\t  ", "test")
		assert.Empty(t, claims)
	})

	t.Run("single sentence no period", func(t *testing.T) {
		claims := extractor.Extract("A sentence without punctuation", "test")
		require.Len(t, claims, 1)
		assert.Equal(t, "A sentence without punctuation", claims[0].Content)
	})

	t.Run("unicode text", func(t *testing.T) {
		text := "日本語のテキストです。これは二番目の文です。"
		claims := extractor.Extract(text, "test")
		assert.GreaterOrEqual(t, len(claims), 1)
	})
}

// TestRealWorldExamples tests with realistic LLM output patterns.
func TestRealWorldExamples(t *testing.T) {
	extractor := NewClaimExtractor()

	t.Run("technical explanation", func(t *testing.T) {
		text := `Go is a statically typed, compiled programming language [1].
It was designed at Google by Robert Griesemer, Ken Thompson, and Rob Pike [2].
The language is known for its simplicity and efficiency.
Would you like me to explain more about its features?
Let me show you an example of Go code.`

		claims := extractor.ExtractAssertive(text, "response")

		// Should extract the factual statements, not the question or meta-commentary
		assert.GreaterOrEqual(t, len(claims), 2)

		// First claim should have citation
		foundCitedClaim := false
		for _, c := range claims {
			if len(c.Citations) > 0 {
				foundCitedClaim = true
				break
			}
		}
		assert.True(t, foundCitedClaim, "should find at least one cited claim")
	})

	t.Run("mixed confidence levels", func(t *testing.T) {
		text := `The function definitely returns an error on invalid input.
It might also log the error depending on configuration.
Users typically call this function at startup.
I think this is the correct approach.`

		claims := extractor.Extract(text, "response")
		require.Len(t, claims, 4)

		// Check confidence levels match patterns
		assert.Equal(t, 0.95, claims[0].Confidence) // "definitely"
		assert.Equal(t, 0.6, claims[1].Confidence)  // "might"
		assert.Equal(t, 0.8, claims[2].Confidence)  // "typically"
		assert.Equal(t, 0.6, claims[3].Confidence)  // "I think"
	})
}
