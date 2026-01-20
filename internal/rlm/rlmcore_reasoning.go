package rlm

// RLMCoreReasoningBridge provides a bridge between the Go ToT/LATS modules
// and rlm-core's Deciduous-style reasoning traces. When enabled (RLM_USE_CORE=true),
// it provides provenance tracking via the rlm-core Rust implementation.
//
// The Go ToT/LATS modules handle:
// - Tree of Thoughts: Deliberate reasoning with BFS/DFS/Best-First exploration
// - LATS: MCTS-based tool orchestration with value backpropagation
//
// The rlm-core reasoning module provides:
// - ReasoningTrace: Deciduous-style decision tree capture
// - TraceAnalyzer: Confidence scoring and narrative generation
// - TraceStore: Persistence and querying of traces
//
// This bridge integrates them by recording ToT/LATS decisions into rlm-core
// traces for provenance, explainability, and git commit linkage.

import (
	"errors"
	"log/slog"

	"github.com/rand/recurse/internal/rlmcore"
)

// RLMCoreReasoningBridge wraps rlm-core reasoning functions for use in ToT/LATS.
type RLMCoreReasoningBridge struct {
	store        *rlmcore.ReasoningTraceStore
	activeTraces map[string]*rlmcore.ReasoningTrace
}

// ReasoningTrace is an alias for the rlmcore type.
type ReasoningTrace = rlmcore.ReasoningTrace

// TraceStats is an alias for the rlmcore type.
type TraceStats = rlmcore.TraceStats

// DecisionResult is an alias for the rlmcore type.
type DecisionResult = rlmcore.DecisionResult

// ActionResult is an alias for the rlmcore type.
type ActionResult = rlmcore.ActionResult

// TraceAnalysis is an alias for the rlmcore type.
type TraceAnalysis = rlmcore.TraceAnalysis

// TraceStoreStats is an alias for the rlmcore type.
type TraceStoreStats = rlmcore.TraceStoreStats

// NewRLMCoreReasoningBridge creates a new reasoning bridge using rlm-core.
// Returns nil, nil if rlm-core is not available.
func NewRLMCoreReasoningBridge() (*RLMCoreReasoningBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	store, err := rlmcore.NewReasoningTraceStoreInMemory()
	if err != nil {
		return nil, err
	}

	slog.Info("rlm-core reasoning bridge initialized")
	return &RLMCoreReasoningBridge{
		store:        store,
		activeTraces: make(map[string]*rlmcore.ReasoningTrace),
	}, nil
}

// NewRLMCoreReasoningBridgeWithStore creates a bridge with a file-backed store.
func NewRLMCoreReasoningBridgeWithStore(dbPath string) (*RLMCoreReasoningBridge, error) {
	if !rlmcore.Available() {
		return nil, nil
	}

	store, err := rlmcore.OpenReasoningTraceStore(dbPath)
	if err != nil {
		return nil, err
	}

	slog.Info("rlm-core reasoning bridge initialized", "db", dbPath)
	return &RLMCoreReasoningBridge{
		store:        store,
		activeTraces: make(map[string]*rlmcore.ReasoningTrace),
	}, nil
}

// StartTrace begins a new reasoning trace for a goal.
// Returns the trace ID for future reference.
func (b *RLMCoreReasoningBridge) StartTrace(goal, sessionID string) (string, error) {
	trace, err := rlmcore.NewReasoningTrace(goal, sessionID)
	if err != nil {
		return "", err
	}

	traceID, err := trace.ID()
	if err != nil {
		trace.Free()
		return "", err
	}

	b.activeTraces[traceID] = trace
	slog.Debug("started reasoning trace", "trace_id", traceID, "goal", goal)
	return traceID, nil
}

// LogDecision records a decision point in an active trace.
// This integrates with ToT thought node expansion.
func (b *RLMCoreReasoningBridge) LogDecision(traceID, question string, options []string, chosenIndex int, rationale string) (*DecisionResult, error) {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return nil, ErrTraceNotFound
	}
	return trace.LogDecision(question, options, chosenIndex, rationale)
}

// LogAction records an action and its outcome in an active trace.
// This integrates with LATS tool execution.
func (b *RLMCoreReasoningBridge) LogAction(traceID, action, outcome, parentID string) (*ActionResult, error) {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return nil, ErrTraceNotFound
	}
	return trace.LogAction(action, outcome, parentID)
}

// LinkCommit associates a trace with a git commit.
func (b *RLMCoreReasoningBridge) LinkCommit(traceID, commitSHA string) error {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return ErrTraceNotFound
	}
	return trace.LinkCommit(commitSHA)
}

// GetTraceStats returns statistics for an active trace.
func (b *RLMCoreReasoningBridge) GetTraceStats(traceID string) (*TraceStats, error) {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return nil, ErrTraceNotFound
	}
	return trace.Stats()
}

// AnalyzeTrace runs analysis on an active trace.
func (b *RLMCoreReasoningBridge) AnalyzeTrace(traceID string) (*TraceAnalysis, error) {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return nil, ErrTraceNotFound
	}
	return trace.Analyze()
}

// ExportTraceJSON exports an active trace to JSON format.
func (b *RLMCoreReasoningBridge) ExportTraceJSON(traceID string) (string, error) {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return "", ErrTraceNotFound
	}
	return trace.ToJSON()
}

// ExportTraceMermaid exports an active trace to Mermaid flowchart format.
func (b *RLMCoreReasoningBridge) ExportTraceMermaid(traceID string) (string, error) {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return "", ErrTraceNotFound
	}
	return trace.ToMermaid()
}

// FinishTrace completes a trace and persists it to the store.
func (b *RLMCoreReasoningBridge) FinishTrace(traceID string) error {
	trace, ok := b.activeTraces[traceID]
	if !ok {
		return ErrTraceNotFound
	}

	if err := b.store.Save(trace); err != nil {
		return err
	}

	delete(b.activeTraces, traceID)
	trace.Free()
	slog.Debug("finished reasoning trace", "trace_id", traceID)
	return nil
}

// LoadTrace retrieves a persisted trace from the store.
func (b *RLMCoreReasoningBridge) LoadTrace(traceID string) (*ReasoningTrace, error) {
	return b.store.Load(traceID)
}

// FindTracesBySession finds all traces for a session.
func (b *RLMCoreReasoningBridge) FindTracesBySession(sessionID string) ([]string, error) {
	return b.store.FindBySession(sessionID)
}

// FindTracesByCommit finds all traces linked to a git commit.
func (b *RLMCoreReasoningBridge) FindTracesByCommit(commitSHA string) ([]string, error) {
	return b.store.FindByCommit(commitSHA)
}

// GetStoreStats returns statistics about the trace store.
func (b *RLMCoreReasoningBridge) GetStoreStats() (*TraceStoreStats, error) {
	return b.store.Stats()
}

// Close releases all resources.
func (b *RLMCoreReasoningBridge) Close() error {
	// Free any active traces that weren't finished
	for traceID, trace := range b.activeTraces {
		slog.Warn("closing unfinished trace", "trace_id", traceID)
		trace.Free()
	}
	b.activeTraces = nil

	if b.store != nil {
		b.store.Free()
	}
	return nil
}

// UseRLMCoreReasoning returns true if rlm-core reasoning should be used.
func UseRLMCoreReasoning() bool {
	return rlmcore.Available()
}

// ErrTraceNotFound is returned when a trace ID is not found in active traces.
var ErrTraceNotFound = errors.New("reasoning trace not found")
