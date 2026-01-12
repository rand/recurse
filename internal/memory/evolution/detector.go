package evolution

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// DetectorConfig configures the pattern detector.
type DetectorConfig struct {
	// MinSampleSize is the minimum observations before detection runs.
	MinSampleSize int

	// AnalysisWindow is how far back to analyze.
	AnalysisWindow time.Duration

	// MismatchThreshold - relevance below this triggers mismatch detection.
	MismatchThreshold float64

	// ClusterCohesionMin - minimum cohesion for subtype proposal.
	ClusterCohesionMin float64

	// ClusterSeparationMin - minimum separation from other clusters.
	ClusterSeparationMin float64

	// MinClusterSize - minimum nodes to form a subtype cluster.
	MinClusterSize int

	// HitRateThreshold - hit rate below this triggers pattern detection.
	HitRateThreshold float64

	// HighAccessThreshold - access count above this is "high".
	HighAccessThreshold int
}

// DefaultDetectorConfig returns sensible defaults.
func DefaultDetectorConfig() DetectorConfig {
	return DetectorConfig{
		MinSampleSize:        10,
		AnalysisWindow:       7 * 24 * time.Hour, // 1 week
		MismatchThreshold:    0.4,
		ClusterCohesionMin:   0.7,
		ClusterSeparationMin: 0.5,
		MinClusterSize:       5,
		HitRateThreshold:     0.6,
		HighAccessThreshold:  5,
	}
}

// PatternDetector analyzes retrieval outcomes and evolution logs
// to detect patterns that may warrant architectural adaptation.
type PatternDetector struct {
	store  *hypergraph.Store
	config DetectorConfig

	// outcomeStore handles retrieval outcome persistence.
	// This is injected to allow testing and flexible storage.
	outcomeStore OutcomeStore
}

// OutcomeStore abstracts retrieval outcome storage.
type OutcomeStore interface {
	// RecordOutcome records a retrieval outcome.
	RecordOutcome(ctx context.Context, outcome RetrievalOutcome) error

	// QueryOutcomes retrieves outcomes within a time window.
	QueryOutcomes(ctx context.Context, since time.Time) ([]RetrievalOutcome, error)

	// QueryOutcomesByNodeType retrieves outcomes for a specific node type.
	QueryOutcomesByNodeType(ctx context.Context, nodeType string, since time.Time) ([]RetrievalOutcome, error)

	// QueryOutcomesByQueryType retrieves outcomes for a specific query type.
	QueryOutcomesByQueryType(ctx context.Context, queryType string, since time.Time) ([]RetrievalOutcome, error)

	// GetOutcomeStats returns aggregated statistics.
	GetOutcomeStats(ctx context.Context, since time.Time) (*OutcomeStats, error)
}

// OutcomeStats contains aggregated retrieval statistics.
type OutcomeStats struct {
	TotalOutcomes     int
	AvgRelevance      float64
	HitRate           float64 // Percentage with WasUsed=true
	ByNodeType        map[string]TypeStats
	ByQueryType       map[string]TypeStats
	AvgLatencyMs      int
}

// TypeStats contains stats for a specific type.
type TypeStats struct {
	Count        int
	AvgRelevance float64
	HitRate      float64
	AvgLatencyMs int
}

// NewPatternDetector creates a new pattern detector.
func NewPatternDetector(store *hypergraph.Store, outcomeStore OutcomeStore, config DetectorConfig) *PatternDetector {
	return &PatternDetector{
		store:        store,
		outcomeStore: outcomeStore,
		config:       config,
	}
}

// DetectPatterns analyzes recent data to detect actionable patterns.
func (d *PatternDetector) DetectPatterns(ctx context.Context) ([]Pattern, error) {
	since := time.Now().Add(-d.config.AnalysisWindow)

	// Get all outcomes in the analysis window
	outcomes, err := d.outcomeStore.QueryOutcomes(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("query outcomes: %w", err)
	}

	// Skip if insufficient data
	if len(outcomes) < d.config.MinSampleSize {
		return nil, nil
	}

	var patterns []Pattern

	// 1. Detect node type mismatches
	if mismatchPatterns := d.detectTypeMismatches(outcomes); len(mismatchPatterns) > 0 {
		patterns = append(patterns, mismatchPatterns...)
	}

	// 2. Detect retrieval strategy mismatches
	if retrievalPatterns := d.detectRetrievalMismatches(outcomes); len(retrievalPatterns) > 0 {
		patterns = append(patterns, retrievalPatterns...)
	}

	// 3. Detect missing subtypes (requires embedding analysis)
	if subtypePatterns, err := d.detectMissingSubtypes(ctx, outcomes); err == nil && len(subtypePatterns) > 0 {
		patterns = append(patterns, subtypePatterns...)
	}

	// 4. Detect high decay on useful nodes
	if decayPatterns, err := d.detectHighDecayOnUseful(ctx); err == nil && len(decayPatterns) > 0 {
		patterns = append(patterns, decayPatterns...)
	}

	return patterns, nil
}

// detectTypeMismatches finds node types that are frequently retrieved
// but rarely relevant for specific query types.
func (d *PatternDetector) detectTypeMismatches(outcomes []RetrievalOutcome) []Pattern {
	// Group by (node_type, query_type)
	type key struct {
		nodeType  string
		queryType string
	}
	groups := make(map[key][]RetrievalOutcome)

	for _, o := range outcomes {
		k := key{nodeType: o.NodeType, queryType: o.QueryType}
		groups[k] = append(groups[k], o)
	}

	var patterns []Pattern

	for k, groupOutcomes := range groups {
		if len(groupOutcomes) < d.config.MinSampleSize {
			continue
		}

		// Calculate average relevance and usage rate
		var totalRelevance float64
		var usedCount int
		for _, o := range groupOutcomes {
			totalRelevance += o.RelevanceScore
			if o.WasUsed {
				usedCount++
			}
		}
		avgRelevance := totalRelevance / float64(len(groupOutcomes))
		usageRate := float64(usedCount) / float64(len(groupOutcomes))

		// Check if this combination is underperforming
		if avgRelevance < d.config.MismatchThreshold && usageRate < 0.5 {
			// Collect sample node IDs
			seen := make(map[string]bool)
			var samples []string
			for _, o := range groupOutcomes {
				if !seen[o.NodeID] && len(samples) < 5 {
					samples = append(samples, o.NodeID)
					seen[o.NodeID] = true
				}
			}

			patterns = append(patterns, &NodeTypeMismatchPattern{
				CurrentType:   k.nodeType,
				SuggestedType: suggestAlternativeType(k.nodeType, k.queryType),
				confidence:    1.0 - avgRelevance,
				Examples:      samples,
				QueryType:     k.queryType,
				Occurrences:   len(groupOutcomes),
				AvgRelevance:  avgRelevance,
				detectedAt:    time.Now(),
			})
		}
	}

	return patterns
}

// detectRetrievalMismatches finds query types with poor retrieval performance.
func (d *PatternDetector) detectRetrievalMismatches(outcomes []RetrievalOutcome) []Pattern {
	// Group by query type
	byQueryType := make(map[string][]RetrievalOutcome)
	for _, o := range outcomes {
		byQueryType[o.QueryType] = append(byQueryType[o.QueryType], o)
	}

	var patterns []Pattern

	for queryType, typeOutcomes := range byQueryType {
		if len(typeOutcomes) < d.config.MinSampleSize {
			continue
		}

		metrics := calculateMetrics(typeOutcomes)

		// Check if hit rate is below threshold
		if metrics.HitRate < d.config.HitRateThreshold {
			patterns = append(patterns, &RetrievalMismatchPattern{
				QueryType:       queryType,
				CurrentStrategy: "hybrid", // Assume hybrid as default
				SuggestedChange: suggestStrategyChange(queryType, metrics),
				Metrics:         metrics,
				detectedAt:      time.Now(),
			})
		}
	}

	return patterns
}

// detectMissingSubtypes analyzes node embeddings to find clusters
// that could benefit from dedicated subtypes.
func (d *PatternDetector) detectMissingSubtypes(ctx context.Context, outcomes []RetrievalOutcome) ([]Pattern, error) {
	// Group outcomes by node type
	byNodeType := make(map[string][]string) // nodeType -> unique nodeIDs
	seen := make(map[string]bool)

	for _, o := range outcomes {
		nodeKey := o.NodeType + ":" + o.NodeID
		if !seen[nodeKey] {
			byNodeType[o.NodeType] = append(byNodeType[o.NodeType], o.NodeID)
			seen[nodeKey] = true
		}
	}

	var patterns []Pattern

	for nodeType, nodeIDs := range byNodeType {
		if len(nodeIDs) < d.config.MinClusterSize*2 {
			// Need at least 2x min cluster size to detect meaningful clusters
			continue
		}

		// Perform clustering analysis on the nodes
		clusters, err := d.clusterNodes(ctx, nodeIDs)
		if err != nil {
			continue // Skip this type on error
		}

		for _, cluster := range clusters {
			if cluster.Size >= d.config.MinClusterSize &&
				cluster.Cohesion >= d.config.ClusterCohesionMin &&
				cluster.Separation >= d.config.ClusterSeparationMin {

				patterns = append(patterns, &MissingSubtypePattern{
					ParentType:    nodeType,
					ProposedName:  cluster.SuggestedName,
					ClusterSize:   cluster.Size,
					Cohesion:      cluster.Cohesion,
					Separation:    cluster.Separation,
					SampleNodeIDs: cluster.SampleIDs,
					CommonTerms:   cluster.CommonTerms,
					detectedAt:    time.Now(),
				})
			}
		}
	}

	return patterns, nil
}

// ClusterResult represents a detected cluster of similar nodes.
type ClusterResult struct {
	Size          int
	Cohesion      float64  // Intra-cluster similarity
	Separation    float64  // Distance from other clusters
	SuggestedName string
	SampleIDs     []string
	CommonTerms   []string
}

// clusterNodes performs semantic clustering on a set of node IDs.
func (d *PatternDetector) clusterNodes(ctx context.Context, nodeIDs []string) ([]ClusterResult, error) {
	if d.store == nil {
		return nil, nil
	}

	// Get nodes with embeddings
	nodes := make([]*hypergraph.Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		node, err := d.store.GetNode(ctx, id)
		if err != nil {
			continue
		}
		if len(node.Embedding) > 0 {
			nodes = append(nodes, node)
		}
	}

	if len(nodes) < d.config.MinClusterSize {
		return nil, nil
	}

	// Simple k-means-like clustering based on embeddings
	// For production, use proper clustering library
	clusters := d.simpleCluster(nodes)

	return clusters, nil
}

// simpleCluster performs basic clustering on nodes with embeddings.
// This is a simplified implementation; production should use proper clustering.
func (d *PatternDetector) simpleCluster(nodes []*hypergraph.Node) []ClusterResult {
	if len(nodes) < d.config.MinClusterSize*2 {
		return nil
	}

	// For now, return a single cluster if nodes are cohesive enough
	// Real implementation would use k-means, DBSCAN, or hierarchical clustering

	// Calculate average pairwise similarity (simplified)
	var totalSim float64
	var comparisons int
	for i := 0; i < len(nodes) && i < 20; i++ {
		for j := i + 1; j < len(nodes) && j < 20; j++ {
			sim := cosineSimilarity(nodes[i].Embedding, nodes[j].Embedding)
			totalSim += sim
			comparisons++
		}
	}

	if comparisons == 0 {
		return nil
	}

	avgSim := totalSim / float64(comparisons)

	// If similarity is high enough, suggest it as a cluster
	if avgSim >= d.config.ClusterCohesionMin {
		// Extract common terms from content
		commonTerms := extractCommonTerms(nodes)

		// Generate suggested name
		suggestedName := generateSubtypeName(commonTerms)

		// Collect sample IDs
		var samples []string
		for i := 0; i < len(nodes) && len(samples) < 5; i++ {
			samples = append(samples, nodes[i].ID)
		}

		return []ClusterResult{{
			Size:          len(nodes),
			Cohesion:      avgSim,
			Separation:    0.6, // Placeholder; real impl would calculate vs other clusters
			SuggestedName: suggestedName,
			SampleIDs:     samples,
			CommonTerms:   commonTerms,
		}}
	}

	return nil
}

// detectHighDecayOnUseful finds nodes that are frequently accessed
// but experiencing high decay rates.
func (d *PatternDetector) detectHighDecayOnUseful(ctx context.Context) ([]Pattern, error) {
	if d.store == nil {
		return nil, nil
	}

	// Query nodes in longterm tier with high access but low confidence
	// This would require extending hypergraph.Store with access tracking
	// For now, return empty - this is a placeholder for future implementation

	return nil, nil
}

// Helper functions

func suggestAlternativeType(currentType, queryType string) string {
	// Simple heuristic-based suggestion
	suggestions := map[string]map[string]string{
		"fact": {
			"computational": "computed_value",
			"analytical":    "relationship",
		},
		"code_snippet": {
			"retrieval": "api_pattern",
		},
	}

	if typeMap, ok := suggestions[currentType]; ok {
		if suggested, ok := typeMap[queryType]; ok {
			return suggested
		}
	}
	return currentType + "_" + queryType
}

func suggestStrategyChange(queryType string, metrics RetrievalMetrics) string {
	switch queryType {
	case "computational":
		return "keyword" // Exact matching better for computational
	case "analytical":
		return "semantic" // Semantic understanding for relationships
	case "retrieval":
		return "hybrid" // Balance for lookups
	default:
		if metrics.FalsePositives > 0.3 {
			return "keyword" // More precise matching
		}
		return "semantic"
	}
}

func calculateMetrics(outcomes []RetrievalOutcome) RetrievalMetrics {
	if len(outcomes) == 0 {
		return RetrievalMetrics{}
	}

	var totalRelevance float64
	var usedCount int
	var totalLatency int

	for _, o := range outcomes {
		totalRelevance += o.RelevanceScore
		if o.WasUsed {
			usedCount++
		}
		totalLatency += o.LatencyMs
	}

	n := float64(len(outcomes))
	return RetrievalMetrics{
		AvgRelevance:   totalRelevance / n,
		HitRate:        float64(usedCount) / n,
		FalsePositives: 1.0 - float64(usedCount)/n,
		SampleSize:     len(outcomes),
		AvgLatency:     time.Duration(totalLatency/len(outcomes)) * time.Millisecond,
	}
}

func cosineSimilarity(a, b []byte) float64 {
	// Embeddings are stored as []byte (serialized float32 values)
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	// Ensure length is multiple of 4 (float32 size)
	if len(a)%4 != 0 {
		return 0
	}

	n := len(a) / 4
	var dotProduct, normA, normB float64

	for i := 0; i < n; i++ {
		offset := i * 4
		// Read float32 from bytes (little endian)
		aVal := bytesToFloat32(a[offset : offset+4])
		bVal := bytesToFloat32(b[offset : offset+4])

		dotProduct += float64(aVal) * float64(bVal)
		normA += float64(aVal) * float64(aVal)
		normB += float64(bVal) * float64(bVal)
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

func bytesToFloat32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	// Little-endian float32
	bits := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
	return float32frombits(bits)
}

func float32frombits(b uint32) float32 {
	// Reinterpret uint32 bits as float32
	return *(*float32)(unsafe.Pointer(&b))
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Newton's method for square root
	z := x / 2
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

func extractCommonTerms(nodes []*hypergraph.Node) []string {
	// Count term frequency across all nodes
	termCounts := make(map[string]int)

	for _, node := range nodes {
		words := strings.Fields(strings.ToLower(node.Content))
		seen := make(map[string]bool)
		for _, word := range words {
			// Skip short words and common stop words
			if len(word) < 4 || isStopWord(word) {
				continue
			}
			if !seen[word] {
				termCounts[word]++
				seen[word] = true
			}
		}
	}

	// Find terms that appear in majority of nodes
	threshold := len(nodes) / 2
	var commonTerms []string
	for term, count := range termCounts {
		if count >= threshold {
			commonTerms = append(commonTerms, term)
		}
	}

	// Sort by frequency
	sort.Slice(commonTerms, func(i, j int) bool {
		return termCounts[commonTerms[i]] > termCounts[commonTerms[j]]
	})

	// Return top 5
	if len(commonTerms) > 5 {
		commonTerms = commonTerms[:5]
	}

	return commonTerms
}

func generateSubtypeName(commonTerms []string) string {
	if len(commonTerms) == 0 {
		return "cluster"
	}
	// Use the most common term as the subtype name
	name := strings.ReplaceAll(commonTerms[0], " ", "_")
	return strings.ToLower(name)
}

func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "with": true,
		"this": true, "that": true, "from": true, "have": true,
		"been": true, "were": true, "will": true, "would": true,
		"could": true, "should": true, "about": true, "which": true,
		"their": true, "there": true, "when": true, "what": true,
		"func": true, "return": true, "error": true, "string": true,
	}
	return stopWords[word]
}
