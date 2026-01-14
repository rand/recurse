package hypergraph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// EvolutionOperation represents the type of evolution operation.
type EvolutionOperation string

const (
	EvolutionCreate      EvolutionOperation = "create"
	EvolutionConsolidate EvolutionOperation = "consolidate"
	EvolutionPromote     EvolutionOperation = "promote"
	EvolutionDecay       EvolutionOperation = "decay"
	EvolutionPrune       EvolutionOperation = "prune"
	EvolutionArchive     EvolutionOperation = "archive"
)

// EvolutionEntry represents a single evolution log entry.
type EvolutionEntry struct {
	ID        int64              `json:"id,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
	Operation EvolutionOperation `json:"operation"`
	NodeIDs   []string           `json:"node_ids,omitempty"`
	FromTier  Tier               `json:"from_tier,omitempty"`
	ToTier    Tier               `json:"to_tier,omitempty"`
	Reasoning string             `json:"reasoning,omitempty"`
	Metadata  json.RawMessage    `json:"metadata,omitempty"`
}

// RecordEvolution inserts a new entry into the evolution_log table.
func (s *Store) RecordEvolution(ctx context.Context, entry *EvolutionEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	var nodeIDsJSON []byte
	if len(entry.NodeIDs) > 0 {
		var err error
		nodeIDsJSON, err = json.Marshal(entry.NodeIDs)
		if err != nil {
			return fmt.Errorf("marshal node_ids: %w", err)
		}
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO evolution_log (timestamp, operation, node_ids, from_tier, to_tier, reasoning, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		entry.Timestamp, entry.Operation, nullString(string(nodeIDsJSON)),
		nullString(string(entry.FromTier)), nullString(string(entry.ToTier)),
		nullString(entry.Reasoning), entry.Metadata,
	)
	if err != nil {
		return fmt.Errorf("insert evolution entry: %w", err)
	}

	id, err := result.LastInsertId()
	if err == nil {
		entry.ID = id
	}

	return nil
}

// EvolutionFilter defines criteria for filtering evolution log entries.
type EvolutionFilter struct {
	Operations []EvolutionOperation
	FromTiers  []Tier
	ToTiers    []Tier
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// ListEvolutionLog retrieves evolution log entries matching the given filter.
func (s *Store) ListEvolutionLog(ctx context.Context, filter EvolutionFilter) ([]*EvolutionEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT id, timestamp, operation, node_ids, from_tier, to_tier, reasoning, metadata
		FROM evolution_log WHERE 1=1`
	var args []any

	if len(filter.Operations) > 0 {
		query += " AND operation IN (?" + repeatString(",?", len(filter.Operations)-1) + ")"
		for _, op := range filter.Operations {
			args = append(args, op)
		}
	}

	if len(filter.FromTiers) > 0 {
		query += " AND from_tier IN (?" + repeatString(",?", len(filter.FromTiers)-1) + ")"
		for _, t := range filter.FromTiers {
			args = append(args, t)
		}
	}

	if len(filter.ToTiers) > 0 {
		query += " AND to_tier IN (?" + repeatString(",?", len(filter.ToTiers)-1) + ")"
		for _, t := range filter.ToTiers {
			args = append(args, t)
		}
	}

	if filter.Since != nil {
		query += " AND timestamp >= ?"
		args = append(args, *filter.Since)
	}

	if filter.Until != nil {
		query += " AND timestamp <= ?"
		args = append(args, *filter.Until)
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query evolution log: %w", err)
	}
	defer rows.Close()

	var entries []*EvolutionEntry
	for rows.Next() {
		entry, err := scanEvolutionEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// CountEvolutionLog returns the count of evolution log entries matching the filter.
func (s *Store) CountEvolutionLog(ctx context.Context, filter EvolutionFilter) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT COUNT(*) FROM evolution_log WHERE 1=1"
	var args []any

	if len(filter.Operations) > 0 {
		query += " AND operation IN (?" + repeatString(",?", len(filter.Operations)-1) + ")"
		for _, op := range filter.Operations {
			args = append(args, op)
		}
	}

	if filter.Since != nil {
		query += " AND timestamp >= ?"
		args = append(args, *filter.Since)
	}

	var count int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count evolution log: %w", err)
	}

	return count, nil
}

// scanEvolutionEntry scans an evolution entry from the database row.
func scanEvolutionEntry(rows interface{ Scan(...any) error }) (*EvolutionEntry, error) {
	var entry EvolutionEntry
	var nodeIDsJSON, fromTier, toTier, reasoning, metadata sql.NullString

	err := rows.Scan(
		&entry.ID, &entry.Timestamp, &entry.Operation,
		&nodeIDsJSON, &fromTier, &toTier, &reasoning, &metadata,
	)
	if err != nil {
		return nil, fmt.Errorf("scan evolution entry: %w", err)
	}

	if nodeIDsJSON.Valid && nodeIDsJSON.String != "" {
		if err := json.Unmarshal([]byte(nodeIDsJSON.String), &entry.NodeIDs); err != nil {
			return nil, fmt.Errorf("unmarshal node_ids: %w", err)
		}
	}

	if fromTier.Valid {
		entry.FromTier = Tier(fromTier.String)
	}
	if toTier.Valid {
		entry.ToTier = Tier(toTier.String)
	}
	entry.Reasoning = reasoning.String
	if metadata.Valid {
		entry.Metadata = json.RawMessage(metadata.String)
	}

	return &entry, nil
}
