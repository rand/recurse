package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Metrics tests

func TestCounter(t *testing.T) {
	c := &Counter{}
	assert.Equal(t, int64(0), c.Value())

	c.Inc()
	assert.Equal(t, int64(1), c.Value())

	c.Add(5)
	assert.Equal(t, int64(6), c.Value())
}

func TestGauge(t *testing.T) {
	g := &Gauge{}
	assert.Equal(t, int64(0), g.Value())

	g.Set(10)
	assert.Equal(t, int64(10), g.Value())

	g.Inc()
	assert.Equal(t, int64(11), g.Value())

	g.Dec()
	assert.Equal(t, int64(10), g.Value())

	g.Add(-5)
	assert.Equal(t, int64(5), g.Value())
}

func TestHistogram(t *testing.T) {
	h := NewHistogram([]float64{0.1, 0.5, 1.0}, nil)

	h.Observe(0.05) // bucket 0
	h.Observe(0.3)  // bucket 1
	h.Observe(0.8)  // bucket 2
	h.Observe(2.0)  // infinity bucket

	snap := h.Snapshot()
	assert.Equal(t, int64(4), snap.Count)
	assert.InDelta(t, 3.15, snap.Sum, 0.001)
	assert.Equal(t, int64(1), snap.Counts[0]) // <= 0.1
	assert.Equal(t, int64(1), snap.Counts[1]) // <= 0.5
	assert.Equal(t, int64(1), snap.Counts[2]) // <= 1.0
	assert.Equal(t, int64(1), snap.Counts[3]) // > 1.0 (infinity)
}

func TestHistogram_ObserveDuration(t *testing.T) {
	h := NewHistogram(DefaultBuckets, nil)
	start := time.Now()
	time.Sleep(10 * time.Millisecond)
	h.ObserveDuration(start)

	snap := h.Snapshot()
	assert.Equal(t, int64(1), snap.Count)
	assert.Greater(t, snap.Sum, 0.01) // At least 10ms
}

func TestHistogramSnapshot_Mean(t *testing.T) {
	h := NewHistogram(nil, nil)
	h.Observe(1.0)
	h.Observe(2.0)
	h.Observe(3.0)

	snap := h.Snapshot()
	assert.InDelta(t, 2.0, snap.Mean(), 0.001)
}

func TestHistogramSnapshot_Percentile(t *testing.T) {
	h := NewHistogram([]float64{1, 2, 3, 4, 5}, nil)
	for i := 0; i < 100; i++ {
		h.Observe(float64(i%5) + 0.5) // Values 0.5-4.5
	}

	snap := h.Snapshot()
	p50 := snap.Percentile(50)
	assert.Greater(t, p50, 0.0)
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	// Counter
	c1 := reg.Counter("test_counter", nil)
	c1.Inc()
	c2 := reg.Counter("test_counter", nil)
	assert.Same(t, c1, c2)
	assert.Equal(t, int64(1), c2.Value())

	// Counter with labels
	c3 := reg.Counter("test_counter", Labels{"env": "prod"})
	assert.NotSame(t, c1, c3)

	// Gauge
	g := reg.Gauge("test_gauge", nil)
	g.Set(42)
	assert.Equal(t, int64(42), g.Value())

	// Histogram
	h := reg.Histogram("test_histogram", nil, nil)
	h.Observe(1.5)
	assert.Equal(t, int64(1), h.Snapshot().Count)
}

func TestRegistry_Snapshot(t *testing.T) {
	reg := NewRegistry()

	reg.Counter("c1", nil).Add(10)
	reg.Counter("c2", Labels{"a": "b"}).Add(20)
	reg.Gauge("g1", nil).Set(30)
	reg.Histogram("h1", nil, nil).Observe(1.0)

	snap := reg.Snapshot()
	assert.Equal(t, int64(10), snap.Counters["c1"])
	assert.Equal(t, int64(20), snap.Counters["c2,a=b"])
	assert.Equal(t, int64(30), snap.Gauges["g1"])
	assert.Equal(t, int64(1), snap.Histograms["h1"].Count)
}

func TestRLMMetrics(t *testing.T) {
	reg := NewRegistry()
	m := NewRLMMetrics(reg)

	m.RecordCall(100 * time.Millisecond)
	m.RecordSubcall()
	m.RecordTokens(100, 50)
	m.RecordCacheHit()
	m.RecordCacheHit()
	m.RecordCacheMiss()
	m.RecordAsyncExecution(4)

	assert.Equal(t, int64(1), m.callsTotal.Value())
	assert.Equal(t, int64(1), m.subcallsTotal.Value())
	assert.Equal(t, int64(100), m.tokensInput.Value())
	assert.Equal(t, int64(50), m.tokensOutput.Value())
	assert.Equal(t, int64(2), m.cacheHits.Value())
	assert.Equal(t, int64(1), m.cacheMisses.Value())
	assert.InDelta(t, 0.667, m.CacheHitRate(), 0.01)
}

func TestMetrics_Concurrent(t *testing.T) {
	reg := NewRegistry()
	c := reg.Counter("concurrent", nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Inc()
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(100), c.Value())
}

// Tracer tests

func TestTracer_StartSpan(t *testing.T) {
	tracer := NewTracer("test")
	ctx, span := tracer.Start(context.Background(), "test.span")

	assert.NotNil(t, span)
	assert.NotNil(t, ctx)
	assert.True(t, span.Context().IsValid())

	span.End()
}

func TestSpan_Attributes(t *testing.T) {
	tracer := NewTracer("test")
	_, span := tracer.Start(context.Background(), "test.span")

	span.SetAttribute("key1", "value1")
	span.SetAttributes(map[string]any{
		"key2": 42,
		"key3": true,
	})

	data := span.ToData()
	assert.Equal(t, "value1", data.Attributes["key1"])
	assert.Equal(t, 42, data.Attributes["key2"])
	assert.Equal(t, true, data.Attributes["key3"])

	span.End()
}

func TestSpan_Events(t *testing.T) {
	tracer := NewTracer("test")
	_, span := tracer.Start(context.Background(), "test.span")

	span.AddEvent("event1", map[string]any{"data": "value"})
	span.AddEvent("event2", nil)

	data := span.ToData()
	assert.Len(t, data.Events, 2)
	assert.Equal(t, "event1", data.Events[0].Name)
	assert.Equal(t, "value", data.Events[0].Attributes["data"])

	span.End()
}

func TestSpan_Status(t *testing.T) {
	tracer := NewTracer("test")
	_, span := tracer.Start(context.Background(), "test.span")

	span.SetStatus(SpanStatusOK, "success")
	data := span.ToData()
	assert.Equal(t, SpanStatusOK, data.Status)

	span.End()
}

func TestSpan_RecordError(t *testing.T) {
	tracer := NewTracer("test")
	_, span := tracer.Start(context.Background(), "test.span")

	err := errors.New("test error")
	span.RecordError(err)

	data := span.ToData()
	assert.Equal(t, SpanStatusError, data.Status)
	assert.Len(t, data.Events, 1)
	assert.Equal(t, "exception", data.Events[0].Name)

	span.End()
}

func TestSpan_Duration(t *testing.T) {
	tracer := NewTracer("test")
	_, span := tracer.Start(context.Background(), "test.span")

	time.Sleep(10 * time.Millisecond)
	span.End()

	assert.GreaterOrEqual(t, span.Duration(), 10*time.Millisecond)
}

func TestTracer_ParentChild(t *testing.T) {
	tracer := NewTracer("test")

	ctx, parent := tracer.Start(context.Background(), "parent")
	_, child := tracer.Start(ctx, "child")

	// Child should share trace ID but have different span ID
	assert.Equal(t, parent.Context().TraceID, child.Context().TraceID)
	assert.NotEqual(t, parent.Context().SpanID, child.Context().SpanID)
	assert.Equal(t, parent.Context().SpanID, child.parentID)

	child.End()
	parent.End()
}

func TestSpanFromContext(t *testing.T) {
	tracer := NewTracer("test")

	// No span in context
	assert.Nil(t, SpanFromContext(context.Background()))

	// Span in context
	ctx, span := tracer.Start(context.Background(), "test")
	found := SpanFromContext(ctx)
	assert.Same(t, span, found)

	span.End()
}

func TestTracer_RecentSpans(t *testing.T) {
	tracer := NewTracer("test", WithMaxSpans(10))

	for i := 0; i < 5; i++ {
		_, span := tracer.Start(context.Background(), "span")
		span.End()
	}

	spans := tracer.RecentSpans(3)
	assert.Len(t, spans, 3)
}

func TestTracer_SpanCallback(t *testing.T) {
	var captured []SpanData
	tracer := NewTracer("test", WithSpanCallback(func(data SpanData) {
		captured = append(captured, data)
	}))

	_, span := tracer.Start(context.Background(), "test.span")
	span.SetAttribute("key", "value")
	span.End()

	require.Len(t, captured, 1)
	assert.Equal(t, "test.span", captured[0].Name)
	assert.Equal(t, "value", captured[0].Attributes["key"])
}

func TestSamplers(t *testing.T) {
	// Always sample
	always := AlwaysSample()
	assert.True(t, always.ShouldSample("any"))

	// Never sample
	never := NeverSample()
	assert.False(t, never.ShouldSample("any"))

	// Ratio sampler
	ratio := RatioSampler(0.5)
	sampled := 0
	for i := 0; i < 100; i++ {
		if ratio.ShouldSample("test") {
			sampled++
		}
	}
	// Should be roughly 50%, allow wide margin
	assert.Greater(t, sampled, 30)
	assert.Less(t, sampled, 70)
}

func TestSpanKind_String(t *testing.T) {
	assert.Equal(t, "internal", SpanKindInternal.String())
	assert.Equal(t, "client", SpanKindClient.String())
	assert.Equal(t, "server", SpanKindServer.String())
}

func TestSpanStatus_String(t *testing.T) {
	assert.Equal(t, "unset", SpanStatusUnset.String())
	assert.Equal(t, "ok", SpanStatusOK.String())
	assert.Equal(t, "error", SpanStatusError.String())
}

// Events tests

func TestEventLevel_String(t *testing.T) {
	assert.Equal(t, "debug", LevelDebug.String())
	assert.Equal(t, "info", LevelInfo.String())
	assert.Equal(t, "warn", LevelWarn.String())
	assert.Equal(t, "error", LevelError.String())
}

func TestEventLogger_Log(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(WithWriter(&buf), WithLevel(LevelDebug))

	logger.Info("test.event", "test message", map[string]any{"key": "value"})

	var event map[string]any
	err := json.Unmarshal(buf.Bytes(), &event)
	require.NoError(t, err)

	assert.Equal(t, "info", event["level"])
	assert.Equal(t, "test.event", event["type"])
	assert.Equal(t, "test message", event["message"])
	assert.Equal(t, "value", event["fields"].(map[string]any)["key"])
}

func TestEventLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(WithWriter(&buf), WithLevel(LevelWarn))

	logger.Debug("debug", "debug msg", nil)
	logger.Info("info", "info msg", nil)
	logger.Warn("warn", "warn msg", nil)
	logger.Error("error", "error msg", nil)

	// Should only have warn and error
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	assert.Len(t, lines, 2)
}

func TestEventLogger_DefaultFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(
		WithWriter(&buf),
		WithDefaultFields(map[string]any{"service": "rlm", "version": "1.0"}),
	)

	logger.Info("test", "msg", map[string]any{"custom": "field"})

	var event map[string]any
	err := json.Unmarshal(buf.Bytes(), &event)
	require.NoError(t, err)

	fields := event["fields"].(map[string]any)
	assert.Equal(t, "rlm", fields["service"])
	assert.Equal(t, "1.0", fields["version"])
	assert.Equal(t, "field", fields["custom"])
}

func TestEventLogger_Buffer(t *testing.T) {
	logger := NewEventLogger(WithBuffer(10), WithWriter(nil))

	for i := 0; i < 5; i++ {
		logger.Info("test", "msg", map[string]any{"i": i})
	}

	events := logger.RecentEvents(3)
	assert.Len(t, events, 3)
}

func TestEventLogger_LogError(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(WithWriter(&buf))

	err := errors.New("test error")
	logger.LogError(err, "test.error", map[string]any{"context": "value"})

	var event map[string]any
	jsonErr := json.Unmarshal(buf.Bytes(), &event)
	require.NoError(t, jsonErr)

	assert.Equal(t, "error", event["level"])
	fields := event["fields"].(map[string]any)
	assert.Equal(t, "test error", fields["error"])
	assert.Equal(t, "value", fields["context"])
}

func TestSpanLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(WithWriter(&buf))
	tracer := NewTracer("test")

	_, span := tracer.Start(context.Background(), "test.span")
	spanLogger := logger.WithSpan(span)

	spanLogger.Info("test.event", "with span", nil)
	span.End()

	var event map[string]any
	err := json.Unmarshal(buf.Bytes(), &event)
	require.NoError(t, err)

	fields := event["fields"].(map[string]any)
	assert.Contains(t, fields, "trace_id")
	assert.Contains(t, fields, "span_id")
}

func TestRLMLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(WithWriter(&buf), WithLevel(LevelDebug))
	rlmLog := NewRLMLogger(logger)

	rlmLog.CallStart("task-1", 0)
	rlmLog.SubcallStart("op-1", "haiku")
	rlmLog.SubcallEnd("op-1", 100, 50, 50*time.Millisecond)
	rlmLog.CacheHit("session-1", 500)
	rlmLog.CacheMiss("session-2")
	rlmLog.BreakerTrip("sonnet", 5)
	rlmLog.BreakerReset("sonnet")
	rlmLog.AsyncStart("plan-1", 4, 2)
	rlmLog.AsyncComplete("plan-1", 3, 1, 100*time.Millisecond)
	rlmLog.CallEnd("task-1", 200*time.Millisecond, nil)

	// Should have 10 log lines
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	assert.Len(t, lines, 10)
}

func TestRLMLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	logger := NewEventLogger(WithWriter(&buf))
	rlmLog := NewRLMLogger(logger)

	rlmLog.Error("decompose", errors.New("failed to decompose"), map[string]any{
		"depth": 2,
	})

	var event map[string]any
	err := json.Unmarshal(buf.Bytes(), &event)
	require.NoError(t, err)

	assert.Equal(t, "error", event["level"])
	assert.Equal(t, EventError, event["type"])
}

func TestEvent_MarshalJSON(t *testing.T) {
	event := Event{
		Timestamp: time.Now(),
		Level:     LevelInfo,
		Type:      "test",
		Message:   "test message",
		Fields:    map[string]any{"key": "value"},
	}

	data, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded map[string]any
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "info", decoded["level"])
}

// Integration test

func TestObservability_Integration(t *testing.T) {
	// Create registry and metrics
	reg := NewRegistry()
	metrics := NewRLMMetrics(reg)

	// Create tracer with callback
	var spans []SpanData
	tracer := NewTracer("rlm", WithSpanCallback(func(data SpanData) {
		spans = append(spans, data)
	}))

	// Create logger
	var buf bytes.Buffer
	logger := NewEventLogger(WithWriter(&buf), WithLevel(LevelDebug), WithBuffer(100))
	rlmLog := NewRLMLogger(logger)

	// Simulate RLM operation
	ctx, rootSpan := tracer.Start(context.Background(), SpanRLMCall)
	rootSpan.SetAttribute(AttrModel, "sonnet")

	rlmLog.CallStart("task-1", 0)
	metrics.RecordCall(100 * time.Millisecond)

	// Subcall
	_, subSpan := tracer.Start(ctx, SpanRLMSubcall)
	subSpan.SetAttribute(AttrTokensInput, 100)
	subSpan.SetAttribute(AttrTokensOutput, 50)
	metrics.RecordSubcall()
	metrics.RecordTokens(100, 50)
	rlmLog.SubcallEnd("op-1", 100, 50, 50*time.Millisecond)
	subSpan.End()

	// Cache
	metrics.RecordCacheHit()
	rlmLog.CacheHit("session-1", 500)

	rlmLog.CallEnd("task-1", 100*time.Millisecond, nil)
	rootSpan.End()

	// Verify metrics
	assert.Equal(t, int64(1), metrics.callsTotal.Value())
	assert.Equal(t, int64(1), metrics.subcallsTotal.Value())
	assert.Equal(t, int64(1), metrics.cacheHits.Value())

	// Verify spans
	assert.Len(t, spans, 2)

	// Verify logs
	events := logger.RecentEvents(10)
	assert.Greater(t, len(events), 0)
}
