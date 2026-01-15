package compress

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHierarchicalCompressor(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		h := NewHierarchicalCompressor(DefaultHierarchicalConfig())
		assert.NotNil(t, h)
		assert.Len(t, h.levels, 3)
		assert.Equal(t, []float64{0.5, 0.25, 0.125}, h.levels)
	})

	t.Run("custom levels", func(t *testing.T) {
		cfg := HierarchicalConfig{
			Levels: []float64{0.7, 0.4, 0.2, 0.1},
		}
		h := NewHierarchicalCompressor(cfg)
		assert.Len(t, h.levels, 4)
		assert.Equal(t, []float64{0.7, 0.4, 0.2, 0.1}, h.levels)
	})

	t.Run("empty levels uses defaults", func(t *testing.T) {
		cfg := HierarchicalConfig{
			Levels: []float64{},
		}
		h := NewHierarchicalCompressor(cfg)
		assert.Len(t, h.levels, 3)
	})
}

func TestHierarchicalCompressor_Compress(t *testing.T) {
	h := NewHierarchicalCompressor(DefaultHierarchicalConfig())

	t.Run("creates multiple levels", func(t *testing.T) {
		// Generate substantial content
		content := generateTestContent(50)

		result, err := h.Compress(context.Background(), content, DefaultOptions())
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Greater(t, result.OriginalTokens, 0)
		assert.NotEmpty(t, result.Levels)
		assert.Greater(t, result.Duration, 0*result.Duration) // Duration > 0

		// Each level should be progressively smaller
		prevTokens := result.OriginalTokens
		for _, level := range result.Levels {
			assert.LessOrEqual(t, level.TokenCount, prevTokens,
				"level %d should be smaller than previous", level.Level)
			assert.Greater(t, level.Ratio, 0.0)
			assert.LessOrEqual(t, level.Ratio, 1.0)
			prevTokens = level.TokenCount
		}
	})

	t.Run("empty content", func(t *testing.T) {
		result, err := h.Compress(context.Background(), "", DefaultOptions())
		require.NoError(t, err)
		assert.Equal(t, 0, result.OriginalTokens)
		assert.Empty(t, result.Levels)
	})

	t.Run("small content may stop early", func(t *testing.T) {
		// Very small content might reach passthrough quickly
		content := "This is a short test sentence."

		result, err := h.Compress(context.Background(), content, DefaultOptions())
		require.NoError(t, err)

		// Should have at least one level
		assert.NotEmpty(t, result.Levels)
	})

	t.Run("levels have correct indices", func(t *testing.T) {
		content := generateTestContent(40)

		result, err := h.Compress(context.Background(), content, DefaultOptions())
		require.NoError(t, err)

		for i, level := range result.Levels {
			assert.Equal(t, i, level.Level)
		}
	})
}

func TestHierarchicalResult_SelectLevel(t *testing.T) {
	result := &HierarchicalResult{
		OriginalTokens: 1000,
		Levels: []*LevelResult{
			{Level: 0, TokenCount: 500, Ratio: 0.5},
			{Level: 1, TokenCount: 250, Ratio: 0.25},
			{Level: 2, TokenCount: 125, Ratio: 0.125},
		},
	}

	t.Run("select first level that fits", func(t *testing.T) {
		level := result.SelectLevel(600)
		assert.Equal(t, 0, level.Level)
		assert.Equal(t, 500, level.TokenCount)
	})

	t.Run("select middle level", func(t *testing.T) {
		level := result.SelectLevel(300)
		assert.Equal(t, 1, level.Level)
	})

	t.Run("select most compressed when tight", func(t *testing.T) {
		level := result.SelectLevel(130)
		assert.Equal(t, 2, level.Level)
	})

	t.Run("returns most compressed when none fit", func(t *testing.T) {
		level := result.SelectLevel(50)
		assert.Equal(t, 2, level.Level)
	})

	t.Run("empty levels returns nil", func(t *testing.T) {
		empty := &HierarchicalResult{}
		assert.Nil(t, empty.SelectLevel(1000))
	})
}

func TestHierarchicalResult_SelectLevelByRatio(t *testing.T) {
	result := &HierarchicalResult{
		OriginalTokens: 1000,
		Levels: []*LevelResult{
			{Level: 0, TokenCount: 500, Ratio: 0.5},
			{Level: 1, TokenCount: 250, Ratio: 0.25},
			{Level: 2, TokenCount: 125, Ratio: 0.125},
		},
	}

	t.Run("exact match", func(t *testing.T) {
		level := result.SelectLevelByRatio(0.25)
		assert.Equal(t, 1, level.Level)
	})

	t.Run("closest to target", func(t *testing.T) {
		level := result.SelectLevelByRatio(0.3)
		assert.Equal(t, 1, level.Level) // 0.25 is closer than 0.5
	})

	t.Run("very low ratio", func(t *testing.T) {
		level := result.SelectLevelByRatio(0.1)
		assert.Equal(t, 2, level.Level) // 0.125 is closest
	})

	t.Run("high ratio prefers first level", func(t *testing.T) {
		level := result.SelectLevelByRatio(0.6)
		assert.Equal(t, 0, level.Level) // 0.5 is closest
	})
}

func TestHierarchicalResult_BestLevel(t *testing.T) {
	result := &HierarchicalResult{
		OriginalTokens: 1000,
		Levels: []*LevelResult{
			{Level: 0, TokenCount: 500, Ratio: 0.5},
			{Level: 1, TokenCount: 250, Ratio: 0.25},
			{Level: 2, TokenCount: 125, Ratio: 0.125},
		},
	}

	t.Run("uses budget efficiently", func(t *testing.T) {
		// Budget of 550 - level 0 (500) uses it better than level 1 (250)
		level := result.BestLevel(550)
		assert.Equal(t, 0, level.Level)
	})

	t.Run("exact fit prefers larger", func(t *testing.T) {
		level := result.BestLevel(500)
		assert.Equal(t, 0, level.Level)
	})

	t.Run("falls back to most compressed", func(t *testing.T) {
		level := result.BestLevel(50)
		assert.Equal(t, 2, level.Level)
	})
}

func TestHierarchicalCompressor_Metrics(t *testing.T) {
	h := NewHierarchicalCompressor(DefaultHierarchicalConfig())

	// Perform some compressions
	content := generateTestContent(30)
	for i := 0; i < 3; i++ {
		_, err := h.Compress(context.Background(), content, DefaultOptions())
		require.NoError(t, err)
	}

	metrics := h.Metrics()
	assert.Equal(t, int64(3), metrics.TotalCompressions)
	assert.Greater(t, metrics.AvgLevelsGenerated, 0.0)
}

func TestHierarchicalCompressor_RecordLevelSelection(t *testing.T) {
	h := NewHierarchicalCompressor(DefaultHierarchicalConfig())

	h.RecordLevelSelection(0)
	h.RecordLevelSelection(0)
	h.RecordLevelSelection(1)

	metrics := h.Metrics()
	assert.Equal(t, int64(2), metrics.LevelSelections[0])
	assert.Equal(t, int64(1), metrics.LevelSelections[1])
}

// generateTestContent creates test content with the given number of sentences.
func generateTestContent(numSentences int) string {
	sentences := make([]string, numSentences)
	words := []string{
		"The", "quick", "brown", "fox", "jumps", "over", "lazy", "dog",
		"while", "the", "cat", "watches", "from", "above", "with", "great",
		"interest", "and", "curiosity", "wondering", "what", "will", "happen",
		"next", "in", "this", "exciting", "story", "about", "animals",
	}

	for i := 0; i < numSentences; i++ {
		// Create sentence with 5-15 words
		numWords := 5 + (i % 11)
		sentWords := make([]string, numWords)
		for j := 0; j < numWords; j++ {
			sentWords[j] = words[(i*7+j)%len(words)]
		}
		// Capitalize first word
		if len(sentWords[0]) > 0 {
			sentWords[0] = strings.ToUpper(sentWords[0][:1]) + sentWords[0][1:]
		}
		sentences[i] = strings.Join(sentWords, " ") + "."
	}

	return strings.Join(sentences, " ")
}
