package rlm

import (
	"sync"
	"time"

	"github.com/rand/recurse/internal/tui/components/dialogs/rlmtrace"
)

// TraceProvider implements rlmtrace.TraceProvider for the RLM controller.
type TraceProvider struct {
	mu     sync.RWMutex
	events []rlmtrace.TraceEvent
	stats  rlmtrace.TraceStats
	maxLen int
}

// NewTraceProvider creates a new trace provider.
func NewTraceProvider(maxEvents int) *TraceProvider {
	if maxEvents <= 0 {
		maxEvents = 1000
	}
	return &TraceProvider{
		events: make([]rlmtrace.TraceEvent, 0, maxEvents),
		maxLen: maxEvents,
		stats: rlmtrace.TraceStats{
			EventsByType: make(map[rlmtrace.TraceEventType]int),
		},
	}
}

// RecordEvent implements TraceRecorder interface for the RLM controller.
func (p *TraceProvider) RecordEvent(event TraceEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Convert internal event to rlmtrace event
	traceEvent := rlmtrace.TraceEvent{
		ID:        event.ID,
		Type:      mapEventType(event.Type),
		Action:    event.Action,
		Details:   event.Details,
		Tokens:    event.Tokens,
		Duration:  event.Duration,
		Timestamp: event.Timestamp,
		Depth:     event.Depth,
		ParentID:  event.ParentID,
		Status:    event.Status,
	}

	// Add to events list
	p.events = append(p.events, traceEvent)
	if len(p.events) > p.maxLen {
		p.events = p.events[len(p.events)-p.maxLen:]
	}

	// Update stats
	p.stats.TotalEvents++
	p.stats.TotalTokens += event.Tokens
	p.stats.TotalDuration += event.Duration
	if event.Depth > p.stats.MaxDepth {
		p.stats.MaxDepth = event.Depth
	}
	p.stats.EventsByType[traceEvent.Type]++

	return nil
}

// GetEvents implements rlmtrace.TraceProvider.
func (p *TraceProvider) GetEvents(limit int) ([]rlmtrace.TraceEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if limit <= 0 || limit > len(p.events) {
		limit = len(p.events)
	}

	// Return most recent events
	start := len(p.events) - limit
	if start < 0 {
		start = 0
	}

	result := make([]rlmtrace.TraceEvent, limit)
	copy(result, p.events[start:])
	return result, nil
}

// GetEvent implements rlmtrace.TraceProvider.
func (p *TraceProvider) GetEvent(id string) (*rlmtrace.TraceEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for i := range p.events {
		if p.events[i].ID == id {
			event := p.events[i]
			return &event, nil
		}
	}
	return nil, nil
}

// ClearEvents implements rlmtrace.TraceProvider.
func (p *TraceProvider) ClearEvents() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.events = p.events[:0]
	p.stats = rlmtrace.TraceStats{
		EventsByType: make(map[rlmtrace.TraceEventType]int),
	}
	return nil
}

// Stats implements rlmtrace.TraceProvider.
func (p *TraceProvider) Stats() rlmtrace.TraceStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Return a copy to avoid race conditions
	statsCopy := rlmtrace.TraceStats{
		TotalEvents:   p.stats.TotalEvents,
		TotalTokens:   p.stats.TotalTokens,
		TotalDuration: p.stats.TotalDuration,
		MaxDepth:      p.stats.MaxDepth,
		EventsByType:  make(map[rlmtrace.TraceEventType]int),
	}
	for k, v := range p.stats.EventsByType {
		statsCopy.EventsByType[k] = v
	}
	return statsCopy
}

// GetEventsByType returns events of a specific type.
func (p *TraceProvider) GetEventsByType(eventType rlmtrace.TraceEventType, limit int) []rlmtrace.TraceEvent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []rlmtrace.TraceEvent
	for i := len(p.events) - 1; i >= 0 && len(result) < limit; i-- {
		if p.events[i].Type == eventType {
			result = append(result, p.events[i])
		}
	}
	return result
}

// GetEventsByParent returns child events of a parent.
func (p *TraceProvider) GetEventsByParent(parentID string) []rlmtrace.TraceEvent {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []rlmtrace.TraceEvent
	for _, event := range p.events {
		if event.ParentID == parentID {
			result = append(result, event)
		}
	}
	return result
}

// GetRecentDuration returns the total duration of recent events.
func (p *TraceProvider) GetRecentDuration(since time.Time) time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var total time.Duration
	for _, event := range p.events {
		if event.Timestamp.After(since) {
			total += event.Duration
		}
	}
	return total
}

// mapEventType converts internal event type to rlmtrace type.
func mapEventType(eventType string) rlmtrace.TraceEventType {
	switch eventType {
	case "decision", "DIRECT":
		return rlmtrace.EventDecision
	case "DECOMPOSE":
		return rlmtrace.EventDecompose
	case "SUBCALL":
		return rlmtrace.EventSubcall
	case "SYNTHESIZE":
		return rlmtrace.EventSynthesize
	case "MEMORY_QUERY":
		return rlmtrace.EventMemoryQuery
	case "execute":
		return rlmtrace.EventExecute
	default:
		return rlmtrace.EventDecision
	}
}

// Ensure TraceProvider implements rlmtrace.TraceProvider
var _ rlmtrace.TraceProvider = (*TraceProvider)(nil)
