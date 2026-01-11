package rlm

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rand/recurse/internal/tui/components/dialogs/rlmtrace"
)

//go:embed trace_schema.sql
var traceSchemaSQL string

// PersistentTraceProvider implements rlmtrace.TraceProvider with SQLite persistence.
type PersistentTraceProvider struct {
	db        *sql.DB
	mu        sync.RWMutex
	ownsDB    bool // whether we own the db connection
	sessionID string
}

// PersistentTraceConfig configures the persistent trace provider.
type PersistentTraceConfig struct {
	// DB is an existing database connection to use.
	// If nil, a new in-memory database is created.
	DB *sql.DB

	// SessionID optionally links trace events to a session.
	SessionID string
}

// NewPersistentTraceProvider creates a new persistent trace provider.
func NewPersistentTraceProvider(cfg PersistentTraceConfig) (*PersistentTraceProvider, error) {
	var db *sql.DB
	var ownsDB bool

	if cfg.DB != nil {
		db = cfg.DB
		ownsDB = false
	} else {
		// Create in-memory database
		var err error
		db, err = sql.Open("sqlite3", "file::memory:?cache=shared")
		if err != nil {
			return nil, fmt.Errorf("open database: %w", err)
		}
		ownsDB = true
	}

	p := &PersistentTraceProvider{
		db:        db,
		ownsDB:    ownsDB,
		sessionID: cfg.SessionID,
	}

	// Initialize schema
	if err := p.initSchema(); err != nil {
		if ownsDB {
			db.Close()
		}
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return p, nil
}

// initSchema creates the trace tables if they don't exist.
func (p *PersistentTraceProvider) initSchema() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, err := p.db.Exec(traceSchemaSQL)
	if err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}

	return nil
}

// Close closes the database if owned.
func (p *PersistentTraceProvider) Close() error {
	if p.ownsDB && p.db != nil {
		return p.db.Close()
	}
	return nil
}

// SetSessionID sets the current session ID for new events.
func (p *PersistentTraceProvider) SetSessionID(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessionID = sessionID
}

// RecordEvent implements TraceRecorder interface for the RLM controller.
func (p *PersistentTraceProvider) RecordEvent(event TraceEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ctx := context.Background()

	var sessionID sql.NullString
	if p.sessionID != "" {
		sessionID = sql.NullString{String: p.sessionID, Valid: true}
	}

	var parentID sql.NullString
	if event.ParentID != "" {
		parentID = sql.NullString{String: event.ParentID, Valid: true}
	}

	var details sql.NullString
	if event.Details != "" {
		details = sql.NullString{String: event.Details, Valid: true}
	}

	_, err := p.db.ExecContext(ctx, `
		INSERT INTO trace_events (
			id, session_id, type, action, details, tokens,
			duration_ns, depth, parent_id, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		event.ID,
		sessionID,
		mapEventType(event.Type),
		event.Action,
		details,
		event.Tokens,
		event.Duration.Nanoseconds(),
		event.Depth,
		parentID,
		event.Status,
		event.Timestamp.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert trace event: %w", err)
	}

	return nil
}

// GetEvents implements rlmtrace.TraceProvider.
func (p *PersistentTraceProvider) GetEvents(limit int) ([]rlmtrace.TraceEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	ctx := context.Background()
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, session_id, type, action, details, tokens,
		       duration_ns, depth, parent_id, status, created_at
		FROM trace_events
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query trace events: %w", err)
	}
	defer rows.Close()

	var events []rlmtrace.TraceEvent
	for rows.Next() {
		var (
			id         string
			sessionID  sql.NullString
			eventType  string
			action     string
			details    sql.NullString
			tokens     int
			durationNs int64
			depth      int
			parentID   sql.NullString
			status     string
			createdAt  int64
		)

		if err := rows.Scan(
			&id, &sessionID, &eventType, &action, &details, &tokens,
			&durationNs, &depth, &parentID, &status, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan trace event: %w", err)
		}

		event := rlmtrace.TraceEvent{
			ID:        id,
			Type:      rlmtrace.TraceEventType(eventType),
			Action:    action,
			Tokens:    tokens,
			Duration:  time.Duration(durationNs),
			Timestamp: time.UnixMilli(createdAt),
			Depth:     depth,
			Status:    status,
		}

		if details.Valid {
			event.Details = details.String
		}
		if parentID.Valid {
			event.ParentID = parentID.String
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace events: %w", err)
	}

	// Reverse to get chronological order (oldest first)
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}

	return events, nil
}

// GetEvent implements rlmtrace.TraceProvider.
func (p *PersistentTraceProvider) GetEvent(id string) (*rlmtrace.TraceEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ctx := context.Background()
	row := p.db.QueryRowContext(ctx, `
		SELECT id, session_id, type, action, details, tokens,
		       duration_ns, depth, parent_id, status, created_at
		FROM trace_events
		WHERE id = ?
	`, id)

	var (
		eventID    string
		sessionID  sql.NullString
		eventType  string
		action     string
		details    sql.NullString
		tokens     int
		durationNs int64
		depth      int
		parentID   sql.NullString
		status     string
		createdAt  int64
	)

	if err := row.Scan(
		&eventID, &sessionID, &eventType, &action, &details, &tokens,
		&durationNs, &depth, &parentID, &status, &createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan trace event: %w", err)
	}

	event := &rlmtrace.TraceEvent{
		ID:        eventID,
		Type:      rlmtrace.TraceEventType(eventType),
		Action:    action,
		Tokens:    tokens,
		Duration:  time.Duration(durationNs),
		Timestamp: time.UnixMilli(createdAt),
		Depth:     depth,
		Status:    status,
	}

	if details.Valid {
		event.Details = details.String
	}
	if parentID.Valid {
		event.ParentID = parentID.String
	}

	return event, nil
}

// ClearEvents implements rlmtrace.TraceProvider.
func (p *PersistentTraceProvider) ClearEvents() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ctx := context.Background()

	// Delete all events
	if _, err := p.db.ExecContext(ctx, "DELETE FROM trace_events"); err != nil {
		return fmt.Errorf("delete trace events: %w", err)
	}

	// Reset stats
	if _, err := p.db.ExecContext(ctx, `
		UPDATE trace_stats SET
			total_events = 0,
			total_tokens = 0,
			total_duration_ns = 0,
			max_depth = 0,
			events_by_type = '{}'
		WHERE id = 1
	`); err != nil {
		return fmt.Errorf("reset trace stats: %w", err)
	}

	return nil
}

// Stats implements rlmtrace.TraceProvider.
func (p *PersistentTraceProvider) Stats() rlmtrace.TraceStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ctx := context.Background()
	row := p.db.QueryRowContext(ctx, `
		SELECT total_events, total_tokens, total_duration_ns, max_depth, events_by_type
		FROM trace_stats
		WHERE id = 1
	`)

	var (
		totalEvents    int
		totalTokens    int
		totalDurationNs int64
		maxDepth       int
		eventsByTypeJSON string
	)

	if err := row.Scan(&totalEvents, &totalTokens, &totalDurationNs, &maxDepth, &eventsByTypeJSON); err != nil {
		// Return empty stats on error
		return rlmtrace.TraceStats{
			EventsByType: make(map[rlmtrace.TraceEventType]int),
		}
	}

	eventsByType := make(map[rlmtrace.TraceEventType]int)
	if eventsByTypeJSON != "" {
		var rawMap map[string]int
		if err := json.Unmarshal([]byte(eventsByTypeJSON), &rawMap); err == nil {
			for k, v := range rawMap {
				eventsByType[rlmtrace.TraceEventType(k)] = v
			}
		}
	}

	return rlmtrace.TraceStats{
		TotalEvents:   totalEvents,
		TotalTokens:   totalTokens,
		TotalDuration: time.Duration(totalDurationNs),
		MaxDepth:      maxDepth,
		EventsByType:  eventsByType,
	}
}

// GetEventsBySession returns events for a specific session.
func (p *PersistentTraceProvider) GetEventsBySession(sessionID string, limit int) ([]rlmtrace.TraceEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	ctx := context.Background()
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, session_id, type, action, details, tokens,
		       duration_ns, depth, parent_id, status, created_at
		FROM trace_events
		WHERE session_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("query trace events: %w", err)
	}
	defer rows.Close()

	return p.scanEvents(rows)
}

// GetEventsByParent returns child events of a parent.
func (p *PersistentTraceProvider) GetEventsByParent(parentID string) ([]rlmtrace.TraceEvent, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ctx := context.Background()
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, session_id, type, action, details, tokens,
		       duration_ns, depth, parent_id, status, created_at
		FROM trace_events
		WHERE parent_id = ?
		ORDER BY created_at ASC
	`, parentID)
	if err != nil {
		return nil, fmt.Errorf("query trace events: %w", err)
	}
	defer rows.Close()

	return p.scanEvents(rows)
}

// scanEvents scans rows into trace events.
func (p *PersistentTraceProvider) scanEvents(rows *sql.Rows) ([]rlmtrace.TraceEvent, error) {
	var events []rlmtrace.TraceEvent
	for rows.Next() {
		var (
			id         string
			sessionID  sql.NullString
			eventType  string
			action     string
			details    sql.NullString
			tokens     int
			durationNs int64
			depth      int
			parentID   sql.NullString
			status     string
			createdAt  int64
		)

		if err := rows.Scan(
			&id, &sessionID, &eventType, &action, &details, &tokens,
			&durationNs, &depth, &parentID, &status, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan trace event: %w", err)
		}

		event := rlmtrace.TraceEvent{
			ID:        id,
			Type:      rlmtrace.TraceEventType(eventType),
			Action:    action,
			Tokens:    tokens,
			Duration:  time.Duration(durationNs),
			Timestamp: time.UnixMilli(createdAt),
			Depth:     depth,
			Status:    status,
		}

		if details.Valid {
			event.Details = details.String
		}
		if parentID.Valid {
			event.ParentID = parentID.String
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace events: %w", err)
	}

	return events, nil
}

// Ensure PersistentTraceProvider implements rlmtrace.TraceProvider
var _ rlmtrace.TraceProvider = (*PersistentTraceProvider)(nil)
