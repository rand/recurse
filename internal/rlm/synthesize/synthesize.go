// Package synthesize combines results from multiple sub-LM calls into coherent responses.
package synthesize

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// SubCallResult represents the result of a sub-LM call.
type SubCallResult struct {
	// ID identifies the chunk or subtask this result corresponds to.
	ID string `json:"id"`

	// Name is a human-readable name for this result.
	Name string `json:"name"`

	// Response is the LLM's response for this subtask.
	Response string `json:"response"`

	// TokensUsed is the number of tokens consumed.
	TokensUsed int `json:"tokens_used"`

	// Error contains any error message if the sub-call failed.
	Error string `json:"error,omitempty"`
}

// SynthesisResult contains the synthesized output.
type SynthesisResult struct {
	// Response is the combined, synthesized response.
	Response string `json:"response"`

	// TotalTokensUsed is the sum of tokens from all sub-calls plus synthesis.
	TotalTokensUsed int `json:"total_tokens_used"`

	// PartCount is the number of sub-call results that were synthesized.
	PartCount int `json:"part_count"`
}

// Strategy specifies how to combine results.
type Strategy string

const (
	// StrategyConcatenate simply joins results with separators.
	StrategyConcatenate Strategy = "concatenate"

	// StrategySummarize uses an LLM to create a coherent summary.
	StrategySummarize Strategy = "summarize"

	// StrategyMerge intelligently merges overlapping information.
	StrategyMerge Strategy = "merge"
)

// Synthesizer combines multiple sub-call results.
type Synthesizer interface {
	// Synthesize combines the given results into a unified response.
	Synthesize(ctx context.Context, task string, results []SubCallResult) (*SynthesisResult, error)
}

// ConcatenateSynthesizer simply concatenates results.
type ConcatenateSynthesizer struct {
	// Separator between results (default: "\n\n---\n\n")
	Separator string

	// IncludeHeaders adds section headers for each result.
	IncludeHeaders bool
}

// NewConcatenateSynthesizer creates a concatenation-based synthesizer.
func NewConcatenateSynthesizer() *ConcatenateSynthesizer {
	return &ConcatenateSynthesizer{
		Separator:      "\n\n",
		IncludeHeaders: false, // Don't add chunk headers - cleaner output
	}
}

// Synthesize implements Synthesizer.
func (s *ConcatenateSynthesizer) Synthesize(ctx context.Context, task string, results []SubCallResult) (*SynthesisResult, error) {
	if len(results) == 0 {
		return &SynthesisResult{
			Response: "(no results to synthesize)",
		}, nil
	}

	var parts []string
	totalTokens := 0

	for _, r := range results {
		if r.Error != "" {
			continue // Skip failed results
		}

		var part string
		if s.IncludeHeaders && r.Name != "" {
			part = fmt.Sprintf("## %s\n\n%s", r.Name, r.Response)
		} else {
			part = r.Response
		}
		parts = append(parts, part)
		totalTokens += r.TokensUsed
	}

	sep := s.Separator
	if sep == "" {
		sep = "\n\n---\n\n"
	}

	return &SynthesisResult{
		Response:        strings.Join(parts, sep),
		TotalTokensUsed: totalTokens,
		PartCount:       len(parts),
	}, nil
}

// LLMSynthesizer uses an LLM to intelligently combine results.
type LLMSynthesizer struct {
	provider fantasy.Provider
	model    string
}

// NewLLMSynthesizer creates an LLM-based synthesizer.
func NewLLMSynthesizer(provider fantasy.Provider, model string) *LLMSynthesizer {
	if model == "" {
		model = "claude-3-5-sonnet-latest"
	}
	return &LLMSynthesizer{
		provider: provider,
		model:    model,
	}
}

// Synthesize implements Synthesizer.
func (s *LLMSynthesizer) Synthesize(ctx context.Context, task string, results []SubCallResult) (*SynthesisResult, error) {
	if len(results) == 0 {
		return &SynthesisResult{
			Response: "(no results to synthesize)",
		}, nil
	}

	// Build synthesis prompt
	var sb strings.Builder
	sb.WriteString("You are synthesizing results from multiple analysis passes into a coherent response.\n\n")
	sb.WriteString(fmt.Sprintf("Original task: %s\n\n", task))
	sb.WriteString("Results to synthesize:\n\n")

	totalInputTokens := 0
	validResults := 0

	for i, r := range results {
		if r.Error != "" {
			continue
		}
		validResults++
		sb.WriteString(fmt.Sprintf("### Part %d: %s\n\n", i+1, r.Name))
		sb.WriteString(r.Response)
		sb.WriteString("\n\n")
		totalInputTokens += r.TokensUsed
	}

	if validResults == 0 {
		return &SynthesisResult{
			Response: "(all sub-calls failed)",
		}, nil
	}

	sb.WriteString("---\n\n")
	sb.WriteString("Create a coherent, unified response that:\n")
	sb.WriteString("1. Combines insights from all parts\n")
	sb.WriteString("2. Removes redundancy\n")
	sb.WriteString("3. Maintains important details\n")
	sb.WriteString("4. Presents information in a logical order\n")

	// Call LLM
	lm, err := s.provider.LanguageModel(ctx, s.model)
	if err != nil {
		return nil, fmt.Errorf("get language model: %w", err)
	}

	maxTokens := int64(2000)
	call := fantasy.Call{
		Prompt:          fantasy.Prompt{fantasy.NewUserMessage(sb.String())},
		MaxOutputTokens: &maxTokens,
	}

	resp, err := lm.Generate(ctx, call)
	if err != nil {
		return nil, fmt.Errorf("synthesis generation: %w", err)
	}

	return &SynthesisResult{
		Response:        resp.Content.Text(),
		TotalTokensUsed: totalInputTokens + int(resp.Usage.TotalTokens),
		PartCount:       validResults,
	}, nil
}

// MergeSynthesizer combines results by merging similar sections.
type MergeSynthesizer struct {
	// MaxOutputLength caps the merged output length.
	MaxOutputLength int
}

// NewMergeSynthesizer creates a merge-based synthesizer.
func NewMergeSynthesizer(maxOutputLength int) *MergeSynthesizer {
	if maxOutputLength == 0 {
		maxOutputLength = 10000
	}
	return &MergeSynthesizer{
		MaxOutputLength: maxOutputLength,
	}
}

// Synthesize implements Synthesizer.
func (s *MergeSynthesizer) Synthesize(ctx context.Context, task string, results []SubCallResult) (*SynthesisResult, error) {
	if len(results) == 0 {
		return &SynthesisResult{
			Response: "(no results to synthesize)",
		}, nil
	}

	// Group results by detecting common themes/sections
	sections := make(map[string][]string)
	totalTokens := 0
	validCount := 0

	for _, r := range results {
		if r.Error != "" {
			continue
		}
		validCount++
		totalTokens += r.TokensUsed

		// Simple section detection based on markdown headers
		currentSection := "main"
		lines := strings.Split(r.Response, "\n")

		for _, line := range lines {
			if strings.HasPrefix(line, "## ") {
				currentSection = strings.TrimPrefix(line, "## ")
			} else if strings.HasPrefix(line, "# ") {
				currentSection = strings.TrimPrefix(line, "# ")
			}
			sections[currentSection] = append(sections[currentSection], line)
		}
	}

	// Build merged output
	var sb strings.Builder
	written := 0

	// Write main content first
	if main, ok := sections["main"]; ok {
		for _, line := range main {
			if written+len(line) > s.MaxOutputLength {
				break
			}
			sb.WriteString(line)
			sb.WriteString("\n")
			written += len(line) + 1
		}
		delete(sections, "main")
	}

	// Write other sections
	for section, lines := range sections {
		if written > s.MaxOutputLength {
			break
		}
		sb.WriteString(fmt.Sprintf("\n## %s\n\n", section))
		written += len(section) + 6

		for _, line := range lines {
			if written+len(line) > s.MaxOutputLength {
				break
			}
			sb.WriteString(line)
			sb.WriteString("\n")
			written += len(line) + 1
		}
	}

	return &SynthesisResult{
		Response:        strings.TrimSpace(sb.String()),
		TotalTokensUsed: totalTokens,
		PartCount:       validCount,
	}, nil
}

// Auto selects the best synthesis strategy based on the results.
func Auto(results []SubCallResult) Synthesizer {
	if len(results) <= 2 {
		// For small number of results, simple concatenation works well
		return NewConcatenateSynthesizer()
	}

	// Check total size of results
	totalSize := 0
	for _, r := range results {
		totalSize += len(r.Response)
	}

	if totalSize > 20000 {
		// Large results benefit from merging
		return NewMergeSynthesizer(10000)
	}

	// Default to concatenation
	return NewConcatenateSynthesizer()
}
