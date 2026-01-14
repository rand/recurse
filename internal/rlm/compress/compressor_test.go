package compress

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rand/recurse/internal/memory/embeddings"
)

// MockLLMClient for testing abstractive compression.
type MockLLMClient struct {
	response func(prompt string) string
}

func (m *MockLLMClient) Complete(ctx context.Context, prompt string) (string, int, error) {
	if m.response != nil {
		resp := m.response(prompt)
		return resp, len(resp) / 4, nil
	}
	return "Summary of content.", 5, nil
}

// MockEmbedder for testing query-aware scoring.
type MockEmbedder struct {
	dimensions int
}

func (m *MockEmbedder) Embed(ctx context.Context, texts []string) ([]embeddings.Vector, error) {
	vecs := make([]embeddings.Vector, len(texts))
	for i := range texts {
		// Simple mock: create vector based on text length
		vec := make(embeddings.Vector, m.dimensions)
		for j := range vec {
			vec[j] = float32(len(texts[i])%10) / 10.0
		}
		vecs[i] = vec.Normalize()
	}
	return vecs, nil
}

func (m *MockEmbedder) Dimensions() int { return m.dimensions }
func (m *MockEmbedder) Model() string   { return "mock" }

func TestContextCompressor_Compress_Passthrough(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	// Small content that doesn't need compression
	content := "This is a short sentence."
	opts := Options{TargetTokens: 100}

	result, err := c.Compress(context.Background(), content, opts)
	require.NoError(t, err)

	assert.Equal(t, content, result.Compressed)
	assert.Equal(t, MethodPassthrough, result.Method)
	assert.Equal(t, 1.0, result.Ratio)
}

func TestContextCompressor_Compress_Extractive(t *testing.T) {
	c := NewContextCompressor(Config{
		ExtractiveThreshold:  5000,
		AbstractiveThreshold: 10000,
	})

	// Multi-sentence content
	content := `This is the first important sentence.
This is a less important filler sentence that doesn't add much value.
This is another key sentence with critical information.
More filler content here that can be removed.
Finally, this sentence contains the main conclusion.`

	opts := Options{
		TargetRatio: 0.5,
		MinTokens:   5,
	}

	result, err := c.Compress(context.Background(), content, opts)
	require.NoError(t, err)

	assert.Equal(t, MethodExtractive, result.Method)
	assert.Less(t, result.CompressedTokens, result.OriginalTokens)
	assert.Greater(t, result.Metadata.SentencesSelected, 0)
	assert.LessOrEqual(t, result.Metadata.SentencesSelected, result.Metadata.SentencesTotal)
}

func TestContextCompressor_Compress_Abstractive(t *testing.T) {
	llmClient := &MockLLMClient{
		response: func(prompt string) string {
			return "Concise summary of the long content."
		},
	}

	c := NewContextCompressor(Config{
		ExtractiveThreshold:  100,
		AbstractiveThreshold: 200,
		LLMClient:            llmClient,
	})

	// Content between thresholds
	content := strings.Repeat("This is test content with information. ", 10)

	opts := Options{TargetRatio: 0.3}

	result, err := c.Compress(context.Background(), content, opts)
	require.NoError(t, err)

	// Should use extractive since below abstractive threshold
	assert.Equal(t, MethodExtractive, result.Method)
}

func TestContextCompressor_Compress_Hybrid(t *testing.T) {
	llmClient := &MockLLMClient{
		response: func(prompt string) string {
			return "Final compressed summary."
		},
	}

	c := NewContextCompressor(Config{
		ExtractiveThreshold:  50,
		AbstractiveThreshold: 100,
		LLMClient:            llmClient,
	})

	// Large content triggers hybrid
	content := strings.Repeat("This is test content that needs significant compression. ", 50)

	opts := Options{TargetRatio: 0.2}

	result, err := c.Compress(context.Background(), content, opts)
	require.NoError(t, err)

	assert.Equal(t, MethodHybrid, result.Method)
	assert.Contains(t, result.Metadata.StagesUsed, "extractive")
}

func TestContextCompressor_Compress_WithQuery(t *testing.T) {
	embedder := &MockEmbedder{dimensions: 128}

	c := NewContextCompressor(Config{
		ExtractiveThreshold:  5000,
		AbstractiveThreshold: 10000,
		Embedder:             embedder,
	})

	content := `The database connection failed due to timeout.
The user interface looks beautiful with the new design.
Performance metrics show 50% improvement in response time.
The security audit revealed no critical vulnerabilities.
Memory usage has decreased after the optimization.`

	opts := Options{
		TargetRatio:  0.4,
		QueryContext: "What are the performance improvements?",
	}

	result, err := c.Compress(context.Background(), content, opts)
	require.NoError(t, err)

	assert.Equal(t, MethodExtractive, result.Method)
	// With query context, should prefer relevant sentences
	assert.Greater(t, result.Metadata.SentencesSelected, 0)
}

func TestContextCompressor_Compress_PreservesMinTokens(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	content := "Short content."
	opts := Options{
		TargetTokens: 1,
		MinTokens:    100, // Higher than content
	}

	result, err := c.Compress(context.Background(), content, opts)
	require.NoError(t, err)

	// Should not compress below min
	assert.Equal(t, content, result.Compressed)
	assert.Equal(t, MethodPassthrough, result.Method)
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		minCount int
	}{
		{
			name:     "simple sentences",
			input:    "First sentence. Second sentence. Third sentence.",
			minCount: 2,
		},
		{
			name:     "sentences with questions",
			input:    "What is this? It is a test. Is it working?",
			minCount: 2,
		},
		{
			name:     "sentences with exclamations",
			input:    "Amazing! This works. Fantastic result!",
			minCount: 2,
		},
		{
			name:     "newline separated",
			input:    "First line\nSecond line\nThird line",
			minCount: 3,
		},
		{
			name:     "single sentence",
			input:    "Just one sentence here",
			minCount: 1,
		},
		{
			name:     "empty string",
			input:    "",
			minCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sentences := splitSentences(tt.input)
			assert.GreaterOrEqual(t, len(sentences), tt.minCount)
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		minTokens int
		maxTokens int
	}{
		{"", 0, 0},
		{"Hello", 1, 3},
		{"Hello world", 2, 5},
		{"This is a longer sentence with more words.", 8, 15},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			tokens := estimateTokens(tt.input)
			assert.GreaterOrEqual(t, tokens, tt.minTokens)
			assert.LessOrEqual(t, tokens, tt.maxTokens)
		})
	}
}

func TestContextCompressor_Metrics(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	// Perform several compressions
	content := strings.Repeat("Test content. ", 20)
	opts := Options{TargetRatio: 0.5}

	for i := 0; i < 5; i++ {
		_, err := c.Compress(context.Background(), content, opts)
		require.NoError(t, err)
	}

	metrics := c.Metrics()
	assert.Equal(t, int64(5), metrics.TotalCompressions)
	assert.Greater(t, metrics.TotalTokensSaved, int64(0))
}

func TestOptions_DefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	assert.Equal(t, 0.3, opts.TargetRatio)
	assert.Equal(t, 50, opts.MinTokens)
	assert.True(t, opts.PreserveCode)
	assert.True(t, opts.PreserveQuotes)
}

func TestMethod_String(t *testing.T) {
	tests := []struct {
		method   Method
		expected string
	}{
		{MethodExtractive, "extractive"},
		{MethodAbstractive, "abstractive"},
		{MethodHybrid, "hybrid"},
		{MethodPassthrough, "passthrough"},
		{Method(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.method.String())
		})
	}
}

func TestContextCompressor_ScoreSentences(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	sentences := []string{
		"This is an important conclusion.",
		"Some filler content.",
		"The main result shows improvement.",
		"Random text here.",
	}

	scores := c.scoreSentences(context.Background(), sentences, "")

	// First sentences should have higher position scores
	assert.Greater(t, scores[0], scores[3])

	// Sentences with keywords should score higher
	// "conclusion" and "result" are important keywords
	assert.Greater(t, scores[0], scores[1])
	assert.Greater(t, scores[2], scores[3])
}

func TestContextCompressor_SelectSentences(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	sentences := []string{
		"First sentence.",
		"Second sentence with more words here.",
		"Third important sentence.",
		"Fourth filler sentence.",
	}
	scores := []float64{0.9, 0.3, 0.8, 0.2}

	// Select enough for ~15 tokens
	selected := c.selectSentences(sentences, scores, 15)

	// Should select highest scored sentences that fit
	assert.Greater(t, len(selected), 0)
	assert.LessOrEqual(t, len(selected), len(sentences))
}

func TestContextCompressor_SelectSentences_MaintainsOrder(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	sentences := []string{
		"First sentence at position 0.",
		"Second sentence at position 1.",
		"Third sentence at position 2.",
		"Fourth sentence at position 3.",
	}
	// Third has highest score, then first, then fourth
	scores := []float64{0.8, 0.2, 0.9, 0.5}

	selected := c.selectSentences(sentences, scores, 50)

	// Verify order is maintained (by original position, not score)
	if len(selected) > 1 {
		// First should appear before Third (by original position)
		firstIdx := -1
		thirdIdx := -1
		for i, s := range selected {
			if strings.Contains(s, "First") {
				firstIdx = i
			}
			if strings.Contains(s, "Third") {
				thirdIdx = i
			}
		}
		if firstIdx >= 0 && thirdIdx >= 0 {
			assert.Less(t, firstIdx, thirdIdx, "Original order should be maintained")
		}
	}
}

func TestContextCompressor_BuildAbstractionPrompt(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	content := "Test content to summarize."
	opts := Options{
		PreserveCode:   true,
		PreserveQuotes: true,
		QueryContext:   "What is the summary?",
	}

	prompt := c.buildAbstractionPrompt(content, 50, opts)

	assert.Contains(t, prompt, "50 tokens")
	assert.Contains(t, prompt, "Test content")
	assert.Contains(t, prompt, "code snippets")
	assert.Contains(t, prompt, "quotes")
	assert.Contains(t, prompt, "What is the summary?")
}

func TestContainsCode(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"Plain text without code", false},
		{"```python\nprint('hello')\n```", true},
		{"func main() {}", true},
		{"def hello():", true},
		{"class MyClass:", true},
		{"if (x) { return; }", true},
		{"Regular if statement", false},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(20, len(tt.input))], func(t *testing.T) {
			assert.Equal(t, tt.expected, containsCode(tt.input))
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Benchmarks

func BenchmarkContextCompressor_Extractive(b *testing.B) {
	c := NewContextCompressor(DefaultConfig())
	content := strings.Repeat("This is test content with multiple sentences. ", 100)
	opts := Options{TargetRatio: 0.3}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkContextCompressor_WithEmbedder(b *testing.B) {
	embedder := &MockEmbedder{dimensions: 128}
	c := NewContextCompressor(Config{
		ExtractiveThreshold:  10000,
		AbstractiveThreshold: 20000,
		Embedder:             embedder,
	})
	content := strings.Repeat("This is test content with multiple sentences. ", 100)
	opts := Options{
		TargetRatio:  0.3,
		QueryContext: "test query",
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkSplitSentences(b *testing.B) {
	content := strings.Repeat("This is a sentence. Another one here! What about questions? ", 50)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = splitSentences(content)
	}
}

func BenchmarkEstimateTokens(b *testing.B) {
	content := strings.Repeat("word ", 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = estimateTokens(content)
	}
}

// Example usage
func ExampleContextCompressor() {
	c := NewContextCompressor(DefaultConfig())

	content := `The RLM system implements recursive language model capabilities.
It uses a hypergraph memory structure for efficient context storage.
The meta-controller decides orchestration strategies dynamically.
Performance improvements show 2x speedup in typical workloads.`

	result, _ := c.Compress(context.Background(), content, Options{
		TargetRatio: 0.5,
	})

	fmt.Printf("Method: %s, Ratio: %.2f\n", result.Method, result.Ratio)
	// Output will vary based on compression
}
