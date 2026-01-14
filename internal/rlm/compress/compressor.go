// Package compress provides context compression for the RLM system.
// It implements two-stage compression (extractive + abstractive) to reduce
// token costs while preserving semantic content.
package compress

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rand/recurse/internal/memory/embeddings"
)

// Compressor compresses context content.
type Compressor interface {
	// Compress reduces content size while preserving meaning.
	Compress(ctx context.Context, content string, opts Options) (*Result, error)
}

// LLMClient interface for abstractive compression.
type LLMClient interface {
	// Complete sends a prompt and returns the response.
	Complete(ctx context.Context, prompt string) (string, int, error)
}

// Options configures compression behavior.
type Options struct {
	// TargetTokens is the desired output size in tokens.
	// If zero, uses TargetRatio instead.
	TargetTokens int

	// TargetRatio is the target compression ratio (e.g., 0.25 = 25% of original).
	TargetRatio float64

	// MinTokens is the minimum output size (never compress below this).
	MinTokens int

	// PreserveCode keeps code blocks intact when possible.
	PreserveCode bool

	// PreserveQuotes keeps direct quotes.
	PreserveQuotes bool

	// QueryContext optimizes compression for this query.
	QueryContext string
}

// DefaultOptions returns sensible default options.
func DefaultOptions() Options {
	return Options{
		TargetRatio:    0.3, // 30% of original
		MinTokens:      50,
		PreserveCode:   true,
		PreserveQuotes: true,
	}
}

// Method indicates the compression technique used.
type Method int

const (
	// MethodExtractive selects important sentences.
	MethodExtractive Method = iota
	// MethodAbstractive generates a summary via LLM.
	MethodAbstractive
	// MethodHybrid combines extractive and abstractive.
	MethodHybrid
	// MethodPassthrough indicates no compression was needed.
	MethodPassthrough
)

func (m Method) String() string {
	switch m {
	case MethodExtractive:
		return "extractive"
	case MethodAbstractive:
		return "abstractive"
	case MethodHybrid:
		return "hybrid"
	case MethodPassthrough:
		return "passthrough"
	default:
		return "unknown"
	}
}

// Result contains compressed content and metadata.
type Result struct {
	// Compressed is the compressed content.
	Compressed string

	// OriginalTokens is the token count before compression.
	OriginalTokens int

	// CompressedTokens is the token count after compression.
	CompressedTokens int

	// Ratio is the compression ratio (compressed/original).
	Ratio float64

	// Method indicates which compression technique was used.
	Method Method

	// Duration is how long compression took.
	Duration time.Duration

	// Metadata contains additional information.
	Metadata Metadata
}

// Metadata provides details about the compression.
type Metadata struct {
	// SentencesSelected is the count for extractive compression.
	SentencesSelected int

	// SentencesTotal is the original sentence count.
	SentencesTotal int

	// PreservedCode indicates if code blocks were preserved.
	PreservedCode bool

	// StagesUsed lists the stages for hybrid compression.
	StagesUsed []string
}

// ContextCompressor implements two-stage context compression.
type ContextCompressor struct {
	// Thresholds for method selection
	extractiveThreshold  int // Use extractive below this token count
	abstractiveThreshold int // Use abstractive above extractive threshold

	// Components
	embedder  embeddings.Provider
	llmClient LLMClient

	// Metrics
	mu      sync.Mutex
	metrics Metrics
}

// Metrics tracks compression statistics.
type Metrics struct {
	TotalCompressions int64
	TotalTokensSaved  int64
	AvgCompressionRatio float64
	MethodCounts      map[Method]int64
}

// Config configures the ContextCompressor.
type Config struct {
	// ExtractiveThreshold: use extractive for content below this size.
	ExtractiveThreshold int

	// AbstractiveThreshold: use abstractive (two-stage) above this size.
	AbstractiveThreshold int

	// Embedder for query-aware sentence scoring (optional).
	Embedder embeddings.Provider

	// LLMClient for abstractive compression (optional, extractive-only if nil).
	LLMClient LLMClient
}

// DefaultConfig returns default compressor configuration.
func DefaultConfig() Config {
	return Config{
		ExtractiveThreshold:  2000,  // ~500 words
		AbstractiveThreshold: 8000,  // ~2000 words
	}
}

// NewContextCompressor creates a new context compressor.
func NewContextCompressor(cfg Config) *ContextCompressor {
	return &ContextCompressor{
		extractiveThreshold:  cfg.ExtractiveThreshold,
		abstractiveThreshold: cfg.AbstractiveThreshold,
		embedder:             cfg.Embedder,
		llmClient:            cfg.LLMClient,
		metrics: Metrics{
			MethodCounts: make(map[Method]int64),
		},
	}
}

// Compress reduces content size using the appropriate method.
func (c *ContextCompressor) Compress(ctx context.Context, content string, opts Options) (*Result, error) {
	start := time.Now()

	originalTokens := estimateTokens(content)

	// Compute target
	targetTokens := c.computeTarget(originalTokens, opts)

	// No compression needed
	if originalTokens <= targetTokens || originalTokens <= opts.MinTokens {
		return &Result{
			Compressed:       content,
			OriginalTokens:   originalTokens,
			CompressedTokens: originalTokens,
			Ratio:            1.0,
			Method:           MethodPassthrough,
			Duration:         time.Since(start),
		}, nil
	}

	var result *Result
	var err error

	// Select method based on content size and available components
	switch {
	case originalTokens < c.extractiveThreshold:
		// Small content: extractive only
		result, err = c.extractive(ctx, content, targetTokens, opts)
	case originalTokens < c.abstractiveThreshold || c.llmClient == nil:
		// Medium content or no LLM: extractive
		result, err = c.extractive(ctx, content, targetTokens, opts)
	default:
		// Large content with LLM: two-stage hybrid
		result, err = c.hybrid(ctx, content, targetTokens, opts)
	}

	if err != nil {
		return nil, err
	}

	result.OriginalTokens = originalTokens
	result.Duration = time.Since(start)
	result.Ratio = float64(result.CompressedTokens) / float64(originalTokens)

	// Update metrics
	c.updateMetrics(result)

	return result, nil
}

func (c *ContextCompressor) computeTarget(originalTokens int, opts Options) int {
	if opts.TargetTokens > 0 {
		return opts.TargetTokens
	}
	if opts.TargetRatio > 0 && opts.TargetRatio < 1 {
		return int(float64(originalTokens) * opts.TargetRatio)
	}
	// Default to 30%
	return int(float64(originalTokens) * 0.3)
}

// extractive performs extractive compression by selecting important sentences.
func (c *ContextCompressor) extractive(ctx context.Context, content string, targetTokens int, opts Options) (*Result, error) {
	// Split into sentences
	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return &Result{
			Compressed:       content,
			CompressedTokens: estimateTokens(content),
			Method:           MethodExtractive,
		}, nil
	}

	// Score sentences
	scores := c.scoreSentences(ctx, sentences, opts.QueryContext)

	// Select top sentences
	selected := c.selectSentences(sentences, scores, targetTokens)

	compressed := strings.Join(selected, " ")

	return &Result{
		Compressed:       compressed,
		CompressedTokens: estimateTokens(compressed),
		Method:           MethodExtractive,
		Metadata: Metadata{
			SentencesSelected: len(selected),
			SentencesTotal:    len(sentences),
		},
	}, nil
}

// abstractive performs abstractive compression using LLM summarization.
func (c *ContextCompressor) abstractive(ctx context.Context, content string, targetTokens int, opts Options) (*Result, error) {
	if c.llmClient == nil {
		// Fall back to extractive
		return c.extractive(ctx, content, targetTokens, opts)
	}

	prompt := c.buildAbstractionPrompt(content, targetTokens, opts)

	compressed, _, err := c.llmClient.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("abstractive compression: %w", err)
	}

	return &Result{
		Compressed:       compressed,
		CompressedTokens: estimateTokens(compressed),
		Method:           MethodAbstractive,
	}, nil
}

// hybrid performs two-stage compression: extractive then abstractive.
func (c *ContextCompressor) hybrid(ctx context.Context, content string, targetTokens int, opts Options) (*Result, error) {
	stages := []string{}

	// Stage 1: Extractive to reduce size by 50%
	intermediateTarget := estimateTokens(content) / 2
	extracted, err := c.extractive(ctx, content, intermediateTarget, opts)
	if err != nil {
		return nil, fmt.Errorf("hybrid extractive stage: %w", err)
	}
	stages = append(stages, "extractive")

	// Stage 2: Abstractive on reduced content
	if c.llmClient != nil && extracted.CompressedTokens > targetTokens {
		final, err := c.abstractive(ctx, extracted.Compressed, targetTokens, opts)
		if err != nil {
			// Fall back to extractive result
			extracted.Method = MethodHybrid
			extracted.Metadata.StagesUsed = stages
			return extracted, nil
		}
		stages = append(stages, "abstractive")
		final.Method = MethodHybrid
		final.Metadata.StagesUsed = stages
		final.Metadata.SentencesSelected = extracted.Metadata.SentencesSelected
		final.Metadata.SentencesTotal = extracted.Metadata.SentencesTotal
		return final, nil
	}

	extracted.Method = MethodHybrid
	extracted.Metadata.StagesUsed = stages
	return extracted, nil
}

func (c *ContextCompressor) scoreSentences(ctx context.Context, sentences []string, query string) []float64 {
	scores := make([]float64, len(sentences))

	// Position score: earlier sentences often more important
	for i := range sentences {
		scores[i] = 1.0 - float64(i)*0.005 // Decay with position
		if scores[i] < 0.5 {
			scores[i] = 0.5 // Floor at 0.5
		}
	}

	// Length score: prefer informative sentences (not too short, not too long)
	for i, sent := range sentences {
		words := len(strings.Fields(sent))
		switch {
		case words < 3:
			scores[i] *= 0.5 // Penalize very short
		case words >= 5 && words <= 30:
			scores[i] *= 1.2 // Boost medium length
		case words > 50:
			scores[i] *= 0.8 // Slight penalty for very long
		}
	}

	// Query relevance (if embedder available)
	if query != "" && c.embedder != nil {
		queryScores := c.scoreByQuery(ctx, sentences, query)
		for i, qs := range queryScores {
			scores[i] += qs * 2.0 // Weight query relevance highly
		}
	}

	// Keyword importance
	importantKeywords := []string{
		"important", "key", "main", "critical", "essential",
		"result", "conclusion", "summary", "therefore", "finally",
		"error", "warning", "note", "must", "should",
	}
	for i, sent := range sentences {
		lower := strings.ToLower(sent)
		for _, kw := range importantKeywords {
			if strings.Contains(lower, kw) {
				scores[i] += 0.3
				break
			}
		}
	}

	return scores
}

func (c *ContextCompressor) scoreByQuery(ctx context.Context, sentences []string, query string) []float64 {
	scores := make([]float64, len(sentences))

	// Embed query
	queryVecs, err := c.embedder.Embed(ctx, []string{query})
	if err != nil || len(queryVecs) == 0 {
		return scores
	}
	queryVec := queryVecs[0]

	// Embed sentences in batches
	const batchSize = 50
	for i := 0; i < len(sentences); i += batchSize {
		end := i + batchSize
		if end > len(sentences) {
			end = len(sentences)
		}
		batch := sentences[i:end]

		vecs, err := c.embedder.Embed(ctx, batch)
		if err != nil {
			continue
		}

		for j, vec := range vecs {
			scores[i+j] = float64(queryVec.Similarity(vec))
		}
	}

	return scores
}

func (c *ContextCompressor) selectSentences(sentences []string, scores []float64, targetTokens int) []string {
	// Create scored pairs
	type scoredSentence struct {
		index int
		score float64
		sent  string
	}
	pairs := make([]scoredSentence, len(sentences))
	for i, s := range sentences {
		pairs[i] = scoredSentence{i, scores[i], s}
	}

	// Sort by score descending
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	// Select until target reached
	var selected []scoredSentence
	tokenCount := 0
	for _, p := range pairs {
		sentTokens := estimateTokens(p.sent)
		if tokenCount+sentTokens > targetTokens && len(selected) > 0 {
			break
		}
		selected = append(selected, p)
		tokenCount += sentTokens
	}

	// Sort by original position to maintain coherence
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].index < selected[j].index
	})

	result := make([]string, len(selected))
	for i, s := range selected {
		result[i] = s.sent
	}

	return result
}

func (c *ContextCompressor) buildAbstractionPrompt(content string, targetTokens int, opts Options) string {
	var preserveInstructions strings.Builder
	if opts.PreserveCode {
		preserveInstructions.WriteString("- Preserve code snippets and technical details\n")
	}
	if opts.PreserveQuotes {
		preserveInstructions.WriteString("- Keep important quotes verbatim\n")
	}

	var queryInstruction string
	if opts.QueryContext != "" {
		queryInstruction = fmt.Sprintf("\nFocus on information relevant to: %s\n", opts.QueryContext)
	}

	return fmt.Sprintf(`Summarize the following content in approximately %d tokens.
%s
Requirements:
- Preserve key facts, entities, and relationships
- Maintain technical accuracy
- Use concise language
%s
Content:
---
%s
---

Summary:`, targetTokens, queryInstruction, preserveInstructions.String(), content)
}

func (c *ContextCompressor) updateMetrics(result *Result) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics.TotalCompressions++
	c.metrics.TotalTokensSaved += int64(result.OriginalTokens - result.CompressedTokens)
	c.metrics.MethodCounts[result.Method]++

	// Running average
	n := float64(c.metrics.TotalCompressions)
	c.metrics.AvgCompressionRatio = c.metrics.AvgCompressionRatio*(n-1)/n + result.Ratio/n
}

// Metrics returns current compression metrics.
func (c *ContextCompressor) Metrics() Metrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Return copy
	m := c.metrics
	m.MethodCounts = make(map[Method]int64)
	for k, v := range c.metrics.MethodCounts {
		m.MethodCounts[k] = v
	}
	return m
}

// Helper functions

// estimateTokens provides a rough token count estimate.
// Rule of thumb: ~4 characters per token for English.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	// More accurate: count words and add for punctuation
	words := len(strings.Fields(s))
	return int(float64(words) * 1.3) // ~1.3 tokens per word average
}

// splitSentences splits text into sentences.
func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// First, check if text has multiple lines - treat each line as a sentence
	if strings.Contains(text, "\n") {
		var sentences []string
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				sentences = append(sentences, line)
			}
		}
		if len(sentences) > 1 {
			return sentences
		}
	}

	// Otherwise, split on sentence-ending punctuation
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)

		// Check for sentence-ending punctuation
		if r == '.' || r == '!' || r == '?' {
			// Look ahead to see if this is a sentence boundary
			// Skip whitespace
			j := i + 1
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}

			// If at end or next non-space is uppercase, end sentence
			if j >= len(runes) || unicode.IsUpper(runes[j]) {
				sent := strings.TrimSpace(current.String())
				if sent != "" && len(sent) > 2 {
					sentences = append(sentences, sent)
				}
				current.Reset()
				i = j - 1 // Position before the next non-space char
			}
		}
	}

	// Add remaining content
	remaining := strings.TrimSpace(current.String())
	if remaining != "" && len(remaining) > 2 {
		sentences = append(sentences, remaining)
	}

	// If still nothing, return original as single sentence
	if len(sentences) == 0 {
		return []string{text}
	}

	return sentences
}

// truncateWords truncates to approximately n words.
func truncateWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) <= n {
		return s
	}
	return strings.Join(words[:n], " ") + "..."
}

// containsCode checks if text contains code blocks.
func containsCode(s string) bool {
	return strings.Contains(s, "```") ||
		strings.Contains(s, "func ") ||
		strings.Contains(s, "def ") ||
		strings.Contains(s, "class ") ||
		regexp.MustCompile(`\b(if|for|while|return)\s*[({]`).MatchString(s)
}

// isUpperCase checks if a rune is uppercase.
func isUpperCase(r rune) bool {
	return unicode.IsUpper(r)
}
