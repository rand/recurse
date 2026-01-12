package rlm

import (
	"fmt"
	"strings"
	"time"
)

// RLMProfile captures timing and performance metrics for RLM execution.
type RLMProfile struct {
	// Overall timing
	TotalDuration     time.Duration
	PrepareContextDur time.Duration

	// Per-iteration metrics
	Iterations []IterationProfile

	// Aggregated metrics
	TotalLLMTime      time.Duration
	TotalREPLTime     time.Duration
	TotalParseTime    time.Duration
	TotalOtherTime    time.Duration

	// Token metrics
	TotalPromptTokens     int
	TotalCompletionTokens int
}

// IterationProfile captures metrics for a single RLM iteration.
type IterationProfile struct {
	Number       int
	StartTime    time.Time
	Duration     time.Duration

	// Phase timings
	LLMCallDur   time.Duration
	REPLExecDur  time.Duration
	ParseDur     time.Duration
	OtherDur     time.Duration

	// Token counts
	PromptTokens     int
	CompletionTokens int

	// Execution details
	CodeLength      int
	HasCode         bool
	HasFinal        bool
	REPLOutputLen   int
	REPLError       string
}

// NewRLMProfile creates a new profile instance.
func NewRLMProfile() *RLMProfile {
	return &RLMProfile{
		Iterations: make([]IterationProfile, 0),
	}
}

// StartIteration begins tracking a new iteration.
func (p *RLMProfile) StartIteration(number int) *IterationProfile {
	iter := IterationProfile{
		Number:    number,
		StartTime: time.Now(),
	}
	return &iter
}

// EndIteration completes an iteration and adds it to the profile.
func (p *RLMProfile) EndIteration(iter *IterationProfile) {
	iter.Duration = time.Since(iter.StartTime)

	// Calculate "other" time (overhead not accounted for)
	accounted := iter.LLMCallDur + iter.REPLExecDur + iter.ParseDur
	if accounted < iter.Duration {
		iter.OtherDur = iter.Duration - accounted
	}

	p.Iterations = append(p.Iterations, *iter)

	// Update aggregates
	p.TotalLLMTime += iter.LLMCallDur
	p.TotalREPLTime += iter.REPLExecDur
	p.TotalParseTime += iter.ParseDur
	p.TotalOtherTime += iter.OtherDur
	p.TotalPromptTokens += iter.PromptTokens
	p.TotalCompletionTokens += iter.CompletionTokens
}

// Finalize calculates final metrics.
func (p *RLMProfile) Finalize() {
	// Total duration is already set externally
}

// Summary returns a human-readable summary of the profile.
func (p *RLMProfile) Summary() string {
	var sb strings.Builder

	sb.WriteString("\n=== RLM Execution Profile ===\n\n")

	// Overall metrics
	sb.WriteString(fmt.Sprintf("Total Duration: %v\n", p.TotalDuration.Round(time.Millisecond)))
	sb.WriteString(fmt.Sprintf("Iterations: %d\n", len(p.Iterations)))
	sb.WriteString(fmt.Sprintf("Total Tokens: %d (prompt: %d, completion: %d)\n\n",
		p.TotalPromptTokens+p.TotalCompletionTokens,
		p.TotalPromptTokens,
		p.TotalCompletionTokens))

	// Time breakdown
	sb.WriteString("Time Breakdown:\n")
	if p.TotalDuration > 0 {
		sb.WriteString(fmt.Sprintf("  LLM Calls:    %v (%.1f%%)\n",
			p.TotalLLMTime.Round(time.Millisecond),
			float64(p.TotalLLMTime)/float64(p.TotalDuration)*100))
		sb.WriteString(fmt.Sprintf("  REPL Exec:    %v (%.1f%%)\n",
			p.TotalREPLTime.Round(time.Millisecond),
			float64(p.TotalREPLTime)/float64(p.TotalDuration)*100))
		sb.WriteString(fmt.Sprintf("  Parsing:      %v (%.1f%%)\n",
			p.TotalParseTime.Round(time.Millisecond),
			float64(p.TotalParseTime)/float64(p.TotalDuration)*100))
		sb.WriteString(fmt.Sprintf("  Other:        %v (%.1f%%)\n",
			p.TotalOtherTime.Round(time.Millisecond),
			float64(p.TotalOtherTime)/float64(p.TotalDuration)*100))
	}

	// Per-iteration breakdown
	sb.WriteString("\nPer-Iteration:\n")
	for _, iter := range p.Iterations {
		sb.WriteString(fmt.Sprintf("  [%d] %v - LLM: %v, REPL: %v",
			iter.Number,
			iter.Duration.Round(time.Millisecond),
			iter.LLMCallDur.Round(time.Millisecond),
			iter.REPLExecDur.Round(time.Millisecond)))

		if iter.HasCode {
			sb.WriteString(fmt.Sprintf(" (code: %d bytes)", iter.CodeLength))
		}
		if iter.HasFinal {
			sb.WriteString(" [FINAL]")
		}
		if iter.REPLError != "" {
			sb.WriteString(" [ERROR]")
		}
		sb.WriteString("\n")
	}

	// Bottleneck analysis
	sb.WriteString("\nBottleneck Analysis:\n")
	if p.TotalLLMTime > p.TotalREPLTime && p.TotalLLMTime > p.TotalOtherTime {
		sb.WriteString("  Primary bottleneck: LLM calls\n")
		sb.WriteString("  Recommendation: Reduce iterations or use faster model\n")
	} else if p.TotalREPLTime > p.TotalLLMTime && p.TotalREPLTime > p.TotalOtherTime {
		sb.WriteString("  Primary bottleneck: REPL execution\n")
		sb.WriteString("  Recommendation: Optimize Python code or reduce context size\n")
	} else {
		sb.WriteString("  Primary bottleneck: Overhead/other\n")
		sb.WriteString("  Recommendation: Review context preparation and serialization\n")
	}

	// Average metrics
	if len(p.Iterations) > 0 {
		avgDur := p.TotalDuration / time.Duration(len(p.Iterations))
		avgLLM := p.TotalLLMTime / time.Duration(len(p.Iterations))
		avgREPL := p.TotalREPLTime / time.Duration(len(p.Iterations))

		sb.WriteString("\nAverages:\n")
		sb.WriteString(fmt.Sprintf("  Iteration: %v\n", avgDur.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("  LLM call: %v\n", avgLLM.Round(time.Millisecond)))
		sb.WriteString(fmt.Sprintf("  REPL exec: %v\n", avgREPL.Round(time.Millisecond)))
	}

	return sb.String()
}

// JSON returns a JSON-serializable representation of the profile.
func (p *RLMProfile) JSON() map[string]any {
	iterations := make([]map[string]any, len(p.Iterations))
	for i, iter := range p.Iterations {
		iterations[i] = map[string]any{
			"number":            iter.Number,
			"duration_ms":       iter.Duration.Milliseconds(),
			"llm_call_ms":       iter.LLMCallDur.Milliseconds(),
			"repl_exec_ms":      iter.REPLExecDur.Milliseconds(),
			"parse_ms":          iter.ParseDur.Milliseconds(),
			"other_ms":          iter.OtherDur.Milliseconds(),
			"prompt_tokens":     iter.PromptTokens,
			"completion_tokens": iter.CompletionTokens,
			"code_length":       iter.CodeLength,
			"has_code":          iter.HasCode,
			"has_final":         iter.HasFinal,
			"repl_output_len":   iter.REPLOutputLen,
			"has_error":         iter.REPLError != "",
		}
	}

	return map[string]any{
		"total_duration_ms":      p.TotalDuration.Milliseconds(),
		"iterations":             len(p.Iterations),
		"total_llm_time_ms":      p.TotalLLMTime.Milliseconds(),
		"total_repl_time_ms":     p.TotalREPLTime.Milliseconds(),
		"total_parse_time_ms":    p.TotalParseTime.Milliseconds(),
		"total_other_time_ms":    p.TotalOtherTime.Milliseconds(),
		"total_prompt_tokens":    p.TotalPromptTokens,
		"total_completion_tokens": p.TotalCompletionTokens,
		"iteration_details":      iterations,
	}
}

// BottleneckType indicates the primary performance bottleneck.
type BottleneckType string

const (
	BottleneckLLM   BottleneckType = "llm"
	BottleneckREPL  BottleneckType = "repl"
	BottleneckOther BottleneckType = "other"
)

// PrimaryBottleneck returns the primary bottleneck type.
func (p *RLMProfile) PrimaryBottleneck() BottleneckType {
	if p.TotalLLMTime >= p.TotalREPLTime && p.TotalLLMTime >= p.TotalOtherTime {
		return BottleneckLLM
	}
	if p.TotalREPLTime >= p.TotalLLMTime && p.TotalREPLTime >= p.TotalOtherTime {
		return BottleneckREPL
	}
	return BottleneckOther
}

// LLMTimePercent returns the percentage of time spent in LLM calls.
func (p *RLMProfile) LLMTimePercent() float64 {
	if p.TotalDuration == 0 {
		return 0
	}
	return float64(p.TotalLLMTime) / float64(p.TotalDuration) * 100
}

// REPLTimePercent returns the percentage of time spent in REPL execution.
func (p *RLMProfile) REPLTimePercent() float64 {
	if p.TotalDuration == 0 {
		return 0
	}
	return float64(p.TotalREPLTime) / float64(p.TotalDuration) * 100
}
