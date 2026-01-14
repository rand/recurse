package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// AuditEventType identifies the type of evolution event.
type AuditEventType string

const (
	AuditConsolidate AuditEventType = "consolidate"
	AuditMerge       AuditEventType = "merge"
	AuditSummarize   AuditEventType = "summarize"
	AuditPromote     AuditEventType = "promote"
	AuditDemote      AuditEventType = "demote"
	AuditDecay       AuditEventType = "decay"
	AuditArchive     AuditEventType = "archive"
	AuditRestore     AuditEventType = "restore"
	AuditPrune       AuditEventType = "prune"
	AuditAccess      AuditEventType = "access"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	EventType   AuditEventType    `json:"event_type"`
	NodeID      string            `json:"node_id,omitempty"`
	NodeIDs     []string          `json:"node_ids,omitempty"`
	SourceTier  hypergraph.Tier   `json:"source_tier,omitempty"`
	TargetTier  hypergraph.Tier   `json:"target_tier,omitempty"`
	Details     map[string]any    `json:"details,omitempty"`
	Result      *AuditResult      `json:"result,omitempty"`
	Duration    time.Duration     `json:"duration,omitempty"`
}

// AuditResult contains operation outcome.
type AuditResult struct {
	Success       bool   `json:"success"`
	NodesAffected int    `json:"nodes_affected,omitempty"`
	Error         string `json:"error,omitempty"`
}

// AuditLogger logs evolution events for debugging and analysis.
type AuditLogger struct {
	mu       sync.Mutex
	file     *os.File
	encoder  *json.Encoder
	path     string
	enabled  bool
	entries  []AuditEntry // In-memory buffer for recent entries
	maxBuf   int
	store    *hypergraph.Store // Optional database store for persistence
}

// AuditConfig configures the audit logger.
type AuditConfig struct {
	// Path to the audit log file. If empty, logging is disabled.
	Path string

	// MaxBufferSize is the max entries to keep in memory.
	MaxBufferSize int

	// Enabled controls whether logging is active.
	Enabled bool
}

// DefaultAuditConfig returns sensible defaults.
func DefaultAuditConfig() AuditConfig {
	return AuditConfig{
		Path:          "",
		MaxBufferSize: 1000,
		Enabled:       true,
	}
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(config AuditConfig) (*AuditLogger, error) {
	logger := &AuditLogger{
		path:    config.Path,
		enabled: config.Enabled,
		maxBuf:  config.MaxBufferSize,
		entries: make([]AuditEntry, 0, config.MaxBufferSize),
	}

	if config.Path != "" && config.Enabled {
		// Ensure directory exists
		dir := filepath.Dir(config.Path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create audit directory: %w", err)
		}

		// Open file for appending
		file, err := os.OpenFile(config.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open audit file: %w", err)
		}
		logger.file = file
		logger.encoder = json.NewEncoder(file)
	}

	return logger, nil
}

// SetStore sets the hypergraph store for database persistence.
// When set, audit entries will be written to the evolution_log table.
func (l *AuditLogger) SetStore(store *hypergraph.Store) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.store = store
}

// Log records an audit entry.
func (l *AuditLogger) Log(entry AuditEntry) error {
	if !l.enabled {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Set timestamp if not set
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	// Add to in-memory buffer
	l.entries = append(l.entries, entry)
	if len(l.entries) > l.maxBuf {
		// Remove oldest entries
		l.entries = l.entries[len(l.entries)-l.maxBuf:]
	}

	// Write to file if configured
	if l.encoder != nil {
		if err := l.encoder.Encode(entry); err != nil {
			return fmt.Errorf("encode audit entry: %w", err)
		}
	}

	// Persist to database if store is configured
	if l.store != nil {
		if err := l.persistToStore(entry); err != nil {
			return fmt.Errorf("persist to store: %w", err)
		}
	}

	return nil
}

// persistToStore writes an audit entry to the evolution_log table.
func (l *AuditLogger) persistToStore(entry AuditEntry) error {
	// Map audit event type to evolution operation
	op := mapEventToOperation(entry.EventType)
	if op == "" {
		// Skip events that don't map to evolution operations
		return nil
	}

	// Collect node IDs
	var nodeIDs []string
	if entry.NodeID != "" {
		nodeIDs = append(nodeIDs, entry.NodeID)
	}
	nodeIDs = append(nodeIDs, entry.NodeIDs...)

	// Build reasoning from event details
	var reasoning string
	if entry.Result != nil && entry.Result.Error != "" {
		reasoning = fmt.Sprintf("Error: %s", entry.Result.Error)
	} else if entry.Details != nil {
		// Serialize details as reasoning
		if detailsJSON, err := json.Marshal(entry.Details); err == nil {
			reasoning = string(detailsJSON)
		}
	}

	// Create evolution entry
	evolutionEntry := &hypergraph.EvolutionEntry{
		Timestamp: entry.Timestamp,
		Operation: op,
		NodeIDs:   nodeIDs,
		FromTier:  entry.SourceTier,
		ToTier:    entry.TargetTier,
		Reasoning: reasoning,
	}

	return l.store.RecordEvolution(context.Background(), evolutionEntry)
}

// mapEventToOperation maps an AuditEventType to an EvolutionOperation.
func mapEventToOperation(eventType AuditEventType) hypergraph.EvolutionOperation {
	switch eventType {
	case AuditConsolidate, AuditMerge, AuditSummarize:
		return hypergraph.EvolutionConsolidate
	case AuditPromote:
		return hypergraph.EvolutionPromote
	case AuditDecay:
		return hypergraph.EvolutionDecay
	case AuditArchive:
		return hypergraph.EvolutionArchive
	case AuditPrune:
		return hypergraph.EvolutionPrune
	default:
		// AuditDemote, AuditRestore, AuditAccess don't have direct mappings
		return ""
	}
}

// LogConsolidation logs a consolidation operation.
func (l *AuditLogger) LogConsolidation(sourceTier, targetTier hypergraph.Tier, result *ConsolidationResult, err error) error {
	entry := AuditEntry{
		EventType:  AuditConsolidate,
		SourceTier: sourceTier,
		TargetTier: targetTier,
		Duration:   result.Duration,
		Details: map[string]any{
			"nodes_processed":    result.NodesProcessed,
			"nodes_merged":       result.NodesMerged,
			"summaries_created":  result.SummariesCreated,
			"edges_strengthened": result.EdgesStrengthened,
		},
		Result: &AuditResult{
			Success:       err == nil,
			NodesAffected: result.NodesProcessed,
		},
	}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	return l.Log(entry)
}

// LogMerge logs a node merge operation.
func (l *AuditLogger) LogMerge(targetID, sourceID string) error {
	return l.Log(AuditEntry{
		EventType: AuditMerge,
		NodeID:    targetID,
		Details: map[string]any{
			"merged_from": sourceID,
		},
		Result: &AuditResult{Success: true, NodesAffected: 2},
	})
}

// LogSummarize logs summary node creation.
func (l *AuditLogger) LogSummarize(summaryID string, sourceIDs []string, tier hypergraph.Tier) error {
	return l.Log(AuditEntry{
		EventType:  AuditSummarize,
		NodeID:     summaryID,
		NodeIDs:    sourceIDs,
		TargetTier: tier,
		Result:     &AuditResult{Success: true, NodesAffected: len(sourceIDs) + 1},
	})
}

// LogPromotion logs a tier promotion operation.
func (l *AuditLogger) LogPromotion(result *PromotionResult, err error) error {
	entry := AuditEntry{
		EventType: AuditPromote,
		Duration:  result.Duration,
		Details: map[string]any{
			"task_to_session":     result.TaskToSession,
			"session_to_longterm": result.SessionToLongterm,
			"skipped":             result.Skipped,
		},
		Result: &AuditResult{
			Success:       err == nil,
			NodesAffected: result.TaskToSession + result.SessionToLongterm,
		},
	}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	return l.Log(entry)
}

// LogNodePromotion logs a single node promotion.
func (l *AuditLogger) LogNodePromotion(nodeID string, sourceTier, targetTier hypergraph.Tier) error {
	return l.Log(AuditEntry{
		EventType:  AuditPromote,
		NodeID:     nodeID,
		SourceTier: sourceTier,
		TargetTier: targetTier,
		Result:     &AuditResult{Success: true, NodesAffected: 1},
	})
}

// LogDemotion logs a tier demotion operation.
func (l *AuditLogger) LogDemotion(nodeID string, sourceTier, targetTier hypergraph.Tier, err error) error {
	entry := AuditEntry{
		EventType:  AuditDemote,
		NodeID:     nodeID,
		SourceTier: sourceTier,
		TargetTier: targetTier,
		Result:     &AuditResult{Success: err == nil, NodesAffected: 1},
	}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	return l.Log(entry)
}

// LogDecay logs a decay operation.
func (l *AuditLogger) LogDecay(result *DecayResult, err error) error {
	entry := AuditEntry{
		EventType: AuditDecay,
		Duration:  result.Duration,
		Details: map[string]any{
			"nodes_processed": result.NodesProcessed,
			"nodes_decayed":   result.NodesDecayed,
		},
		Result: &AuditResult{
			Success:       err == nil,
			NodesAffected: result.NodesDecayed,
		},
	}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	return l.Log(entry)
}

// LogArchive logs an archive operation.
func (l *AuditLogger) LogArchive(result *DecayResult, err error) error {
	entry := AuditEntry{
		EventType: AuditArchive,
		Duration:  result.Duration,
		Details: map[string]any{
			"nodes_processed": result.NodesProcessed,
			"nodes_archived":  result.NodesArchived,
		},
		Result: &AuditResult{
			Success:       err == nil,
			NodesAffected: result.NodesArchived,
		},
	}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	return l.Log(entry)
}

// LogRestore logs a restore from archive operation.
func (l *AuditLogger) LogRestore(nodeID string, err error) error {
	entry := AuditEntry{
		EventType:  AuditRestore,
		NodeID:     nodeID,
		SourceTier: hypergraph.TierArchive,
		TargetTier: hypergraph.TierLongterm,
		Result:     &AuditResult{Success: err == nil, NodesAffected: 1},
	}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	return l.Log(entry)
}

// LogPrune logs a prune operation.
func (l *AuditLogger) LogPrune(result *DecayResult, err error) error {
	entry := AuditEntry{
		EventType: AuditPrune,
		Duration:  result.Duration,
		Details: map[string]any{
			"nodes_processed": result.NodesProcessed,
			"nodes_pruned":    result.NodesPruned,
		},
		Result: &AuditResult{
			Success:       err == nil,
			NodesAffected: result.NodesPruned,
		},
	}
	if err != nil {
		entry.Result.Error = err.Error()
	}
	return l.Log(entry)
}

// LogAccess logs a node access event.
func (l *AuditLogger) LogAccess(nodeID string) error {
	return l.Log(AuditEntry{
		EventType: AuditAccess,
		NodeID:    nodeID,
		Result:    &AuditResult{Success: true, NodesAffected: 1},
	})
}

// GetRecentEntries returns recent audit entries from the buffer.
func (l *AuditLogger) GetRecentEntries(limit int) []AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	if limit <= 0 || limit > len(l.entries) {
		limit = len(l.entries)
	}

	// Return most recent entries
	start := len(l.entries) - limit
	result := make([]AuditEntry, limit)
	copy(result, l.entries[start:])
	return result
}

// GetEntriesByType returns entries of a specific type.
func (l *AuditLogger) GetEntriesByType(eventType AuditEventType, limit int) []AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	var result []AuditEntry
	// Iterate in reverse for most recent first
	for i := len(l.entries) - 1; i >= 0 && len(result) < limit; i-- {
		if l.entries[i].EventType == eventType {
			result = append(result, l.entries[i])
		}
	}
	return result
}

// GetEntriesByNode returns entries for a specific node.
func (l *AuditLogger) GetEntriesByNode(nodeID string, limit int) []AuditEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	var result []AuditEntry
	for i := len(l.entries) - 1; i >= 0 && len(result) < limit; i-- {
		entry := l.entries[i]
		if entry.NodeID == nodeID {
			result = append(result, entry)
			continue
		}
		// Check NodeIDs array
		for _, id := range entry.NodeIDs {
			if id == nodeID {
				result = append(result, entry)
				break
			}
		}
	}
	return result
}

// GetStats returns summary statistics from the audit log.
func (l *AuditLogger) GetStats() *AuditStats {
	l.mu.Lock()
	defer l.mu.Unlock()

	stats := &AuditStats{
		TotalEntries: len(l.entries),
		ByType:       make(map[AuditEventType]int),
	}

	var totalDuration time.Duration
	durationCount := 0

	for _, entry := range l.entries {
		stats.ByType[entry.EventType]++

		if entry.Result != nil {
			if entry.Result.Success {
				stats.SuccessCount++
			} else {
				stats.ErrorCount++
			}
			stats.TotalNodesAffected += entry.Result.NodesAffected
		}

		if entry.Duration > 0 {
			totalDuration += entry.Duration
			durationCount++
		}
	}

	if durationCount > 0 {
		stats.AverageDuration = totalDuration / time.Duration(durationCount)
	}

	return stats
}

// AuditStats contains audit log statistics.
type AuditStats struct {
	TotalEntries       int
	SuccessCount       int
	ErrorCount         int
	TotalNodesAffected int
	AverageDuration    time.Duration
	ByType             map[AuditEventType]int
}

// Clear clears the in-memory buffer.
func (l *AuditLogger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
}

// Close closes the audit logger.
func (l *AuditLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// SetEnabled enables or disables logging.
func (l *AuditLogger) SetEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = enabled
}

// IsEnabled returns whether logging is enabled.
func (l *AuditLogger) IsEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}
