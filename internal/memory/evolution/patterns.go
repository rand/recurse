package evolution

import (
	"time"
)

// Pattern represents a detected anomaly in memory system behavior
// that may warrant architectural adaptation.
type Pattern interface {
	// Type returns the pattern type identifier.
	Type() PatternType
	// Confidence returns confidence level (0.0-1.0).
	Confidence() float64
	// Description returns a human-readable description.
	Description() string
	// DetectedAt returns when the pattern was detected.
	DetectedAt() time.Time
}

// PatternType identifies the category of detected pattern.
type PatternType string

const (
	PatternNodeTypeMismatch    PatternType = "node_type_mismatch"
	PatternMissingSubtype      PatternType = "missing_subtype"
	PatternRetrievalMismatch   PatternType = "retrieval_mismatch"
	PatternHighDecayOnUseful   PatternType = "high_decay_on_useful"
	PatternLowRetrievalHitRate PatternType = "low_retrieval_hit_rate"
)

// NodeTypeMismatchPattern is detected when nodes are consistently retrieved
// but marked as irrelevant by the user or system.
type NodeTypeMismatchPattern struct {
	CurrentType   string    // e.g., "fact"
	SuggestedType string    // e.g., "code_pattern" (derived from analysis)
	confidence    float64   // Based on frequency and consistency
	Examples      []string  // Sample node IDs demonstrating the mismatch
	QueryType     string    // The query type that triggers this mismatch
	Occurrences   int       // Number of times observed
	AvgRelevance  float64   // Average relevance score for these retrievals
	detectedAt    time.Time
}

func (p *NodeTypeMismatchPattern) Type() PatternType     { return PatternNodeTypeMismatch }
func (p *NodeTypeMismatchPattern) Confidence() float64   { return p.confidence }
func (p *NodeTypeMismatchPattern) DetectedAt() time.Time { return p.detectedAt }
func (p *NodeTypeMismatchPattern) Description() string {
	return "Nodes of type '" + p.CurrentType + "' consistently retrieved but marked irrelevant for '" + p.QueryType + "' queries"
}

// MissingSubtypePattern is detected when nodes cluster semantically
// but lack distinguishing subtypes.
type MissingSubtypePattern struct {
	ParentType    string    // The parent node type
	ProposedName  string    // Generated name for the new subtype
	ClusterSize   int       // Number of nodes in the cluster
	Cohesion      float64   // Intra-cluster similarity (0.0-1.0)
	Separation    float64   // Distance from other clusters (0.0-1.0)
	SampleNodeIDs []string  // Representative node IDs
	CommonTerms   []string  // Terms that characterize this cluster
	detectedAt    time.Time
}

func (p *MissingSubtypePattern) Type() PatternType     { return PatternMissingSubtype }
func (p *MissingSubtypePattern) Confidence() float64   { return (p.Cohesion + p.Separation) / 2 }
func (p *MissingSubtypePattern) DetectedAt() time.Time { return p.detectedAt }
func (p *MissingSubtypePattern) Description() string {
	return "Detected semantic cluster of " + string(rune(p.ClusterSize+'0')) + " nodes within '" + p.ParentType + "' that could benefit from dedicated subtype"
}

// RetrievalMismatchPattern is detected when certain query types
// consistently underperform with the current retrieval strategy.
type RetrievalMismatchPattern struct {
	QueryType       string  // computational, retrieval, analytical, transformational
	CurrentStrategy string  // "semantic", "keyword", "hybrid"
	SuggestedChange string  // Recommended strategy change
	Metrics         RetrievalMetrics
	detectedAt      time.Time
}

func (p *RetrievalMismatchPattern) Type() PatternType     { return PatternRetrievalMismatch }
func (p *RetrievalMismatchPattern) Confidence() float64   { return 1.0 - p.Metrics.AvgRelevance }
func (p *RetrievalMismatchPattern) DetectedAt() time.Time { return p.detectedAt }
func (p *RetrievalMismatchPattern) Description() string {
	return "Retrieval strategy '" + p.CurrentStrategy + "' underperforming for '" + p.QueryType + "' queries"
}

// RetrievalMetrics captures performance metrics for memory retrieval.
type RetrievalMetrics struct {
	AvgRelevance   float64       // Average relevance of retrieved nodes (0.0-1.0)
	AvgLatency     time.Duration // Average query latency
	HitRate        float64       // Percentage of queries with useful results
	FalsePositives float64       // Percentage of retrieved but unused nodes
	SampleSize     int           // Number of queries analyzed
}

// HighDecayOnUsefulPattern is detected when nodes that are frequently
// accessed are still experiencing high decay.
type HighDecayOnUsefulPattern struct {
	NodeType       string    // Type of nodes affected
	NodesAffected  int       // Count of nodes in this pattern
	AvgAccessCount float64   // Average access count for these nodes
	AvgDecayRate   float64   // Average decay applied
	SampleNodeIDs  []string  // Sample affected node IDs
	detectedAt     time.Time
}

func (p *HighDecayOnUsefulPattern) Type() PatternType     { return PatternHighDecayOnUseful }
func (p *HighDecayOnUsefulPattern) Confidence() float64   { return p.AvgAccessCount / (p.AvgAccessCount + 10) } // Sigmoid-like
func (p *HighDecayOnUsefulPattern) DetectedAt() time.Time { return p.detectedAt }
func (p *HighDecayOnUsefulPattern) Description() string {
	return "Frequently accessed '" + p.NodeType + "' nodes experiencing high decay rate"
}

// LowRetrievalHitRatePattern is detected when overall retrieval
// success rate falls below acceptable thresholds.
type LowRetrievalHitRatePattern struct {
	OverallHitRate float64   // Current hit rate
	TargetHitRate  float64   // Expected hit rate
	QueryTypes     []string  // Query types most affected
	TimePeriod     time.Duration
	detectedAt     time.Time
}

func (p *LowRetrievalHitRatePattern) Type() PatternType     { return PatternLowRetrievalHitRate }
func (p *LowRetrievalHitRatePattern) Confidence() float64   { return p.TargetHitRate - p.OverallHitRate }
func (p *LowRetrievalHitRatePattern) DetectedAt() time.Time { return p.detectedAt }
func (p *LowRetrievalHitRatePattern) Description() string {
	return "Overall retrieval hit rate below target threshold"
}

// RetrievalOutcome records the result of a single memory retrieval operation.
// Used for pattern detection analysis.
type RetrievalOutcome struct {
	ID            int64
	Timestamp     time.Time
	QueryHash     string  // Hash of the query for deduplication
	QueryType     string  // computational, retrieval, analytical, transformational
	NodeID        string  // Retrieved node ID
	NodeType      string  // Type of the retrieved node
	NodeSubtype   string  // Subtype if any
	RelevanceScore float64 // 0.0-1.0, from feedback or implicit signals
	WasUsed       bool    // Did the retrieved content get used?
	ContextTokens int     // Token count of the context
	LatencyMs     int     // Query latency in milliseconds
}

// PatternSummary provides a condensed view of detected patterns.
type PatternSummary struct {
	Type        PatternType
	Count       int
	AvgConfidence float64
	FirstSeen   time.Time
	LastSeen    time.Time
}
