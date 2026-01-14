package embeddings

import (
	"time"

	"github.com/rand/recurse/internal/rlm/observability"
)

// Metric names for embeddings.
const (
	MetricEmbedLatency     = "embeddings_latency_seconds"
	MetricEmbedBatchSize   = "embeddings_batch_size"
	MetricEmbedQueueDepth  = "embeddings_queue_depth"
	MetricEmbedTotal       = "embeddings_total"
	MetricEmbedErrors      = "embeddings_errors_total"
	MetricCacheHits        = "embeddings_cache_hits_total"
	MetricCacheMisses      = "embeddings_cache_misses_total"
	MetricSearchLatency    = "embeddings_search_latency_seconds"
	MetricHybridLatency    = "hybrid_search_latency_seconds"
	MetricIndexSize        = "embeddings_index_size"
)

// EmbeddingMetrics provides metrics for the embeddings system.
type EmbeddingMetrics struct {
	registry *observability.Registry

	// Pre-allocated metrics for hot paths
	embedLatency   *observability.Histogram
	embedTotal     *observability.Counter
	embedErrors    *observability.Counter
	cacheHits      *observability.Counter
	cacheMisses    *observability.Counter
	searchLatency  *observability.Histogram
	hybridLatency  *observability.Histogram
	queueDepth     *observability.Gauge
	indexSize      *observability.Gauge
}

// NewEmbeddingMetrics creates embedding metrics using the given registry.
func NewEmbeddingMetrics(registry *observability.Registry) *EmbeddingMetrics {
	if registry == nil {
		registry = observability.DefaultRegistry()
	}

	m := &EmbeddingMetrics{registry: registry}

	// Pre-allocate common metrics
	m.embedLatency = registry.Histogram(MetricEmbedLatency, nil, observability.DefaultBuckets)
	m.embedTotal = registry.Counter(MetricEmbedTotal, nil)
	m.embedErrors = registry.Counter(MetricEmbedErrors, nil)
	m.cacheHits = registry.Counter(MetricCacheHits, nil)
	m.cacheMisses = registry.Counter(MetricCacheMisses, nil)
	m.searchLatency = registry.Histogram(MetricSearchLatency, nil, observability.DefaultBuckets)
	m.hybridLatency = registry.Histogram(MetricHybridLatency, nil, observability.DefaultBuckets)
	m.queueDepth = registry.Gauge(MetricEmbedQueueDepth, nil)
	m.indexSize = registry.Gauge(MetricIndexSize, nil)

	return m
}

// RecordEmbed records an embedding operation.
func (m *EmbeddingMetrics) RecordEmbed(duration time.Duration, batchSize int) {
	m.embedTotal.Add(int64(batchSize))
	m.embedLatency.Observe(duration.Seconds())
}

// RecordEmbedError records an embedding error.
func (m *EmbeddingMetrics) RecordEmbedError() {
	m.embedErrors.Inc()
}

// RecordCacheHit records a cache hit.
func (m *EmbeddingMetrics) RecordCacheHit(count int) {
	m.cacheHits.Add(int64(count))
}

// RecordCacheMiss records a cache miss.
func (m *EmbeddingMetrics) RecordCacheMiss(count int) {
	m.cacheMisses.Add(int64(count))
}

// RecordSearch records a semantic search operation.
func (m *EmbeddingMetrics) RecordSearch(duration time.Duration) {
	m.searchLatency.Observe(duration.Seconds())
}

// RecordHybridSearch records a hybrid search operation.
func (m *EmbeddingMetrics) RecordHybridSearch(duration time.Duration) {
	m.hybridLatency.Observe(duration.Seconds())
}

// SetQueueDepth sets the current queue depth.
func (m *EmbeddingMetrics) SetQueueDepth(depth int) {
	m.queueDepth.Set(int64(depth))
}

// SetIndexSize sets the current index size.
func (m *EmbeddingMetrics) SetIndexSize(size int) {
	m.indexSize.Set(int64(size))
}

// CacheHitRate returns the cache hit rate (0-1).
func (m *EmbeddingMetrics) CacheHitRate() float64 {
	hits := m.cacheHits.Value()
	misses := m.cacheMisses.Value()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// Snapshot returns a snapshot of all embedding metrics.
func (m *EmbeddingMetrics) Snapshot() observability.MetricsSnapshot {
	return m.registry.Snapshot()
}

// ModelMetrics returns a histogram for model-specific latencies.
func (m *EmbeddingMetrics) ModelLatency(model string) *observability.Histogram {
	return m.registry.Histogram(MetricEmbedLatency, observability.Labels{"model": model}, observability.DefaultBuckets)
}
