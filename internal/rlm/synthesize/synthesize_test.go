package synthesize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcatenateSynthesizer_Basic(t *testing.T) {
	s := NewConcatenateSynthesizer()
	ctx := context.Background()

	results := []SubCallResult{
		{ID: "1", Name: "Part 1", Response: "First result", TokensUsed: 10},
		{ID: "2", Name: "Part 2", Response: "Second result", TokensUsed: 15},
	}

	out, err := s.Synthesize(ctx, "test task", results)
	require.NoError(t, err)

	// Default synthesizer doesn't include headers for cleaner output
	assert.Contains(t, out.Response, "First result")
	assert.Contains(t, out.Response, "Second result")
	assert.Equal(t, 25, out.TotalTokensUsed)
	assert.Equal(t, 2, out.PartCount)
}

func TestConcatenateSynthesizer_WithHeaders(t *testing.T) {
	s := &ConcatenateSynthesizer{
		Separator:      "\n\n---\n\n",
		IncludeHeaders: true,
	}
	ctx := context.Background()

	results := []SubCallResult{
		{ID: "1", Name: "Part 1", Response: "First result", TokensUsed: 10},
		{ID: "2", Name: "Part 2", Response: "Second result", TokensUsed: 15},
	}

	out, err := s.Synthesize(ctx, "test task", results)
	require.NoError(t, err)

	assert.Contains(t, out.Response, "## Part 1")
	assert.Contains(t, out.Response, "First result")
	assert.Contains(t, out.Response, "## Part 2")
	assert.Contains(t, out.Response, "Second result")
	assert.Equal(t, 25, out.TotalTokensUsed)
	assert.Equal(t, 2, out.PartCount)
}

func TestConcatenateSynthesizer_SkipsErrors(t *testing.T) {
	s := NewConcatenateSynthesizer()
	ctx := context.Background()

	results := []SubCallResult{
		{ID: "1", Name: "Part 1", Response: "Good result", TokensUsed: 10},
		{ID: "2", Name: "Part 2", Response: "", Error: "failed", TokensUsed: 0},
		{ID: "3", Name: "Part 3", Response: "Another good result", TokensUsed: 20},
	}

	out, err := s.Synthesize(ctx, "test task", results)
	require.NoError(t, err)

	assert.Contains(t, out.Response, "Good result")
	assert.Contains(t, out.Response, "Another good result")
	assert.NotContains(t, out.Response, "Part 2")
	assert.Equal(t, 2, out.PartCount)
	assert.Equal(t, 30, out.TotalTokensUsed)
}

func TestConcatenateSynthesizer_Empty(t *testing.T) {
	s := NewConcatenateSynthesizer()
	ctx := context.Background()

	out, err := s.Synthesize(ctx, "test task", []SubCallResult{})
	require.NoError(t, err)

	assert.Contains(t, out.Response, "no results")
	assert.Equal(t, 0, out.PartCount)
}

func TestConcatenateSynthesizer_NoHeaders(t *testing.T) {
	s := &ConcatenateSynthesizer{
		Separator:      "\n\n",
		IncludeHeaders: false,
	}
	ctx := context.Background()

	results := []SubCallResult{
		{ID: "1", Name: "Part 1", Response: "First", TokensUsed: 5},
		{ID: "2", Name: "Part 2", Response: "Second", TokensUsed: 5},
	}

	out, err := s.Synthesize(ctx, "test task", results)
	require.NoError(t, err)

	assert.NotContains(t, out.Response, "## Part 1")
	assert.Contains(t, out.Response, "First")
	assert.Contains(t, out.Response, "Second")
}

func TestMergeSynthesizer_Basic(t *testing.T) {
	s := NewMergeSynthesizer(10000)
	ctx := context.Background()

	results := []SubCallResult{
		{ID: "1", Name: "File 1", Response: "Content from file 1", TokensUsed: 10},
		{ID: "2", Name: "File 2", Response: "Content from file 2", TokensUsed: 10},
	}

	out, err := s.Synthesize(ctx, "test task", results)
	require.NoError(t, err)

	assert.Contains(t, out.Response, "Content from file 1")
	assert.Contains(t, out.Response, "Content from file 2")
	assert.Equal(t, 2, out.PartCount)
}

func TestMergeSynthesizer_WithSections(t *testing.T) {
	s := NewMergeSynthesizer(10000)
	ctx := context.Background()

	results := []SubCallResult{
		{
			ID:         "1",
			Name:       "Analysis 1",
			Response:   "## Summary\nFirst summary\n\n## Details\nFirst details",
			TokensUsed: 20,
		},
		{
			ID:         "2",
			Name:       "Analysis 2",
			Response:   "## Summary\nSecond summary\n\n## Details\nSecond details",
			TokensUsed: 20,
		},
	}

	out, err := s.Synthesize(ctx, "test task", results)
	require.NoError(t, err)

	assert.Contains(t, out.Response, "Summary")
	assert.Contains(t, out.Response, "Details")
	assert.Equal(t, 40, out.TotalTokensUsed)
}

func TestMergeSynthesizer_MaxLength(t *testing.T) {
	s := NewMergeSynthesizer(50)
	ctx := context.Background()

	results := []SubCallResult{
		{ID: "1", Name: "Long", Response: "This is a very long response that exceeds the limit", TokensUsed: 10},
	}

	out, err := s.Synthesize(ctx, "test task", results)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(out.Response), 60) // Some tolerance for formatting
}

func TestMergeSynthesizer_Empty(t *testing.T) {
	s := NewMergeSynthesizer(10000)
	ctx := context.Background()

	out, err := s.Synthesize(ctx, "test task", []SubCallResult{})
	require.NoError(t, err)

	assert.Contains(t, out.Response, "no results")
}

func TestAuto_SmallResults(t *testing.T) {
	results := []SubCallResult{
		{ID: "1", Response: "Short"},
	}

	s := Auto(results)
	_, ok := s.(*ConcatenateSynthesizer)
	assert.True(t, ok, "should use concatenate for small results")
}

func TestAuto_ManyResults(t *testing.T) {
	results := []SubCallResult{
		{ID: "1", Response: "Result 1"},
		{ID: "2", Response: "Result 2"},
		{ID: "3", Response: "Result 3"},
	}

	s := Auto(results)
	_, ok := s.(*ConcatenateSynthesizer)
	assert.True(t, ok, "should use concatenate for moderate results")
}

func TestAuto_LargeResults(t *testing.T) {
	// Create large results
	largeContent := make([]byte, 10000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}

	results := []SubCallResult{
		{ID: "1", Response: string(largeContent)},
		{ID: "2", Response: string(largeContent)},
		{ID: "3", Response: string(largeContent)},
	}

	s := Auto(results)
	_, ok := s.(*MergeSynthesizer)
	assert.True(t, ok, "should use merge for large results")
}

func TestSubCallResult_Fields(t *testing.T) {
	r := SubCallResult{
		ID:         "test-id",
		Name:       "Test Result",
		Response:   "Some response",
		TokensUsed: 100,
		Error:      "",
	}

	assert.Equal(t, "test-id", r.ID)
	assert.Equal(t, "Test Result", r.Name)
	assert.Equal(t, "Some response", r.Response)
	assert.Equal(t, 100, r.TokensUsed)
	assert.Empty(t, r.Error)
}

func TestSynthesisResult_Fields(t *testing.T) {
	r := SynthesisResult{
		Response:        "Combined response",
		TotalTokensUsed: 250,
		PartCount:       3,
	}

	assert.Equal(t, "Combined response", r.Response)
	assert.Equal(t, 250, r.TotalTokensUsed)
	assert.Equal(t, 3, r.PartCount)
}
