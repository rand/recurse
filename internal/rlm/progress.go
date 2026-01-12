package rlm

import (
	"time"
)

// ProgressEventType indicates the type of progress event.
type ProgressEventType string

const (
	// ProgressIterationStart signals the start of an iteration.
	ProgressIterationStart ProgressEventType = "iteration_start"

	// ProgressIterationEnd signals the end of an iteration.
	ProgressIterationEnd ProgressEventType = "iteration_end"

	// ProgressLLMStart signals the start of an LLM call.
	ProgressLLMStart ProgressEventType = "llm_start"

	// ProgressLLMEnd signals the end of an LLM call.
	ProgressLLMEnd ProgressEventType = "llm_end"

	// ProgressREPLStart signals the start of REPL execution.
	ProgressREPLStart ProgressEventType = "repl_start"

	// ProgressREPLEnd signals the end of REPL execution.
	ProgressREPLEnd ProgressEventType = "repl_end"

	// ProgressREPLOutput signals REPL output is available.
	ProgressREPLOutput ProgressEventType = "repl_output"

	// ProgressFinal signals FINAL() was called.
	ProgressFinal ProgressEventType = "final"

	// ProgressError signals an error occurred.
	ProgressError ProgressEventType = "error"

	// ProgressComplete signals execution is complete.
	ProgressComplete ProgressEventType = "complete"
)

// ProgressEvent contains information about RLM execution progress.
type ProgressEvent struct {
	// Type is the event type.
	Type ProgressEventType

	// Timestamp is when the event occurred.
	Timestamp time.Time

	// Iteration is the current iteration number (1-based).
	Iteration int

	// MaxIterations is the maximum iterations configured.
	MaxIterations int

	// Message is a human-readable description of the event.
	Message string

	// Data contains event-specific data.
	Data ProgressData
}

// ProgressData contains optional data for progress events.
type ProgressData struct {
	// Duration is how long the phase took (for *End events).
	Duration time.Duration

	// TokensUsed is tokens consumed (for LLMEnd events).
	TokensUsed int

	// Code is the Python code being executed (for REPLStart events).
	Code string

	// Output is the REPL output (for REPLOutput/REPLEnd events).
	Output string

	// Error is any error message.
	Error string

	// FinalOutput is the final answer (for Final/Complete events).
	FinalOutput string

	// HasCode indicates if code was extracted from LLM response.
	HasCode bool

	// EarlyTerminated indicates if execution terminated early.
	EarlyTerminated bool

	// TerminationReason explains why execution terminated.
	TerminationReason string
}

// ProgressCallback is called for each progress event during RLM execution.
// Implementations should be non-blocking and fast.
type ProgressCallback func(event ProgressEvent)

// ProgressEmitter emits progress events to a callback.
type ProgressEmitter struct {
	callback      ProgressCallback
	maxIterations int
}

// NewProgressEmitter creates a new progress emitter.
func NewProgressEmitter(callback ProgressCallback, maxIterations int) *ProgressEmitter {
	if callback == nil {
		return nil
	}
	return &ProgressEmitter{
		callback:      callback,
		maxIterations: maxIterations,
	}
}

// Emit sends a progress event.
func (e *ProgressEmitter) Emit(eventType ProgressEventType, iteration int, message string, data ProgressData) {
	if e == nil || e.callback == nil {
		return
	}

	event := ProgressEvent{
		Type:          eventType,
		Timestamp:     time.Now(),
		Iteration:     iteration,
		MaxIterations: e.maxIterations,
		Message:       message,
		Data:          data,
	}

	e.callback(event)
}

// EmitIterationStart emits an iteration start event.
func (e *ProgressEmitter) EmitIterationStart(iteration int) {
	e.Emit(ProgressIterationStart, iteration, "Starting iteration", ProgressData{})
}

// EmitIterationEnd emits an iteration end event.
func (e *ProgressEmitter) EmitIterationEnd(iteration int, duration time.Duration, hasCode bool) {
	e.Emit(ProgressIterationEnd, iteration, "Iteration complete", ProgressData{
		Duration: duration,
		HasCode:  hasCode,
	})
}

// EmitLLMStart emits an LLM call start event.
func (e *ProgressEmitter) EmitLLMStart(iteration int) {
	e.Emit(ProgressLLMStart, iteration, "Calling LLM...", ProgressData{})
}

// EmitLLMEnd emits an LLM call end event.
func (e *ProgressEmitter) EmitLLMEnd(iteration int, duration time.Duration, tokens int, hasCode bool) {
	msg := "LLM response received"
	if hasCode {
		msg = "LLM generated code"
	}
	e.Emit(ProgressLLMEnd, iteration, msg, ProgressData{
		Duration:   duration,
		TokensUsed: tokens,
		HasCode:    hasCode,
	})
}

// EmitREPLStart emits a REPL execution start event.
func (e *ProgressEmitter) EmitREPLStart(iteration int, code string) {
	// Truncate code for display
	displayCode := code
	if len(displayCode) > 100 {
		displayCode = displayCode[:100] + "..."
	}
	e.Emit(ProgressREPLStart, iteration, "Executing code...", ProgressData{
		Code: displayCode,
	})
}

// EmitREPLEnd emits a REPL execution end event.
func (e *ProgressEmitter) EmitREPLEnd(iteration int, duration time.Duration, output string, err string) {
	msg := "Code executed"
	if err != "" {
		msg = "Code execution error"
	}
	e.Emit(ProgressREPLEnd, iteration, msg, ProgressData{
		Duration: duration,
		Output:   truncateOutput(output, 200),
		Error:    err,
	})
}

// EmitREPLOutput emits REPL output as it's produced.
func (e *ProgressEmitter) EmitREPLOutput(iteration int, output string) {
	e.Emit(ProgressREPLOutput, iteration, "", ProgressData{
		Output: output,
	})
}

// EmitFinal emits a FINAL() called event.
func (e *ProgressEmitter) EmitFinal(iteration int, output string) {
	e.Emit(ProgressFinal, iteration, "Answer found", ProgressData{
		FinalOutput: truncateOutput(output, 500),
	})
}

// EmitError emits an error event.
func (e *ProgressEmitter) EmitError(iteration int, err string) {
	e.Emit(ProgressError, iteration, "Error occurred", ProgressData{
		Error: err,
	})
}

// EmitComplete emits a completion event.
func (e *ProgressEmitter) EmitComplete(iterations int, duration time.Duration, output string, earlyTerm bool, termReason string) {
	msg := "Execution complete"
	if earlyTerm {
		msg = "Execution complete (early termination)"
	}
	e.Emit(ProgressComplete, iterations, msg, ProgressData{
		Duration:          duration,
		FinalOutput:       truncateOutput(output, 500),
		EarlyTerminated:   earlyTerm,
		TerminationReason: termReason,
	})
}

// truncateOutput truncates output for progress display.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// FormatProgressEvent formats a progress event for display.
func FormatProgressEvent(event ProgressEvent) string {
	prefix := formatPrefix(event)

	switch event.Type {
	case ProgressIterationStart:
		return prefix + "Starting..."

	case ProgressIterationEnd:
		return prefix + formatDuration(event.Data.Duration)

	case ProgressLLMStart:
		return prefix + "Thinking..."

	case ProgressLLMEnd:
		if event.Data.HasCode {
			return prefix + "Generated code"
		}
		return prefix + "Response received"

	case ProgressREPLStart:
		if event.Data.Code != "" {
			return prefix + "Running: " + firstLine(event.Data.Code)
		}
		return prefix + "Executing..."

	case ProgressREPLEnd:
		if event.Data.Error != "" {
			return prefix + "Error: " + firstLine(event.Data.Error)
		}
		if event.Data.Output != "" {
			return prefix + "Output: " + firstLine(event.Data.Output)
		}
		return prefix + "Done"

	case ProgressREPLOutput:
		return event.Data.Output

	case ProgressFinal:
		return prefix + "Answer: " + firstLine(event.Data.FinalOutput)

	case ProgressError:
		return prefix + "Error: " + event.Data.Error

	case ProgressComplete:
		return prefix + "Complete in " + formatDuration(event.Data.Duration)

	default:
		return prefix + event.Message
	}
}

func formatPrefix(event ProgressEvent) string {
	if event.MaxIterations > 0 {
		return "[" + string(rune('0'+event.Iteration)) + "/" + string(rune('0'+event.MaxIterations)) + "] "
	}
	return "[RLM] "
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' || c == '\r' {
			if i > 50 {
				return s[:50] + "..."
			}
			return s[:i]
		}
	}
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
