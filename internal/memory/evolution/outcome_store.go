package evolution

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rand/recurse/internal/memory/hypergraph"
)

// SQLiteOutcomeStore implements OutcomeStore using the hypergraph database.
type SQLiteOutcomeStore struct {
	store *hypergraph.Store
}

// NewSQLiteOutcomeStore creates a new SQLite-backed outcome store.
func NewSQLiteOutcomeStore(store *hypergraph.Store) *SQLiteOutcomeStore {
	return &SQLiteOutcomeStore{store: store}
}

// RecordOutcome records a retrieval outcome (implements hypergraph.OutcomeRecorder).
func (s *SQLiteOutcomeStore) RecordOutcome(ctx context.Context, outcome hypergraph.RetrievalOutcome) error {
	db := s.store.DB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	query := `
		INSERT INTO retrieval_outcomes (
			query_hash, query_type, node_id, node_type, node_subtype,
			relevance_score, was_used, context_tokens, latency_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := db.ExecContext(ctx, query,
		outcome.QueryHash,
		outcome.QueryType,
		outcome.NodeID,
		outcome.NodeType,
		outcome.NodeSubtype,
		outcome.RelevanceScore,
		outcome.WasUsed,
		outcome.ContextTokens,
		outcome.LatencyMs,
	)
	if err != nil {
		return fmt.Errorf("insert outcome: %w", err)
	}

	return nil
}

// QueryOutcomes retrieves outcomes within a time window.
func (s *SQLiteOutcomeStore) QueryOutcomes(ctx context.Context, since time.Time) ([]RetrievalOutcome, error) {
	db := s.store.DB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	query := `
		SELECT id, timestamp, query_hash, query_type, node_id, node_type,
		       node_subtype, relevance_score, was_used, context_tokens, latency_ms
		FROM retrieval_outcomes
		WHERE timestamp >= ?
		ORDER BY timestamp DESC
	`

	rows, err := db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("query outcomes: %w", err)
	}
	defer rows.Close()

	return scanOutcomes(rows)
}

// QueryOutcomesByNodeType retrieves outcomes for a specific node type.
func (s *SQLiteOutcomeStore) QueryOutcomesByNodeType(ctx context.Context, nodeType string, since time.Time) ([]RetrievalOutcome, error) {
	db := s.store.DB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	query := `
		SELECT id, timestamp, query_hash, query_type, node_id, node_type,
		       node_subtype, relevance_score, was_used, context_tokens, latency_ms
		FROM retrieval_outcomes
		WHERE node_type = ? AND timestamp >= ?
		ORDER BY timestamp DESC
	`

	rows, err := db.QueryContext(ctx, query, nodeType, since)
	if err != nil {
		return nil, fmt.Errorf("query outcomes by node type: %w", err)
	}
	defer rows.Close()

	return scanOutcomes(rows)
}

// QueryOutcomesByQueryType retrieves outcomes for a specific query type.
func (s *SQLiteOutcomeStore) QueryOutcomesByQueryType(ctx context.Context, queryType string, since time.Time) ([]RetrievalOutcome, error) {
	db := s.store.DB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	query := `
		SELECT id, timestamp, query_hash, query_type, node_id, node_type,
		       node_subtype, relevance_score, was_used, context_tokens, latency_ms
		FROM retrieval_outcomes
		WHERE query_type = ? AND timestamp >= ?
		ORDER BY timestamp DESC
	`

	rows, err := db.QueryContext(ctx, query, queryType, since)
	if err != nil {
		return nil, fmt.Errorf("query outcomes by query type: %w", err)
	}
	defer rows.Close()

	return scanOutcomes(rows)
}

// GetOutcomeStats returns aggregated statistics.
func (s *SQLiteOutcomeStore) GetOutcomeStats(ctx context.Context, since time.Time) (*OutcomeStats, error) {
	db := s.store.DB()
	if db == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Get overall stats
	overallQuery := `
		SELECT
			COUNT(*) as total,
			COALESCE(AVG(relevance_score), 0) as avg_relevance,
			COALESCE(SUM(CASE WHEN was_used THEN 1 ELSE 0 END) * 1.0 / NULLIF(COUNT(*), 0), 0) as hit_rate,
			COALESCE(AVG(latency_ms), 0) as avg_latency
		FROM retrieval_outcomes
		WHERE timestamp >= ?
	`

	var total int
	var avgRelevance, hitRate float64
	var avgLatency int

	err := db.QueryRowContext(ctx, overallQuery, since).Scan(&total, &avgRelevance, &hitRate, &avgLatency)
	if err != nil {
		return nil, fmt.Errorf("query overall stats: %w", err)
	}

	stats := &OutcomeStats{
		TotalOutcomes: total,
		AvgRelevance:  avgRelevance,
		HitRate:       hitRate,
		AvgLatencyMs:  avgLatency,
		ByNodeType:    make(map[string]TypeStats),
		ByQueryType:   make(map[string]TypeStats),
	}

	// Get stats by node type
	nodeTypeQuery := `
		SELECT
			node_type,
			COUNT(*) as count,
			COALESCE(AVG(relevance_score), 0) as avg_relevance,
			COALESCE(SUM(CASE WHEN was_used THEN 1 ELSE 0 END) * 1.0 / NULLIF(COUNT(*), 0), 0) as hit_rate,
			COALESCE(AVG(latency_ms), 0) as avg_latency
		FROM retrieval_outcomes
		WHERE timestamp >= ?
		GROUP BY node_type
	`

	rows, err := db.QueryContext(ctx, nodeTypeQuery, since)
	if err != nil {
		return nil, fmt.Errorf("query node type stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nodeType string
		var ts TypeStats
		if err := rows.Scan(&nodeType, &ts.Count, &ts.AvgRelevance, &ts.HitRate, &ts.AvgLatencyMs); err != nil {
			return nil, fmt.Errorf("scan node type stats: %w", err)
		}
		stats.ByNodeType[nodeType] = ts
	}

	// Get stats by query type
	queryTypeQuery := `
		SELECT
			query_type,
			COUNT(*) as count,
			COALESCE(AVG(relevance_score), 0) as avg_relevance,
			COALESCE(SUM(CASE WHEN was_used THEN 1 ELSE 0 END) * 1.0 / NULLIF(COUNT(*), 0), 0) as hit_rate,
			COALESCE(AVG(latency_ms), 0) as avg_latency
		FROM retrieval_outcomes
		WHERE timestamp >= ?
		GROUP BY query_type
	`

	rows2, err := db.QueryContext(ctx, queryTypeQuery, since)
	if err != nil {
		return nil, fmt.Errorf("query query type stats: %w", err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var queryType string
		var ts TypeStats
		if err := rows2.Scan(&queryType, &ts.Count, &ts.AvgRelevance, &ts.HitRate, &ts.AvgLatencyMs); err != nil {
			return nil, fmt.Errorf("scan query type stats: %w", err)
		}
		stats.ByQueryType[queryType] = ts
	}

	return stats, nil
}

// MarkUsed marks a retrieval outcome as used.
func (s *SQLiteOutcomeStore) MarkUsed(ctx context.Context, nodeID, queryHash string) error {
	db := s.store.DB()
	if db == nil {
		return fmt.Errorf("database not available")
	}

	query := `
		UPDATE retrieval_outcomes
		SET was_used = 1
		WHERE node_id = ? AND query_hash = ? AND was_used = 0
	`

	_, err := db.ExecContext(ctx, query, nodeID, queryHash)
	if err != nil {
		return fmt.Errorf("mark used: %w", err)
	}

	return nil
}

// Prune removes old outcomes older than the given time.
func (s *SQLiteOutcomeStore) Prune(ctx context.Context, before time.Time) (int64, error) {
	db := s.store.DB()
	if db == nil {
		return 0, fmt.Errorf("database not available")
	}

	query := `DELETE FROM retrieval_outcomes WHERE timestamp < ?`

	result, err := db.ExecContext(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("prune outcomes: %w", err)
	}

	return result.RowsAffected()
}

// scanOutcomes scans rows into RetrievalOutcome slice.
func scanOutcomes(rows *sql.Rows) ([]RetrievalOutcome, error) {
	var outcomes []RetrievalOutcome

	for rows.Next() {
		var o RetrievalOutcome
		var subtype sql.NullString
		var timestamp time.Time

		err := rows.Scan(
			&o.ID,
			&timestamp,
			&o.QueryHash,
			&o.QueryType,
			&o.NodeID,
			&o.NodeType,
			&subtype,
			&o.RelevanceScore,
			&o.WasUsed,
			&o.ContextTokens,
			&o.LatencyMs,
		)
		if err != nil {
			return nil, fmt.Errorf("scan outcome: %w", err)
		}

		o.Timestamp = timestamp
		if subtype.Valid {
			o.NodeSubtype = subtype.String
		}

		outcomes = append(outcomes, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return outcomes, nil
}
