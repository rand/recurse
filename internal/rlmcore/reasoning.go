package rlmcore

import (
	core "github.com/rand/rlm-core/go/rlmcore"
)

// ReasoningTrace wraps the rlm-core ReasoningTrace.
// Captures the provenance of decisions during reasoning using Deciduous-style traces.
type ReasoningTrace struct {
	inner *core.ReasoningTrace
}

// TraceStats contains statistics about a reasoning trace.
type TraceStats = core.TraceStats

// DecisionResult contains the ID of the chosen option after logging a decision.
type DecisionResult = core.DecisionResult

// ActionResult contains the IDs of nodes created when logging an action.
type ActionResult = core.ActionResult

// TraceAnalysis contains the results of analyzing a trace.
type TraceAnalysis = core.TraceAnalysis

// NewReasoningTrace creates a new reasoning trace for tracking decisions.
// goal: The objective being reasoned about
// sessionID: Optional session identifier (can be empty)
func NewReasoningTrace(goal, sessionID string) (*ReasoningTrace, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewReasoningTrace(goal, sessionID)
	return &ReasoningTrace{inner: inner}, nil
}

// Free releases the reasoning trace resources.
func (t *ReasoningTrace) Free() {
	if t.inner != nil {
		t.inner.Free()
		t.inner = nil
	}
}

// ID returns the trace's unique identifier.
func (t *ReasoningTrace) ID() (string, error) {
	return t.inner.ID()
}

// LogDecision logs a decision point with multiple options.
// question: The decision being made
// options: Available choices
// chosenIndex: Index of the selected option (0-based)
// rationale: Explanation for the choice
func (t *ReasoningTrace) LogDecision(question string, options []string, chosenIndex int, rationale string) (*DecisionResult, error) {
	return t.inner.LogDecision(question, options, chosenIndex, rationale)
}

// LogAction logs an action taken and its outcome.
// actionDescription: What was done
// outcomeDescription: What resulted
// parentID: Optional parent decision node ID (can be empty)
func (t *ReasoningTrace) LogAction(actionDescription, outcomeDescription, parentID string) (*ActionResult, error) {
	return t.inner.LogAction(actionDescription, outcomeDescription, parentID)
}

// LinkCommit associates the trace with a git commit.
func (t *ReasoningTrace) LinkCommit(commitSHA string) error {
	return t.inner.LinkCommit(commitSHA)
}

// Stats returns statistics about the trace.
func (t *ReasoningTrace) Stats() (*TraceStats, error) {
	return t.inner.Stats()
}

// ToJSON exports the trace to JSON format.
func (t *ReasoningTrace) ToJSON() (string, error) {
	return t.inner.ToJSON()
}

// ToMermaid exports the trace to Mermaid flowchart format.
func (t *ReasoningTrace) ToMermaid() (string, error) {
	return t.inner.ToMermaid()
}

// Analyze runs analysis on the trace and returns insights.
func (t *ReasoningTrace) Analyze() (*TraceAnalysis, error) {
	return t.inner.Analyze()
}

// ReasoningTraceStore wraps the rlm-core ReasoningTraceStore.
// Provides persistence for reasoning traces.
type ReasoningTraceStore struct {
	inner *core.ReasoningTraceStore
}

// TraceStoreStats contains statistics about the trace store.
type TraceStoreStats = core.TraceStoreStats

// NewReasoningTraceStoreInMemory creates an in-memory trace store.
func NewReasoningTraceStoreInMemory() (*ReasoningTraceStore, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner := core.NewReasoningTraceStoreInMemory()
	return &ReasoningTraceStore{inner: inner}, nil
}

// OpenReasoningTraceStore opens a file-backed trace store.
func OpenReasoningTraceStore(path string) (*ReasoningTraceStore, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	inner, err := core.OpenReasoningTraceStore(path)
	if err != nil {
		return nil, err
	}
	return &ReasoningTraceStore{inner: inner}, nil
}

// Free releases the trace store resources.
func (s *ReasoningTraceStore) Free() {
	if s.inner != nil {
		s.inner.Free()
		s.inner = nil
	}
}

// Save persists a trace to the store.
func (s *ReasoningTraceStore) Save(trace *ReasoningTrace) error {
	return s.inner.Save(trace.inner)
}

// Load retrieves a trace from the store by ID.
func (s *ReasoningTraceStore) Load(traceID string) (*ReasoningTrace, error) {
	inner, err := s.inner.Load(traceID)
	if err != nil {
		return nil, err
	}
	return &ReasoningTrace{inner: inner}, nil
}

// FindBySession finds all traces associated with a session.
func (s *ReasoningTraceStore) FindBySession(sessionID string) ([]string, error) {
	return s.inner.FindBySession(sessionID)
}

// FindByCommit finds all traces linked to a git commit.
func (s *ReasoningTraceStore) FindByCommit(commit string) ([]string, error) {
	return s.inner.FindByCommit(commit)
}

// Stats returns statistics about the store.
func (s *ReasoningTraceStore) Stats() (*TraceStoreStats, error) {
	return s.inner.Stats()
}
