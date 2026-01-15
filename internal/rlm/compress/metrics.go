// Package compress provides context compression for the RLM system.
package compress

import (
	"sync"
	"time"
)

// Metrics provides observability for compression operations.
type MetricsCollector struct {
	mu sync.RWMutex

	// Counters
	TotalCompressions   int64
	TotalChunksProcessed int64
	TotalTokensOriginal int64
	TotalTokensSaved    int64

	// Cache metrics
	CacheHits   int64
	CacheMisses int64

	// Method distribution
	MethodCounts map[Method]int64

	// Chunk type distribution
	ChunkTypeCounts map[string]int64

	// Timing
	TotalDuration    time.Duration
	MinDuration      time.Duration
	MaxDuration      time.Duration
	LastCompression  time.Time

	// Ratios
	ratioSum   float64
	ratioCount int64
}

// NewMetrics creates a new metrics instance.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		MethodCounts:    make(map[Method]int64),
		ChunkTypeCounts: make(map[string]int64),
	}
}

// Record records a compression operation.
func (m *MetricsCollector) Record(result *PreparedContext) {
	if result == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalCompressions++
	m.TotalChunksProcessed += int64(len(result.ChunkResults))
	m.TotalTokensOriginal += int64(result.OriginalTokens)
	m.TotalTokensSaved += int64(result.OriginalTokens - result.CompressedTokens)

	// Track timing
	m.TotalDuration += result.Duration
	if m.MinDuration == 0 || result.Duration < m.MinDuration {
		m.MinDuration = result.Duration
	}
	if result.Duration > m.MaxDuration {
		m.MaxDuration = result.Duration
	}
	m.LastCompression = time.Now()

	// Track ratio
	m.ratioSum += result.Ratio
	m.ratioCount++

	// Track per-chunk metrics
	for _, chunk := range result.ChunkResults {
		m.MethodCounts[chunk.Method]++
		if chunk.Type != "" {
			m.ChunkTypeCounts[chunk.Type]++
		}
		if chunk.Cached {
			m.CacheHits++
		} else {
			m.CacheMisses++
		}
	}
}

// RecordCacheHit records a cache hit.
func (m *MetricsCollector) RecordCacheHit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CacheHits++
}

// RecordCacheMiss records a cache miss.
func (m *MetricsCollector) RecordCacheMiss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CacheMisses++
}

// Snapshot returns a point-in-time snapshot of the metrics.
func (m *MetricsCollector) Snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var avgRatio float64
	if m.ratioCount > 0 {
		avgRatio = m.ratioSum / float64(m.ratioCount)
	}

	var avgDuration time.Duration
	if m.TotalCompressions > 0 {
		avgDuration = m.TotalDuration / time.Duration(m.TotalCompressions)
	}

	var cacheHitRate float64
	totalCacheOps := m.CacheHits + m.CacheMisses
	if totalCacheOps > 0 {
		cacheHitRate = float64(m.CacheHits) / float64(totalCacheOps)
	}

	// Copy maps
	methodCounts := make(map[Method]int64)
	for k, v := range m.MethodCounts {
		methodCounts[k] = v
	}
	chunkTypeCounts := make(map[string]int64)
	for k, v := range m.ChunkTypeCounts {
		chunkTypeCounts[k] = v
	}

	return MetricsSnapshot{
		TotalCompressions:   m.TotalCompressions,
		TotalChunksProcessed: m.TotalChunksProcessed,
		TotalTokensOriginal: m.TotalTokensOriginal,
		TotalTokensSaved:    m.TotalTokensSaved,
		CacheHits:           m.CacheHits,
		CacheMisses:         m.CacheMisses,
		CacheHitRate:        cacheHitRate,
		MethodCounts:        methodCounts,
		ChunkTypeCounts:     chunkTypeCounts,
		AvgRatio:            avgRatio,
		AvgDuration:         avgDuration,
		MinDuration:         m.MinDuration,
		MaxDuration:         m.MaxDuration,
		LastCompression:     m.LastCompression,
	}
}

// Reset resets all metrics.
func (m *MetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalCompressions = 0
	m.TotalChunksProcessed = 0
	m.TotalTokensOriginal = 0
	m.TotalTokensSaved = 0
	m.CacheHits = 0
	m.CacheMisses = 0
	m.MethodCounts = make(map[Method]int64)
	m.ChunkTypeCounts = make(map[string]int64)
	m.TotalDuration = 0
	m.MinDuration = 0
	m.MaxDuration = 0
	m.ratioSum = 0
	m.ratioCount = 0
}

// MetricsSnapshot is a point-in-time copy of metrics.
type MetricsSnapshot struct {
	TotalCompressions   int64             `json:"total_compressions"`
	TotalChunksProcessed int64            `json:"total_chunks_processed"`
	TotalTokensOriginal int64             `json:"total_tokens_original"`
	TotalTokensSaved    int64             `json:"total_tokens_saved"`
	CacheHits           int64             `json:"cache_hits"`
	CacheMisses         int64             `json:"cache_misses"`
	CacheHitRate        float64           `json:"cache_hit_rate"`
	MethodCounts        map[Method]int64  `json:"method_counts"`
	ChunkTypeCounts     map[string]int64  `json:"chunk_type_counts"`
	AvgRatio            float64           `json:"avg_ratio"`
	AvgDuration         time.Duration     `json:"avg_duration"`
	MinDuration         time.Duration     `json:"min_duration"`
	MaxDuration         time.Duration     `json:"max_duration"`
	LastCompression     time.Time         `json:"last_compression"`
}

// SavedTokensPercent returns the percentage of tokens saved.
func (s MetricsSnapshot) SavedTokensPercent() float64 {
	if s.TotalTokensOriginal == 0 {
		return 0
	}
	return float64(s.TotalTokensSaved) / float64(s.TotalTokensOriginal) * 100
}

// AvgChunksPerCompression returns the average chunks processed per compression.
func (s MetricsSnapshot) AvgChunksPerCompression() float64 {
	if s.TotalCompressions == 0 {
		return 0
	}
	return float64(s.TotalChunksProcessed) / float64(s.TotalCompressions)
}
