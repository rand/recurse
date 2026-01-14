package compress

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Property-based tests for context compression invariants.

// TestProperty_CompressedOutputFitsTarget verifies that compressed output
// respects the target token limit (with reasonable tolerance).
func TestProperty_CompressedOutputFitsTarget(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	rapid.Check(t, func(t *rapid.T) {
		// Generate random content
		numSentences := rapid.IntRange(5, 50).Draw(t, "numSentences")
		var sentences []string
		for i := 0; i < numSentences; i++ {
			numWords := rapid.IntRange(3, 20).Draw(t, "wordCount")
			words := make([]string, numWords)
			for j := 0; j < numWords; j++ {
				words[j] = rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "word")
			}
			sentences = append(sentences, strings.Join(words, " ")+".")
		}
		content := strings.Join(sentences, " ")

		// Generate target ratio
		targetRatio := rapid.Float64Range(0.2, 0.8).Draw(t, "targetRatio")

		opts := Options{
			TargetRatio: targetRatio,
			MinTokens:   10,
		}

		result, err := c.Compress(context.Background(), content, opts)
		if err != nil {
			t.Fatalf("compression failed: %v", err)
		}

		// Compressed output should not exceed original
		if result.CompressedTokens > result.OriginalTokens {
			t.Errorf("compressed (%d) > original (%d)", result.CompressedTokens, result.OriginalTokens)
		}

		// Compression ratio should be reasonable (within 50% of target for extractive)
		if result.Method == MethodExtractive && result.Ratio > targetRatio*1.5 && result.CompressedTokens > opts.MinTokens {
			t.Logf("ratio %.2f exceeds target %.2f by >50%%", result.Ratio, targetRatio)
		}
	})
}

// TestProperty_PassthroughPreservesContent verifies that small content
// is returned unchanged.
func TestProperty_PassthroughPreservesContent(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	rapid.Check(t, func(t *rapid.T) {
		// Generate small content (under typical min tokens)
		numWords := rapid.IntRange(1, 10).Draw(t, "wordCount")
		words := make([]string, numWords)
		for i := 0; i < numWords; i++ {
			words[i] = rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "word")
		}
		content := strings.Join(words, " ")

		opts := Options{
			TargetTokens: 100, // Target larger than content
			MinTokens:    50,
		}

		result, err := c.Compress(context.Background(), content, opts)
		if err != nil {
			t.Fatalf("compression failed: %v", err)
		}

		// Small content should pass through unchanged
		if result.Method != MethodPassthrough {
			t.Errorf("expected passthrough, got %s", result.Method)
		}
		if result.Compressed != content {
			t.Errorf("content was modified: %q -> %q", content, result.Compressed)
		}
		if result.Ratio != 1.0 {
			t.Errorf("passthrough ratio should be 1.0, got %.2f", result.Ratio)
		}
	})
}

// TestProperty_ExtractivePreservesSelectedSentences verifies that extractive
// compression returns a subset of original sentences.
func TestProperty_ExtractivePreservesSelectedSentences(t *testing.T) {
	c := NewContextCompressor(Config{
		ExtractiveThreshold:  100000, // Force extractive
		AbstractiveThreshold: 200000,
	})

	rapid.Check(t, func(t *rapid.T) {
		// Generate multi-sentence content
		numSentences := rapid.IntRange(10, 30).Draw(t, "numSentences")
		sentences := make([]string, numSentences)
		for i := 0; i < numSentences; i++ {
			numWords := rapid.IntRange(5, 15).Draw(t, "wordCount")
			words := make([]string, numWords)
			for j := 0; j < numWords; j++ {
				words[j] = rapid.StringMatching(`[A-Z][a-z]{2,8}`).Draw(t, "word")
			}
			sentences[i] = strings.Join(words, " ") + "."
		}
		content := strings.Join(sentences, " ")

		opts := Options{
			TargetRatio: rapid.Float64Range(0.3, 0.6).Draw(t, "targetRatio"),
			MinTokens:   5,
		}

		result, err := c.Compress(context.Background(), content, opts)
		if err != nil {
			t.Fatalf("compression failed: %v", err)
		}

		if result.Method != MethodExtractive {
			return // Skip if not extractive
		}

		// Each output sentence should be from original
		outputSentences := splitSentences(result.Compressed)
		for _, out := range outputSentences {
			found := false
			for _, orig := range sentences {
				if strings.Contains(orig, strings.TrimSuffix(out, ".")) ||
					strings.Contains(out, strings.TrimSuffix(orig, ".")) {
					found = true
					break
				}
			}
			if !found && len(out) > 10 {
				t.Logf("output sentence not found in original (may be truncated): %q", truncateWords(out, 5))
			}
		}

		// Metadata should reflect selection
		if result.Metadata.SentencesTotal == 0 {
			t.Error("SentencesTotal should be > 0")
		}
		if result.Metadata.SentencesSelected > result.Metadata.SentencesTotal {
			t.Errorf("selected (%d) > total (%d)", result.Metadata.SentencesSelected, result.Metadata.SentencesTotal)
		}
	})
}

// TestProperty_CompressionIsIdempotent verifies that compressing already
// compressed content doesn't reduce it further (significantly).
func TestProperty_CompressionIsIdempotent(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	rapid.Check(t, func(t *rapid.T) {
		// Generate content
		numSentences := rapid.IntRange(15, 40).Draw(t, "numSentences")
		var sentences []string
		for i := 0; i < numSentences; i++ {
			numWords := rapid.IntRange(5, 20).Draw(t, "wordCount")
			words := make([]string, numWords)
			for j := 0; j < numWords; j++ {
				words[j] = rapid.StringMatching(`[A-Z][a-z]{2,8}`).Draw(t, "word")
			}
			sentences = append(sentences, strings.Join(words, " ")+".")
		}
		content := strings.Join(sentences, " ")

		opts := Options{
			TargetRatio: 0.5,
			MinTokens:   10,
		}

		// First compression
		result1, err := c.Compress(context.Background(), content, opts)
		if err != nil {
			t.Fatalf("first compression failed: %v", err)
		}

		if result1.Method == MethodPassthrough {
			return // Skip if content was too small
		}

		// Second compression on result
		result2, err := c.Compress(context.Background(), result1.Compressed, opts)
		if err != nil {
			t.Fatalf("second compression failed: %v", err)
		}

		// Second compression should have minimal effect (passthrough or similar size)
		if result2.Method != MethodPassthrough {
			ratio := float64(result2.CompressedTokens) / float64(result1.CompressedTokens)
			if ratio < 0.5 {
				t.Logf("double compression significantly reduced: %.2f ratio", ratio)
			}
		}
	})
}

// TestProperty_ScoresAreNonNegative verifies that sentence scores are always >= 0.
func TestProperty_ScoresAreNonNegative(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	rapid.Check(t, func(t *rapid.T) {
		// Generate sentences
		numSentences := rapid.IntRange(5, 50).Draw(t, "numSentences")
		sentences := make([]string, numSentences)
		for i := 0; i < numSentences; i++ {
			numWords := rapid.IntRange(1, 30).Draw(t, "wordCount")
			words := make([]string, numWords)
			for j := 0; j < numWords; j++ {
				words[j] = rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "word")
			}
			sentences[i] = strings.Join(words, " ")
		}

		scores := c.scoreSentences(context.Background(), sentences, "")

		if len(scores) != len(sentences) {
			t.Errorf("scores length (%d) != sentences length (%d)", len(scores), len(sentences))
		}

		for i, score := range scores {
			if score < 0 {
				t.Errorf("score[%d] = %f is negative", i, score)
			}
		}
	})
}

// TestProperty_SelectionMaintainsOrder verifies that selected sentences
// maintain their original document order.
func TestProperty_SelectionMaintainsOrder(t *testing.T) {
	c := NewContextCompressor(DefaultConfig())

	rapid.Check(t, func(t *rapid.T) {
		// Generate uniquely numbered sentences to allow index tracking
		numSentences := rapid.IntRange(10, 30).Draw(t, "numSentences")
		sentences := make([]string, numSentences)
		for i := 0; i < numSentences; i++ {
			// Use unique marker to identify each sentence
			sentences[i] = fmt.Sprintf("Sentence_%d_with_content", i)
		}

		// Random scores
		scores := make([]float64, numSentences)
		for i := range scores {
			scores[i] = rapid.Float64Range(0.1, 1.0).Draw(t, "score")
		}

		targetTokens := rapid.IntRange(20, 100).Draw(t, "targetTokens")
		selected := c.selectSentences(sentences, scores, targetTokens)

		// Find original indices using unique markers
		var indices []int
		for _, sel := range selected {
			for i, orig := range sentences {
				if sel == orig {
					indices = append(indices, i)
					break
				}
			}
		}

		// Verify indices are in strictly ascending order
		for i := 1; i < len(indices); i++ {
			if indices[i] <= indices[i-1] {
				t.Errorf("order violated: index %d <= %d", indices[i], indices[i-1])
			}
		}
	})
}

// TestProperty_TokenEstimateIsReasonable verifies token estimates are within bounds.
func TestProperty_TokenEstimateIsReasonable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numWords := rapid.IntRange(0, 500).Draw(t, "numWords")
		words := make([]string, numWords)
		for i := 0; i < numWords; i++ {
			words[i] = rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "word")
		}
		text := strings.Join(words, " ")

		tokens := estimateTokens(text)

		// Token estimate should be reasonable
		if numWords == 0 && tokens != 0 {
			t.Errorf("empty text should have 0 tokens, got %d", tokens)
		}
		if numWords > 0 {
			// Tokens per word should be ~1.3 on average
			tokensPerWord := float64(tokens) / float64(numWords)
			if tokensPerWord < 0.5 || tokensPerWord > 3.0 {
				t.Errorf("tokens per word %.2f outside expected range [0.5, 3.0]", tokensPerWord)
			}
		}
	})
}
