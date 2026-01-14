// Package observability provides metrics, tracing, and logging for RLM components.
package observability

import (
	"sync"
	"sync/atomic"
	"time"
)

// MetricType identifies the type of metric.
type MetricType int

const (
	MetricCounter MetricType = iota
	MetricGauge
	MetricHistogram
)

// Metric names for RLM components.
const (
	// Controller metrics
	MetricCallsTotal        = "rlm_calls_total"
	MetricCallDuration      = "rlm_call_duration_seconds"
	MetricDecomposeDepth    = "rlm_decompose_depth"
	MetricSubcallsTotal     = "rlm_subcalls_total"
	MetricTokensInput       = "rlm_tokens_input_total"
	MetricTokensOutput      = "rlm_tokens_output_total"

	// Async executor metrics
	MetricAsyncExecutions   = "rlm_async_executions_total"
	MetricAsyncParallel     = "rlm_async_parallel_ops"
	MetricAsyncSpeculative  = "rlm_async_speculative_total"
	MetricAsyncCancelled    = "rlm_async_cancelled_total"

	// Cache metrics
	MetricCacheHits         = "rlm_cache_hits_total"
	MetricCacheMisses       = "rlm_cache_misses_total"
	MetricCacheTokensSaved  = "rlm_cache_tokens_saved_total"
	MetricCacheCostSaved    = "rlm_cache_cost_saved_total"

	// Circuit breaker metrics
	MetricBreakerState      = "rlm_breaker_state"
	MetricBreakerTrips      = "rlm_breaker_trips_total"
	MetricBreakerRejections = "rlm_breaker_rejections_total"

	// Memory metrics
	MetricMemoryNodes       = "rlm_memory_nodes_total"
	MetricMemoryEdges       = "rlm_memory_edges_total"
	MetricMemorySearches    = "rlm_memory_searches_total"
)

// Labels for metrics.
type Labels map[string]string

// Counter is a monotonically increasing metric.
type Counter struct {
	value  int64
	labels Labels
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	atomic.AddInt64(&c.value, 1)
}

// Add adds the given value to the counter.
func (c *Counter) Add(v int64) {
	atomic.AddInt64(&c.value, v)
}

// Value returns the current counter value.
func (c *Counter) Value() int64 {
	return atomic.LoadInt64(&c.value)
}

// Gauge is a metric that can go up and down.
type Gauge struct {
	value  int64
	labels Labels
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(v int64) {
	atomic.StoreInt64(&g.value, v)
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	atomic.AddInt64(&g.value, 1)
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	atomic.AddInt64(&g.value, -1)
}

// Add adds the given value to the gauge.
func (g *Gauge) Add(v int64) {
	atomic.AddInt64(&g.value, v)
}

// Value returns the current gauge value.
func (g *Gauge) Value() int64 {
	return atomic.LoadInt64(&g.value)
}

// Histogram tracks the distribution of values.
type Histogram struct {
	buckets []float64
	counts  []int64
	sum     float64
	count   int64
	labels  Labels
	mu      sync.Mutex
}

// DefaultBuckets are standard latency buckets in seconds.
var DefaultBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// NewHistogram creates a histogram with the given buckets.
func NewHistogram(buckets []float64, labels Labels) *Histogram {
	if buckets == nil {
		buckets = DefaultBuckets
	}
	return &Histogram{
		buckets: buckets,
		counts:  make([]int64, len(buckets)+1), // +1 for infinity bucket
		labels:  labels,
	}
}

// Observe records a value in the histogram.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sum += v
	h.count++

	// Find bucket and increment
	for i, bound := range h.buckets {
		if v <= bound {
			h.counts[i]++
			return
		}
	}
	// Infinity bucket
	h.counts[len(h.buckets)]++
}

// ObserveDuration records a duration since start.
func (h *Histogram) ObserveDuration(start time.Time) {
	h.Observe(time.Since(start).Seconds())
}

// Snapshot returns a snapshot of the histogram.
func (h *Histogram) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	counts := make([]int64, len(h.counts))
	copy(counts, h.counts)

	return HistogramSnapshot{
		Buckets: h.buckets,
		Counts:  counts,
		Sum:     h.sum,
		Count:   h.count,
	}
}

// HistogramSnapshot is a point-in-time snapshot of a histogram.
type HistogramSnapshot struct {
	Buckets []float64
	Counts  []int64
	Sum     float64
	Count   int64
}

// Mean returns the mean value.
func (s HistogramSnapshot) Mean() float64 {
	if s.Count == 0 {
		return 0
	}
	return s.Sum / float64(s.Count)
}

// Percentile estimates the value at the given percentile (0-100).
func (s HistogramSnapshot) Percentile(p float64) float64 {
	if s.Count == 0 {
		return 0
	}

	threshold := int64(float64(s.Count) * p / 100)
	var cumulative int64

	for i, count := range s.Counts {
		cumulative += count
		if cumulative >= threshold {
			if i < len(s.Buckets) {
				return s.Buckets[i]
			}
			// Last bucket - return the last bound
			if len(s.Buckets) > 0 {
				return s.Buckets[len(s.Buckets)-1]
			}
		}
	}

	return 0
}

// Registry holds all metrics for a component.
type Registry struct {
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
	mu         sync.RWMutex
}

// NewRegistry creates a new metrics registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   make(map[string]*Counter),
		gauges:     make(map[string]*Gauge),
		histograms: make(map[string]*Histogram),
	}
}

// Counter returns or creates a counter with the given name and labels.
func (r *Registry) Counter(name string, labels Labels) *Counter {
	key := metricKey(name, labels)

	r.mu.RLock()
	if c, ok := r.counters[key]; ok {
		r.mu.RUnlock()
		return c
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.counters[key]; ok {
		return c
	}

	c := &Counter{labels: labels}
	r.counters[key] = c
	return c
}

// Gauge returns or creates a gauge with the given name and labels.
func (r *Registry) Gauge(name string, labels Labels) *Gauge {
	key := metricKey(name, labels)

	r.mu.RLock()
	if g, ok := r.gauges[key]; ok {
		r.mu.RUnlock()
		return g
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if g, ok := r.gauges[key]; ok {
		return g
	}

	g := &Gauge{labels: labels}
	r.gauges[key] = g
	return g
}

// Histogram returns or creates a histogram with the given name and labels.
func (r *Registry) Histogram(name string, labels Labels, buckets []float64) *Histogram {
	key := metricKey(name, labels)

	r.mu.RLock()
	if h, ok := r.histograms[key]; ok {
		r.mu.RUnlock()
		return h
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if h, ok := r.histograms[key]; ok {
		return h
	}

	h := NewHistogram(buckets, labels)
	r.histograms[key] = h
	return h
}

// Snapshot returns a snapshot of all metrics.
func (r *Registry) Snapshot() MetricsSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := MetricsSnapshot{
		Counters:   make(map[string]int64, len(r.counters)),
		Gauges:     make(map[string]int64, len(r.gauges)),
		Histograms: make(map[string]HistogramSnapshot, len(r.histograms)),
	}

	for k, c := range r.counters {
		snap.Counters[k] = c.Value()
	}
	for k, g := range r.gauges {
		snap.Gauges[k] = g.Value()
	}
	for k, h := range r.histograms {
		snap.Histograms[k] = h.Snapshot()
	}

	return snap
}

// MetricsSnapshot is a point-in-time snapshot of all metrics.
type MetricsSnapshot struct {
	Counters   map[string]int64
	Gauges     map[string]int64
	Histograms map[string]HistogramSnapshot
}

func metricKey(name string, labels Labels) string {
	if len(labels) == 0 {
		return name
	}

	// Simple key generation - could be more sophisticated
	key := name
	for k, v := range labels {
		key += "," + k + "=" + v
	}
	return key
}

// Global default registry.
var defaultRegistry = NewRegistry()

// DefaultRegistry returns the global default metrics registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// RLMMetrics provides convenient access to RLM-specific metrics.
type RLMMetrics struct {
	registry *Registry

	// Pre-allocated metrics for hot paths
	callsTotal      *Counter
	callDuration    *Histogram
	subcallsTotal   *Counter
	tokensInput     *Counter
	tokensOutput    *Counter
	cacheHits       *Counter
	cacheMisses     *Counter
	asyncExecutions *Counter
	asyncParallel   *Gauge
}

// NewRLMMetrics creates RLM metrics using the given registry.
func NewRLMMetrics(registry *Registry) *RLMMetrics {
	if registry == nil {
		registry = defaultRegistry
	}

	m := &RLMMetrics{registry: registry}

	// Pre-allocate common metrics
	m.callsTotal = registry.Counter(MetricCallsTotal, nil)
	m.callDuration = registry.Histogram(MetricCallDuration, nil, DefaultBuckets)
	m.subcallsTotal = registry.Counter(MetricSubcallsTotal, nil)
	m.tokensInput = registry.Counter(MetricTokensInput, nil)
	m.tokensOutput = registry.Counter(MetricTokensOutput, nil)
	m.cacheHits = registry.Counter(MetricCacheHits, nil)
	m.cacheMisses = registry.Counter(MetricCacheMisses, nil)
	m.asyncExecutions = registry.Counter(MetricAsyncExecutions, nil)
	m.asyncParallel = registry.Gauge(MetricAsyncParallel, nil)

	return m
}

// RecordCall records an RLM call with the given duration.
func (m *RLMMetrics) RecordCall(duration time.Duration) {
	m.callsTotal.Inc()
	m.callDuration.Observe(duration.Seconds())
}

// RecordSubcall records a subcall.
func (m *RLMMetrics) RecordSubcall() {
	m.subcallsTotal.Inc()
}

// RecordTokens records token usage.
func (m *RLMMetrics) RecordTokens(input, output int64) {
	m.tokensInput.Add(input)
	m.tokensOutput.Add(output)
}

// RecordCacheHit records a cache hit.
func (m *RLMMetrics) RecordCacheHit() {
	m.cacheHits.Inc()
}

// RecordCacheMiss records a cache miss.
func (m *RLMMetrics) RecordCacheMiss() {
	m.cacheMisses.Inc()
}

// RecordAsyncExecution records an async execution.
func (m *RLMMetrics) RecordAsyncExecution(parallel int) {
	m.asyncExecutions.Inc()
	m.asyncParallel.Set(int64(parallel))
}

// CacheHitRate returns the cache hit rate (0-1).
func (m *RLMMetrics) CacheHitRate() float64 {
	hits := m.cacheHits.Value()
	misses := m.cacheMisses.Value()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// BreakerCounter returns a counter for circuit breaker events.
func (m *RLMMetrics) BreakerCounter(name string, tier string) *Counter {
	return m.registry.Counter(name, Labels{"tier": tier})
}

// BreakerGauge returns a gauge for circuit breaker state.
func (m *RLMMetrics) BreakerGauge(tier string) *Gauge {
	return m.registry.Gauge(MetricBreakerState, Labels{"tier": tier})
}

// ModelCounter returns a counter for model-specific metrics.
func (m *RLMMetrics) ModelCounter(name string, model string) *Counter {
	return m.registry.Counter(name, Labels{"model": model})
}

// ModelHistogram returns a histogram for model-specific latencies.
func (m *RLMMetrics) ModelHistogram(name string, model string) *Histogram {
	return m.registry.Histogram(name, Labels{"model": model}, DefaultBuckets)
}

// Snapshot returns a snapshot of all RLM metrics.
func (m *RLMMetrics) Snapshot() MetricsSnapshot {
	return m.registry.Snapshot()
}
