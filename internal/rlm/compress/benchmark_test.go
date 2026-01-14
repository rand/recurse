package compress

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Comprehensive benchmarks for context compression.

// BenchmarkCompress_ByContentSize benchmarks compression with varying content sizes.
func BenchmarkCompress_ByContentSize(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("words-%d", size), func(b *testing.B) {
			c := NewContextCompressor(DefaultConfig())
			content := generateContent(size)
			opts := Options{TargetRatio: 0.3}
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = c.Compress(ctx, content, opts)
			}
		})
	}
}

// BenchmarkCompress_ByTargetRatio benchmarks compression with varying target ratios.
func BenchmarkCompress_ByTargetRatio(b *testing.B) {
	ratios := []float64{0.1, 0.3, 0.5, 0.7}
	content := generateContent(1000)

	for _, ratio := range ratios {
		b.Run(fmt.Sprintf("ratio-%.0f", ratio*100), func(b *testing.B) {
			c := NewContextCompressor(DefaultConfig())
			opts := Options{TargetRatio: ratio}
			ctx := context.Background()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = c.Compress(ctx, content, opts)
			}
		})
	}
}

// BenchmarkCompress_ByMethod benchmarks different compression methods.
func BenchmarkCompress_ByMethod(b *testing.B) {
	content := generateContent(2000)
	ctx := context.Background()

	b.Run("extractive-only", func(b *testing.B) {
		c := NewContextCompressor(Config{
			ExtractiveThreshold:  100000,
			AbstractiveThreshold: 200000,
		})
		opts := Options{TargetRatio: 0.3}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Compress(ctx, content, opts)
		}
	})

	b.Run("with-llm-fallback", func(b *testing.B) {
		llm := &MockLLMClient{
			response: func(prompt string) string {
				return "Summary of content."
			},
		}
		c := NewContextCompressor(Config{
			ExtractiveThreshold:  500,
			AbstractiveThreshold: 1000,
			LLMClient:            llm,
		})
		opts := Options{TargetRatio: 0.3}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Compress(ctx, content, opts)
		}
	})
}

// BenchmarkCompress_ByContentType benchmarks compression with different content types.
func BenchmarkCompress_ByContentType(b *testing.B) {
	ctx := context.Background()
	c := NewContextCompressor(DefaultConfig())
	opts := Options{TargetRatio: 0.3}

	b.Run("prose", func(b *testing.B) {
		content := generateProse(500)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Compress(ctx, content, opts)
		}
	})

	b.Run("code", func(b *testing.B) {
		content := generateCode(500)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Compress(ctx, content, opts)
		}
	})

	b.Run("technical", func(b *testing.B) {
		content := generateTechnical(500)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Compress(ctx, content, opts)
		}
	})

	b.Run("mixed", func(b *testing.B) {
		content := generateMixed(500)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Compress(ctx, content, opts)
		}
	})
}

// BenchmarkSplitSentences_ByLength benchmarks sentence splitting with varying lengths.
func BenchmarkSplitSentences_ByLength(b *testing.B) {
	lengths := []int{10, 50, 100, 500}

	for _, numSentences := range lengths {
		b.Run(fmt.Sprintf("sentences-%d", numSentences), func(b *testing.B) {
			content := generateSentences(numSentences)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = splitSentences(content)
			}
		})
	}
}

// BenchmarkScoreSentences benchmarks sentence scoring.
func BenchmarkScoreSentences(b *testing.B) {
	c := NewContextCompressor(DefaultConfig())
	ctx := context.Background()

	sizes := []int{10, 50, 100, 200}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("sentences-%d", size), func(b *testing.B) {
			sentences := make([]string, size)
			for i := 0; i < size; i++ {
				sentences[i] = fmt.Sprintf("This is sentence number %d with some content.", i)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = c.scoreSentences(ctx, sentences, "")
			}
		})
	}
}

// BenchmarkScoreSentences_WithQuery benchmarks query-aware scoring without embedder.
func BenchmarkScoreSentences_WithQuery(b *testing.B) {
	c := NewContextCompressor(DefaultConfig())
	ctx := context.Background()

	sentences := make([]string, 50)
	for i := 0; i < 50; i++ {
		sentences[i] = fmt.Sprintf("This is sentence number %d with some content.", i)
	}
	query := "What is sentence number 25?"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.scoreSentences(ctx, sentences, query)
	}
}

// BenchmarkSelectSentences benchmarks sentence selection.
func BenchmarkSelectSentences(b *testing.B) {
	c := NewContextCompressor(DefaultConfig())

	sizes := []int{20, 50, 100, 200}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("sentences-%d", size), func(b *testing.B) {
			sentences := make([]string, size)
			scores := make([]float64, size)
			for i := 0; i < size; i++ {
				sentences[i] = fmt.Sprintf("This is a sentence with approximately ten words in it.")
				scores[i] = float64(size-i) / float64(size)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = c.selectSentences(sentences, scores, 100)
			}
		})
	}
}

// BenchmarkEstimateTokens_BySize benchmarks token estimation with varying sizes.
func BenchmarkEstimateTokens_BySize(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("chars-%d", size), func(b *testing.B) {
			content := strings.Repeat("word ", size/5)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = estimateTokens(content)
			}
		})
	}
}

// BenchmarkCompress_Throughput measures compression throughput.
func BenchmarkCompress_Throughput(b *testing.B) {
	c := NewContextCompressor(DefaultConfig())
	content := generateContent(1000)
	opts := Options{TargetRatio: 0.3}
	ctx := context.Background()

	b.SetBytes(int64(len(content)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

// Helper functions to generate test content.

func generateContent(numWords int) string {
	words := []string{"the", "quick", "brown", "fox", "jumps", "over", "lazy", "dog", "and", "runs"}
	var result strings.Builder
	for i := 0; i < numWords; i++ {
		result.WriteString(words[i%len(words)])
		if i%10 == 9 {
			result.WriteString(". ")
		} else {
			result.WriteString(" ")
		}
	}
	return result.String()
}

func generateSentences(count int) string {
	var sentences []string
	for i := 0; i < count; i++ {
		sentences = append(sentences, fmt.Sprintf("This is sentence number %d with some variable content.", i))
	}
	return strings.Join(sentences, " ")
}

func generateProse(numWords int) string {
	templates := []string{
		"The development of artificial intelligence has revolutionized many industries.",
		"Machine learning algorithms continue to improve in accuracy and efficiency.",
		"Natural language processing enables computers to understand human speech.",
		"Deep learning networks can recognize patterns in complex data sets.",
		"The future of technology depends on continued research and innovation.",
	}
	var result strings.Builder
	words := 0
	for words < numWords {
		template := templates[words%len(templates)]
		result.WriteString(template)
		result.WriteString(" ")
		words += len(strings.Fields(template))
	}
	return result.String()
}

func generateCode(numWords int) string {
	codeSnippets := []string{
		"func main() { fmt.Println(\"Hello\") }",
		"if err != nil { return err }",
		"for i := 0; i < n; i++ { sum += arr[i] }",
		"type Config struct { Name string; Value int }",
		"ctx, cancel := context.WithTimeout(parent, time.Second)",
	}
	var result strings.Builder
	words := 0
	for words < numWords {
		snippet := codeSnippets[words%len(codeSnippets)]
		result.WriteString(snippet)
		result.WriteString("\n")
		words += len(strings.Fields(snippet))
	}
	return result.String()
}

func generateTechnical(numWords int) string {
	technical := []string{
		"The API endpoint returns a JSON response with status code 200.",
		"Configure the database connection string in environment variables.",
		"The microservice architecture enables horizontal scaling.",
		"Implement rate limiting to prevent denial of service attacks.",
		"Use TLS encryption for all network communication channels.",
	}
	var result strings.Builder
	words := 0
	for words < numWords {
		sentence := technical[words%len(technical)]
		result.WriteString(sentence)
		result.WriteString(" ")
		words += len(strings.Fields(sentence))
	}
	return result.String()
}

func generateMixed(numWords int) string {
	mixed := []string{
		"The function processes user input and validates the data.",
		"```go\nfunc validate(s string) bool { return len(s) > 0 }\n```",
		"Error handling is critical for production applications.",
		"if err != nil { log.Fatal(err) }",
		"The performance benchmark shows 2x improvement over baseline.",
	}
	var result strings.Builder
	words := 0
	for words < numWords {
		content := mixed[words%len(mixed)]
		result.WriteString(content)
		result.WriteString("\n")
		words += len(strings.Fields(content))
	}
	return result.String()
}
