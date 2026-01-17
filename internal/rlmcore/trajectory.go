package rlmcore

import (
	core "github.com/rand/rlm-core/go/rlmcore"
)

// Re-export event types from core
type TrajectoryEventType = core.TrajectoryEventType

const (
	EventRLMStart          = core.EventRLMStart
	EventAnalyze           = core.EventAnalyze
	EventREPLExec          = core.EventREPLExec
	EventREPLResult        = core.EventREPLResult
	EventReason            = core.EventReason
	EventRecurseStart      = core.EventRecurseStart
	EventRecurseEnd        = core.EventRecurseEnd
	EventFinal             = core.EventFinal
	EventError             = core.EventError
	EventToolUse           = core.EventToolUse
	EventCostReport        = core.EventCostReport
	EventVerifyStart       = core.EventVerifyStart
	EventClaimExtracted    = core.EventClaimExtracted
	EventEvidenceChecked   = core.EventEvidenceChecked
	EventBudgetComputed    = core.EventBudgetComputed
	EventHallucinationFlag = core.EventHallucinationFlag
	EventVerifyComplete    = core.EventVerifyComplete
)

// TrajectoryEvent wraps the rlm-core TrajectoryEvent.
type TrajectoryEvent struct {
	inner *core.TrajectoryEvent
}

// NewRLMStartEvent creates an RLM start event.
func NewRLMStartEvent(query string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewRLMStartEvent(query)}
}

// NewAnalyzeEvent creates an analysis event.
func NewAnalyzeEvent(depth uint32, analysis string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewAnalyzeEvent(depth, analysis)}
}

// NewREPLExecEvent creates a REPL execution event.
func NewREPLExecEvent(depth uint32, code string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewREPLExecEvent(depth, code)}
}

// NewREPLResultEvent creates a REPL result event.
func NewREPLResultEvent(depth uint32, result string, success bool) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewREPLResultEvent(depth, result, success)}
}

// NewReasonEvent creates a reasoning event.
func NewReasonEvent(depth uint32, reasoning string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewReasonEvent(depth, reasoning)}
}

// NewRecurseStartEvent creates a recurse start event.
func NewRecurseStartEvent(depth uint32, query string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewRecurseStartEvent(depth, query)}
}

// NewRecurseEndEvent creates a recurse end event.
func NewRecurseEndEvent(depth uint32, result string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewRecurseEndEvent(depth, result)}
}

// NewFinalAnswerEvent creates a final answer event.
func NewFinalAnswerEvent(depth uint32, answer string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewFinalAnswerEvent(depth, answer)}
}

// NewErrorEvent creates an error event.
func NewErrorEvent(depth uint32, err string) *TrajectoryEvent {
	return &TrajectoryEvent{inner: core.NewErrorEvent(depth, err)}
}

// Type returns the event type.
func (e *TrajectoryEvent) Type() TrajectoryEventType {
	return e.inner.Type()
}

// Depth returns the recursion depth.
func (e *TrajectoryEvent) Depth() uint32 {
	return e.inner.Depth()
}

// Content returns the event content.
func (e *TrajectoryEvent) Content() string {
	return e.inner.Content()
}

// LogLine returns a formatted log line.
func (e *TrajectoryEvent) LogLine() string {
	return e.inner.LogLine()
}

// IsError returns true if this is an error event.
func (e *TrajectoryEvent) IsError() bool {
	return e.inner.IsError()
}

// IsFinal returns true if this is a final event.
func (e *TrajectoryEvent) IsFinal() bool {
	return e.inner.IsFinal()
}

// Free releases event resources.
func (e *TrajectoryEvent) Free() {
	if e.inner != nil {
		e.inner.Free()
		e.inner = nil
	}
}

// TrajectoryCollector collects trajectory events.
type TrajectoryCollector struct {
	inner *core.TrajectoryCollector
}

// NewTrajectoryCollector creates a new collector.
func NewTrajectoryCollector() *TrajectoryCollector {
	return &TrajectoryCollector{inner: core.NewTrajectoryCollector()}
}

// Add adds an event to the collector.
func (c *TrajectoryCollector) Add(event *TrajectoryEvent) {
	c.inner.Add(event.inner)
	event.inner = nil // Transfer ownership
}

// Events returns all collected events.
func (c *TrajectoryCollector) Events() []*TrajectoryEvent {
	coreEvents := c.inner.Events()
	events := make([]*TrajectoryEvent, len(coreEvents))
	for i, e := range coreEvents {
		events[i] = &TrajectoryEvent{inner: e}
	}
	return events
}

// FinalAnswer returns the final answer if present.
func (c *TrajectoryCollector) FinalAnswer() string {
	return c.inner.FinalAnswer()
}

// HasError returns true if any error events were collected.
func (c *TrajectoryCollector) HasError() bool {
	return c.inner.HasError()
}

// TrajectoryEmitter streams events via a channel.
type TrajectoryEmitter struct {
	inner *core.TrajectoryEmitter
}

// NewTrajectoryEmitter creates an emitter with the given buffer size.
func NewTrajectoryEmitter(bufferSize int) *TrajectoryEmitter {
	return &TrajectoryEmitter{inner: core.NewTrajectoryEmitter(bufferSize)}
}

// Emit sends an event to the channel.
func (e *TrajectoryEmitter) Emit(event *TrajectoryEvent) {
	e.inner.Emit(event.inner)
	event.inner = nil // Transfer ownership
}

// Events returns the event channel for receiving.
func (e *TrajectoryEmitter) Events() <-chan *core.TrajectoryEvent {
	return e.inner.Events()
}

// Close closes the emitter.
func (e *TrajectoryEmitter) Close() {
	e.inner.Close()
}
