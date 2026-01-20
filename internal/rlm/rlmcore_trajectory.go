package rlm

import (
	"fmt"
	"sync"
	"time"

	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreTrajectoryBridge provides integration between rlm-core's TrajectoryEvent
// and the Go trace system. When enabled (RLM_USE_CORE=true), it converts rlm-core
// events to the Go TraceEvent format for the TUI trace view.
type RLMCoreTrajectoryBridge struct {
	mu        sync.Mutex
	collector *rlmcore.TrajectoryCollector
	emitter   *rlmcore.TrajectoryEmitter
	recorder  TraceRecorder
	nextID    int64
	parentIDs map[uint32]string // depth -> parent ID mapping
}

// NewRLMCoreTrajectoryBridge creates a new trajectory bridge.
// recorder is the TraceRecorder to forward converted events to.
// Returns nil if rlm-core is not available.
func NewRLMCoreTrajectoryBridge(recorder TraceRecorder) *RLMCoreTrajectoryBridge {
	if !rlmcore.Available() {
		return nil
	}

	return &RLMCoreTrajectoryBridge{
		collector: rlmcore.NewTrajectoryCollector(),
		emitter:   rlmcore.NewTrajectoryEmitter(100),
		recorder:  recorder,
		parentIDs: make(map[uint32]string),
	}
}

// Available returns true if the bridge is available.
func (b *RLMCoreTrajectoryBridge) Available() bool {
	return b != nil && b.collector != nil
}

// RecordEvent records an rlm-core TrajectoryEvent, converting it to Go's TraceEvent
// and forwarding to the configured TraceRecorder.
func (b *RLMCoreTrajectoryBridge) RecordEvent(event *rlmcore.TrajectoryEvent) error {
	if b == nil || b.recorder == nil || event == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Generate a unique ID
	b.nextID++
	eventID := fmt.Sprintf("core-%d", b.nextID)

	// Convert rlm-core event to Go TraceEvent
	traceEvent := b.convertEvent(event, eventID)

	// Store as parent for subsequent child events
	depth := event.Depth()
	b.parentIDs[depth] = eventID

	// Collect for later retrieval
	b.collector.Add(event)

	// Forward to recorder
	return b.recorder.RecordEvent(traceEvent)
}

// EmitEvent emits an event to the streaming channel.
func (b *RLMCoreTrajectoryBridge) EmitEvent(event *rlmcore.TrajectoryEvent) {
	if b == nil || b.emitter == nil || event == nil {
		return
	}
	b.emitter.Emit(event)
}

// GetCollector returns the underlying collector.
func (b *RLMCoreTrajectoryBridge) GetCollector() *rlmcore.TrajectoryCollector {
	if b == nil {
		return nil
	}
	return b.collector
}

// GetEmitter returns the underlying emitter.
func (b *RLMCoreTrajectoryBridge) GetEmitter() *rlmcore.TrajectoryEmitter {
	if b == nil {
		return nil
	}
	return b.emitter
}

// FinalAnswer returns the final answer from collected events.
func (b *RLMCoreTrajectoryBridge) FinalAnswer() string {
	if b == nil || b.collector == nil {
		return ""
	}
	return b.collector.FinalAnswer()
}

// HasError returns true if any error events were recorded.
func (b *RLMCoreTrajectoryBridge) HasError() bool {
	if b == nil || b.collector == nil {
		return false
	}
	return b.collector.HasError()
}

// Close closes the emitter and releases resources.
func (b *RLMCoreTrajectoryBridge) Close() {
	if b == nil {
		return
	}
	if b.emitter != nil {
		b.emitter.Close()
	}
}

// convertEvent converts an rlm-core TrajectoryEvent to a Go TraceEvent.
func (b *RLMCoreTrajectoryBridge) convertEvent(event *rlmcore.TrajectoryEvent, id string) TraceEvent {
	depth := int(event.Depth())

	// Get parent ID from depth - 1
	var parentID string
	if depth > 0 {
		if pid, ok := b.parentIDs[uint32(depth-1)]; ok {
			parentID = pid
		}
	}

	return TraceEvent{
		ID:        id,
		Type:      mapTrajectoryEventType(event.Type()),
		Action:    getEventAction(event),
		Details:   event.Content(),
		Timestamp: time.Now(),
		Depth:     depth,
		ParentID:  parentID,
		Status:    getEventStatus(event),
	}
}

// mapTrajectoryEventType converts rlm-core TrajectoryEventType to Go's string type
// that mapEventType() can then convert to rlmtrace.TraceEventType.
func mapTrajectoryEventType(t rlmcore.TrajectoryEventType) string {
	switch t {
	case rlmcore.EventRLMStart:
		return "decision" // RLM start is a decision point
	case rlmcore.EventAnalyze:
		return "DECOMPOSE" // Analysis decomposes the problem
	case rlmcore.EventREPLExec:
		return "execute" // REPL execution
	case rlmcore.EventREPLResult:
		return "execute" // REPL result is part of execution
	case rlmcore.EventReason:
		return "decision" // Reasoning is a decision process
	case rlmcore.EventRecurseStart:
		return "SUBCALL" // Recursion is a subcall
	case rlmcore.EventRecurseEnd:
		return "SYNTHESIZE" // Synthesis at end of recursion
	case rlmcore.EventFinal:
		return "SYNTHESIZE" // Final answer is synthesis
	case rlmcore.EventError:
		return "decision" // Error is a terminal decision
	case rlmcore.EventToolUse:
		return "execute" // Tool use is execution
	case rlmcore.EventCostReport:
		return "decision" // Cost report is informational
	case rlmcore.EventVerifyStart:
		return "DECOMPOSE" // Verification decomposes claims
	case rlmcore.EventClaimExtracted:
		return "DECOMPOSE" // Claim extraction is decomposition
	case rlmcore.EventEvidenceChecked:
		return "MEMORY_QUERY" // Evidence check queries memory/context
	case rlmcore.EventBudgetComputed:
		return "decision" // Budget computation is a decision
	case rlmcore.EventHallucinationFlag:
		return "decision" // Flagging is a decision
	case rlmcore.EventVerifyComplete:
		return "SYNTHESIZE" // Verification complete synthesizes results
	default:
		return "decision"
	}
}

// getEventAction returns a human-readable action string for the event.
func getEventAction(event *rlmcore.TrajectoryEvent) string {
	switch event.Type() {
	case rlmcore.EventRLMStart:
		return "RLM Start"
	case rlmcore.EventAnalyze:
		return "Analyzing"
	case rlmcore.EventREPLExec:
		return "REPL Execute"
	case rlmcore.EventREPLResult:
		return "REPL Result"
	case rlmcore.EventReason:
		return "Reasoning"
	case rlmcore.EventRecurseStart:
		return "Recurse Start"
	case rlmcore.EventRecurseEnd:
		return "Recurse End"
	case rlmcore.EventFinal:
		return "Final Answer"
	case rlmcore.EventError:
		return "Error"
	case rlmcore.EventToolUse:
		return "Tool Use"
	case rlmcore.EventCostReport:
		return "Cost Report"
	case rlmcore.EventVerifyStart:
		return "Verify Start"
	case rlmcore.EventClaimExtracted:
		return "Claim Extracted"
	case rlmcore.EventEvidenceChecked:
		return "Evidence Checked"
	case rlmcore.EventBudgetComputed:
		return "Budget Computed"
	case rlmcore.EventHallucinationFlag:
		return "Hallucination Flag"
	case rlmcore.EventVerifyComplete:
		return "Verify Complete"
	default:
		return "Unknown Event"
	}
}

// getEventStatus returns the status string for the event.
func getEventStatus(event *rlmcore.TrajectoryEvent) string {
	if event.IsError() {
		return "failed"
	}
	if event.IsFinal() {
		return "completed"
	}
	return "completed" // Most events are "completed" when recorded
}

// UseRLMCoreTrajectory returns true if rlm-core should be used for trajectory tracking.
func UseRLMCoreTrajectory() bool {
	return rlmcore.Available()
}

// Helper functions for creating trajectory events via the bridge.

// RecordRLMStart records an RLM start event.
func (b *RLMCoreTrajectoryBridge) RecordRLMStart(query string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewRLMStartEvent(query)
	return b.RecordEvent(event)
}

// RecordAnalyze records an analysis event.
func (b *RLMCoreTrajectoryBridge) RecordAnalyze(depth uint32, analysis string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewAnalyzeEvent(depth, analysis)
	return b.RecordEvent(event)
}

// RecordREPLExec records a REPL execution event.
func (b *RLMCoreTrajectoryBridge) RecordREPLExec(depth uint32, code string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewREPLExecEvent(depth, code)
	return b.RecordEvent(event)
}

// RecordREPLResult records a REPL result event.
func (b *RLMCoreTrajectoryBridge) RecordREPLResult(depth uint32, result string, success bool) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewREPLResultEvent(depth, result, success)
	return b.RecordEvent(event)
}

// RecordReason records a reasoning event.
func (b *RLMCoreTrajectoryBridge) RecordReason(depth uint32, reasoning string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewReasonEvent(depth, reasoning)
	return b.RecordEvent(event)
}

// RecordRecurseStart records a recurse start event.
func (b *RLMCoreTrajectoryBridge) RecordRecurseStart(depth uint32, query string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewRecurseStartEvent(depth, query)
	return b.RecordEvent(event)
}

// RecordRecurseEnd records a recurse end event.
func (b *RLMCoreTrajectoryBridge) RecordRecurseEnd(depth uint32, result string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewRecurseEndEvent(depth, result)
	return b.RecordEvent(event)
}

// RecordFinalAnswer records a final answer event.
func (b *RLMCoreTrajectoryBridge) RecordFinalAnswer(depth uint32, answer string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewFinalAnswerEvent(depth, answer)
	return b.RecordEvent(event)
}

// RecordError records an error event.
func (b *RLMCoreTrajectoryBridge) RecordError(depth uint32, err string) error {
	if b == nil {
		return nil
	}
	event := rlmcore.NewErrorEvent(depth, err)
	return b.RecordEvent(event)
}
