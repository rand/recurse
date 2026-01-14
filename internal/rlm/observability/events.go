package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// EventLevel represents the severity of an event.
type EventLevel int

const (
	LevelDebug EventLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l EventLevel) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// Event types for RLM operations.
const (
	EventCallStart      = "rlm.call.start"
	EventCallEnd        = "rlm.call.end"
	EventDecomposeStart = "rlm.decompose.start"
	EventDecomposeEnd   = "rlm.decompose.end"
	EventSubcallStart   = "rlm.subcall.start"
	EventSubcallEnd     = "rlm.subcall.end"
	EventSynthesizeEnd  = "rlm.synthesize.end"
	EventCacheHit       = "rlm.cache.hit"
	EventCacheMiss      = "rlm.cache.miss"
	EventBreakerTrip    = "rlm.breaker.trip"
	EventBreakerReset   = "rlm.breaker.reset"
	EventAsyncStart     = "rlm.async.start"
	EventAsyncComplete  = "rlm.async.complete"
	EventError          = "rlm.error"
	EventMemoryStore    = "rlm.memory.store"
	EventMemoryRetrieve = "rlm.memory.retrieve"
)

// Event represents a structured log event.
type Event struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     EventLevel     `json:"level"`
	Type      string         `json:"type"`
	Message   string         `json:"message,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	SpanID    string         `json:"span_id,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (e Event) MarshalJSON() ([]byte, error) {
	type alias Event
	return json.Marshal(&struct {
		Level string `json:"level"`
		alias
	}{
		Level: e.Level.String(),
		alias: alias(e),
	})
}

// EventLogger logs structured events.
type EventLogger struct {
	writer    io.Writer
	level     EventLevel
	fields    map[string]any // Default fields for all events
	tracer    *Tracer
	mu        sync.Mutex
	buffer    []Event
	maxBuffer int
}

// LoggerOption configures an event logger.
type LoggerOption func(*EventLogger)

// WithWriter sets the output writer.
func WithWriter(w io.Writer) LoggerOption {
	return func(l *EventLogger) {
		l.writer = w
	}
}

// WithLevel sets the minimum log level.
func WithLevel(level EventLevel) LoggerOption {
	return func(l *EventLogger) {
		l.level = level
	}
}

// WithDefaultFields sets default fields for all events.
func WithDefaultFields(fields map[string]any) LoggerOption {
	return func(l *EventLogger) {
		l.fields = fields
	}
}

// WithTracer links the logger to a tracer.
func WithTracerLink(t *Tracer) LoggerOption {
	return func(l *EventLogger) {
		l.tracer = t
	}
}

// WithBuffer enables event buffering.
func WithBuffer(size int) LoggerOption {
	return func(l *EventLogger) {
		l.maxBuffer = size
		l.buffer = make([]Event, 0, size)
	}
}

// NewEventLogger creates a new event logger.
func NewEventLogger(opts ...LoggerOption) *EventLogger {
	l := &EventLogger{
		writer: os.Stderr,
		level:  LevelInfo,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Log logs an event at the specified level.
func (l *EventLogger) Log(level EventLevel, eventType string, message string, fields map[string]any) {
	if level < l.level {
		return
	}

	event := Event{
		Timestamp: time.Now(),
		Level:     level,
		Type:      eventType,
		Message:   message,
		Fields:    l.mergeFields(fields),
	}

	l.emit(event)
}

// Debug logs a debug event.
func (l *EventLogger) Debug(eventType string, message string, fields map[string]any) {
	l.Log(LevelDebug, eventType, message, fields)
}

// Info logs an info event.
func (l *EventLogger) Info(eventType string, message string, fields map[string]any) {
	l.Log(LevelInfo, eventType, message, fields)
}

// Warn logs a warning event.
func (l *EventLogger) Warn(eventType string, message string, fields map[string]any) {
	l.Log(LevelWarn, eventType, message, fields)
}

// Error logs an error event.
func (l *EventLogger) Error(eventType string, message string, fields map[string]any) {
	l.Log(LevelError, eventType, message, fields)
}

// LogError logs an error with stack context.
func (l *EventLogger) LogError(err error, eventType string, fields map[string]any) {
	if err == nil {
		return
	}

	if fields == nil {
		fields = make(map[string]any)
	}
	fields["error"] = err.Error()

	l.Error(eventType, err.Error(), fields)
}

// WithSpan creates an event with trace context from a span.
func (l *EventLogger) WithSpan(span *Span) *SpanLogger {
	return &SpanLogger{
		logger: l,
		span:   span,
	}
}

// RecentEvents returns recent buffered events.
func (l *EventLogger) RecentEvents(n int) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.buffer == nil {
		return nil
	}

	if n <= 0 || n > len(l.buffer) {
		n = len(l.buffer)
	}

	result := make([]Event, n)
	copy(result, l.buffer[len(l.buffer)-n:])
	return result
}

// mergeFields merges event fields with default fields.
func (l *EventLogger) mergeFields(fields map[string]any) map[string]any {
	if len(l.fields) == 0 && len(fields) == 0 {
		return nil
	}

	merged := make(map[string]any, len(l.fields)+len(fields))
	for k, v := range l.fields {
		merged[k] = v
	}
	for k, v := range fields {
		merged[k] = v
	}
	return merged
}

// emit writes an event to the output and buffer.
func (l *EventLogger) emit(event Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Buffer if enabled
	if l.buffer != nil {
		l.buffer = append(l.buffer, event)
		if len(l.buffer) > l.maxBuffer {
			l.buffer = l.buffer[len(l.buffer)-l.maxBuffer/2:]
		}
	}

	// Write to output
	if l.writer != nil {
		data, err := json.Marshal(event)
		if err == nil {
			l.writer.Write(data)
			l.writer.Write([]byte("\n"))
		}
	}
}

// SpanLogger logs events with span context.
type SpanLogger struct {
	logger *EventLogger
	span   *Span
}

// Log logs an event with span context.
func (sl *SpanLogger) Log(level EventLevel, eventType string, message string, fields map[string]any) {
	if sl.span == nil {
		sl.logger.Log(level, eventType, message, fields)
		return
	}

	if fields == nil {
		fields = make(map[string]any)
	}

	ctx := sl.span.Context()
	fields["trace_id"] = fmt.Sprintf("%x", ctx.TraceID)
	fields["span_id"] = fmt.Sprintf("%x", ctx.SpanID)

	sl.logger.Log(level, eventType, message, fields)
}

// Debug logs a debug event with span context.
func (sl *SpanLogger) Debug(eventType string, message string, fields map[string]any) {
	sl.Log(LevelDebug, eventType, message, fields)
}

// Info logs an info event with span context.
func (sl *SpanLogger) Info(eventType string, message string, fields map[string]any) {
	sl.Log(LevelInfo, eventType, message, fields)
}

// Warn logs a warning event with span context.
func (sl *SpanLogger) Warn(eventType string, message string, fields map[string]any) {
	sl.Log(LevelWarn, eventType, message, fields)
}

// Error logs an error event with span context.
func (sl *SpanLogger) Error(eventType string, message string, fields map[string]any) {
	sl.Log(LevelError, eventType, message, fields)
}

// Global default logger.
var defaultLogger = NewEventLogger(WithBuffer(100))

// DefaultLogger returns the global default event logger.
func DefaultLogger() *EventLogger {
	return defaultLogger
}

// RLMLogger provides convenient logging for RLM operations.
type RLMLogger struct {
	logger *EventLogger
}

// NewRLMLogger creates an RLM-specific logger.
func NewRLMLogger(logger *EventLogger) *RLMLogger {
	if logger == nil {
		logger = defaultLogger
	}
	return &RLMLogger{logger: logger}
}

// CallStart logs the start of an RLM call.
func (l *RLMLogger) CallStart(taskID string, depth int) {
	l.logger.Info(EventCallStart, "RLM call started", map[string]any{
		"task_id": taskID,
		"depth":   depth,
	})
}

// CallEnd logs the end of an RLM call.
func (l *RLMLogger) CallEnd(taskID string, duration time.Duration, err error) {
	fields := map[string]any{
		"task_id":     taskID,
		"duration_ms": duration.Milliseconds(),
	}
	if err != nil {
		fields["error"] = err.Error()
		l.logger.Error(EventCallEnd, "RLM call failed", fields)
	} else {
		l.logger.Info(EventCallEnd, "RLM call completed", fields)
	}
}

// SubcallStart logs the start of a subcall.
func (l *RLMLogger) SubcallStart(operationID string, model string) {
	l.logger.Debug(EventSubcallStart, "Subcall started", map[string]any{
		"operation_id": operationID,
		"model":        model,
	})
}

// SubcallEnd logs the end of a subcall.
func (l *RLMLogger) SubcallEnd(operationID string, tokensIn, tokensOut int, duration time.Duration) {
	l.logger.Debug(EventSubcallEnd, "Subcall completed", map[string]any{
		"operation_id": operationID,
		"tokens_in":    tokensIn,
		"tokens_out":   tokensOut,
		"duration_ms":  duration.Milliseconds(),
	})
}

// CacheHit logs a cache hit.
func (l *RLMLogger) CacheHit(sessionID string, tokensSaved int) {
	l.logger.Debug(EventCacheHit, "Cache hit", map[string]any{
		"session_id":   sessionID,
		"tokens_saved": tokensSaved,
	})
}

// CacheMiss logs a cache miss.
func (l *RLMLogger) CacheMiss(sessionID string) {
	l.logger.Debug(EventCacheMiss, "Cache miss", map[string]any{
		"session_id": sessionID,
	})
}

// BreakerTrip logs a circuit breaker trip.
func (l *RLMLogger) BreakerTrip(tier string, failures int) {
	l.logger.Warn(EventBreakerTrip, "Circuit breaker tripped", map[string]any{
		"tier":     tier,
		"failures": failures,
	})
}

// BreakerReset logs a circuit breaker reset.
func (l *RLMLogger) BreakerReset(tier string) {
	l.logger.Info(EventBreakerReset, "Circuit breaker reset", map[string]any{
		"tier": tier,
	})
}

// AsyncStart logs the start of async execution.
func (l *RLMLogger) AsyncStart(planID string, operations int, parallel int) {
	l.logger.Debug(EventAsyncStart, "Async execution started", map[string]any{
		"plan_id":    planID,
		"operations": operations,
		"parallel":   parallel,
	})
}

// AsyncComplete logs async execution completion.
func (l *RLMLogger) AsyncComplete(planID string, succeeded, failed int, duration time.Duration) {
	level := LevelDebug
	if failed > 0 {
		level = LevelWarn
	}
	l.logger.Log(level, EventAsyncComplete, "Async execution completed", map[string]any{
		"plan_id":     planID,
		"succeeded":   succeeded,
		"failed":      failed,
		"duration_ms": duration.Milliseconds(),
	})
}

// MemoryStore logs a memory store operation.
func (l *RLMLogger) MemoryStore(nodeType string, nodeID string) {
	l.logger.Debug(EventMemoryStore, "Memory stored", map[string]any{
		"node_type": nodeType,
		"node_id":   nodeID,
	})
}

// MemoryRetrieve logs a memory retrieval.
func (l *RLMLogger) MemoryRetrieve(query string, results int, duration time.Duration) {
	l.logger.Debug(EventMemoryRetrieve, "Memory retrieved", map[string]any{
		"query":       query,
		"results":     results,
		"duration_ms": duration.Milliseconds(),
	})
}

// Error logs an RLM error.
func (l *RLMLogger) Error(operation string, err error, fields map[string]any) {
	if fields == nil {
		fields = make(map[string]any)
	}
	fields["operation"] = operation
	l.logger.LogError(err, EventError, fields)
}
