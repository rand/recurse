package observability

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// SpanKind identifies the type of span.
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindClient
	SpanKindServer
)

func (k SpanKind) String() string {
	switch k {
	case SpanKindInternal:
		return "internal"
	case SpanKindClient:
		return "client"
	case SpanKindServer:
		return "server"
	default:
		return "unknown"
	}
}

// SpanStatus represents the status of a span.
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

func (s SpanStatus) String() string {
	switch s {
	case SpanStatusUnset:
		return "unset"
	case SpanStatusOK:
		return "ok"
	case SpanStatusError:
		return "error"
	default:
		return "unknown"
	}
}

// Span names for RLM operations.
const (
	SpanRLMCall        = "rlm.call"
	SpanRLMDecompose   = "rlm.decompose"
	SpanRLMSubcall     = "rlm.subcall"
	SpanRLMSynthesize  = "rlm.synthesize"
	SpanRLMMemory      = "rlm.memory"
	SpanLLMRequest     = "llm.request"
	SpanCacheCheck     = "cache.check"
	SpanBreakerCall    = "breaker.call"
	SpanAsyncExecute   = "async.execute"
	SpanAsyncOperation = "async.operation"
)

// Attribute keys.
const (
	AttrModel        = "model"
	AttrTokensInput  = "tokens.input"
	AttrTokensOutput = "tokens.output"
	AttrCacheHit     = "cache.hit"
	AttrDepth        = "depth"
	AttrOperationID  = "operation.id"
	AttrBreakerState = "breaker.state"
	AttrErrorType    = "error.type"
	AttrErrorMessage = "error.message"
)

// TraceID uniquely identifies a trace.
type TraceID [16]byte

// SpanID uniquely identifies a span within a trace.
type SpanID [8]byte

// SpanContext carries trace context across boundaries.
type SpanContext struct {
	TraceID TraceID
	SpanID  SpanID
	Sampled bool
}

// IsValid returns true if the context has valid IDs.
func (sc SpanContext) IsValid() bool {
	return sc.TraceID != TraceID{} && sc.SpanID != SpanID{}
}

// Span represents a unit of work in a trace.
type Span struct {
	name       string
	kind       SpanKind
	context    SpanContext
	parentID   SpanID
	startTime  time.Time
	endTime    time.Time
	status     SpanStatus
	statusMsg  string
	attributes map[string]any
	events     []SpanEvent
	ended      bool
	mu         sync.Mutex
	tracer     *Tracer
}

// SpanEvent represents a timestamped event within a span.
type SpanEvent struct {
	Name       string
	Timestamp  time.Time
	Attributes map[string]any
}

// SetAttribute sets an attribute on the span.
func (s *Span) SetAttribute(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	if s.attributes == nil {
		s.attributes = make(map[string]any)
	}
	s.attributes[key] = value
}

// SetAttributes sets multiple attributes.
func (s *Span) SetAttributes(attrs map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	if s.attributes == nil {
		s.attributes = make(map[string]any)
	}
	for k, v := range attrs {
		s.attributes[k] = v
	}
}

// AddEvent adds a timestamped event to the span.
func (s *Span) AddEvent(name string, attrs map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.events = append(s.events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

// SetStatus sets the span status.
func (s *Span) SetStatus(status SpanStatus, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.status = status
	s.statusMsg = message
}

// RecordError records an error on the span.
func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}
	s.SetStatus(SpanStatusError, err.Error())
	s.AddEvent("exception", map[string]any{
		AttrErrorType:    errorType(err),
		AttrErrorMessage: err.Error(),
	})
}

// End finishes the span.
func (s *Span) End() {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.endTime = time.Now()
	s.mu.Unlock()

	if s.tracer != nil {
		s.tracer.recordSpan(s)
	}
}

// Duration returns the span duration.
func (s *Span) Duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endTime.IsZero() {
		return time.Since(s.startTime)
	}
	return s.endTime.Sub(s.startTime)
}

// Context returns the span's context.
func (s *Span) Context() SpanContext {
	return s.context
}

// SpanData is an immutable snapshot of a span for export.
type SpanData struct {
	Name       string
	Kind       SpanKind
	Context    SpanContext
	ParentID   SpanID
	StartTime  time.Time
	EndTime    time.Time
	Duration   time.Duration
	Status     SpanStatus
	StatusMsg  string
	Attributes map[string]any
	Events     []SpanEvent
}

// ToData converts the span to immutable data.
func (s *Span) ToData() SpanData {
	s.mu.Lock()
	defer s.mu.Unlock()

	attrs := make(map[string]any, len(s.attributes))
	for k, v := range s.attributes {
		attrs[k] = v
	}

	events := make([]SpanEvent, len(s.events))
	copy(events, s.events)

	return SpanData{
		Name:       s.name,
		Kind:       s.kind,
		Context:    s.context,
		ParentID:   s.parentID,
		StartTime:  s.startTime,
		EndTime:    s.endTime,
		Duration:   s.endTime.Sub(s.startTime),
		Status:     s.status,
		StatusMsg:  s.statusMsg,
		Attributes: attrs,
		Events:     events,
	}
}

// Tracer creates and manages spans.
type Tracer struct {
	name       string
	sampler    Sampler
	spans      []SpanData
	spanCount  int64
	maxSpans   int
	onSpanEnd  func(SpanData)
	mu         sync.Mutex
}

// TracerOption configures a tracer.
type TracerOption func(*Tracer)

// WithSampler sets the sampling strategy.
func WithSampler(s Sampler) TracerOption {
	return func(t *Tracer) {
		t.sampler = s
	}
}

// WithMaxSpans sets the maximum spans to retain.
func WithMaxSpans(max int) TracerOption {
	return func(t *Tracer) {
		t.maxSpans = max
	}
}

// WithSpanCallback sets a callback for completed spans.
func WithSpanCallback(fn func(SpanData)) TracerOption {
	return func(t *Tracer) {
		t.onSpanEnd = fn
	}
}

// NewTracer creates a new tracer.
func NewTracer(name string, opts ...TracerOption) *Tracer {
	t := &Tracer{
		name:     name,
		sampler:  AlwaysSample(),
		maxSpans: 1000,
	}

	for _, opt := range opts {
		opt(t)
	}

	return t
}

// Start creates and starts a new span.
func (t *Tracer) Start(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	cfg := spanConfig{kind: SpanKindInternal}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Get parent context if any
	var parentCtx SpanContext
	var parentID SpanID
	if parent := SpanFromContext(ctx); parent != nil {
		parentCtx = parent.context
		parentID = parent.context.SpanID
	}

	// Generate new context
	spanCtx := SpanContext{
		TraceID: parentCtx.TraceID,
		SpanID:  generateSpanID(),
		Sampled: t.sampler.ShouldSample(name),
	}

	// New trace if no parent
	if spanCtx.TraceID == (TraceID{}) {
		spanCtx.TraceID = generateTraceID()
	}

	span := &Span{
		name:       name,
		kind:       cfg.kind,
		context:    spanCtx,
		parentID:   parentID,
		startTime:  time.Now(),
		attributes: cfg.attributes,
		tracer:     t,
	}

	atomic.AddInt64(&t.spanCount, 1)

	return contextWithSpan(ctx, span), span
}

// recordSpan records a completed span.
func (t *Tracer) recordSpan(s *Span) {
	if !s.context.Sampled {
		return
	}

	data := s.ToData()

	// Callback first (for external export)
	if t.onSpanEnd != nil {
		t.onSpanEnd(data)
	}

	// Store locally
	t.mu.Lock()
	defer t.mu.Unlock()

	t.spans = append(t.spans, data)

	// Trim if over limit
	if len(t.spans) > t.maxSpans {
		t.spans = t.spans[len(t.spans)-t.maxSpans/2:]
	}
}

// RecentSpans returns recent completed spans.
func (t *Tracer) RecentSpans(n int) []SpanData {
	t.mu.Lock()
	defer t.mu.Unlock()

	if n <= 0 || n > len(t.spans) {
		n = len(t.spans)
	}

	result := make([]SpanData, n)
	copy(result, t.spans[len(t.spans)-n:])
	return result
}

// SpanCount returns the total number of spans created.
func (t *Tracer) SpanCount() int64 {
	return atomic.LoadInt64(&t.spanCount)
}

// spanConfig holds span creation options.
type spanConfig struct {
	kind       SpanKind
	attributes map[string]any
}

// SpanOption configures span creation.
type SpanOption func(*spanConfig)

// WithSpanKind sets the span kind.
func WithSpanKind(kind SpanKind) SpanOption {
	return func(c *spanConfig) {
		c.kind = kind
	}
}

// WithSpanAttributes sets initial attributes.
func WithSpanAttributes(attrs map[string]any) SpanOption {
	return func(c *spanConfig) {
		c.attributes = attrs
	}
}

// Sampler determines which spans to sample.
type Sampler interface {
	ShouldSample(spanName string) bool
}

// alwaysSampler samples all spans.
type alwaysSampler struct{}

func (alwaysSampler) ShouldSample(string) bool { return true }

// AlwaysSample returns a sampler that samples all spans.
func AlwaysSample() Sampler { return alwaysSampler{} }

// neverSampler samples no spans.
type neverSampler struct{}

func (neverSampler) ShouldSample(string) bool { return false }

// NeverSample returns a sampler that samples no spans.
func NeverSample() Sampler { return neverSampler{} }

// ratioSampler samples a fraction of spans.
type ratioSampler struct {
	ratio   float64
	counter uint64
}

func (s *ratioSampler) ShouldSample(string) bool {
	c := atomic.AddUint64(&s.counter, 1)
	return float64(c%100)/100 < s.ratio
}

// RatioSampler returns a sampler that samples the given fraction.
func RatioSampler(ratio float64) Sampler {
	if ratio <= 0 {
		return NeverSample()
	}
	if ratio >= 1 {
		return AlwaysSample()
	}
	return &ratioSampler{ratio: ratio}
}

// Context key for spans.
type spanContextKey struct{}

// SpanFromContext returns the current span from context.
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	if span, ok := ctx.Value(spanContextKey{}).(*Span); ok {
		return span
	}
	return nil
}

// contextWithSpan returns a context with the span attached.
func contextWithSpan(ctx context.Context, span *Span) context.Context {
	return context.WithValue(ctx, spanContextKey{}, span)
}

// ID generation (simple implementation - production would use crypto/rand).
var (
	traceIDCounter uint64
	spanIDCounter  uint64
)

func generateTraceID() TraceID {
	c := atomic.AddUint64(&traceIDCounter, 1)
	t := time.Now().UnixNano()
	var id TraceID
	id[0] = byte(t >> 56)
	id[1] = byte(t >> 48)
	id[2] = byte(t >> 40)
	id[3] = byte(t >> 32)
	id[4] = byte(t >> 24)
	id[5] = byte(t >> 16)
	id[6] = byte(t >> 8)
	id[7] = byte(t)
	id[8] = byte(c >> 56)
	id[9] = byte(c >> 48)
	id[10] = byte(c >> 40)
	id[11] = byte(c >> 32)
	id[12] = byte(c >> 24)
	id[13] = byte(c >> 16)
	id[14] = byte(c >> 8)
	id[15] = byte(c)
	return id
}

func generateSpanID() SpanID {
	c := atomic.AddUint64(&spanIDCounter, 1)
	t := time.Now().UnixNano()
	var id SpanID
	id[0] = byte(t >> 24)
	id[1] = byte(t >> 16)
	id[2] = byte(t >> 8)
	id[3] = byte(t)
	id[4] = byte(c >> 24)
	id[5] = byte(c >> 16)
	id[6] = byte(c >> 8)
	id[7] = byte(c)
	return id
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	return "error"
}

// Global default tracer.
var defaultTracer = NewTracer("rlm")

// DefaultTracer returns the global default tracer.
func DefaultTracer() *Tracer {
	return defaultTracer
}

// StartSpan starts a span using the default tracer.
func StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, *Span) {
	return defaultTracer.Start(ctx, name, opts...)
}
